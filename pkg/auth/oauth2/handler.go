// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package oauth2 implements a generic configurable OAuth2 auth handler.
// Each CustomOAuth2Config registers as its own named auth.Handler supporting
// authorization code + PKCE, device code (RFC 8628), client credentials
// (RFC 6749 Section 4.4), workload identity federation (RFC 7523 & RFC 8693),
// and pre-issued static/robot tokens (PAT).
package oauth2

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"
	oauth "github.com/oakwood-commons/oauth-helpers"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/authpage"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/gotmpl"
	"github.com/oakwood-commons/scafctl/pkg/secrets"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"golang.org/x/sync/singleflight"
)

const (
	secretKeyPrefix               = "scafctl.auth.oauth2." //nolint:gosec // key prefix, not credential
	secretKeyRefreshSuffix        = "refresh_token"        //nolint:gosec // key suffix, not credential
	secretKeyMetadataSuffix       = "metadata"             //nolint:gosec // key suffix, not credential
	secretKeyPATTokenSuffix       = "pat_token"            //nolint:gosec // key suffix, not credential
	secretKeySessionStateSuffix   = "session_state"        //nolint:gosec // key suffix, not credential
	secretKeySessionVersionSuffix = "session_version"      //nolint:gosec // key suffix, not credential
	sessionStateLoggedOut         = "logged_out"
	sessionStateLoginFailed       = "login_failed"
	sessionStateCorrupt           = "corrupt"
	tokenCacheSuffix              = "token."           //nolint:gosec // key suffix, not credential
	derivedTokenSuffix            = "derived_token"    //nolint:gosec // key suffix, not credential
	derivedUsernameSuffix         = "derived_username" //nolint:gosec // key suffix, not credential
	defaultTimeout                = 5 * time.Minute
	callbackShutdownTimeout       = 5 * time.Second
	defaultPollInterval           = 5
	defaultImplicitExpiresIn      = 3600
	maxResponseBody               = 1 << 20
	defaultTokenType              = "Bearer"
	dynamicClientSecretKey        = "dynamic_client"
	jwtBearerAssertionType        = "urn:ietf:params:oauth:client-assertion-type:jwt-bearer" //nolint:gosec // RFC 7523 URI, not credential
	tokenExchangeGrantType        = "urn:ietf:params:oauth:grant-type:token-exchange"        //nolint:gosec // RFC 8693 URI, not credential
	deviceCodeGrantType           = "urn:ietf:params:oauth:grant-type:device_code"           //nolint:gosec // RFC 8628 URI, not credential
)

type dynamicClient struct {
	ClientID              string `json:"client_id"`
	ClientSecret          string `json:"client_secret"` //nolint:gosec // struct field, not a credential literal
	ClientSecretExpiresAt int64  `json:"client_secret_expires_at,omitempty"`
	// Fingerprint binds the cached credentials to the registration config
	// that produced them (registration endpoint, token URL, and client
	// metadata). If the config changes -- e.g. the handler is repointed at
	// a different authorization server -- the fingerprint no longer
	// matches and the cache is treated as a miss, forcing re-registration
	// instead of silently reusing credentials that were never issued for
	// the new target.
	Fingerprint string `json:"fingerprint,omitempty"`
}

// expired reports whether the cached dynamic client's secret has expired.
// A zero ClientSecretExpiresAt means the secret does not expire (RFC 7591).
func (c *dynamicClient) expired() bool {
	return c.ClientSecretExpiresAt > 0 && time.Now().Unix() >= c.ClientSecretExpiresAt
}

