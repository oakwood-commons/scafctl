// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package login

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/kube"
	"github.com/oakwood-commons/scafctl/pkg/kubeconfig"
)

// resolveCluster assembles the cluster connection details from (in priority
// order) explicit request flags, the configured cluster resolver, and
// best-effort auto-detection of the authentication method. Explicit flags
// always override resolved values.
func resolveCluster(ctx context.Context, deps Deps, req Request) (kube.ClusterInfo, error) {
	var info kube.ClusterInfo

	// A concrete http(s) URL argument is used directly as the API server (no
	// resolver required), and is not treated as the logical cluster name.
	clusterArg := req.Cluster
	switch {
	case clusterArg != "" && isConcreteClusterURL(clusterArg):
		info.APIServerURL = clusterArg
		clusterArg = ""
	case clusterArg != "" && deps.Resolver != nil:
		resolved, err := deps.Resolver.Resolve(ctx, clusterArg)
		switch {
		case err != nil && req.Server != "":
			// An explicit --server supplies the connection details, so a
			// resolver miss (an unlisted name or an unavailable dynamic
			// inventory) is non-fatal here: fall back to treating the
			// positional as a plain cluster name.
		case err != nil:
			return kube.ClusterInfo{}, fmt.Errorf("resolve cluster %q: %w", clusterArg, err)
		case resolved != nil:
			info = *resolved
		}
	}
	if info.Name == "" {
		info.Name = firstNonEmpty(clusterArg, req.ClusterName)
	}

	if req.Server != "" {
		info.APIServerURL = req.Server
	}
	if req.Audience != "" {
		info.OIDCAudience = req.Audience
	}
	if req.InsecureSkipTLS {
		info.InsecureSkipTLS = true
		// The kubeconfig writer prefers CAData over InsecureSkipTLS, so a
		// resolver-supplied CA bundle would silently defeat the explicit
		// insecure flag. Clear it so --insecure-skip-tls-verify takes effect.
		info.CAData = ""
	}

	if info.APIServerURL == "" {
		return kube.ClusterInfo{}, ErrNoServer
	}

	// Fall back to a name derived from the server host for the direct
	// --server flow (no cluster argument and no --cluster-name), which the CLI
	// advertises. Validate() rejects an empty name, so this keeps that flow
	// working instead of forcing users to add --cluster-name.
	if info.Name == "" {
		info.Name = clusterNameFromServer(info.APIServerURL)
	}

	// Auto-detect the auth method when it is unset. Detection failure (including
	// an unavailable provider) is non-fatal: the auth type stays auto and the
	// handler decides the flow.
	if info.AuthType == kube.AuthTypeAuto && deps.Kubeconfig != nil {
		det, err := deps.Kubeconfig.DetectAuthType(ctx, kubeconfig.DetectInput{
			Server:          info.APIServerURL,
			InsecureSkipTLS: info.InsecureSkipTLS,
		})
		if err == nil && det.Success && det.AuthType.Valid() {
			info.AuthType = det.AuthType
		}
	}

	if err := info.Validate(); err != nil {
		return kube.ClusterInfo{}, err
	}
	return info, nil
}

// isConcreteClusterURL reports whether the cluster argument is already an
// absolute http(s) URL, in which case it is used directly as the API server
// endpoint rather than resolved by name.
func isConcreteClusterURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != ""
}

// clusterNameFromServer derives a kubeconfig entry name from an API server URL,
// used for the direct --server flow when no cluster name is supplied. It returns
// the host (without port), falling back to the trimmed raw server string so the
// result is never empty for a non-empty server.
func clusterNameFromServer(server string) string {
	if u, err := url.Parse(server); err == nil && u.Hostname() != "" {
		return u.Hostname()
	}
	return strings.TrimSpace(server)
}
