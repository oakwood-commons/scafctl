// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import sdkauth "github.com/oakwood-commons/scafctl-plugin-sdk/auth"

// ParseFlow converts a flow string to a Flow constant. If flowStr is empty,
// an empty Flow is returned (caller should auto-detect). handlerName is used
// to produce handler-specific error messages for unrecognised values.
func ParseFlow(flowStr, handlerName string) (Flow, error) {
	return sdkauth.ParseFlow(flowStr, handlerName)
}
