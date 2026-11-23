// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	authofficial "github.com/oakwood-commons/scafctl/pkg/auth/official"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/metrics"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// authTracer is the OpenTelemetry tracer for auth handler plugin operations.
var authTracer = telemetry.Tracer(telemetry.TracerPlugin + "/auth")

// Compile-time interface checks.
var (
	_ auth.Handler      = (*AuthHandlerWrapper)(nil)
	_ auth.TokenLister  = (*AuthHandlerWrapper)(nil)
	_ auth.TokenPurger  = (*AuthHandlerWrapper)(nil)
	_ auth.FlowDetector = (*AuthHandlerWrapper)(nil)
	_ auth.Configurer   = (*AuthHandlerWrapper)(nil)
)

// defaultPreflightFlowDetectTimeout bounds the advisory DetectAvailableFlows
// preflight RPC when Login is called with an empty Flow and no shorter
// caller or LoginOptions.Timeout deadline is active.
const defaultPreflightFlowDetectTimeout = 5 * time.Second

// ErrUntrustedAuthorizationURL is returned by a paste-back prompt whose
// authorization URL falls outside the handler's trusted domains.
var ErrUntrustedAuthorizationURL = errors.New("authorization URL is not in the handler's trusted domains")

// trustedPasteBack wraps prompt so the authorization URL is checked against
// the handler's trusted domains before any host-authored text displays it.
// A nil prompt stays nil (non-interactive session).
func trustedPasteBack(prompt auth.PasteBackFunc, handlerName string, trusted []string, requireTrust bool) auth.PasteBackFunc {
	if prompt == nil {
		return nil
	}
	return func(ctx context.Context, authorizationURL, redirectURI string) (string, error) {
		if requireTrust && len(trusted) == 0 {
			return "", fmt.Errorf("%w: handler %q has no configured trusted domains; set auth.handlers.%s.trustedVerificationDomains",
				ErrUntrustedAuthorizationURL, handlerName, handlerName)
		}
		if len(trusted) > 0 && !hostInTrustedDomains(authorizationURL, trusted) {
			return "", ErrUntrustedAuthorizationURL
		}
		return prompt(ctx, authorizationURL, redirectURI)
	}
}

// hostInTrustedDomains reports whether rawURL's host equals, or is a
// subdomain of, an entry in trusted.
func hostInTrustedDomains(rawURL string, trusted []string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if host == "" {
		return false
	}
	for _, domain := range trusted {
		d := strings.ToLower(strings.TrimSuffix(domain, "."))
		if host == d || strings.HasSuffix(host, "."+d) {
			return true
		}
	}
	return false
}

// AuthHandlerWrapper wraps a plugin auth handler to implement the auth.Handler
// (and optionally auth.TokenLister / auth.TokenPurger) interfaces.
type AuthHandlerWrapper struct {
	client         *AuthHandlerClient
	handlerName    string
	info           AuthHandlerInfo
	trustedDomains []string
	// requireTrustedDomains, when true, makes device-code verification fail
	// closed if no trusted domains are configured (instead of allowing any
	// HTTPS URL). Set for non-official handlers, which do not carry hardcoded
	// trust domains.
	requireTrustedDomains bool
	startupLatency        time.Duration
	hostCfg               hostConfig // host-level config preserved across reconfigurations
	mu                    sync.RWMutex

	// configMu guards overrides, serverMode, configured*, and the active-call
	// gate (inFlight, inFlightProfile, activeCalls, reconfiguring, writersWaiting,
	// waitCh) so stateful plugin configuration stays stable across each RPC while
	// calls sharing the same profile and settings execute concurrently.
	configMu              sync.Mutex
	overrides             map[string]map[string]string
	serverMode            bool
	configured            bool
	configuredProfile     string
	configuredSettingsRaw []byte
	inFlight              int
	inFlightProfile       string
	activeCalls           map[string]struct{}
	reconfiguring         bool
	writersWaiting        int
	waitCh                chan struct{}
}

// hostConfig stores host-level ProviderConfig fields so ApplyOverrides and
// per-profile reconfigurations can reconstruct a full config without zeroing them.
type hostConfig struct {
	Quiet             bool
	NoColor           bool
	BinaryName        string
	Profile           string
	GlobalProfile     string
	AppConfig         *config.Config
	AuthCallbackPages *AuthCallbackPages
	BaseSettings      map[string]json.RawMessage
}

// NewAuthHandlerWrapper creates a new wrapper for a plugin auth handler.
func NewAuthHandlerWrapper(client *AuthHandlerClient, info AuthHandlerInfo) *AuthHandlerWrapper {
	w := &AuthHandlerWrapper{
		client:      client,
		handlerName: info.Name,
		info:        info,
	}
	if client != nil {
		client.registerWrapper(w)
	}
	return w
}

// SetTrustedDomains sets the trusted verification URI domains for device code
// URL validation. When non-empty, device code prompts from this handler are
// validated against these domains (exact match or subdomain).
func (w *AuthHandlerWrapper) SetTrustedDomains(domains []string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if domains == nil {
		w.trustedDomains = nil
		return
	}
	w.trustedDomains = make([]string, len(domains))
	copy(w.trustedDomains, domains)
}

// SetRequireTrustedDomains controls whether device-code verification fails
// closed when no trusted domains are configured. Set true for non-official
// handlers so a catalog-resolved third-party handler cannot display arbitrary
// verification URLs without explicitly configured
// auth.handlers.<name>.trustedVerificationDomains.
func (w *AuthHandlerWrapper) SetRequireTrustedDomains(require bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.requireTrustedDomains = require
}

// Name implements auth.Handler.
func (w *AuthHandlerWrapper) Name() string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.info.Name
}

// DisplayName implements auth.Handler.
func (w *AuthHandlerWrapper) DisplayName() string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.info.DisplayName
}

// SupportedFlows implements auth.Handler.
func (w *AuthHandlerWrapper) SupportedFlows() []auth.Flow {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.info.Flows
}

// Capabilities implements auth.Handler.
func (w *AuthHandlerWrapper) Capabilities() []auth.Capability {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.info.Capabilities
}

