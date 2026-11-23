// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"context"
	"fmt"
	"os"
	"strings"
	"unicode"

	"github.com/oakwood-commons/scafctl/pkg/settings"
)

const (
	secretRefSchemeEnv    = "env://"
	secretRefSchemeFile   = "file://"
	secretRefSchemeSecret = "secret://"
	maxSecretRefNameLen   = 255
)

// SecretGetter retrieves a named secret from a backing secret store.
// Defined in pkg/config to keep configuration types decoupled from pkg/secrets;
// satisfied directly by secrets.Store.
type SecretGetter interface {
	Get(ctx context.Context, name string) ([]byte, error)
}

// IsSecretRef reports whether raw uses one of the supported SecretRef URI
// schemes (env://, file://, or secret://).
func IsSecretRef(raw string) bool {
	return strings.HasPrefix(raw, secretRefSchemeEnv) ||
		strings.HasPrefix(raw, secretRefSchemeFile) ||
		strings.HasPrefix(raw, secretRefSchemeSecret)
}

// ReservedSecretPrefixes returns the reserved internal secret key prefixes that
// user-configured secret:// references are never permitted to read (always
// including "scafctl.", plus "<binaryName>." for any custom embedder binary).
func ReservedSecretPrefixes(binaryNames ...string) []string {
	seen := map[string]struct{}{
		settings.CliBinaryName + ".": {},
	}
	prefixes := []string{settings.CliBinaryName + "."}
	addPrefix := func(name string) {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			return
		}
		for _, candidate := range []string{trimmed + ".", settings.SanitizeBinaryName(trimmed) + "."} {
			if candidate == "." {
				continue
			}
			if _, ok := seen[candidate]; !ok {
				seen[candidate] = struct{}{}
				prefixes = append(prefixes, candidate)
			}
		}
	}
	for _, bn := range binaryNames {
		addPrefix(bn)
	}
	return prefixes
}

// Validate checks that the SecretRef has a recognized scheme and a valid target.
func (s SecretRef) Validate() error {
	return s.ValidateFor()
}

// ValidateFor checks that the SecretRef has a recognized scheme and a valid
// target, additionally rejecting secret:// names that match any embedder
// binary's internal secret prefix.
func (s SecretRef) ValidateFor(binaryNames ...string) error {
	str := string(s)
	switch {
	case strings.HasPrefix(str, secretRefSchemeEnv):
		name := strings.TrimPrefix(str, secretRefSchemeEnv)
		if name == "" {
			return fmt.Errorf("env:// scheme requires a variable name")
		}
		if hasWhitespaceOrControl(name) {
			return fmt.Errorf("env:// variable name %q must not contain whitespace or control characters", name)
		}
	case strings.HasPrefix(str, secretRefSchemeFile):
		path := strings.TrimPrefix(str, secretRefSchemeFile)
		if path == "" {
			return fmt.Errorf("file:// scheme requires a path")
		}
		if strings.ContainsRune(path, 0) {
			return fmt.Errorf("file:// path must not contain NUL bytes")
		}
	case strings.HasPrefix(str, secretRefSchemeSecret):
		name := strings.TrimPrefix(str, secretRefSchemeSecret)
		if err := validateSecretRefStoreName(name, binaryNames...); err != nil {
			return err
		}
	default:
		return unsupportedSecretRefSchemeError(str)
	}
	return nil
}

func unsupportedSecretRefSchemeError(raw string) error {
	if idx := strings.Index(raw, "://"); idx > 0 {
		scheme := raw[:idx]
		if isSimpleURIScheme(scheme) {
			return fmt.Errorf("unsupported secret ref scheme %q: must start with env://, file://, or secret://", scheme)
		}
	}
	return fmt.Errorf("unsupported secret ref scheme: value must start with env://, file://, or secret://")
}

func isSimpleURIScheme(s string) bool {
	if len(s) == 0 || len(s) > 32 {
		return false
	}
	first := s[0]
	if (first < 'a' || first > 'z') && (first < 'A' || first > 'Z') {
		return false
	}
	for i := 1; i < len(s); i++ {
		c := s[i]
		if isASCIIAlphaNumeric(c) || c == '+' || c == '-' || c == '.' {
			continue
		}
		return false
	}
	return true
}

