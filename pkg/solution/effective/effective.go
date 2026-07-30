// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package effective renders the effective (fully-composed, canonicalized)
// solution document as stable, deterministic JSON or YAML.
//
// The "effective" solution is the document that results after applying the
// solution's compose: merges. Unlike rendering the action graph, this transform
// performs NO resolver execution and NO provider calls -- it is a pure document
// projection suitable for golden-file fidelity diffing, code review, and
// debugging composition (mirroring `docker compose config`, `helm template`,
// and `kustomize build`).
//
// Determinism: both encoding/json and gopkg.in/yaml.v3 marshal Go maps in
// sorted-key order and structs in field-declaration order, so the output is
// byte-stable across runs for a given input. This is what makes the output
// safe to commit as a golden file and diff in CI.
package effective

import (
	"encoding/json"
	"fmt"

	"github.com/oakwood-commons/scafctl/pkg/solution"
	"gopkg.in/yaml.v3"
)

// Section scopes the effective output to a portion of the solution.
type Section string

const (
	// SectionAll emits the entire effective solution document.
	SectionAll Section = "all"
	// SectionWorkflow emits only spec.workflow (actions and finally).
	SectionWorkflow Section = "workflow"
	// SectionResolvers emits only spec.resolvers.
	SectionResolvers Section = "resolvers"
)

// Format selects the output serialization.
type Format string

const (
	// FormatYAML serializes the effective document as YAML.
	FormatYAML Format = "yaml"
	// FormatJSON serializes the effective document as JSON.
	FormatJSON Format = "json"
)

// ValidSections lists the accepted --section values for help and validation.
var ValidSections = []string{
	string(SectionAll),
	string(SectionWorkflow),
	string(SectionResolvers),
}

// Options configures how the effective solution is rendered.
type Options struct {
	// Section scopes the output. Defaults to SectionAll when empty.
	Section Section
	// Format selects the serialization. Defaults to FormatYAML when empty.
	Format Format
	// Compact disables pretty-printing for JSON output. Ignored for YAML.
	Compact bool
}

// Render serializes the effective (post-compose) solution into deterministic
// bytes. The caller is responsible for loading the solution with compose
// already applied (the standard getter does this on load).
//
// Render never executes resolvers or providers; it is a pure projection of the
// already-composed document.
func Render(sol *solution.Solution, opts Options) ([]byte, error) {
	if sol == nil {
		return nil, fmt.Errorf("solution is nil")
	}

	section := opts.Section
	if section == "" {
		section = SectionAll
	}

	value, err := project(sol, section)
	if err != nil {
		return nil, err
	}

	format := opts.Format
	if format == "" {
		format = FormatYAML
	}

	switch format {
	case FormatYAML:
		out, err := yaml.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("marshaling effective solution to YAML: %w", err)
		}
		return out, nil
	case FormatJSON:
		var out []byte
		if opts.Compact {
			out, err = json.Marshal(value)
		} else {
			out, err = json.MarshalIndent(value, "", "  ")
		}
		if err != nil {
			return nil, fmt.Errorf("marshaling effective solution to JSON: %w", err)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unsupported format %q (supported: %s, %s)", format, FormatYAML, FormatJSON)
	}
}

// project returns the value to serialize for the requested section.
func project(sol *solution.Solution, section Section) (any, error) {
	switch section {
	case SectionAll:
		return sol, nil
	case SectionWorkflow:
		if !sol.Spec.HasWorkflow() {
			return nil, fmt.Errorf("solution does not define a workflow")
		}
		return sol.Spec.Workflow, nil
	case SectionResolvers:
		if !sol.Spec.HasResolvers() {
			return nil, fmt.Errorf("solution does not define any resolvers")
		}
		return sol.Spec.Resolvers, nil
	default:
		return nil, fmt.Errorf("invalid section %q (valid: %s, %s, %s)", section, SectionAll, SectionWorkflow, SectionResolvers)
	}
}
