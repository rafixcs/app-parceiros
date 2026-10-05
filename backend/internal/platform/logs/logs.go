// Package logs cria o logger do processo: JSON no stdout, com os nomes de
// campo que o Cloud Logging reconhece (severity e message), para que o nível
// de cada linha apareça certo no console da cloud.
package logs

import (
	"io"
	"log/slog"
)

func New(w io.Writer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{ReplaceAttr: renomear}))
}

func renomear(grupos []string, a slog.Attr) slog.Attr {
	if len(grupos) > 0 {
		return a
	}
	switch a.Key {
	case slog.LevelKey:
		// Só o nível do registro é um slog.Level; um atributo do chamador
		// chamado "level" fica como está.
		if l, ok := a.Value.Any().(slog.Level); ok {
			return slog.String("severity", severidade(l))
		}
	case slog.MessageKey:
		a.Key = "message"
	}
	return a
}

func severidade(l slog.Level) string {
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
