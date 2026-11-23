// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package login

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogin_NormalizesAndValidatesProfile(t *testing.T) {
	t.Parallel()

	t.Run("--profile default normalizes handler context to empty while preserving exec arg", func(t *testing.T) {
		t.Parallel()
		var gotResolved bool
		handler := &stubAuth{
			name:  "openshift",
			caps:  []auth.Capability{auth.CapHostname, auth.CapTokenHostname},
			token: &auth.Token{AccessToken: "tok"},
		}
		kubeWriter := &stubKube{}
		deps := Deps{
			Handler:    handler,
			Kubeconfig: kubeWriter,
			LoginRunner: func(ctx context.Context, h Authenticator, opts auth.LoginOptions) (*auth.Result, error) {
				gotResolved = auth.IsProfileResolved(ctx)
				return h.Login(ctx, opts)
			},
		}

		res, err := Login(context.Background(), deps, Request{
			Cluster:        "prod",
			Server:         "https://api.prod.example.com:6443",
			Profile:        "default",
			KubeconfigPath: filepath.Join(t.TempDir(), "kubeconfig"),
		})
		require.NoError(t, err)
		require.NotNil(t, res)
		assert.Empty(t, handler.loginProfile, "default profile must normalize to empty in context")
		assert.True(t, gotResolved, "context must mark profile resolved so activeProfile is not re-applied")
		assert.Contains(t, kubeWriter.writeIn.ExecArgs, "--profile")
		assert.Contains(t, kubeWriter.writeIn.ExecArgs, "default")
	})

	t.Run("invalid profile returns ErrInvalidProfile", func(t *testing.T) {
		t.Parallel()
		deps := Deps{
			Handler:    &stubAuth{name: "openshift"},
			Kubeconfig: &stubKube{},
		}
		_, err := Login(context.Background(), deps, Request{
			Cluster: "prod",
			Server:  "https://api.prod.example.com:6443",
			Profile: "bad profile",
		})
		require.ErrorIs(t, err, ErrInvalidProfile)
	})

	t.Run("populateStatusIdentity normalizes default profile", func(t *testing.T) {
		t.Parallel()
		var gotProfile string
		var gotResolved bool
		handler := &stubAuth{
			name:  "openshift",
			token: &auth.Token{AccessToken: "tok"},
		}
		deps := Deps{
			Kubeconfig: &stubKube{},
			HandlerLookup: func(ctx context.Context, _ string) (Authenticator, error) {
				gotProfile = auth.ProfileFromContext(ctx)
				gotResolved = auth.IsProfileResolved(ctx)
				return handler, nil
			},
		}
		st := ContextStatus{
			Managed: true,
			Handler: "openshift",
			Server:  "https://api.prod.example.com:6443",
			Profile: "default",
		}
		populateStatusIdentity(context.Background(), deps, &st)
		assert.Empty(t, gotProfile)
		assert.True(t, gotResolved)
	})
}
