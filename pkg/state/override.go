// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"fmt"

	"github.com/oakwood-commons/scafctl/pkg/spec"
)

// FileProviderName is the provider name used for file-backed state. It is
// the provider ApplyOverrides synthesizes for --state-file / --state-output.
const FileProviderName = "file"

// FileInputPath is the file provider input key that carries the state file
// location.
const FileInputPath = "path"

// Overrides are caller-level replacements for a solution's declared state
// configuration -- the programmatic form of the CLI's --state-file,
// --state-output, and --no-state-output flags. Each replaces exactly one half
// of the configuration: LoadFile replaces the load block, and OutputFile or
// NoOutput replace the save targets. LoadFile and OutputFile also enable
// state for the run (see ApplyOverrides and OverrideInfo.OverridesEnabled).
type Overrides struct {
	// LoadFile, when set, replaces the declared load block with a file load at
	// this path. The declared save targets still run.
	LoadFile string

	// OutputFile, when set, replaces every declared save target with a single
	// full-format file save at this path.
	OutputFile string

	// NoOutput disables every save target. Loading and immutable verification
	// still happen.
	NoOutput bool
}

// IsZero reports whether no override is set.
func (o Overrides) IsZero() bool {
	return o.LoadFile == "" && o.OutputFile == "" && !o.NoOutput
}

// ApplyOverrides returns the effective state configuration for a run. declared
// is the solution's own state block and may be nil; a nil result means state is
// not configured for this run. declared itself is never modified.
//
// extends: load targets are resolved against the DECLARED load block before
// LoadFile replaces it, so overriding where a run reads from never redirects
// where an extends target writes -- a file passed to read from is never
// overwritten by a save target the solution meant for its own load location.
//
// LoadFile and OutputFile are explicit requests for state, so they enable it
// even when the solution declared no state block or declared it disabled.
// NoOutput alone never enables state.
func ApplyOverrides(declared *Config, o Overrides) (*Config, error) {
	if o.OutputFile != "" && o.NoOutput {
		return nil, fmt.Errorf("state: an output file and disabling output are mutually exclusive")
	}
	if o.IsZero() {
		return declared, nil
	}
	if declared == nil && o.LoadFile == "" && o.OutputFile == "" {
		// Disabling output of a solution with no state block: nothing to change.
		return nil, nil
	}

	cfg := materializeExtends(declared)
	if cfg == nil {
		cfg = &Config{}
	}
	if o.LoadFile != "" || o.OutputFile != "" {
		cfg.Enabled = &spec.ValueRef{Literal: true}
	}
	if o.LoadFile != "" {
		cfg.Load = &LoadConfig{
			Provider: FileProviderName,
			Inputs:   map[string]*spec.ValueRef{FileInputPath: {Literal: o.LoadFile}},
		}
	}
	switch {
	case o.OutputFile != "":
		cfg.Save = []SaveTarget{{
			Provider: FileProviderName,
			Format:   FormatFull,
			Inputs:   map[string]*spec.ValueRef{FileInputPath: {Literal: o.OutputFile}},
		}}
	case o.NoOutput:
		cfg.Save = nil
	}
	return cfg, nil
}

// OverrideInfo reports what an Overrides value replaces in a declared
// configuration, so a caller can tell the user without re-deriving the
// comparison. Zero values mean nothing declared was replaced.
type OverrideInfo struct {
	// ReplacedLoad is true when LoadFile replaces a declared load block.
	ReplacedLoad bool

	// ReplacedLoadProvider is the provider of the replaced load block.
	ReplacedLoadProvider string

	// ReplacedSaveTargets is the number of declared save targets that
	// OutputFile or NoOutput replace.
	ReplacedSaveTargets int

	// OverridesEnabled is true when LoadFile or OutputFile force state on
	// over a declared enabled condition that could otherwise turn it off (any
	// value other than an absent or literal true enabled). The condition is
	// not evaluated: it may depend on parameters that only the state being
	// loaded carries, so it cannot be judged before the load.
	OverridesEnabled bool
}

// DescribeOverrides reports what o replaces in declared. The result is
// advisory: the caller decides whether and how to surface it.
func DescribeOverrides(declared *Config, o Overrides) OverrideInfo {
	var info OverrideInfo
	if declared == nil {
		return info
	}
	if o.LoadFile != "" && declared.Load != nil {
		info.ReplacedLoad = true
		info.ReplacedLoadProvider = declared.Load.Provider
	}
	if o.OutputFile != "" || o.NoOutput {
		info.ReplacedSaveTargets = len(declared.Save)
	}
	if (o.LoadFile != "" || o.OutputFile != "") && !alwaysEnabled(declared.Enabled) {
		info.OverridesEnabled = true
	}
	return info
}

// alwaysEnabled reports whether an enabled condition can never turn state off:
// absent, or the literal true.
func alwaysEnabled(enabled *spec.ValueRef) bool {
	if enabled == nil {
		return true
	}
	on, ok := enabled.Literal.(bool)
	return ok && on && enabled.Expr == nil && enabled.Tmpl == nil && enabled.Resolver == nil
}

// materializeExtends returns a copy of cfg in which every extends: load save
// target carries the load block's provider and merged inputs: the load inputs,
// overridden key by key by the target's own. A target is left unresolved
// (Extends still set) only when cfg has no load block, and the manager refuses
// to write it. cfg itself is never modified. It is idempotent: a materialized
// target no longer has Extends set.
func materializeExtends(cfg *Config) *Config {
	if cfg == nil {
		return nil
	}
	out := *cfg
	if len(cfg.Save) == 0 {
		return &out
	}
	out.Save = make([]SaveTarget, len(cfg.Save))
	for i, target := range cfg.Save {
		if target.Extends == ExtendsLoad && cfg.Load != nil {
			merged := make(map[string]*spec.ValueRef, len(cfg.Load.Inputs)+len(target.Inputs))
			for k, v := range cfg.Load.Inputs {
				merged[k] = v
			}
			for k, v := range target.Inputs {
				// A dangling key (nil) never erases an inherited input
				// (lint reports it as nil-provider-input).
				if v != nil {
					merged[k] = v
				}
			}
			target.Provider = cfg.Load.Provider
			target.Inputs = merged
			target.Extends = ""
		}
		out.Save[i] = target
	}
	return &out
}
