// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"fmt"

	"github.com/oakwood-commons/scafctl/pkg/spec"
)

// FileBackendProvider is the provider name used for file-backed state. It is
// the backend synthesized by ConfigForFile when a caller points a run at an
// explicit state file.
const FileBackendProvider = "file"

// BackendInputPath is the backend input key that carries the state file
// location for file-backed state.
const BackendInputPath = "path"

// ConfigForFile builds a state Config that reads and writes a single state
// file at path, using the builtin file backend, saved in the given format
// (FormatFull or FormatIntent; empty is treated as FormatFull).
//
// It is the programmatic equivalent of this solution-level block:
//
//	state:
//	  enabled: true
//	  backend:
//	    provider: file
//	    format: <format>
//	    inputs:
//	      path: <path>
//
// It exists so a caller (for example a CLI flag, or an embedder) can point a
// run at a specific state file without the solution having to declare a state
// block of its own, and without threading the path through a CLI parameter.
// Routing the path through a parameter would persist it into the saved
// parameter set, permanently polluting the document. The returned Config has
// no Emit targets: pointing a run at an explicit file is a single, complete
// substitution for the solution's own state configuration, not an additional
// output.
//
// The solution's own state block remains the primary way to configure state;
// this is an alternate input source that overrides it. See DescribeOverride
// for the override-reporting helper used to tell the user what was replaced.
func ConfigForFile(path, format string) (*Config, error) {
	if path == "" {
		return nil, fmt.Errorf("state file path is required")
	}

	enabled := &spec.ValueRef{Literal: true}
	return &Config{
		Enabled: enabled,
		Backend: Backend{
			Provider: FileBackendProvider,
			Format:   format,
			Inputs: map[string]*spec.ValueRef{
				BackendInputPath: {Literal: path},
			},
		},
	}, nil
}

// OverrideInfo describes how an explicit state file path relates to a
// solution's own state configuration. It lets a caller report an override to
// the user without duplicating the comparison logic.
type OverrideInfo struct {
	// Overridden is true when the solution declared a state block that the
	// explicit path replaces. It is false when the solution declared no state
	// block, in which case nothing was overridden and no message is warranted.
	Overridden bool

	// PreviousProvider is the backend provider the solution configured, when
	// known. Empty when the solution declared no state block.
	PreviousProvider string

	// DroppedEmits is the number of Emit targets the solution's own state
	// block declared. An explicit --state-file is a complete substitution for
	// state (see ConfigForFile), so these targets are silently dropped unless
	// the caller surfaces this count to the user.
	DroppedEmits int
}

// DescribeOverride reports what an explicit state file path replaces in the
// given solution state config. A nil config means the solution declared no
// state block, so the explicit path adds state rather than overriding it.
//
// The returned info is advisory: the caller decides whether and how to surface
// it. The path is always honored regardless.
func DescribeOverride(cfg *Config) OverrideInfo {
	if cfg == nil {
		return OverrideInfo{}
	}
	return OverrideInfo{
		Overridden:       true,
		PreviousProvider: cfg.Backend.Provider,
		DroppedEmits:     len(cfg.Emit),
	}
}
