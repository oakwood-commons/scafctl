// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package gitcredential

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/config"
)

// ResolvedRoute describes the auth handler, profile, scope, and username
// overrides matched for a Git credential request.
type ResolvedRoute struct {
	HandlerName  string
	Profile      string
	Scope        string
	RuleUsername string
	FromRule     bool
}

// MatchHandler resolves whether req routes to an auth handler and computes the
// effective profile and scope according to precedence:
//   - Handler: handlerOverride > auth.git.rules > InferGitAuthHandler
//   - Profile: profileOverride > GlobalProfileFromContext > rule.Profile > SingleMatchingGHESProfile > ResolveActiveProfile
//   - Scope:   rule.Scope > InferGitDefaultScope
func MatchHandler(ctx context.Context, req *Request, cfg *config.Config, handlerOverride, profileOverride string) (*ResolvedRoute, bool) {
	route, ok, _ := matchHandlerWithValidation(ctx, req, cfg, handlerOverride, profileOverride)
	return route, ok
}

func matchHandlerWithValidation(ctx context.Context, req *Request, cfg *config.Config, handlerOverride, profileOverride string) (*ResolvedRoute, bool, error) {
	if req == nil || strings.TrimSpace(req.Host) == "" {
		return nil, false, nil
	}
	proto := effectiveMatchProtocol(req.Protocol)
	normHost := NormalizeHostForProtocol(proto, req.Host)

	var rules []config.GitCredentialRule
	if cfg != nil && cfg.Auth.Git != nil {
		rules = cfg.Auth.Git.Rules
	}
	matchedRule, hasRule := MatchRuleForProtocol(proto, normHost, req.Path, rules)

	var handlerName string
	overrideHandler := strings.TrimSpace(handlerOverride)
	switch {
	case overrideHandler != "":
		handlerName = overrideHandler
		if hasRule && matchedRule.Handler != handlerName {
			matchedRule = nil
			hasRule = false
		}
	case hasRule:
		handlerName = matchedRule.Handler
	default:
		handlerName = InferGitAuthHandlerForProtocol(proto, normHost, cfg)
	}

	if handlerName == "" {
		return nil, false, nil
	}

	profile, profileErr := resolveGitProfileValidated(ctx, proto, normHost, handlerName, cfg, matchedRule, profileOverride)

	var scope string
	if matchedRule != nil && matchedRule.Scope != "" {
		scope = matchedRule.Scope
	} else {
		scope = InferGitDefaultScope(handlerName)
	}

	var ruleUsername string
	if matchedRule != nil {
		ruleUsername = matchedRule.Username
	}

	return &ResolvedRoute{
		HandlerName:  handlerName,
		Profile:      profile,
		Scope:        scope,
		RuleUsername: ruleUsername,
		FromRule:     hasRule,
	}, true, profileErr
}

func resolveGitProfileValidated(ctx context.Context, proto, host, handlerName string, cfg *config.Config, matchedRule *config.GitCredentialRule, profileOverride string) (string, error) {
	if strings.TrimSpace(profileOverride) != "" {
		norm := auth.NormalizeProfileName(strings.TrimSpace(profileOverride))
		if norm != "" {
			if err := auth.ValidateProfileName(norm); err != nil {
				return "", fmt.Errorf("invalid --profile: %w", err)
			}
		}
		return norm, nil
	}

	if gp := auth.GlobalProfileFromContext(ctx); gp != "" {
		norm := auth.NormalizeProfileName(gp)
		if norm != "" {
			if err := auth.ValidateProfileName(norm); err != nil {
				return "", fmt.Errorf("invalid auth profile: %w", err)
			}
		}
		return norm, nil
	}

	if matchedRule != nil && matchedRule.Profile != "" {
		norm := auth.NormalizeProfileName(matchedRule.Profile)
		if norm != "" {
			if err := auth.ValidateProfileName(norm); err != nil {
				return "", fmt.Errorf("invalid profile in auth.git.rules: %w", err)
			}
		}
		return norm, nil
	}

	if handlerName == "github" {
		ghesProfile, ghesErr := singleMatchingGHESProfileValidated(proto, host, cfg)
		if ghesErr != nil {
			return "", ghesErr
		}
		if ghesProfile != "" {
			return ghesProfile, nil
		}
	}

	resolveCtx := ctx
	if cfg != nil && config.FromContext(resolveCtx) == nil {
		resolveCtx = config.WithConfig(resolveCtx, cfg)
	}

	if cp := auth.ProfileFromContext(resolveCtx); cp != "" {
		norm := auth.NormalizeProfileName(cp)
		if norm != "" {
			if err := auth.ValidateProfileName(norm); err != nil {
				return "", fmt.Errorf("invalid auth profile: %w", err)
			}
		}
		return norm, nil
	}
	if auth.IsProfileResolved(resolveCtx) {
		return "", nil
	}

	if rawActive := rawConfiguredActiveProfile(config.FromContext(resolveCtx), handlerName); rawActive != "" {
		norm := auth.NormalizeProfileName(rawActive)
		if norm != "" {
			if err := auth.ValidateProfileName(norm); err != nil {
				return "", fmt.Errorf("invalid activeProfile for handler %q: %w", handlerName, err)
			}
		}
		return norm, nil
	}

	return auth.ResolveActiveProfile(resolveCtx, handlerName), nil
}

