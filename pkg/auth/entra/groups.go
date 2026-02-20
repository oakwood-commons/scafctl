// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package entra

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/logger"
)

const (
	// graphGroupsMemberOfURL is the Graph API endpoint for paginated group memberships.
	// The microsoft.graph.group OData cast limits results to security/M365 groups only,
	// excluding directory roles. $select=id reduces payload size. $top=999 maximises
	// page size (Graph hard-cap is 999 for this endpoint).
	graphGroupsMemberOfURL = "https://graph.microsoft.com/v1.0/me/memberOf/microsoft.graph.group?$select=id&$top=999"

	// graphGroupsScope is the OAuth scope used to obtain a Graph access token.
	// The .default suffix requests all statically consented permissions; this includes
	// GroupMember.Read.All or Directory.Read.All which are required for /me/memberOf.
	graphGroupsScope = "https://graph.microsoft.com/.default"
)

// graphGroupsPage is the paged JSON response from Graph /me/memberOf.
type graphGroupsPage struct {
	NextLink string            `json:"@odata.nextLink"`
	Value    []graphGroupEntry `json:"value"`
}

// graphGroupEntry is a single entry in the /me/memberOf response.
type graphGroupEntry struct {
	ID string `json:"id"`
}

// GetGroups returns the ObjectIDs of all Microsoft Entra groups the authenticated
// user belongs to. It always calls the Microsoft Graph API (bypassing the 200-group
// JWT token limit) and handles pagination transparently.
//
// This method implements [auth.GroupsProvider].
//
// Prerequisites:
//   - The user must be logged in via device code flow (service principal and workload
//     identity flows do not have a /me context).
//   - The application registration must have GroupMember.Read.All or
//     Directory.Read.All consented for the https://graph.microsoft.com/.default scope.
func (h *Handler) GetGroups(ctx context.Context) ([]string, error) {
	lgr := logger.FromContext(ctx)

	if err := h.ensureSecrets(); err != nil {
		return nil, err
	}

	// /me/memberOf requires a delegated (user) token — service principal and workload
	// identity tokens represent an application identity and have no /me context.
	if HasWorkloadIdentityCredentials() {
		return nil, fmt.Errorf("group membership queries are not supported for workload identity flows: /me/memberOf requires a delegated user token")
	}
	if HasServicePrincipalCredentials() {
		return nil, fmt.Errorf("group membership queries are not supported for service principal flows: /me/memberOf requires a delegated user token")
	}

	// Acquire a Graph access token via the existing refresh-token machinery.
	token, err := h.GetToken(ctx, auth.TokenOptions{Scope: graphGroupsScope})
	if err != nil {
		return nil, fmt.Errorf("failed to acquire Graph token for group lookup: %w", err)
	}

	// Paginate through all group memberships.
	var groups []string
	nextURL := graphGroupsMemberOfURL

	for nextURL != "" {
		lgr.V(1).Info("fetching group memberships from Graph", "url", nextURL)

		resp, err := h.graphClient.Get(ctx, nextURL, token.AccessToken)
		if err != nil {
			return nil, fmt.Errorf("graph memberOf request failed: %w", err)
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to read Graph memberOf response: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("graph memberOf returned HTTP %d: %s", resp.StatusCode, truncate(string(body), 512))
		}

		var page graphGroupsPage
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, fmt.Errorf("failed to parse Graph memberOf response: %w", err)
		}

		for _, entry := range page.Value {
			if entry.ID != "" {
				groups = append(groups, entry.ID)
			}
		}

		nextURL = page.NextLink
		lgr.V(2).Info("fetched group membership page", "pageSize", len(page.Value), "hasMore", nextURL != "")
	}

	lgr.V(1).Info("fetched all group memberships", "count", len(groups))
	return groups, nil
}

// truncate limits s to at most n bytes for safe error message embedding.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// Compile-time check that Handler implements auth.GroupsProvider.
var _ auth.GroupsProvider = (*Handler)(nil)
