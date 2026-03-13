package mcp

import (
	"context"
	"io"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
)

// ContextOption configures the MCP context setup.
type ContextOption func(*contextConfig)

type contextConfig struct {
	config       *config.Config
	logger       *logr.Logger
	authRegistry *auth.Registry
	settings     *settings.Run
	ioStreams    *terminal.IOStreams
}

// WithConfig injects application configuration into the MCP context.
func WithConfig(cfg *config.Config) ContextOption {
	return func(c *contextConfig) {
		c.config = cfg
	}
}

// WithLogger injects a logger into the MCP context.
// If not set, a discard logger is used.
func WithLogger(lgr logr.Logger) ContextOption {
	return func(c *contextConfig) {
		c.logger = &lgr
	}
}

// WithAuthRegistry injects an auth handler registry into the MCP context.
// If not set, an empty registry is created.
func WithAuthRegistry(reg *auth.Registry) ContextOption {
	return func(c *contextConfig) {
		c.authRegistry = reg
	}
}

// WithSettings injects runtime settings into the MCP context.
// If not set, quiet/no-color defaults are used.
func WithSettings(s *settings.Run) ContextOption {
	return func(c *contextConfig) {
		c.settings = s
	}
}

// WithIOStreams injects custom IO streams into the MCP context.
// If not set, discard writers are used (MCP output goes through JSON-RPC).
func WithIOStreams(ios *terminal.IOStreams) ContextOption {
	return func(c *contextConfig) {
		c.ioStreams = ios
	}
}

// NewContext creates a context.Context pre-populated with all values that
// scafctl packages pull from context. This ensures MCP tool handlers do not
// need to figure out which context values each package expects.
//
// Injected values:
//   - logger (logr.Logger) - defaults to discard
//   - config (*config.Config) - optional
//   - auth registry (*auth.Registry) - defaults to empty
//   - writer (*writer.Writer) - defaults to quiet/no-op
//   - settings (*settings.Run) - defaults to quiet mode
func NewContext(opts ...ContextOption) context.Context {
	cfg := &contextConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	ctx := context.Background()

	// Logger
	if cfg.logger != nil {
		ctx = logger.WithLogger(ctx, cfg.logger)
	} else {
		discardLogger := logr.Discard()
		ctx = logger.WithLogger(ctx, &discardLogger)
	}

	// Config
	if cfg.config != nil {
		ctx = config.WithConfig(ctx, cfg.config)
	}

	// Auth registry
	authReg := cfg.authRegistry
	if authReg == nil {
		authReg = auth.NewRegistry()
	}
	ctx = auth.WithRegistry(ctx, authReg)

	// Settings
	s := cfg.settings
	if s == nil {
		s = &settings.Run{
			IsQuiet: true,
			NoColor: true,
		}
	}
	ctx = settings.IntoContext(ctx, s)

	// IO streams
	ios := cfg.ioStreams
	if ios == nil {
		ios = &terminal.IOStreams{
			Out:    io.Discard,
			ErrOut: io.Discard,
		}
	}

	// Writer
	w := writer.New(ios, s)
	ctx = writer.WithWriter(ctx, w)

	return ctx
}

// contextWithCwd returns the server context with a working directory override.
// If cwd is empty, the original context is returned unchanged. If cwd is
// non-empty, it is resolved to an absolute path and validated as an existing
// directory before being injected into the context.
func (s *Server) contextWithCwd(cwd string) (context.Context, error) {
	if cwd == "" {
		return s.ctx, nil
	}
	absCwd, err := provider.ValidateDirectory(cwd)
	if err != nil {
		return nil, err
	}
	return provider.WithWorkingDirectory(s.ctx, absCwd), nil
}
