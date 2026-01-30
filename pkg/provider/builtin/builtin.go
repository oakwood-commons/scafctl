package builtin

import (
	"sync"

	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/celprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/debugprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/envprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/execprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/fileprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/gitprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/gotmplprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/httpprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/parameterprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/sleepprovider"
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
	}

	for _, p := range providers {
		if err := reg.Register(p); err != nil {
			return err
		}
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
	}
}
