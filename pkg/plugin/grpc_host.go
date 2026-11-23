// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/oakwood-commons/scafctl-plugin-sdk/plugin/proto"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	hostnameresolver "github.com/oakwood-commons/scafctl/pkg/auth/hostname"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/paths"
	"github.com/oakwood-commons/scafctl/pkg/redact"
	"github.com/oakwood-commons/scafctl/pkg/secrets"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/spec"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// authProfileMetadataKey carries the execution auth profile (including an
// explicit "default" marker) across HostService RPCs whose proto message lacks
// a dedicated profile field (such as GetAuthIdentityRequest in SDK v0.19.0).
const authProfileMetadataKey = "x-plugin-auth-profile"

// authLineageMetadataKey carries active AuthHandlerWrapper call-lineage tokens
// across AuthHandlerService and HostService gRPC hops so reentrant callbacks
// can share compatible active slots despite queued writers and fail fast on
// incompatible reconfigurations.
const authLineageMetadataKey = "x-plugin-auth-lineage"

type callbackLineageCtxKey struct{}

func lineageFromContext(ctx context.Context) []string {
	ids, _ := ctx.Value(callbackLineageCtxKey{}).([]string)
	return ids
}

func withCallbackLineage(ctx context.Context, ids ...string) context.Context {
	existing := lineageFromContext(ctx)
	merged := make([]string, 0, len(existing)+len(ids))
	seen := make(map[string]struct{}, len(existing)+len(ids))
	for _, id := range existing {
		if id == "" {
			continue
		}
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			merged = append(merged, id)
		}
	}
	for _, id := range ids {
		if id == "" {
			continue
		}
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			merged = append(merged, id)
		}
	}
	if len(merged) == 0 {
		return ctx
	}
	return context.WithValue(ctx, callbackLineageCtxKey{}, merged)
}

func withoutCallbackLineage(ctx context.Context) context.Context {
	if len(lineageFromContext(ctx)) == 0 {
		return ctx
	}
	return context.WithValue(ctx, callbackLineageCtxKey{}, ([]string)(nil))
}

func appendOutgoingLineageMetadata(ctx context.Context) context.Context {
	ids := lineageFromContext(ctx)
	if len(ids) == 0 {
		ids = incomingMetadataLineage(ctx)
	}
	if len(ids) == 0 {
		return ctx
	}
	pairs := make([]string, 0, len(ids)*2)
	for _, id := range ids {
		pairs = append(pairs, authLineageMetadataKey, id)
	}
	return metadata.AppendToOutgoingContext(ctx, pairs...)
}

func incomingMetadataLineage(ctx context.Context) []string {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if vals := md.Get(authLineageMetadataKey); len(vals) > 0 {
			return append([]string(nil), vals...)
		}
	}
	if md, ok := metadata.FromOutgoingContext(ctx); ok {
		if vals := md.Get(authLineageMetadataKey); len(vals) > 0 {
			return append([]string(nil), vals...)
		}
	}
	return nil
}

func extractIncomingLineageToContext(ctx context.Context) context.Context {
	if vals := incomingMetadataLineage(ctx); len(vals) > 0 {
		return withCallbackLineage(ctx, vals...)
	}
	return ctx
}

type authCallbackFrame struct {
	handler    string
	profile    string
	hasProfile bool
	ids        []string
}

// authCallbackLineage tracks in-flight AuthHandlerWrapper call stacks per
// AuthHandlerClient so HostService callbacks from SDK plugin binaries that do
// not echo custom gRPC metadata still carry ancestor call lineage and the
// active handler profile.
type authCallbackLineage struct {
	mu     sync.Mutex
	nextID uint64
	stacks map[uint64]authCallbackFrame
}

func newAuthCallbackLineage() *authCallbackLineage {
	return &authCallbackLineage{stacks: make(map[uint64]authCallbackFrame)}
}

func (l *authCallbackLineage) push(handler, profile string, hasProfile bool, ids []string) func() {
	if l == nil || len(ids) == 0 {
		return func() {}
	}
	cp := append([]string(nil), ids...)
	l.mu.Lock()
	l.nextID++
	id := l.nextID
	if l.stacks == nil {
		l.stacks = make(map[uint64]authCallbackFrame)
	}
	l.stacks[id] = authCallbackFrame{
		handler:    handler,
		profile:    profile,
		hasProfile: hasProfile,
		ids:        cp,
	}
	l.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			l.mu.Lock()
			delete(l.stacks, id)
			l.mu.Unlock()
		})
	}
}

func (l *authCallbackLineage) snapshot() []string {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.stacks) == 0 {
		return nil
	}
	var out []string
	seen := make(map[string]struct{})
	for _, frame := range l.stacks {
		for _, id := range frame.ids {
			if _, ok := seen[id]; !ok {
				seen[id] = struct{}{}
				out = append(out, id)
			}
		}
	}
	return out
}

