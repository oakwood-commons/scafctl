// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"crypto/sha256"
	"encoding/hex"
)

// FingerprintHash computes a short, deterministic hash from an identity string
// for use as a cache-key segment. It prevents cross-config cache collisions by
// partitioning cache entries per unique configuration identity (e.g., clientID+tenantID).
//
// Returns "_" for an empty identity, indicating that no config-specific partitioning
// is required (e.g., the metadata-server flow where the identity is implicit).
func FingerprintHash(identity string) string {
	if identity == "" {
		return "_"
	}
	h := sha256.Sum256([]byte(identity))
	return hex.EncodeToString(h[:6]) // 12 hex chars — 48 bits of entropy
}