// Login implements auth.Handler.
func (w *AuthHandlerWrapper) Login(ctx context.Context, opts auth.LoginOptions) (*auth.Result, error) {
	ctx, span := authTracer.Start(ctx, "auth.plugin.login",
		trace.WithAttributes(attribute.String("auth.handler", w.handlerName)))
	defer span.End()

	lgr := logger.FromContext(ctx)
	lgr.V(1).Info("login via plugin auth handler", "handler", w.handlerName)

	effectiveBrowser, err := auth.ResolveBrowserPreference(ctx, opts.Browser)
	if err != nil {
		return nil, fmt.Errorf("plugin auth handler %s login: invalid browser preference: %w", w.handlerName, err)
	}

	if opts.Timeout > 0 {
		var cancelTimeout context.CancelFunc
		ctx, cancelTimeout = context.WithTimeout(ctx, opts.Timeout)
		defer cancelTimeout()
	}

	ctx, perCallSettings, release, err := w.beginCall(ctx)
	if err != nil {
		return nil, err
	}
	defer release()

	willOpen, err := w.willOpenBrowserOnLogin(ctx, opts.Flow, effectiveBrowser)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, fmt.Errorf("plugin auth handler %s login: %w: %w", w.handlerName, auth.ErrTimeout, err)
		}
		return nil, fmt.Errorf("plugin auth handler %s login: %w", w.handlerName, err)
	}
	if willOpen {
		auth.WarnIfBrowserUnavailable(ctx, effectiveBrowser)
	}

	req := LoginRequest{
		TenantID:     opts.TenantID,
		Hostname:     opts.Hostname,
		Scopes:       opts.Scopes,
		Flow:         opts.Flow,
		Timeout:      opts.Timeout,
		CallbackPort: opts.CallbackPort,
		Browser:      effectiveBrowser,
		Settings:     cloneSettingsMap(perCallSettings),
	}

	// Bridge the LoginOptions.DeviceCodeCallback to the plugin's streaming callback.
	// Includes host-side verification URI validation to prevent phishing via
	// malicious plugins sending fake device code URLs.
	loginCtx, cancelCause := context.WithCancelCause(ctx)
	defer cancelCause(nil)

	// Snapshot trusted domains under lock to avoid races with SetTrustedDomains.
	w.mu.RLock()
	trustedSnapshot := make([]string, len(w.trustedDomains))
	copy(trustedSnapshot, w.trustedDomains)
	requireTrust := w.requireTrustedDomains
	w.mu.RUnlock()

	// Gate PromptAuthResponse callbacks to this Login: the broker window is
	// open exactly while the plugin's Login RPC runs. The prompt function was
	// installed into ctx by the login UI when the session is interactive; a
	// nil prompt yields Unavailable on the RPC (non-interactive session).
	// The authorization URL is held to the same trusted-domain policy as
	// device-code verification URLs before the host displays it.
	if broker := w.client.authPromptBroker(); broker != nil {
		defer broker.Begin(w.handlerName, trustedPasteBack(auth.PasteBackFromContext(ctx), w.handlerName, trustedSnapshot, requireTrust))()
	}

	var deviceCodeCb func(DeviceCodePrompt)
	if opts.DeviceCodeCallback != nil {
		deviceCodeCb = func(prompt DeviceCodePrompt) {
			// Fail closed for non-official handlers with no configured trust:
			// an empty trusted list would otherwise allow any HTTPS URL.
			if requireTrust && len(trustedSnapshot) == 0 {
				err := fmt.Errorf("device code verification blocked: handler %q has no configured trusted domains; set auth.handlers.%s.trustedVerificationDomains",
					w.handlerName, w.handlerName)
				lgr.Error(err, "device code prompt blocked -- terminating login",
					"handler", w.handlerName,
					"uri", prompt.VerificationURI)
				cancelCause(err)
				return
			}
			if err := ValidateVerificationURI(prompt.VerificationURI, trustedSnapshot); err != nil {
				lgr.Error(err, "device code prompt blocked -- terminating login",
					"handler", w.handlerName,
					"uri", prompt.VerificationURI)
				cancelCause(fmt.Errorf("device code URL validation failed for handler %q: %w",
					w.handlerName, err))
				return
			}
			opts.DeviceCodeCallback(prompt.UserCode, prompt.VerificationURI, prompt.Message)
		}
	}

	resp, err := w.client.plugin.Login(loginCtx, w.handlerName, req, deviceCodeCb)

	// Enforce the security check host-side: if the verification URI was
	// rejected, return that error even if the plugin returned success.
	if cause := context.Cause(loginCtx); cause != nil {
		return nil, cause
	}

	if err != nil {
		return nil, fmt.Errorf("plugin auth handler %s login: %w", w.handlerName, err)
	}

	return &auth.Result{
		Claims:    resp.Claims,
		ExpiresAt: resp.ExpiresAt,
	}, nil
}

// Logout implements auth.Handler.
//
// The empty LogoutRequest{} sends no instance Hostname: the auth.Handler
// interface's Logout(ctx) carries no selector, so scafctl's own commands clear
// all of a handler's cached credentials. Per-instance status/logout selection
// (LogoutRequest/StatusRequest.Hostname, backed by CapInstanceHostname) is
// forwarded faithfully at the gRPC boundary but not yet wired into a
// hostname-aware auth.Handler path -- that is tracked separately as part of
// scafctl-plugin-sdk#58 and is intentionally out of scope for the
// --callback-port work (#589).
func (w *AuthHandlerWrapper) Logout(ctx context.Context) error {
	ctx, _, release, err := w.beginCall(ctx)
	if err != nil {
		return err
	}
	defer release()
	return w.client.plugin.Logout(ctx, w.handlerName, LogoutRequest{})
}

// Status implements auth.Handler.
//
// See Logout: StatusRequest{} carries no instance Hostname because the
// auth.Handler interface has no selector; per-instance status is deferred to
// the CapInstanceHostname follow-up (scafctl-plugin-sdk#58).
func (w *AuthHandlerWrapper) Status(ctx context.Context) (*auth.Status, error) {
	ctx, span := authTracer.Start(ctx, "auth.plugin.status",
		trace.WithAttributes(attribute.String("auth.handler", w.handlerName)))
	defer span.End()

	ctx, perCallSettings, release, err := w.beginCall(ctx)
	if err != nil {
		return nil, err
	}
	defer release()

	return w.client.plugin.GetStatus(ctx, w.handlerName, StatusRequest{
		Settings: cloneSettingsMap(perCallSettings),
	})
}