func validateSecretRefStoreName(name string, binaryNames ...string) error {
	if name == "" {
		return fmt.Errorf("secret:// scheme requires a secret name")
	}
	if len(name) > maxSecretRefNameLen {
		return fmt.Errorf("secret:// name exceeds maximum length of %d characters", maxSecretRefNameLen)
	}
	if hasWhitespaceOrControl(name) {
		return fmt.Errorf("secret:// name must not contain whitespace or control characters")
	}
	if strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		return fmt.Errorf("secret:// name must not contain path separators or '..'")
	}
	if !isASCIIAlphaNumeric(name[0]) {
		return fmt.Errorf("secret:// name must start with an alphanumeric character (a-z, A-Z, 0-9)")
	}
	for _, r := range name {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '.' && r != '_' && r != '-' {
			return fmt.Errorf("secret:// name contains invalid characters (allowed: a-z, A-Z, 0-9, -, _, .)")
		}
	}
	for _, prefix := range ReservedSecretPrefixes(binaryNames...) {
		if strings.HasPrefix(name, prefix) {
			return fmt.Errorf("secret:// name uses reserved internal secret prefix %q", prefix)
		}
	}
	return nil
}

func isASCIIAlphaNumeric(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

func hasWhitespaceOrControl(s string) bool {
	for _, r := range s {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return true
		}
	}
	return false
}

// Resolve reads the secret value from an env:// or file:// reference.
// References using secret:// require a SecretGetter store and must be resolved
// via ResolveWithStore.
func (s SecretRef) Resolve() (string, error) {
	str := string(s)
	switch {
	case strings.HasPrefix(str, secretRefSchemeEnv):
		if err := s.Validate(); err != nil {
			return "", err
		}
		name := strings.TrimPrefix(str, secretRefSchemeEnv)
		val := os.Getenv(name)
		if val == "" {
			return "", fmt.Errorf("environment variable %q is empty or not set", name)
		}
		return val, nil
	case strings.HasPrefix(str, secretRefSchemeFile):
		if err := s.Validate(); err != nil {
			return "", err
		}
		path := strings.TrimPrefix(str, secretRefSchemeFile)
		data, err := os.ReadFile(path) //nolint:gosec // G304: file path comes from trusted local application config
		if err != nil {
			return "", fmt.Errorf("reading secret file %q: %w", path, err)
		}
		val := strings.TrimSpace(string(data))
		if val == "" {
			return "", fmt.Errorf("secret file %q is empty", path)
		}
		return val, nil
	case strings.HasPrefix(str, secretRefSchemeSecret):
		if err := s.Validate(); err != nil {
			return "", err
		}
		return "", fmt.Errorf("secret ref uses secret:// scheme and requires a secret store: use ResolveWithStore")
	default:
		return "", unsupportedSecretRefSchemeError(str)
	}
}

// ResolveWithStore reads the secret value from env://, file://, or secret://.
// When secret:// is used, store must be non-nil and internal secret prefixes
// ("scafctl." and any configured binary name in ctx or binaryNames) are rejected.
func (s SecretRef) ResolveWithStore(ctx context.Context, store SecretGetter, binaryNames ...string) (string, error) {
	allBinaryNames := binaryNames
	if ctx != nil {
		if bn := settings.BinaryNameFromContext(ctx); bn != "" {
			allBinaryNames = append(append([]string(nil), binaryNames...), bn)
		}
	}
	if err := s.ValidateFor(allBinaryNames...); err != nil {
		return "", err
	}

	str := string(s)
	if strings.HasPrefix(str, secretRefSchemeSecret) {
		if store == nil {
			return "", fmt.Errorf("resolving %q: secret store is unavailable", str)
		}
		name := strings.TrimPrefix(str, secretRefSchemeSecret)
		data, err := store.Get(ctx, name)
		if err != nil {
			return "", fmt.Errorf("reading secret %q from store: %w", name, err)
		}
		val := strings.TrimSpace(string(data))
		if val == "" {
			return "", fmt.Errorf("secret %q is empty", name)
		}
		return val, nil
	}

	return s.Resolve()
}

// ValidateCredentialValue validates a credential field that accepts either a
// SecretRef URI (secret://, file://, env://) or a bare literal string for
// backward compatibility.
func ValidateCredentialValue(raw string, binaryNames ...string) error {
	if raw == "" {
		return nil
	}
	if IsSecretRef(raw) {
		return SecretRef(raw).ValidateFor(binaryNames...)
	}
	return nil
}

// ResolveCredentialValue resolves a credential field that accepts either a
// SecretRef URI (secret://, file://, env://) or a bare literal string. Empty
// inputs return ("", nil).
func ResolveCredentialValue(ctx context.Context, raw string, store SecretGetter, binaryNames ...string) (string, error) {
	if raw == "" {
		return "", nil
	}
	if IsSecretRef(raw) {
		return SecretRef(raw).ResolveWithStore(ctx, store, binaryNames...)
	}
	return raw, nil
}

// Scheme returns the scheme portion of the secret reference (e.g. "env", "file", "secret").
func (s SecretRef) Scheme() string {
	str := string(s)
	if idx := strings.Index(str, "://"); idx >= 0 {
		return str[:idx]
	}
	return ""
}
