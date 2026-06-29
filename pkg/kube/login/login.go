// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package login orchestrates Kubernetes / OpenShift login: it resolves a
// cluster's connection details, runs an auth handler's interactive login, and
// writes a kubeconfig exec-credential entry that invokes the host binary as a
// client-go credential plugin on every subsequent kubectl/oc call.
//
// The package is dependency-light: it carries no client-go. The heavy
// kubeconfig machinery lives behind the kubeconfig provider plugin (driven via
// the KubeconfigWriter interface, satisfied by *kubeconfig.Manager). When that
// provider is unavailable, login falls back to writing a minimal static
// kubeconfig with a hand-rolled YAML writer (see fallback.go).
package login

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/kube"
	"github.com/oakwood-commons/scafctl/pkg/kubeconfig"
	"github.com/oakwood-commons/scafctl/pkg/settings"
)

// Exec-credential argument fragments baked into the kubeconfig exec block. The
// runtime command is "<binary> auth token <handler> --exec-credential [...]".
const (
	execAuthCmd  = "auth"
	execTokenCmd = "token"
	// execCredentialFlag is a CLI flag name passed to the exec plugin, not a secret.
	execCredentialFlag = "--exec-credential" //nolint:gosec // G101: flag name, not a credential
	execScopeFlag      = "--scope"
	execProfileFlag    = "--profile"
)

// Sentinel errors returned by the orchestration.
var (
	// ErrNoHandler indicates no auth handler could be determined from an
	// explicit handler, the request handler name, or the resolved cluster's
	// DefaultHandler.
	ErrNoHandler = errors.New("login: auth handler is required; pass a handler or configure the cluster's default handler")

	// ErrNoServer indicates no cluster API server could be resolved from a
	// cluster resolver or an explicit --server flag.
	ErrNoServer = errors.New("login: no cluster server resolved; provide --server or configure a cluster resolver")

	// ErrNoCluster indicates a logout request did not identify a cluster.
	ErrNoCluster = errors.New("login: cluster name is required")

	// ErrNoKubeconfigWriter indicates the request did not supply a kubeconfig
	// writer dependency.
	ErrNoKubeconfigWriter = errors.New("login: kubeconfig writer is required")

	// ErrVerificationFailed indicates a --verify login could not confirm the
	// authenticated identity via a post-login whoami. The kubeconfig entry is
	// still written; only the verification step failed.
	ErrVerificationFailed = errors.New("login: post-login identity verification failed")
)

// Authenticator is the subset of auth.Handler the login orchestration needs.
// *auth.Handler implementations satisfy it; tests supply a stub.
type Authenticator interface {
	Name() string
	DisplayName() string
	Login(ctx context.Context, opts auth.LoginOptions) (*auth.Result, error)
	GetToken(ctx context.Context, opts auth.TokenOptions) (*auth.Token, error)
	Logout(ctx context.Context) error
	Capabilities() []auth.Capability
}

// KubeconfigWriter is the subset of *kubeconfig.Manager the orchestration
// needs. Implementations return kubeconfig.ErrProviderUnavailable to signal that
// the caller should fall back to a static kubeconfig write.
type KubeconfigWriter interface {
	WriteKubeconfig(ctx context.Context, in kubeconfig.WriteInput) (kubeconfig.WriteResult, error)
	RemoveEntry(ctx context.Context, in kubeconfig.RemoveInput) (kubeconfig.RemoveResult, error)
	DetectAuthType(ctx context.Context, in kubeconfig.DetectInput) (kubeconfig.DetectResult, error)
	Whoami(ctx context.Context, in kubeconfig.WhoamiInput) (kubeconfig.WhoamiResult, error)
}

// Deps carries the collaborators the orchestration depends on. Resolver is
// optional: when nil, cluster details come from explicit flags and
// auto-detection.
type Deps struct {
	// Handler authenticates the user and supplies tokens. When nil, the handler
	// is resolved via HandlerLookup using the request handler name or the
	// resolved cluster's DefaultHandler.
	Handler Authenticator

	// HandlerLookup resolves an auth handler by name. It is consulted only when
	// Handler is nil, so the handler can be chosen from the resolved cluster's
	// DefaultHandler. The CLI passes a thin wrapper around auth.GetHandler.
	HandlerLookup func(ctx context.Context, name string) (Authenticator, error)

	// Kubeconfig drives kubeconfig reads/writes via the provider.
	Kubeconfig KubeconfigWriter

	// Resolver resolves cluster names to connection details. Optional.
	Resolver kube.ClusterResolver

	// BinaryName is the host binary baked into the kubeconfig exec command.
	// Empty falls back to settings.CliBinaryName.
	BinaryName string
}