// GetToken implements auth.Handler.
func (w *AuthHandlerWrapper) GetToken(ctx context.Context, opts auth.TokenOptions) (*auth.Token, error) {
	ctx, span := authTracer.Start(ctx, "auth.plugin.get_token",
		trace.WithAttributes(attribute.String("auth.handler", w.handlerName)))
	defer span.End()

	ctx, perCallSettings, release, err := w.beginCall(ctx)
	if err != nil {
		return nil, err
	}
	defer release()

	req := TokenRequest{
		Scope:         opts.Scope,
		MinValidFor:   opts.MinValidFor,
		ForceRefresh:  opts.ForceRefresh,
		Hostname:      opts.Hostname,
		ServerContext: opts.ServerContext,
		Caller:        opts.Caller,
		Assertion:     opts.Assertion,
		Settings:      cloneSettingsMap(perCallSettings),
	}

	resp, err := w.client.plugin.GetToken(ctx, w.handlerName, req)
	if err != nil {
		return nil, fmt.Errorf("plugin auth handler %s get-token: %w", w.handlerName, err)
	}

	return &auth.Token{
		AccessToken: resp.AccessToken,
		TokenType:   resp.TokenType,
		ExpiresAt:   resp.ExpiresAt,
		Scope:       resp.Scope,
		CachedAt:    resp.CachedAt,
		Flow:        resp.Flow,
		SessionID:   resp.SessionID,
	}, nil
}

// InjectAuth implements auth.Handler.
// Since http.Request cannot be serialized over gRPC, this method decomposes into
// GetToken (over gRPC) + local header injection.
func (w *AuthHandlerWrapper) InjectAuth(ctx context.Context, req *http.Request, opts auth.TokenOptions) error {
	token, err := w.GetToken(ctx, opts)
	if err != nil {
		return fmt.Errorf("plugin auth handler %s inject-auth: %w", w.handlerName, err)
	}

	tokenType := token.TokenType
	if tokenType == "" {
		tokenType = "Bearer"
	}
	req.Header.Set("Authorization", tokenType+" "+token.AccessToken)
	return nil
}

