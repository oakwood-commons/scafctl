// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package oauth2

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/fs"
	"github.com/oakwood-commons/scafctl/pkg/secrets"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"gopkg.in/yaml.v3"
)

const (
	// MaxStdinSecretBytes caps the size of a secret read from standard input.
	MaxStdinSecretBytes = 64 * 1024

	// ClientSecretNameSuffix is appended to a custom OAuth2 handler name when
	// storing its client secret via --client-secret-stdin.
	ClientSecretNameSuffix = "-client-secret" //nolint:gosec // key suffix, not credential
)

// Sentinel errors returned by custom OAuth2 configuration management helpers.
var (
	// ErrHandlerAlreadyExists indicates a custom OAuth2 handler with the same name already exists in config.
	ErrHandlerAlreadyExists = errors.New("custom OAuth2 handler already exists")

	// ErrHandlerNotFound indicates no custom OAuth2 handler with the given name exists in config.
	ErrHandlerNotFound = errors.New("custom OAuth2 handler not found")

	// ErrReservedHandlerName indicates the requested name collides with a built-in, official, or plugin auth handler.
	ErrReservedHandlerName = errors.New("handler name conflicts with a built-in, official, or plugin auth handler")

	// ErrPlaintextSecretFlag indicates a raw non-URI secret was passed on a CLI flag instead of a SecretRef URI.
	ErrPlaintextSecretFlag = errors.New("plaintext secret value not allowed on CLI flag")

	// ErrMaxHandlersExceeded indicates adding the entry would exceed config.MaxCustomOAuth2Handlers.
	ErrMaxHandlersExceeded = errors.New("maximum number of custom OAuth2 handlers exceeded")

	// ErrImmutableHandler indicates a custom OAuth2 handler is defined by WithBaseConfig
	// or config.d and cannot be deleted from config.yaml.
	ErrImmutableHandler = errors.New("custom OAuth2 handler is defined by base or drop-in configuration and cannot be removed")
)

// ClientSecretStoreKey returns the secret store key used for a handler's stdin-supplied client secret.
func ClientSecretStoreKey(handlerName string) string {
	return handlerName + ClientSecretNameSuffix
}

// ValidateHandlerName checks that name is a valid, safe custom OAuth2 handler identifier
// and does not use a reserved internal secret prefix.
func ValidateHandlerName(name string, binaryNames ...string) error {
	if name == "" {
		return fmt.Errorf("custom OAuth2 handler name is required")
	}
	if len(name) > config.MaxCustomOAuth2NameLength {
		return fmt.Errorf("custom OAuth2 handler name %q exceeds maximum length of %d characters", name, config.MaxCustomOAuth2NameLength)
	}
	if strings.Contains(name, "..") || strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("custom OAuth2 handler name %q must not contain path separators or '..'", name)
	}
	first := name[0]
	if !isHandlerNameAlphaNum(first) {
		return fmt.Errorf("custom OAuth2 handler name %q must start with an ASCII letter or digit", name)
	}
	for i := 1; i < len(name); i++ {
		c := name[i]
		if isHandlerNameAlphaNum(c) || c == '-' || c == '_' || c == '.' {
			continue
		}
		return fmt.Errorf("custom OAuth2 handler name %q contains invalid characters (allowed: a-z, A-Z, 0-9, -, _, .)", name)
	}
	for _, prefix := range config.ReservedSecretPrefixes(binaryNames...) {
		if strings.HasPrefix(name, prefix) || name+"." == prefix {
			return fmt.Errorf("custom OAuth2 handler name %q uses reserved prefix %q", name, strings.TrimSuffix(prefix, "."))
		}
	}
	return nil
}

