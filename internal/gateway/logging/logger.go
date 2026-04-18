package logging

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/wweir/warden/config"
	"github.com/wweir/warden/internal/reqlog"
)

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

func NewLogger(cfg *config.LogConfig) reqlog.Logger {
	if cfg == nil || len(cfg.Targets) == 0 {
		return nil
	}

	var loggers []reqlog.Logger
	for i, t := range cfg.Targets {
		l, err := buildTarget(i, t)
		if err != nil {
			targetType := "<nil>"
			if t != nil {
				targetType = t.Type
			}
			slog.Warn("Failed to create log target", "index", i, "type", targetType, "error", err)
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
	if t == nil {
		return nil, fmt.Errorf("log target at index %d is nil", i)
	}
	switch t.Type {
	case "file":
		return reqlog.NewFileLogger(t.Dir)
	case "http":
		wh := t.WebhookCfg
		if wh == nil {
			return nil, fmt.Errorf("http log target at index %d has nil webhook config", i)
		}
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

func LogRequest(r *http.Request, provider, model string) {
	slog.Info("Request received", "method", r.Method, "path", r.URL.Path,
		"provider", provider, "model", model)
}