// dcrFingerprint computes a stable fingerprint for a Dynamic Client
// Registration config, binding cached credentials to the registration
// endpoint, token endpoint, and client metadata that produced them.
func dcrFingerprint(dcr *config.DynamicClientRegistrationConfig, tokenURL string) string {
	keys := make([]string, 0, len(dcr.ClientMetadata))
	for k := range dcr.ClientMetadata {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	sb.WriteString(dcr.RegistrationEndpoint)
	sb.WriteString("|")
	sb.WriteString(tokenURL)
	for _, k := range keys {
		fmt.Fprintf(&sb, "|%s=%v", k, dcr.ClientMetadata[k])
	}
	sum := sha256.Sum256([]byte(sb.String()))
	return hex.EncodeToString(sum[:])
}

type basicAuthCredentials struct {
	username string
	password string
}

type selectedDynamicClientKey struct{}

func withSelectedDynamicClient(ctx context.Context, c *dynamicClient) context.Context {
	if c == nil {
		return ctx
	}
	cp := *c
	return context.WithValue(ctx, selectedDynamicClientKey{}, &cp)
}

func selectedDynamicClientFromContext(ctx context.Context) *dynamicClient {
	if ctx == nil {
		return nil
	}
	c, _ := ctx.Value(selectedDynamicClientKey{}).(*dynamicClient)
	return c
}

type persistedDerivedToken struct {
	auth.Token
	Fingerprint string `json:"fingerprint"`
	Username    string `json:"username,omitempty"`
}

type persistedRefreshToken struct {
	Value       string    `json:"v"`
	Flow        auth.Flow `json:"flow,omitempty"`
	Fingerprint string    `json:"fingerprint"`
	Epoch       string    `json:"epoch,omitempty"`
}

type persistedPATSession struct {
	Value       string       `json:"v"`
	Username    string       `json:"username,omitempty"`
	Fingerprint string       `json:"fingerprint"`
	Claims      *auth.Claims `json:"claims,omitempty"`
	Scopes      []string     `json:"scopes,omitempty"`
}

// Handler implements auth.Handler for generic configurable OAuth2 services.
type Handler struct {
	cfg                   config.CustomOAuth2Config
	secretStore           secrets.Store
	secretErr             error
	secretStoreConfigured bool
	tokenCache            *auth.TokenCache
	httpClient            *http.Client
	logger                logr.Logger
	secretKeyPrefix       string
	binaryName            string
	pageRenderer          authpage.Renderer
	openBrowser           func(context.Context, string) error

	mu              sync.RWMutex
	dcrClients      map[string]*dynamicClient
	activeFlows     map[string]auth.Flow
	patUsernames    map[string]string
	derivedUsers    map[string]string
	sessionStates   map[string]string
	sessionStateSeq map[string]uint64
	sessionGen      map[string]uint64
	publishLocks    map[string]*sync.Mutex
	memCache        map[string]*auth.Token
	memDerived      map[string]*persistedDerivedToken
	sfGroup         singleflight.Group
}

// Compile-time interface assertions.
var (
	_ auth.Handler             = (*Handler)(nil)
	_ auth.TokenLister         = (*Handler)(nil)
	_ auth.TokenPurger         = (*Handler)(nil)
	_ auth.FlowDetector        = (*Handler)(nil)
	_ auth.FlowReporter        = (*Handler)(nil)
	_ auth.DefaultFlowProvider = (*Handler)(nil)
)

// Option configures the Handler.
type Option func(*Handler)

// WithSecretStore sets a custom secrets store.
func WithSecretStore(store secrets.Store) Option {
	return func(h *Handler) {
		h.secretStore = store
		h.secretStoreConfigured = true
	}
}

// WithSecretStoreError configures the handler in keyless in-memory mode with
// the given store-initialization error (for example when requireSecureKeyring
// is true and no OS keyring is available), preventing New from falling back to
// default file-backed secrets.New().
func WithSecretStoreError(err error) Option {
	return func(h *Handler) {
		h.secretStore = nil
		h.secretErr = err
		h.secretStoreConfigured = true
	}
}

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(client *http.Client) Option {
	return func(h *Handler) { h.httpClient = client }
}

// WithLogger sets the logger for the handler.
func WithLogger(lgr logr.Logger) Option {
	return func(h *Handler) { h.logger = lgr }
}

// WithSecretKeyPrefix overrides the default secret key prefix used for
// storing secrets in the keychain. This allows the generic oauth2.Handler
// to share tokens written by plugin auth handlers that use a different
// prefix convention.
func WithSecretKeyPrefix(prefix string) Option {
	return func(h *Handler) { h.secretKeyPrefix = prefix }
}

// WithBinaryName sets the CLI binary name passed to the authentication callback
// page renderer and internal-secret prefix checks. When empty, defaults to
// settings.CliBinaryName ("scafctl").
func WithBinaryName(name string) Option {
	return func(h *Handler) {
		if strings.TrimSpace(name) == "" {
			h.binaryName = settings.CliBinaryName
			return
		}
		h.binaryName = name
	}
}

// WithAuthPageRenderer overrides the HTML renderer used for interactive browser
// callback pages (authorization code and implicit grant flows). When nil,
// authpage.DefaultRenderer is used.
func WithAuthPageRenderer(r authpage.Renderer) Option {
	return func(h *Handler) { h.pageRenderer = r }
}

// New creates a new generic OAuth2 auth handler from a CustomOAuth2Config.
func New(cfg config.CustomOAuth2Config, opts ...Option) (*Handler, error) {
	// Implicit grant flow does not use PKCE.
	if cfg.ResponseType == "token" {
		cfg.DisablePKCE = true
	}

	h := &Handler{
		cfg:             cfg,
		logger:          logr.Discard(),
		secretKeyPrefix: secretKeyPrefix,
		binaryName:      settings.CliBinaryName,
		dcrClients:      make(map[string]*dynamicClient),
		activeFlows:     make(map[string]auth.Flow),
		patUsernames:    make(map[string]string),
		derivedUsers:    make(map[string]string),
		sessionStates:   make(map[string]string),
		sessionStateSeq: make(map[string]uint64),
		sessionGen:      make(map[string]uint64),
		publishLocks:    make(map[string]*sync.Mutex),
		memCache:        make(map[string]*auth.Token),
		memDerived:      make(map[string]*persistedDerivedToken),
	}
	for _, opt := range opts {
		opt(h)
	}

	if !h.secretStoreConfigured && h.secretStore == nil {
		store, err := secrets.New()
		if err != nil {
			h.secretErr = fmt.Errorf("failed to initialize secrets store: %w", err)
		} else {
			h.secretStore = store
		}
	}
	if h.httpClient == nil {
		h.httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	if h.secretStore != nil {
		prefix := h.secretKeyPrefix + h.cfg.Name + "." + tokenCacheSuffix
		h.tokenCache = auth.NewTokenCache(h.secretStore, prefix)
	}
	return h, nil
}

// Name returns the unique identifier for this auth handler.
func (h *Handler) Name() string { return h.cfg.Name }

// DisplayName returns the human-readable name.
func (h *Handler) DisplayName() string {
	if h.cfg.DisplayName != "" {
		return h.cfg.DisplayName
	}
	return h.cfg.Name
}

// DefaultFlow returns the handler's configured default authentication flow,
// implementing auth.DefaultFlowProvider.
func (h *Handler) DefaultFlow() auth.Flow {
	return h.resolveDefaultFlow()
}

// RegistryUsername returns the configured OCI registry username convention for
// the default profile context. Prefer RegistryUsernameForContext when a
// profile-bearing request context is available.
func (h *Handler) RegistryUsername() string {
	return h.RegistryUsernameForContext(context.Background())
}

// RegistryUsernameForContext returns the OCI registry username for the active
// profile in ctx (e.g. "$oauthtoken" for Quay.io, the derived username from
// tokenExchange, or the resolved robot username when FlowPAT is active and
// Username is set). Implements catalog.ContextualRegistryUsernameProvider.
func (h *Handler) RegistryUsernameForContext(ctx context.Context) string {
	ctx = h.resolveContextProfile(ctx)
	profile := auth.ProfileFromContext(ctx)

	if state, err := h.getSessionState(ctx); err != nil || state == sessionStateLoggedOut {
		return h.cfg.RegistryUsername
	}

	h.mu.RLock()
	var (
		flow        auth.Flow
		patUser     string
		derivedUser string
	)
	if h.activeFlows != nil {
		flow = h.activeFlows[profile]
	}
	if h.patUsernames != nil {
		patUser = h.patUsernames[profile]
	}
	if h.derivedUsers != nil {
		derivedUser = h.derivedUsers[profile]
	}
	h.mu.RUnlock()

	if flow == "" {
		flow = h.inferFlowForContext(ctx)
	}
	if flow == auth.FlowPAT && h.cfg.Username != "" {
		if patUser != "" {
			return patUser
		}
		if _, storedUser, patOK, patErr := h.loadValidStoredPAT(ctx); patErr == nil && patOK && storedUser != "" {
			h.setActivePAT(ctx, storedUser)
			return storedUser
		}
		if resolved, err := h.resolvePATUsername(ctx); err == nil && resolved != "" {
			return resolved
		}
	}
	if h.cfg.TokenExchange != nil && flow != auth.FlowPAT && derivedUser != "" {
		return derivedUser
	}
	return h.cfg.RegistryUsername
}

// ActiveFlow returns the authentication flow currently in use, implementing
// auth.FlowReporter.
func (h *Handler) ActiveFlow(ctx context.Context) auth.Flow {
	ctx = h.resolveContextProfile(ctx)
	profile := auth.ProfileFromContext(ctx)

	if state, err := h.getSessionState(ctx); err != nil || state == sessionStateLoggedOut {
		return ""
	}

	h.mu.RLock()
	var flow auth.Flow
	if h.activeFlows != nil {
		flow = h.activeFlows[profile]
	}
	h.mu.RUnlock()
	if flow != "" {
		return flow
	}
	if h.secretStore != nil {
		if meta, err := h.loadMetadata(ctx); err == nil && meta != nil && meta.LastLoginFlow != "" {
			if meta.LastLoginFlow != auth.FlowPAT {
				return meta.LastLoginFlow
			}
			if _, _, patOK, patErr := h.loadValidStoredPAT(ctx); patErr == nil && patOK {
				return auth.FlowPAT
			}
		}
	}
	if st, err := h.Status(ctx); err == nil && st.Authenticated && st.Flow != "" {
		return st.Flow
	}
	return ""
}

// SupportedFlows returns the flows this handler supports based on config.
func (h *Handler) SupportedFlows() []auth.Flow {
	var flows []auth.Flow
	if h.cfg.AuthorizeURL != "" {
		flows = append(flows, auth.FlowInteractive)
	}
	if h.cfg.DeviceAuthURL != "" {
		flows = append(flows, auth.FlowDeviceCode)
	}
	if h.hasWorkloadIdentityConfig() {
		flows = append(flows, auth.FlowWorkloadIdentity)
	}
	if h.hasClientCredentialsConfig() {
		flows = append(flows, auth.FlowClientCredentials)
	}
	if h.cfg.Token != "" {
		flows = append(flows, auth.FlowPAT)
	}
	return flows
}

// DetectAvailableFlows reports which configured non-interactive flows have
// resolvable credentials in the current environment, enabling automatic flow
// selection during 'auth login <name>' without '--flow'.
// When defaultFlow explicitly specifies 'interactive' or 'device_code',
// headless credentials are not auto-selected for 'auth login' so hybrid
// workstation configs preserve user login unless '--flow' is passed.
func (h *Handler) DetectAvailableFlows(ctx context.Context) ([]auth.FlowAvailability, error) {
	ctx = h.resolveContextProfile(ctx)
	explicitInteractiveDefault := h.cfg.DefaultFlow == "interactive" || h.cfg.DefaultFlow == "device_code"

	var out []auth.FlowAvailability
	for _, flow := range h.nonInteractiveFlowOrder() {
		switch flow { //nolint:exhaustive // only headless non-interactive flows are probed
		case auth.FlowWorkloadIdentity:
			if !h.hasWorkloadIdentityConfig() {
				continue
			}
			if explicitInteractiveDefault {
				out = append(out, auth.FlowAvailability{
					Flow:      auth.FlowWorkloadIdentity,
					Available: false,
					Reason:    fmt.Sprintf("defaultFlow is %s", h.cfg.DefaultFlow),
				})
				continue
			}
			_, err := h.resolveFederatedToken(ctx)
			if err == nil {
				out = append(out, auth.FlowAvailability{
					Flow:      auth.FlowWorkloadIdentity,
					Available: true,
					Reason:    "federated workload identity token is resolvable",
				})
			} else {
				out = append(out, auth.FlowAvailability{
					Flow:      auth.FlowWorkloadIdentity,
					Available: false,
					Reason:    err.Error(),
				})
			}
		case auth.FlowClientCredentials:
			if !h.hasClientCredentialsConfig() {
				continue
			}
			avail, reason := h.detectClientCredentialsAvailability(ctx)
			out = append(out, auth.FlowAvailability{
				Flow:      auth.FlowClientCredentials,
				Available: avail,
				Reason:    reason,
			})
		case auth.FlowPAT:
			if h.cfg.Token == "" {
				continue
			}
			if explicitInteractiveDefault {
				out = append(out, auth.FlowAvailability{
					Flow:      auth.FlowPAT,
					Available: false,
					Reason:    fmt.Sprintf("defaultFlow is %s", h.cfg.DefaultFlow),
				})
				continue
			}
			_, err := h.resolvePATToken(ctx)
			if err == nil {
				out = append(out, auth.FlowAvailability{
					Flow:      auth.FlowPAT,
					Available: true,
					Reason:    "static token credential is resolvable",
				})
			} else {
				out = append(out, auth.FlowAvailability{
					Flow:      auth.FlowPAT,
					Available: false,
					Reason:    err.Error(),
				})
			}
		}
	}
	if h.cfg.AuthorizeURL != "" {
		out = append(out, auth.FlowAvailability{
			Flow:      auth.FlowInteractive,
			Available: false,
			Reason:    "interactive browser flow requires user interaction",
		})
	}
	if h.cfg.DeviceAuthURL != "" {
		out = append(out, auth.FlowAvailability{
			Flow:      auth.FlowDeviceCode,
			Available: false,
			Reason:    "device code flow requires user interaction",
		})
	}
	return out, nil
}

func (h *Handler) detectClientCredentialsAvailability(ctx context.Context) (bool, string) {
	ctx = h.resolveContextProfile(ctx)
	if h.dcrEnabled() {
		// Only auto-select DCR for client_credentials when explicitly configured
		// as defaultFlow or when no interactive endpoints exist.
		if h.cfg.DefaultFlow != "client_credentials" && (h.cfg.AuthorizeURL != "" || h.cfg.DeviceAuthURL != "") {
			return false, "dynamic client registration shares interactive endpoints"
		}
		// Prefer an already-registered, unexpired dynamic client for this profile
		// without probing any bootstrap initialAccessToken or unused static secret.
		if existing := h.reusableDynamicClient(ctx); existing != nil && existing.ClientSecret != "" {
			return true, "persisted dynamic client credentials are available"
		}
		if h.cfg.DynamicClientRegistration.InitialAccessToken != "" {
			if _, err := config.ResolveCredentialValue(ctx, h.cfg.DynamicClientRegistration.InitialAccessToken, h.secretStore, h.reservedBinaryNames()...); err != nil {
				return false, err.Error()
			}
		}
		return true, "dynamic client registration is configured"
	}
	if h.cfg.ClientSecret != "" {
		// When authorizeURL is also configured and defaultFlow is explicitly
		// interactive or device_code, clientSecret may belong to confidential
		// authorization-code exchange rather than headless client_credentials.
		if h.cfg.DefaultFlow == "interactive" || h.cfg.DefaultFlow == "device_code" {
			return false, fmt.Sprintf("defaultFlow is %s", h.cfg.DefaultFlow)
		}
		secret, err := config.ResolveCredentialValue(ctx, h.cfg.ClientSecret, h.secretStore, h.reservedBinaryNames()...)
		if err != nil {
			return false, err.Error()
		}
		if secret != "" {
			return true, "clientSecret credential is resolvable"
		}
		return false, "clientSecret resolved to empty value"
	}
	return false, "clientSecret not configured"
}

func (h *Handler) hasWorkloadIdentityConfig() bool {
	return h.cfg.FederatedToken != "" || h.cfg.FederatedTokenFile != ""
}

func (h *Handler) hasClientCredentialsConfig() bool {
	return h.cfg.ClientSecret != "" || h.dcrEnabled()
}

// dcrEnabled reports whether this handler has RFC 7591 Dynamic Client
// Registration configured with a registration endpoint. When true, static
// clientID/clientSecret are supplied by registration rather than config.
func (h *Handler) dcrEnabled() bool {
	return h.cfg.DynamicClientRegistration != nil && h.cfg.DynamicClientRegistration.RegistrationEndpoint != ""
}

// Capabilities returns the handler's capabilities.
func (h *Handler) Capabilities() []auth.Capability {
	return []auth.Capability{
		auth.CapScopesOnLogin,
		auth.CapCallbackPort,
		auth.CapFlowOverride,
	}
}

func (h *Handler) storeDynamicClient(ctx context.Context, client *dynamicClient) error {
	profile := auth.ProfileFromContext(ctx)
	cp := *client
	h.mu.Lock()
	if h.dcrClients == nil {
		h.dcrClients = make(map[string]*dynamicClient)
	}
	h.dcrClients[profile] = &cp
	h.mu.Unlock()

	if h.secretStore == nil {
		return nil
	}
	data, err := json.Marshal(&cp) //nolint:gosec // marshaling to store in the secret store, not logging
	if err != nil {
		return fmt.Errorf("marshal dynamic client: %w", err)
	}
	return h.secretStore.Set(ctx, h.profileSecretKey(ctx, dynamicClientSecretKey), data)
}

func (h *Handler) loadDynamicClient(ctx context.Context) (*dynamicClient, error) {
	profile := auth.ProfileFromContext(ctx)
	if h.secretStore == nil {
		h.mu.RLock()
		var c *dynamicClient
		if h.dcrClients != nil {
			c = h.dcrClients[profile]
		}
		h.mu.RUnlock()
		if c == nil {
			return nil, secrets.ErrNotFound
		}
		cp := *c
		return &cp, nil
	}
	data, err := h.secretStore.Get(ctx, h.profileSecretKey(ctx, dynamicClientSecretKey))
	if err != nil {
		return nil, err
	}
	var client dynamicClient
	if err := json.Unmarshal(data, &client); err != nil {
		return nil, fmt.Errorf("unmarshal dynamic client: %w", err)
	}
	return &client, nil
}

func (h *Handler) reusableDynamicClient(ctx context.Context) *dynamicClient {
	if !h.dcrEnabled() {
		return nil
	}
	profile := auth.ProfileFromContext(ctx)
	fingerprint := dcrFingerprint(h.cfg.DynamicClientRegistration, h.cfg.TokenURL)

	h.mu.RLock()
	var existing *dynamicClient
	if h.dcrClients != nil {
		existing = h.dcrClients[profile]
	}
	h.mu.RUnlock()
	if existing != nil && existing.ClientID != "" && !existing.expired() && existing.Fingerprint == fingerprint {
		cp := *existing
		return &cp
	}

	cachedClient, err := h.loadDynamicClient(ctx)
	if err == nil && cachedClient != nil && cachedClient.ClientID != "" && !cachedClient.expired() && cachedClient.Fingerprint == fingerprint {
		cp := *cachedClient
		h.mu.Lock()
		if h.dcrClients == nil {
			h.dcrClients = make(map[string]*dynamicClient)
		}
		h.dcrClients[profile] = &cp
		h.mu.Unlock()
		ret := cp
		return &ret
	}
	return nil
}

// ensureLoadedDynamicClient populates the in-memory profile DCR client from
// secretStore when Dynamic Client Registration is configured so new CLI
// processes reuse the registered client_id/client_secret for token cache
// lookups and refresh_token grants.
func (h *Handler) ensureLoadedDynamicClient(ctx context.Context) {
	_ = h.reusableDynamicClient(ctx)
}

// ensureDynamicClient coalesces RFC 7591 Dynamic Client Registration per
// profile and registration fingerprint, returning an immutable snapshot of the
// selected client so concurrent requests never clobber one another's client
// binding mid-acquisition.
func (h *Handler) ensureDynamicClient(ctx context.Context, snap sessionSnapshot, flow auth.Flow) (*dynamicClient, error) {
	dcr := h.cfg.DynamicClientRegistration
	if dcr == nil || dcr.RegistrationEndpoint == "" {
		return nil, nil
	}
	profile := auth.ProfileFromContext(ctx)
	fingerprint := dcrFingerprint(dcr, h.cfg.TokenURL)
	sfKey := fmt.Sprintf("dcr|%s|%s|gen=%d|ver=%s", profile, fingerprint, snap.gen, snap.persistedVer)

	ch := h.sfGroup.DoChan(sfKey, func() (any, error) {
		sfCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), defaultTimeout)
		defer cancel()

		// Check in-memory first (especially important when secretStore == nil).
		h.mu.RLock()
		var memExisting *dynamicClient
		if h.dcrClients != nil {
			memExisting = h.dcrClients[profile]
		}
		h.mu.RUnlock()
		if memExisting != nil && memExisting.ClientID != "" && !memExisting.expired() && memExisting.Fingerprint == fingerprint {
			if h.secretStore == nil {
				cp := *memExisting
				return &cp, nil
			}
		}

		// Check persisted store (or in-memory fallback when secretStore == nil).
		cachedClient, err := h.loadDynamicClient(sfCtx)
		if err == nil && cachedClient != nil && cachedClient.ClientID != "" && !cachedClient.expired() && cachedClient.Fingerprint == fingerprint {
			h.logger.V(1).Info("using cached dynamic client")
			unlockCommit, commitErr := h.acquireSessionCommitLock(sfCtx, profile)
			if commitErr != nil {
				return nil, commitErr
			}
			pubMu := h.profilePublishLock(profile)
			pubMu.Lock()
			if state, stateErr := h.getSessionStateInternal(sfCtx, true); stateErr != nil {
				pubMu.Unlock()
				unlockCommit()
				return nil, auth.NewError(h.cfg.Name, "session_state", stateErr)
			} else if state == sessionStateLoggedOut || h.isLocallyLoggedOut(profile) {
				pubMu.Unlock()
				unlockCommit()
				return nil, auth.NewError(h.cfg.Name, "get_token", auth.ErrNotAuthenticated)
			}
			if ok, verErr := h.matchesSessionSnapshotLocked(sfCtx, profile, snap); verErr != nil || !ok {
				pubMu.Unlock()
				unlockCommit()
				if verErr != nil {
					return nil, verErr
				}
				return nil, auth.NewError(h.cfg.Name, "get_token", &sessionReplacedError{flow: flow})
			}
			cp := *cachedClient
			h.mu.Lock()
			if h.dcrClients == nil {
				h.dcrClients = make(map[string]*dynamicClient)
			}
			h.dcrClients[profile] = &cp
			h.mu.Unlock()
			pubMu.Unlock()
			unlockCommit()
			ret := cp
			return &ret, nil
		}
		if err != nil && !errors.Is(err, secrets.ErrNotFound) {
			return nil, auth.NewError(h.cfg.Name, "load_dynamic_client", err)
		}
		if cachedClient != nil {
			switch {
			case cachedClient.expired():
				h.logger.V(1).Info("cached dynamic client secret expired, re-registering")
			case cachedClient.Fingerprint != fingerprint:
				h.logger.V(1).Info("dynamic client registration config changed, re-registering")
			}
		}

		h.logger.V(1).Info("performing dynamic client registration")

		var initialToken string
		if dcr.InitialAccessToken != "" {
			var resolveErr error
			initialToken, resolveErr = config.ResolveCredentialValue(sfCtx, dcr.InitialAccessToken, h.secretStore, h.reservedBinaryNames()...)
			if resolveErr != nil {
				return nil, auth.NewError(h.cfg.Name, "dcr_initial_access_token", resolveErr)
			}
		}

		// Perform dynamic client registration. RFC 7591 requires the request
		// body to be a JSON object; marshal a nil metadata map as "{}" rather
		// than JSON null.
		clientMetadata := dcr.ClientMetadata
		if clientMetadata == nil {
			clientMetadata = map[string]any{}
		}
		reqBody, err := json.Marshal(clientMetadata)
		if err != nil {
			return nil, auth.NewError(h.cfg.Name, "dcr_marshal", fmt.Errorf("marshal client metadata: %w", err))
		}

		req, err := http.NewRequestWithContext(sfCtx, http.MethodPost, dcr.RegistrationEndpoint, bytes.NewBuffer(reqBody))
		if err != nil {
			return nil, auth.NewError(h.cfg.Name, "dcr_request", fmt.Errorf("create registration request: %w", err))
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		if initialToken != "" {
			req.Header.Set("Authorization", "Bearer "+initialToken)
		}

		resp, err := h.httpClient.Do(req) //nolint:gosec // URL from trusted admin config
		if err != nil {
			return nil, auth.NewError(h.cfg.Name, "dcr_http", fmt.Errorf("registration request failed: %w", err))
		}
		defer resp.Body.Close()

		respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
		if err != nil {
			return nil, auth.NewError(h.cfg.Name, "dcr_read_body", fmt.Errorf("read registration response: %w", err))
		}

		if resp.StatusCode != http.StatusCreated {
			return nil, auth.NewError(h.cfg.Name, "dcr_http_status", fmt.Errorf("registration failed (HTTP %d): %s", resp.StatusCode, string(respBody)))
		}

		var regResp struct {
			ClientID              string `json:"client_id"`
			ClientSecret          string `json:"client_secret"` //nolint:gosec // JSON field
			ClientSecretExpiresAt int64  `json:"client_secret_expires_at"`
		}
		if err := json.Unmarshal(respBody, &regResp); err != nil {
			return nil, auth.NewError(h.cfg.Name, "dcr_unmarshal", fmt.Errorf("parse registration response: %w", err))
		}

		if regResp.ClientID == "" {
			return nil, auth.NewError(h.cfg.Name, "dcr_missing_field", fmt.Errorf("registration response missing client_id"))
		}

		newClient := &dynamicClient{
			ClientID:              regResp.ClientID,
			ClientSecret:          regResp.ClientSecret,
			ClientSecretExpiresAt: regResp.ClientSecretExpiresAt,
			Fingerprint:           fingerprint,
		}
		unlockCommit, commitErr := h.acquireSessionCommitLock(sfCtx, profile)
		if commitErr != nil {
			return nil, commitErr
		}
		pubMu := h.profilePublishLock(profile)
		pubMu.Lock()
		if state, stateErr := h.getSessionStateInternal(sfCtx, true); stateErr != nil {
			pubMu.Unlock()
			unlockCommit()
			return nil, auth.NewError(h.cfg.Name, "session_state", stateErr)
		} else if state == sessionStateLoggedOut || h.isLocallyLoggedOut(profile) {
			pubMu.Unlock()
			unlockCommit()
			return nil, auth.NewError(h.cfg.Name, "get_token", auth.ErrNotAuthenticated)
		}
		if ok, verErr := h.matchesSessionSnapshotLocked(sfCtx, profile, snap); verErr != nil || !ok {
			pubMu.Unlock()
			unlockCommit()
			if verErr != nil {
				return nil, verErr
			}
			return nil, auth.NewError(h.cfg.Name, "get_token", &sessionReplacedError{flow: flow})
		}
		if err := h.storeDynamicClient(sfCtx, newClient); err != nil {
			pubMu.Unlock()
			unlockCommit()
			return nil, auth.NewError(h.cfg.Name, "dcr_store", err)
		}
		pubMu.Unlock()
		unlockCommit()

		cp := *newClient
		return &cp, nil
	})

	select {
	case res := <-ch:
		if res.Err != nil {
			return nil, res.Err
		}
		c, _ := res.Val.(*dynamicClient)
		if c == nil {
			return nil, nil
		}
		cp := *c
		return &cp, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Login performs authentication using the configured OAuth2 or credential flow.
func (h *Handler) Login(ctx context.Context, opts auth.LoginOptions) (*auth.Result, error) {
	if err := h.ensureSecrets(); err != nil {
		return nil, err
	}
	ctx = h.resolveContextProfile(ctx)
	profile := auth.ProfileFromContext(ctx)
	snap, snapErr := h.captureSessionSnapshot(ctx, profile)
	if snapErr != nil {
		return nil, auth.NewError(h.cfg.Name, "session_version", snapErr)
	}

	resolvedBrowser, err := auth.ResolveBrowserPreference(ctx, opts.Browser)
	if err != nil {
		return nil, auth.NewError(h.cfg.Name, "login", fmt.Errorf("invalid browser preference: %w", err))
	}
	opts.Browser = resolvedBrowser

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = defaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	flow := opts.Flow
	if flow == "" {
		if h.cfg.DefaultFlow == "" {
			if avail, err := h.DetectAvailableFlows(ctx); err == nil {
				for _, fa := range avail {
					if fa.Available {
						flow = fa.Flow
						break
					}
				}
			}
		}
		if flow == "" {
			flow = h.resolveDefaultFlow()
		}
	}

	scopes := opts.Scopes
	if len(scopes) == 0 {
		scopes = h.cfg.Scopes
	}

	if flow == auth.FlowPAT {
		res, recordSnap, patErr := h.patLogin(ctx, scopes, snap)
		if patErr != nil {
			if !errors.Is(patErr, auth.ErrNotAuthenticated) {
				h.recordFailedLogin(ctx, recordSnap)
			}
			return nil, patErr
		}
		return res, nil
	}

	dcrClient, err := h.ensureDynamicClient(ctx, snap, flow)
	if err != nil {
		if !errors.Is(err, auth.ErrNotAuthenticated) {
			h.recordFailedLogin(ctx, snap)
		}
		return nil, err
	}
	ctx = withSelectedDynamicClient(ctx, dcrClient)

	var tokenResp *tokenResponse
	var cbServer *oauth.CallbackServer
	var callbackHit bool
	var loginErr error
	defer func() {
		if cbServer != nil {
			_ = cbServer.Close()
		}
	}()

	settleCallback := func(success bool) {
		if cbServer == nil {
			return
		}
		if !callbackHit {
			_ = cbServer.Close()
			cbServer = nil
			return
		}
		cbServer.Complete(success)
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), callbackShutdownTimeout)
		defer shutdownCancel()
		_ = cbServer.Shutdown(shutdownCtx)
		cbServer = nil
	}

	switch flow { //nolint:exhaustive // only generic OAuth2 flows + WIF/PAT are supported
	case auth.FlowInteractive:
		callbackPort := opts.CallbackPort
		if callbackPort == 0 {
			callbackPort = h.cfg.CallbackPort
		}
		tokenResp, cbServer, callbackHit, loginErr = h.authCodeLogin(ctx, opts, scopes, callbackPort)
	case auth.FlowDeviceCode:
		tokenResp, loginErr = h.deviceCodeLogin(ctx, opts, scopes)
	case auth.FlowClientCredentials:
		tokenResp, loginErr = h.clientCredentialsLogin(ctx, scopes)
	case auth.FlowWorkloadIdentity:
		tokenResp, loginErr = h.workloadIdentityLogin(ctx, scopes)
	default:
		return nil, auth.NewError(h.cfg.Name, "login",
			fmt.Errorf("%w: %s (supported: %v)", auth.ErrFlowNotSupported, flow, h.SupportedFlows()))
	}
	if loginErr != nil {
		settleCallback(false)
		h.recordFailedLogin(ctx, snap)
		return nil, loginErr
	}

	// Token exchange (optional post-flow pipeline)
	expiresAt := h.tokenExpiresAt(tokenResp)
	var derived *exchangeResult
	if h.cfg.TokenExchange != nil {
		var exchangeErr error
		derived, exchangeErr = h.executeTokenExchange(ctx, tokenResp.AccessToken)
		if exchangeErr != nil {
			settleCallback(false)
			h.recordFailedLogin(ctx, snap)
			return nil, auth.NewError(h.cfg.Name, "token_exchange", exchangeErr)
		}
	}

	// Verify token identity (optional)
	var claims *auth.Claims
	if h.cfg.VerifyURL != "" {
		var verifyErr error
		claims, verifyErr = h.verifyToken(ctx, tokenResp.AccessToken)
		if verifyErr != nil {
			h.logger.V(1).Info("token verification failed, continuing without identity", "error", verifyErr)
		}
	}
	if claims == nil {
		claims = h.fallbackClaims(ctx, tokenResp.AccessToken, flow)
	}

	unlockCommit, commitLockErr := h.acquireSessionCommitLock(ctx, profile)
	if commitLockErr != nil {
		settleCallback(false)
		h.recordFailedLogin(ctx, snap)
		return nil, auth.NewError(h.cfg.Name, "login", commitLockErr)
	}
	pubMu := h.profilePublishLock(profile)
	pubMu.Lock()
	snapOK, snapVerErr := h.matchesSessionSnapshotLocked(ctx, profile, snap)
	if snapVerErr != nil || !snapOK {
		pubMu.Unlock()
		unlockCommit()
		settleCallback(false)
		if snapVerErr != nil {
			return nil, auth.NewError(h.cfg.Name, "session_version", snapVerErr)
		}
		return nil, auth.NewError(h.cfg.Name, "login", auth.ErrNotAuthenticated)
	}
	newSnap, advErr := h.advanceSessionVersionLocked(ctx, profile)
	if advErr != nil {
		pubMu.Unlock()
		unlockCommit()
		settleCallback(false)
		h.recordFailedLogin(ctx, snap)
		return nil, auth.NewError(h.cfg.Name, "session_version", advErr)
	}
	if derived != nil {
		// Use the primary token's expiry for the derived token since the exchange
		// response does not carry its own expiry.
		if storeErr := h.storeDerivedToken(ctx, derived, expiresAt, flow, scopes); storeErr != nil {
			pubMu.Unlock()
			unlockCommit()
			settleCallback(false)
			h.recordFailedLogin(ctx, newSnap)
			return nil, auth.NewError(h.cfg.Name, "store_derived_token", storeErr)
		}
	} else if clearDerivedErr := h.clearDerivedStateErr(ctx); clearDerivedErr != nil {
		pubMu.Unlock()
		unlockCommit()
		settleCallback(false)
		h.recordFailedLogin(ctx, newSnap)
		return nil, auth.NewError(h.cfg.Name, "clear_derived_token", clearDerivedErr)
	}
	if storeErr := h.storeTokens(ctx, tokenResp, claims, expiresAt, flow, scopes); storeErr != nil {
		pubMu.Unlock()
		unlockCommit()
		settleCallback(false)
		h.recordFailedLogin(ctx, newSnap)
		return nil, auth.NewError(h.cfg.Name, "store_tokens", storeErr)
	}
	if clearErr := h.clearSessionState(ctx); clearErr != nil {
		pubMu.Unlock()
		unlockCommit()
		settleCallback(false)
		h.recordFailedLogin(ctx, newSnap)
		return nil, auth.NewError(h.cfg.Name, "session_state", clearErr)
	}
	h.setActiveFlow(ctx, flow)
	pubMu.Unlock()
	unlockCommit()

	settleCallback(true)
	return &auth.Result{Claims: claims, ExpiresAt: expiresAt}, nil
}

// patLogin handles explicit 'auth login <name> --flow pat'. It resolves the
// static token via SecretRef, skips TokenExchange, strictly validates against
// VerifyURL when configured, and stores the session under pat_token bound to a
// PAT configuration fingerprint without placing a zero-expiry entry into TokenCache.
func (h *Handler) patLogin(ctx context.Context, scopes []string, snap sessionSnapshot) (*auth.Result, sessionSnapshot, error) {
	patToken, err := h.resolvePATToken(ctx)
	if err != nil {
		return nil, snap, auth.NewError(h.cfg.Name, "login", err)
	}

	username, err := h.resolvePATUsername(ctx)
	if err != nil {
		return nil, snap, auth.NewError(h.cfg.Name, "login", err)
	}

	var claims *auth.Claims
	if h.cfg.VerifyURL != "" {
		var verifyErr error
		claims, verifyErr = h.verifyToken(ctx, patToken)
		if verifyErr != nil {
			return nil, snap, auth.NewError(h.cfg.Name, "verify_token", verifyErr)
		}
	}
	if claims == nil {
		claims = &auth.Claims{}
	}
	if claims.Username == "" && username != "" {
		claims.Username = username
	}

	meta := &auth.HandlerMetadata{
		Claims:        claims,
		ExpiresAt:     time.Time{},
		Scopes:        scopes,
		LastLoginFlow: auth.FlowPAT,
	}
	metaBytes, err := json.Marshal(meta)
	if err != nil {
		return nil, snap, auth.NewError(h.cfg.Name, "store_metadata", fmt.Errorf("marshal metadata: %w", err))
	}

	profile := auth.ProfileFromContext(ctx)
	unlockCommit, commitLockErr := h.acquireSessionCommitLock(ctx, profile)
	if commitLockErr != nil {
		return nil, snap, auth.NewError(h.cfg.Name, "login", commitLockErr)
	}
	defer unlockCommit()

	pubMu := h.profilePublishLock(profile)
	pubMu.Lock()
	defer pubMu.Unlock()

	snapOK, snapVerErr := h.matchesSessionSnapshotLocked(ctx, profile, snap)
	if snapVerErr != nil {
		return nil, snap, auth.NewError(h.cfg.Name, "session_version", snapVerErr)
	}
	if !snapOK {
		return nil, snap, auth.NewError(h.cfg.Name, "login", auth.ErrNotAuthenticated)
	}

	// Clear any OAuth refresh/derived/cached tokens from an earlier flow on this profile
	// before committing the PAT session so stale refresh credentials are never retained.
	if err := h.secretStore.Delete(ctx, h.profileSecretKey(ctx, secretKeyRefreshSuffix)); err != nil && !errors.Is(err, secrets.ErrNotFound) {
		return nil, snap, auth.NewError(h.cfg.Name, "clear_refresh_token", fmt.Errorf("clear previous refresh token: %w", err))
	}
	if err := h.clearDerivedStateErr(ctx); err != nil {
		return nil, snap, auth.NewError(h.cfg.Name, "clear_derived_token", err)
	}
	if tc := h.profileTokenCache(ctx); tc != nil {
		_ = tc.Clear(ctx)
	}
	newSnap, advErr := h.advanceSessionVersionLocked(ctx, profile)
	if advErr != nil {
		return nil, snap, auth.NewError(h.cfg.Name, "session_version", advErr)
	}
	if err := h.storePATSessionSecrets(ctx, patToken, username, claims, scopes); err != nil {
		return nil, newSnap, auth.NewError(h.cfg.Name, "store_pat", err)
	}
	if err := h.secretStore.Set(ctx, h.profileSecretKey(ctx, secretKeyMetadataSuffix), metaBytes); err != nil {
		return nil, newSnap, auth.NewError(h.cfg.Name, "store_metadata", fmt.Errorf("store metadata: %w", err))
	}
	if clearErr := h.clearSessionState(ctx); clearErr != nil {
		return nil, newSnap, auth.NewError(h.cfg.Name, "session_state", clearErr)
	}
	h.setActivePAT(ctx, username)
	return &auth.Result{Claims: claims, ExpiresAt: time.Time{}}, newSnap, nil
}

type sessionSnapshot struct {
	gen          uint64
	persistedVer string
}

type sessionReplacedError struct {
	flow auth.Flow
}

func (e *sessionReplacedError) Error() string {
	return auth.ErrNotAuthenticated.Error()
}

func (e *sessionReplacedError) Unwrap() error {
	return auth.ErrNotAuthenticated
}

func (h *Handler) readPersistedSessionVersion(ctx context.Context) (string, error) {
	if h.secretStore == nil {
		return "", nil
	}
	key := h.profileSecretKey(ctx, secretKeySessionVersionSuffix)
	data, err := h.secretStore.Get(ctx, key)
	if err != nil {
		if errors.Is(err, secrets.ErrNotFound) {
			return "", nil
		}
		if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			// Real secrets.Store.Get auto-deletes corrupted .enc files before
			// returning ErrCorrupted. Re-seed a fresh non-empty epoch so
			// subsequent reads cannot revert to ErrNotFound ("") and match
			// pre-session snapshots.
			_ = h.secretStore.Set(context.WithoutCancel(ctx), key, []byte(newSessionEpoch()))
		}
		return "", err
	}
	ver := strings.TrimSpace(string(data))
	if ver == "" {
		_ = h.secretStore.Set(context.WithoutCancel(ctx), key, []byte(newSessionEpoch()))
		return "", fmt.Errorf("session version is empty or corrupt")
	}
	return ver, nil
}