// ListCachedTokens implements auth.TokenLister.
func (w *AuthHandlerWrapper) ListCachedTokens(ctx context.Context) ([]*auth.CachedTokenInfo, error) {
	ctx, _, release, err := w.beginCall(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	return w.client.plugin.ListCachedTokens(ctx, w.handlerName)
}

// PurgeExpiredTokens implements auth.TokenPurger.
func (w *AuthHandlerWrapper) PurgeExpiredTokens(ctx context.Context) (int, error) {
	ctx, _, release, err := w.beginCall(ctx)
	if err != nil {
		return 0, err
	}
	defer release()
	return w.client.plugin.PurgeExpiredTokens(ctx, w.handlerName)
}

// DetectAvailableFlows implements auth.FlowDetector.
func (w *AuthHandlerWrapper) DetectAvailableFlows(ctx context.Context) ([]auth.FlowAvailability, error) {
	ctx, _, release, err := w.beginCall(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	return w.client.plugin.DetectAvailableFlows(ctx, w.handlerName)
}

// ApplyOverrides implements auth.Configurer.
// It merges the provided key-value overrides into the handler's current
// plugin configuration and re-sends ConfigureAuthHandler to the plugin process.
// This allows CLI flags (e.g., --client-id, --tenant) to override config-file
// and default settings at runtime before login, and preserves them across
// subsequent per-profile RPCs on this wrapper.
func (w *AuthHandlerWrapper) ApplyOverrides(ctx context.Context, overrides map[string]string) error {
	if len(overrides) == 0 {
		return nil
	}

	// Filter out empty values before marshaling — empty strings mean "not set"
	// and should not be forwarded to the plugin.
	filtered := make(map[string]string, len(overrides))
	for k, v := range overrides {
		if v != "" {
			filtered[k] = v
		}
	}
	if len(filtered) == 0 {
		return nil
	}

	ctx, profile, err := w.resolveEffectiveProfile(ctx)
	if err != nil {
		return err
	}
	hc := w.getHostConfig()

	w.configMu.Lock()
	if err := w.acquireExclusiveReconfigLocked(ctx); err != nil {
		w.configMu.Unlock()
		return err
	}

	if w.serverMode {
		w.reconfiguring = false
		w.broadcastLocked()
		w.configMu.Unlock()
		return nil
	}
	if w.overrides == nil {
		w.overrides = make(map[string]map[string]string)
	}
	prevProfileOverrides, hadPrev := w.overrides[profile]
	w.overrides[profile] = maps.Clone(filtered)

	rawForConfigure, _, _, err := w.resolveHandlerSettingsLocked(ctx, hc, profile)
	if err != nil {
		restoreProfileOverrides(w.overrides, profile, prevProfileOverrides, hadPrev)
		w.reconfiguring = false
		w.broadcastLocked()
		w.configMu.Unlock()
		return err
	}
	if w.client == nil || w.client.plugin == nil {
		w.reconfiguring = false
		w.broadcastLocked()
		w.configMu.Unlock()
		return nil
	}

	cfg := w.buildProviderConfig(hc, profile, rawForConfigure)
	cfgCtx, endCfgLineage := w.startLineageLocked(ctx, profile, true)
	w.configMu.Unlock()

	cfgErr := w.client.ConfigureAuthHandler(cfgCtx, w.handlerName, cfg)

	w.configMu.Lock()
	endCfgLineage()
	w.reconfiguring = false
	if cfgErr != nil {
		restoreProfileOverrides(w.overrides, profile, prevProfileOverrides, hadPrev)
		w.broadcastLocked()
		w.configMu.Unlock()
		return cfgErr
	}
	w.recordConfiguredLocked(profile, rawForConfigure)
	w.broadcastLocked()
	w.configMu.Unlock()
	return nil
}

func restoreProfileOverrides(m map[string]map[string]string, profile string, prev map[string]string, hadPrev bool) {
	if hadPrev {
		m[profile] = prev
		return
	}
	delete(m, profile)
}

// resolveEffectiveProfile normalizes, validates, and resolves the target
// profile on ctx so both host-side settings lookup and gRPC serialization
// enforce the same validated profile name.
func (w *AuthHandlerWrapper) resolveEffectiveProfile(ctx context.Context) (context.Context, string, error) {
	finalize := func(ctx context.Context, raw string) (context.Context, string, error) {
		normalized := auth.NormalizeProfileName(raw)
		if normalized != "" {
			if err := auth.ValidateProfileName(normalized); err != nil {
				return ctx, "", err
			}
		}
		ctx = auth.WithProfile(ctx, normalized)
		ctx = auth.WithProfileResolved(ctx)
		return ctx, normalized, nil
	}

	if p := auth.ProfileFromContext(ctx); p != "" {
		return finalize(ctx, p)
	}
	if auth.IsProfileResolved(ctx) {
		return finalize(ctx, "")
	}

	hc := w.getHostConfig()
	resolveCtx := ctx
	if config.FromContext(resolveCtx) == nil && hc.AppConfig != nil {
		resolveCtx = config.WithConfig(resolveCtx, hc.AppConfig)
	}
	if auth.GlobalProfileFromContext(resolveCtx) == "" && hc.GlobalProfile != "" {
		resolveCtx = auth.WithGlobalProfile(resolveCtx, hc.GlobalProfile)
	}

	if auth.GlobalProfileFromContext(resolveCtx) != "" || config.FromContext(resolveCtx) != nil {
		return finalize(ctx, auth.ResolveActiveProfile(resolveCtx, w.handlerName))
	}
	if hc.Profile != "" {
		return finalize(ctx, hc.Profile)
	}
	return finalize(ctx, "")
}

// beginCall resolves the per-call profile and handler settings, re-sending
// ConfigureAuthHandler when the target profile or effective settings differ
// from what the plugin last received (for stateful/legacy plugins), attaching
// stateless per-RPC settings to ctx and the returned map (for SDK v0.19.0+
// plugins), and holding an active-call slot until the returned release func is
// called so concurrent requests for another profile cannot reconfigure a
// stateful plugin mid-RPC. Calls sharing the same profile and settings run
// concurrently.
func (w *AuthHandlerWrapper) beginCall(ctx context.Context) (context.Context, map[string]json.RawMessage, func(), error) {
	ctx, profile, err := w.resolveEffectiveProfile(ctx)
	if err != nil {
		return ctx, nil, nil, err
	}
	hc := w.getHostConfig()

	var perCallSettings map[string]json.RawMessage
	ctx, release, err := w.acquireCallSlot(ctx, hc, profile, &perCallSettings)
	if err != nil {
		return ctx, nil, nil, err
	}

	if perCallSettings != nil {
		ctx = WithRequestSettings(ctx, cloneSettingsMap(perCallSettings))
	}
	return ctx, perCallSettings, release, nil
}

func (w *AuthHandlerWrapper) acquireCallSlot(
	ctx context.Context,
	hc hostConfig,
	profile string,
	perCallSettings *map[string]json.RawMessage,
) (context.Context, func(), error) {
	lineage := lineageFromContext(ctx)
	w.configMu.Lock()
	for {
		isReentrant := w.hasActiveParentLocked(lineage)

		// In server mode (scafctl serve), ActivateServerMode owns the plugin's
		// confidential credentials; never clobber them with CLI settings, while
		// still counting in-flight RPCs so concurrent or nested ActivateServerMode
		// calls wait for active server-mode RPCs or fail fast on reentrancy.
		if w.serverMode {
			if !w.reconfiguring && (w.writersWaiting == 0 || isReentrant) {
				w.inFlight++
				callCtx, endLineage := w.startLineageLocked(ctx, "", false)
				w.configMu.Unlock()
				return callCtx, w.releaseCallSlotOnce(endLineage), nil
			}
			if isReentrant {
				w.configMu.Unlock()
				return ctx, nil, fmt.Errorf(
					"plugin auth handler %s cannot reconfigure inside a nested callback",
					w.handlerName,
				)
			}
			waitCh := w.waitChanLocked()
			w.configMu.Unlock()
			select {
			case <-waitCh:
				w.configMu.Lock()
			case <-ctx.Done():
				return ctx, nil, ctx.Err()
			}
			continue
		}

		rawForConfigure, rpcSettings, hasConfigSource, err := w.resolveHandlerSettingsLocked(ctx, hc, profile)
		if err != nil {
			w.configMu.Unlock()
			return ctx, nil, err
		}

		needsReconfig := w.needsConfigureLocked(profile, rawForConfigure, hasConfigSource)
		if !needsReconfig && !w.reconfiguring && (w.writersWaiting == 0 || isReentrant) && (w.inFlight == 0 || w.inFlightProfile == profile) {
			*perCallSettings = rpcSettings
			w.inFlight++
			w.inFlightProfile = profile
			callCtx, endLineage := w.startLineageLocked(ctx, profile, true)
			w.configMu.Unlock()
			return callCtx, w.releaseCallSlotOnce(endLineage), nil
		}

		if isReentrant {
			activeProf := w.inFlightProfile
			if w.reconfiguring && activeProf == "" {
				activeProf = w.configuredProfile
			}
			reconfigInProgress := w.reconfiguring
			w.configMu.Unlock()
			if reconfigInProgress || activeProf == profile {
				return ctx, nil, fmt.Errorf(
					"plugin auth handler %s cannot reconfigure inside a nested callback",
					w.handlerName,
				)
			}
			return ctx, nil, fmt.Errorf(
				"plugin auth handler %s cannot reconfigure from profile %q to %q inside a nested callback",
				w.handlerName, auth.DisplayProfileName(activeProf), auth.DisplayProfileName(profile),
			)
		}

		if needsReconfig && !w.reconfiguring && w.inFlight == 0 {
			w.reconfiguring = true
			cfg := w.buildProviderConfig(hc, profile, rawForConfigure)
			cfgCtx, endCfgLineage := w.startLineageLocked(ctx, profile, true)
			w.configMu.Unlock()

			cfgErr := w.client.ConfigureAuthHandler(cfgCtx, w.handlerName, cfg)

			w.configMu.Lock()
			endCfgLineage()
			w.reconfiguring = false
			if cfgErr != nil {
				w.broadcastLocked()
				w.configMu.Unlock()
				return ctx, nil, fmt.Errorf("plugin auth handler %s configure for profile %q: %w",
					w.handlerName, auth.DisplayProfileName(profile), cfgErr)
			}
			w.recordConfiguredLocked(profile, rawForConfigure)
			*perCallSettings = rpcSettings
			w.inFlight++
			w.inFlightProfile = profile
			callCtx, endLineage := w.startLineageLocked(ctx, profile, true)
			w.broadcastLocked()
			w.configMu.Unlock()
			return callCtx, w.releaseCallSlotOnce(endLineage), nil
		}

		countedWriter := needsReconfig || (w.inFlight > 0 && w.inFlightProfile != profile)
		if countedWriter {
			w.writersWaiting++
		}
		waitCh := w.waitChanLocked()
		w.configMu.Unlock()

		select {
		case <-waitCh:
			w.configMu.Lock()
			if countedWriter {
				w.writersWaiting--
			}
		case <-ctx.Done():
			w.configMu.Lock()
			if countedWriter {
				w.writersWaiting--
				w.broadcastLocked()
			}
			w.configMu.Unlock()
			return ctx, nil, ctx.Err()
		}
	}
}

func newCallLineageID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func (w *AuthHandlerWrapper) hasActiveParentLocked(lineage []string) bool {
	if len(lineage) == 0 || len(w.activeCalls) == 0 {
		return false
	}
	for _, id := range lineage {
		if _, ok := w.activeCalls[id]; ok {
			return true
		}
	}
	return false
}

func (w *AuthHandlerWrapper) startLineageLocked(ctx context.Context, profile string, hasProfile bool) (context.Context, func()) {
	callID := newCallLineageID()
	if w.activeCalls == nil {
		w.activeCalls = make(map[string]struct{})
	}
	w.activeCalls[callID] = struct{}{}
	nextCtx := withCallbackLineage(ctx, callID)
	var popBroker func()
	if w.client != nil {
		popBroker = w.client.authCallbackLineage().push(w.handlerName, profile, hasProfile, lineageFromContext(nextCtx))
	}
	return nextCtx, func() {
		delete(w.activeCalls, callID)
		if popBroker != nil {
			popBroker()
		}
	}
}

func (w *AuthHandlerWrapper) needsConfigureLocked(profile string, rawForConfigure json.RawMessage, hasConfigSource bool) bool {
	if w.client == nil || w.client.plugin == nil {
		return false
	}
	var normalizedRaw []byte
	if len(rawForConfigure) > 0 && string(rawForConfigure) != "{}" {
		normalizedRaw = rawForConfigure
	}
	if !hasConfigSource {
		return w.configured && (w.configuredProfile != profile || !bytes.Equal(w.configuredSettingsRaw, normalizedRaw))
	}
	if !w.configured {
		return true
	}
	return w.configuredProfile != profile || !bytes.Equal(w.configuredSettingsRaw, normalizedRaw)
}

func (w *AuthHandlerWrapper) releaseCallSlotOnce(endLineage func()) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			w.configMu.Lock()
			if endLineage != nil {
				endLineage()
			}
			w.inFlight--
			if w.inFlight == 0 {
				w.inFlightProfile = ""
				w.broadcastLocked()
			}
			w.configMu.Unlock()
		})
	}
}

