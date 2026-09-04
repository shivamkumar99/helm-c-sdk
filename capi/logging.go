package main

/*
#include <stdint.h>
#include <stdlib.h>
#include "logbridge.h"
*/
import "C"

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"unsafe"

	"github.com/shivamkumar99/helm-c-sdk/pkg/cerrors"
	"github.com/shivamkumar99/helm-c-sdk/pkg/wrapper"
)

// The registered C callback. Guarded: the callback may be swapped while other
// goroutines are logging.
var (
	logCBMu    sync.RWMutex
	logCB      C.helmc_log_callback
	logCBData  unsafe.Pointer
	logCBLevel slog.Level
)

// cLevel maps slog levels onto the frozen HELM_LOG_* enum.
func cLevel(l slog.Level) C.int32_t {
	switch {
	case l < slog.LevelInfo:
		return 0 // HELM_LOG_DEBUG
	case l < slog.LevelWarn:
		return 1 // HELM_LOG_INFO
	case l < slog.LevelError:
		return 2 // HELM_LOG_WARN
	default:
		return 3 // HELM_LOG_ERROR
	}
}

// cSlogLevel maps a HELM_LOG_* minimum onto slog levels.
func cSlogLevel(l C.int32_t) slog.Level {
	switch l {
	case 0:
		return slog.LevelDebug
	case 1:
		return slog.LevelInfo
	case 2:
		return slog.LevelWarn
	default:
		return slog.LevelError
	}
}

// cLogHandler is a slog.Handler forwarding records to the registered C
// callback. The message pointer is valid only for the duration of the
// callback; the host must copy it.
type cLogHandler struct {
	attrs []slog.Attr
}

func (h *cLogHandler) Enabled(_ context.Context, l slog.Level) bool {
	logCBMu.RLock()
	defer logCBMu.RUnlock()
	return logCB != nil && l >= logCBLevel
}

func (h *cLogHandler) Handle(_ context.Context, r slog.Record) error {
	var b strings.Builder
	b.WriteString(r.Message)
	appendAttr := func(a slog.Attr) bool {
		fmt.Fprintf(&b, " %s=%v", a.Key, a.Value)
		return true
	}
	for _, a := range h.attrs {
		appendAttr(a)
	}
	r.Attrs(appendAttr)

	// The callback runs while the read lock is held, so
	// helm_set_log_handler (which takes the write lock) cannot return while
	// a delivery is in flight: once it returns, no callback will touch the
	// old user_data again. The documented contract forbids the callback
	// from calling back into helm-c, which is what makes holding the lock
	// across it deadlock-free.
	logCBMu.RLock()
	defer logCBMu.RUnlock()
	if logCB == nil {
		return nil
	}
	msg := C.CString(b.String())
	C.helmc_invoke_log_cb(logCB, cLevel(r.Level), msg, logCBData)
	// Released after the synchronous callback returns, through the one
	// audited unsafe.Pointer site (convert.go freeCString; see
	// docs/DESIGN.md, "Use of unsafe").
	freeCString(msg)
	return nil
}

func (h *cLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	merged := make([]slog.Attr, 0, len(h.attrs)+len(attrs))
	merged = append(merged, h.attrs...)
	merged = append(merged, attrs...)
	return &cLogHandler{attrs: merged}
}

func (h *cLogHandler) WithGroup(string) slog.Handler { return h }

// helm_set_log_handler routes helm-c's internal logging (helm SDK log/slog
// records) to a host callback. Applies to configs created AFTERWARDS. Pass a
// NULL callback to restore silence (the default). min_level is a HELM_LOG_*
// value. The callback may be invoked concurrently from arbitrary threads and
// must copy `message` before returning; it must not call back into helm-c.
//
//export helm_set_log_handler
func helm_set_log_handler(cb C.helmc_log_callback, userData unsafe.Pointer, minLevel C.int32_t) (code C.int32_t) {
	var discard **C.char
	defer recoverToCode(&code, discard)

	logCBMu.Lock()
	logCB = cb
	logCBData = userData
	logCBLevel = cSlogLevel(minLevel)
	logCBMu.Unlock()

	if cb == nil {
		wrapper.SetLogHandler(nil)
	} else {
		wrapper.SetLogHandler(&cLogHandler{})
	}
	return C.int32_t(cerrors.CodeOK)
}
