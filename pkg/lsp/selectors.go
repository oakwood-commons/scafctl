// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lsp

import (
	"slices"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/settings"
)

// jsonFileExtension is the suffix that marks a recognized file as a JSON
// document (as opposed to YAML). It is used to partition recognized file names
// into their editor language groups.
const jsonFileExtension = ".json"

// RecognizedFiles describes the solution/action file names an LSP client should
// attach to, partitioned by document language so the client can emit correct
// per-language document selectors. It is the editor-facing projection of the
// CLI's discovery set (settings.SolutionFileNamesFor / ActionFileNamesFor),
// keeping editor integrations in lockstep with CLI auto-discovery instead of
// hardcoding a list that drifts.
type RecognizedFiles struct {
	// BinaryName is the effective CLI binary name these patterns are built for.
	// For embedders this is the embedding CLI's name (e.g. "mycli"), so the
	// editor attaches to "<mycli>.yaml" and friends automatically.
	BinaryName string `json:"binaryName" yaml:"binaryName" doc:"Effective CLI binary name these patterns are built for" example:"scafctl" maxLength:"128"`
	// YAMLNames are recognized solution/action file names whose editor language
	// is YAML (e.g. "solution.yaml", "taskfile.yml", "actions.yaml").
	YAMLNames []string `json:"yamlNames" yaml:"yamlNames" doc:"Recognized solution/action file names with a YAML language" maxItems:"64"`
	// JSONNames are recognized solution file names whose editor language is JSON
	// (e.g. "solution.json"). A JSON document never matches a YAML-scoped
	// selector, so these must be surfaced separately.
	JSONNames []string `json:"jsonNames" yaml:"jsonNames" doc:"Recognized solution file names with a JSON language" maxItems:"64"`
}

// RecognizedFilesFor returns the recognized solution/action file names for the
// given binary name, deduplicated and partitioned by extension. It unions the
// solution discovery set with the action discovery set so editors attach to
// every file the CLI would auto-discover (including .json solutions,
// taskfile.*, and actions.*). The binary name is sanitized via
// settings.SanitizeBinaryName, so a path, extension, or empty value is
// normalized (empty falls back to settings.CliBinaryName).
func RecognizedFilesFor(binaryName string) RecognizedFiles {
	// Normalize at the boundary: settings.SolutionFileNamesFor/ActionFileNamesFor
	// expect an already-sanitized name, so a caller passing a path
	// ("/opt/mycli"), an extension ("mycli.exe"), or an empty value would
	// otherwise produce an invalid editor contract. SanitizeBinaryName strips
	// directory/extension, replaces unsafe characters, and falls back to
	// CliBinaryName when the result is empty.
	binaryName = settings.SanitizeBinaryName(binaryName)

	// Union solution and action discovery sets; order is preserved by first
	// appearance so the output is deterministic and stable across releases.
	names := slices.Concat(
		settings.SolutionFileNamesFor(binaryName),
		settings.ActionFileNamesFor(binaryName),
	)

	rf := RecognizedFiles{BinaryName: binaryName}
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		if strings.HasSuffix(name, jsonFileExtension) {
			rf.JSONNames = append(rf.JSONNames, name)
		} else {
			rf.YAMLNames = append(rf.YAMLNames, name)
		}
	}
	return rf
}
