// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package gitcredential

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/secrets"
	"github.com/oakwood-commons/scafctl/pkg/settings"
)

const (
	// gitCredSecretNamespace is appended after "<binary>." so stored Git fallback
	// credentials live under the reserved internal secret prefix and are blocked
	// from scrt: ValueRefs and plugin HostService access.
	gitCredSecretNamespace = "gitcred." //nolint:gosec // namespace segment, not a credential
)

type storedCredential struct {
	CanonicalURL      string     `json:"canonicalUrl"`
	Protocol          string     `json:"protocol"`
	Host              string     `json:"host"`
	Path              string     `json:"path,omitempty"`
	Username          string     `json:"username"`
	Password          string     `json:"password"` //nolint:gosec // encrypted in secrets.Store
	PasswordExpiryUTC *time.Time `json:"passwordExpiryUtc,omitempty"`
	StoredAtUnixNano  int64      `json:"storedAtUnixNano,omitempty"`
}

func decodeStoredCredential(data []byte) (*storedCredential, error) {
	var entry storedCredential
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, err
	}
	if entry.Password == "" {
		return nil, fmt.Errorf("stored git credential has empty password")
	}
	return &entry, nil
}

// SecretKeyPrefixFor returns the internal secret key prefix "<binary>.gitcred."
// for the given CLI binary name.
func SecretKeyPrefixFor(binaryName string) string {
	bin := settings.SanitizeBinaryName(strings.TrimSpace(binaryName))
	if bin == "" {
		bin = settings.CliBinaryName
	}
	return secrets.InternalSecretPrefixFor(bin) + gitCredSecretNamespace
}

// SecretKeyURLPrefix returns the per-URL namespace "<binary>.gitcred.u.<urlHash>."
// shared by all stored accounts for canonicalURL so each (canonicalURL, username)
// pair is written and deleted as an independent atomic secret key without
// read-modify-write races across goroutines or processes.
func SecretKeyURLPrefix(binaryName, canonicalURL string) string {
	sum := sha256.Sum256([]byte(canonicalURL))
	return SecretKeyPrefixFor(binaryName) + "u." + hex.EncodeToString(sum[:16]) + "."
}

// SecretKeyForAccount builds a lowercase secrets.Store-compliant key
// ("^[a-z0-9._-]{1,255}$") isolated per (canonicalURL, username) under
// "<binary>.gitcred.u.<urlHash>." so case-insensitive filesystems (APFS/NTFS)
// never collide on mixed-case encodings and filenames never expose usernames.
func SecretKeyForAccount(binaryName, canonicalURL, username string) string {
	urlPrefix := SecretKeyURLPrefix(binaryName, canonicalURL)
	if username == "" {
		return urlPrefix + "anon"
	}
	uSum := sha256.Sum256([]byte(username))
	return urlPrefix + "h." + hex.EncodeToString(uSum[:])
}

// SecretKeyForURL returns the anonymous account secret key for canonicalURL.
func SecretKeyForURL(binaryName, canonicalURL string) string {
	return SecretKeyForAccount(binaryName, canonicalURL, "")
}

// Option configures a Helper.
type Option func(*Helper)

// WithBinaryName configures the CLI binary name used for internal secret keys.
func WithBinaryName(name string) Option {
	return func(h *Helper) {
		sanitized := settings.SanitizeBinaryName(strings.TrimSpace(name))
		if sanitized != "" {
			h.binaryName = sanitized
		}
	}
}

// WithAllowInsecureHTTP allows serving or storing credentials over plain HTTP
// to non-loopback hosts when explicitly enabled in config.
func WithAllowInsecureHTTP(allow bool) Option {
	return func(h *Helper) { h.allowInsecureHTTP = allow }
}

// WithTokenResolver attaches a dynamic AuthTokenResolver to the Helper.
func WithTokenResolver(resolver *AuthTokenResolver) Option {
	return func(h *Helper) { h.tokenResolver = resolver }
}

// Helper implements the Git credential helper operations (get, store, erase).
type Helper struct {
	store             secrets.Store
	binaryName        string
	allowInsecureHTTP bool
	tokenResolver     *AuthTokenResolver
	lastStoredNano    atomic.Int64
}