func (h *Handler) captureSessionSnapshot(ctx context.Context, profile string) (sessionSnapshot, error) {
	ver, err := h.readPersistedSessionVersion(ctx)
	if err != nil {
		return sessionSnapshot{}, err
	}
	return sessionSnapshot{
		gen:          h.currentSessionGen(profile),
		persistedVer: ver,
	}, nil
}

func (h *Handler) matchesSessionSnapshotLocked(ctx context.Context, profile string, snap sessionSnapshot) (bool, error) {
	if h.currentSessionGen(profile) != snap.gen {
		return false, nil
	}
	if h.secretStore != nil {
		curVer, err := h.readPersistedSessionVersion(ctx)
		if err != nil {
			return false, err
		}
		if curVer != snap.persistedVer {
			return false, nil
		}
	}
	return true, nil
}

func (h *Handler) advanceSessionVersionLocked(ctx context.Context, profile string) (sessionSnapshot, error) {
	ver := newSessionEpoch()
	if h.secretStore != nil {
		if err := h.secretStore.Set(ctx, h.profileSecretKey(ctx, secretKeySessionVersionSuffix), []byte(ver)); err != nil {
			return sessionSnapshot{}, fmt.Errorf("store session version: %w", err)
		}
	}
	h.mu.Lock()
	if h.sessionGen == nil {
		h.sessionGen = make(map[string]uint64)
	}
	h.sessionGen[profile]++
	newGen := h.sessionGen[profile]
	if h.activeFlows != nil {
		delete(h.activeFlows, profile)
	}
	if h.patUsernames != nil {
		delete(h.patUsernames, profile)
	}
	if h.derivedUsers != nil {
		delete(h.derivedUsers, profile)
	}
	profilePrefix := profile + "|"
	for k := range h.memCache {
		if strings.HasPrefix(k, profilePrefix) {
			delete(h.memCache, k)
		}
	}
	for k := range h.memDerived {
		if strings.HasPrefix(k, profilePrefix) {
			delete(h.memDerived, k)
		}
	}
	h.mu.Unlock()
	return sessionSnapshot{gen: newGen, persistedVer: ver}, nil
}

// Logout clears all stored tokens for this handler and records an explicit
// logged-out session state for the active profile so ambient config credentials
// are not silently re-activated until the next Login.
func (h *Handler) Logout(ctx context.Context) error {
	ctx = h.resolveContextProfile(ctx)
	profile := auth.ProfileFromContext(ctx)

	if unlockCommit, lockErr := h.acquireSessionCommitLock(context.WithoutCancel(ctx), profile); lockErr == nil {
		defer unlockCommit()
	}

	pubMu := h.profilePublishLock(profile)
	pubMu.Lock()
	defer pubMu.Unlock()

	h.mu.Lock()
	if h.sessionGen == nil {
		h.sessionGen = make(map[string]uint64)
	}
	h.sessionGen[profile]++
	if h.activeFlows != nil {
		delete(h.activeFlows, profile)
	}
	if h.patUsernames != nil {
		delete(h.patUsernames, profile)
	}
	if h.derivedUsers != nil {
		delete(h.derivedUsers, profile)
	}
	if h.dcrClients != nil {
		delete(h.dcrClients, profile)
	}
	profilePrefix := profile + "|"
	for k := range h.memCache {
		if strings.HasPrefix(k, profilePrefix) {
			delete(h.memCache, k)
		}
	}
	for k := range h.memDerived {
		if strings.HasPrefix(k, profilePrefix) {
			delete(h.memDerived, k)
		}
	}
	if h.sessionStates == nil {
		h.sessionStates = make(map[string]string)
	}
	h.sessionStates[profile] = sessionStateLoggedOut
	if h.sessionStateSeq == nil {
		h.sessionStateSeq = make(map[string]uint64)
	}
	h.sessionStateSeq[profile]++
	h.mu.Unlock()

	if err := h.ensureSecrets(); err != nil {
		return err
	}
	var errs []error
	tc := h.profileTokenCache(ctx)
	if tc != nil {
		if err := tc.Clear(ctx); err != nil {
			errs = append(errs, fmt.Errorf("clear token cache: %w", err))
		}
	}
	for _, suffix := range []string{
		secretKeyRefreshSuffix,
		secretKeyMetadataSuffix,
		derivedTokenSuffix,
		derivedUsernameSuffix,
		secretKeyPATTokenSuffix,
	} {
		if err := h.secretStore.Delete(ctx, h.profileSecretKey(ctx, suffix)); err != nil {
			errs = append(errs, err)
		}
	}
	if err := h.secretStore.Set(ctx, h.profileSecretKey(ctx, secretKeySessionVersionSuffix), []byte(newSessionEpoch())); err != nil {
		errs = append(errs, err)
	}
	if err := h.secretStore.Set(ctx, h.profileSecretKey(ctx, secretKeySessionStateSuffix), []byte(sessionStateLoggedOut)); err != nil {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return fmt.Errorf("logout %s: %w", h.cfg.Name, errs[0])
	}
	return nil
}

// ListCachedTokens returns all cached tokens for this handler.
func (h *Handler) ListCachedTokens(ctx context.Context) ([]*auth.CachedTokenInfo, error) {
	tc := h.profileTokenCache(ctx)
	if tc == nil {
		return nil, nil
	}

	entries, err := tc.ListCachedEntries(ctx)
	if err != nil {
		return nil, fmt.Errorf("list cached tokens for %s: %w", h.cfg.Name, err)
	}

	var result []*auth.CachedTokenInfo
	var getErrors []string
	for _, entry := range entries {
		token, err := tc.Get(ctx, entry.Flow, entry.Fingerprint, entry.Scope)
		if err != nil {
			getErrors = append(getErrors, fmt.Sprintf("flow=%s scope=%s: %v", entry.Flow, entry.Scope, err))
			continue
		}
		if token == nil {
			continue
		}
		result = append(result, &auth.CachedTokenInfo{
			Handler:     h.cfg.Name,
			TokenKind:   "access",
			Scope:       entry.Scope,
			TokenType:   token.TokenType,
			Flow:        entry.Flow,
			Fingerprint: entry.Fingerprint,
			ExpiresAt:   token.ExpiresAt,
			CachedAt:    token.CachedAt,
			IsExpired:   token.IsExpired(),
			SessionID:   token.SessionID,
		})
	}

	var retErr error
	if len(getErrors) > 0 {
		retErr = fmt.Errorf("partial read failures for %s: %s", h.cfg.Name, strings.Join(getErrors, "; "))
	}
	return result, retErr
}

// PurgeExpiredTokens removes all expired access tokens from the cache.
func (h *Handler) PurgeExpiredTokens(ctx context.Context) (int, error) {
	tc := h.profileTokenCache(ctx)
	if tc == nil {
		return 0, nil
	}
	n, err := tc.PurgeExpired(ctx)
	if err != nil {
		return n, fmt.Errorf("purge expired tokens for %s: %w", h.cfg.Name, err)
	}
	return n, nil
}

// Status returns the current authentication state, falling back automatically
// to configured headless credentials (workload_identity -> client_credentials
// -> pat) when no active login session is stored or when the secrets store is
// unavailable in keyless CI containers.
func (h *Handler) Status(ctx context.Context) (*auth.Status, error) {
	ctx = h.resolveContextProfile(ctx)

	state, stateErr := h.getSessionState(ctx)
	if stateErr != nil {
		return &auth.Status{Authenticated: false, Reason: "session state unreadable"}, nil //nolint:nilerr // Status reports unreadable session state via Authenticated=false + Reason
	}
	if state == sessionStateLoggedOut {
		return &auth.Status{Authenticated: false, Reason: "not logged in"}, nil
	}

	h.ensureLoadedDynamicClient(ctx)

	var expiredMeta *auth.HandlerMetadata
	var corruptMeta bool

	if h.secretStore != nil {
		meta, err := h.loadMetadata(ctx)
		switch {
		case err == nil && meta != nil:
			// Stored PAT sessions use zero ExpiresAt (non-expiring) and must still
			// match the current PAT configuration fingerprint and have pat_token present.
			switch {
			case meta.LastLoginFlow == auth.FlowPAT:
				storedPAT, patOK, patErr := h.loadValidStoredPATSession(ctx)
				switch {
				case patErr != nil:
					corruptMeta = true
				case patOK && (meta.ExpiresAt.IsZero() || !meta.ExpiresAt.Before(time.Now())):
					h.restoreStoredPATUsername(ctx, meta, storedPAT.Username)
					claims := meta.Claims
					if storedPAT.Claims != nil {
						claims = storedPAT.Claims
					}
					scopes := meta.Scopes
					if storedPAT.Scopes != nil {
						scopes = storedPAT.Scopes
					}
					return &auth.Status{
						Authenticated: true,
						Claims:        claims,
						ExpiresAt:     meta.ExpiresAt,
						Scopes:        scopes,
						Flow:          meta.LastLoginFlow,
					}, nil
				case patOK && !meta.ExpiresAt.IsZero() && meta.ExpiresAt.Before(time.Now()):
					expiredMeta = meta
				}
			case !meta.ExpiresAt.IsZero() && !meta.ExpiresAt.Before(time.Now()):
				if meta.LastLoginFlow != "" {
					h.setActiveFlow(ctx, meta.LastLoginFlow)
				}
				return &auth.Status{
					Authenticated: true,
					Claims:        meta.Claims,
					ExpiresAt:     meta.ExpiresAt,
					Scopes:        meta.Scopes,
					Flow:          meta.LastLoginFlow,
				}, nil
			default:
				expiredMeta = meta
			}
		case err != nil && !errors.Is(err, secrets.ErrNotFound):
			corruptMeta = true
		}
	}

	if corruptMeta {
		return &auth.Status{Authenticated: false, Reason: "metadata corrupt or unreadable"}, nil
	}

	// Do not advertise ambient headless authentication immediately after a
	// failed Login attempt on this profile.
	if state == sessionStateLoginFailed {
		return &auth.Status{Authenticated: false, Reason: "not logged in"}, nil
	}

	// Zero-ceremony headless credential detection (no network I/O).
	if headlessStatus := h.headlessStatus(ctx); headlessStatus != nil {
		return headlessStatus, nil
	}

	if h.secretStore == nil {
		return &auth.Status{Authenticated: false, Reason: "secrets store unavailable"}, nil
	}
	if expiredMeta != nil {
		return &auth.Status{Authenticated: false, Reason: "session expired", Claims: expiredMeta.Claims}, nil
	}
	return &auth.Status{Authenticated: false, Reason: "not logged in"}, nil
}