func (l *authCallbackLineage) activeProfile(handlerName string) (string, bool) {
	if l == nil || handlerName == "" {
		return "", false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	var (
		matched string
		found   bool
	)
	for _, frame := range l.stacks {
		if frame.handler != handlerName || !frame.hasProfile {
			continue
		}
		encoded := frame.profile
		if encoded == "" {
			encoded = auth.DefaultProfileName
		}
		if !found {
			matched = encoded
			found = true
			continue
		}
		if matched != encoded {
			return "", false
		}
	}
	return matched, found
}

// secretNameMaxLen is the maximum allowed length for a secret name.
const secretNameMaxLen = 256

// validSecretName matches alphanumeric characters, underscores, hyphens, dots,
// and forward slashes. This prevents path traversal and shell metacharacter injection.
var validSecretName = regexp.MustCompile(`^[a-zA-Z0-9_\-./]+$`)

// validateSecretName checks that a secret name is non-empty, within length
// bounds, and contains only safe characters.
func validateSecretName(name string) error {
	if name == "" {
		return fmt.Errorf("secret name must not be empty")
	}
	if len(name) > secretNameMaxLen {
		return fmt.Errorf("secret name exceeds maximum length of %d", secretNameMaxLen)
	}
	if !validSecretName.MatchString(name) {
		return fmt.Errorf("secret name contains invalid characters")
	}
	// Reject path traversal sequences
	if strings.Contains(name, "..") {
		return fmt.Errorf("secret name must not contain path traversal sequences")
	}
	return nil
}

// HostServiceDeps holds the host-side dependencies that the HostService
// exposes to plugins via gRPC callbacks. This is an internal dependency-injection
// struct passed by value into HostServiceServer; fields are not serialized.
type HostServiceDeps struct {
	// SecretStore provides access to the host's secret store (may be nil).
	SecretStore secrets.Store `json:"-" yaml:"-" doc:"Host secret store (not serialized)."`
	// AllowedSecretPrefix restricts secret access to names starting with this prefix.
	// If empty, all secrets are accessible (for backward compatibility).
	AllowedSecretPrefix string `json:"allowedSecretPrefix,omitempty" yaml:"allowedSecretPrefix,omitempty" doc:"Prefix restriction for secret access."`
	// ReadOnlySecrets blocks SetSecret and DeleteSecret RPCs when true.
	// Enforced automatically on ProviderPlugin clients.
	ReadOnlySecrets bool `json:"readOnlySecrets,omitempty" yaml:"readOnlySecrets,omitempty" doc:"Block secret write and delete RPCs."`
	// DeniedSecretPrefixes lists secret name prefixes that are rejected by GetSecret,
	// SetSecret, and DeleteSecret and filtered from ListSecrets (e.g. "scafctl.").
	DeniedSecretPrefixes []string `json:"deniedSecretPrefixes,omitempty" yaml:"deniedSecretPrefixes,omitempty" doc:"Denied secret name prefixes."`
	// providerDeniedPrefixes carries context-derived internal prefixes (e.g. embedder
	// binary name) to apply when constructing ProviderPlugin clients without affecting
	// AuthHandlerPlugin clients sharing the same ClientOption slice.
	providerDeniedPrefixes []string
	// AuthIdentityFunc returns claims for a named auth handler and scope.
	// handler may be empty for the default handler. May be nil if auth is unavailable.
	AuthIdentityFunc func(ctx context.Context, handler, scope string) (*proto.Claims, error) `json:"-" yaml:"-" doc:"Auth identity callback (not serialized)."`
	// AllowedAuthHandlers restricts which auth handler names a plugin may request.
	// If nil or empty, all handlers are allowed (for backward compatibility).
	AllowedAuthHandlers []string `json:"allowedAuthHandlers,omitempty" yaml:"allowedAuthHandlers,omitempty" doc:"Allowed auth handler names."`
	// AuthHandlersFunc returns available auth handler names and the default handler.
	// May be nil if auth is unavailable.
	AuthHandlersFunc func(ctx context.Context) (handlers []string, defaultHandler string, err error) `json:"-" yaml:"-" doc:"Auth handlers callback (not serialized)."`
	// AuthTokenFunc retrieves a valid access token from the host's auth registry.
	// Plugins call this for authenticated HTTP requests and token refresh on 401.
	// May be nil if auth is unavailable.
	AuthTokenFunc func(ctx context.Context, handler, scope string, minValidFor int64, forceRefresh bool) (*proto.GetAuthTokenResponse, error) `json:"-" yaml:"-" doc:"Auth token callback (not serialized)."`
	// AuthTokenWithHostnameFunc retrieves a valid access token with optional per-cluster/server
	// hostname selection. When set, GetAuthToken prefers this callback over AuthTokenFunc.
	AuthTokenWithHostnameFunc func(ctx context.Context, handler, scope, hostname string, minValidFor int64, forceRefresh bool) (*proto.GetAuthTokenResponse, error) `json:"-" yaml:"-" doc:"Auth token callback with hostname support (not serialized)."`
	// AuthGroupsFunc retrieves group memberships for the authenticated user.
	// Only handlers that implement auth.GroupsProvider (e.g. Entra) return
	// results; others yield an actionable error. Wired in production by
	// HostDepsFromAuthRegistry. May be nil if group queries are unavailable.
	AuthGroupsFunc func(ctx context.Context, handler string) ([]string, error) `json:"-" yaml:"-" doc:"Auth groups callback (not serialized)."`
	// ProfileResolverFunc resolves the active auth profile for a handler when
	// the plugin request does not specify one explicitly. This bridges the gap
	// between the host's --auth-profile flag / config activeProfile and plugin
	// providers that call back for auth tokens.
	// May be nil — in which case no profile resolution is performed.
	ProfileResolverFunc func(handlerName string) string `json:"-" yaml:"-" doc:"Profile resolver callback (not serialized)."`
	// PromptBroker gates HostService.PromptAuthResponse callbacks to the
	// Login currently in progress and carries the interactive prompt
	// installed by the login UI. When nil, the RPC is Unimplemented.
	// Populated by HostDepsFromAuthRegistry.
	PromptBroker *AuthPromptBroker `json:"-" yaml:"-" doc:"Paste-back prompt broker (not serialized)."`
	// HostConfig carries the host's loaded configuration so incoming gRPC
	// GetAuthToken callbacks can resolve configured hostname aliases and
	// dynamic hostname inventories.
	HostConfig *config.Config `json:"-" yaml:"-" doc:"Host configuration for auth hostname resolution (not serialized)."`
	// trackerSlot associates incoming HostService GetSecret and GetAuthToken
	// callbacks with the active execution's redaction tracker per plugin client.
	trackerSlot *brokerTrackerSlot
	// callbackLineage associates incoming HostService callbacks on an
	// AuthHandlerClient with its active AuthHandlerWrapper call frames.
	callbackLineage *authCallbackLineage
}

type brokerTrackerSlot struct {
	mu                   sync.Mutex
	activeRoot           *redact.Tracker
	activePlaceholder    bool
	activeAuthProfile    string
	activeHasAuthProfile bool
	activeLineage        []string
	activeList           []*redact.Tracker
	activeCount          int
	waitCh               chan struct{}
}

func newBrokerTrackerSlot() *brokerTrackerSlot {
	return &brokerTrackerSlot{}
}

func encodeContextAuthProfile(ctx context.Context) (string, bool) {
	if p := auth.ProfileFromContext(ctx); p != "" {
		return p, true
	}
	if auth.IsProfileResolved(ctx) {
		return auth.DefaultProfileName, true
	}
	return "", false
}

func incomingMetadataAuthProfile(ctx context.Context) (string, bool) {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if vals := md.Get(authProfileMetadataKey); len(vals) > 0 && vals[0] != "" {
			return vals[0], true
		}
	}
	if md, ok := metadata.FromOutgoingContext(ctx); ok {
		if vals := md.Get(authProfileMetadataKey); len(vals) > 0 && vals[0] != "" {
			return vals[0], true
		}
	}
	return "", false
}

