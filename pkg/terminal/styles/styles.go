// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package styles

import "github.com/charmbracelet/lipgloss"

var (
	SuccessStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#00FF00")). // Green
			Bold(true).
			PaddingLeft(1).
			PaddingRight(1)

	WarningStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFF00")). // Yellow
			PaddingLeft(1).
			PaddingRight(1)

	ErrorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF0000")). // Red
			Bold(true).
			PaddingLeft(1).
			PaddingRight(1)

	// ErrorTextStyle styles the error message body (red, not bold).
	// Used alongside ErrorStyle which styles the icon.
	ErrorTextStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF0000")) // Red

	InfoStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#00FFFF")). // Cyan
			PaddingLeft(1).
			PaddingRight(1)

	DebugStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF00FF")). // Magenta
			Bold(true).
			PaddingLeft(1).
			PaddingRight(1)

	VerboseStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#808080")). // Gray
			PaddingLeft(1).
			PaddingRight(1)
)
