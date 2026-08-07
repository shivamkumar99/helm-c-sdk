// Package handles implements the opaque-handle registry that stands between C
// callers and Go objects. cgo forbids passing Go pointers to C (the GC may
// move or collect them), so every stateful Go object crosses the boundary as
// a uint64 id into this registry instead.
package handles

import (
	"fmt"
	"sync"

	"github.com/shivamkumar99/helm-c-sdk/internal/cerrors"
)

// Type tags every registry entry so a handle can only be used as the type it
// was created as; using a chart handle where a config is expected is a
// defined error, never a wrong cast.
type Type uint32

const (
	TypeInvalid Type = iota
	TypeChart
	TypeConfig
	TypeRegistryClient
	TypeContext
)

var typeNames = map[Type]string{
	TypeInvalid:        "invalid",
	TypeChart:          "chart",
	TypeConfig:         "config",
	TypeRegistryClient: "registry-client",
	TypeContext:        "context",
}

func (t Type) String() string {
	if name, ok := typeNames[t]; ok {
		return name
	}
	return fmt.Sprintf("type(%d)", uint32(t))
}

type entry struct {
	typ Type
	obj any
}

// Registry is a thread-safe id → object store. Ids start at 1 and are never
// reused, so a stale handle can only miss — it can never alias a newer object.
type Registry struct {
	mu      sync.RWMutex
	nextID  uint64
	entries map[uint64]entry
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{entries: make(map[uint64]entry)}
}

// Put stores obj under a fresh id and returns it. The zero id is never issued
// so callers can treat 0 as "no handle".
func (r *Registry) Put(t Type, obj any) uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextID++
	r.entries[r.nextID] = entry{typ: t, obj: obj}
	return r.nextID
}

// Get returns the object stored under id, requiring it to be a t.
func (r *Registry) Get(id uint64, t Type) (any, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.entries[id]
	if !ok {
		return nil, errInvalidHandle(id)
	}
	if e.typ != t {
		return nil, errWrongType(id, e.typ, t)
	}
	return e.obj, nil
}

// Free removes id regardless of its type. Freeing an unknown or already-freed
// id is a defined error, not a crash — hosts with GC finalizers will
// double-free, and must be able to do so safely.
func (r *Registry) Free(id uint64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.entries[id]; !ok {
		return errInvalidHandle(id)
	}
	delete(r.entries, id)
	return nil
}

// FreeTyped removes id only if it holds a t; it backs the per-type
// helm_*_free functions so a caller cannot free through the wrong type.
func (r *Registry) FreeTyped(id uint64, t Type) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.entries[id]
	if !ok {
		return errInvalidHandle(id)
	}
	if e.typ != t {
		return errWrongType(id, e.typ, t)
	}
	delete(r.entries, id)
	return nil
}

// Count reports live entries — the leak probe behind helm_open_handles_count.
func (r *Registry) Count() int64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return int64(len(r.entries))
}

func errInvalidHandle(id uint64) error {
	return cerrors.New(cerrors.CodeInvalidHandle,
		fmt.Sprintf("invalid or already-freed handle %d", id))
}

func errWrongType(id uint64, got, want Type) error {
	return cerrors.New(cerrors.CodeWrongHandleType,
		fmt.Sprintf("handle %d holds a %s, want a %s", id, got, want))
}
