package wrapper

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"
)

// The library is silent by default; the host opts in through
// helm_set_log_handler, which installs a slog.Handler here. Configs created
// afterwards use it — set the handler before helm_config_new.

var (
	logMu      sync.RWMutex
	logHandler slog.Handler = slog.DiscardHandler
)

// SetLogHandler installs the handler future configs log through. nil restores
// silence. Safe from any thread.
func SetLogHandler(h slog.Handler) {
	logMu.Lock()
	defer logMu.Unlock()
	if h == nil {
		logHandler = slog.DiscardHandler
		return
	}
	logHandler = h
}

// CurrentLogHandler returns the active handler (never nil).
func CurrentLogHandler() slog.Handler {
	logMu.RLock()
	defer logMu.RUnlock()
	return logHandler
}

// handlerWriter adapts the SDK's io.Writer-style output (registry client
// debug, push/dependency progress) onto the installed log handler. The
// handler is resolved per write, so output from long-lived clients follows
// handler changes.
type handlerWriter struct {
	level slog.Level
}

func (w handlerWriter) Write(p []byte) (int, error) {
	for line := range strings.SplitSeq(strings.TrimRight(string(p), "\n"), "\n") {
		if line == "" {
			continue
		}
		slog.New(CurrentLogHandler()).Log(context.Background(), w.level, line)
	}
	return len(p), nil
}

// LogWriter returns an io.Writer that forwards each line to the installed
// log handler at the given level (silent while no handler is set).
func LogWriter(level slog.Level) io.Writer {
	return handlerWriter{level: level}
}
