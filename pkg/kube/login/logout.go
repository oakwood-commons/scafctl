// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package login

import (
	"context"
	"errors"
	"fmt"

	"github.com/oakwood-commons/scafctl/pkg/kubeconfig"
)

// LogoutRequest configures a logout.
type LogoutRequest struct {
	// Cluster is the logical cluster name; it defaults the entry names.
	Cluster string

	// ClusterName, ContextName, and UserName name the kubeconfig entries to
	// remove. Empty values default to the cluster name.
	ClusterName string
	ContextName string
	UserName    string

	// KubeconfigPath is the kubeconfig file to edit. Empty resolves KUBECONFIG
	// or ~/.kube/config.
	KubeconfigPath string

	// KeepCredentials skips the handler logout, leaving cached tokens in place.
	KeepCredentials bool
}

// LogoutResult reports the outcome of a logout.
type LogoutResult struct {
	// Removed reports whether a matching kubeconfig entry was removed.
	Removed bool

	// UsedFallback reports that the static kubeconfig writer was used because
	// the kubeconfig provider was unavailable.
	UsedFallback bool
}

// Logout removes the managed kubeconfig entry and, unless KeepCredentials is
// set, clears the handler's cached credentials. It falls back to a static
// kubeconfig edit when the provider is unavailable.
func Logout(ctx context.Context, deps Deps, req LogoutRequest) (*LogoutResult, error) {
	clusterName := firstNonEmpty(req.ClusterName, req.Cluster)
	if clusterName == "" {
		return nil, ErrNoCluster
	}
	if deps.Kubeconfig == nil {
		return nil, ErrNoKubeconfigWriter
	}
	removeIn := kubeconfig.RemoveInput{
		ClusterName:    clusterName,
		ContextName:    firstNonEmpty(req.ContextName, clusterName),
		UserName:       firstNonEmpty(req.UserName, clusterName),
		KubeconfigPath: req.KubeconfigPath,
	}

	result := &LogoutResult{}
	res, err := deps.Kubeconfig.RemoveEntry(ctx, removeIn)
	switch {
	case err == nil:
		result.Removed = res.Removed
	case errors.Is(err, kubeconfig.ErrProviderUnavailable):
		removed, fbErr := removeStaticKubeconfig(removeIn)
		if fbErr != nil {
			return nil, fmt.Errorf("kubeconfig provider unavailable and static fallback failed: %w", fbErr)
		}
		result.Removed = removed
		result.UsedFallback = true
	default:
		return nil, fmt.Errorf("remove kubeconfig entry: %w", err)
	}

	if !req.KeepCredentials {
		if handler := logoutHandler(ctx, deps, req); handler != nil {
			if err := handler.Logout(ctx); err != nil {
				return result, fmt.Errorf("logout handler %q: %w", handler.Name(), err)
			}
		}
	}
	return result, nil
}

// logoutHandler returns the handler whose cached credentials should be revoked.
// An explicit Deps.Handler wins; otherwise the resolved cluster's DefaultHandler
// is looked up best-effort so "logout <cluster>" revokes the same handler that
// "login <cluster>" used. Returns nil when no handler can be determined; the
// caller treats credential revocation as optional.
func logoutHandler(ctx context.Context, deps Deps, req LogoutRequest) Authenticator {
	if deps.Handler != nil {
		return deps.Handler
	}
	if deps.Resolver == nil || deps.HandlerLookup == nil || req.Cluster == "" {
		return nil
	}
	resolved, err := deps.Resolver.Resolve(ctx, req.Cluster)
	if err != nil || resolved == nil || resolved.DefaultHandler == "" {
		return nil
	}
	handler, err := deps.HandlerLookup(ctx, resolved.DefaultHandler)
	if err != nil {
		return nil
	}
	return handler
}
