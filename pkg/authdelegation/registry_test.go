package authdelegation

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockDelegator implements TokenDelegator for testing.
type mockDelegator struct {
	name string
}

func (m *mockDelegator) DelegateToken(_ context.Context, _ string) (TokenResult, error) {
	return TokenResult{AccessToken: m.name + "-token", ExpiresIn: 3600}, nil
}

func TestNewDelegatorRegistry(t *testing.T) {
	t.Parallel()
	reg := NewDelegatorRegistry()
	require.NotNil(t, reg)
	assert.False(t, reg.Has("anything"))
	assert.Empty(t, reg.Names())
}

func TestDelegatorRegistry_RegisterAndGet(t *testing.T) {
	t.Parallel()
	reg := NewDelegatorRegistry()
	mock := &mockDelegator{name: "entra"}
	reg.Register("entra", mock)

	got, ok := reg.Get("entra")
	require.True(t, ok)
	assert.Same(t, mock, got)
}

func TestDelegatorRegistry_GetUnknown(t *testing.T) {
	t.Parallel()
	reg := NewDelegatorRegistry()
	got, ok := reg.Get("nonexistent")
	assert.False(t, ok)
	assert.Nil(t, got)
}

func TestDelegatorRegistry_Has(t *testing.T) {
	t.Parallel()
	reg := NewDelegatorRegistry()
	reg.Register("gcp", &mockDelegator{name: "gcp"})

	assert.True(t, reg.Has("gcp"))
	assert.False(t, reg.Has("entra"))
}

func TestDelegatorRegistry_Names(t *testing.T) {
	t.Parallel()
	reg := NewDelegatorRegistry()
	reg.Register("gcp", &mockDelegator{name: "gcp"})
	reg.Register("entra", &mockDelegator{name: "entra"})
	reg.Register("github", &mockDelegator{name: "github"})

	names := reg.Names()
	assert.Equal(t, []string{"entra", "gcp", "github"}, names)
}

func TestDelegatorRegistry_RegisterOverwrites(t *testing.T) {
	t.Parallel()
	reg := NewDelegatorRegistry()
	first := &mockDelegator{name: "first"}
	second := &mockDelegator{name: "second"}

	reg.Register("entra", first)
	reg.Register("entra", second)

	got, ok := reg.Get("entra")
	require.True(t, ok)
	assert.Same(t, second, got)
}

func TestDelegatorRegistry_RegisterEmptyNamePanics(t *testing.T) {
	t.Parallel()
	reg := NewDelegatorRegistry()
	assert.Panics(t, func() {
		reg.Register("", &mockDelegator{name: "x"})
	})
}

func TestDelegatorRegistry_ConcurrentGet(t *testing.T) {
	t.Parallel()
	reg := NewDelegatorRegistry()
	reg.Register("entra", &mockDelegator{name: "entra"})
	reg.Register("gcp", &mockDelegator{name: "gcp"})

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = reg.Get("entra")
			_ = reg.Has("gcp")
			_ = reg.Names()
		}()
	}
	wg.Wait()
}