func isHandlerNameAlphaNum(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// ValidateFlagSecretRef enforces that a secret-bearing CLI flag value is a valid
// SecretRef URI (secret://, env://, or file://) and never a bare plaintext secret.
func ValidateFlagSecretRef(flagName, raw string, allowStdinHint bool, binaryNames ...string) error {
	if raw == "" {
		return nil
	}
	if !config.IsSecretRef(raw) {
		if allowStdinHint {
			return fmt.Errorf(
				"%w: --%s must be a SecretRef URI (secret://<name>, env://<VAR>, or file://<path>) to avoid exposing plaintext secrets in process arguments; use --client-secret-stdin to read a secret from standard input",
				ErrPlaintextSecretFlag, flagName,
			)
		}
		return fmt.Errorf(
			"%w: --%s must be a SecretRef URI (secret://<name>, env://<VAR>, or file://<path>) to avoid exposing plaintext secrets in process arguments",
			ErrPlaintextSecretFlag, flagName,
		)
	}
	if err := config.SecretRef(raw).ValidateFor(binaryNames...); err != nil {
		return fmt.Errorf("invalid --%s: %w", flagName, err)
	}
	return nil
}

// ParseConfigYAML parses a single CustomOAuth2Config from YAML or JSON bytes,
// rejecting unknown fields and trailing YAML documents.
func ParseConfigYAML(data []byte) (config.CustomOAuth2Config, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return config.CustomOAuth2Config{}, fmt.Errorf("custom OAuth2 handler definition is empty")
	}

	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)

	var cfg config.CustomOAuth2Config
	if err := dec.Decode(&cfg); err != nil {
		return config.CustomOAuth2Config{}, fmt.Errorf("parsing custom OAuth2 handler definition: %w", err)
	}

	var extra any
	if err := dec.Decode(&extra); err != nil && !errors.Is(err, io.EOF) {
		return config.CustomOAuth2Config{}, fmt.Errorf("parsing custom OAuth2 handler definition: %w", err)
	} else if err == nil {
		return config.CustomOAuth2Config{}, fmt.Errorf("custom OAuth2 handler file must contain a single YAML document")
	}

	return cfg, nil
}

// LoadConfigFile reads and parses a CustomOAuth2Config YAML or JSON file from path.
func LoadConfigFile(path string, readFn ...fs.ReadFileFunc) (config.CustomOAuth2Config, error) {
	cleaned := filepath.Clean(path)
	reader := fs.ReadFileFunc(os.ReadFile)
	if len(readFn) > 0 && readFn[0] != nil {
		reader = readFn[0]
	}
	data, err := reader(cleaned)
	if err != nil {
		return config.CustomOAuth2Config{}, fmt.Errorf("reading handler file %q: %w", path, err)
	}
	return ParseConfigYAML(data)
}

// NameChecker reports whether a handler name is reserved by an official or
// external handler registry (for example *authofficial.Registry). Defining the
// narrow interface here avoids coupling pkg/auth/oauth2 to pkg/solution.
type NameChecker interface {
	Has(name string) bool
}

func hasReservedName(checker NameChecker, name string) bool {
	if checker == nil {
		return false
	}
	rv := reflect.ValueOf(checker)
	if rv.Kind() == reflect.Pointer && rv.IsNil() {
		return false
	}
	return checker.Has(name)
}

// CheckHandlerNameCollision verifies that name does not collide with an official,
// built-in, or plugin auth handler. Collisions are rejected unconditionally
// (including when --force is specified).
func CheckHandlerNameCollision(ctx context.Context, cfg *config.Config, official NameChecker, name string) error {
	disableOfficial := cfg != nil && cfg.Settings.DisableOfficialAuthHandlers
	if !disableOfficial && hasReservedName(official, name) {
		return fmt.Errorf("%w: %q is reserved by an official auth handler", ErrReservedHandlerName, name)
	}

	if authReg := auth.RegistryFromContext(ctx); authReg != nil {
		if registered, ok := authReg.GetRegistered(name); ok && registered != nil {
			customH, isCustomHandler := auth.UnwrapHandler(registered).(*Handler)
			if !isCustomHandler || !customH.IsCustomConfig() {
				return fmt.Errorf("%w: %q is already registered as a built-in or plugin auth handler", ErrReservedHandlerName, name)
			}
		}
	}

	if cfg != nil {
		if hc, ok := cfg.Auth.Handlers[name]; ok && hc.Plugin != nil {
			return fmt.Errorf("%w: %q is configured as a plugin auth handler under auth.handlers.%s.plugin", ErrReservedHandlerName, name, name)
		}
	}

	return nil
}