func (s *brokerTrackerSlot) acquire(ctx context.Context, tracker *redact.Tracker) (func(), error) {
	if s == nil {
		return func() {}, nil
	}
	placeholder := spec.IsPlaceholderCredentialResolver(ctx)
	execProfile, hasExecProfile := encodeContextAuthProfile(ctx)
	execLineage := lineageFromContext(ctx)
	if tracker == nil {
		if !placeholder && !hasExecProfile && len(execLineage) == 0 {
			return func() {}, nil
		}
		tracker = redact.NewTracker()
	}
	root := tracker.Root()
	for {
		s.mu.Lock()
		if s.activeCount == 0 || (s.activeRoot == root && s.activePlaceholder == placeholder && s.activeAuthProfile == execProfile && s.activeHasAuthProfile == hasExecProfile && stringSlicesEqual(s.activeLineage, execLineage)) {
			s.activeRoot = root
			s.activePlaceholder = placeholder
			s.activeAuthProfile = execProfile
			s.activeHasAuthProfile = hasExecProfile
			s.activeLineage = append([]string(nil), execLineage...)
			s.activeCount++
			s.activeList = append(s.activeList, tracker)
			if s.waitCh == nil {
				s.waitCh = make(chan struct{})
			}
			s.mu.Unlock()

			var once sync.Once
			return func() {
				once.Do(func() {
					s.mu.Lock()
					for i, t := range s.activeList {
						if t == tracker {
							s.activeList = append(s.activeList[:i], s.activeList[i+1:]...)
							break
						}
					}
					s.activeCount--
					if s.activeCount == 0 {
						s.activeRoot = nil
						s.activePlaceholder = false
						s.activeAuthProfile = ""
						s.activeHasAuthProfile = false
						s.activeLineage = nil
						if s.waitCh != nil {
							close(s.waitCh)
							s.waitCh = nil
						}
					}
					s.mu.Unlock()
				})
			}, nil
		}
		waitCh := s.waitCh
		s.mu.Unlock()

		select {
		case <-waitCh:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func (s *brokerTrackerSlot) isPlaceholder() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.activeCount > 0 && s.activePlaceholder
}

func (s *brokerTrackerSlot) activeProfile() (string, bool) {
	if s == nil {
		return "", false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.activeCount == 0 || !s.activeHasAuthProfile {
		return "", false
	}
	return s.activeAuthProfile, true
}

func (d *HostServiceDeps) isPlaceholderMode() bool {
	return d != nil && d.trackerSlot.isPlaceholder()
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (s *brokerTrackerSlot) activeLineageSnapshot() []string {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.activeCount == 0 || len(s.activeLineage) == 0 {
		return nil
	}
	return append([]string(nil), s.activeLineage...)
}

func (d *HostServiceDeps) activeExecutionProfile() (string, bool) {
	if d == nil {
		return "", false
	}
	return d.trackerSlot.activeProfile()
}

func (d *HostServiceDeps) activeCallbackLineage() []string {
	if d == nil {
		return nil
	}
	if ids := d.callbackLineage.snapshot(); len(ids) > 0 {
		return ids
	}
	return d.trackerSlot.activeLineageSnapshot()
}

func (d *HostServiceDeps) activeCallbackProfile(handlerName string) (string, bool) {
	if d == nil {
		return "", false
	}
	return d.callbackLineage.activeProfile(handlerName)
}

func (s *brokerTrackerSlot) track(secret string) {
	if s == nil || secret == "" {
		return
	}
	s.mu.Lock()
	root := s.activeRoot
	children := append([]*redact.Tracker(nil), s.activeList...)
	s.mu.Unlock()

	if root != nil {
		root.Track(secret)
	}
	for _, t := range children {
		if t != nil && t != root {
			t.Track(secret)
		}
	}
}

func (d *HostServiceDeps) trackBrokerSecret(secret string) {
	if d == nil || d.trackerSlot == nil || secret == "" {
		return
	}
	d.trackerSlot.track(secret)
}

// isSecretAllowed checks whether the given secret name is within the allowed
// prefix scope. Returns true when no prefix restriction is configured.
func (d *HostServiceDeps) isSecretAllowed(name string) bool {
	if d.AllowedSecretPrefix == "" {
		return true
	}
	return strings.HasPrefix(name, d.AllowedSecretPrefix)
}

func (d *HostServiceDeps) isSecretDenied(name string) bool {
	for _, prefix := range d.DeniedSecretPrefixes {
		if prefix != "" && strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// isAuthHandlerAllowed checks whether the given handler name is in the
// AllowedAuthHandlers list. Returns true when no restriction is configured.
// When an allowlist is configured, an empty handler name is rejected to
// prevent bypassing the restriction via the default handler.
func (d *HostServiceDeps) isAuthHandlerAllowed(handler string) bool {
	if len(d.AllowedAuthHandlers) == 0 {
		return true
	}
	if handler == "" {
		return false
	}
	for _, h := range d.AllowedAuthHandlers {
		if h == handler {
			return true
		}
	}
	return false
}

// HostServiceServer implements the HostService gRPC server (runs on the host).
// Plugins call these RPCs back into the host process via the go-plugin GRPCBroker.
type HostServiceServer struct {
	proto.UnimplementedHostServiceServer
	Deps HostServiceDeps
}

// GetSecret implements HostService.GetSecret
func (h *HostServiceServer) GetSecret(ctx context.Context, req *proto.GetSecretRequest) (*proto.GetSecretResponse, error) {
	placeholder := h.Deps.isPlaceholderMode()
	if h.Deps.SecretStore == nil && !placeholder {
		return &proto.GetSecretResponse{
			Error: "secret store not available",
		}, nil
	}

	if err := validateSecretName(req.Name); err != nil {
		//nolint:nilerr // Error is communicated via response, not gRPC error
		return &proto.GetSecretResponse{Error: err.Error()}, nil
	}
	if !h.Deps.isSecretAllowed(req.Name) {
		return &proto.GetSecretResponse{Error: "access denied: secret name outside allowed scope"}, nil
	}
	if h.Deps.isSecretDenied(req.Name) {
		return &proto.GetSecretResponse{Error: "access denied: secret name is in a reserved internal namespace"}, nil
	}

	if placeholder {
		return &proto.GetSecretResponse{
			Value: fmt.Sprintf("<redacted:scrt:%s>", req.Name),
			Found: true,
		}, nil
	}

	value, err := h.Deps.SecretStore.Get(ctx, req.Name)
	if err != nil {
		if errors.Is(err, secrets.ErrNotFound) {
			return &proto.GetSecretResponse{Found: false}, nil
		}
		return &proto.GetSecretResponse{
			Error: fmt.Sprintf("get secret %q: %v", req.Name, err),
		}, nil
	}

	secretVal := string(value)
	h.Deps.trackBrokerSecret(secretVal)
	return &proto.GetSecretResponse{
		Value: secretVal,
		Found: true,
	}, nil
}

// SetSecret implements HostService.SetSecret
func (h *HostServiceServer) SetSecret(ctx context.Context, req *proto.SetSecretRequest) (*proto.SetSecretResponse, error) {
	if h.Deps.SecretStore == nil {
		return &proto.SetSecretResponse{
			Error: "secret store not available",
		}, nil
	}
	if h.Deps.ReadOnlySecrets {
		return &proto.SetSecretResponse{
			Error: "access denied: secret store is read-only for provider plugins",
		}, nil
	}

	if err := validateSecretName(req.Name); err != nil {
		//nolint:nilerr // Error is communicated via response, not gRPC error
		return &proto.SetSecretResponse{Error: err.Error()}, nil
	}
	if !h.Deps.isSecretAllowed(req.Name) {
		return &proto.SetSecretResponse{Error: "access denied: secret name outside allowed scope"}, nil
	}
	if h.Deps.isSecretDenied(req.Name) {
		return &proto.SetSecretResponse{Error: "access denied: secret name is in a reserved internal namespace"}, nil
	}

	if err := h.Deps.SecretStore.Set(ctx, req.Name, []byte(req.Value)); err != nil {
		return &proto.SetSecretResponse{
			Error: fmt.Sprintf("set secret %q: %v", req.Name, err),
		}, nil
	}

	return &proto.SetSecretResponse{}, nil
}

// DeleteSecret implements HostService.DeleteSecret
func (h *HostServiceServer) DeleteSecret(ctx context.Context, req *proto.DeleteSecretRequest) (*proto.DeleteSecretResponse, error) {
	if h.Deps.SecretStore == nil {
		return &proto.DeleteSecretResponse{
			Error: "secret store not available",
		}, nil
	}
	if h.Deps.ReadOnlySecrets {
		return &proto.DeleteSecretResponse{
			Error: "access denied: secret store is read-only for provider plugins",
		}, nil
	}

	if err := validateSecretName(req.Name); err != nil {
		//nolint:nilerr // Error is communicated via response, not gRPC error
		return &proto.DeleteSecretResponse{Error: err.Error()}, nil
	}
	if !h.Deps.isSecretAllowed(req.Name) {
		return &proto.DeleteSecretResponse{Error: "access denied: secret name outside allowed scope"}, nil
	}
	if h.Deps.isSecretDenied(req.Name) {
		return &proto.DeleteSecretResponse{Error: "access denied: secret name is in a reserved internal namespace"}, nil
	}

	if err := h.Deps.SecretStore.Delete(ctx, req.Name); err != nil {
		return &proto.DeleteSecretResponse{
			Error: fmt.Sprintf("delete secret %q: %v", req.Name, err),
		}, nil
	}

	return &proto.DeleteSecretResponse{}, nil
}

// maxSecretPatternLength limits the length of the regex pattern to prevent ReDoS.
const maxSecretPatternLength = 256

// ListSecrets implements HostService.ListSecrets
func (h *HostServiceServer) ListSecrets(ctx context.Context, req *proto.ListSecretsRequest) (*proto.ListSecretsResponse, error) {
	if h.Deps.SecretStore == nil {
		return &proto.ListSecretsResponse{
			Error: "secret store not available",
		}, nil
	}

	names, err := h.Deps.SecretStore.List(ctx)
	if err != nil {
		return &proto.ListSecretsResponse{
			Error: fmt.Sprintf("list secrets: %v", err),
		}, nil
	}

	// Filter to only secrets within the allowed scope
	if h.Deps.AllowedSecretPrefix != "" {
		filtered := make([]string, 0, len(names))
		for _, n := range names {
			if strings.HasPrefix(n, h.Deps.AllowedSecretPrefix) {
				filtered = append(filtered, n)
			}
		}
		names = filtered
	}

	// Exclude denied internal secret prefixes
	if len(h.Deps.DeniedSecretPrefixes) > 0 {
		filtered := make([]string, 0, len(names))
		for _, n := range names {
			if !h.Deps.isSecretDenied(n) {
				filtered = append(filtered, n)
			}
		}
		names = filtered
	}

	// Apply optional regex pattern filter
	if req.Pattern != "" {
		if len(req.Pattern) > maxSecretPatternLength {
			return &proto.ListSecretsResponse{
				Error: fmt.Sprintf("pattern too long: max %d characters", maxSecretPatternLength),
			}, nil
		}
		re, err := regexp.Compile(req.Pattern)
		if err != nil {
			return &proto.ListSecretsResponse{
				Error: fmt.Sprintf("invalid pattern: %v", err),
			}, nil
		}
		filtered := make([]string, 0, len(names))
		for _, n := range names {
			if re.MatchString(n) {
				filtered = append(filtered, n)
			}
		}
		names = filtered
	}

	return &proto.ListSecretsResponse{
		Names: names,
	}, nil
}

func (h *HostServiceServer) restoreCallbackLineage(ctx context.Context) context.Context {
	if mdIDs := incomingMetadataLineage(ctx); len(mdIDs) > 0 {
		return withCallbackLineage(ctx, mdIDs...)
	}
	if brokerIDs := h.Deps.activeCallbackLineage(); len(brokerIDs) > 0 {
		return withCallbackLineage(ctx, brokerIDs...)
	}
	return ctx
}

// resolveHostAuthCallback selects the default handler when requestedHandler is
// empty (via AuthHandlersFunc) before resolving the effective auth profile.
// Profile precedence:
//  1. Explicit request field (including "default" / "built-in" markers)
//  2. Incoming gRPC metadata (for RPCs without a proto profile field)
//  3. Active provider execution profile captured on trackerSlot
//  4. Active auth handler callback profile on callbackLineage
//  5. Host ProfileResolverFunc(effectiveHandler)
func (h *HostServiceServer) resolveHostAuthCallback(
	ctx context.Context,
	requestedHandler string,
	explicitProfile string,
) (context.Context, string, error) {
	ctx = h.restoreCallbackLineage(ctx)
	effectiveHandler := requestedHandler
	if effectiveHandler == "" && h.Deps.AuthHandlersFunc != nil {
		if _, defaultHandler, err := h.Deps.AuthHandlersFunc(ctx); err == nil && defaultHandler != "" {
			effectiveHandler = defaultHandler
		}
	}

	var (
		rawProfile string
		resolved   bool
	)
	switch {
	case explicitProfile != "":
		rawProfile = explicitProfile
		resolved = true
	default:
		if mdProfile, ok := incomingMetadataAuthProfile(ctx); ok {
			rawProfile = mdProfile
			resolved = true
		} else if execProfile, ok := h.Deps.activeExecutionProfile(); ok {
			rawProfile = execProfile
			resolved = true
		} else if cbProfile, ok := h.Deps.activeCallbackProfile(effectiveHandler); ok {
			rawProfile = cbProfile
			resolved = true
		} else if h.Deps.ProfileResolverFunc != nil {
			rawProfile = h.Deps.ProfileResolverFunc(effectiveHandler)
			resolved = effectiveHandler != "" || rawProfile != ""
		}
	}

	if resolved {
		profile := auth.NormalizeProfileName(rawProfile)
		if profile != "" {
			if err := auth.ValidateProfileName(profile); err != nil {
				return ctx, effectiveHandler, err
			}
		}
		// Mark profile as resolved even when normalized to "" (built-in/default)
		// so downstream handlers do not fall back to HostConfig activeProfile.
		ctx = auth.WithProfile(ctx, profile)
		ctx = auth.WithProfileResolved(ctx)
	}

	if h.Deps.HostConfig != nil && config.FromContext(ctx) == nil {
		ctx = config.WithConfig(ctx, h.Deps.HostConfig)
	}
	return ctx, effectiveHandler, nil
}

// GetAuthIdentity implements HostService.GetAuthIdentity
func (h *HostServiceServer) GetAuthIdentity(ctx context.Context, req *proto.GetAuthIdentityRequest) (*proto.GetAuthIdentityResponse, error) {
	if h.Deps.AuthIdentityFunc == nil {
		return &proto.GetAuthIdentityResponse{
			Error: "auth identity not available",
		}, nil
	}

	if !h.Deps.isAuthHandlerAllowed(req.HandlerName) {
		return &proto.GetAuthIdentityResponse{
			Error: fmt.Sprintf("access denied: auth handler %q is not in the allowed set", req.HandlerName),
		}, nil
	}

	ctx, handlerName, err := h.resolveHostAuthCallback(ctx, req.HandlerName, "")
	if err != nil {
		return &proto.GetAuthIdentityResponse{
			Error: fmt.Sprintf("invalid profile: %v", err),
		}, nil
	}

	claims, err := h.Deps.AuthIdentityFunc(ctx, handlerName, req.Scope)
	if err != nil {
		return &proto.GetAuthIdentityResponse{
			Error: fmt.Sprintf("get auth identity: %v", err),
		}, nil
	}

	return &proto.GetAuthIdentityResponse{
		Claims: claims,
	}, nil
}

// ListAuthHandlers implements HostService.ListAuthHandlers
//
//nolint:revive // req is required by gRPC interface even if unused
func (h *HostServiceServer) ListAuthHandlers(ctx context.Context, _ *proto.ListAuthHandlersRequest) (*proto.ListAuthHandlersResponse, error) {
	if h.Deps.AuthHandlersFunc == nil {
		return &proto.ListAuthHandlersResponse{}, nil
	}

	handlers, defaultHandler, err := h.Deps.AuthHandlersFunc(ctx)
	if err != nil {
		// ListAuthHandlersResponse has no Error field, so log and degrade
		// gracefully instead of returning internal errors to the plugin.
		lgr := logger.FromContext(ctx)
		lgr.V(1).Info("list auth handlers failed, returning empty", "error", err)
		return &proto.ListAuthHandlersResponse{}, nil
	}

	// Filter handlers to only those allowed for this plugin. This prevents
	// plugins from discovering handler names they are not permitted to use.
	if len(h.Deps.AllowedAuthHandlers) > 0 {
		allowed := make(map[string]struct{}, len(h.Deps.AllowedAuthHandlers))
		for _, a := range h.Deps.AllowedAuthHandlers {
			allowed[a] = struct{}{}
		}
		filtered := make([]string, 0, len(handlers))
		for _, name := range handlers {
			if _, ok := allowed[name]; ok {
				filtered = append(filtered, name)
			}
		}
		handlers = filtered
		// Redact default handler if not in allowed set
		if _, ok := allowed[defaultHandler]; !ok {
			defaultHandler = ""
		}
	}

	return &proto.ListAuthHandlersResponse{
		HandlerNames:   handlers,
		DefaultHandler: defaultHandler,
	}, nil
}

// GetAuthToken implements HostService.GetAuthToken
func (h *HostServiceServer) GetAuthToken(ctx context.Context, req *proto.GetAuthTokenRequest) (*proto.GetAuthTokenResponse, error) {
	placeholder := h.Deps.isPlaceholderMode()
	if h.Deps.AuthTokenFunc == nil && h.Deps.AuthTokenWithHostnameFunc == nil && !placeholder {
		return &proto.GetAuthTokenResponse{
			Error: "auth token service not available",
		}, nil
	}

	if !h.Deps.isAuthHandlerAllowed(req.HandlerName) {
		return &proto.GetAuthTokenResponse{
			Error: fmt.Sprintf("access denied: auth handler %q is not in the allowed set", req.HandlerName),
		}, nil
	}

	ctx, handlerName, profileErr := h.resolveHostAuthCallback(ctx, req.HandlerName, req.Profile)
	if profileErr != nil {
		return &proto.GetAuthTokenResponse{
			Error: fmt.Sprintf("invalid profile from plugin: %v", profileErr),
		}, nil
	}

	if placeholder {
		handlerLabel := req.HandlerName
		if handlerLabel == "" {
			handlerLabel = "default"
		}
		return &proto.GetAuthTokenResponse{
			AccessToken: fmt.Sprintf("<redacted:tkn:%s>", handlerLabel),
			TokenType:   "Bearer",
		}, nil
	}

	var (
		resp *proto.GetAuthTokenResponse
		err  error
	)
	switch {
	case h.Deps.AuthTokenWithHostnameFunc != nil:
		resp, err = h.Deps.AuthTokenWithHostnameFunc(ctx, handlerName, req.Scope, req.Hostname, req.MinValidForSeconds, req.ForceRefresh)
	case req.Hostname != "":
		return &proto.GetAuthTokenResponse{
			Error: "auth token service does not support hostname selection",
		}, nil
	default:
		resp, err = h.Deps.AuthTokenFunc(ctx, handlerName, req.Scope, req.MinValidForSeconds, req.ForceRefresh)
	}
	if err != nil {
		return &proto.GetAuthTokenResponse{
			Error: fmt.Sprintf("get auth token: %v", err),
		}, nil
	}
	if resp != nil && resp.Error == "" && resp.AccessToken != "" {
		h.Deps.trackBrokerSecret(resp.AccessToken)
	}

	return resp, nil
}

// GetAuthGroups implements HostService.GetAuthGroups
func (h *HostServiceServer) GetAuthGroups(ctx context.Context, req *proto.GetAuthGroupsRequest) (*proto.GetAuthGroupsResponse, error) {
	if h.Deps.AuthGroupsFunc == nil {
		return &proto.GetAuthGroupsResponse{
			Error: "auth groups not available",
		}, nil
	}

	if !h.Deps.isAuthHandlerAllowed(req.HandlerName) {
		return &proto.GetAuthGroupsResponse{
			Error: fmt.Sprintf("access denied: auth handler %q is not in the allowed set", req.HandlerName),
		}, nil
	}

	ctx, handlerName, profileErr := h.resolveHostAuthCallback(ctx, req.HandlerName, req.Profile)
	if profileErr != nil {
		return &proto.GetAuthGroupsResponse{
			Error: fmt.Sprintf("invalid profile: %v", profileErr),
		}, nil
	}

	groups, err := h.Deps.AuthGroupsFunc(ctx, handlerName)
	if err != nil {
		return &proto.GetAuthGroupsResponse{
			Error: fmt.Sprintf("get auth groups: %v", err),
		}, nil
	}

	return &proto.GetAuthGroupsResponse{
		Groups: groups,
	}, nil
}

// promptPasteMaxLen bounds the pasted redirect URL length; a real redirect
// URL stays well under this, so the limit only stops runaway input.
const promptPasteMaxLen = 8192

// PromptAuthResponse implements HostService.PromptAuthResponse.
//
// It renders the host-authored paste-back prompt for an interactive
// authorization-code login and returns the redirect URL the user pasted.
// Guardrails:
//   - Returns Unavailable (per the SDK v0.18.0 HostService contract) unless a
//     Login for the calling handler is currently in progress -- the login
//     wrapper marks that window on the broker.
//   - The prompt text is host-written; the plugin only supplies URLs, and
//     the authorization URL itself is shape-checked here too, so the call
//     cannot phish for input with foreign wording or an attacker URL.
//   - Returns Unavailable when the login is not interactive (no TTY) and
//     Unimplemented when the host wires no broker.
//   - The pasted value is never logged at any verbosity (it carries an
//     authorization code), on success or on any error path.
//   - ctx cancellation (the plugin's own localhost callback arrived first)
//     stops the prompt and returns Canceled; the cancellable read leaves no
//     reader behind to steal the next terminal input.
func (h *HostServiceServer) PromptAuthResponse(ctx context.Context, req *proto.PromptAuthResponseRequest) (*proto.PromptAuthResponseResponse, error) {
	if h.Deps.PromptBroker == nil {
		return nil, status.Error(codes.Unimplemented, "host does not implement PromptAuthResponse")
	}

	prompt, active := h.Deps.PromptBroker.Prompt(req.HandlerName)
	if !active {
		return nil, status.Errorf(codes.Unavailable, "paste-back prompt is unavailable: no active login for auth handler %q", req.HandlerName)
	}
	if prompt == nil {
		return nil, status.Error(codes.Unavailable, "paste-back prompt is unavailable: this session is not interactive")
	}

	if err := validateAuthorizationURL(req.AuthorizationUrl); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if err := validateExpectedRedirectURI(req.RedirectUri); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	value, err := prompt(ctx, req.AuthorizationUrl, req.RedirectUri)
	if err != nil {
		// Never include the value in any error path.
		if ctx.Err() != nil {
			return nil, status.Error(codes.Canceled, "paste-back prompt canceled")
		}
		if errors.Is(err, ErrUntrustedAuthorizationURL) {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		return nil, status.Errorf(codes.Internal, "reading pasted redirect URL: %v", err)
	}

	normalized, err := normalizePastedRedirectURL(value, req.RedirectUri)
	if err != nil {
		// The value (which may embed an authorization code) is not included
		// in the message.
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	return &proto.PromptAuthResponseResponse{Value: normalized}, nil
}

// isLoopbackHost reports whether an URL hostname is a loopback endpoint:
// the literal name "localhost" or any loopback IP (each address of
// 127.0.0.0/8, ::1), covering dev login servers bound to 127.0.0.2 and
// similar addresses.
func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// validateAuthorizationURL shape-checks the plugin-supplied authorization URL
// (the sign-in page the host displays) before any host-authored prompt text
// renders it: an absolute https URL, or an http URL on a loopback host (local
// dev login servers). This keeps a handler from laundering an attacker URL
// through the host's trusted prompt banner.
func validateAuthorizationURL(authURL string) error {
	if authURL == "" {
		return errors.New("authorization URL must not be empty")
	}
	parsed, err := url.Parse(authURL)
	if err != nil || !parsed.IsAbs() || parsed.Hostname() == "" {
		return errors.New("authorization URL is not an absolute URL with a host")
	}
	switch parsed.Scheme {
	case "https":
		return nil
	case "http":
		if isLoopbackHost(parsed.Hostname()) {
			return nil
		}
		return errors.New("http authorization URL is only allowed on loopback hosts")
	default:
		return errors.New("authorization URL must use https (or http on loopback)")
	}
}

// validateExpectedRedirectURI requires the plugin-supplied redirect prefix
// to be an absolute URL with a host: it is the main guard on the pasted
// value, so a missing or malformed one must not silently disable it.
func validateExpectedRedirectURI(redirectURI string) error {
	parsed, err := url.Parse(redirectURI)
	if redirectURI == "" || err != nil || !parsed.IsAbs() || parsed.Host == "" {
		return errors.New("redirect URI must be an absolute URL with a host")
	}
	return nil
}

// validatePastedRedirectURL checks that a pasted value looks like the
// expected redirect URL. Errors never embed the value itself.
func validatePastedRedirectURL(value, redirectURI string) error {
	_, err := normalizePastedRedirectURL(value, redirectURI)
	return err
}

// normalizePastedRedirectURL trims, normalizes, and validates a pasted redirect
// URL against redirectURI, returning the canonical absolute URL safe to pass to
// plugins. Errors never embed the pasted value itself.
//
// Chromium-based browsers omit the "http://" scheme when copying a loopback
// URL from the address bar (e.g. "localhost:8400/callback?code=..."). When
// redirectURI uses "http://" on a loopback host and the trimmed input starts
// with the expected loopback host/port followed by end-of-input, "/", "?", or
// "#", "http://" is restored before parsing (even when query or fragment
// parameters contain URL values such as error_uri=https://...).
func normalizePastedRedirectURL(value, redirectURI string) (string, error) {
	if len(value) > promptPasteMaxLen {
		return "", fmt.Errorf("pasted redirect URL exceeds the maximum length of %d characters", promptPasteMaxLen)
	}
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", errors.New("empty redirect URL: paste the full address from the browser's address bar")
	}
	var want *url.URL
	if validateExpectedRedirectURI(redirectURI) == nil {
		want, _ = url.Parse(redirectURI)
	}
	if want != nil && want.Scheme == "http" && isLoopbackHost(want.Hostname()) &&
		len(trimmed) >= len(want.Host) && strings.EqualFold(trimmed[:len(want.Host)], want.Host) &&
		(len(trimmed) == len(want.Host) || trimmed[len(want.Host)] == '/' || trimmed[len(want.Host)] == '?' || trimmed[len(want.Host)] == '#') {
		trimmed = "http://" + want.Host + trimmed[len(want.Host):]
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || !parsed.IsAbs() || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil {
		return "", errors.New("pasted value is not an absolute URL (e.g. http://localhost:8400/callback?code=...)")
	}
	if err := validateExpectedRedirectURI(redirectURI); err != nil {
		return "", err
	}
	if !strings.EqualFold(parsed.Scheme, want.Scheme) || !strings.EqualFold(parsed.Host, want.Host) {
		return "", fmt.Errorf("pasted URL does not match the expected redirect address %s", redirectURI)
	}
	wantPath := strings.TrimSuffix(want.Path, "/")
	gotPath := strings.TrimSuffix(parsed.Path, "/")
	if wantPath != "" && gotPath != wantPath && !strings.HasPrefix(gotPath, wantPath+"/") {
		return "", fmt.Errorf("pasted URL does not match the expected redirect address %s", redirectURI)
	}

	// Canonicalize scheme and host casing to match redirectURI while preserving
	// path, raw query, and fragment bytes verbatim.
	if idx := strings.Index(trimmed, "://"); idx >= 0 {
		afterScheme := trimmed[idx+3:]
		hostEnd := strings.IndexAny(afterScheme, "/?#")
		if hostEnd < 0 {
			hostEnd = len(afterScheme)
		}
		trimmed = want.Scheme + "://" + want.Host + afterScheme[hostEnd:]
	}
	if len(trimmed) > promptPasteMaxLen {
		return "", fmt.Errorf("pasted redirect URL exceeds the maximum length of %d characters", promptPasteMaxLen)
	}
	return trimmed, nil
}

// HostServiceClient wraps the HostService gRPC client (used by plugins).
// Plugin code uses this to call back into the host process.
type HostServiceClient struct {
	client proto.HostServiceClient
}

// NewHostServiceClient creates a HostServiceClient from a gRPC connection.
func NewHostServiceClient(conn *grpc.ClientConn) *HostServiceClient {
	return &HostServiceClient{client: proto.NewHostServiceClient(conn)}
}

// GetSecret retrieves a secret from the host's secret store.
func (c *HostServiceClient) GetSecret(ctx context.Context, name string) (string, bool, error) {
	resp, err := c.client.GetSecret(ctx, &proto.GetSecretRequest{Name: name})
	if err != nil {
		return "", false, fmt.Errorf("host GetSecret: %w", err)
	}
	if resp.Error != "" {
		return "", false, fmt.Errorf("host GetSecret: %s", resp.Error)
	}
	return resp.Value, resp.Found, nil
}

// SetSecret stores a secret in the host's secret store.
func (c *HostServiceClient) SetSecret(ctx context.Context, name, value string) error {
	resp, err := c.client.SetSecret(ctx, &proto.SetSecretRequest{Name: name, Value: value})
	if err != nil {
		return fmt.Errorf("host SetSecret: %w", err)
	}
	if resp.Error != "" {
		return fmt.Errorf("host SetSecret: %s", resp.Error)
	}
	return nil
}

// DeleteSecret removes a secret from the host's secret store.
func (c *HostServiceClient) DeleteSecret(ctx context.Context, name string) error {
	resp, err := c.client.DeleteSecret(ctx, &proto.DeleteSecretRequest{Name: name})
	if err != nil {
		return fmt.Errorf("host DeleteSecret: %w", err)
	}
	if resp.Error != "" {
		return fmt.Errorf("host DeleteSecret: %s", resp.Error)
	}
	return nil
}

// ListSecrets lists secret names from the host's secret store.
func (c *HostServiceClient) ListSecrets(ctx context.Context, pattern string) ([]string, error) {
	resp, err := c.client.ListSecrets(ctx, &proto.ListSecretsRequest{Pattern: pattern})
	if err != nil {
		return nil, fmt.Errorf("host ListSecrets: %w", err)
	}
	if resp.Error != "" {
		return nil, fmt.Errorf("host ListSecrets: %s", resp.Error)
	}
	return resp.Names, nil
}

// GetAuthIdentity retrieves identity claims from the host's auth registry,
// forwarding the execution auth profile (including explicit default selection)
// via gRPC metadata.
func (c *HostServiceClient) GetAuthIdentity(ctx context.Context, handler, scope string) (*proto.Claims, error) {
	ctx = appendOutgoingLineageMetadata(ctx)
	if encoded, ok := encodeContextAuthProfile(ctx); ok {
		ctx = metadata.AppendToOutgoingContext(ctx, authProfileMetadataKey, encoded)
	}
	resp, err := c.client.GetAuthIdentity(ctx, &proto.GetAuthIdentityRequest{
		HandlerName: handler,
		Scope:       scope,
	})
	if err != nil {
		return nil, fmt.Errorf("host GetAuthIdentity: %w", err)
	}
	if resp.Error != "" {
		return nil, fmt.Errorf("host GetAuthIdentity: %s", resp.Error)
	}
	return resp.Claims, nil
}

// ListAuthHandlers lists available auth handlers on the host.
func (c *HostServiceClient) ListAuthHandlers(ctx context.Context) (handlers []string, defaultHandler string, err error) {
	resp, err := c.client.ListAuthHandlers(ctx, &proto.ListAuthHandlersRequest{})
	if err != nil {
		return nil, "", fmt.Errorf("host ListAuthHandlers: %w", err)
	}
	return resp.HandlerNames, resp.DefaultHandler, nil
}

// AuthTokenRequest configures a HostServiceClient.GetAuthTokenWithOptions call.
type AuthTokenRequest struct {
	Handler            string `json:"handler" yaml:"handler" doc:"Auth handler name" maxLength:"128" example:"github"`
	Scope              string `json:"scope,omitempty" yaml:"scope,omitempty" doc:"Requested OAuth scope" maxLength:"2048" example:"read:user"`
	MinValidForSeconds int64  `json:"minValidForSeconds,omitempty" yaml:"minValidForSeconds,omitempty" doc:"Minimum remaining token validity in seconds" maximum:"86400" example:"60"`
	ForceRefresh       bool   `json:"forceRefresh,omitempty" yaml:"forceRefresh,omitempty" doc:"Bypass cached tokens and force refresh"`
	Profile            string `json:"profile,omitempty" yaml:"profile,omitempty" doc:"Auth profile override" maxLength:"128" example:"work"`
	Hostname           string `json:"hostname,omitempty" yaml:"hostname,omitempty" doc:"Cluster or server hostname for token_hostname handlers" maxLength:"512" example:"prod-east"`
}

// GetAuthToken retrieves a valid access token from the host's auth registry.
func (c *HostServiceClient) GetAuthToken(ctx context.Context, handler, scope string, minValidFor int64, forceRefresh bool) (*proto.GetAuthTokenResponse, error) {
	return c.GetAuthTokenWithOptions(ctx, AuthTokenRequest{
		Handler:            handler,
		Scope:              scope,
		MinValidForSeconds: minValidFor,
		ForceRefresh:       forceRefresh,
	})
}

// GetAuthTokenWithOptions retrieves a valid access token from the host's auth
// registry using structured options, including optional per-instance Hostname
// and Profile override (falling back to the context auth profile).
func (c *HostServiceClient) GetAuthTokenWithOptions(ctx context.Context, opts AuthTokenRequest) (*proto.GetAuthTokenResponse, error) {
	ctx = appendOutgoingLineageMetadata(ctx)
	profile := opts.Profile
	if profile == "" {
		if encoded, ok := encodeContextAuthProfile(ctx); ok {
			profile = encoded
		}
	}
	resp, err := c.client.GetAuthToken(ctx, &proto.GetAuthTokenRequest{
		HandlerName:        opts.Handler,
		Scope:              opts.Scope,
		MinValidForSeconds: opts.MinValidForSeconds,
		ForceRefresh:       opts.ForceRefresh,
		Profile:            profile,
		Hostname:           opts.Hostname,
	})
	if err != nil {
		return nil, fmt.Errorf("host GetAuthToken: %w", err)
	}
	if resp.Error != "" {
		return nil, fmt.Errorf("host GetAuthToken: %s", resp.Error)
	}
	return resp, nil
}

// GetAuthGroups retrieves group memberships for the authenticated user from
// the host's auth registry, forwarding the context auth profile.
func (c *HostServiceClient) GetAuthGroups(ctx context.Context, handler string) ([]string, error) {
	ctx = appendOutgoingLineageMetadata(ctx)
	profile, _ := encodeContextAuthProfile(ctx)
	resp, err := c.client.GetAuthGroups(ctx, &proto.GetAuthGroupsRequest{
		HandlerName: handler,
		Profile:     profile,
	})
	if err != nil {
		return nil, fmt.Errorf("host GetAuthGroups: %w", err)
	}
	if resp.Error != "" {
		return nil, fmt.Errorf("host GetAuthGroups: %s", resp.Error)
	}
	return resp.Groups, nil
}

// HostDepsFromAuthRegistry builds a HostServiceDeps that delegates auth
// callbacks to the given auth.Registry. Returns nil when the registry is nil,
// so callers can safely pass the result to WithHostDeps without a nil check.
func HostDepsFromAuthRegistry(authReg *auth.Registry) *HostServiceDeps {
	if authReg == nil {
		return nil
	}
	tokenWithHostname := func(ctx context.Context, handler, scope, hostname string, minValidFor int64, forceRefresh bool) *proto.GetAuthTokenResponse {
		if auth.RegistryFromContext(ctx) == nil {
			ctx = auth.WithRegistry(ctx, authReg)
		}
		// Resolve empty handler name to the default (first registered) handler.
		if handler == "" {
			handlers := authReg.List()
			if len(handlers) == 0 {
				return &proto.GetAuthTokenResponse{
					Error: "no auth handlers registered",
				}
			}
			handler = handlers[0]
		}
		h, err := authReg.GetContext(ctx, handler)
		if err != nil {
			return &proto.GetAuthTokenResponse{
				Error: fmt.Sprintf("auth handler %q: %v", handler, err),
			}
		}
		if rc, ok := h.(reentrantInitChecker); ok {
			if err := rc.checkReentrantInit(ctx); err != nil {
				return &proto.GetAuthTokenResponse{
					Error: fmt.Sprintf("get token from %q: %v", handler, err),
				}
			}
		}
		var resolvedHostname string
		if rawHostname := strings.TrimSpace(hostname); rawHostname != "" {
			if !auth.HasCapability(h.Capabilities(), auth.CapTokenHostname) {
				return &proto.GetAuthTokenResponse{
					Error: fmt.Sprintf("auth handler %q does not support per-request hostname %q (missing %s capability)", handler, rawHostname, auth.CapTokenHostname),
				}
			}
			resolved, rErr := hostnameresolver.Resolve(ctx, handler, rawHostname)
			if rErr != nil {
				return &proto.GetAuthTokenResponse{
					Error: fmt.Sprintf("resolve hostname %q for %q: %v", rawHostname, handler, rErr),
				}
			}
			resolvedHostname = resolved
		}
		// Clamp to non-negative so a malicious/buggy plugin cannot
		// bypass the DefaultMinValidFor fallback in token acquisition.
		if minValidFor < 0 {
			minValidFor = 0
		}
		token, err := h.GetToken(ctx, auth.TokenOptions{
			Scope:        scope,
			MinValidFor:  time.Duration(minValidFor) * time.Second,
			ForceRefresh: forceRefresh,
			Hostname:     resolvedHostname,
		})
		if err != nil {
			return &proto.GetAuthTokenResponse{
				Error: fmt.Sprintf("get token from %q: %v", handler, err),
			}
		}
		if token == nil || token.AccessToken == "" {
			return &proto.GetAuthTokenResponse{
				Error: fmt.Sprintf("get token from %q: handler returned empty token", handler),
			}
		}
		return &proto.GetAuthTokenResponse{
			AccessToken:   token.AccessToken,
			TokenType:     token.TokenType,
			ExpiresAtUnix: token.ExpiresAt.Unix(),
			Scope:         token.Scope,
		}
	}

	return &HostServiceDeps{
		// One broker per deps: the login wrapper and the HostServiceServer
		// for the same plugin client share this instance (the pointer
		// survives the struct copies WithSecretScope makes).
		PromptBroker:    &AuthPromptBroker{},
		callbackLineage: newAuthCallbackLineage(),
		AuthTokenFunc: func(ctx context.Context, handler, scope string, minValidFor int64, forceRefresh bool) (*proto.GetAuthTokenResponse, error) {
			return tokenWithHostname(ctx, handler, scope, "", minValidFor, forceRefresh), nil
		},
		AuthTokenWithHostnameFunc: func(ctx context.Context, handler, scope, hostname string, minValidFor int64, forceRefresh bool) (*proto.GetAuthTokenResponse, error) {
			return tokenWithHostname(ctx, handler, scope, hostname, minValidFor, forceRefresh), nil
		},
		AuthHandlersFunc: func(_ context.Context) ([]string, string, error) {
			handlers := authReg.List()
			var defaultHandler string
			if len(handlers) > 0 {
				// authReg.List() returns handler names in sorted order, so the
				// default is the first handler alphabetically.
				defaultHandler = handlers[0]
			}
			return handlers, defaultHandler, nil
		},
		AuthIdentityFunc: func(ctx context.Context, handler, scope string) (*proto.Claims, error) {
			// Resolve empty handler name to the default handler (the first in
			// authReg.List(), which is sorted, i.e. first alphabetically).
			if handler == "" {
				handlers := authReg.List()
				if len(handlers) == 0 {
					return nil, fmt.Errorf("no auth handlers registered")
				}
				handler = handlers[0]
			}
			h, err := authReg.Get(handler)
			if err != nil {
				return nil, fmt.Errorf("auth handler %q: %w", handler, err)
			}

			// When a scope is supplied, mint a token for that scope and derive
			// claims from the JWT. This path works even when no ID token is
			// cached (e.g. device-code sessions) because the access token
			// itself carries identity claims.
			if scope != "" {
				token, terr := h.GetToken(ctx, auth.TokenOptions{Scope: scope})
				if terr != nil {
					return nil, fmt.Errorf("get token from %q for scope %q: %w", handler, scope, terr)
				}
				if token == nil {
					return nil, fmt.Errorf("get token from %q for scope %q: handler returned no token", handler, scope)
				}
				if claims, perr := auth.ParseJWTClaims(token.AccessToken); perr == nil {
					return claimsToProto(claims), nil
				}
				// Opaque/non-JWT access token: fall back to stored status claims.
			}

			// No scope (or the scoped token was opaque): use the claims cached
			// at login time from the OIDC ID token.
			st, serr := h.Status(ctx)
			if serr != nil {
				return nil, fmt.Errorf("get auth status from %q: %w", handler, serr)
			}
			if st == nil || !st.Authenticated {
				return nil, fmt.Errorf("auth handler %q is not authenticated", handler)
			}
			if st.Claims == nil || st.Claims.IsEmpty() {
				// Authenticated, but no identity claims are available without a
				// scope. This is common for device-code sessions where no ID
				// token is stored. Return an actionable error rather than a
				// silent unauthenticated fallback.
				return nil, fmt.Errorf(
					"no identity claims available for auth handler %q without a scope; "+
						"supply a scope (e.g. inputs.scope: api://<app-id>/.default) so a "+
						"scoped token can be minted to derive identity claims", handler,
				)
			}
			return claimsToProto(st.Claims), nil
		},
		AuthGroupsFunc: func(ctx context.Context, handler string) ([]string, error) {
			// Resolve empty handler name to the default handler (the first in
			// authReg.List(), which is sorted, i.e. first alphabetically).
			if handler == "" {
				handlers := authReg.List()
				if len(handlers) == 0 {
					return nil, fmt.Errorf("no auth handlers registered")
				}
				handler = handlers[0]
			}
			h, err := authReg.Get(handler)
			if err != nil {
				return nil, fmt.Errorf("auth handler %q: %w", handler, err)
			}
			// Group membership is served via the optional GroupsProvider
			// interface (a separate paginated callback), not the claim set,
			// because Entra emits a _claim_names.groups overage past 200 groups.
			gp, ok := h.(auth.GroupsProvider)
			if !ok {
				return nil, fmt.Errorf("auth handler %q does not support group membership queries (GroupsProvider not implemented)", handler)
			}
			groups, err := gp.GetGroups(ctx)
			if err != nil {
				return nil, fmt.Errorf("get groups from %q: %w", handler, err)
			}
			return groups, nil
		},
	}
}

// AuthClientOptsFromContext extracts the auth registry and secret store from ctx
// and returns ClientOption(s) that wire host-side deps into plugin clients.
// Returns nil when neither an auth registry nor a secret store is in context.
func AuthClientOptsFromContext(ctx context.Context) []ClientOption {
	authReg := auth.RegistryFromContext(ctx)
	secretStore := secrets.FromContext(ctx)
	if authReg == nil && secretStore == nil {
		return nil
	}
	var deps *HostServiceDeps
	if authReg != nil {
		deps = HostDepsFromAuthRegistry(authReg)
		if deps != nil {
			// Wire profile resolution so plugin providers inherit the host's active
			// auth profile (from --auth-profile flag or config activeProfile).
			deps.ProfileResolverFunc = func(handlerName string) string {
				return auth.ResolveActiveProfile(ctx, handlerName)
			}
		}
	}
	if deps == nil {
		deps = &HostServiceDeps{}
	}
	if secretStore != nil {
		deps.SecretStore = secretStore
	}
	deps.HostConfig = config.FromContext(ctx)
	deps.providerDeniedPrefixes = deniedSecretPrefixesFromContext(ctx)
	return []ClientOption{WithHostDeps(deps)}
}

func deniedSecretPrefixesFromContext(ctx context.Context) []string {
	prefixes := []string{secrets.InternalSecretPrefix}
	add := func(p string) {
		if p == "" {
			return
		}
		for _, existing := range prefixes {
			if existing == p {
				return
			}
		}
		prefixes = append(prefixes, p)
	}
	add(secrets.InternalSecretPrefixFor(paths.AppName()))
	if bin := settings.BinaryNameFromContext(ctx); bin != "" {
		add(secrets.InternalSecretPrefixFor(settings.SanitizeBinaryName(bin)))
		add(secrets.InternalSecretPrefixFor(bin))
	}
	return prefixes
}

// providerPluginHostDeps returns a shallow copy of src enforcing read-only,
// internal-prefix-isolated secret store access for ProviderPlugin clients,
// without mutating src (which may also be passed to AuthHandlerPlugin clients).
func providerPluginHostDeps(src *HostServiceDeps) *HostServiceDeps {
	if src == nil {
		return nil
	}
	cp := *src
	cp.ReadOnlySecrets = true
	cp.callbackLineage = nil

	denied := make([]string, 0, len(src.DeniedSecretPrefixes)+len(src.providerDeniedPrefixes)+2)
	add := func(p string) {
		if p == "" {
			return
		}
		for _, existing := range denied {
			if existing == p {
				return
			}
		}
		denied = append(denied, p)
	}
	add(secrets.InternalSecretPrefix)
	add(secrets.InternalSecretPrefixFor(paths.AppName()))
	for _, p := range src.DeniedSecretPrefixes {
		add(p)
	}
	for _, p := range src.providerDeniedPrefixes {
		add(p)
	}
	cp.DeniedSecretPrefixes = denied
	return &cp
}
