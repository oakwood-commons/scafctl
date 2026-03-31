// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package builtin

import (
	"context"
	"fmt"
	"sync"

	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/celprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/debugprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/directoryprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/envprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/execprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/fileprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/githubprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/gitprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/gotmplprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/hclprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/httpprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/identityprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/messageprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/metadataprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/parameterprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/secretprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/sleepprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/staticprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/validationprovider"
	"github.com/oakwood-commons/scafctl/pkg/secrets"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
)

var (
	defaultRegistryOnce sync.Once
	defaultRegistry     *provider.Registry
	registrationErr     error
)

// DefaultRegistry returns a shared registry with all built-in providers pre-registered.
// Thread-safe and initialized only once using sync.Once.
// This is the recommended way to get a provider registry for resolver execution.
//
// The ctx is used on the first call to extract a Writer for user-facing warnings
// (e.g., when the secret store falls back to a less secure backend).
// Subsequent calls ignore ctx since the registry is already initialized.
func DefaultRegistry(ctx context.Context) (*provider.Registry, error) {
	defaultRegistryOnce.Do(func() {
		defaultRegistry = provider.NewRegistry()
		registrationErr = registerAllToRegistry(ctx, defaultRegistry)
	})
	return defaultRegistry, registrationErr
}

// registerAllToRegistry registers all built-in providers to the given registry.
// If the secrets store cannot be initialized (e.g., no OS keyring, no env var, no writable data dir),
// all other providers are still registered and the error is surfaced as a
// user-facing warning (not a structured log).
func registerAllToRegistry(ctx context.Context, reg *provider.Registry) error {
	providers := []provider.Provider{
		httpprovider.NewHTTPProvider(),
		envprovider.NewEnvProvider(),
		celprovider.NewCelProvider(),
		fileprovider.NewFileProvider(),
		directoryprovider.NewDirectoryProvider(),
		validationprovider.NewValidationProvider(),
		execprovider.NewExecProvider(),
		gitprovider.NewGitProvider(),
		githubprovider.NewGitHubProvider(),
		debugprovider.NewDebugProvider(),
		sleepprovider.NewSleepProvider(),
		parameterprovider.NewParameterProvider(),
		staticprovider.New(),
		gotmplprovider.NewGoTemplateProvider(),
		identityprovider.NewIdentityProvider(),
		hclprovider.NewHCLProvider(),
		metadataprovider.New(),
		messageprovider.NewMessageProvider(),
	}

	// Initialize secrets store for the secret provider.
	// The keyring chain tries: OS keyring → SCAFCTL_SECRET_KEY env var → file-based key.
	// If all backends fail, skip the secret provider but register everything else.
	var secretOpts []secrets.Option
	if cfg := config.FromContext(ctx); cfg != nil {
		secretOpts = append(secretOpts, secrets.WithRequireSecureKeyring(cfg.Settings.RequireSecureKeyring))
	}
	secretStore, err := secrets.New(secretOpts...)
	if err == nil {
		providers = append(providers, secretprovider.NewSecretProvider(secretprovider.WithSecretStore(secretStore)))

		// Warn the user if we fell back to a less-secure backend
		backend := secretStore.KeyringBackend()
		if backend == secrets.KeyringBackendFile {
			warnUser(ctx, " Secret store using file-based key storage. For better security, configure an OS keyring or set SCAFCTL_SECRET_KEY.")
		}
	} else {
		warnUser(ctx, fmt.Sprintf(" Secret provider unavailable: %v. Secret features will be disabled. Set SCAFCTL_SECRET_KEY to enable.", err))
	}

	var errs []error
	for _, p := range providers {
		if regErr := reg.Register(p); regErr != nil {
			errs = append(errs, regErr)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("failed to register %d provider(s): %w", len(errs), errs[0])
	}

	return nil
}

// warnUser emits a user-facing warning message via the Writer in ctx.
// Writes to stderr (via w.WarnStderr) so that diagnostic messages do not
// corrupt structured stdout output (e.g., -o json).
// If no Writer is available in ctx, the warning is silently dropped
// (it would only be visible at debug log level via structured logging).
func warnUser(ctx context.Context, msg string) {
	if w := writer.FromContext(ctx); w != nil {
		w.WarnStderr(msg)
	}
}

// ProviderNames returns the names of all built-in providers.
// This is useful for documentation and testing.
func ProviderNames() []string {
	return []string{
		"http",
		"env",
		"cel",
		"file",
		"directory",
		"validation",
		"exec",
		"git",
		"debug",
		"sleep",
		"parameter",
		"static",
		"go-template",
		"secret",
		"identity",
		"hcl",
		"github",
		"metadata",
		"message",
	}
}

// SecretStoreAvailable returns true if the secrets store can be initialized.
// This is useful for tests to determine whether the secret provider will be registered.
func SecretStoreAvailable() bool {
	_, err := secrets.New()
	return err == nil
}
