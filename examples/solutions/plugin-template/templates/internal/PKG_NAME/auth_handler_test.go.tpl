package <% .pkg_name %>

import (
	"context"
	"testing"

	"github.com/oakwood-commons/scafctl-plugin-sdk/auth"
	sdkplugin "github.com/oakwood-commons/scafctl-plugin-sdk/plugin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetAuthHandlers(t *testing.T) {
	p := &Plugin{}
	handlers, err := p.GetAuthHandlers(context.Background())
	require.NoError(t, err)
	require.Len(t, handlers, 1)
	assert.Equal(t, HandlerName, handlers[0].Name)
	assert.NotEmpty(t, handlers[0].DisplayName)
	assert.NotEmpty(t, handlers[0].Flows)
}

func TestConfigureAuthHandler(t *testing.T) {
	p := &Plugin{}
	err := p.ConfigureAuthHandler(context.Background(), HandlerName, sdkplugin.ProviderConfig{
		BinaryName: "scafctl",
	})
	require.NoError(t, err)
	assert.Equal(t, "scafctl", p.cfg.BinaryName)
}

func TestLogin(t *testing.T) {
	p := &Plugin{}

	t.Run("known handler", func(t *testing.T) {
		resp, err := p.Login(context.Background(), HandlerName, sdkplugin.LoginRequest{}, nil)
		require.NoError(t, err)
		assert.NotNil(t, resp.Claims)
		assert.NotEmpty(t, resp.Claims.Subject)
		assert.False(t, resp.ExpiresAt.IsZero())
	})

	t.Run("unknown handler", func(t *testing.T) {
		_, err := p.Login(context.Background(), "unknown", sdkplugin.LoginRequest{}, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unknown handler")
	})
}

func TestLogout(t *testing.T) {
	p := &Plugin{}

	t.Run("known handler", func(t *testing.T) {
		err := p.Logout(context.Background(), HandlerName)
		require.NoError(t, err)
	})

	t.Run("unknown handler", func(t *testing.T) {
		err := p.Logout(context.Background(), "unknown")
		assert.Error(t, err)
	})
}

func TestGetStatus(t *testing.T) {
	p := &Plugin{}

	t.Run("known handler", func(t *testing.T) {
		status, err := p.GetStatus(context.Background(), HandlerName)
		require.NoError(t, err)
		assert.NotNil(t, status)
	})

	t.Run("unknown handler", func(t *testing.T) {
		_, err := p.GetStatus(context.Background(), "unknown")
		assert.Error(t, err)
	})
}

func TestGetToken(t *testing.T) {
	p := &Plugin{}

	t.Run("not authenticated", func(t *testing.T) {
		_, err := p.GetToken(context.Background(), HandlerName, sdkplugin.TokenRequest{})
		assert.ErrorIs(t, err, auth.ErrNotAuthenticated)
	})

	t.Run("unknown handler", func(t *testing.T) {
		_, err := p.GetToken(context.Background(), "unknown", sdkplugin.TokenRequest{})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unknown handler")
	})
}

func TestListCachedTokens(t *testing.T) {
	p := &Plugin{}
	tokens, err := p.ListCachedTokens(context.Background(), HandlerName)
	require.NoError(t, err)
	assert.Nil(t, tokens)
}

func TestPurgeExpiredTokens(t *testing.T) {
	p := &Plugin{}
	count, err := p.PurgeExpiredTokens(context.Background(), HandlerName)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func BenchmarkGetToken(b *testing.B) {
	p := &Plugin{}
	ctx := context.Background()
	req := sdkplugin.TokenRequest{}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		_, _ = p.GetToken(ctx, HandlerName, req)
	}
}