func (w *AuthHandlerWrapper) acquireExclusiveReconfigLocked(ctx context.Context) error {
	lineage := lineageFromContext(ctx)
	for w.reconfiguring || w.inFlight > 0 {
		if w.hasActiveParentLocked(lineage) {
			return fmt.Errorf("plugin auth handler %s cannot reconfigure inside a nested callback", w.handlerName)
		}
		w.writersWaiting++
		waitCh := w.waitChanLocked()
		w.configMu.Unlock()

		select {
		case <-waitCh:
			w.configMu.Lock()
			w.writersWaiting--
		case <-ctx.Done():
			w.configMu.Lock()
			w.writersWaiting--
			w.broadcastLocked()
			return ctx.Err()
		}
	}
	w.reconfiguring = true
	return nil
}

func (w *AuthHandlerWrapper) waitChanLocked() chan struct{} {
	if w.waitCh == nil {
		w.waitCh = make(chan struct{})
	}
	return w.waitCh
}

func (w *AuthHandlerWrapper) broadcastLocked() {
	if w.waitCh != nil {
		close(w.waitCh)
		w.waitCh = nil
	}
}

// resolveHandlerSettingsLocked computes the resolved JSON settings for profile
// with any runtime CLI overrides overlaid. Caller must hold w.configMu.
func (w *AuthHandlerWrapper) resolveHandlerSettingsLocked(
	ctx context.Context,
	hc hostConfig,
	profile string,
) (rawForConfigure json.RawMessage, perRPCSettings map[string]json.RawMessage, hasConfigSource bool, err error) {
	cfgCtx := ctx
	if config.FromContext(cfgCtx) == nil && hc.AppConfig != nil {
		cfgCtx = config.WithConfig(cfgCtx, hc.AppConfig)
	}
	baseRaw, fromAppCfg, marshalErr := marshalAppAuthHandlerSettings(cfgCtx, w.handlerName, profile)
	if marshalErr != nil {
		return nil, nil, false, marshalErr
	}
	hasConfigSource = fromAppCfg

	if !hasConfigSource {
		if staticRaw, ok := hc.BaseSettings[w.handlerName]; ok && len(staticRaw) > 0 {
			hasConfigSource = true
			baseRaw = bytes.Clone(staticRaw)
		}
	}

	profileOverrides := w.overrides[profile]
	if len(profileOverrides) > 0 {
		hasConfigSource = true
		var baseMap map[string]any
		if len(baseRaw) > 0 {
			if unmarshalErr := json.Unmarshal(baseRaw, &baseMap); unmarshalErr != nil {
				return nil, nil, false, fmt.Errorf("unmarshaling base settings for handler %s: %w", w.handlerName, unmarshalErr)
			}
		}
		if baseMap == nil {
			baseMap = make(map[string]any, len(profileOverrides))
		}
		for k, v := range profileOverrides {
			for ek := range baseMap {
				if ek != k && strings.EqualFold(ek, k) {
					delete(baseMap, ek)
				}
			}
			baseMap[k] = v
		}
		merged, mergeErr := json.Marshal(baseMap)
		if mergeErr != nil {
			return nil, nil, false, fmt.Errorf("marshaling merged settings for handler %s: %w", w.handlerName, mergeErr)
		}
		baseRaw = merged
	}

	if !hasConfigSource {
		return nil, nil, false, nil
	}

	if len(baseRaw) == 0 {
		baseRaw = []byte("{}")
	}

	var rpcMap map[string]json.RawMessage
	if unmarshalErr := json.Unmarshal(baseRaw, &rpcMap); unmarshalErr != nil {
		return nil, nil, false, fmt.Errorf("decoding resolved settings for handler %s: %w", w.handlerName, unmarshalErr)
	}
	if rpcMap == nil {
		rpcMap = make(map[string]json.RawMessage)
	}

	if string(baseRaw) != "{}" {
		rawForConfigure = json.RawMessage(bytes.Clone(baseRaw))
	}
	return rawForConfigure, rpcMap, hasConfigSource, nil
}

func (w *AuthHandlerWrapper) buildProviderConfig(hc hostConfig, profile string, handlerRaw json.RawMessage) ProviderConfig {
	var settings map[string]json.RawMessage
	if len(hc.BaseSettings) > 0 {
		settings = cloneSettingsMap(hc.BaseSettings)
		delete(settings, w.handlerName)
	}
	if len(handlerRaw) > 0 {
		if settings == nil {
			settings = make(map[string]json.RawMessage, 1)
		}
		settings[w.handlerName] = json.RawMessage(bytes.Clone(handlerRaw))
	}
	if len(settings) == 0 {
		settings = nil
	}
	var hostServiceID uint32
	if w.client != nil {
		hostServiceID = w.client.HostServiceID()
	}
	return ProviderConfig{
		Quiet:             hc.Quiet,
		NoColor:           hc.NoColor,
		BinaryName:        hc.BinaryName,
		Profile:           profile,
		AuthCallbackPages: hc.AuthCallbackPages,
		Settings:          settings,
		HostServiceID:     hostServiceID,
	}
}