// New creates a Git credential Helper backed by store and optional dynamic auth resolution.
func New(store secrets.Store, opts ...Option) *Helper {
	h := &Helper{
		store:      store,
		binaryName: settings.CliBinaryName,
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

func (h *Helper) nextStoredTimestampNano() int64 {
	now := time.Now().UTC().UnixNano()
	for {
		prev := h.lastStoredNano.Load()
		next := now
		if next <= prev {
			next = prev + 1
		}
		if h.lastStoredNano.CompareAndSwap(prev, next) {
			return next
		}
	}
}

func (h *Helper) effectiveAllowInsecureHTTP(ctx context.Context) bool {
	if h.allowInsecureHTTP {
		return true
	}
	if cfg := config.FromContext(ctx); cfg != nil && cfg.Auth.Git != nil {
		return cfg.Auth.Git.AllowInsecureHTTP
	}
	return false
}

// MatchesAuthHandler reports whether req maps to a dynamic authentication handler
// rather than static secret storage.
func (h *Helper) MatchesAuthHandler(ctx context.Context, req *Request) bool {
	if req == nil || strings.TrimSpace(req.Host) == "" {
		return false
	}
	if h.tokenResolver != nil {
		_, ok := h.tokenResolver.MatchHandler(ctx, req)
		return ok
	}
	_, ok := MatchHandler(ctx, req, config.FromContext(ctx), "", "")
	return ok
}

// IsMutationNoOp reports whether a store (isStore=true) or erase (isStore=false)
// request requires no secrets.Store access (e.g. missing host, empty password
// on store, or an auth-handler-managed host).
func (h *Helper) IsMutationNoOp(ctx context.Context, req *Request, isStore bool) bool {
	if req == nil || strings.TrimSpace(req.Host) == "" {
		return true
	}
	if isStore && req.Password == "" {
		return true
	}
	return h.MatchesAuthHandler(ctx, req)
}

// Get resolves credentials for req, checking dynamic auth handlers first and
// falling back to internal "<binary>.gitcred.*" secrets for non-handler hosts.
func (h *Helper) Get(ctx context.Context, req *Request) (*Response, error) {
	if req == nil || strings.TrimSpace(req.Host) == "" {
		return nil, ErrMissingHost
	}
	if err := ValidateHostAttribute(req.Host); err != nil {
		return nil, err
	}
	if err := ValidateRepoPath(req.Path); err != nil {
		return nil, err
	}
	if err := ValidateTransport(req.Protocol, req.Host, h.effectiveAllowInsecureHTTP(ctx)); err != nil {
		return nil, err
	}

	matchedHandler := false
	if h.tokenResolver != nil {
		_, matchedHandler = h.tokenResolver.MatchHandler(ctx, req)
		resp, err := h.tokenResolver.Resolve(ctx, req)
		if err == nil && resp != nil {
			return resp, nil
		}
		if !errors.Is(err, ErrCredentialsNotFound) {
			var reauth *ReauthRequiredError
			if errors.As(err, &reauth) {
				return nil, reauth
			}
			return nil, err
		}
	}

	if !matchedHandler && h.store != nil {
		candidates, err := candidateCanonicalURLs(req.Protocol, req.Host, req.Path)
		if err != nil {
			return nil, err
		}
		now := time.Now()
		hasUsername := req.EffectiveHasUsername()

		var allKeys []string
		if !hasUsername {
			allKeys, err = h.store.List(ctx)
			if err != nil {
				return nil, fmt.Errorf("list stored git credentials: %w", err)
			}
		}

		for _, candidateURL := range candidates {
			_, stored, findErr := h.selectStoredAccount(ctx, candidateURL, req.Username, hasUsername, "", allKeys, now, true)
			if findErr != nil {
				return nil, findErr
			}
			if stored != nil {
				return &Response{
					Protocol:          strings.ToLower(strings.TrimSpace(req.Protocol)),
					Host:              req.Host,
					Path:              req.Path,
					Username:          stored.Username,
					Password:          stored.Password,
					PasswordExpiryUTC: stored.PasswordExpiryUTC,
				}, nil
			}
		}
	}

	return nil, ErrCredentialsNotFound
}

// Store persists static Git credentials for non-handler hosts under an isolated
// "<binary>.gitcred.u.<urlHash>.<account>" key coordinated by a per-account lock
// so concurrent Store and Erase calls never race. When req matches an auth
// handler, Store is a strict no-op.
func (h *Helper) Store(ctx context.Context, req *Request) error {
	if req == nil || strings.TrimSpace(req.Host) == "" || req.Password == "" {
		return nil
	}
	if err := ValidateHostAttribute(req.Host); err != nil {
		return err
	}
	if err := ValidateRepoPath(req.Path); err != nil {
		return err
	}
	if err := ValidateTransport(req.Protocol, req.Host, h.effectiveAllowInsecureHTTP(ctx)); err != nil {
		return err
	}
	if h.MatchesAuthHandler(ctx, req) {
		return nil
	}
	if h.store == nil {
		return fmt.Errorf("secrets store is not available")
	}

	canonicalURL, err := CanonicalURL(req.Protocol, req.Host, req.Path)
	if err != nil {
		return err
	}

	key := SecretKeyForAccount(h.binaryName, canonicalURL, req.Username)
	unlock, err := acquireAccountMutationLock(ctx, h.store, key)
	if err != nil {
		return err
	}
	defer unlock()

	entry := storedCredential{
		CanonicalURL:      canonicalURL,
		Protocol:          strings.ToLower(strings.TrimSpace(req.Protocol)),
		Host:              NormalizeHostForProtocol(req.Protocol, req.Host),
		Path:              CleanRepoPath(req.Path),
		Username:          req.Username,
		Password:          req.Password,
		PasswordExpiryUTC: req.PasswordExpiryUTC,
		StoredAtUnixNano:  h.nextStoredTimestampNano(),
	}
	// #nosec G117 -- marshaling into encrypted secrets.Store
	data, err := json.Marshal(entry) //nolint:gosec
	if err != nil {
		return fmt.Errorf("marshal git credential: %w", err)
	}
	return h.store.Set(ctx, key, data)
}

// Erase deletes the live stored static Git credential that matches req (by URL
// prefix, explicit or omitted username, and optional rejected password) using a
// locked compare-and-delete so a concurrently stored replacement credential is
// never deleted. When req matches an auth handler, Erase is a strict no-op.
func (h *Helper) Erase(ctx context.Context, req *Request) error {
	if req == nil || strings.TrimSpace(req.Host) == "" {
		return nil
	}
	if err := ValidateHostAttribute(req.Host); err != nil {
		return err
	}
	if err := ValidateRepoPath(req.Path); err != nil {
		return err
	}
	if h.MatchesAuthHandler(ctx, req) {
		return nil
	}
	if h.store == nil {
		return nil
	}

	candidates, err := candidateCanonicalURLs(req.Protocol, req.Host, req.Path)
	if err != nil {
		return err
	}

	hasUsername := req.EffectiveHasUsername()
	var allKeys []string
	if !hasUsername {
		allKeys, err = h.store.List(ctx)
		if err != nil {
			return fmt.Errorf("list stored git credentials: %w", err)
		}
	}

	now := time.Now()
	// First pass: delete the first LIVE credential matching Get's selection rules
	// (plus req.Password when provided), so an expired child entry never masks a
	// live parent/host credential that Git actually rejected.
	for _, candidateURL := range candidates {
		matchedKey, matchedEntry, findErr := h.selectStoredAccount(ctx, candidateURL, req.Username, hasUsername, req.Password, allKeys, now, true)
		if findErr != nil {
			return findErr
		}
		if matchedKey != "" {
			done, delErr := h.compareAndDeleteAccount(ctx, matchedKey, candidateURL, matchedEntry, req, hasUsername, now, true)
			if delErr != nil {
				return delErr
			}
			if done {
				return nil
			}
		}
	}

	// Fallback pass: if no live entry matched, clean up any matching expired entry
	// under the same locked compare-and-delete guard.
	for _, candidateURL := range candidates {
		matchedKey, matchedEntry, findErr := h.selectStoredAccount(ctx, candidateURL, req.Username, hasUsername, req.Password, allKeys, now, false)
		if findErr != nil {
			return findErr
		}
		if matchedKey != "" {
			done, delErr := h.compareAndDeleteAccount(ctx, matchedKey, candidateURL, matchedEntry, req, hasUsername, now, false)
			if delErr != nil {
				return delErr
			}
			if done {
				return nil
			}
		}
	}
	return nil
}

func (h *Helper) compareAndDeleteAccount(
	ctx context.Context,
	key, canonicalURL string,
	expected *storedCredential,
	req *Request,
	hasUsername bool,
	now time.Time,
	requireLive bool,
) (bool, error) {
	unlock, err := acquireAccountMutationLock(ctx, h.store, key)
	if err != nil {
		return false, err
	}
	defer unlock()

	data, getErr := h.store.Get(ctx, key)
	if getErr != nil {
		if errors.Is(getErr, secrets.ErrNotFound) {
			// Already deleted concurrently.
			return true, nil
		}
		return false, fmt.Errorf("read stored git credential: %w", getErr)
	}
	if len(data) == 0 {
		return true, nil
	}

	current, decErr := decodeStoredCredential(data)
	if decErr != nil {
		return false, fmt.Errorf("corrupted stored git credential %q: %w", key, decErr)
	}
	if current.CanonicalURL != "" && current.CanonicalURL != canonicalURL {
		return false, nil
	}
	if hasUsername && current.Username != req.Username {
		return false, nil
	}

	// If a concurrent Store replaced this slot after selectStoredAccount read it
	// (different password, newer StoredAtUnixNano, or an expired entry replaced by
	// a live credential), leave the new credential intact.
	isExpired := current.PasswordExpiryUTC != nil && !current.PasswordExpiryUTC.After(now)
	if !requireLive && !isExpired {
		return true, nil
	}
	if req.Password != "" && current.Password != req.Password {
		return true, nil
	}
	if expected != nil && (current.StoredAtUnixNano != expected.StoredAtUnixNano || current.Password != expected.Password) {
		return true, nil
	}

	if delErr := h.store.Delete(ctx, key); delErr != nil && !errors.Is(delErr, secrets.ErrNotFound) {
		return false, delErr
	}
	return true, nil
}

func (h *Helper) selectStoredAccount(
	ctx context.Context,
	canonicalURL, reqUsername string,
	hasUsername bool,
	reqPassword string,
	listedKeys []string,
	now time.Time,
	requireLive bool,
) (string, *storedCredential, error) {
	var keysToCheck []string
	if hasUsername {
		keysToCheck = []string{SecretKeyForAccount(h.binaryName, canonicalURL, reqUsername)}
	} else {
		urlPrefix := SecretKeyURLPrefix(h.binaryName, canonicalURL)
		for _, k := range listedKeys {
			if strings.HasPrefix(k, urlPrefix) {
				keysToCheck = append(keysToCheck, k)
			}
		}
	}

	var (
		bestKey   string
		bestEntry *storedCredential
	)
	for _, key := range keysToCheck {
		data, err := h.store.Get(ctx, key)
		if err != nil {
			if errors.Is(err, secrets.ErrNotFound) {
				continue
			}
			return "", nil, fmt.Errorf("read stored git credential: %w", err)
		}
		if len(data) == 0 {
			continue
		}
		entry, decErr := decodeStoredCredential(data)
		if decErr != nil {
			return "", nil, fmt.Errorf("corrupted stored git credential %q: %w", key, decErr)
		}
		if entry.CanonicalURL != "" && entry.CanonicalURL != canonicalURL {
			continue
		}
		if hasUsername && entry.Username != reqUsername {
			continue
		}
		if reqPassword != "" && entry.Password != reqPassword {
			continue
		}
		isExpired := entry.PasswordExpiryUTC != nil && !entry.PasswordExpiryUTC.After(now)
		if requireLive && isExpired {
			continue
		}
		if bestEntry == nil || isNewerStoredCredential(entry, bestEntry) {
			bestKey = key
			bestEntry = entry
		}
	}
	return bestKey, bestEntry, nil
}

func isNewerStoredCredential(candidate, current *storedCredential) bool {
	if candidate.StoredAtUnixNano != current.StoredAtUnixNano {
		return candidate.StoredAtUnixNano > current.StoredAtUnixNano
	}
	return candidate.Username > current.Username
}

func candidateCanonicalURLs(protocol, host, repoPath string) ([]string, error) {
	if err := ValidateRepoPath(repoPath); err != nil {
		return nil, err
	}
	rootURL, err := CanonicalURL(protocol, host, "")
	if err != nil {
		return nil, err
	}
	cleanPath := CleanRepoPath(repoPath)
	if cleanPath == "" {
		return []string{rootURL}, nil
	}

	segments := strings.Split(cleanPath, "/")
	out := make([]string, 0, len(segments))
	for i := len(segments); i >= 1; i-- {
		out = append(out, rootURL+"/"+strings.Join(segments[:i], "/"))
	}
	out = append(out, rootURL)
	return out, nil
}