func rawConfiguredActiveProfile(cfg *config.Config, handlerName string) string {
	if cfg == nil {
		return ""
	}
	switch handlerName {
	case "entra":
		if cfg.Auth.Entra != nil {
			return cfg.Auth.Entra.ActiveProfile
		}
	case "github":
		if cfg.Auth.GitHub != nil {
			return cfg.Auth.GitHub.ActiveProfile
		}
	case "gcp":
		if cfg.Auth.GCP != nil {
			return cfg.Auth.GCP.ActiveProfile
		}
	default:
		if hc, ok := cfg.Auth.Handlers[handlerName]; ok {
			if hc.ActiveProfile != "" {
				return hc.ActiveProfile
			}
			for k, v := range hc.Settings {
				if strings.EqualFold(k, "activeProfile") || strings.EqualFold(k, "active_profile") {
					if s, isStr := v.(string); isStr && s != "" {
						return s
					}
				}
			}
		}
	}
	return ""
}

// ResolverOption configures an AuthTokenResolver.
type ResolverOption func(*AuthTokenResolver)

// WithConfig attaches application configuration for auth.git.rules, GHES
// hostnames, and CustomOAuth2 GitHost mappings.
func WithConfig(cfg *config.Config) ResolverOption {
	return func(r *AuthTokenResolver) { r.cfg = cfg }
}

// WithHandlerOverride forces a specific auth handler name (--handler).
func WithHandlerOverride(handler string) ResolverOption {
	return func(r *AuthTokenResolver) { r.handlerOverride = strings.TrimSpace(handler) }
}

// WithProfileOverride forces a specific auth profile (--profile).
func WithProfileOverride(profile string) ResolverOption {
	return func(r *AuthTokenResolver) { r.profileOverride = strings.TrimSpace(profile) }
}

// AuthTokenResolver resolves Git HTTPS credentials dynamically from registered
// auth handlers.
type AuthTokenResolver struct {
	registry        *auth.Registry
	cfg             *config.Config
	handlerOverride string
	profileOverride string
}