func (h *Handler) headlessStatus(ctx context.Context) *auth.Status {
	scope := strings.Join(h.cfg.Scopes, " ")
	for _, flow := range h.nonInteractiveFlowOrder() {
		switch flow { //nolint:exhaustive // only headless non-interactive flows are checked
		case auth.FlowWorkloadIdentity:
			if !h.hasWorkloadIdentityConfig() {
				continue
			}
			cached := h.lookupEffectiveCachedToken(ctx, auth.FlowWorkloadIdentity, scope, 0)
			jwt, err := h.resolveFederatedToken(ctx)
			if (err != nil || jwt == "") && cached == nil {
				continue
			}
			clientID := h.effectiveClientID(ctx)
			claims := &auth.Claims{ClientID: clientID}
			tokenToParse := jwt
			if tokenToParse == "" && cached != nil {
				tokenToParse = cached.AccessToken
			}
			if tokenToParse != "" {
				if jwtClaims, parseErr := auth.ParseJWTClaims(tokenToParse); parseErr == nil {
					if jwtClaims.Subject != "" {
						claims.Subject = jwtClaims.Subject
					}
					if jwtClaims.Issuer != "" {
						claims.Issuer = jwtClaims.Issuer
					}
				}
			}
			if claims.DisplayIdentity() == "" && clientID != "" {
				claims.Name = fmt.Sprintf("Workload Identity (%s)", clientID)
			}
			var tokenFile string
			switch {
			case h.cfg.FederatedTokenFile != "":
				tokenFile = h.cfg.FederatedTokenFile
			case h.cfg.FederatedToken.Scheme() == "file":
				tokenFile = strings.TrimPrefix(string(h.cfg.FederatedToken), "file://")
			}
			var expiresAt time.Time
			if cached != nil {
				expiresAt = cached.ExpiresAt
			} else if primaryCached := h.lookupCachedToken(ctx, auth.FlowWorkloadIdentity, scope, 0); primaryCached != nil {
				expiresAt = primaryCached.ExpiresAt
			}
			h.setActiveFlow(ctx, auth.FlowWorkloadIdentity)
			return &auth.Status{
				Authenticated: true,
				Flow:          auth.FlowWorkloadIdentity,
				IdentityType:  auth.IdentityTypeWorkloadIdentity,
				ClientID:      clientID,
				TokenFile:     tokenFile,
				Claims:        claims,
				Scopes:        h.cfg.Scopes,
				ExpiresAt:     expiresAt,
			}

		case auth.FlowClientCredentials:
			if !h.hasClientCredentialsConfig() {
				continue
			}
			cached := h.lookupEffectiveCachedToken(ctx, auth.FlowClientCredentials, scope, 0)
			avail, _ := h.detectClientCredentialsAvailability(ctx)
			if !avail && cached == nil {
				continue
			}
			clientID := h.effectiveClientID(ctx)
			claims := &auth.Claims{ClientID: clientID}
			if clientID != "" {
				claims.Username = clientID
			}
			var expiresAt time.Time
			if cached != nil {
				expiresAt = cached.ExpiresAt
			} else if primaryCached := h.lookupCachedToken(ctx, auth.FlowClientCredentials, scope, 0); primaryCached != nil {
				expiresAt = primaryCached.ExpiresAt
			}
			h.setActiveFlow(ctx, auth.FlowClientCredentials)
			return &auth.Status{
				Authenticated: true,
				Flow:          auth.FlowClientCredentials,
				IdentityType:  auth.IdentityTypeServicePrincipal,
				ClientID:      clientID,
				Claims:        claims,
				Scopes:        h.cfg.Scopes,
				ExpiresAt:     expiresAt,
			}

		case auth.FlowPAT:
			if h.cfg.Token == "" {
				continue
			}
			tok, err := h.resolvePATToken(ctx)
			if err != nil || tok == "" {
				continue
			}
			username, uErr := h.resolvePATUsername(ctx)
			if uErr != nil {
				continue
			}
			h.setActivePAT(ctx, username)
			claims := &auth.Claims{Username: username}
			return &auth.Status{
				Authenticated: true,
				Flow:          auth.FlowPAT,
				IdentityType:  auth.IdentityTypeUser,
				Claims:        claims,
				Scopes:        h.cfg.Scopes,
			}
		}
	}
	return nil
}

// GetToken returns a valid access token, refreshing or acquiring via configured
// non-interactive pipeline credentials (workload_identity -> client_credentials
// -> pat) when no interactive session is stored.
// If tokenExchange is configured (on non-PAT flows), returns the derived token.
func (h *Handler) GetToken(ctx context.Context, opts auth.TokenOptions) (*auth.Token, error) {
	tok, err := h.getTokenOnce(ctx, opts)
	if err != nil {
		var replErr *sessionReplacedError
		if errors.As(err, &replErr) && h.secretStore != nil {
			resolvedCtx := h.resolveContextProfile(ctx)
			profile := auth.ProfileFromContext(resolvedCtx)
			var shouldRetry bool
			if unlockCommit, lockErr := h.acquireSessionCommitLock(resolvedCtx, profile); lockErr == nil {
				pubMu := h.profilePublishLock(profile)
				pubMu.Lock()
				if state, stateErr := h.getSessionStateInternal(resolvedCtx, true); stateErr == nil && state != sessionStateLoggedOut && !h.isLocallyLoggedOut(profile) {
					if meta, metaErr := h.loadMetadata(resolvedCtx); metaErr == nil && meta != nil && meta.LastLoginFlow != "" && meta.LastLoginFlow == replErr.flow {
						shouldRetry = true
					}
				}
				pubMu.Unlock()
				unlockCommit()
			}
			if shouldRetry {
				return h.getTokenOnce(ctx, opts)
			}
		}
		return nil, err
	}
	return tok, nil
}

func (h *Handler) getTokenOnce(ctx context.Context, opts auth.TokenOptions) (*auth.Token, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	ctx = h.resolveContextProfile(ctx)
	profile := auth.ProfileFromContext(ctx)
	snap, snapErr := h.captureSessionSnapshot(ctx, profile)
	if snapErr != nil {
		return nil, auth.NewError(h.cfg.Name, "session_version", snapErr)
	}

	// Enforce logout and fail closed on unreadable session_state before serving
	// any cached, derived, refreshed, or ambient credential.
	state, stateErr := h.getSessionState(ctx)
	if stateErr != nil {
		return nil, auth.NewError(h.cfg.Name, "session_state", stateErr)
	}
	if state == sessionStateLoggedOut {
		return nil, auth.NewError(h.cfg.Name, "get_token", auth.ErrNotAuthenticated)
	}

	h.ensureLoadedDynamicClient(ctx)

	scope := opts.Scope
	if scope == "" {
		scope = strings.Join(h.cfg.Scopes, " ")
	}

	// 1. Check stored login session when secretStore is available.
	if h.secretStore != nil {
		meta, metaErr := h.loadMetadata(ctx)
		if metaErr != nil && !errors.Is(metaErr, secrets.ErrNotFound) {
			return nil, auth.NewError(h.cfg.Name, "load_metadata", metaErr)
		}

		if meta != nil {
			sessionFlow := meta.LastLoginFlow
			if sessionFlow == "" {
				sessionFlow = h.resolveDefaultFlow()
			}

			// Stored PAT session: read pat_token directly (never stored in TokenCache),
			// validating that the persisted PAT configuration fingerprint still matches.
			if sessionFlow == auth.FlowPAT {
				patTok, storedUser, patOK, patErr := h.loadValidStoredPAT(ctx)
				if patErr != nil {
					return nil, auth.NewError(h.cfg.Name, "pat_token", patErr)
				}
				if patOK {
					if !opts.ForceRefresh {
						if activeErr := h.ensureSessionActive(ctx, profile, snap, auth.FlowPAT); activeErr != nil {
							return nil, activeErr
						}
						h.restoreStoredPATUsername(ctx, meta, storedUser)
						return &auth.Token{
							AccessToken: patTok,
							TokenType:   defaultTokenType,
							ExpiresAt:   time.Time{},
							Scope:       scope,
							Flow:        auth.FlowPAT,
						}, nil
					}
					if h.cfg.Token != "" {
						tok, err := h.acquirePATToken(ctx, scope, true, snap)
						if err != nil {
							return nil, err
						}
						if activeErr := h.ensureSessionActive(ctx, profile, snap, auth.FlowPAT); activeErr != nil {
							return nil, activeErr
						}
						return tok, nil
					}
				}
			}

			// Stored derived or primary token shortcuts apply strictly to an active
			// stored login session (meta != nil). Ambient headless credentials omit
			// metadata and go through acquireHeadlessToken so nonInteractiveFlowOrder
			// is always respected.
			if sessionFlow != auth.FlowPAT && !opts.ForceRefresh {
				if h.cfg.TokenExchange != nil {
					derived, err := h.loadDerivedTokenForFlow(ctx, sessionFlow)
					if err == nil && derived != nil && (derived.Scope == "" || derived.Scope == scope) && derived.IsValidFor(opts.MinValidFor) {
						if activeErr := h.ensureSessionActive(ctx, profile, snap, sessionFlow); activeErr != nil {
							return nil, activeErr
						}
						h.setActiveFlow(ctx, sessionFlow)
						return derived, nil
					}
				} else if cached := h.lookupCachedToken(ctx, sessionFlow, scope, opts.MinValidFor); cached != nil {
					if activeErr := h.ensureSessionActive(ctx, profile, snap, sessionFlow); activeErr != nil {
						return nil, activeErr
					}
					h.setActiveFlow(ctx, sessionFlow)
					return cached, nil
				}
			}
		}

		// Try stored OAuth refresh token if present and matching the current
		// issuer/client configuration fingerprint. If a matching refresh token is
		// stored for this session and refresh fails, fail closed rather than
		// silently minting a new headless token over a revoked/invalid session.
		_, storedRefreshFlow, hasRefresh, err := h.loadRefreshTokenWithFlow(ctx)
		if err != nil {
			return nil, auth.NewError(h.cfg.Name, "refresh_token", err)
		}
		if hasRefresh {
			sessionFlow := h.resolveDefaultFlow()
			if storedRefreshFlow != "" {
				sessionFlow = storedRefreshFlow
			} else if meta != nil && meta.LastLoginFlow != "" {
				sessionFlow = meta.LastLoginFlow
			}
			refreshed, refreshErr := h.refreshWithSingleflight(ctx, sessionFlow, scope, opts, snap)
			if refreshErr != nil {
				if errors.Is(refreshErr, auth.ErrNotAuthenticated) {
					return nil, refreshErr
				}
				return nil, auth.NewError(h.cfg.Name, "refresh_token", refreshErr)
			}
			if curState, curErr := h.getSessionState(ctx); curErr != nil {
				return nil, auth.NewError(h.cfg.Name, "session_state", curErr)
			} else if curState == sessionStateLoggedOut || h.isLocallyLoggedOut(profile) {
				return nil, auth.NewError(h.cfg.Name, "get_token", auth.ErrNotAuthenticated)
			}
			return refreshed, nil
		}
	}

	// 2. Zero-ceremony headless pipeline acquisition (works with or without secretStore).
	token, headlessErr := h.acquireHeadlessToken(ctx, scope, opts, snap)
	if token != nil {
		if activeErr := h.ensureSessionActive(ctx, profile, snap, token.Flow); activeErr != nil {
			return nil, activeErr
		}
		if curState, curErr := h.getSessionState(ctx); curErr == nil && curState == sessionStateLoginFailed {
			if unlockCommit, lockErr := h.acquireSessionCommitLock(ctx, profile); lockErr == nil {
				pubMu := h.profilePublishLock(profile)
				pubMu.Lock()
				if ok, verErr := h.matchesSessionSnapshotLocked(ctx, profile, snap); verErr == nil && ok {
					_ = h.clearSessionState(ctx)
				}
				pubMu.Unlock()
				unlockCommit()
			}
		}
		return token, nil
	}
	if headlessErr != nil {
		return nil, headlessErr
	}

	if h.secretStore == nil {
		return nil, h.ensureSecrets()
	}

	return nil, auth.NewError(h.cfg.Name, "get_token", auth.ErrNotAuthenticated)
}

func (h *Handler) refreshWithSingleflight(ctx context.Context, flow auth.Flow, scope string, opts auth.TokenOptions, snap sessionSnapshot) (*auth.Token, error) {
	profile := auth.ProfileFromContext(ctx)
	sfKey := fmt.Sprintf("refresh|%s|%s|min=%d|force=%t|gen=%d|ver=%s", profile, scope, opts.MinValidFor, opts.ForceRefresh, snap.gen, snap.persistedVer)
	return h.doSingleflightToken(ctx, sfKey, func(sfCtx context.Context) (*auth.Token, error) {
		// Serialize refresh-token consumption across goroutines, Handler instances
		// sharing a store, and separate CLI processes sharing a secrets directory so
		// concurrent callers never submit the same single-use refresh token.
		unlockRefresh, lockErr := h.acquireRefreshLock(sfCtx, profile)
		if lockErr != nil {
			return nil, lockErr
		}
		defer unlockRefresh()

		if state, stateErr := h.getSessionState(sfCtx); stateErr != nil {
			return nil, auth.NewError(h.cfg.Name, "session_state", stateErr)
		} else if state == sessionStateLoggedOut || h.currentSessionGen(profile) != snap.gen && h.isLocallyLoggedOut(profile) {
			return nil, auth.NewError(h.cfg.Name, "get_token", auth.ErrNotAuthenticated)
		}
		if dcrClient := h.reusableDynamicClient(sfCtx); dcrClient != nil {
			sfCtx = withSelectedDynamicClient(sfCtx, dcrClient)
		}

		// Reload the stored refresh envelope and its originating flow together
		// under the refresh lock BEFORE cache checks so any waiting request
		// observes flow/token replacements committed while it waited.
		storedEnv, hasRefresh, getErr := h.loadRefreshTokenEnvelope(sfCtx)
		if getErr != nil {
			return nil, getErr
		}
		if !hasRefresh {
			return nil, auth.ErrNotAuthenticated
		}
		activeFlow := flow
		if storedEnv.Flow != "" {
			activeFlow = storedEnv.Flow
		}
		// Refresh snap after acquiring refreshLock so a waiting caller whose
		// session switched flows while waiting validates against the active
		// session version that owns storedEnv, while still fencing any
		// Logout/Login occurring during refreshAccessToken I/O below!
		activeSnap, activeSnapErr := h.captureSessionSnapshot(sfCtx, profile)
		if activeSnapErr != nil {
			return nil, auth.NewError(h.cfg.Name, "session_version", activeSnapErr)
		}
		if h.isLocallyLoggedOut(profile) {
			return nil, auth.NewError(h.cfg.Name, "get_token", auth.ErrNotAuthenticated)
		}

		if !opts.ForceRefresh {
			if h.cfg.TokenExchange != nil {
				if derived, loadErr := h.loadDerivedTokenForFlow(sfCtx, activeFlow); loadErr == nil && derived != nil && (derived.Scope == "" || derived.Scope == scope) && derived.IsValidFor(opts.MinValidFor) {
					return derived, nil
				}
			} else if cached := h.lookupCachedToken(sfCtx, activeFlow, scope, opts.MinValidFor); cached != nil {
				return cached, nil
			}
		}

		tokenResp, refreshErr := h.refreshAccessToken(sfCtx, activeFlow, storedEnv.Value, scope)
		if refreshErr != nil {
			return nil, refreshErr
		}
		token := h.tokenRespToToken(tokenResp, activeFlow, scope)

		// Persist any rotated refresh_token immediately under the store-scoped
		// session commit lock + publish lock before running optional post-grant
		// token exchange, verifying that no Login/patLogin/Logout replaced or
		// cleared the session or refresh envelope while refreshAccessToken was in flight.
		unlockCommit, commitErr := h.acquireSessionCommitLock(sfCtx, profile)
		if commitErr != nil {
			return nil, commitErr
		}
		pubMu := h.profilePublishLock(profile)
		pubMu.Lock()
		if activeErr := h.ensureSessionActiveLocked(sfCtx, profile, activeSnap, activeFlow); activeErr != nil {
			pubMu.Unlock()
			unlockCommit()
			return nil, activeErr
		}
		currentEnv, stillHas, checkErr := h.loadRefreshTokenEnvelope(sfCtx)
		if checkErr != nil {
			pubMu.Unlock()
			unlockCommit()
			return nil, checkErr
		}
		if !stillHas || !sameRefreshTokenEnvelope(currentEnv, storedEnv) {
			pubMu.Unlock()
			unlockCommit()
			return nil, auth.NewError(h.cfg.Name, "refresh_token", auth.ErrNotAuthenticated)
		}
		expectedEnv := storedEnv
		if tokenResp.RefreshToken != "" {
			newEnv, setErr := h.storeRefreshTokenEnvelope(sfCtx, activeFlow, tokenResp.RefreshToken)
			if setErr != nil {
				pubMu.Unlock()
				unlockCommit()
				return nil, fmt.Errorf("persist rotated refresh token: %w", setErr)
			}
			expectedEnv = newEnv
		}
		if h.cfg.TokenExchange == nil {
			h.saveCachedToken(sfCtx, activeFlow, scope, token)
			h.setActiveFlow(sfCtx, activeFlow)
			pubMu.Unlock()
			unlockCommit()
			return token, nil
		}
		pubMu.Unlock()
		unlockCommit()

		derived, exchangeErr := h.executeTokenExchange(sfCtx, tokenResp.AccessToken)
		if exchangeErr != nil {
			return nil, auth.NewError(h.cfg.Name, "token_exchange", exchangeErr)
		}

		unlockCommit2, commitErr2 := h.acquireSessionCommitLock(sfCtx, profile)
		if commitErr2 != nil {
			return nil, commitErr2
		}
		pubMu.Lock()
		if activeErr := h.ensureSessionActiveLocked(sfCtx, profile, activeSnap, activeFlow); activeErr != nil {
			pubMu.Unlock()
			unlockCommit2()
			return nil, activeErr
		}
		currentEnv2, stillHas2, checkErr2 := h.loadRefreshTokenEnvelope(sfCtx)
		if checkErr2 != nil {
			pubMu.Unlock()
			unlockCommit2()
			return nil, checkErr2
		}
		if !stillHas2 || !sameRefreshTokenEnvelope(currentEnv2, expectedEnv) {
			pubMu.Unlock()
			unlockCommit2()
			return nil, auth.NewError(h.cfg.Name, "refresh_token", auth.ErrNotAuthenticated)
		}
		h.saveCachedToken(sfCtx, activeFlow, scope, token)
		h.setActiveFlow(sfCtx, activeFlow)
		_ = h.storeDerivedToken(sfCtx, derived, token.ExpiresAt, activeFlow, scopeSliceFromString(scope))
		pubMu.Unlock()
		unlockCommit2()

		return &auth.Token{
			AccessToken: derived.Token,
			TokenType:   defaultTokenType,
			ExpiresAt:   token.ExpiresAt,
			Scope:       scope,
			Flow:        activeFlow,
		}, nil
	})
}

