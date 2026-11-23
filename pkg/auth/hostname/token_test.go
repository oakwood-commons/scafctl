// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package hostname

import (
	"context"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type tokenProbeHandler struct {
	*auth.MockHandler
	onGetToken func(ctx context.Context, opts auth.TokenOptions) (*auth.Token, error)
}

func (h *tokenProbeHandler) GetToken(ctx context.Context, opts auth.TokenOptions) (*auth.Token, error) {
	return h.onGetToken(ctx, opts)
}

func TestDefaultToken_DoesNotLeakTargetHandlerProfile(t *testing.T) {
	t.Parallel()

	var gotProfile string
	var gotResolved bool
	entraProbe := &tokenProbeHandler{
		MockHandler: auth.NewMockHandler("entra"),
		onGetToken: func(ctx context.Context, _ auth.TokenOptions) (*auth.Token, error) {
			gotProfile = auth.ResolveActiveProfile(ctx, "entra")
			gotResolved = auth.IsProfileResolved(ctx)
			return &auth.Token{AccessToken: "tok-" + gotProfile, ExpiresAt: time.Now().Add(time.Hour)}, nil
		},
	}

	reg := auth.NewRegistry()
	require.NoError(t, reg.Register(entraProbe))

	cfg := &config.Config{
		Auth: config.GlobalAuthConfig{
			Entra: &config.EntraAuthConfig{ActiveProfile: "corp"},
		},
	}
	baseCtx := auth.WithRegistry(config.WithConfig(context.Background(), cfg), reg)

	// Simulate login to openshift with --profile cluster-admin (or resolved default ""):
	// defaultToken for "entra" must reset per-handler profile resolution so entra uses "corp".
	openshiftCtx := auth.WithProfileResolved(auth.WithProfile(baseCtx, "cluster-admin"))
	tok, err := defaultToken(openshiftCtx, "entra", "api://fleet/.default")
	require.NoError(t, err)
	assert.Equal(t, "tok-corp", tok)
	assert.Equal(t, "corp", gotProfile)
	assert.False(t, gotResolved)

	// Explicit provider@profile syntax overrides entra's activeProfile.
	tokExplicit, err := defaultToken(openshiftCtx, "entra@staging", "api://fleet/.default")
	require.NoError(t, err)
	assert.Equal(t, "tok-staging", tokExplicit)
	assert.True(t, gotResolved)

	// Explicit provider@default resolves to "" (built-in), overriding activeProfile "corp".
	tokDefault, err := defaultToken(openshiftCtx, "entra@default", "api://fleet/.default")
	require.NoError(t, err)
	assert.Equal(t, "tok-", tokDefault)
	assert.True(t, gotResolved)

	// Invalid profile in provider@bad is rejected.
	_, err = defaultToken(openshiftCtx, "entra@bad profile", "api://fleet/.default")
	require.Error(t, err)
}
