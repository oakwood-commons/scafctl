// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"fmt"
	"os"

	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/state"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
)

// summaryTimeFormat is the timestamp layout used in the summary header. It is a
// UTC RFC3339-style layout without sub-second precision.
const summaryTimeFormat = "2006-01-02T15:04:05Z"

// loadStateForDisplay resolves and loads a state file for read-only display
// commands (list, show). It validates the path, verifies the file exists, and
// returns the parsed Data. Errors are written to w and wrapped with an exit
// code so callers can simply return them.
func loadStateForDisplay(w *writer.Writer, path string) (*state.Data, error) {
	if path == "" {
		err := fmt.Errorf("--path is required")
		w.Errorf("%v", err)
		return nil, exitcode.WithCode(err, exitcode.InvalidInput)
	}

	cwd, err := os.Getwd()
	if err != nil {
		err = fmt.Errorf("cannot determine working directory: %w", err)
		w.Errorf("%v", err)
		return nil, exitcode.WithCode(err, exitcode.GeneralError)
	}

	resolved, err := state.ResolveStatePath(path, cwd)
	if err != nil {
		w.Errorf("%v", err)
		return nil, exitcode.WithCode(err, exitcode.InvalidInput)
	}
	if _, statErr := os.Stat(resolved); os.IsNotExist(statErr) {
		err := fmt.Errorf("state file not found: %s", resolved)
		w.Errorf("%v", err)
		return nil, exitcode.WithCode(err, exitcode.FileNotFound)
	}

	sd, err := state.LoadFromFile(path, cwd)
	if err != nil {
		err = fmt.Errorf("failed to load state: %w", err)
		w.Errorf("%v", err)
		return nil, exitcode.WithCode(err, exitcode.GeneralError)
	}
	return sd, nil
}

// wantsStructuredOutput reports whether output should operate on the state data
// in a single write rather than the grouped human view. This is true for
// structured/interactive formats or any CEL filter (-e/-w), mirroring how other
// commands (e.g. lint) route output when an expression is present.
func wantsStructuredOutput(f *flags.KvxOutputFlags, format kvx.OutputFormat) bool {
	return f.Interactive || kvx.IsStructuredFormat(format) ||
		f.Expression != "" || f.Where != ""
}

// writeFullView renders the entire state document for human-readable output: a
// compact summary header followed by each populated section under its own
// heading. The overview section (top-level scalars) is folded into the header.
func writeFullView(w *writer.Writer, kvxOpts *kvx.OutputOptions, sd *state.Data, quiet bool) error {
	view := state.BuildListView(sd)

	writeSummaryHeader(w, sd.Summarize())

	if view.EntryCount() == 0 && !quiet {
		w.Warning("State file has no parameters, persisted resolvers, or fingerprints stored")
	}

	for i := range view.Sections {
		s := view.Sections[i]

		// The overview section (top-level scalars like schemaVersion) is folded
		// into the summary header; those fields remain available via -o json.
		if s.Name == state.SectionNameOverview {
			continue
		}
		if err := writeSection(w, kvxOpts, s); err != nil {
			return err
		}
	}

	return nil
}

// writeSection renders a single section under its title. Empty collection (row)
// sections are skipped so headings never appear with no rows beneath them.
func writeSection(w *writer.Writer, kvxOpts *kvx.OutputOptions, s state.Section) error {
	var payload any
	switch s.Kind {
	case state.SectionKindRows:
		if len(s.Rows) == 0 {
			return nil
		}
		payload = s.Rows
	default:
		payload = s.Fields
	}

	w.SectionHeader(s.Title())
	return kvxOpts.Write(payload)
}

// writeSummaryHeader prints a compact, one-line identity + size overview above
// the detailed sections. It is suppressed under --quiet (via the writer).
func writeSummaryHeader(w *writer.Writer, s state.Summary) {
	solution := s.Solution
	if solution == "" {
		solution = "(unnamed)"
	}
	version := s.Version
	if version == "" {
		version = "-"
	}
	updated := "never"
	if !s.LastUpdated.IsZero() {
		updated = s.LastUpdated.UTC().Format(summaryTimeFormat)
	}

	w.Infof("%s@%s  schema v%d  updated %s", solution, version, s.SchemaVersion, updated)
	w.Infof("%d parameters, %d resolvers, %d fingerprints",
		s.ParameterCount, s.ResolverCount, s.FingerprintCount)
}

// scopeToSection extracts a single top-level section from the normalized state
// map. It returns the section subtree when present, or an empty map when the
// normalized value is not a map or the key is absent (so structured output and
// CEL filters operate on a well-defined, non-nil value).
func scopeToSection(normalized any, section string) any {
	m, ok := normalized.(map[string]any)
	if !ok {
		return map[string]any{}
	}
	if v, present := m[section]; present {
		return v
	}
	return map[string]any{}
}