func (h *Handler) acquireHeadlessToken(ctx context.Context, scope string, opts auth.TokenOptions, snap sessionSnapshot) (*auth.Token, error) {
	profile := auth.ProfileFromContext(ctx)
	var lastCredentialErr error

	for _, flow := range h.nonInteractiveFlowOrder() {
		switch flow { //nolint:exhaustive // only headless non-interactive flows are tried
		case auth.FlowWorkloadIdentity:
			if !h.hasWorkloadIdentityConfig() {
				continue
			}
			// Check cached token before touching federated token file.
			if !opts.ForceRefresh {
				if cached := h.lookupEffectiveCachedToken(ctx, auth.FlowWorkloadIdentity, scope, opts.MinValidFor); cached != nil {
					if activeErr := h.ensureSessionActive(ctx, profile, snap, auth.FlowWorkloadIdentity); activeErr != nil {
						return nil, activeErr
					}
					h.setActiveFlow(ctx, auth.FlowWorkloadIdentity)
					return cached, nil
				}
			}
			// Probe federated token availability so an absent ambient file/env
			// can fall through cleanly to lower-priority configured flows.
			if _, resolveErr := h.resolveFederatedToken(ctx); resolveErr != nil {
				lastCredentialErr = resolveErr
				h.logger.V(1).Info("workload identity federated token unavailable", "error", resolveErr)
				continue
			}
			tok, err := h.mintHeadlessOAuthToken(ctx, auth.FlowWorkloadIdentity, scope, opts, snap)
			if err != nil {
				return nil, err
			}
			return tok, nil

		case auth.FlowClientCredentials:
			if !h.hasClientCredentialsConfig() {
				continue
			}
			if !opts.ForceRefresh {
				if cached := h.lookupEffectiveCachedToken(ctx, auth.FlowClientCredentials, scope, opts.MinValidFor); cached != nil {
					if activeErr := h.ensureSessionActive(ctx, profile, snap, auth.FlowClientCredentials); activeErr != nil {
						return nil, activeErr
					}
					h.setActiveFlow(ctx, auth.FlowClientCredentials)
					return cached, nil
				}
			}
			avail, reason := h.detectClientCredentialsAvailability(ctx)
			if !avail {
				dcrClientCredsAllowed := h.dcrEnabled() && (h.cfg.DefaultFlow == "client_credentials" || (h.cfg.AuthorizeURL == "" && h.cfg.DeviceAuthURL == ""))
				staticClientCredsAllowed := !h.dcrEnabled() && h.cfg.ClientSecret != "" && h.cfg.DefaultFlow != "interactive" && h.cfg.DefaultFlow != "device_code"
				if dcrClientCredsAllowed || staticClientCredsAllowed {
					lastCredentialErr = fmt.Errorf("%s", reason)
				}
				continue
			}
			tok, err := h.mintHeadlessOAuthToken(ctx, auth.FlowClientCredentials, scope, opts, snap)
			if err != nil {
				return nil, err
			}
			return tok, nil

		case auth.FlowPAT:
			if h.cfg.Token == "" {
				continue
			}
			tok, err := h.acquirePATToken(ctx, scope, opts.ForceRefresh, snap)
			if err != nil {
				if errors.Is(err, auth.ErrNotAuthenticated) {
					return nil, err
				}
				lastCredentialErr = err
				h.logger.V(1).Info("static PAT credential unavailable", "error", err)
				continue
			}
			return tok, nil
		}
	}

	if lastCredentialErr != nil {
		return nil, auth.NewError(h.cfg.Name, "get_token", fmt.Errorf("%w: %w", auth.ErrNotAuthenticated, lastCredentialErr))
	}
	return nil, nil
}

func (h *Handler) mintHeadlessOAuthToken(ctx context.Context, flow auth.Flow, scope string, opts auth.TokenOptions, snap sessionSnapshot) (*auth.Token, error) {
	profile := auth.ProfileFromContext(ctx)
	sfKey := fmt.Sprintf("mint|%s|%s|%s|min=%d|force=%t|gen=%d|ver=%s", profile, flow, scope, opts.MinValidFor, opts.ForceRefresh, snap.gen, snap.persistedVer)
	return h.doSingleflightToken(ctx, sfKey, func(sfCtx context.Context) (*auth.Token, error) {
		if activeErr := h.ensureSessionActive(sfCtx, profile, snap, flow); activeErr != nil {
			return nil, activeErr
		}

		dcrClient, dcrErr := h.ensureDynamicClient(sfCtx, snap, flow)
		if dcrErr != nil {
			return nil, dcrErr
		}
		sfCtx = withSelectedDynamicClient(sfCtx, dcrClient)

		if !opts.ForceRefresh {
			if cached := h.lookupEffectiveCachedToken(sfCtx, flow, scope, opts.MinValidFor); cached != nil {
				if activeErr := h.ensureSessionActive(sfCtx, profile, snap, flow); activeErr != nil {
					return nil, activeErr
				}
				h.setActiveFlow(sfCtx, flow)
				return cached, nil
			}
		}

		scopes := scopeSliceFromString(scope)
		var (
			tokenResp *tokenResponse
			err       error
		)
		switch flow { //nolint:exhaustive // only WIF and client_credentials mint OAuth tokens headlessly
		case auth.FlowWorkloadIdentity:
			tokenResp, err = h.workloadIdentityLogin(sfCtx, scopes)
		case auth.FlowClientCredentials:
			tokenResp, err = h.clientCredentialsLogin(sfCtx, scopes)
		default:
			return nil, auth.NewError(h.cfg.Name, "get_token", fmt.Errorf("%w: %s", auth.ErrFlowNotSupported, flow))
		}
		if err != nil {
			return nil, auth.NewError(h.cfg.Name, "get_token", err)
		}

		primary := h.tokenRespToToken(tokenResp, flow, scope)
		var derived *exchangeResult
		if h.cfg.TokenExchange != nil {
			var exchangeErr error
			derived, exchangeErr = h.executeTokenExchange(sfCtx, tokenResp.AccessToken)
			if exchangeErr != nil {
				return nil, auth.NewError(h.cfg.Name, "token_exchange", exchangeErr)
			}
		}

		unlockCommit, commitErr := h.acquireSessionCommitLock(sfCtx, profile)
		if commitErr != nil {
			return nil, commitErr
		}
		pubMu := h.profilePublishLock(profile)
		pubMu.Lock()
		if activeErr := h.ensureSessionActiveLocked(sfCtx, profile, snap, flow); activeErr != nil {
			pubMu.Unlock()
			unlockCommit()
			return nil, activeErr
		}
		h.saveCachedToken(sfCtx, flow, scope, primary)
		h.setActiveFlow(sfCtx, flow)
		if derived != nil {
			_ = h.storeDerivedToken(sfCtx, derived, primary.ExpiresAt, flow, scopes)
			pubMu.Unlock()
			unlockCommit()
			return &auth.Token{
				AccessToken: derived.Token,
				TokenType:   defaultTokenType,
				ExpiresAt:   primary.ExpiresAt,
				Scope:       scope,
				Flow:        flow,
			}, nil
		}
		pubMu.Unlock()
		unlockCommit()

		return primary, nil
	})
}

func (h *Handler) acquirePATToken(ctx context.Context, scope string, updateStored bool, snap sessionSnapshot) (*auth.Token, error) {
	tok, err := h.resolvePATToken(ctx)
	if err != nil {
		return nil, err
	}
	username, err := h.resolvePATUsername(ctx)
	if err != nil {
		return nil, auth.NewError(h.cfg.Name, "resolve_username", err)
	}

	var verifiedClaims *auth.Claims
	if updateStored && h.secretStore != nil && h.cfg.VerifyURL != "" {
		if existing, ok, _ := h.loadValidStoredPATSession(ctx); !ok || existing.Value != tok || existing.Username != username {
			if vc, verifyErr := h.verifyToken(ctx, tok); verifyErr == nil && vc != nil {
				verifiedClaims = vc
			} else if verifyErr != nil {
				h.logger.V(1).Info("PAT verification failed during refresh, falling back to resolved username", "error", verifyErr)
			}
		}
	}

	profile := auth.ProfileFromContext(ctx)
	unlockCommit, commitErr := h.acquireSessionCommitLock(ctx, profile)
	if commitErr != nil {
		return nil, commitErr
	}
	defer unlockCommit()

	pubMu := h.profilePublishLock(profile)
	pubMu.Lock()
	defer pubMu.Unlock()

	if activeErr := h.ensureSessionActiveLocked(ctx, profile, snap, auth.FlowPAT); activeErr != nil {
		return nil, activeErr
	}
	if updateStored && h.secretStore != nil {
		var claimsToStore *auth.Claims
		var scopesToStore []string
		existing, ok, _ := h.loadValidStoredPATSession(ctx)
		if ok {
			scopesToStore = existing.Scopes
		}
		if scopesToStore == nil && scope != "" {
			scopesToStore = scopeSliceFromString(scope)
		}
		if ok && existing.Value == tok && existing.Username == username {
			claimsToStore = existing.Claims
		} else {
			if verifiedClaims != nil {
				cp := *verifiedClaims
				if cp.Username == "" && username != "" {
					cp.Username = username
				}
				claimsToStore = &cp
			} else {
				claimsToStore = &auth.Claims{Username: username}
			}
		}
		if storeErr := h.storePATSessionSecrets(ctx, tok, username, claimsToStore, scopesToStore); storeErr != nil {
			return nil, auth.NewError(h.cfg.Name, "store_pat", storeErr)
		}
		if meta, metaErr := h.loadMetadata(ctx); metaErr == nil && meta != nil && meta.LastLoginFlow == auth.FlowPAT {
			meta.Claims = claimsToStore
			if metaBytes, marshalErr := json.Marshal(meta); marshalErr == nil {
				_ = h.secretStore.Set(ctx, h.profileSecretKey(ctx, secretKeyMetadataSuffix), metaBytes)
			}
		}
	}
	h.setActivePAT(ctx, username)
	return &auth.Token{
		AccessToken: tok,
		TokenType:   defaultTokenType,
		ExpiresAt:   time.Time{},
		Scope:       scope,
		Flow:        auth.FlowPAT,
	}, nil
}