// NewAuthTokenResolver creates an AuthTokenResolver backed by registry.
func NewAuthTokenResolver(registry *auth.Registry, opts ...ResolverOption) *AuthTokenResolver {
	r := &AuthTokenResolver{registry: registry}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// MatchHandler reports whether req maps to an auth handler under this resolver's
// overrides and configuration.
func (r *AuthTokenResolver) MatchHandler(ctx context.Context, req *Request) (*ResolvedRoute, bool) {
	if r == nil {
		return nil, false
	}
	cfg := r.effectiveConfig(ctx)
	return MatchHandler(ctx, req, cfg, r.handlerOverride, r.profileOverride)
}

func (r *AuthTokenResolver) effectiveConfig(ctx context.Context) *config.Config {
	if r.cfg != nil {
		return r.cfg
	}
	return config.FromContext(ctx)
}

// Resolve acquires a fresh token for req from the matched auth handler and
// builds a Git credential Response.
func (r *AuthTokenResolver) Resolve(ctx context.Context, req *Request) (*Response, error) {
	if req == nil || strings.TrimSpace(req.Host) == "" {
		return nil, ErrMissingHost
	}
	if err := ValidateHostAttribute(req.Host); err != nil {
		return nil, err
	}
	if err := ValidateRepoPath(req.Path); err != nil {
		return nil, err
	}
	if r.profileOverride != "" {
		if norm := auth.NormalizeProfileName(r.profileOverride); norm != "" {
			if err := auth.ValidateProfileName(norm); err != nil {
				return nil, fmt.Errorf("invalid --profile: %w", err)
			}
		}
	}

	cfg := r.effectiveConfig(ctx)
	route, ok, profileErr := matchHandlerWithValidation(ctx, req, cfg, r.handlerOverride, r.profileOverride)
	if !ok {
		return nil, ErrCredentialsNotFound
	}
	if profileErr != nil {
		return nil, profileErr
	}
	if r.registry == nil {
		return nil, fmt.Errorf("auth registry not available for handler %q", route.HandlerName)
	}

	handler, err := r.registry.GetContext(ctx, route.HandlerName)
	if err != nil {
		return nil, fmt.Errorf("auth handler %q not available: %w", route.HandlerName, err)
	}

	// Inject the resolved profile and mark resolution complete so downstream
	// handlers never re-resolve and overwrite an explicit selection.
	if cfg != nil && config.FromContext(ctx) == nil {
		ctx = config.WithConfig(ctx, cfg)
	}
	ctx = auth.WithProfile(ctx, route.Profile)
	ctx = auth.WithProfileResolved(ctx)

	var tokenOpts auth.TokenOptions
	caps := handler.Capabilities()
	if route.Scope != "" && (len(caps) == 0 || auth.HasCapability(caps, auth.CapScopesOnTokenRequest)) {
		tokenOpts.Scope = route.Scope
	}
	if req.Host != "" && auth.HasCapability(caps, auth.CapTokenHostname) {
		proto := strings.ToLower(strings.TrimSpace(req.Protocol))
		if proto == "" {
			proto = "https"
		}
		if origin, originErr := CanonicalURL(proto, req.Host, ""); originErr == nil {
			tokenOpts.Hostname = origin
		} else {
			tokenOpts.Hostname = proto + "://" + NormalizeHostForProtocol(proto, req.Host)
		}
	}

	serverURL := displayTargetURL(req)
	token, err := handler.GetToken(ctx, tokenOpts)
	if err != nil {
		return nil, &ReauthRequiredError{
			Handler:   route.HandlerName,
			Profile:   route.Profile,
			ServerURL: serverURL,
			Err:       fmt.Errorf("get token for %q: %w", serverURL, err),
		}
	}
	if token == nil || token.AccessToken == "" {
		return nil, &ReauthRequiredError{
			Handler:   route.HandlerName,
			Profile:   route.Profile,
			ServerURL: serverURL,
			Err:       fmt.Errorf("auth handler %q returned an empty access token", route.HandlerName),
		}
	}

	username := ResolveGitUsername(ctx, handler, route.RuleUsername, req.Username)
	resp := &Response{
		Protocol: strings.ToLower(strings.TrimSpace(req.Protocol)),
		Host:     req.Host,
		Path:     req.Path,
		Username: username,
		Password: token.AccessToken,
	}
	if !token.ExpiresAt.IsZero() && token.ExpiresAt.After(time.Now()) {
		exp := token.ExpiresAt.UTC()
		resp.PasswordExpiryUTC = &exp
	}
	return resp, nil
}

func displayTargetURL(req *Request) string {
	if req == nil {
		return ""
	}
	proto := strings.ToLower(strings.TrimSpace(req.Protocol))
	if proto == "" {
		proto = "https"
	}
	if u, err := CanonicalURL(proto, req.Host, req.Path); err == nil {
		return u
	}
	if req.Host != "" {
		return proto + "://" + NormalizeHostForProtocol(proto, req.Host)
	}
	return ""
}
