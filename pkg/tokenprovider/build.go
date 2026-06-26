// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package tokenprovider

import (
	"context"
	"fmt"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/runmode"
)

// Build constructs a Registry for the given mode.
// In CLI mode, auth handlers are wrapped as AuthHandlerAdapter.
// In API mode, the pre-built identityReg (from serveridentity/registry) is merged
// directly since identity providers already implement TokenProvider.
func Build(mode runmode.Mode, authReg *auth.Registry, identityReg *Registry) (*Registry, error) {
	reg := NewRegistry()

	switch mode { //nolint:exhaustive // API is the only special case for now; all other modes use CLI adapters.
	case runmode.API:
		if identityReg == nil {
			return reg, nil
		}
		for _, name := range identityReg.Names() {
			src, ok := identityReg.Get(name)
			if ok {
				if err := reg.Register(src); err != nil {
					return nil, fmt.Errorf("registering identity source %q: %w", name, err)
				}
			}
		}
	default:
		if authReg == nil {
			return reg, nil
		}
		for _, h := range authReg.All() {
			if err := reg.Register(NewAuthHandlerAdapter(h)); err != nil {
				return nil, fmt.Errorf("registering CLI adapter %q: %w", h.Name(), err)
			}
		}
		// Lazily resolve sources that are not eagerly registered. Official auth
		// handlers are not built into the core binary; they live in the official
		// registry and are downloaded on demand. The auth registry's own
		// fallback resolver fetches and registers them, so consult it here before
		// failing. Without this, a post-login registry bridge fails with
		// "source not found" whenever the plugin was not cached at startup
		// (e.g. headless CI).
		reg.SetFallback(func(ctx context.Context, name string) (TokenProvider, error) {
			h, err := authReg.GetContext(ctx, name)
			if err != nil {
				return nil, err
			}
			return NewAuthHandlerAdapter(h), nil
		})
	}

	return reg, nil
}
