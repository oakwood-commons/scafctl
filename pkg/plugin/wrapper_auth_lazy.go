// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	authofficial "github.com/oakwood-commons/scafctl/pkg/auth/official"
	"github.com/oakwood-commons/scafctl/pkg/logger"
)

// Compile-time interface checks.
var (
	_ auth.Handler      = (*LazyAuthHandlerWrapper)(nil)
	_ auth.TokenLister  = (*LazyAuthHandlerWrapper)(nil)
	_ auth.TokenPurger  = (*LazyAuthHandlerWrapper)(nil)
	_ auth.FlowDetector = (*LazyAuthHandlerWrapper)(nil)
	_ auth.Configurer   = (*LazyAuthHandlerWrapper)(nil)
)

// lazyInitTimeout is the maximum time allowed for plugin initialization
// when triggered by metadata queries (SupportedFlows, Capabilities).
const lazyInitTimeout = 30 * time.Second

// LazyAuthHandlerWrapper implements auth.Handler by deferring plugin subprocess
// startup until the first method that requires it. Name() and DisplayName() are
// served from static metadata, so registry.List() and registry.Has() work
// without any I/O.
type LazyAuthHandlerWrapper struct {
	name        string
	displayName string

	// initFn starts the plugin subprocess and returns the real wrapper.
	// It is called at most once via sync.Once.
	initFn func(ctx context.Context) (*AuthHandlerWrapper, error)

	once    sync.Once
	wrapper atomic.Pointer[AuthHandlerWrapper]
	initErr error

	// baseCtx holds the wired application context captured from the first
	// context-bearing call (Login, ApplyOverrides, etc.). It is used as a
	// fallback by SupportedFlows/Capabilities instead of context.Background()
	// to ensure initialization has access to config, logger, and secret-store.
	baseCtxMu  sync.Mutex
	baseCtx    context.Context
	baseCtxSet bool
}

// LazyAuthHandlerConfig holds parameters needed to construct a lazy wrapper.
type LazyAuthHandlerConfig struct {
	// Name is the handler name (e.g., "entra").
	Name string

	// BinPath is the absolute path to the cached plugin binary.
	BinPath string

	// PluginCfg is passed to ConfigureAuthHandler after startup.
	PluginCfg *ProviderConfig

	// ClientOpts are options for the plugin client (e.g., host deps).
	ClientOpts []ClientOption

	// OfficialRegistry provides trusted domains for the handler.
	OfficialRegistry *authofficial.Registry
}

// NewLazyAuthHandlerWrapper creates a lazy auth handler that defers plugin
// startup until a method requiring the plugin is called.
func NewLazyAuthHandlerWrapper(cfg LazyAuthHandlerConfig) *LazyAuthHandlerWrapper {
	return &LazyAuthHandlerWrapper{
		name:        cfg.Name,
		displayName: cfg.Name, // best we can do without starting the plugin
		initFn: func(ctx context.Context) (*AuthHandlerWrapper, error) {
			lgr := logger.FromContext(ctx)
			lgr.V(1).Info("lazy-starting auth handler plugin", "handler", cfg.Name, "path", cfg.BinPath)

			client, err := NewAuthHandlerClient(cfg.BinPath, cfg.ClientOpts...)
			if err != nil {
				return nil, fmt.Errorf("starting auth handler plugin %s: %w", cfg.Name, err)
			}

			handlers, err := client.GetAuthHandlers(ctx)
			if err != nil {
				client.Kill()
				return nil, fmt.Errorf("getting handlers from plugin %s: %w", cfg.Name, err)
			}

			// Find the handler matching our name.
			for _, info := range handlers {
				if info.Name == cfg.Name {
					wrapper := NewAuthHandlerWrapper(client, info)

					// Set trusted domains from official registry + config.
					if cfg.OfficialRegistry != nil {
						if official, ok := cfg.OfficialRegistry.Get(cfg.Name); ok {
							wrapper.SetTrustedDomains(official.TrustedVerificationDomains)
						}
					}

					// Configure the handler.
					if cfg.PluginCfg != nil {
						hostCfg := *cfg.PluginCfg
						hostCfg.HostServiceID = client.HostServiceID()
						injectAuthHandlerSettings(ctx, info.Name, &hostCfg)
						if cfgErr := client.ConfigureAuthHandler(ctx, info.Name, hostCfg); cfgErr != nil {
							lgr.Info("failed to configure lazy auth handler",
								"handler", info.Name, "error", cfgErr)
						}
						wrapper.hostCfg = hostConfig{
							Quiet:      cfg.PluginCfg.Quiet,
							NoColor:    cfg.PluginCfg.NoColor,
							BinaryName: cfg.PluginCfg.BinaryName,
						}
					}

					return wrapper, nil
				}
			}

			client.Kill()
			return nil, fmt.Errorf("plugin %s does not expose handler %q", cfg.BinPath, cfg.Name)
		},
	}
}

// init starts the plugin if not already started. The provided context is used
// for the initialization call only.
func (l *LazyAuthHandlerWrapper) init(ctx context.Context) (*AuthHandlerWrapper, error) {
	l.storeBaseCtx(ctx)
	l.once.Do(func() {
		w, err := l.initFn(ctx)
		l.initErr = err
		if w != nil {
			l.wrapper.Store(w)
		}
	})
	return l.wrapper.Load(), l.initErr
}

// storeBaseCtx captures the first non-background context for use by metadata
// methods that lack a context parameter.
func (l *LazyAuthHandlerWrapper) storeBaseCtx(ctx context.Context) {
	if ctx != nil && ctx != context.Background() && ctx != context.TODO() {
		l.baseCtxMu.Lock()
		defer l.baseCtxMu.Unlock()
		if !l.baseCtxSet {
			l.baseCtx = ctx
			l.baseCtxSet = true
		}
	}
}