func (w *AuthHandlerWrapper) recordConfiguredLocked(profile string, handlerRaw json.RawMessage) {
	w.configured = true
	w.configuredProfile = profile
	if len(handlerRaw) > 0 && string(handlerRaw) != "{}" {
		w.configuredSettingsRaw = bytes.Clone(handlerRaw)
	} else {
		w.configuredSettingsRaw = nil
	}
}

// Client returns the underlying plugin client.
func (w *AuthHandlerWrapper) Client() *AuthHandlerClient {
	return w.client
}

// StartupLatency returns the plugin process startup latency.
func (w *AuthHandlerWrapper) StartupLatency() time.Duration {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.startupLatency
}

// SetStartupLatency records the plugin process startup latency.
func (w *AuthHandlerWrapper) SetStartupLatency(d time.Duration) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.startupLatency = d
}

func (w *AuthHandlerWrapper) setHostConfig(hc hostConfig) {
	w.mu.Lock()
	defer w.mu.Unlock()
	hc.BaseSettings = cloneSettingsMap(hc.BaseSettings)
	w.hostCfg = hc
}

func (w *AuthHandlerWrapper) getHostConfig() hostConfig {
	w.mu.RLock()
	defer w.mu.RUnlock()
	hc := w.hostCfg
	hc.BaseSettings = cloneSettingsMap(hc.BaseSettings)
	return hc
}

// configureAndRegisterAuthHandlers configures each handler with host-side
// settings (when cfg is non-nil) and registers it in the auth registry.
// It also sets trusted verification domains on each wrapper from the official
// auth handler registry (per-handler) plus config.Auth.TrustedVerificationDomains.
// Returns the names of successfully registered handlers.
func configureAndRegisterAuthHandlers(ctx context.Context, registry *auth.Registry, client *AuthHandlerClient, handlers []AuthHandlerInfo, cfg *ProviderConfig) []string {
	lgr := logger.FromContext(ctx)

	// Resolve trusted verification domains from context sources.
	officialReg := authofficial.RegistryFromContext(ctx)
	var cfgDomains []string
	var handlerCfgs map[string]config.HandlerConfig
	if appCfg := config.FromContext(ctx); appCfg != nil {
		cfgDomains = appCfg.Auth.TrustedVerificationDomains
		handlerCfgs = appCfg.Auth.Handlers
	}

	var registered []string

	for _, info := range handlers {
		// Pre-check registration before sending config to the plugin.
		// This avoids leaking secrets (via ConfigureAuthHandler) to a
		// plugin whose handler name collides with an existing entry.
		if registry.Has(info.Name) {
			lgr.V(1).Info("skipping auth handler: name already registered", "handler", info.Name)
			continue
		}

		wrapper := NewAuthHandlerWrapper(client, info)

		// Resolve trusted verification domains (official + config) onto the
		// wrapper and fail closed on device-code verification when none resolve.
		applyTrustedDomains(wrapper, info.Name, officialReg, cfgDomains, handlerCfgs[info.Name].TrustedVerificationDomains)

		if cfg != nil {
			wrapper.setHostConfig(hostConfig{
				Quiet:             cfg.Quiet,
				NoColor:           cfg.NoColor,
				BinaryName:        cfg.BinaryName,
				Profile:           cfg.Profile,
				GlobalProfile:     auth.GlobalProfileFromContext(ctx),
				AppConfig:         config.FromContext(ctx),
				AuthCallbackPages: cfg.AuthCallbackPages,
				BaseSettings:      cloneSettingsMap(cfg.Settings),
			})
			// Mark reconfiguring under configMu before registration so concurrent
			// callers obtaining wrapper from registry or client.wrappers wait until
			// initial configuration completes without racing.
			wrapper.configMu.Lock()
			wrapper.reconfiguring = true
			wrapper.configMu.Unlock()
		}

		if err := registry.Register(wrapper); err != nil {
			client.unregisterWrapper(wrapper)
			wrapper.configMu.Lock()
			wrapper.reconfiguring = false
			wrapper.broadcastLocked()
			wrapper.configMu.Unlock()
			lgr.V(1).Info("failed to register auth handler", "handler", info.Name, "error", err)
			continue
		}

		registered = append(registered, info.Name)

		// Configure only after successful registration so secrets are
		// never sent to a plugin that won't actually be used.
		if cfg != nil {
			hostCfg := *cfg
			hostCfg.Settings = maps.Clone(cfg.Settings)
			hostCfg.HostServiceID = client.HostServiceID()
			injectAuthHandlerSettings(ctx, info.Name, &hostCfg)
			wrapper.configMu.Lock()
			cfgCtx, endCfgLineage := wrapper.startLineageLocked(ctx, hostCfg.Profile, true)
			wrapper.configMu.Unlock()
			cfgErr := client.ConfigureAuthHandler(cfgCtx, info.Name, hostCfg)
			wrapper.configMu.Lock()
			endCfgLineage()
			wrapper.reconfiguring = false
			if cfgErr != nil {
				lgr.Info("failed to configure auth handler plugin",
					"handler", info.Name,
					"error", cfgErr)
			} else {
				wrapper.recordConfiguredLocked(hostCfg.Profile, hostCfg.Settings[info.Name])
			}
			wrapper.broadcastLocked()
			wrapper.configMu.Unlock()
		}
	}

	return registered
}

// propagateStartupLatency sets startup latency on each wrapper registered by a
// specific client and records the associated metric. It iterates only the given
// handler names instead of scanning the full registry.
func propagateStartupLatency(ctx context.Context, registry *auth.Registry, client *AuthHandlerClient, names []string) {
	startupDuration := client.StartupDuration()
	for _, name := range names {
		h, err := registry.Get(name)
		if err != nil {
			continue
		}
		w, ok := h.(*AuthHandlerWrapper)
		if !ok {
			continue
		}
		w.SetStartupLatency(startupDuration)
		metrics.RecordAuthPluginStartup(ctx, name, startupDuration.Seconds())
	}
}

