// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lint

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/refactor"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/pmezard/go-difflib/difflib"
)

// ErrNotFixable signals that a specific finding cannot be auto-fixed in the
// current solution (for example, the camelCase target of a hyphenated-name
// rename collides with an existing resolver, or a reference could not be
// located byte-exact). A fixer wraps it so callers can distinguish a per-finding
// skip (recorded as an un-applied FixOutcome) from a hard failure that aborts
// the whole fix run.
var ErrNotFixable = errors.New("finding is not auto-fixable")

// Fix is the outcome a Fixer computes for a single finding: the source edits
// that resolve it plus a short human-readable summary of what changed.
type Fix struct {
	// Edits are source-preserving text edits that resolve the finding. They are
	// applied against the exact bytes the fixer was given (no YAML round-trip),
	// so comments, key order, and formatting are preserved verbatim.
	Edits []refactor.TextEdit
	// Detail is a short human summary, e.g. `renamed "my-svc" -> "mySvc"`.
	Detail string
}

// Fixer computes the edits that resolve a single finding. It returns a wrapped
// ErrNotFixable when this specific finding cannot be auto-fixed; the caller
// records that as a skip, not a hard failure. Any other error aborts the whole
// fix run so a partially-fixed file is never persisted.
//
// IMPORTANT: implementations must treat f as an IDENTIFIER ONLY (e.g. use
// f.RuleName / f.Location to decide what to fix) and must recompute every edit
// offset from the passed sol. The finding's byte ranges are computed from the
// ORIGINAL source, whereas sol is re-parsed from the already-partially-fixed
// bytes on each iteration (see ComputeFixPlan); reusing f's offsets would
// produce corrupt edits after a preceding fix shifts the source.
type Fixer func(sol *solution.Solution, f *Finding) (*Fix, error)

// defaultFixers maps a rule name to its Fixer. Rules opt into auto-fixing by
// registering here (see fix_hyphenated.go). Keeping the mapping in the domain
// layer lets CLI, MCP, and future API consumers share one source of truth for
// which rules are fixable.
var defaultFixers = map[string]Fixer{}

// RegisterFixer attaches a fixer to a rule name. It is intended to be called
// from package init blocks. Registering a second fixer for the same rule
// overwrites the first.
func RegisterFixer(ruleName string, fx Fixer) {
	defaultFixers[ruleName] = fx
}

// Fixable reports whether the named rule has a registered auto-fixer.
func Fixable(ruleName string) bool {
	_, ok := defaultFixers[ruleName]
	return ok
}

// FixableRuleNames returns the sorted names of all rules that have a registered
// auto-fixer.
func FixableRuleNames() []string {
	names := make([]string, 0, len(defaultFixers))
	for name := range defaultFixers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// FixOutcome records what happened to one fixable finding during a fix run.
type FixOutcome struct {
	RuleName string `json:"ruleName" yaml:"ruleName" doc:"Lint rule that produced the finding" maxLength:"128" example:"hyphenated-name"`
	Location string `json:"location" yaml:"location" doc:"Logical path of the finding" maxLength:"512" example:"resolvers.my-svc"`
	Applied  bool   `json:"applied" yaml:"applied" doc:"Whether the fix was applied" example:"true"`
	Detail   string `json:"detail" yaml:"detail" doc:"Summary of the fix or skip reason" maxLength:"2048" example:"renamed \"my-svc\" -> \"mySvc\""`
}

// FixPlan is the aggregate outcome of a fix run: per-finding outcomes plus the
// rewritten file bytes. NewContent always holds the final bytes (equal to the
// original when nothing changed), so a diff preview and the applied write are
// derived from the same source and can never drift.
type FixPlan struct {
	Outcomes   []FixOutcome `json:"outcomes" yaml:"outcomes" doc:"Per-finding fix outcomes" maxItems:"1000"`
	NewContent []byte       `json:"-" yaml:"-"`
	Changed    bool         `json:"changed" yaml:"changed" doc:"Whether any fix was applied" example:"true"`
}

// AppliedCount returns the number of outcomes whose fix was applied.
func (p *FixPlan) AppliedCount() int {
	n := 0
	for _, o := range p.Outcomes {
		if o.Applied {
			n++
		}
	}
	return n
}

// SkippedCount returns the number of outcomes whose fix was skipped.
func (p *FixPlan) SkippedCount() int {
	return len(p.Outcomes) - p.AppliedCount()
}

// UnifiedDiff renders a git-style unified diff between original and the plan's
// rewritten content, labelled with path. It returns an empty string when
// nothing changed.
func (p *FixPlan) UnifiedDiff(path string, original []byte) (string, error) {
	if !p.Changed {
		return "", nil
	}
	return difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A:        splitDiffLines(string(original)),
		B:        splitDiffLines(string(p.NewContent)),
		FromFile: "a/" + path,
		ToFile:   "b/" + path,
		Context:  3,
	})
}