func findCustomOAuth2Index(cfg *config.Config, name string) (int, bool) {
	if cfg == nil {
		return -1, false
	}
	for i := range cfg.Auth.CustomOAuth2 {
		if cfg.Auth.CustomOAuth2[i].Name == name {
			return i, true
		}
	}
	return -1, false
}

// AddOptions configures AddCustomOAuth2.
type AddOptions struct {
	// ConfigPath is an optional explicit config file path (empty uses XDG default).
	ConfigPath string

	// BinaryName is the CLI binary name for reserved secret prefix validation.
	// Falls back to settings.BinaryNameFromContext(ctx) when empty.
	BinaryName string

	// Entry is the CustomOAuth2Config to add or update.
	Entry config.CustomOAuth2Config

	// Force overwrites an existing customOAuth2 entry of the same name when true.
	// Built-in, official, and plugin handler collisions are still rejected.
	Force bool

	// ReadClientSecretStdin reads the client secret from ClientSecretReader,
	// stores it in SecretStore under <name>-client-secret, and sets
	// Entry.ClientSecret to secret://<name>-client-secret.
	ReadClientSecretStdin bool

	// ClientSecretReader supplies standard input bytes when ReadClientSecretStdin is true.
	ClientSecretReader io.Reader

	// OfficialHandlers optionally supplies the official auth handler registry
	// (e.g. authofficial.RegistryFromContext(ctx)) for reserved-name collision checks.
	OfficialHandlers NameChecker

	// SecretStore optionally overrides the secret store used when ReadClientSecretStdin is true.
	// Defaults to secrets.FromContext(ctx), falling back to secrets.New().
	SecretStore secrets.Store
}

// AddResult describes the outcome of AddCustomOAuth2.
type AddResult struct {
	// Name is the persisted custom OAuth2 handler name.
	Name string

	// Updated reports whether an existing customOAuth2 entry was overwritten.
	Updated bool

	// StoredSecretName is the secret store key written when ReadClientSecretStdin was used.
	StoredSecretName string
}

// ConfigPersistenceError wraps failures loading or saving the application configuration file
// so callers can distinguish config-layer errors from input validation errors.
type ConfigPersistenceError struct {
	Err error
}

func (e *ConfigPersistenceError) Error() string { return e.Err.Error() }
func (e *ConfigPersistenceError) Unwrap() error { return e.Err }

