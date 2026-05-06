// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package builtin

import (
	"context"
	"fmt"
	"sync"

	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/celprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/debugprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/fileprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/gotmplprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/httpprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/messageprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/parameterprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/stateprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/staticprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/validationprovider"
)

var (
	defaultRegistryOnce sync.Once
	defaultRegistry     *provider.Registry
	registrationErr     error
)

// DefaultRegistry returns a shared registry with all built-in providers pre-registered.
// Thread-safe and initialized only once using sync.Once.
// This is the recommended way to get a provider registry for resolver execution.
func DefaultRegistry(ctx context.Context) (*provider.Registry, error) {
	defaultRegistryOnce.Do(func() {
		defaultRegistry = provider.NewRegistry()
		registrationErr = registerAllToRegistry(ctx, defaultRegistry)
	})
	return defaultRegistry, registrationErr
}

// registerAllToRegistry registers all built-in providers to the given registry.
func registerAllToRegistry(ctx context.Context, reg *provider.Registry) error {
	lgr := logger.FromContext(ctx)

	providers := []provider.Provider{
		httpprovider.NewHTTPProvider(),
		celprovider.NewCelProvider(),
		fileprovider.NewFileProvider(),
		validationprovider.NewValidationProvider(),
		debugprovider.NewDebugProvider(),
		gotmplprovider.NewGoTemplateProvider(),
		messageprovider.NewMessageProvider(),
		stateprovider.New(),
		staticprovider.New(),
		parameterprovider.NewParameterProvider(),
	}

	lgr.V(2).Info("registering built-in providers", "count", len(providers))

	var errs []error
	for _, p := range providers {
		if regErr := reg.Register(p); regErr != nil {
			lgr.V(1).Info("failed to register built-in provider", "name", p.Descriptor().Name, "error", regErr)
			errs = append(errs, regErr)
		} else {
			lgr.V(3).Info("registered built-in provider", "name", p.Descriptor().Name)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("failed to register %d provider(s): %w", len(errs), errs[0])
	}

	lgr.V(2).Info("all built-in providers registered successfully", "count", len(providers))
	return nil
}

// ProviderNames returns the names of all built-in providers.
// This is useful for documentation and testing.
func ProviderNames() []string {
	return []string{
		"http",
		"cel",
		"file",
		"validation",
		"debug",
		"go-template",
		"message",
		"state",
		"static",
		"parameter",
	}
}
