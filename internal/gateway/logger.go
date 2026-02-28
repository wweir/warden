package gateway

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/wweir/warden/config"
	"github.com/wweir/warden/internal/reqlog"
)

// multiLogger fans out Log calls to multiple Logger backends.
type multiLogger struct {
	loggers []reqlog.Logger
}

func (m *multiLogger) Log(r reqlog.Record) {
	for _, l := range m.loggers {
		l.Log(r)
	}
}

func (m *multiLogger) Close() error {
	var first error
	for _, l := range m.loggers {
		if err := l.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// newLogger builds a Logger from the configured targets.
// Returns nil if there are no targets.
func newLogger(cfg *config.LogConfig) reqlog.Logger {
	if cfg == nil || len(cfg.Targets) == 0 {
		return nil
	}

	var loggers []reqlog.Logger
	for i, t := range cfg.Targets {
		l, err := buildTarget(i, t)
		if err != nil {
			slog.Warn("Failed to create log target", "index", i, "type", t.Type, "error", err)
			continue
		}
		loggers = append(loggers, l)
	}

	switch len(loggers) {
	case 0:
		return nil
	case 1:
		return loggers[0]
	default:
		return &multiLogger{loggers: loggers}
	}
}

func buildTarget(i int, t *config.LogTarget) (reqlog.Logger, error) {
	switch t.Type {
	case "file":
		return reqlog.NewFileLogger(t.Dir)
	case "http":
		wh := t.WebhookCfg
		return reqlog.NewHTTPLogger(reqlog.HTTPLoggerConfig{
			URL:          wh.URL,
			Method:       wh.Method,
			Headers:      wh.Headers,
			BodyTemplate: wh.BodyTemplate,
			Timeout:      wh.Timeout,
			Retry:        wh.Retry,
		})
	default:
		return nil, fmt.Errorf("unknown log target type %q at index %d", t.Type, i)
	}
}

// logRequest logs an incoming request with provider and model info.
func logRequest(r *http.Request, provider, model string) {
	slog.Info("Request received", "method", r.Method, "path", r.URL.Path,
		"provider", provider, "model", model)
}