// buildTrustedDomains merges per-handler official domains with global config
// domains and per-handler config domains. Non-official handlers match no
// official entry, so their trust derives solely from config (global +
// per-handler) — they never inherit the hardcoded official domains.
func buildTrustedDomains(handlerName string, officialReg *authofficial.Registry, cfgDomains, handlerDomains []string) []string {
	var domains []string

	if officialReg != nil {
		if h, ok := officialReg.Get(handlerName); ok {
			domains = append(domains, h.TrustedVerificationDomains...)
		}
	}

	domains = append(domains, cfgDomains...)
	domains = append(domains, handlerDomains...)
	return domains
}

// applyTrustedDomains resolves a handler's trusted device-code verification
// domains (official hardcoded + operator config) onto the wrapper. When none
// resolve it fails closed by requiring trusted domains, so a plugin cannot phish
// via an arbitrary HTTPS verification URL. This uniformly covers catalog-resolved
// third-party handlers (no hardcoded domains) and official handlers whose OAuth
// host is per-cluster and thus carry no hardcoded domains (e.g. openshift);
// operators supply trust via auth.handlers.<name>.trustedVerificationDomains.
func applyTrustedDomains(wrapper *AuthHandlerWrapper, handlerName string, officialReg *authofficial.Registry, cfgDomains, handlerDomains []string) {
	trusted := buildTrustedDomains(handlerName, officialReg, cfgDomains, handlerDomains)
	if len(trusted) > 0 {
		wrapper.SetTrustedDomains(trusted)
		return
	}
	wrapper.SetRequireTrustedDomains(true)
}

// injectAuthHandlerSettings reads per-handler auth configuration from the app
// config, resolves the active profile (merging profile overrides on top of
// top-level config), and serializes the result into cfg.Settings[handlerName]
// so the plugin can apply host-side overrides (clientId, hostname, scopes, etc.).
func injectAuthHandlerSettings(ctx context.Context, handlerName string, cfg *ProviderConfig) {
	appCfg := config.FromContext(ctx)
	if appCfg == nil {
		return
	}

	profile := auth.ResolveActiveProfile(ctx, handlerName)
	cfg.Profile = profile

	raw, hasSource, err := marshalAppAuthHandlerSettings(ctx, handlerName, profile)
	if err != nil || !hasSource || len(raw) == 0 || string(raw) == "{}" {
		return
	}

	if cfg.Settings == nil {
		cfg.Settings = make(map[string]json.RawMessage)
	}
	cfg.Settings[handlerName] = raw
}

// marshalAppAuthHandlerSettings resolves and JSON-encodes the effective handler
// settings for profile from config.FromContext(ctx). hasSource is true when the
// app config declares a section for handlerName (even if the resolved default
// profile marshals to "{}").
func marshalAppAuthHandlerSettings(ctx context.Context, handlerName, profile string) (raw json.RawMessage, hasSource bool, err error) {
	appCfg := config.FromContext(ctx)
	if appCfg == nil {
		return nil, false, nil
	}

	switch handlerName {
	case "github":
		if appCfg.Auth.GitHub == nil {
			return nil, false, nil
		}
		resolved := auth.ResolveGitHubConfig(appCfg.Auth.GitHub, profile)
		raw, err = json.Marshal(resolved) //nolint:gosec // G117: intentionally forwarding host config (including clientSecret) to plugin over local gRPC
		return raw, true, err
	case "entra":
		if appCfg.Auth.Entra == nil {
			return nil, false, nil
		}
		resolved := auth.ResolveEntraConfig(appCfg.Auth.Entra, profile)
		raw, err = json.Marshal(resolved) //nolint:gosec // G117: intentionally forwarding host config (including clientSecret) to plugin over local gRPC
		return raw, true, err
	case "gcp":
		if appCfg.Auth.GCP == nil {
			return nil, false, nil
		}
		resolved := auth.ResolveGCPConfig(appCfg.Auth.GCP, profile)
		raw, err = json.Marshal(resolved) //nolint:gosec // G117: intentionally forwarding host config (including clientSecret) to plugin over local gRPC
		return raw, true, err
	default:
		// Open per-handler namespace (auth.handlers.<name>). scafctl core is
		// shape-blind to these settings: host-consumed blocks ("hostname",
		// "plugin", "trustedVerificationDomains") and profile metadata are
		// stripped, and per-profile overrides are merged on top of base settings.
		handlerCfg, ok := appCfg.Auth.Handlers[handlerName]
		if !ok || !auth.HasHandlerSettingsOrProfiles(handlerCfg) {
			return nil, false, nil
		}
		resolved := auth.ResolveHandlerSettings(handlerCfg, profile)
		raw, err = json.Marshal(resolved) //nolint:gosec // G117: intentionally forwarding opaque host config to plugin over local gRPC
		return raw, true, err
	}
}

// RegisterAuthHandlerPlugins discovers auth handler plugins in pluginDirs,
// validates each advertised handler name, and registers an isolated per-handler
// client scoped via WithSecretScope(name) with the auth registry. Returns the
// created clients (caller should Kill() them on cleanup).
func RegisterAuthHandlerPlugins(ctx context.Context, registry *auth.Registry, pluginDirs []string, cfg *ProviderConfig, clientOpts ...ClientOption) ([]*AuthHandlerClient, error) {
	var cfgFn func() *ProviderConfig
	if cfg != nil {
		cfgFn = func() *ProviderConfig { return cfg }
	}
	return RegisterScopedAuthHandlerPlugins(ctx, registry, pluginDirs, cfgFn, clientOpts...)
}

// probeDiscoveryClientOpts forwards caller transport/isolation options (such as
// WithSanitizedEnv, WithStartTimeout, and WithGRPCMaxMessageSize) to discovery
// probes while appending WithHostDeps(nil) last so HostService stays disabled
// until handler names have been validated and scoped.
func probeDiscoveryClientOpts(baseClientOpts []ClientOption) []ClientOption {
	return slices.Concat(baseClientOpts, []ClientOption{WithHostDeps(nil)})
}

