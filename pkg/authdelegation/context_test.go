package authdelegation

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWithRegistry_RoundTrip(t *testing.T) {
	t.Parallel()
	reg := NewDelegatorRegistry()
	reg.Register("entra", &mockDelegator{name: "entra"})

	ctx := WithRegistry(context.Background(), reg)
	got := RegistryFromContext(ctx)
	require.NotNil(t, got)
	assert.Same(t, reg, got)
}

func TestRegistryFromContext_EmptyContext(t *testing.T) {
	t.Parallel()
	got := RegistryFromContext(context.Background())
	assert.Nil(t, got)
}