// splitDiffLines splits s into newline-terminated lines for difflib. Unlike
// difflib.SplitLines it does NOT append a trailing "\n" to the last element:
// that helper turns a file ending in "\n" into a spurious empty final line,
// which makes the emitted hunk claim one more line than the file actually has.
// GNU patch(1) rejects that phantom trailing context line ("Hunk #N FAILED"),
// even though BSD patch tolerates it -- so the extra line breaks the
// --diff -> patch round-trip on Linux CI. Dropping the trailing empty element
// keeps hunk line counts exact and the diff cleanly appliable.
func splitDiffLines(s string) []string {
	lines := strings.SplitAfter(s, "\n")
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	return lines
}

// ComputeFixPlan lints the solution parsed from raw and applies every registered
// fixer to the fixable findings, producing a FixPlan. It never writes to disk --
// IO stays in the command layer.
//
// Fixes are applied iteratively: each fixer runs against a solution freshly
// parsed from the current (already-partially-fixed) bytes, so byte offsets stay
// valid across successive renames. A fixer's wrapped ErrNotFixable is recorded
// as a skipped outcome and the run continues; any other error aborts the run so
// a partially-fixed file is never returned.
func ComputeFixPlan(raw []byte, filePath string, registry *provider.Registry) (*FixPlan, error) {
	plan := &FixPlan{}

	sol := &solution.Solution{}
	if err := sol.UnmarshalFromBytes(raw); err != nil {
		return nil, fmt.Errorf("parse solution: %w", err)
	}
	result := Solution(sol, filePath, registry)

	// Findings originate from ranging over Go maps (e.g. spec.resolvers), so
	// their order is nondeterministic across runs. Collect the fixable ones and
	// sort by rule then location so the applied edits -- and therefore the
	// resulting bytes and any --diff preview -- are stable and reproducible, and
	// interacting renames (e.g. target-name collisions) resolve identically
	// every run.
	fixable := make([]*Finding, 0, len(result.Findings))
	for _, f := range result.Findings {
		if _, ok := defaultFixers[f.RuleName]; ok {
			fixable = append(fixable, f)
		}
	}
	sort.SliceStable(fixable, func(i, j int) bool {
		if fixable[i].RuleName != fixable[j].RuleName {
			return fixable[i].RuleName < fixable[j].RuleName
		}
		return fixable[i].Location < fixable[j].Location
	})

	current := append([]byte(nil), raw...)
	for _, f := range fixable {
		fx := defaultFixers[f.RuleName]

		// Re-parse the current bytes so the fixer computes edits against
		// up-to-date offsets after any preceding rename.
		cur := &solution.Solution{}
		if err := cur.UnmarshalFromBytes(current); err != nil {
			return nil, fmt.Errorf("re-parse solution before fixing %s at %s: %w", f.RuleName, f.Location, err)
		}

		fix, err := fx(cur, f)
		if err != nil {
			if errors.Is(err, ErrNotFixable) {
				plan.Outcomes = append(plan.Outcomes, FixOutcome{
					RuleName: f.RuleName,
					Location: f.Location,
					Applied:  false,
					Detail:   err.Error(),
				})
				continue
			}
			return nil, fmt.Errorf("compute fix for %s at %s: %w", f.RuleName, f.Location, err)
		}
		if fix == nil || len(fix.Edits) == 0 {
			continue
		}

		next, err := refactor.Apply(current, fix.Edits)
		if err != nil {
			return nil, fmt.Errorf("apply fix for %s at %s: %w", f.RuleName, f.Location, err)
		}
		current = next
		plan.Outcomes = append(plan.Outcomes, FixOutcome{
			RuleName: f.RuleName,
			Location: f.Location,
			Applied:  true,
			Detail:   fix.Detail,
		})
	}

	plan.NewContent = current
	plan.Changed = !bytes.Equal(raw, current)
	return plan, nil
}
