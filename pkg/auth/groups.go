// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import "context"

// GroupsProvider is an optional interface that auth handlers can implement
// to return the authenticated user's group memberships as a slice of ObjectIDs.
//
// Callers should type-assert to GroupsProvider and also inspect the returned
// error with IsGroupsNotSupported(err) (and errors.Is(err, ErrHandlerDisabled)):
// plugin and disabled handler wrappers satisfy GroupsProvider at compile time
// and report unsupported handlers at runtime via ErrGroupsNotSupported.
type GroupsProvider interface {
	// GetGroups returns the ObjectIDs of all groups the authenticated user belongs to.
	// Implementations must handle pagination transparently, so all memberships —
	// including those beyond any per-token cap (e.g. the 200-group JWT limit for
	// Microsoft Entra) — are returned.
	GetGroups(ctx context.Context) ([]string, error)
}
