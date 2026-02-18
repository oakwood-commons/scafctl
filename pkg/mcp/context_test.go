package mcp

import (
	"io"
	"testing"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewContext(t *testing.T) {
	t.Run("defaults produce valid context", func(t *testing.T) {
		ctx := NewContext()
		lgr := logger.FromContext(ctx)
		require.NotNil(t, lgr)
		w := writer.FromContext(ctx)
		require.NotNil(t, w)
		authReg := auth.RegistryFromContext(ctx)
		require.NotNil(t, authReg)
		s, ok := settings.FromContext(ctx)
		require.True(t, ok)
		assert.True(t, s.IsQuiet)
		assert.True(t, s.NoColor)
	})

	t.Run("with config", func(t *testing.T) {
		cfg := &config.Config{Version: 1}
		ctx := NewContext(WithConfig(cfg))
		got := config.FromContext(ctx)
		require.NotNil(t, got)
		assert.Equal(t, 1, got.Version)
	})

	t.Run("with logger", func(t *testing.T) {
		lgr := logr.Discard()
		ctx := NewContext(WithLogger(lgr))
		got := logger.FromContext(ctx)
		require.NotNil(t, got)
	})

	t.Run("with auth registry", func(t *testing.T) {
		reg := auth.NewRegistry()
		ctx := NewContext(WithAuthRegistry(reg))
		got := auth.RegistryFromContext(ctx)
		assert.Equal(t, reg, got)
	})

	t.Run("with settings", func(t *testing.T) {
		s := &settings.Run{IsQuiet: false, NoColor: false}
		ctx := NewContext(WithSettings(s))
		got, ok := settings.FromContext(ctx)
		require.True(t, ok)
		assert.False(t, got.IsQuiet)
	})

	t.Run("config is nil by default", func(t *testing.T) {
		ctx := NewContext()
		got := config.FromContext(ctx)
		assert.Nil(t, got)
	})

	t.Run("with io streams", func(t *testing.T) {
		ios := &terminal.IOStreams{
			Out:    io.Discard,
			ErrOut: io.Discard,
		}
		ctx := NewContext(WithIOStreams(ios))
		w := writer.FromContext(ctx)
		require.NotNil(t, w)
	})
}
