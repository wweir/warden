package reqlog

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/Masterminds/sprig/v3"
	"github.com/go-resty/resty/v2"
)

const httpLogQueueSize = 256

// HTTPLogger pushes log records to an HTTP endpoint using resty.
// Records are serialized via a configurable Go template (sprig functions available).
// Sending is asynchronous; a full queue drops the newest record.
type HTTPLogger struct {
	client *resty.Client
	method string
	tmpl   *template.Template // nil means default JSON marshal

	queue  chan Record
	wg     sync.WaitGroup
	cancel context.CancelFunc
}

// HTTPLoggerConfig holds resolved parameters for HTTPLogger.
type HTTPLoggerConfig struct {
	URL          string
	Method       string
	Headers      map[string]string
	BodyTemplate string
	Timeout      string
	Retry        int
}

// NewHTTPLogger creates and starts an HTTPLogger.
func NewHTTPLogger(cfg HTTPLoggerConfig) (*HTTPLogger, error) {
	method := strings.ToUpper(cfg.Method)
	if method == "" {
		method = http.MethodPost
	}

	timeout := 5 * time.Second
	if cfg.Timeout != "" {
		d, err := time.ParseDuration(cfg.Timeout)
		if err != nil {
			return nil, fmt.Errorf("parse timeout %q: %w", cfg.Timeout, err)
		}
		timeout = d
	}

	retry := cfg.Retry
	if retry < 0 {
		retry = 0
	}

	var tmpl *template.Template
	if cfg.BodyTemplate != "" {
		t, err := template.New("body").Funcs(sprig.FuncMap()).Parse(cfg.BodyTemplate)
		if err != nil {
			return nil, fmt.Errorf("parse body_template: %w", err)
		}
		tmpl = t
	}

	client := resty.New().
		SetBaseURL(cfg.URL).
		SetTimeout(timeout).
		SetRetryCount(retry).
		SetHeaders(map[string]string{"Content-Type": "application/json"})
	for k, v := range cfg.Headers {
		client.SetHeader(k, v)
	}

	ctx, cancel := context.WithCancel(context.Background())
	h := &HTTPLogger{
		client: client,
		method: method,
		tmpl:   tmpl,
		queue:  make(chan Record, httpLogQueueSize),
		cancel: cancel,
	}

	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		h.worker(ctx)
	}()

	return h, nil
}

// Log enqueues a record for async delivery. Drops silently if queue is full.
func (h *HTTPLogger) Log(r Record) {
	select {
	case h.queue <- r:
	default:
		slog.Warn("HTTP log queue full, dropping record", "request_id", r.RequestID)
	}
}

// Close stops the worker and waits for it to drain in-flight records.
func (h *HTTPLogger) Close() error {
	h.cancel()
	h.wg.Wait()
	return nil
}

func (h *HTTPLogger) worker(ctx context.Context) {
	for {
		select {
		case r := <-h.queue:
			h.send(r)
		case <-ctx.Done():
			// drain remaining records
			for {
				select {
				case r := <-h.queue:
					h.send(r)
				default:
					return
				}
			}
		}
	}
}

func (h *HTTPLogger) send(r Record) {
	body, err := h.renderBody(r)
	if err != nil {
		slog.Warn("HTTP log render failed", "request_id", r.RequestID, "error", err)
		return
	}

	resp, err := h.client.R().
		SetContext(context.Background()).
		SetBody(body).
		Execute(h.method, "")
	if err != nil {
		slog.Warn("HTTP log send failed", "request_id", r.RequestID, "error", err)
		return
	}
	if resp.IsError() {
		slog.Warn("HTTP log send failed", "request_id", r.RequestID, "status", resp.StatusCode())
	}
}

// renderBody produces the HTTP request body bytes for a record.
// If a template is configured it is executed with .Record bound to r;
// otherwise the record is marshalled as plain JSON.
func (h *HTTPLogger) renderBody(r Record) ([]byte, error) {
	if h.tmpl == nil {
		return json.Marshal(r)
	}
	var buf bytes.Buffer
	if err := h.tmpl.Execute(&buf, map[string]any{"Record": r}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