// Request configures a login.
type Request struct {
	// Cluster is the logical cluster name resolved via the cluster resolver.
	Cluster string

	// Handler is the auth handler name. When empty, the handler is taken from
	// the resolved cluster's DefaultHandler. The resolved handler is supplied
	// via Deps.Handler or Deps.HandlerLookup.
	Handler string

	// Server overrides the resolved API server URL.
	Server string

	// Audience overrides the resolved OIDC audience.
	Audience string

	// ClusterName, ContextName, and UserName name the kubeconfig entries. Empty
	// values default to the cluster name.
	ClusterName string
	ContextName string
	UserName    string

	// KubeconfigPath is the kubeconfig file to write. Empty resolves KUBECONFIG
	// or ~/.kube/config.
	KubeconfigPath string

	// Profile is the auth profile baked into the exec args. Empty omits it.
	Profile string

	// SetCurrentContext marks the written context as current-context.
	SetCurrentContext bool

	// InsecureSkipTLS disables API server TLS verification (development only).
	InsecureSkipTLS bool

	// Verify requires a successful post-login whoami; when set and the whoami
	// does not succeed, Login returns ErrVerificationFailed. Verification needs
	// the kubeconfig provider and is unavailable in the static-fallback path.
	Verify bool

	// Timeout bounds the interactive login. Zero uses the handler default.
	Timeout time.Duration
}

// Result reports the outcome of a login.
type Result struct {
	// Handler is the auth handler that authenticated the user.
	Handler string

	// ContextName is the kubeconfig context that was written.
	ContextName string

	// KubeconfigPath is the kubeconfig file that was written.
	KubeconfigPath string

	// Server is the cluster API server URL.
	Server string

	// AuthType is the resolved (or detected) authentication method.
	AuthType kube.AuthType

	// Username and Groups come from a best-effort whoami; empty when whoami did
	// not run or did not succeed.
	Username string
	Groups   []string

	// ExpiresAt is the login token's expiry, when known.
	ExpiresAt time.Time

	// UsedFallback reports that the static kubeconfig writer was used because
	// the kubeconfig provider was unavailable.
	UsedFallback bool
}

