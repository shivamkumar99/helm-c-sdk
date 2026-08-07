package wrapper

import (
	"log/slog"
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
