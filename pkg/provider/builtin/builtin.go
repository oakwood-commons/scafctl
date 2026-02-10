// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package builtin

import (
	"fmt"
	"sync"

	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/celprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/debugprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/envprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/execprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/fileprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/gitprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/gotmplprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/httpprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/identityprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/parameterprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/secretprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/sleepprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/staticprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/validationprovider"
	"github.com/oakwood-commons/scafctl/pkg/secrets"
)

var (
	defaultRegistryOnce sync.Once
	defaultRegistry     *provider.Registry
	registrationErr     error
)

// DefaultRegistry returns a shared registry with all built-in providers pre-registered.
// Thread-safe and initialized only once using sync.Once.
// This is the recommended way to get a provider registry for resolver execution.
func DefaultRegistry() (*provider.Registry, error) {
	defaultRegistryOnce.Do(func() {
		defaultRegistry = provider.NewRegistry()
		registrationErr = registerAllToRegistry(defaultRegistry)
	})
	return defaultRegistry, registrationErr
}

// MustDefaultRegistry returns the default registry, panicking if registration fails.
// Use this only when you're certain provider registration will succeed.
func MustDefaultRegistry() *provider.Registry {
	reg, err := DefaultRegistry()
	if err != nil {
		panic("failed to initialize default provider registry: " + err.Error())
	}
	return reg
}

// RegisterAll registers all built-in providers in the global registry.
//
// Deprecated: Use DefaultRegistry() instead for better encapsulation.
func RegisterAll() error {
	return registerAllToRegistry(provider.GetGlobalRegistry())
}

// registerAllToRegistry registers all built-in providers to the given registry.
// If the secrets store cannot be initialized (e.g., no OS keyring in CI),
// all other providers are still registered and the error is returned as a
// non-fatal warning alongside a nil-error return.
func registerAllToRegistry(reg *provider.Registry) error {
	providers := []provider.Provider{
		httpprovider.NewHTTPProvider(),
		envprovider.NewEnvProvider(),
		celprovider.NewCelProvider(),
		fileprovider.NewFileProvider(),
		validationprovider.NewValidationProvider(),
		execprovider.NewExecProvider(),
		gitprovider.NewGitProvider(),
		debugprovider.NewDebugProvider(),
		sleepprovider.NewSleepProvider(),
		parameterprovider.NewParameterProvider(),
		staticprovider.New(),
		gotmplprovider.NewGoTemplateProvider(),
		identityprovider.NewIdentityProvider(),
	}

	// Initialize secrets store for the secret provider.
	// If the keyring is unavailable (e.g., CI without SCAFCTL_SECRET_KEY),
	// skip the secret provider but register everything else.
	secretStore, err := secrets.New()
	if err == nil {
		providers = append(providers, secretprovider.NewSecretProvider(secretprovider.WithSecretStore(secretStore)))
	} else {
		logger.GetGlobalLogger().Info("secret provider unavailable: secrets store failed to initialize, set SCAFCTL_SECRET_KEY to enable", "error", err)
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

// ProviderNames returns the names of all built-in providers.
// This is useful for documentation and testing.
func ProviderNames() []string {
	return []string{
		"http",
		"env",
		"cel",
		"file",
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
	}
}

// SecretStoreAvailable returns true if the secrets store can be initialized.
// This is useful for tests to determine whether the secret provider will be registered.
func SecretStoreAvailable() bool {
	_, err := secrets.New()
	return err == nil
}
