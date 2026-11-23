// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package login

import (
	"context"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/kubeconfig"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogin_BrowserPreferencePriority(t *testing.T) {
	t.Parallel()

	cfgCtx := config.WithConfig(context.Background(), &config.Config{
		Auth: config.GlobalAuthConfig{Browser: "msedge"},
	})

	t.Run("Request.Browser overrides config auth.browser", func(t *testing.T) {
		t.Parallel()
		handler := &stubAuth{name: "oidc", token: &auth.Token{AccessToken: "tok"}}
		kc := &stubKube{writeRes: kubeconfig.WriteResult{Success: true, ContextName: "prod"}}

		_, err := Login(cfgCtx, Deps{Handler: handler, Kubeconfig: kc}, Request{
			Server:      "https://api.example.com:6443",
			ClusterName: "prod",
			Browser:     "firefox",
		})
		require.NoError(t, err)
		assert.Equal(t, "firefox", handler.loginOpts.Browser)
	})

	t.Run("config auth.browser applies when Request.Browser is empty", func(t *testing.T) {
		t.Parallel()
		var runnerOpts auth.LoginOptions
		handler := &stubAuth{name: "oidc", token: &auth.Token{AccessToken: "tok"}}
		kc := &stubKube{writeRes: kubeconfig.WriteResult{Success: true, ContextName: "prod"}}

		_, err := Login(cfgCtx, Deps{
			Handler:    handler,
			Kubeconfig: kc,
			LoginRunner: func(_ context.Context, _ Authenticator, opts auth.LoginOptions) (*auth.Result, error) {
				runnerOpts = opts
				return &auth.Result{}, nil
			},
		}, Request{
			Server:      "https://api.example.com:6443",
			ClusterName: "prod",
		})
		require.NoError(t, err)
		assert.Equal(t, "msedge", runnerOpts.Browser)
	})

	t.Run("invalid Request.Browser fails closed before login", func(t *testing.T) {
		t.Parallel()
		handler := &stubAuth{name: "oidc", token: &auth.Token{AccessToken: "tok"}}
		kc := &stubKube{writeRes: kubeconfig.WriteResult{Success: true, ContextName: "prod"}}

		_, err := Login(context.Background(), Deps{Handler: handler, Kubeconfig: kc}, Request{
			Server:      "https://api.example.com:6443",
			ClusterName: "prod",
			Browser:     "--headless",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid browser preference")
	})
}