// Login resolves the cluster, runs the handler login, and writes a kubeconfig
// exec-credential entry (falling back to a static write when the provider is
// unavailable). A best-effort whoami populates the subject identity. When
// req.Verify is set, a whoami that does not succeed makes Login return
// ErrVerificationFailed (the kubeconfig entry is still written).
func Login(ctx context.Context, deps Deps, req Request) (*Result, error) {
	if deps.Handler == nil && deps.HandlerLookup == nil {
		return nil, ErrNoHandler
	}
	if deps.Kubeconfig == nil {
		return nil, ErrNoKubeconfigWriter
	}
	binaryName := deps.BinaryName
	if binaryName == "" {
		binaryName = settings.CliBinaryName
	}

	// Apply the requested profile to the context so the initial handler login
	// and the post-login whoami store/read credentials under the same profile
	// that the written kubeconfig exec block passes via --profile. Without this
	// the login would write tokens to the active profile while kubectl later
	// looks them up under req.Profile and fails to mint credentials.
	if req.Profile != "" {
		ctx = auth.WithProfile(ctx, req.Profile)
	}

	info, err := resolveCluster(ctx, deps, req)
	if err != nil {
		return nil, err
	}

	handler, err := resolveHandler(ctx, deps, req, info)
	if err != nil {
		return nil, err
	}

	loginOpts := auth.LoginOptions{Timeout: req.Timeout}
	if info.OIDCAudience != "" && auth.HasCapability(handler.Capabilities(), auth.CapScopesOnLogin) {
		loginOpts.Scopes = []string{info.OIDCAudience}
	}
	if _, err := handler.Login(ctx, loginOpts); err != nil {
		return nil, fmt.Errorf("login with handler %q: %w", handler.Name(), err)
	}

	// Name the kubeconfig entries. Unlike resolveCluster (which derives the
	// logical identity used to look the cluster up, so req.Cluster wins), the
	// explicit --cluster-name flag takes precedence here because its sole
	// purpose is to override the written kubeconfig entry name.
	clusterName := firstNonEmpty(req.ClusterName, req.Cluster, info.Name)
	contextName := firstNonEmpty(req.ContextName, clusterName)
	userName := firstNonEmpty(req.UserName, clusterName)

	writeIn := kubeconfig.WriteInput{
		Server:             info.APIServerURL,
		Audience:           info.OIDCAudience,
		ClusterName:        clusterName,
		ContextName:        contextName,
		UserName:           userName,
		KubeconfigPath:     req.KubeconfigPath,
		ExecCommand:        binaryName,
		ExecArgs:           buildExecArgs(handler, info, req.Profile),
		InteractiveMode:    interactiveModeFor(info.AuthType),
		InstallHint:        buildInstallHint(binaryName),
		ProvideClusterInfo: false,
		CAData:             info.CAData,
		InsecureSkipTLS:    info.InsecureSkipTLS,
		SetCurrentContext:  req.SetCurrentContext,
	}

	result := &Result{
		Handler:  handler.Name(),
		Server:   info.APIServerURL,
		AuthType: info.AuthType,
	}

	writeRes, err := deps.Kubeconfig.WriteKubeconfig(ctx, writeIn)
	switch {
	case err == nil:
		result.ContextName = writeRes.ContextName
		result.KubeconfigPath = writeRes.KubeconfigPath
	case errors.Is(err, kubeconfig.ErrProviderUnavailable):
		fbRes, fbErr := writeStaticKubeconfig(writeIn)
		if fbErr != nil {
			return nil, fmt.Errorf("kubeconfig provider unavailable and static fallback failed: %w", fbErr)
		}
		result.ContextName = fbRes.ContextName
		result.KubeconfigPath = fbRes.KubeconfigPath
		result.UsedFallback = true
	default:
		return nil, fmt.Errorf("write kubeconfig: %w", err)
	}

	verified := populateIdentity(ctx, deps, handler, info, result)
	if req.Verify && !verified {
		return result, ErrVerificationFailed
	}
	return result, nil
}

// resolveHandler returns the authenticator to use. An explicit Deps.Handler wins;
// otherwise the handler name comes from the request (explicit --handler) or the
// resolved cluster's DefaultHandler, and is looked up via Deps.HandlerLookup.
func resolveHandler(ctx context.Context, deps Deps, req Request, info kube.ClusterInfo) (Authenticator, error) {
	if deps.Handler != nil {
		return deps.Handler, nil
	}
	name := firstNonEmpty(req.Handler, info.DefaultHandler)
	if name == "" {
		return nil, ErrNoHandler
	}
	if deps.HandlerLookup == nil {
		return nil, ErrNoHandler
	}
	handler, err := deps.HandlerLookup(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("resolve auth handler %q: %w", name, err)
	}
	return handler, nil
}

// populateIdentity runs a best-effort whoami so the result reports the subject.
// It reports whether the whoami succeeded. Every failure here is non-fatal to
// the kubeconfig write (it is already done); the caller decides whether an
// unsuccessful whoami matters (see req.Verify).
func populateIdentity(ctx context.Context, deps Deps, handler Authenticator, info kube.ClusterInfo, result *Result) bool {
	token, err := handler.GetToken(ctx, tokenOptionsFor(handler, info))
	if err != nil || token == nil {
		return false
	}
	result.ExpiresAt = token.ExpiresAt
	who, err := deps.Kubeconfig.Whoami(ctx, kubeconfig.WhoamiInput{
		Server:          info.APIServerURL,
		Token:           token.AccessToken,
		Audience:        info.OIDCAudience,
		CAData:          info.CAData,
		InsecureSkipTLS: info.InsecureSkipTLS,
	})
	if err != nil || !who.Success {
		return false
	}
	result.Username = who.Username
	result.Groups = who.Groups
	return true
}

// tokenOptionsFor builds the token request, passing the cluster audience as a
// scope only for handlers that accept per-request scopes.
func tokenOptionsFor(h Authenticator, info kube.ClusterInfo) auth.TokenOptions {
	opts := auth.TokenOptions{}
	if info.OIDCAudience != "" && auth.HasCapability(h.Capabilities(), auth.CapScopesOnTokenRequest) {
		opts.Scope = info.OIDCAudience
	}
	return opts
}

// firstNonEmpty returns the first non-empty string, or "" when all are empty.
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
