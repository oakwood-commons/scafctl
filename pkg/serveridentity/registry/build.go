package registry

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/serveridentity/entra"
	ghidentity "github.com/oakwood-commons/scafctl/pkg/serveridentity/github"
	"github.com/oakwood-commons/scafctl/pkg/tokenprovider"
)

// TokenProviderRegistry constructs a tokenprovider.Registry populated with
// configured server identity providers. Returns nil, nil when no identities
// are configured.
func TokenProviderRegistry(ctx context.Context, cfg *config.APIServerConfig, lgr *logr.Logger) (*tokenprovider.Registry, error) {
	reg := tokenprovider.NewRegistry()

	if err := registerEntraSource(ctx, cfg, lgr, reg); err != nil {
		return nil, fmt.Errorf("building Entra token provider: %w", err)
	}

	if err := registerGitHubSource(ctx, cfg, lgr, reg); err != nil {
		return nil, fmt.Errorf("building GitHub token provider: %w", err)
	}

	if len(reg.Names()) == 0 {
		return nil, nil
	}
	return reg, nil
}

func registerEntraSource(ctx context.Context, cfg *config.APIServerConfig, lgr *logr.Logger, reg *tokenprovider.Registry) error {
	if cfg.Identity.Entra == nil {
		return nil
	}
	if err := cfg.Identity.Entra.Validate(); err != nil {
		return fmt.Errorf("invalid Entra identity configuration: %w", err)
	}

	lgr.V(0).Info("building token provider registry", "provider", "entra")

	identity, err := entra.NewEntraIdentityFromConfig(ctx, cfg.Identity.Entra)
	if err != nil {
		return fmt.Errorf("entra identity: %w", err)
	}

	return reg.Register(identity)
}

func registerGitHubSource(ctx context.Context, cfg *config.APIServerConfig, lgr *logr.Logger, reg *tokenprovider.Registry) error {
	if cfg.Identity.GitHub == nil {
		return nil
	}
	if err := cfg.Identity.GitHub.Validate(); err != nil {
		return fmt.Errorf("invalid GitHub identity configuration: %w", err)
	}

	lgr.V(0).Info("building token provider registry", "provider", "github")

	identity, err := ghidentity.NewGitHubIdentityFromConfig(ctx, cfg.Identity.GitHub)
	if err != nil {
		return fmt.Errorf("github identity: %w", err)
	}

	return reg.Register(identity)
}
