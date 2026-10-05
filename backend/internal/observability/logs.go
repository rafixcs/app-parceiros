// Package observability builds the process logger: JSON on stdout, with the
// field names Cloud Logging understands (severity and message), so each line
// shows its level correctly in the cloud console.
package observability

import (
	"io"
	"log/slog"
)

func NewLogger(w io.Writer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{ReplaceAttr: rename}))
}

func rename(groups []string, a slog.Attr) slog.Attr {
	if len(groups) > 0 {
		return a
	}
	switch a.Key {
	case slog.LevelKey:
		// Only the record level is a slog.Level; a caller attribute named
		// "level" stays as it is.
		if l, ok := a.Value.Any().(slog.Level); ok {
			return slog.String("severity", severity(l))
		}
	case slog.MessageKey:
		a.Key = "message"
	}
	return a
}

func severity(l slog.Level) string {
	switch {
	case l >= slog.LevelError:
		return "ERROR"
	case l >= slog.LevelWarn:
		return "WARNING"
	case l >= slog.LevelInfo:
		return "INFO"
	default:
		return "DEBUG"
	}
}
