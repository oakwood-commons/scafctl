package builtin

import (
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/celprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/debugprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/envprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/execprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/fileprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/gitprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/httpprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/sleepprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/validationprovider"
)

// RegisterAll registers all built-in providers in the global registry.
func RegisterAll() error {
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
	}

	for _, p := range providers {
		if err := provider.Register(p); err != nil {
			return err
		}
	}

	return nil
}