func (h *Handler) doSingleflightToken(ctx context.Context, key string, fn func(context.Context) (*auth.Token, error)) (*auth.Token, error) {
	ch := h.sfGroup.DoChan(key, func() (any, error) {
		sfCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), defaultTimeout)
		defer cancel()
		return fn(sfCtx)
	})
	select {
	case res := <-ch:
		if res.Err != nil {
			return nil, res.Err
		}
		tok, _ := res.Val.(*auth.Token)
		if tok == nil {
			return nil, nil
		}
		cp := *tok
		return &cp, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// InjectAuth adds an Authorization header to the HTTP request.
func (h *Handler) InjectAuth(ctx context.Context, req *http.Request, opts auth.TokenOptions) error {
	token, err := h.GetToken(ctx, opts)
	if err != nil {
		return err
	}
	tokenType := token.TokenType
	if tokenType == "" {
		tokenType = defaultTokenType
	}
	req.Header.Set("Authorization", fmt.Sprintf("%s %s", tokenType, token.AccessToken))
	return nil
}

// ---------- internal types ----------

type tokenResponse struct {
	AccessToken  string `json:"access_token"` //nolint:gosec // JSON field
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"` //nolint:gosec // JSON field
	Scope        string `json:"scope"`
}

type exchangeResult struct {
	Token    string `json:"token"`
	Username string `json:"username,omitempty"`
}

type tokenEndpointError struct {
	ErrorCode   string `json:"error"`
	Description string `json:"error_description"`
}

func (e *tokenEndpointError) Error() string {
	if e.Description != "" {
		return fmt.Sprintf("%s: %s", e.ErrorCode, e.Description)
	}
	return e.ErrorCode
}

func (h *Handler) effectiveBinaryName() string {
	if strings.TrimSpace(h.binaryName) == "" {
		return settings.CliBinaryName
	}
	return h.binaryName
}

func (h *Handler) reservedBinaryNames() []string {
	names := []string{h.effectiveBinaryName()}
	if trimmed := strings.TrimSuffix(h.secretKeyPrefix, "."); trimmed != "" {
		if idx := strings.IndexByte(trimmed, '.'); idx > 0 {
			names = append(names, trimmed[:idx])
		} else {
			names = append(names, trimmed)
		}
	}
	return names
}

func (h *Handler) resolveContextProfile(ctx context.Context) context.Context {
	if p := auth.ProfileFromContext(ctx); p != "" {
		return auth.WithProfileResolved(auth.WithProfile(ctx, auth.NormalizeProfileName(p)))
	}
	if !auth.IsProfileResolved(ctx) {
		if profile := auth.ResolveActiveProfile(ctx, h.cfg.Name); profile != "" {
			return auth.WithProfileResolved(auth.WithProfile(ctx, auth.NormalizeProfileName(profile)))
		}
	}
	return ctx
}

func (h *Handler) currentSessionGen(profile string) uint64 {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.sessionGen == nil {
		return 0
	}
	return h.sessionGen[profile]
}

func (h *Handler) profilePublishLock(profile string) *sync.Mutex {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.publishLocks == nil {
		h.publishLocks = make(map[string]*sync.Mutex)
	}
	m, ok := h.publishLocks[profile]
	if !ok {
		m = &sync.Mutex{}
		h.publishLocks[profile] = m
	}
	return m
}

func (h *Handler) isLocallyLoggedOut(profile string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.sessionStates != nil && h.sessionStates[profile] == sessionStateLoggedOut
}

// ensureSessionActive verifies that no Logout or session replacement has
// occurred since snap was captured and that persisted session_state is readable
// and not logged_out.
func (h *Handler) ensureSessionActive(ctx context.Context, profile string, snap sessionSnapshot, flow auth.Flow) error {
	return h.ensureSessionActiveInternal(ctx, profile, snap, flow, false)
}

func (h *Handler) ensureSessionActiveLocked(ctx context.Context, profile string, snap sessionSnapshot, flow auth.Flow) error {
	return h.ensureSessionActiveInternal(ctx, profile, snap, flow, true)
}

func (h *Handler) ensureSessionActiveInternal(ctx context.Context, profile string, snap sessionSnapshot, flow auth.Flow, pubLocked bool) error {
	state, err := h.getSessionStateInternal(ctx, pubLocked)
	if err != nil {
		return auth.NewError(h.cfg.Name, "session_state", err)
	}
	if state == sessionStateLoggedOut || h.isLocallyLoggedOut(profile) {
		return auth.NewError(h.cfg.Name, "get_token", auth.ErrNotAuthenticated)
	}
	ok, verErr := h.matchesSessionSnapshotLocked(ctx, profile, snap)
	if verErr != nil {
		return auth.NewError(h.cfg.Name, "session_version", verErr)
	}
	if !ok {
		return auth.NewError(h.cfg.Name, "get_token", &sessionReplacedError{flow: flow})
	}
	return nil
}

func (h *Handler) cacheCorruptSessionState(profile string, seq uint64, pubLocked bool) {
	if !pubLocked {
		pubMu := h.profilePublishLock(profile)
		pubMu.Lock()
		defer pubMu.Unlock()
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.sessionStateSeq != nil && h.sessionStateSeq[profile] != seq {
		return
	}
	if h.sessionStates != nil && h.sessionStates[profile] == sessionStateLoggedOut {
		return
	}
	if h.sessionStates == nil {
		h.sessionStates = make(map[string]string)
	}
	h.sessionStates[profile] = sessionStateCorrupt
}

func (h *Handler) markSessionStateCorrupt(ctx context.Context, profile string, seq uint64, pubLocked bool) {
	if !pubLocked {
		pubMu := h.profilePublishLock(profile)
		pubMu.Lock()
		defer pubMu.Unlock()
	}
	h.mu.Lock()
	if h.sessionStateSeq != nil && h.sessionStateSeq[profile] != seq {
		h.mu.Unlock()
		return
	}
	if h.sessionStates != nil && h.sessionStates[profile] == sessionStateLoggedOut {
		h.mu.Unlock()
		// Restore the persistent logged_out marker if the secret store auto-deleted
		// a corrupted session_state file so a subsequent handler instance cannot
		// silently reactivate ambient credentials.
		if h.secretStore != nil {
			_ = h.secretStore.Set(context.WithoutCancel(ctx), h.profileSecretKey(ctx, secretKeySessionStateSuffix), []byte(sessionStateLoggedOut))
		}
		return
	}
	if h.sessionStates == nil {
		h.sessionStates = make(map[string]string)
	}
	h.sessionStates[profile] = sessionStateCorrupt
	h.mu.Unlock()
	if h.secretStore != nil {
		_ = h.secretStore.Set(context.WithoutCancel(ctx), h.profileSecretKey(ctx, secretKeySessionStateSuffix), []byte(sessionStateCorrupt))
	}
}

func (h *Handler) getSessionState(ctx context.Context) (string, error) {
	return h.getSessionStateInternal(ctx, false)
}

func (h *Handler) getSessionStateInternal(ctx context.Context, pubLocked bool) (string, error) {
	profile := auth.ProfileFromContext(ctx)
	h.mu.RLock()
	var (
		localState string
		seq        uint64
	)
	if h.sessionStates != nil {
		localState = h.sessionStates[profile]
	}
	if h.sessionStateSeq != nil {
		seq = h.sessionStateSeq[profile]
	}
	h.mu.RUnlock()

	if localState == sessionStateCorrupt {
		return "", fmt.Errorf("session state is corrupt")
	}

	if h.secretStore != nil {
		data, err := h.secretStore.Get(ctx, h.profileSecretKey(ctx, secretKeySessionStateSuffix))
		if err != nil {
			if errors.Is(err, secrets.ErrNotFound) {
				return localState, nil
			}
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return "", err
			}
			h.markSessionStateCorrupt(ctx, profile, seq, pubLocked)
			return "", fmt.Errorf("read session state: %w", err)
		}
		persisted := strings.TrimSpace(string(data))
		switch persisted {
		case sessionStateLoggedOut:
			return sessionStateLoggedOut, nil
		case sessionStateLoginFailed:
			if localState == sessionStateLoggedOut {
				return sessionStateLoggedOut, nil
			}
			return sessionStateLoginFailed, nil
		case sessionStateCorrupt:
			if localState == sessionStateLoggedOut {
				return sessionStateLoggedOut, nil
			}
			h.cacheCorruptSessionState(profile, seq, pubLocked)
			return "", fmt.Errorf("session state was corrupted; re-authenticate or logout to reset")
		default:
			h.markSessionStateCorrupt(ctx, profile, seq, pubLocked)
			return "", fmt.Errorf("session state is empty or corrupt")
		}
	}
	return localState, nil
}

func (h *Handler) clearSessionState(ctx context.Context) error {
	profile := auth.ProfileFromContext(ctx)
	h.mu.Lock()
	if h.sessionStates != nil {
		delete(h.sessionStates, profile)
	}
	if h.sessionStateSeq == nil {
		h.sessionStateSeq = make(map[string]uint64)
	}
	h.sessionStateSeq[profile]++
	h.mu.Unlock()
	if h.secretStore != nil {
		if err := h.secretStore.Delete(ctx, h.profileSecretKey(ctx, secretKeySessionStateSuffix)); err != nil && !errors.Is(err, secrets.ErrNotFound) {
			return fmt.Errorf("clear session state: %w", err)
		}
	}
	return nil
}

func (h *Handler) recordFailedLogin(ctx context.Context, snap sessionSnapshot) {
	detachedCtx := context.WithoutCancel(ctx)
	profile := auth.ProfileFromContext(detachedCtx)
	unlockCommit, commitErr := h.acquireSessionCommitLock(detachedCtx, profile)
	if commitErr != nil {
		return
	}
	defer unlockCommit()

	pubMu := h.profilePublishLock(profile)
	pubMu.Lock()
	defer pubMu.Unlock()

	if ok, verErr := h.matchesSessionSnapshotLocked(detachedCtx, profile, snap); verErr != nil || !ok {
		return
	}

	h.mu.RLock()
	if h.sessionStates != nil && (h.sessionStates[profile] == sessionStateLoggedOut || h.sessionStates[profile] == sessionStateCorrupt) {
		h.mu.RUnlock()
		return
	}
	h.mu.RUnlock()

	if h.secretStore != nil {
		if state, err := h.getSessionStateInternal(detachedCtx, true); err != nil || state == sessionStateLoggedOut {
			return
		}
		if meta, err := h.loadMetadata(detachedCtx); err == nil && meta != nil {
			if meta.LastLoginFlow == auth.FlowPAT {
				if _, _, patOK, patErr := h.loadValidStoredPAT(detachedCtx); patErr == nil && patOK && meta.ExpiresAt.IsZero() {
					return
				}
			} else if !meta.ExpiresAt.IsZero() && !meta.ExpiresAt.Before(time.Now()) {
				return
			}
		}
		_ = h.secretStore.Set(detachedCtx, h.profileSecretKey(detachedCtx, secretKeySessionStateSuffix), []byte(sessionStateLoginFailed))
	}
	h.mu.Lock()
	if h.sessionStates == nil {
		h.sessionStates = make(map[string]string)
	}
	h.sessionStates[profile] = sessionStateLoginFailed
	if h.sessionStateSeq == nil {
		h.sessionStateSeq = make(map[string]uint64)
	}
	h.sessionStateSeq[profile]++
	h.mu.Unlock()
}

func (h *Handler) effectiveClientID(ctx context.Context) string {
	if selected := selectedDynamicClientFromContext(ctx); selected != nil && selected.ClientID != "" {
		return selected.ClientID
	}
	h.ensureLoadedDynamicClient(ctx)
	profile := auth.ProfileFromContext(ctx)
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.dcrClients != nil {
		if dcr := h.dcrClients[profile]; dcr != nil && dcr.ClientID != "" {
			return dcr.ClientID
		}
	}
	return h.cfg.ClientID
}

func (h *Handler) resolveClientCredentials(ctx context.Context) (string, string, error) {
	if selected := selectedDynamicClientFromContext(ctx); selected != nil && selected.ClientID != "" {
		return selected.ClientID, selected.ClientSecret, nil
	}
	h.ensureLoadedDynamicClient(ctx)
	profile := auth.ProfileFromContext(ctx)
	h.mu.RLock()
	var dcr *dynamicClient
	if h.dcrClients != nil {
		dcr = h.dcrClients[profile]
	}
	cfgID := h.cfg.ClientID
	cfgSecret := h.cfg.ClientSecret
	h.mu.RUnlock()

	if dcr != nil && dcr.ClientID != "" {
		// Treat DCR-returned client_secret strictly as a literal string so a
		// registration endpoint response can never trigger local SecretRef reads.
		return dcr.ClientID, dcr.ClientSecret, nil
	}
	if cfgSecret == "" {
		return cfgID, "", nil
	}
	resolved, err := config.ResolveCredentialValue(ctx, cfgSecret, h.secretStore, h.reservedBinaryNames()...)
	if err != nil {
		return "", "", fmt.Errorf("resolve clientSecret: %w", err)
	}
	return cfgID, resolved, nil
}

func (h *Handler) resolveFederatedToken(ctx context.Context) (string, error) {
	var (
		raw string
		err error
	)
	switch {
	case h.cfg.FederatedToken != "":
		raw, err = h.cfg.FederatedToken.ResolveWithStore(ctx, h.secretStore, h.reservedBinaryNames()...)
	case h.cfg.FederatedTokenFile != "":
		raw, err = config.SecretRef("file://" + h.cfg.FederatedTokenFile).Resolve()
	default:
		return "", fmt.Errorf("federatedToken or federatedTokenFile is required for workload_identity flow")
	}
	if err != nil {
		return "", fmt.Errorf("resolve federated token: %w", err)
	}
	jwt := strings.TrimSpace(raw)
	if jwt == "" {
		return "", fmt.Errorf("federated token is empty")
	}
	return jwt, nil
}

func (h *Handler) resolvePATToken(ctx context.Context) (string, error) {
	if h.cfg.Token == "" {
		return "", fmt.Errorf("token is required for pat flow")
	}
	raw, err := h.cfg.Token.ResolveWithStore(ctx, h.secretStore, h.reservedBinaryNames()...)
	if err != nil {
		return "", fmt.Errorf("resolve token: %w", err)
	}
	tok := strings.TrimSpace(raw)
	if tok == "" {
		return "", fmt.Errorf("resolved token is empty")
	}
	return tok, nil
}

func (h *Handler) resolvePATUsername(ctx context.Context) (string, error) {
	if h.cfg.Username == "" {
		return "", nil
	}
	raw, err := config.ResolveCredentialValue(ctx, h.cfg.Username, h.secretStore, h.reservedBinaryNames()...)
	if err != nil {
		return "", fmt.Errorf("resolve username: %w", err)
	}
	return strings.TrimSpace(raw), nil
}

func (h *Handler) setActiveFlow(ctx context.Context, flow auth.Flow) {
	profile := auth.ProfileFromContext(ctx)
	h.mu.Lock()
	if h.activeFlows == nil {
		h.activeFlows = make(map[string]auth.Flow)
	}
	h.activeFlows[profile] = flow
	if flow != auth.FlowPAT {
		if h.patUsernames != nil {
			delete(h.patUsernames, profile)
		}
	}
	h.mu.Unlock()
}

func (h *Handler) setActivePAT(ctx context.Context, username string) {
	profile := auth.ProfileFromContext(ctx)
	h.mu.Lock()
	if h.activeFlows == nil {
		h.activeFlows = make(map[string]auth.Flow)
	}
	if h.patUsernames == nil {
		h.patUsernames = make(map[string]string)
	}
	h.activeFlows[profile] = auth.FlowPAT
	h.patUsernames[profile] = username
	if h.derivedUsers != nil {
		delete(h.derivedUsers, profile)
	}
	h.mu.Unlock()
}

func (h *Handler) setDerivedUsername(ctx context.Context, username string) {
	profile := auth.ProfileFromContext(ctx)
	h.mu.Lock()
	if h.derivedUsers == nil {
		h.derivedUsers = make(map[string]string)
	}
	if username == "" {
		delete(h.derivedUsers, profile)
	} else {
		h.derivedUsers[profile] = username
	}
	h.mu.Unlock()
}

func (h *Handler) patFingerprint() string {
	return auth.FingerprintHash(strings.Join([]string{
		string(h.cfg.Token),
		h.cfg.Username,
		h.cfg.Registry,
		h.cfg.RegistryUsername,
		h.cfg.VerifyURL,
	}, "|"))
}

func (h *Handler) storePATSessionSecrets(ctx context.Context, patToken, username string, claims *auth.Claims, scopes []string) error {
	data, err := json.Marshal(persistedPATSession{
		Value:       patToken,
		Username:    username,
		Fingerprint: h.patFingerprint(),
		Claims:      claims,
		Scopes:      scopes,
	})
	if err != nil {
		return fmt.Errorf("marshal pat session: %w", err)
	}
	if err := h.secretStore.Set(ctx, h.profileSecretKey(ctx, secretKeyPATTokenSuffix), data); err != nil {
		return fmt.Errorf("store pat session: %w", err)
	}
	return nil
}

func (h *Handler) loadValidStoredPATSession(ctx context.Context) (*persistedPATSession, bool, error) {
	if h.secretStore == nil {
		return nil, false, nil
	}
	data, err := h.secretStore.Get(ctx, h.profileSecretKey(ctx, secretKeyPATTokenSuffix))
	if err != nil {
		if errors.Is(err, secrets.ErrNotFound) {
			return nil, false, nil
		}
		return nil, false, err
	}
	if len(data) == 0 {
		return nil, false, nil
	}
	var stored persistedPATSession
	if unmarshalErr := json.Unmarshal(data, &stored); unmarshalErr != nil {
		return nil, false, fmt.Errorf("unmarshal pat session: %w", unmarshalErr)
	}
	if stored.Value == "" || stored.Fingerprint == "" || stored.Fingerprint != h.patFingerprint() {
		return nil, false, nil
	}
	return &stored, true, nil
}

func (h *Handler) loadValidStoredPAT(ctx context.Context) (string, string, bool, error) {
	stored, ok, err := h.loadValidStoredPATSession(ctx)
	if err != nil || !ok {
		return "", "", false, err
	}
	return stored.Value, stored.Username, true, nil
}

func (h *Handler) restoreStoredPATUsername(ctx context.Context, meta *auth.HandlerMetadata, storedUsername string) {
	username := storedUsername
	if username == "" && h.cfg.Username != "" {
		if resolved, err := h.resolvePATUsername(ctx); err == nil && resolved != "" {
			username = resolved
		}
	}
	if username == "" && meta != nil && meta.Claims != nil {
		username = meta.Claims.Username
	}
	h.setActivePAT(ctx, username)
}

func (h *Handler) inferFlowForContext(ctx context.Context) auth.Flow {
	if h.secretStore != nil {
		if meta, err := h.loadMetadata(ctx); err == nil && meta != nil && meta.LastLoginFlow != "" {
			if meta.LastLoginFlow != auth.FlowPAT {
				return meta.LastLoginFlow
			}
			if _, _, patOK, patErr := h.loadValidStoredPAT(ctx); patErr == nil && patOK {
				return auth.FlowPAT
			}
		}
	}
	return h.resolveDefaultFlow()
}

// callbackOpts builds CallbackOptions from the handler's config and pre-renders
// the success/failure HTML pages for deferred completion.
func (h *Handler) callbackOpts(ctx context.Context) []oauth.CallbackOption {
	bundle := authpage.RenderBundle(ctx, h.pageRenderer, h.effectiveBinaryName())
	opts := []oauth.CallbackOption{
		oauth.WithCallbackPages(oauth.CallbackPages{
			SuccessHTML: bundle.SuccessHTML,
			FailureHTML: bundle.FailureHTML,
		}),
		oauth.WithDeferredCompletion(),
	}
	if h.cfg.CallbackHost != "" {
		opts = append(opts, oauth.WithCallbackHost(h.cfg.CallbackHost))
	}
	if h.cfg.CallbackPath != "" {
		opts = append(opts, oauth.WithCallbackPath(h.cfg.CallbackPath))
	}
	return opts
}

func (h *Handler) launchBrowser(ctx context.Context, rawURL, browser string) error {
	detachedCtx := context.WithoutCancel(ctx)
	if h.openBrowser != nil {
		return h.openBrowser(detachedCtx, rawURL)
	}
	return auth.OpenBrowser(detachedCtx, rawURL, browser)
}

func (h *Handler) notifyManualAuthURL(ctx context.Context, opts auth.LoginOptions, rawURL string) {
	if ctx.Err() != nil {
		return
	}
	if opts.DeviceCodeCallback != nil {
		opts.DeviceCodeCallback("", rawURL, "")
		return
	}
	if w := writer.FromContext(ctx); w != nil {
		w.Plainln("")
		w.Plainln("To sign in, use a web browser to open the page:")
		w.Plainln("  " + rawURL)
	}
}

// ---------- flow: authorization code + PKCE ----------

func (h *Handler) authCodeLogin(ctx context.Context, opts auth.LoginOptions, scopes []string, callbackPort int) (*tokenResponse, *oauth.CallbackServer, bool, error) {
	if h.cfg.AuthorizeURL == "" {
		return nil, nil, false, fmt.Errorf("authorizeURL is required for interactive flow")
	}

	// Implicit grant flow (response_type=token) uses a different callback mechanism.
	if h.cfg.ResponseType == "token" {
		return h.implicitGrantLogin(ctx, opts, scopes, callbackPort)
	}

	var verifier string
	if !h.cfg.DisablePKCE {
		var err error
		verifier, err = oauth.GenerateCodeVerifier()
		if err != nil {
			return nil, nil, false, fmt.Errorf("generate PKCE verifier: %w", err)
		}
	}

	state, err := oauth.GenerateCodeVerifier()
	if err != nil {
		return nil, nil, false, fmt.Errorf("generate state: %w", err)
	}

	callbackServer, err := oauth.StartCallbackServer(ctx, callbackPort, state, h.callbackOpts(ctx)...)
	if err != nil {
		return nil, nil, false, fmt.Errorf("start callback server: %w", err)
	}

	authURL, err := url.Parse(h.cfg.AuthorizeURL)
	if err != nil {
		_ = callbackServer.Close()
		return nil, nil, false, fmt.Errorf("parse authorizeURL: %w", err)
	}
	q := authURL.Query()
	q.Set("response_type", "code")
	q.Set("client_id", h.effectiveClientID(ctx))
	q.Set("redirect_uri", callbackServer.RedirectURI)
	q.Set("state", state)
	if !h.cfg.DisablePKCE {
		challenge := oauth.GenerateCodeChallenge(verifier)
		q.Set("code_challenge", challenge)
		q.Set("code_challenge_method", "S256")
	}
	if len(scopes) > 0 {
		q.Set("scope", strings.Join(scopes, " "))
	}
	authURL.RawQuery = q.Encode()

	if openErr := h.launchBrowser(ctx, authURL.String(), opts.Browser); openErr != nil {
		h.logger.V(1).Info("failed to open browser", "url", authURL.String(), "error", openErr)
		h.notifyManualAuthURL(ctx, opts, authURL.String())
	}

	select {
	case result := <-callbackServer.ResultChan():
		if result.Err != nil {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), callbackShutdownTimeout)
			defer cancel()
			_ = callbackServer.Shutdown(shutdownCtx)
			return nil, nil, false, fmt.Errorf("callback error: %w", result.Err)
		}
		tok, exErr := h.exchangeAuthCode(ctx, result.Code, callbackServer.RedirectURI, verifier, scopes)
		return tok, callbackServer, true, exErr
	case <-ctx.Done():
		return nil, callbackServer, false, auth.ErrTimeout
	}
}

func (h *Handler) exchangeAuthCode(ctx context.Context, code, redirectURI, codeVerifier string, scopes []string) (*tokenResponse, error) {
	clientID, clientSecret, err := h.resolveClientCredentials(ctx)
	if err != nil {
		return nil, err
	}
	data := url.Values{
		"grant_type":   {"authorization_code"},
		"code":         {code},
		"redirect_uri": {redirectURI},
		"client_id":    {clientID},
	}
	// codeVerifier is empty when h.cfg.DisablePKCE is true (set by authCodeLogin).
	if codeVerifier != "" {
		data.Set("code_verifier", codeVerifier)
	}
	if clientSecret != "" {
		data.Set("client_secret", clientSecret)
	}
	if len(scopes) > 0 {
		data.Set("scope", strings.Join(scopes, " "))
	}
	return h.postTokenEndpoint(ctx, data)
}

// ---------- flow: implicit grant (response_type=token) ----------

func (h *Handler) implicitGrantLogin(ctx context.Context, opts auth.LoginOptions, scopes []string, callbackPort int) (*tokenResponse, *oauth.CallbackServer, bool, error) {
	state, err := oauth.GenerateCodeVerifier()
	if err != nil {
		return nil, nil, false, fmt.Errorf("generate state: %w", err)
	}

	callbackServer, err := oauth.StartImplicitCallbackServer(ctx, callbackPort, state, h.callbackOpts(ctx)...)
	if err != nil {
		return nil, nil, false, fmt.Errorf("start callback server: %w", err)
	}

	authURL, err := url.Parse(h.cfg.AuthorizeURL)
	if err != nil {
		_ = callbackServer.Close()
		return nil, nil, false, fmt.Errorf("parse authorizeURL: %w", err)
	}
	q := authURL.Query()
	q.Set("response_type", "token")
	q.Set("client_id", h.effectiveClientID(ctx))
	q.Set("redirect_uri", callbackServer.RedirectURI)
	q.Set("state", state)
	if len(scopes) > 0 {
		q.Set("scope", strings.Join(scopes, " "))
	}
	authURL.RawQuery = q.Encode()

	if openErr := h.launchBrowser(ctx, authURL.String(), opts.Browser); openErr != nil {
		h.logger.V(1).Info("failed to open browser", "url", authURL.String(), "error", openErr)
		h.notifyManualAuthURL(ctx, opts, authURL.String())
	}

	select {
	case result := <-callbackServer.ResultChan():
		if result.Err != nil {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), callbackShutdownTimeout)
			defer cancel()
			_ = callbackServer.Shutdown(shutdownCtx)
			return nil, nil, false, fmt.Errorf("callback error: %w", result.Err)
		}
		resp := &tokenResponse{
			AccessToken: result.AccessToken,
			TokenType:   result.TokenType,
		}
		if resp.TokenType == "" {
			resp.TokenType = defaultTokenType
		}
		if result.ExpiresIn != "" {
			var expiresIn int
			if _, scanErr := fmt.Sscanf(result.ExpiresIn, "%d", &expiresIn); scanErr == nil {
				resp.ExpiresIn = expiresIn
			} else {
				h.logger.V(1).Info("failed to parse expires_in from implicit grant, defaulting to 3600", "value", result.ExpiresIn, "error", scanErr)
				resp.ExpiresIn = defaultImplicitExpiresIn
			}
		} else {
			h.logger.V(1).Info("expires_in not provided by implicit grant server, defaulting to 3600")
			resp.ExpiresIn = defaultImplicitExpiresIn
		}
		return resp, callbackServer, true, nil
	case <-ctx.Done():
		return nil, callbackServer, false, auth.ErrTimeout
	}
}

// ---------- flow: device code (RFC 8628) ----------

