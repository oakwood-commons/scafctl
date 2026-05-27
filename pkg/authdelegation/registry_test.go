package authdelegation

import (
	"context"
	"sync"
	"testing"

	"github.com/go-logr/logr"

	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockDelegator implements TokenDelegator for testing.
type mockDelegator struct {
	name string
}

func (m *mockDelegator) Name() string {
	return m.name
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

func TestRegisterPassThroughDelegators_Default(t *testing.T) {
	reg := NewDelegatorRegistry()
	lgr := logr.Discard()

	err := registerPassThroughDelegators(&config.APIServerConfig{}, &lgr, reg)

	require.NoError(t, err)
	assert.Equal(t, []string{"passThrough:Github"}, reg.Names())
}

func TestRegisterPassThroughDelegators_ConfiguredHeaders(t *testing.T) {
	reg := NewDelegatorRegistry()
	lgr := logr.Discard()
	cfg := &config.Config{
		APIServer: config.APIServerConfig{
			TokenPassThrough: &config.TokenPassThroughConfig{
				AllowedHeaders: []string{"Github", "Azure-Ad"},
			},
		},
	}

	err := registerPassThroughDelegators(&cfg.APIServer, &lgr, reg)

	require.NoError(t, err)
	assert.Equal(t, []string{"passThrough:Azure-Ad", "passThrough:Github"}, reg.Names())
}

func TestRegisterPassThroughDelegators_ExplicitEmpty(t *testing.T) {
	reg := NewDelegatorRegistry()
	lgr := logr.Discard()
	cfg := &config.Config{
		APIServer: config.APIServerConfig{
			TokenPassThrough: &config.TokenPassThroughConfig{
				AllowedHeaders: []string{},
			},
		},
	}

	err := registerPassThroughDelegators(&cfg.APIServer, &lgr, reg)

	require.NoError(t, err)
	assert.Empty(t, reg.Names())
}

func TestBuildDelegationRegistry_NoDelegators(t *testing.T) {
	lgr := logr.Discard()
	cfg := &config.Config{
		APIServer: config.APIServerConfig{
			TokenPassThrough: &config.TokenPassThroughConfig{
				AllowedHeaders: []string{},
			},
		},
	}

	reg, err := BuildDelegationRegistry(context.Background(), &cfg.APIServer, &lgr)

	require.NoError(t, err)
	assert.Nil(t, reg)
}

func TestBuildDelegationRegistry_DefaultPassThrough(t *testing.T) {
	lgr := logr.Discard()

	reg, err := BuildDelegationRegistry(context.Background(), &config.APIServerConfig{}, &lgr)

	require.NoError(t, err)
	require.NotNil(t, reg)
	assert.Equal(t, []string{"passThrough:Github"}, reg.Names())
}
