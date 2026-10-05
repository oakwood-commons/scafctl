// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package diffreport

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
)

var (
	addedStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#00FF00")).Bold(true)
	removedStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000")).Bold(true)
	modifiedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFF00"))
	headerStyle   = lipgloss.NewStyle().Bold(true)
	summaryStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#00FFFF"))
)

// Render writes the shared human-readable diff report to the writer's stdout.
// All lines go to stdout so the report can be redirected as a whole.
func Render(w *writer.Writer, r *Report) {
	paint := func(s lipgloss.Style, text string) string {
		if w.NoColor() {
			return text
		}
		return s.Render(text)
	}

	w.Plainlnf("%s", paint(headerStyle, fmt.Sprintf("Diff: %s -> %s", r.LeftRef, r.RightRef)))
	w.Plainln("")

	if len(r.Entries) == 0 {
		w.Plainln("No differences found.")
		return
	}

	groups, order := groupEntries(r.Entries)
	for _, g := range order {
		grouped := g != ""
		if grouped {
			w.Plainlnf("%s", paint(headerStyle, g+":"))
		}
		for _, e := range groups[g] {
			w.Plainln(renderEntry(paint, e, grouped))
		}
		w.Plainln("")
	}

	summary := fmt.Sprintf("Summary: %d total | %d added | %d removed | %d modified",
		r.Summary.Total, r.Summary.Added, r.Summary.Removed, r.Summary.Modified)
	if r.Summary.Unchanged > 0 {
		summary += fmt.Sprintf(" | %d unchanged", r.Summary.Unchanged)
	}
	w.Plainln(paint(summaryStyle, summary))
}

// groupEntries buckets entries by Group, preserving first-seen group order.
func groupEntries(entries []Entry) (map[string][]Entry, []string) {
	groups := make(map[string][]Entry)
	var order []string
	for _, e := range entries {
		if _, ok := groups[e.Group]; !ok {
			order = append(order, e.Group)
		}
		groups[e.Group] = append(groups[e.Group], e)
	}
	return groups, order
}

func renderEntry(paint func(lipgloss.Style, string) string, e Entry, grouped bool) string {
	indent := "  "
	if grouped {
		indent = "    "
	}
	switch e.Kind {
	case ChangeAdded:
		return paint(addedStyle, indent+"+ "+e.Path+detailSuffix(e.Detail))
	case ChangeRemoved:
		return paint(removedStyle, indent+"- "+e.Path+detailSuffix(e.Detail))
	case ChangeModified:
		if e.Before != nil || e.After != nil {
			return fmt.Sprintf("%s%s %s: %s -> %s", indent,
				paint(modifiedStyle, "~"), e.Path,
				paint(removedStyle, fmtValue(e.Before)),
				paint(addedStyle, fmtValue(e.After)))
		}
		return paint(modifiedStyle, indent+"~ "+e.Path+detailSuffix(e.Detail))
	case ChangeUnchanged:
		return indent + "  " + e.Path
	default:
		return indent + "  " + e.Path
	}
}

func detailSuffix(detail string) string {
	if detail == "" {
		return ""
	}
	return " (" + detail + ")"
}

func fmtValue(v any) string {
	if v == nil {
		return "<nil>"
	}
	return fmt.Sprintf("%v", v)
}
