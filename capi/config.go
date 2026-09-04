package main

/*
#include <stdint.h>
*/
import "C"

import (
	"context"

	"github.com/shivamkumar99/helm-c-sdk/internal/handles"
	"github.com/shivamkumar99/helm-c-sdk/pkg/cerrors"
	"github.com/shivamkumar99/helm-c-sdk/pkg/wrapper"
)

// helm_config_new builds a cluster-connected configuration from a kubeconfig
// (opts_json optional; keys: kubeconfig_path, kube_context, namespace,
// storage_driver). Parsing happens now; the cluster is first contacted by an
// action. *out receives a handle; free with helm_config_free.
//
//export helm_config_new
func helm_config_new(optsJSON *C.char, out *C.uint64_t, errOut **C.char) (code C.int32_t) {
	clearErrorOut(errOut)
	defer recoverToCode(&code, errOut)
	if out == nil {
		return failure(errOut, cerrors.New(cerrors.CodeInvalidArg, "out must not be NULL"))
	}
	opts, err := wrapper.ParseConfigOptions(optionalGoString(optsJSON))
	if err != nil {
		return failure(errOut, err)
	}
	cfg, err := wrapper.NewConfig(opts)
	if err != nil {
		return failure(errOut, err)
	}
	*out = C.uint64_t(registry.Put(handles.TypeConfig, cfg))
	return C.int32_t(cerrors.CodeOK)
}

// helm_config_free releases a config handle (type-checked, idempotent) and
// any resources it owns (e.g. the private temp file backing
// kubeconfig_content).
//
//export helm_config_free
func helm_config_free(h C.uint64_t, errOut **C.char) C.int32_t {
	return freeTyped(h, handles.TypeConfig, errOut, wrapper.CloseConfig)
}

// ctxEntry pairs a context with its cancel func so freeing always releases
// the context's resources.
type ctxEntry struct {
	ctx    context.Context
	cancel context.CancelFunc
}

// ctxEntryFor resolves a context handle. The registry's type tag guarantees
// the entry is a *ctxEntry, so the assertion here can only ever succeed.
func ctxEntryFor(h C.uint64_t) (*ctxEntry, error) {
	obj, err := registry.Get(uint64(h), handles.TypeContext)
	if err != nil {
		return nil, err
	}
	return obj.(*ctxEntry), nil
}

// helm_context_new creates a cancellation context for long-running actions.
// *out receives a handle; free with helm_context_free (freeing also cancels).
//
//export helm_context_new
func helm_context_new(out *C.uint64_t, errOut **C.char) (code C.int32_t) {
	clearErrorOut(errOut)
	defer recoverToCode(&code, errOut)
	if out == nil {
		return failure(errOut, cerrors.New(cerrors.CodeInvalidArg, "out must not be NULL"))
	}
	ctx, cancel := context.WithCancel(context.Background())
	*out = C.uint64_t(registry.Put(handles.TypeContext, &ctxEntry{ctx: ctx, cancel: cancel}))
	return C.int32_t(cerrors.CodeOK)
}

// helm_context_cancel cancels the context; a blocked helm_install/helm_upgrade
// using it returns HELM_ERR_CANCELLED. Safe to call from any thread, and
// repeatedly.
//
//export helm_context_cancel
func helm_context_cancel(h C.uint64_t, errOut **C.char) C.int32_t {
	return statusResult(errOut, func() error {
		entry, err := ctxEntryFor(h)
		if err != nil {
			return err
		}
		entry.cancel()
		return nil
	})
}

// helm_context_free cancels and releases a context handle.
//
//export helm_context_free
func helm_context_free(h C.uint64_t, errOut **C.char) C.int32_t {
	return freeTyped(h, handles.TypeContext, errOut, func(obj any) {
		obj.(*ctxEntry).cancel()
	})
}

// ctxOrBackground resolves an optional context handle (0 = background).
func ctxOrBackground(h C.uint64_t) (context.Context, error) {
	if h == 0 {
		return context.Background(), nil
	}
	entry, err := ctxEntryFor(h)
	if err != nil {
		return nil, err
	}
	return entry.ctx, nil
}