// AddCustomOAuth2 validates and persists a custom OAuth2 handler configuration.
func AddCustomOAuth2(ctx context.Context, opts AddOptions) (*AddResult, error) {
	binName := strings.TrimSpace(opts.BinaryName)
	if binName == "" {
		binName = settings.BinaryNameFromContext(ctx)
	}

	entry := opts.Entry
	if err := ValidateHandlerName(entry.Name, binName); err != nil {
		return nil, err
	}

	mgr := config.NewManager(opts.ConfigPath, config.ManagerOptionsFromContext(ctx)...)
	cfg, err := mgr.Load()
	if err != nil {
		return nil, &ConfigPersistenceError{Err: fmt.Errorf("loading config: %w", err)}
	}

	if err := CheckHandlerNameCollision(ctx, cfg, opts.OfficialHandlers, entry.Name); err != nil {
		return nil, err
	}

	existingIdx, exists := findCustomOAuth2Index(cfg, entry.Name)
	if exists && !opts.Force {
		return nil, fmt.Errorf("%w: %q (use --force to overwrite)", ErrHandlerAlreadyExists, entry.Name)
	}
	if !exists && len(cfg.Auth.CustomOAuth2) >= config.MaxCustomOAuth2Handlers {
		return nil, fmt.Errorf("%w: cannot exceed %d custom OAuth2 handlers", ErrMaxHandlersExceeded, config.MaxCustomOAuth2Handlers)
	}

	var (
		secretKey   string
		secretBytes []byte
	)
	if opts.ReadClientSecretStdin {
		val, readErr := readStdinSecret(opts.ClientSecretReader)
		if readErr != nil {
			return nil, readErr
		}
		secretKey = ClientSecretStoreKey(entry.Name)
		if nameErr := secrets.ValidateSecretNameFor(secretKey, false, secrets.InternalSecretPrefixFor(binName)); nameErr != nil {
			return nil, fmt.Errorf("invalid secret name %q for handler %q: %w", secretKey, entry.Name, nameErr)
		}
		ref := config.SecretRef("secret://" + secretKey)
		if refErr := ref.ValidateFor(binName); refErr != nil {
			return nil, refErr
		}
		entry.ClientSecret = string(ref)
		secretBytes = []byte(val)
	}

	if err := ValidateConfigFor(entry, binName); err != nil {
		return nil, err
	}

	if exists {
		cfg.Auth.CustomOAuth2[existingIdx] = entry
	} else {
		cfg.Auth.CustomOAuth2 = append(cfg.Auth.CustomOAuth2, entry)
	}
	mgr.MarkUserCustomOAuth2(entry.Name)

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validating config: %w", err)
	}

	var (
		store       secrets.Store
		hadPrev     bool
		prevSecret  []byte
		wroteSecret bool
	)
	if opts.ReadClientSecretStdin {
		store = opts.SecretStore
		if store == nil {
			store = secrets.FromContext(ctx)
		}
		if store == nil {
			var storeErr error
			store, storeErr = secrets.New(secrets.WithRequireSecureKeyring(cfg.Settings.RequireSecureKeyring))
			if storeErr != nil {
				return nil, &ConfigPersistenceError{Err: fmt.Errorf("initializing secrets store: %w", storeErr)}
			}
		}

		if prev, getErr := store.Get(ctx, secretKey); getErr == nil {
			hadPrev = true
			prevSecret = prev
		} else if !errors.Is(getErr, secrets.ErrNotFound) {
			return nil, &ConfigPersistenceError{Err: fmt.Errorf("checking existing secret %q: %w", secretKey, getErr)}
		}

		if setErr := store.Set(ctx, secretKey, secretBytes); setErr != nil {
			return nil, &ConfigPersistenceError{Err: fmt.Errorf("storing client secret %q: %w", secretKey, setErr)}
		}
		wroteSecret = true
	}

	if saveErr := mgr.Save(); saveErr != nil {
		var rollbackErr error
		if wroteSecret && store != nil {
			rollbackCtx := context.WithoutCancel(ctx)
			if hadPrev {
				rollbackErr = store.Set(rollbackCtx, secretKey, prevSecret)
			} else {
				rollbackErr = store.Delete(rollbackCtx, secretKey)
			}
		}
		combined := fmt.Errorf("saving config: %w", saveErr)
		if rollbackErr != nil {
			combined = errors.Join(combined, fmt.Errorf("rolling back client secret %q: %w", secretKey, rollbackErr))
		}
		return nil, &ConfigPersistenceError{Err: combined}
	}

	return &AddResult{
		Name:             entry.Name,
		Updated:          exists,
		StoredSecretName: secretKey,
	}, nil
}

