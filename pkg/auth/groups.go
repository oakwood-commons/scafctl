// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import "context"

// GroupsProvider is an optional interface that auth handlers can implement
// to return the authenticated user's group memberships as a slice of ObjectIDs.
//
// Handlers that do not implement this interface do not support group membership
// queries. Callers should type-assert to this interface before invoking GetGroups.
type GroupsProvider interface {
	// GetGroups returns the ObjectIDs of all groups the authenticated user belongs to.
	// Implementations must handle pagination transparently, so all memberships —
	// including those beyond any per-token cap (e.g. the 200-group JWT limit for
	// Microsoft Entra) — are returned.
	GetGroups(ctx context.Context) ([]string, error)
}
