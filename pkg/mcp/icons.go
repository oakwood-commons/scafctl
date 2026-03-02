// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import "github.com/mark3labs/mcp-go/mcp"

// Icon constants for MCP tools, resources, and prompts.
// These use data URIs with inline SVGs for maximum compatibility across
// MCP clients (VS Code, Claude Desktop, Cursor, etc.). Each icon is a
// simple, recognizable symbol that communicates the tool's purpose.

// toolIcons maps tool categories to their icons.
var toolIcons = map[string]mcp.Icon{
	"solution": {
		Src:      "data:image/svg+xml,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24' fill='none' stroke='%234A90D9' stroke-width='2'><path d='M14 2H6a2 2 0 00-2 2v16a2 2 0 002 2h12a2 2 0 002-2V8z'/><path d='M14 2v6h6'/><path d='M8 13h8'/><path d='M8 17h8'/></svg>",
		MIMEType: "image/svg+xml",
	},
	"provider": {
		Src:      "data:image/svg+xml,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24' fill='none' stroke='%2350C878' stroke-width='2'><rect x='2' y='3' width='20' height='18' rx='2'/><path d='M7 8h10M7 12h10M7 16h6'/></svg>",
		MIMEType: "image/svg+xml",
	},
	"cel": {
		Src:      "data:image/svg+xml,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24' fill='none' stroke='%23E57373' stroke-width='2'><path d='M7 8l-4 4 4 4M17 8l4 4-4 4'/><line x1='14' y1='4' x2='10' y2='20'/></svg>",
		MIMEType: "image/svg+xml",
	},
	"template": {
		Src:      "data:image/svg+xml,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24' fill='none' stroke='%23FFB74D' stroke-width='2'><path d='M12 2L2 7l10 5 10-5-10-5z'/><path d='M2 17l10 5 10-5'/><path d='M2 12l10 5 10-5'/></svg>",
		MIMEType: "image/svg+xml",
	},
	"schema": {
		Src:      "data:image/svg+xml,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24' fill='none' stroke='%239575CD' stroke-width='2'><path d='M4 7V4a2 2 0 012-2h8.5L20 7.5V20a2 2 0 01-2 2H6a2 2 0 01-2-2v-3'/><path d='M14 2v6h6'/><path d='M3 15l3-3-3-3'/></svg>",
		MIMEType: "image/svg+xml",
	},
	"example": {
		Src:      "data:image/svg+xml,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24' fill='none' stroke='%2326A69A' stroke-width='2'><path d='M2 3h6a4 4 0 014 4v14a3 3 0 00-3-3H2z'/><path d='M22 3h-6a4 4 0 00-4 4v14a3 3 0 013-3h7z'/></svg>",
		MIMEType: "image/svg+xml",
	},
	"catalog": {
		Src:      "data:image/svg+xml,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24' fill='none' stroke='%23AB47BC' stroke-width='2'><path d='M4 19.5A2.5 2.5 0 016.5 17H20'/><path d='M6.5 2H20v20H6.5A2.5 2.5 0 014 19.5v-15A2.5 2.5 0 016.5 2z'/></svg>",
		MIMEType: "image/svg+xml",
	},
	"auth": {
		Src:      "data:image/svg+xml,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24' fill='none' stroke='%23EF5350' stroke-width='2'><path d='M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z'/></svg>",
		MIMEType: "image/svg+xml",
	},
	"lint": {
		Src:      "data:image/svg+xml,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24' fill='none' stroke='%23FFA726' stroke-width='2'><path d='M22 11.08V12a10 10 0 11-5.93-9.14'/><path d='M22 4L12 14.01l-3-3'/></svg>",
		MIMEType: "image/svg+xml",
	},
	"scaffold": {
		Src:      "data:image/svg+xml,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24' fill='none' stroke='%2342A5F5' stroke-width='2'><rect x='3' y='3' width='18' height='18' rx='2'/><path d='M3 9h18M9 21V9'/></svg>",
		MIMEType: "image/svg+xml",
	},
	"action": {
		Src:      "data:image/svg+xml,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24' fill='none' stroke='%23FF7043' stroke-width='2'><polygon points='5,3 19,12 5,21 5,3'/></svg>",
		MIMEType: "image/svg+xml",
	},
	"diff": {
		Src:      "data:image/svg+xml,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24' fill='none' stroke='%237E57C2' stroke-width='2'><line x1='18' y1='20' x2='18' y2='10'/><line x1='12' y1='20' x2='12' y2='4'/><line x1='6' y1='20' x2='6' y2='14'/></svg>",
		MIMEType: "image/svg+xml",
	},
	"dryrun": {
		Src:      "data:image/svg+xml,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24' fill='none' stroke='%2366BB6A' stroke-width='2'><circle cx='12' cy='12' r='10'/><path d='M12 6v6l4 2'/></svg>",
		MIMEType: "image/svg+xml",
	},
	"config": {
		Src:      "data:image/svg+xml,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24' fill='none' stroke='%2378909C' stroke-width='2'><circle cx='12' cy='12' r='3'/><path d='M19.4 15a1.65 1.65 0 00.33 1.82l.06.06a2 2 0 010 2.83 2 2 0 01-2.83 0l-.06-.06a1.65 1.65 0 00-1.82-.33 1.65 1.65 0 00-1 1.51V21a2 2 0 01-4 0v-.09A1.65 1.65 0 009 19.4a1.65 1.65 0 00-1.82.33l-.06.06a2 2 0 01-2.83-2.83l.06-.06A1.65 1.65 0 004.68 15a1.65 1.65 0 00-1.51-1H3a2 2 0 010-4h.09A1.65 1.65 0 004.6 9a1.65 1.65 0 00-.33-1.82l-.06-.06a2 2 0 012.83-2.83l.06.06A1.65 1.65 0 009 4.68a1.65 1.65 0 001-1.51V3a2 2 0 014 0v.09a1.65 1.65 0 001 1.51 1.65 1.65 0 001.82-.33l.06-.06a2 2 0 012.83 2.83l-.06.06A1.65 1.65 0 0019.4 9a1.65 1.65 0 001.51 1H21a2 2 0 010 4h-.09a1.65 1.65 0 00-1.51 1z'/></svg>",
		MIMEType: "image/svg+xml",
	},
	"refs": {
		Src:      "data:image/svg+xml,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24' fill='none' stroke='%235C6BC0' stroke-width='2'><path d='M10 13a5 5 0 007.54.54l3-3a5 5 0 00-7.07-7.07l-1.72 1.71'/><path d='M14 11a5 5 0 00-7.54-.54l-3 3a5 5 0 007.07 7.07l1.71-1.71'/></svg>",
		MIMEType: "image/svg+xml",
	},
	"testing": {
		Src:      "data:image/svg+xml,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24' fill='none' stroke='%234CAF50' stroke-width='2'><path d='M14.7 6.3a1 1 0 000 1.4l1.6 1.6a1 1 0 001.4 0l3.77-3.77a6 6 0 01-7.94 7.94l-6.91 6.91a2.12 2.12 0 01-3-3l6.91-6.91a6 6 0 017.94-7.94l-3.76 3.76z'/></svg>",
		MIMEType: "image/svg+xml",
	},
	"snapshot": {
		Src:      "data:image/svg+xml,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24' fill='none' stroke='%238D6E63' stroke-width='2'><path d='M23 19a2 2 0 01-2 2H3a2 2 0 01-2-2V8a2 2 0 012-2h4l2-3h6l2 3h4a2 2 0 012 2z'/><circle cx='12' cy='13' r='4'/></svg>",
		MIMEType: "image/svg+xml",
	},
	"version": {
		Src:      "data:image/svg+xml,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24' fill='none' stroke='%2390A4AE' stroke-width='2'><circle cx='12' cy='12' r='10'/><line x1='12' y1='16' x2='12' y2='12'/><line x1='12' y1='8' x2='12.01' y2='8'/></svg>",
		MIMEType: "image/svg+xml",
	},
	"help": {
		Src:      "data:image/svg+xml,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24' fill='none' stroke='%2329B6F6' stroke-width='2'><circle cx='12' cy='12' r='10'/><path d='M9.09 9a3 3 0 015.83 1c0 2-3 3-3 3'/><line x1='12' y1='17' x2='12.01' y2='17'/></svg>",
		MIMEType: "image/svg+xml",
	},
}

