// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import "github.com/mark3labs/mcp-go/mcp"

// enumOpt is a guarded wrapper around mcp.Enum for enum value sets that are
// computed at registration time and may legitimately be empty.
//
// mcp-go's mcp.Enum writes schema["enum"] = values unconditionally. A nil or
// empty []string then serializes to "enum": null (nil) or "enum": [] (empty),
// both of which are wrong for a "any string is allowed" property:
//
//   - "enum": null is INVALID JSON Schema draft 2020-12 (enum MUST be an
//     array). A strict MCP client (Claude Desktop/Code, the Anthropic API)
//     validates every advertised tool schema and rejects the ENTIRE tool list
//     when any one is invalid, so a single null-enum silently disables every
//     tool on the server (see issue #819).
//   - "enum": [] is valid JSON but means "no value is permitted", which would
//     reject every input -- also not the intent.
//
// The correct representation of "no known constraint" is to omit the enum key
// entirely. enumOpt returns a no-op PropertyOption when values is empty so the
// key is never written; otherwise it delegates to mcp.Enum. Apply it at every
// mcp.Enum call site unconditionally -- even where the value set is a
// compile-time constant today -- so no site can silently reintroduce the bug if
// its values later become computed.
func enumOpt(values ...string) mcp.PropertyOption {
	if len(values) == 0 {
		return func(map[string]any) {}
	}
	return mcp.Enum(values...)
}
