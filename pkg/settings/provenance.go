// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package settings

import "context"

// RuntimeProvenance is the primitive, dependency-free description of who
// performed a run: the execution engine (the scafctl library) and the invoking
// CLI/frontend. It is the single source of truth for provenance fallback
// semantics -- the state and resolver-snapshot packages adapt it into their own
// struct families rather than reimplementing the "default CLI to engine" logic.
//
// When the CLI fields are empty they mirror the engine fields, which is the
// case for direct (non-embedded) scafctl use.
type RuntimeProvenance struct {
	// EngineName is the execution engine name (typically CliBinaryName).
	EngineName string
	// EngineVersion is the engine build version.
	EngineVersion string
	// CLIName is the invoking CLI/frontend binary name. Empty mirrors EngineName.
	CLIName string
	// CLIVersion is the invoking CLI/frontend version. Empty mirrors EngineVersion.
	CLIVersion string
}

// ResolvedCLIName returns the effective CLI name, falling back to the engine
// name when no distinct invoker is set.
func (p RuntimeProvenance) ResolvedCLIName() string {
	if p.CLIName != "" {
		return p.CLIName
	}
	return p.EngineName
}

// ResolvedCLIVersion returns the effective CLI version, falling back to the
// engine version when no distinct invoker version is set.
func (p RuntimeProvenance) ResolvedCLIVersion() string {
	if p.CLIVersion != "" {
		return p.CLIVersion
	}
	return p.EngineVersion
}

// RuntimeProvenanceFromContext builds provenance from the ambient CLI settings.
// The engine identity is always scafctl at its build version. The CLI identity
// is the configured binary name and (when embedded) the embedder version;
// otherwise it defaults to the engine via the Resolved* accessors.
//
// engineVersion is passed explicitly so callers that already hold a build
// version string (e.g. snapshot capture) stay authoritative; when empty it
// falls back to VersionInformation.BuildVersion.
func RuntimeProvenanceFromContext(ctx context.Context, engineVersion string) RuntimeProvenance {
	if engineVersion == "" {
		engineVersion = VersionInformation.BuildVersion
	}
	p := RuntimeProvenance{
		EngineName:    CliBinaryName,
		EngineVersion: engineVersion,
	}
	if s, ok := FromContext(ctx); ok {
		if s.BinaryName != "" {
			p.CLIName = s.BinaryName
		}
		p.CLIVersion = s.EmbedderVersion
	}
	return p
}