// promptIcons maps prompt categories to their icons.
var promptIcons = map[string]mcp.Icon{
	"create": {
		Src:      "data:image/svg+xml,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24' fill='none' stroke='%234A90D9' stroke-width='2'><circle cx='12' cy='12' r='10'/><line x1='12' y1='8' x2='12' y2='16'/><line x1='8' y1='12' x2='16' y2='12'/></svg>",
		MIMEType: "image/svg+xml",
	},
	"debug": {
		Src:      "data:image/svg+xml,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24' fill='none' stroke='%23E57373' stroke-width='2'><path d='M12 22c-4.97 0-9-2.69-9-6v-4c0-3.31 4.03-6 9-6s9 2.69 9 6v4c0 3.31-4.03 6-9 6z'/><path d='M12 6V2M6 18H2M22 18h-4M20 10h2M2 10h2M6.3 6.3L4 4M17.7 6.3L20 4'/></svg>",
		MIMEType: "image/svg+xml",
	},
	"guide": {
		Src:      "data:image/svg+xml,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24' fill='none' stroke='%2326A69A' stroke-width='2'><path d='M2 3h6a4 4 0 014 4v14a3 3 0 00-3-3H2z'/><path d='M22 3h-6a4 4 0 00-4 4v14a3 3 0 013-3h7z'/></svg>",
		MIMEType: "image/svg+xml",
	},
	"analyze": {
		Src:      "data:image/svg+xml,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24' fill='none' stroke='%237E57C2' stroke-width='2'><circle cx='11' cy='11' r='8'/><line x1='21' y1='21' x2='16.65' y2='16.65'/><line x1='11' y1='8' x2='11' y2='14'/><line x1='8' y1='11' x2='14' y2='11'/></svg>",
		MIMEType: "image/svg+xml",
	},
}

// resourceIcons maps resource types to their icons.
var resourceIcons = map[string]mcp.Icon{
	"solution": {
		Src:      "data:image/svg+xml,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24' fill='none' stroke='%234A90D9' stroke-width='2'><path d='M14 2H6a2 2 0 00-2 2v16a2 2 0 002 2h12a2 2 0 002-2V8z'/><path d='M14 2v6h6'/></svg>",
		MIMEType: "image/svg+xml",
	},
	"provider": {
		Src:      "data:image/svg+xml,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24' fill='none' stroke='%2350C878' stroke-width='2'><rect x='2' y='3' width='20' height='18' rx='2'/><path d='M7 8h10M7 12h10M7 16h6'/></svg>",
		MIMEType: "image/svg+xml",
	},
}
