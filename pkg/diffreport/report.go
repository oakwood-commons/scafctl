// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package diffreport provides a normalized, command-agnostic diff model shared
// by all `diff` subcommands (solution, bundle, snapshot). Each command maps its
// native result into a Report, which drives both the human-readable default
// render and the flattened entry rows consumed by kvx tabular/interactive output.
package diffreport

// ChangeKind classifies a single diff entry.
type ChangeKind string

// Change kinds shared across every diff command.
const (
	ChangeAdded     ChangeKind = "added"
	ChangeRemoved   ChangeKind = "removed"
	ChangeModified  ChangeKind = "modified"
	ChangeUnchanged ChangeKind = "unchanged"
)

// Entry is a single normalized difference. The field names are the contract for
// json/yaml serialization, the kvx table columns, and the display schema, so the
// json and yaml tags must stay aligned.
type Entry struct {
	Group  string     `json:"group,omitempty" yaml:"group,omitempty" doc:"Section this entry belongs to (e.g. resolvers, files); empty for ungrouped diffs"`
	Path   string     `json:"path" yaml:"path" doc:"Identifier of the changed item (field, name, or path)"`
	Kind   ChangeKind `json:"kind" yaml:"kind" doc:"Change kind (added, removed, modified, unchanged)"`
	Before any        `json:"before,omitempty" yaml:"before,omitempty" doc:"Value before the change"`
	After  any        `json:"after,omitempty" yaml:"after,omitempty" doc:"Value after the change"`
	Detail string     `json:"detail,omitempty" yaml:"detail,omitempty" doc:"Human-readable supplementary detail (e.g. size, version range)"`
}

// Summary counts entries by change kind.
type Summary struct {
	Total     int `json:"total" yaml:"total" doc:"Total number of entries"`
	Added     int `json:"added" yaml:"added" doc:"Number of additions"`
	Removed   int `json:"removed" yaml:"removed" doc:"Number of removals"`
	Modified  int `json:"modified" yaml:"modified" doc:"Number of modifications"`
	Unchanged int `json:"unchanged,omitempty" yaml:"unchanged,omitempty" doc:"Number of unchanged entries"`
}

// Report is the normalized diff shared by all diff subcommands.
type Report struct {
	Kind     string  `json:"kind" yaml:"kind" doc:"Artifact kind being compared (solution, bundle, snapshot)"`
	LeftRef  string  `json:"leftRef" yaml:"leftRef" doc:"Reference/label for the left (before) side"`
	RightRef string  `json:"rightRef" yaml:"rightRef" doc:"Reference/label for the right (after) side"`
	Entries  []Entry `json:"entries" yaml:"entries" doc:"Normalized list of differences"`
	Summary  Summary `json:"summary" yaml:"summary" doc:"Counts by change kind"`
}

// New returns an empty Report for the given artifact kind and refs. Entries is a
// non-nil empty slice so structured output emits [] rather than null.
func New(kind, leftRef, rightRef string) *Report {
	return &Report{
		Kind:     kind,
		LeftRef:  leftRef,
		RightRef: rightRef,
		Entries:  []Entry{},
	}
}

// Add appends an entry and updates the summary counts.
func (r *Report) Add(e Entry) {
	r.Entries = append(r.Entries, e)
	r.Summary.Total++
	switch e.Kind {
	case ChangeAdded:
		r.Summary.Added++
	case ChangeRemoved:
		r.Summary.Removed++
	case ChangeModified:
		r.Summary.Modified++
	case ChangeUnchanged:
		r.Summary.Unchanged++
	}
}
