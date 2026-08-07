package handles

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shivamkumar99/helm-c-sdk/internal/cerrors"
)

func TestPutGetFreeLifecycle(t *testing.T) {
	r := NewRegistry()
	obj := &struct{ v int }{v: 42}

	id := r.Put(TypeChart, obj)
	require.NotZero(t, id, "the zero id must never be issued")
	assert.EqualValues(t, 1, r.Count())

	got, err := r.Get(id, TypeChart)
	require.NoError(t, err)
	assert.Same(t, obj, got)

	require.NoError(t, r.Free(id))
	assert.EqualValues(t, 0, r.Count())
}

func TestGetWrongType(t *testing.T) {
	r := NewRegistry()
	id := r.Put(TypeChart, "chart")

	_, err := r.Get(id, TypeConfig)
	assert.Equal(t, cerrors.CodeWrongHandleType, cerrors.FromError(err))
}

func TestGetUnknownHandle(t *testing.T) {
	r := NewRegistry()
	_, err := r.Get(999, TypeChart)
	assert.Equal(t, cerrors.CodeInvalidHandle, cerrors.FromError(err))
}

func TestDoubleFreeIsDefinedError(t *testing.T) {
	r := NewRegistry()
	id := r.Put(TypeContext, "ctx")

	require.NoError(t, r.Free(id))
	err := r.Free(id)
	assert.Equal(t, cerrors.CodeInvalidHandle, cerrors.FromError(err),
		"double-free must be a defined error, never a crash")
}

func TestFreeTyped(t *testing.T) {
	r := NewRegistry()
	id := r.Put(TypeChart, "chart")

	err := r.FreeTyped(id, TypeConfig)
	assert.Equal(t, cerrors.CodeWrongHandleType, cerrors.FromError(err))
	assert.EqualValues(t, 1, r.Count(), "a wrong-type free must not remove the entry")

	require.NoError(t, r.FreeTyped(id, TypeChart))
	assert.EqualValues(t, 0, r.Count())
}

func TestIdsAreNeverReused(t *testing.T) {
	r := NewRegistry()
	first := r.Put(TypeChart, "a")
	require.NoError(t, r.Free(first))

	second := r.Put(TypeChart, "b")
	assert.NotEqual(t, first, second)

	_, err := r.Get(first, TypeChart)
	assert.Equal(t, cerrors.CodeInvalidHandle, cerrors.FromError(err),
		"a stale handle must miss, never alias a newer object")
}

func TestConcurrentAccess(t *testing.T) {
	const workers = 32
	const perWorker = 100

	r := NewRegistry()
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				id := r.Put(TypeChart, i)
				_, err := r.Get(id, TypeChart)
				assert.NoError(t, err)
				assert.NoError(t, r.Free(id))
			}
		}()
	}
	wg.Wait()
	assert.EqualValues(t, 0, r.Count())
}