func (h *Handler) deviceCodeLogin(ctx context.Context, opts auth.LoginOptions, scopes []string) (*tokenResponse, error) {
	if h.cfg.DeviceAuthURL == "" {
		return nil, fmt.Errorf("deviceAuthURL is required for device_code flow")
	}

	data := url.Values{"client_id": {h.effectiveClientID(ctx)}}
	if len(scopes) > 0 {
		data.Set("scope", strings.Join(scopes, " "))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.cfg.DeviceAuthURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("create device auth request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := h.httpClient.Do(req) //nolint:gosec // URL from trusted admin config
	if err != nil {
		return nil, fmt.Errorf("device auth request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return nil, fmt.Errorf("read device auth response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("device auth failed (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var deviceResp struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURI string `json:"verification_uri"`
		ExpiresIn       int    `json:"expires_in"`
		Interval        int    `json:"interval"`
	}
	if err := json.Unmarshal(body, &deviceResp); err != nil {
		return nil, fmt.Errorf("parse device auth response: %w", err)
	}

	if opts.DeviceCodeCallback != nil {
		opts.DeviceCodeCallback(deviceResp.UserCode, deviceResp.VerificationURI, "")
	}

	interval := deviceResp.Interval
	if interval <= 0 {
		interval = defaultPollInterval
	}
	if h.cfg.DeviceCodePollInterval > 0 && deviceResp.Interval <= 0 {
		interval = h.cfg.DeviceCodePollInterval
	}

	return h.pollDeviceCode(ctx, deviceResp.DeviceCode, interval, scopes)
}

func (h *Handler) pollDeviceCode(ctx context.Context, deviceCode string, interval int, scopes []string) (*tokenResponse, error) {
	ticker := time.NewTicker(time.Duration(interval) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, auth.ErrTimeout
		case <-ticker.C:
			data := url.Values{
				"grant_type":  {deviceCodeGrantType},
				"device_code": {deviceCode},
				"client_id":   {h.effectiveClientID(ctx)},
			}
			if len(scopes) > 0 {
				data.Set("scope", strings.Join(scopes, " "))
			}

			tokenResp, err := h.postTokenEndpoint(ctx, data)
			if err == nil {
				return tokenResp, nil
			}

			var errMsg string
			if e, ok := err.(*tokenEndpointError); ok { //nolint:errorlint // concrete type assertion
				errMsg = e.ErrorCode
			}
			switch errMsg {
			case "authorization_pending":
				continue
			case "slow_down":
				interval++
				ticker.Reset(time.Duration(interval) * time.Second)
				continue
			case "expired_token":
				return nil, fmt.Errorf("device code expired — please try again")
			case "access_denied":
				return nil, fmt.Errorf("access denied by user")
			default:
				return nil, err
			}
		}
	}
}

// ---------- flow: client credentials ----------

func (h *Handler) clientCredentialsLogin(ctx context.Context, scopes []string) (*tokenResponse, error) {
	clientID, clientSecret, err := h.resolveClientCredentials(ctx)
	if err != nil {
		return nil, err
	}
	if clientSecret == "" {
		return nil, fmt.Errorf("clientSecret is required for client_credentials flow")
	}
	data := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {clientID},
		"client_secret": {clientSecret},
	}
	if len(scopes) > 0 {
		data.Set("scope", strings.Join(scopes, " "))
	}
	return h.postTokenEndpoint(ctx, data)
}

// ---------- flow: workload identity federation (RFC 7523 & RFC 8693) ----------

func (h *Handler) workloadIdentityLogin(ctx context.Context, scopes []string) (*tokenResponse, error) {
	jwt, err := h.resolveFederatedToken(ctx)
	if err != nil {
		return nil, err
	}

	grantType := h.cfg.WorkloadIdentityGrantType
	if grantType == "" {
		grantType = config.WorkloadIdentityGrantClientCredentials
	}

	switch grantType {
	case config.WorkloadIdentityGrantClientCredentials:
		clientID := h.effectiveClientID(ctx)
		if clientID == "" {
			return nil, fmt.Errorf("clientID is required for workload_identity flow with client_credentials grant")
		}
		data := url.Values{
			"grant_type":            {"client_credentials"},
			"client_id":             {clientID},
			"client_assertion_type": {jwtBearerAssertionType},
			"client_assertion":      {jwt},
		}
		if len(scopes) > 0 {
			data.Set("scope", strings.Join(scopes, " "))
		}
		if h.cfg.Audience != "" {
			data.Set("audience", h.cfg.Audience)
		}
		return h.postTokenEndpoint(ctx, data)

	case config.WorkloadIdentityGrantTokenExchange:
		clientID, clientSecret, credErr := h.resolveClientCredentials(ctx)
		if credErr != nil {
			return nil, credErr
		}
		subjectTokenType := h.cfg.SubjectTokenType
		if subjectTokenType == "" {
			subjectTokenType = config.DefaultSubjectTokenType
		}
		data := url.Values{
			"grant_type":         {tokenExchangeGrantType},
			"subject_token":      {jwt},
			"subject_token_type": {subjectTokenType},
		}
		var basicAuth *basicAuthCredentials
		if clientID != "" {
			if clientSecret != "" {
				basicAuth = &basicAuthCredentials{username: clientID, password: clientSecret}
			} else {
				data.Set("client_id", clientID)
			}
		}
		if len(scopes) > 0 {
			data.Set("scope", strings.Join(scopes, " "))
		}
		if h.cfg.Audience != "" {
			data.Set("audience", h.cfg.Audience)
		}
		return h.postTokenEndpointWithAuth(ctx, data, basicAuth)

	default:
		return nil, fmt.Errorf("unknown workloadIdentityGrantType %q (valid: %s, %s)",
			grantType, config.WorkloadIdentityGrantClientCredentials, config.WorkloadIdentityGrantTokenExchange)
	}
}

// ---------- token exchange ----------

func (h *Handler) executeTokenExchange(ctx context.Context, primaryToken string) (*exchangeResult, error) {
	exc := h.cfg.TokenExchange
	method := exc.Method
	if method == "" {
		method = http.MethodPost
	}

	regUsername := h.cfg.RegistryUsername

	var bodyReader io.Reader
	if exc.RequestBody != "" {
		result, tmplErr := gotmpl.Execute(ctx, gotmpl.TemplateOptions{
			Name:    "exchange-request-body",
			Content: string(exc.RequestBody),
			Data: map[string]string{
				"Hostname": h.cfg.Registry,
				"Username": regUsername,
			},
		})
		if tmplErr != nil {
			return nil, fmt.Errorf("render exchange request body: %w", tmplErr)
		}
		bodyReader = bytes.NewBufferString(result.Output)
	}

	req, err := http.NewRequestWithContext(ctx, method, exc.URL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create exchange request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+primaryToken)
	if bodyReader != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	resp, err := h.httpClient.Do(req) //nolint:gosec // URL from trusted admin config
	if err != nil {
		return nil, fmt.Errorf("exchange request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return nil, fmt.Errorf("read exchange response: %w", err)
	}

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("token exchange returned 401 — primary token may be expired, try re-authenticating")
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, fmt.Errorf("token exchange failed (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var respData map[string]any
	if err := json.Unmarshal(body, &respData); err != nil {
		return nil, fmt.Errorf("parse exchange response: %w", err)
	}

	token, err := extractJSONPath(respData, exc.TokenJSONPath)
	if err != nil {
		return nil, fmt.Errorf("extract token from exchange response: %w", err)
	}

	result := &exchangeResult{Token: token}
	if exc.UsernameJSONPath != "" {
		if username, extractErr := extractJSONPath(respData, exc.UsernameJSONPath); extractErr == nil && username != "" {
			result.Username = username
		}
	}
	return result, nil
}

// ---------- token verification ----------

func (h *Handler) verifyToken(ctx context.Context, accessToken string) (*auth.Claims, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.cfg.VerifyURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create verify request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")

	resp, err := h.httpClient.Do(req) //nolint:gosec // URL from trusted admin config
	if err != nil {
		return nil, fmt.Errorf("verify request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return nil, fmt.Errorf("read verify response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("verify returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("parse verify response: %w", err)
	}

	claims := &auth.Claims{}
	if h.cfg.IdentityFields != nil {
		if h.cfg.IdentityFields.Username != "" {
			claims.Username = getStringField(data, h.cfg.IdentityFields.Username)
		}
		if h.cfg.IdentityFields.Email != "" {
			claims.Email = getStringField(data, h.cfg.IdentityFields.Email)
		}
		if h.cfg.IdentityFields.Name != "" {
			claims.Name = getStringField(data, h.cfg.IdentityFields.Name)
		}
	}
	return claims, nil
}

func (h *Handler) fallbackClaims(ctx context.Context, accessToken string, flow auth.Flow) *auth.Claims {
	if jwtClaims, err := auth.ParseJWTClaims(accessToken); err == nil {
		return jwtClaims
	}
	clientID := h.effectiveClientID(ctx)
	if (flow == auth.FlowWorkloadIdentity || flow == auth.FlowClientCredentials) && clientID != "" {
		return &auth.Claims{ClientID: clientID, Username: clientID}
	}
	return &auth.Claims{}
}

// ---------- storage & caching ----------

func (h *Handler) cacheFingerprint(ctx context.Context, flow auth.Flow) string {
	clientID := h.effectiveClientID(ctx)
	if flow == auth.FlowWorkloadIdentity {
		grantType := h.cfg.WorkloadIdentityGrantType
		if grantType == "" {
			grantType = config.WorkloadIdentityGrantClientCredentials
		}
		subjectTokenType := h.cfg.SubjectTokenType
		if grantType == config.WorkloadIdentityGrantTokenExchange && subjectTokenType == "" {
			subjectTokenType = config.DefaultSubjectTokenType
		}
		return auth.FingerprintHash(strings.Join([]string{
			clientID,
			h.cfg.TokenURL,
			h.cfg.Audience,
			grantType,
			subjectTokenType,
			string(h.cfg.FederatedToken),
			h.cfg.FederatedTokenFile,
		}, "|"))
	}
	return auth.FingerprintHash(strings.Join([]string{clientID, h.cfg.TokenURL}, "|"))
}

func (h *Handler) derivedFingerprint(ctx context.Context, flow auth.Flow) string {
	base := h.cacheFingerprint(ctx, flow)
	if h.cfg.TokenExchange == nil {
		return base
	}
	exc := h.cfg.TokenExchange
	method := strings.ToUpper(exc.Method)
	if method == "" {
		method = http.MethodPost
	}
	return auth.FingerprintHash(strings.Join([]string{
		base,
		exc.URL,
		method,
		string(exc.RequestBody),
		exc.TokenJSONPath,
		exc.UsernameJSONPath,
		h.cfg.Registry,
		h.cfg.RegistryUsername,
	}, "|"))
}

func (h *Handler) memCacheKey(ctx context.Context, flow auth.Flow, scope string) string {
	return fmt.Sprintf("%s|%s|%s|%s", auth.ProfileFromContext(ctx), flow, h.cacheFingerprint(ctx, flow), scope)
}

func (h *Handler) memDerivedKey(ctx context.Context, flow auth.Flow, scope string) string {
	return fmt.Sprintf("%s|%s|%s|%s", auth.ProfileFromContext(ctx), flow, h.derivedFingerprint(ctx, flow), scope)
}

func (h *Handler) lookupCachedToken(ctx context.Context, flow auth.Flow, scope string, minValidFor time.Duration) *auth.Token {
	fingerprint := h.cacheFingerprint(ctx, flow)
	if tc := h.profileTokenCache(ctx); tc != nil {
		if cached, err := tc.Get(ctx, flow, fingerprint, scope); err == nil && cached != nil && cached.IsValidFor(minValidFor) {
			return cached
		}
		return nil
	}
	key := h.memCacheKey(ctx, flow, scope)
	h.mu.RLock()
	var cached *auth.Token
	if h.memCache != nil {
		cached = h.memCache[key]
	}
	h.mu.RUnlock()
	if cached != nil && cached.IsValidFor(minValidFor) {
		cp := *cached
		return &cp
	}
	return nil
}

func (h *Handler) lookupEffectiveCachedToken(ctx context.Context, flow auth.Flow, scope string, minValidFor time.Duration) *auth.Token {
	if h.cfg.TokenExchange != nil {
		if h.secretStore != nil {
			if derived, err := h.loadDerivedTokenForFlow(ctx, flow); err == nil && derived != nil && (derived.Scope == "" || derived.Scope == scope) && derived.IsValidFor(minValidFor) {
				return derived
			}
			return nil
		}
		key := h.memDerivedKey(ctx, flow, scope)
		expectedFP := h.derivedFingerprint(ctx, flow)
		h.mu.RLock()
		var derived *persistedDerivedToken
		if h.memDerived != nil {
			derived = h.memDerived[key]
		}
		h.mu.RUnlock()
		if derived != nil && derived.Fingerprint == expectedFP && derived.IsValidFor(minValidFor) {
			if derived.Username != "" {
				h.setDerivedUsername(ctx, derived.Username)
			}
			cp := derived.Token
			return &cp
		}
		return nil
	}
	return h.lookupCachedToken(ctx, flow, scope, minValidFor)
}

func (h *Handler) saveCachedToken(ctx context.Context, flow auth.Flow, scope string, token *auth.Token) {
	fingerprint := h.cacheFingerprint(ctx, flow)
	if tc := h.profileTokenCache(ctx); tc != nil {
		_ = tc.Set(ctx, flow, fingerprint, scope, token)
		return
	}
	cp := *token
	cp.CachedAt = time.Now()
	key := h.memCacheKey(ctx, flow, scope)
	h.mu.Lock()
	if h.memCache == nil {
		h.memCache = make(map[string]*auth.Token)
	}
	h.memCache[key] = &cp
	h.mu.Unlock()
}

func (h *Handler) refreshFingerprint(ctx context.Context, flow auth.Flow) string {
	var clientID, dcrFP string
	if h.dcrEnabled() {
		selected := selectedDynamicClientFromContext(ctx)
		if selected == nil {
			selected = h.reusableDynamicClient(ctx)
		}
		if selected == nil || selected.ClientID == "" {
			return ""
		}
		clientID = selected.ClientID
		dcrFP = selected.Fingerprint
	} else {
		clientID = h.cfg.ClientID
	}
	var wifGrantType, wifSubjectTokenType, wifFedToken, wifFedFile string
	if flow == auth.FlowWorkloadIdentity {
		if !h.hasWorkloadIdentityConfig() {
			return ""
		}
		wifGrantType = h.cfg.WorkloadIdentityGrantType
		if wifGrantType == "" {
			wifGrantType = config.WorkloadIdentityGrantClientCredentials
		}
		wifSubjectTokenType = h.cfg.SubjectTokenType
		if wifGrantType == config.WorkloadIdentityGrantTokenExchange && wifSubjectTokenType == "" {
			wifSubjectTokenType = config.DefaultSubjectTokenType
		}
		wifFedToken = string(h.cfg.FederatedToken)
		wifFedFile = h.cfg.FederatedTokenFile
	}
	return auth.FingerprintHash(strings.Join([]string{
		string(flow),
		clientID,
		dcrFP,
		h.cfg.TokenURL,
		h.cfg.AuthorizeURL,
		h.cfg.DeviceAuthURL,
		h.cfg.Audience,
		wifGrantType,
		wifSubjectTokenType,
		wifFedToken,
		wifFedFile,
	}, "|"))
}

func newSessionEpoch() string {
	var buf [8]byte
	_, _ = rand.Read(buf[:])
	return hex.EncodeToString(buf[:])
}

func sameRefreshTokenEnvelope(a, b *persistedRefreshToken) bool {
	if a == nil || b == nil {
		return false
	}
	return a.Value == b.Value && a.Flow == b.Flow && a.Fingerprint == b.Fingerprint && a.Epoch == b.Epoch
}

func (h *Handler) storeRefreshToken(ctx context.Context, refreshToken string) error {
	return h.storeRefreshTokenForFlow(ctx, h.resolveDefaultFlow(), refreshToken)
}

func (h *Handler) storeRefreshTokenForFlow(ctx context.Context, flow auth.Flow, refreshToken string) error {
	_, err := h.storeRefreshTokenEnvelope(ctx, flow, refreshToken)
	return err
}

func (h *Handler) storeRefreshTokenEnvelope(ctx context.Context, flow auth.Flow, refreshToken string) (*persistedRefreshToken, error) {
	fp := h.refreshFingerprint(ctx, flow)
	if fp == "" {
		return nil, fmt.Errorf("cannot compute refresh token fingerprint without valid client configuration")
	}
	env := &persistedRefreshToken{
		Value:       refreshToken,
		Flow:        flow,
		Fingerprint: fp,
		Epoch:       newSessionEpoch(),
	}
	data, err := json.Marshal(env)
	if err != nil {
		return nil, fmt.Errorf("marshal refresh token: %w", err)
	}
	if err := h.secretStore.Set(ctx, h.profileSecretKey(ctx, secretKeyRefreshSuffix), data); err != nil {
		return nil, err
	}
	return env, nil
}

func (h *Handler) loadRefreshTokenWithFlow(ctx context.Context) (string, auth.Flow, bool, error) {
	env, ok, err := h.loadRefreshTokenEnvelope(ctx)
	if err != nil || !ok {
		return "", "", false, err
	}
	return env.Value, env.Flow, true, nil
}

func (h *Handler) loadRefreshTokenEnvelope(ctx context.Context) (*persistedRefreshToken, bool, error) {
	if h.secretStore == nil {
		return nil, false, nil
	}
	data, err := h.secretStore.Get(ctx, h.profileSecretKey(ctx, secretKeyRefreshSuffix))
	if err != nil {
		if errors.Is(err, secrets.ErrNotFound) {
			return nil, false, nil
		}
		return nil, false, err
	}
	if len(data) == 0 {
		return nil, false, nil
	}
	var stored persistedRefreshToken
	if unmarshalErr := json.Unmarshal(data, &stored); unmarshalErr != nil {
		return nil, false, fmt.Errorf("unmarshal refresh token: %w", unmarshalErr)
	}
	expectedFP := h.refreshFingerprint(ctx, stored.Flow)
	if stored.Value == "" || stored.Fingerprint == "" || expectedFP == "" || stored.Fingerprint != expectedFP {
		return nil, false, nil
	}
	return &stored, true, nil
}

func (h *Handler) clearDerivedStateErr(ctx context.Context) error {
	profile := auth.ProfileFromContext(ctx)
	h.setDerivedUsername(ctx, "")
	h.mu.Lock()
	profilePrefix := profile + "|"
	for k := range h.memDerived {
		if strings.HasPrefix(k, profilePrefix) {
			delete(h.memDerived, k)
		}
	}
	h.mu.Unlock()
	if h.secretStore != nil {
		if err := h.secretStore.Delete(ctx, h.profileSecretKey(ctx, derivedTokenSuffix)); err != nil && !errors.Is(err, secrets.ErrNotFound) {
			return fmt.Errorf("clear derived token: %w", err)
		}
		if err := h.secretStore.Delete(ctx, h.profileSecretKey(ctx, derivedUsernameSuffix)); err != nil && !errors.Is(err, secrets.ErrNotFound) {
			return fmt.Errorf("clear derived username: %w", err)
		}
	}
	return nil
}

func (h *Handler) storeTokens(ctx context.Context, resp *tokenResponse, claims *auth.Claims, expiresAt time.Time, flow auth.Flow, scopes []string) error {
	if resp.RefreshToken != "" {
		if err := h.storeRefreshTokenForFlow(ctx, flow, resp.RefreshToken); err != nil {
			return fmt.Errorf("store refresh token: %w", err)
		}
	} else if err := h.secretStore.Delete(ctx, h.profileSecretKey(ctx, secretKeyRefreshSuffix)); err != nil && !errors.Is(err, secrets.ErrNotFound) {
		return fmt.Errorf("clear previous refresh token: %w", err)
	}
	// Clear any PAT session envelope from an earlier FlowPAT login on this profile.
	if err := h.secretStore.Delete(ctx, h.profileSecretKey(ctx, secretKeyPATTokenSuffix)); err != nil && !errors.Is(err, secrets.ErrNotFound) {
		return fmt.Errorf("clear previous pat session: %w", err)
	}
	tc := h.profileTokenCache(ctx)
	if tc != nil {
		fingerprint := h.cacheFingerprint(ctx, flow)
		scope := strings.Join(scopes, " ")
		tok := h.tokenRespToToken(resp, flow, scope)
		tok.ExpiresAt = expiresAt
		if err := tc.Set(ctx, flow, fingerprint, scope, tok); err != nil {
			return err
		}
	}
	meta := &auth.HandlerMetadata{Claims: claims, ExpiresAt: expiresAt, Scopes: scopes, LastLoginFlow: flow}
	metaBytes, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}
	if err := h.secretStore.Set(ctx, h.profileSecretKey(ctx, secretKeyMetadataSuffix), metaBytes); err != nil {
		return fmt.Errorf("store metadata: %w", err)
	}
	return nil
}

func (h *Handler) storeDerivedToken(ctx context.Context, result *exchangeResult, expiresAt time.Time, flow auth.Flow, scopes []string) error {
	scope := strings.Join(scopes, " ")
	fp := h.derivedFingerprint(ctx, flow)
	stored := &persistedDerivedToken{
		Token: auth.Token{
			AccessToken: result.Token,
			TokenType:   defaultTokenType,
			ExpiresAt:   expiresAt,
			Scope:       scope,
			Flow:        flow,
		},
		Fingerprint: fp,
		Username:    result.Username,
	}
	if result.Username != "" {
		h.setDerivedUsername(ctx, result.Username)
	}
	if h.secretStore == nil {
		key := h.memDerivedKey(ctx, flow, scope)
		h.mu.Lock()
		if h.memDerived == nil {
			h.memDerived = make(map[string]*persistedDerivedToken)
		}
		h.memDerived[key] = stored
		h.mu.Unlock()
		return nil
	}
	data, err := json.Marshal(stored) //nolint:gosec // runtime token
	if err != nil {
		return fmt.Errorf("marshal derived token: %w", err)
	}
	if setErr := h.secretStore.Set(ctx, h.profileSecretKey(ctx, derivedTokenSuffix), data); setErr != nil {
		return setErr
	}
	// Persist the extracted username so it survives process restarts.
	if result.Username != "" {
		if setErr := h.secretStore.Set(ctx, h.profileSecretKey(ctx, derivedUsernameSuffix), []byte(result.Username)); setErr != nil {
			h.logger.V(1).Info("failed to persist derived username", "error", setErr)
		}
	} else {
		_ = h.secretStore.Delete(ctx, h.profileSecretKey(ctx, derivedUsernameSuffix))
	}
	return nil
}

func (h *Handler) loadDerivedToken(ctx context.Context) (*auth.Token, error) {
	ctx = h.resolveContextProfile(ctx)
	return h.loadDerivedTokenForFlow(ctx, h.inferFlowForContext(ctx))
}

func (h *Handler) loadDerivedTokenForFlow(ctx context.Context, flow auth.Flow) (*auth.Token, error) {
	data, err := h.secretStore.Get(ctx, h.profileSecretKey(ctx, derivedTokenSuffix))
	if err != nil {
		return nil, err
	}
	var stored persistedDerivedToken
	if err := json.Unmarshal(data, &stored); err != nil {
		return nil, err
	}
	expectedFP := h.derivedFingerprint(ctx, flow)
	if stored.Flow != flow || stored.Fingerprint == "" || stored.Fingerprint != expectedFP {
		return nil, secrets.ErrNotFound
	}
	// Restore the derived username only after validating that the derived token
	// belongs to the current authentication and exchange configuration fingerprint.
	username := stored.Username
	if username == "" {
		if usernameData, usernameErr := h.secretStore.Get(ctx, h.profileSecretKey(ctx, derivedUsernameSuffix)); usernameErr == nil && len(usernameData) > 0 {
			username = string(usernameData)
		}
	}
	if username != "" {
		h.setDerivedUsername(ctx, username)
	}
	tok := stored.Token
	return &tok, nil
}

func (h *Handler) loadMetadata(ctx context.Context) (*auth.HandlerMetadata, error) {
	data, err := h.secretStore.Get(ctx, h.profileSecretKey(ctx, secretKeyMetadataSuffix))
	if err != nil {
		return nil, err
	}
	var meta auth.HandlerMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}

// ---------- refresh ----------

func (h *Handler) refreshAccessToken(ctx context.Context, flow auth.Flow, refreshToken, scope string) (*tokenResponse, error) {
	data := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	}
	var basicAuth *basicAuthCredentials
	if flow == auth.FlowWorkloadIdentity {
		grantType := h.cfg.WorkloadIdentityGrantType
		if grantType == "" {
			grantType = config.WorkloadIdentityGrantClientCredentials
		}
		switch grantType {
		case config.WorkloadIdentityGrantClientCredentials:
			jwt, jwtErr := h.resolveFederatedToken(ctx)
			if jwtErr != nil {
				return nil, jwtErr
			}
			clientID := h.effectiveClientID(ctx)
			if clientID == "" {
				return nil, fmt.Errorf("clientID is required for workload_identity refresh with client_credentials grant")
			}
			data.Set("client_id", clientID)
			data.Set("client_assertion_type", jwtBearerAssertionType)
			data.Set("client_assertion", jwt)
		case config.WorkloadIdentityGrantTokenExchange:
			clientID, clientSecret, credErr := h.resolveClientCredentials(ctx)
			if credErr != nil {
				return nil, credErr
			}
			if clientID != "" {
				if clientSecret != "" {
					basicAuth = &basicAuthCredentials{username: clientID, password: clientSecret}
				} else {
					data.Set("client_id", clientID)
				}
			}
		default:
			return nil, fmt.Errorf("unknown workloadIdentityGrantType %q (valid: %s, %s)",
				grantType, config.WorkloadIdentityGrantClientCredentials, config.WorkloadIdentityGrantTokenExchange)
		}
	} else {
		clientID, clientSecret, err := h.resolveClientCredentials(ctx)
		if err != nil {
			return nil, err
		}
		data.Set("client_id", clientID)
		if clientSecret != "" {
			data.Set("client_secret", clientSecret)
		}
	}
	if scope != "" {
		data.Set("scope", scope)
	}
	return h.postTokenEndpointWithAuth(ctx, data, basicAuth)
}

// ---------- HTTP ----------

func (h *Handler) postTokenEndpoint(ctx context.Context, data url.Values) (*tokenResponse, error) {
	return h.postTokenEndpointWithAuth(ctx, data, nil)
}

func (h *Handler) postTokenEndpointWithAuth(ctx context.Context, data url.Values, basicAuth *basicAuthCredentials) (*tokenResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.cfg.TokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if basicAuth != nil {
		// RFC 6749 Section 2.3.1 requires client_id and client_secret to be
		// individually application/x-www-form-urlencoded prior to Basic encoding.
		req.SetBasicAuth(url.QueryEscape(basicAuth.username), url.QueryEscape(basicAuth.password))
	}

	resp, err := h.httpClient.Do(req) //nolint:gosec // URL from trusted admin config
	if err != nil {
		return nil, fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return nil, fmt.Errorf("read token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var errResp tokenEndpointError
		if jsonErr := json.Unmarshal(body, &errResp); jsonErr == nil && errResp.ErrorCode != "" {
			return nil, &errResp
		}
		return nil, fmt.Errorf("token endpoint returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp tokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("parse token response: %w", err)
	}
	return &tokenResp, nil
}

// ---------- utilities ----------

func (h *Handler) secretKey(suffix string) string {
	return h.secretKeyPrefix + h.cfg.Name + "." + suffix
}

// secretKeyWithProfile returns a secret key that incorporates the profile.
// If profile is empty, falls back to the default (no profile) key.
// Pattern: scafctl.auth.oauth2.<handler>.<profile>.<suffix>
func (h *Handler) secretKeyWithProfile(profile, suffix string) string {
	profile = auth.NormalizeProfileName(profile)
	if profile == "" {
		return h.secretKey(suffix)
	}
	return h.secretKeyPrefix + h.cfg.Name + "." + profile + "." + suffix
}

// profileSecretKey returns the secret key for the given suffix, incorporating
// any profile found in the context.
func (h *Handler) profileSecretKey(ctx context.Context, suffix string) string {
	return h.secretKeyWithProfile(auth.ProfileFromContext(ctx), suffix)
}

// tokenCacheForProfile returns the token cache prefix for the given profile.
func (h *Handler) tokenCacheForProfile(profile string) string {
	profile = auth.NormalizeProfileName(profile)
	if profile == "" {
		return h.secretKeyPrefix + h.cfg.Name + "." + tokenCacheSuffix
	}
	return h.secretKeyPrefix + h.cfg.Name + "." + profile + "." + tokenCacheSuffix
}

// profileTokenCache returns a TokenCache for the active profile in context.
// If no profile is set, returns the default token cache.
func (h *Handler) profileTokenCache(ctx context.Context) *auth.TokenCache {
	profile := auth.NormalizeProfileName(auth.ProfileFromContext(ctx))
	if profile == "" {
		return h.tokenCache
	}
	if h.secretStore == nil {
		return nil
	}
	prefix := h.tokenCacheForProfile(profile)
	return auth.NewTokenCache(h.secretStore, prefix)
}

func (h *Handler) ensureSecrets() error {
	if h.secretStore == nil {
		if h.secretErr != nil {
			return h.secretErr
		}
		return fmt.Errorf("secret store not initialized")
	}
	return nil
}

func (h *Handler) resolveDefaultFlow() auth.Flow {
	switch h.cfg.DefaultFlow {
	case "interactive":
		return auth.FlowInteractive
	case "device_code":
		return auth.FlowDeviceCode
	case "client_credentials":
		return auth.FlowClientCredentials
	case "workload_identity":
		return auth.FlowWorkloadIdentity
	case "pat":
		return auth.FlowPAT
	default:
		if h.cfg.AuthorizeURL != "" {
			return auth.FlowInteractive
		}
		if h.cfg.DeviceAuthURL != "" {
			return auth.FlowDeviceCode
		}
		if h.hasWorkloadIdentityConfig() {
			return auth.FlowWorkloadIdentity
		}
		if h.hasClientCredentialsConfig() {
			return auth.FlowClientCredentials
		}
		if h.cfg.Token != "" {
			return auth.FlowPAT
		}
		return auth.FlowInteractive
	}
}

// nonInteractiveFlowOrder returns the candidate headless flow priority order:
// FlowWorkloadIdentity -> FlowClientCredentials -> FlowPAT, moving DefaultFlow
// to the front when DefaultFlow explicitly names one of those three flows.
func (h *Handler) nonInteractiveFlowOrder() []auth.Flow {
	base := []auth.Flow{
		auth.FlowWorkloadIdentity,
		auth.FlowClientCredentials,
		auth.FlowPAT,
	}
	var preferred auth.Flow
	switch h.cfg.DefaultFlow {
	case "workload_identity":
		preferred = auth.FlowWorkloadIdentity
	case "client_credentials":
		preferred = auth.FlowClientCredentials
	case "pat":
		preferred = auth.FlowPAT
	}
	if preferred == "" || preferred == base[0] {
		return base
	}
	ordered := make([]auth.Flow, 0, len(base))
	ordered = append(ordered, preferred)
	for _, f := range base {
		if f != preferred {
			ordered = append(ordered, f)
		}
	}
	return ordered
}

func (h *Handler) tokenExpiresAt(resp *tokenResponse) time.Time {
	if resp.ExpiresIn > 0 {
		return time.Now().Add(time.Duration(resp.ExpiresIn) * time.Second)
	}
	if claims, err := auth.ParseJWTClaims(resp.AccessToken); err == nil && !claims.ExpiresAt.IsZero() {
		return claims.ExpiresAt
	}
	return time.Now().Add(defaultImplicitExpiresIn * time.Second)
}

func (h *Handler) tokenRespToToken(resp *tokenResponse, flow auth.Flow, scope string) *auth.Token {
	tokenType := resp.TokenType
	if tokenType == "" {
		tokenType = defaultTokenType
	}
	return &auth.Token{
		AccessToken: resp.AccessToken,
		TokenType:   tokenType,
		ExpiresAt:   h.tokenExpiresAt(resp),
		Scope:       scope,
		Flow:        flow,
	}
}

func scopeSliceFromString(scope string) []string {
	if strings.TrimSpace(scope) == "" {
		return nil
	}
	return strings.Fields(scope)
}

// extractJSONPath extracts a value from a nested map using dot-notation.
func extractJSONPath(data map[string]any, path string) (string, error) {
	parts := strings.Split(path, ".")
	var current any = data
	for _, part := range parts {
		m, ok := current.(map[string]any)
		if !ok {
			return "", fmt.Errorf("expected object at %q, got %T", part, current)
		}
		current, ok = m[part]
		if !ok {
			return "", fmt.Errorf("field %q not found", part)
		}
	}
	switch v := current.(type) {
	case string:
		return v, nil
	case float64:
		return fmt.Sprintf("%g", v), nil
	default:
		return fmt.Sprintf("%v", v), nil
	}
}

func getStringField(data map[string]any, field string) string {
	if val, ok := data[field]; ok {
		if s, ok := val.(string); ok {
			return s
		}
	}
	return ""
}

// ValidateConfig validates a CustomOAuth2Config for required fields and constraints.
func ValidateConfig(cfg config.CustomOAuth2Config) error {
	return ValidateConfigFor(cfg)
}

// ValidateConfigFor validates a CustomOAuth2Config, additionally enforcing
// internal-secret prefix checks on secret:// references for any custom embedder
// binary names.
func ValidateConfigFor(cfg config.CustomOAuth2Config, binaryNames ...string) error {
	if cfg.Name == "" {
		return fmt.Errorf("custom OAuth2 handler: name is required")
	}

	if err := cfg.ValidateCredentials(binaryNames...); err != nil {
		return fmt.Errorf("custom OAuth2 handler %q: %w", cfg.Name, err)
	}

	// Validate responseType early so unknown values are rejected before
	// field-presence checks that depend on the response type.
	switch cfg.ResponseType {
	case "", "code":
		// default authorization code flow
	case "token":
		if cfg.AuthorizeURL == "" {
			return fmt.Errorf("custom OAuth2 handler %q: authorizeURL is required when responseType is token", cfg.Name)
		}
		if cfg.DefaultFlow != "" && cfg.DefaultFlow != "interactive" {
			return fmt.Errorf("custom OAuth2 handler %q: implicit grant (responseType=token) only supports interactive flow, got defaultFlow=%q", cfg.Name, cfg.DefaultFlow)
		}
	default:
		return fmt.Errorf("custom OAuth2 handler %q: unknown responseType %q (valid: code, token)", cfg.Name, cfg.ResponseType)
	}

	if cfg.DynamicClientRegistration != nil {
		if cfg.DynamicClientRegistration.RegistrationEndpoint == "" {
			return fmt.Errorf("custom OAuth2 handler %q: dynamicClientRegistration.registrationEndpoint is required", cfg.Name)
		}
	}
	dcrEnabled := cfg.DynamicClientRegistration != nil && cfg.DynamicClientRegistration.RegistrationEndpoint != ""
	hasWIF := cfg.FederatedToken != "" || cfg.FederatedTokenFile != ""
	hasPAT := cfg.Token != ""
	hasOAuthFlow := cfg.AuthorizeURL != "" || cfg.DeviceAuthURL != "" || cfg.ClientSecret != "" || hasWIF || dcrEnabled

	patOnly := (cfg.DefaultFlow == "pat" || hasPAT) && !hasOAuthFlow

	if cfg.TokenURL == "" && cfg.ResponseType != "token" && !patOnly {
		return fmt.Errorf("custom OAuth2 handler %q: tokenURL is required", cfg.Name)
	}

	wifGrant := cfg.WorkloadIdentityGrantType
	if wifGrant == "" {
		wifGrant = config.WorkloadIdentityGrantClientCredentials
	}
	wifTokenExchangeOnly := wifGrant == config.WorkloadIdentityGrantTokenExchange &&
		(cfg.DefaultFlow == "workload_identity" || (cfg.DefaultFlow == "" && hasWIF && cfg.AuthorizeURL == "" && cfg.DeviceAuthURL == "" && cfg.ClientSecret == ""))

	if cfg.ClientID == "" && !dcrEnabled && !patOnly && !wifTokenExchangeOnly {
		return fmt.Errorf("custom OAuth2 handler %q: clientID is required", cfg.Name)
	}

	switch cfg.DefaultFlow {
	case "", "interactive":
		if cfg.AuthorizeURL == "" && cfg.DeviceAuthURL == "" && cfg.ClientSecret == "" && !dcrEnabled && !hasWIF && !hasPAT {
			return fmt.Errorf("custom OAuth2 handler %q: at least one of authorizeURL, deviceAuthURL, clientSecret, federatedToken, federatedTokenFile, or token must be set", cfg.Name)
		}
	case "device_code":
		if cfg.DeviceAuthURL == "" {
			return fmt.Errorf("custom OAuth2 handler %q: deviceAuthURL is required when defaultFlow is device_code", cfg.Name)
		}
	case "client_credentials":
		if cfg.ClientSecret == "" && !dcrEnabled {
			return fmt.Errorf("custom OAuth2 handler %q: clientSecret is required when defaultFlow is client_credentials", cfg.Name)
		}
	case "workload_identity":
		if !hasWIF {
			return fmt.Errorf("custom OAuth2 handler %q: federatedToken or federatedTokenFile is required when defaultFlow is workload_identity", cfg.Name)
		}
		if wifGrant == config.WorkloadIdentityGrantClientCredentials && cfg.ClientID == "" && !dcrEnabled {
			return fmt.Errorf("custom OAuth2 handler %q: clientID is required when defaultFlow is workload_identity with client_credentials grant", cfg.Name)
		}
	case "pat":
		if !hasPAT {
			return fmt.Errorf("custom OAuth2 handler %q: token is required when defaultFlow is pat", cfg.Name)
		}
	default:
		return fmt.Errorf("custom OAuth2 handler %q: unknown defaultFlow %q (valid: interactive, device_code, client_credentials, workload_identity, pat)", cfg.Name, cfg.DefaultFlow)
	}

	if cfg.CallbackPort != 0 && (cfg.CallbackPort < 1024 || cfg.CallbackPort > 65535) {
		return fmt.Errorf("custom OAuth2 handler %q: callbackPort must be 0 (random) or 1024-65535, got %d", cfg.Name, cfg.CallbackPort)
	}

	if cfg.CallbackHost != "" || cfg.CallbackPath != "" {
		if err := oauth.ValidateCallbackHostPath(cfg.CallbackHost, cfg.CallbackPath); err != nil {
			return fmt.Errorf("custom OAuth2 handler %q: %w", cfg.Name, err)
		}
	}

	if cfg.TokenExchange != nil {
		if cfg.TokenExchange.URL == "" {
			return fmt.Errorf("custom OAuth2 handler %q: tokenExchange.url is required", cfg.Name)
		}
		if cfg.TokenExchange.TokenJSONPath == "" {
			return fmt.Errorf("custom OAuth2 handler %q: tokenExchange.tokenJSONPath is required", cfg.Name)
		}
		if cfg.TokenExchange.Method != "" {
			switch strings.ToUpper(cfg.TokenExchange.Method) {
			case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch:
			default:
				return fmt.Errorf("custom OAuth2 handler %q: tokenExchange.method must be GET, POST, PUT, or PATCH", cfg.Name)
			}
		}
	}

	return nil
}
