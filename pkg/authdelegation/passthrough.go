package authdelegation

import (
	"context"
	"fmt"
	"net/http"

	"github.com/oakwood-commons/scafctl/pkg/api/middleware"
)

const PassThroughDelegatorPrefix = "passThrough:"

var _ TokenDelegator = (*PassThroughDelegator)(nil)

type PassThroughDelegator struct {
	provider string
}

func (d *PassThroughDelegator) Name() string {
	return "passThrough:" + d.provider
}

func NewPassThroughDelegator(provider string) (*PassThroughDelegator, error) {
	if provider == "" {
		return nil, fmt.Errorf("provider is required")
	}
	return &PassThroughDelegator{provider: provider}, nil
}

func (d *PassThroughDelegator) DelegateToken(ctx context.Context, _ string) (TokenResult, error) {
	if ctx == nil {
		return TokenResult{}, fmt.Errorf("context is required for token passthrough")
	}

	token := middleware.TokensFromContext(ctx)[d.provider]
	if token == "" {
		return TokenResult{}, fmt.Errorf("token for provider %q not found in context", d.provider)
	}

	return TokenResult{AccessToken: token}, nil
}

func PassThroughDelegatorName(provider string) string {
	return PassThroughDelegatorPrefix + http.CanonicalHeaderKey(provider)
}