func readStdinSecret(r io.Reader) (string, error) {
	if r == nil {
		return "", fmt.Errorf("--client-secret-stdin requires readable standard input")
	}
	if f, ok := r.(*os.File); ok {
		if stat, statErr := f.Stat(); statErr == nil && (stat.Mode()&os.ModeCharDevice) != 0 {
			return "", fmt.Errorf("--client-secret-stdin requires piped or redirected standard input")
		}
	}
	raw, err := io.ReadAll(io.LimitReader(r, MaxStdinSecretBytes+1))
	if err != nil {
		return "", fmt.Errorf("reading client secret from standard input: %w", err)
	}
	if len(raw) > MaxStdinSecretBytes {
		return "", fmt.Errorf("client secret from standard input exceeds maximum size of %d bytes", MaxStdinSecretBytes)
	}
	trimmed := strings.TrimRight(string(raw), "\r\n")
	if strings.TrimSpace(trimmed) == "" {
		return "", fmt.Errorf("client secret read from standard input is empty")
	}
	return trimmed, nil
}

// RemoveResult describes the outcome of RemoveCustomOAuth2WithResult.
type RemoveResult struct {
	// Name is the removed custom OAuth2 handler name.
	Name string

	// RestoredLowerLayer reports whether removing a user override in config.yaml
	// restored an underlying WithBaseConfig or config.d handler definition.
	RestoredLowerLayer bool
}

// RemoveCustomOAuth2 removes the named custom OAuth2 handler from configuration.
func RemoveCustomOAuth2(ctx context.Context, configPath, name string) error {
	_, err := RemoveCustomOAuth2WithResult(ctx, configPath, name)
	return err
}

// RemoveCustomOAuth2WithResult removes the named custom OAuth2 handler from
// configuration and reports whether a lower-layer definition remains active.
func RemoveCustomOAuth2WithResult(ctx context.Context, configPath, name string) (*RemoveResult, error) {
	if strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("custom OAuth2 handler name is required")
	}

	mgr := config.NewManager(configPath, config.ManagerOptionsFromContext(ctx)...)
	cfg, err := mgr.Load()
	if err != nil {
		return nil, &ConfigPersistenceError{Err: fmt.Errorf("loading config: %w", err)}
	}

	idx, exists := findCustomOAuth2Index(cfg, name)
	if !exists {
		return nil, fmt.Errorf("%w: %q", ErrHandlerNotFound, name)
	}

	inLower := mgr.IsLowerLayerCustomOAuth2(name)
	inUser := mgr.IsUserCustomOAuth2(name)
	if inLower && !inUser {
		return nil, fmt.Errorf("%w: %q is defined by base or config.d configuration and cannot be removed from config.yaml", ErrImmutableHandler, name)
	}

	restoredLower := false
	if inLower && inUser {
		if lowerEntry, ok := mgr.LowerLayerCustomOAuth2(name); ok {
			cfg.Auth.CustomOAuth2[idx] = lowerEntry
			restoredLower = true
		}
	} else {
		cfg.Auth.CustomOAuth2 = append(cfg.Auth.CustomOAuth2[:idx], cfg.Auth.CustomOAuth2[idx+1:]...)
	}
	mgr.UnmarkUserCustomOAuth2(name)

	if saveErr := mgr.Save(); saveErr != nil {
		return nil, &ConfigPersistenceError{Err: fmt.Errorf("saving config: %w", saveErr)}
	}
	return &RemoveResult{Name: name, RestoredLowerLayer: restoredLower}, nil
}

// ListCustomOAuth2 returns all configured custom OAuth2 handlers in sanitized form
// (redacting secrets and dropping templated token-exchange request bodies).
// Always returns a non-nil slice when err is nil.
func ListCustomOAuth2(ctx context.Context, configPath string) ([]config.SanitizedCustomOAuth2, error) {
	mgr := config.NewManager(configPath, config.ManagerOptionsFromContext(ctx)...)
	cfg, err := mgr.Load()
	if err != nil {
		return nil, &ConfigPersistenceError{Err: fmt.Errorf("loading config: %w", err)}
	}

	items := make([]config.SanitizedCustomOAuth2, 0, len(cfg.Auth.CustomOAuth2))
	for _, oc := range cfg.Auth.CustomOAuth2 {
		items = append(items, config.SanitizeCustomOAuth2(oc))
	}
	return items, nil
}