// RegisterScopedAuthHandlerPlugins discovers auth handler binaries in
// pluginDirs without exposing HostService, validates each advertised handler
// name as a single dot-free namespace segment, and starts an isolated client
// per new handler scoped via WithSecretScope(name). cfgFn, when non-nil, is
// evaluated only when at least one valid new handler is about to be registered.
func RegisterScopedAuthHandlerPlugins(
	ctx context.Context,
	registry *auth.Registry,
	pluginDirs []string,
	cfgFn func() *ProviderConfig,
	baseClientOpts ...ClientOption,
) ([]*AuthHandlerClient, error) {
	probeClients, err := DiscoverAuthHandlers(pluginDirs, probeDiscoveryClientOpts(baseClientOpts)...)
	if err != nil {
		return nil, fmt.Errorf("failed to discover auth handler plugins: %w", err)
	}
	defer func() {
		for _, p := range probeClients {
			p.Kill()
		}
	}()

	lgr := logger.FromContext(ctx)
	var allClients []*AuthHandlerClient
	var errs []error

	for _, probe := range probeClients {
		binPath := probe.Path()
		handlers, hErr := probe.GetAuthHandlers(ctx)
		probe.Kill()
		if hErr != nil {
			continue
		}

		for _, info := range handlers {
			if registry.Has(info.Name) {
				lgr.V(1).Info("skipping auth handler: name already registered", "handler", info.Name)
				continue
			}
			if !validSecretScopeName(info.Name) {
				errs = append(errs, fmt.Errorf("auth handler plugin %s advertised invalid handler name %q", binPath, info.Name))
				continue
			}

			var cfg *ProviderConfig
			if cfgFn != nil {
				cfg = cfgFn()
			}
			scopedOpts := slices.Concat(baseClientOpts, []ClientOption{WithSecretScope(info.Name)})
			results := []FetchResult{{
				Name: info.Name,
				Kind: solution.PluginKindAuthHandler,
				Path: binPath,
			}}
			clients, regErr := RegisterFetchedAuthHandlerPluginsNamed(ctx, registry, results, cfg, info.Name, scopedOpts...)
			if regErr != nil {
				errs = append(errs, regErr)
				continue
			}
			allClients = append(allClients, clients...)
		}
	}

	return allClients, errors.Join(errs...)
}

// KillAllAuthHandlers terminates all auth handler plugin processes in the given client list.
// This is safe to call with a nil or empty slice.
func KillAllAuthHandlers(clients []*AuthHandlerClient) {
	for _, c := range clients {
		if c != nil {
			c.Kill()
		}
	}
}

func lockSiblings(client *AuthHandlerClient, siblings []*AuthHandlerWrapper) {
	if client != nil {
		client.multiLockMu.Lock()
		defer client.multiLockMu.Unlock()
	}
	for _, sib := range siblings {
		sib.configMu.Lock()
	}
}

func unlockSiblings(siblings []*AuthHandlerWrapper) {
	for i := len(siblings) - 1; i >= 0; i-- {
		siblings[i].configMu.Unlock()
	}
}

// ActivateServerMode coordinates plugin-wide server-mode activation across all
// AuthHandlerWrappers sharing w.client so activation never overlaps an
// in-flight RPC on any handler exposed by the plugin and propagates serverMode
// to every sibling wrapper once non-empty settings succeed.
func (w *AuthHandlerWrapper) ActivateServerMode(ctx context.Context, settings json.RawMessage) error {
	lineage := lineageFromContext(ctx)
	for {
		siblings := w.client.ensureAndSnapshotWrappers(w)
		lockSiblings(w.client, siblings)

		for _, sib := range siblings {
			if sib.hasActiveParentLocked(lineage) {
				unlockSiblings(siblings)
				return fmt.Errorf("plugin auth handler %s cannot reconfigure inside a nested callback", w.handlerName)
			}
		}

		var busy *AuthHandlerWrapper
		for _, sib := range siblings {
			if sib.reconfiguring || sib.inFlight > 0 {
				busy = sib
				break
			}
		}

		if busy == nil {
			endFns := make([]func(), 0, len(siblings))
			srvCtx := ctx
			for _, sib := range siblings {
				sib.reconfiguring = true
				var endFn func()
				srvCtx, endFn = sib.startLineageLocked(srvCtx, "", false)
				endFns = append(endFns, endFn)
			}
			unlockSiblings(siblings)

			err := w.client.ActivateServerMode(srvCtx, settings)
			setServerMode := err == nil && hasServerModeSettings(settings)
			if setServerMode && w.client != nil {
				w.client.markServerMode()
			}

			finalSiblings := w.client.ensureAndSnapshotWrappers(w)
			lockSiblings(w.client, finalSiblings)
			for _, endFn := range endFns {
				endFn()
			}
			for _, sib := range finalSiblings {
				if slices.Contains(siblings, sib) {
					sib.reconfiguring = false
				}
				if setServerMode {
					sib.serverMode = true
				}
				sib.broadcastLocked()
			}
			unlockSiblings(finalSiblings)
			return err
		}

		waitCh := busy.waitChanLocked()
		busy.writersWaiting++
		unlockSiblings(siblings)

		select {
		case <-waitCh:
			busy.configMu.Lock()
			busy.writersWaiting--
			busy.configMu.Unlock()
		case <-ctx.Done():
			busy.configMu.Lock()
			busy.writersWaiting--
			busy.broadcastLocked()
			busy.configMu.Unlock()
			return ctx.Err()
		}
	}
}

// willOpenBrowserOnLogin reports whether delegating Login to the plugin will
// attempt to launch a web browser on the host. When flow is empty (e.g. from
// 'scafctl kube login' or library callers) and a browser preference or $BROWSER
// is set, it consults DetectAvailableFlows (bounded by ctx and
// defaultPreflightFlowDetectTimeout) and Entra's configured defaultFlow so
// headless/CI flows (device_code, service_principal, workload_identity, pat)
// never emit spurious browser warnings. If ctx itself expires or is cancelled
// before preflight completes, that error is returned so Login aborts rather
// than proceeding after its deadline.
func (w *AuthHandlerWrapper) willOpenBrowserOnLogin(ctx context.Context, flow auth.Flow, effectiveBrowser string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if effectiveBrowser == "" && strings.TrimSpace(os.Getenv("BROWSER")) == "" {
		return false, nil
	}
	if flow != "" {
		return flow == auth.FlowInteractive, nil
	}
	detectCtx, detectCancel := context.WithTimeout(ctx, defaultPreflightFlowDetectTimeout)
	available, err := w.DetectAvailableFlows(detectCtx)
	detectCancel()
	if ctxErr := ctx.Err(); ctxErr != nil {
		return false, ctxErr
	}
	if err == nil {
		for _, fa := range available {
			if fa.Available {
				return auth.Flow(fa.Flow) == auth.FlowInteractive, nil
			}
		}
	}
	if w.handlerName == "entra" {
		hc := w.getHostConfig()
		cfgCtx := ctx
		if config.FromContext(cfgCtx) == nil && hc.AppConfig != nil {
			cfgCtx = config.WithConfig(cfgCtx, hc.AppConfig)
		}
		return auth.ResolveEntraDefaultFlow(cfgCtx) == auth.FlowInteractive, nil
	}
	supported := w.SupportedFlows()
	return len(supported) == 0 || slices.Contains(supported, auth.FlowInteractive), nil
}
