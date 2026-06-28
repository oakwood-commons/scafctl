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

	if req.Cluster != "" && deps.Resolver != nil {
		resolved, err := deps.Resolver.Resolve(ctx, req.Cluster)
		if err != nil {
			return kube.ClusterInfo{}, fmt.Errorf("resolve cluster %q: %w", req.Cluster, err)
		}
		if resolved != nil {
			info = *resolved
		}
	}
	if info.Name == "" {
		info.Name = firstNonEmpty(req.Cluster, req.ClusterName)
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
