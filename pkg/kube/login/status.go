// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package login

import (
	"context"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/kubeconfig"
	"github.com/oakwood-commons/scafctl/pkg/settings"
)

// Status reads the current kubeconfig context and, for a scafctl-managed
// context, attempts a best-effort whoami to populate Username/Groups. The whoami
// needs the auth handler baked into the managed entry plus the kubeconfig
// provider; every failure along that path is non-fatal and simply leaves the
// identity fields empty. The static context data (context/cluster/server/
// namespace/managed) is always returned, so `kube status` works offline and
// without the provider.
func Status(ctx context.Context, deps Deps, kubeconfigPath string) (ContextStatus, error) {
	binaryName := deps.BinaryName
	if binaryName == "" {
		binaryName = settings.CliBinaryName
	}
	st, err := CurrentContext(binaryName, kubeconfigPath)
	if err != nil {
		return st, err
	}
	if st.Managed && st.Handler != "" && st.Server != "" &&
		deps.Kubeconfig != nil && deps.HandlerLookup != nil {
		populateStatusIdentity(ctx, deps, &st)
	}
	return st, nil
}

// populateStatusIdentity runs a best-effort whoami for a managed context. It
// resolves the entry's handler, mints (or reuses) a token under the entry's
// profile, and asks the kubeconfig provider who the token authenticates as. Any
// error is swallowed: the identity is advisory, not required.
func populateStatusIdentity(ctx context.Context, deps Deps, st *ContextStatus) {
	// Look tokens up under the same profile the exec block passes via --profile,
	// so the whoami sees the credential kubectl would actually use.
	if st.Profile != "" {
		ctx = auth.WithProfile(ctx, st.Profile)
	}

	handler, err := deps.HandlerLookup(ctx, st.Handler)
	if err != nil {
		return
	}

	tokenOpts := auth.TokenOptions{}
	// Mirror the scope the exec block bakes in (--scope <audience>) so status
	// mints/looks up the same token kubectl would use, for handlers that accept
	// per-request scopes. Without this the whoami could use a different token
	// and report the wrong identity.
	if st.Scope != "" && auth.HasCapability(handler.Capabilities(), auth.CapScopesOnTokenRequest) {
		tokenOpts.Scope = st.Scope
	}
	// Route the token request to this cluster only for handlers that honor a
	// per-cluster hostname, mirroring the login/whoami path (issue #581).
	if auth.HasCapability(handler.Capabilities(), auth.CapTokenHostname) {
		tokenOpts.Hostname = st.Server
	}
	token, err := handler.GetToken(ctx, tokenOpts)
	if err != nil || token == nil {
		return
	}

	who, err := deps.Kubeconfig.Whoami(ctx, kubeconfig.WhoamiInput{
		Server:          st.Server,
		Token:           token.AccessToken,
		CAData:          st.CAData,
		InsecureSkipTLS: st.InsecureSkipTLS,
	})
	if err != nil || !who.Success {
		return
	}
	st.Username = who.Username
	st.Groups = who.Groups
}