// getBaseCtx returns the stored base context, falling back to context.Background().
func (l *LazyAuthHandlerWrapper) getBaseCtx() context.Context {
	l.baseCtxMu.Lock()
	defer l.baseCtxMu.Unlock()
	if l.baseCtx != nil {
		return l.baseCtx
	}
	return context.Background()
}

// Client returns the underlying AuthHandlerClient, or nil if not yet initialized.
func (l *LazyAuthHandlerWrapper) Client() *AuthHandlerClient {
	if w := l.wrapper.Load(); w != nil {
		return w.Client()
	}
	return nil
}

// SetContext stores a wired application context for later use by metadata
// methods (SupportedFlows, Capabilities) that don't receive a context parameter.
// Only the first call takes effect.
func (l *LazyAuthHandlerWrapper) SetContext(ctx context.Context) {
	l.storeBaseCtx(ctx)
}

// IsInitialized reports whether the plugin subprocess has been started.
func (l *LazyAuthHandlerWrapper) IsInitialized() bool {
	return l.wrapper.Load() != nil
}

// --- auth.Handler (static, no plugin needed) ---

// Name implements auth.Handler.
func (l *LazyAuthHandlerWrapper) Name() string {
	return l.name
}

// DisplayName implements auth.Handler.
func (l *LazyAuthHandlerWrapper) DisplayName() string {
	// After initialization, use the real display name from the plugin.
	if w := l.wrapper.Load(); w != nil {
		return w.DisplayName()
	}
	return l.displayName
}

// SupportedFlows implements auth.Handler.
// This triggers plugin initialization because flow information is needed
// for command validation (e.g., determining which login flags to show).
func (l *LazyAuthHandlerWrapper) SupportedFlows() []auth.Flow {
	if w := l.wrapper.Load(); w != nil {
		return w.SupportedFlows()
	}
	ctx, cancel := context.WithTimeout(l.getBaseCtx(), lazyInitTimeout)
	defer cancel()
	w, err := l.init(ctx)
	if err != nil {
		return nil
	}
	return w.SupportedFlows()
}

// Capabilities implements auth.Handler.
// This triggers plugin initialization because capabilities determine which
// flags and validation rules apply (e.g., CapScopesOnTokenRequest controls
// whether --scope is accepted by 'auth token').
func (l *LazyAuthHandlerWrapper) Capabilities() []auth.Capability {
	if w := l.wrapper.Load(); w != nil {
		return w.Capabilities()
	}
	ctx, cancel := context.WithTimeout(l.getBaseCtx(), lazyInitTimeout)
	defer cancel()
	w, err := l.init(ctx)
	if err != nil {
		return nil
	}
	return w.Capabilities()
}

// --- auth.Configurer ---

// ApplyOverrides implements auth.Configurer.
// Triggers plugin initialization if needed, then delegates to the wrapper.
func (l *LazyAuthHandlerWrapper) ApplyOverrides(ctx context.Context, overrides map[string]string) error {
	if len(overrides) == 0 {
		return nil
	}
	w, err := l.init(ctx)
	if err != nil {
		return err
	}
	return w.ApplyOverrides(ctx, overrides)
}

// --- auth.Handler (requires plugin) ---

// Login implements auth.Handler.
func (l *LazyAuthHandlerWrapper) Login(ctx context.Context, opts auth.LoginOptions) (*auth.Result, error) {
	w, err := l.init(ctx)
	if err != nil {
		return nil, err
	}
	return w.Login(ctx, opts)
}

// Logout implements auth.Handler.
func (l *LazyAuthHandlerWrapper) Logout(ctx context.Context) error {
	w, err := l.init(ctx)
	if err != nil {
		return err
	}
	return w.Logout(ctx)
}

// Status implements auth.Handler.
func (l *LazyAuthHandlerWrapper) Status(ctx context.Context) (*auth.Status, error) {
	w, err := l.init(ctx)
	if err != nil {
		return nil, err
	}
	return w.Status(ctx)
}

// GetToken implements auth.Handler.
func (l *LazyAuthHandlerWrapper) GetToken(ctx context.Context, opts auth.TokenOptions) (*auth.Token, error) {
	w, err := l.init(ctx)
	if err != nil {
		return nil, err
	}
	return w.GetToken(ctx, opts)
}

// InjectAuth implements auth.Handler.
func (l *LazyAuthHandlerWrapper) InjectAuth(ctx context.Context, req *http.Request, opts auth.TokenOptions) error {
	w, err := l.init(ctx)
	if err != nil {
		return err
	}
	return w.InjectAuth(ctx, req, opts)
}

// --- auth.TokenLister ---

// ListCachedTokens implements auth.TokenLister.
func (l *LazyAuthHandlerWrapper) ListCachedTokens(ctx context.Context) ([]*auth.CachedTokenInfo, error) {
	w, err := l.init(ctx)
	if err != nil {
		return nil, err
	}
	return w.ListCachedTokens(ctx)
}

// --- auth.TokenPurger ---

// PurgeExpiredTokens implements auth.TokenPurger.
func (l *LazyAuthHandlerWrapper) PurgeExpiredTokens(ctx context.Context) (int, error) {
	w, err := l.init(ctx)
	if err != nil {
		return 0, err
	}
	return w.PurgeExpiredTokens(ctx)
}

// --- auth.FlowDetector ---

// DetectAvailableFlows implements auth.FlowDetector.
func (l *LazyAuthHandlerWrapper) DetectAvailableFlows(ctx context.Context) ([]auth.FlowAvailability, error) {
	w, err := l.init(ctx)
	if err != nil {
		return nil, err
	}
	return w.DetectAvailableFlows(ctx)
}
