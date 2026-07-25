// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package detail

import (
	"fmt"
	"slices"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/solution"
)

// ResolverInfo describes a single resolver for help text rendering.
type ResolverInfo struct {
	Name        string
	Type        string
	Description string
	HasDefault  bool
	// ParameterKeys holds the CLI parameter names this resolver reads via the
	// "parameter" provider: a single "key", the alias list "keys", or the
	// distinct-key set of a "keys" + "as: map" read. Empty when the resolver
	// does not read named CLI parameters.
	ParameterKeys []string
	// AcceptsAllParameters is true when the resolver reads every supplied CLI
	// parameter via the parameter provider's "all: true" map mode.
	AcceptsAllParameters bool
}

// AcceptsParameters reports whether the resolver reads CLI parameters (named
// keys or the whole supplied set).
func (r ResolverInfo) AcceptsParameters() bool {
	return len(r.ParameterKeys) > 0 || r.AcceptsAllParameters
}

// ExtractResolverInfo builds a list of ResolverInfo from a solution's resolvers.
// It inspects each resolver's resolve phase to determine which ones accept
// CLI parameters (via the "parameter" provider) and whether they have fallback
// defaults (additional sources after the parameter provider).
func ExtractResolverInfo(sol *solution.Solution) []ResolverInfo {
	if sol == nil || sol.Spec.Resolvers == nil {
		return nil
	}

	resolvers := sol.Spec.ResolversToSlice()
	infos := make([]ResolverInfo, 0, len(resolvers))

	for _, r := range resolvers {
		info := ResolverInfo{
			Name:        r.Name,
			Type:        string(r.Type),
			Description: r.Description,
		}

		if r.Resolve != nil {
			// seen de-duplicates parameter key names across all parameter
			// sources of this resolver, mirroring the distinct, non-empty key
			// set the parameter provider actually reads (empty names are
			// ignored, duplicates collapsed).
			seen := make(map[string]struct{})
			addKey := func(name string) {
				if name == "" {
					return
				}
				if _, dup := seen[name]; dup {
					return
				}
				seen[name] = struct{}{}
				info.ParameterKeys = append(info.ParameterKeys, name)
			}
			for i, src := range r.Resolve.With {
				if src.Provider == "parameter" {
					// Single-key / alias "keys" reads, and the distinct-key set
					// of a "keys" + "as: map" read, all name CLI parameters.
					if keyRef, ok := src.Inputs["key"]; ok && keyRef != nil {
						if s, ok := keyRef.Literal.(string); ok {
							addKey(s)
						}
					}
					if keysRef, ok := src.Inputs["keys"]; ok && keysRef != nil {
						for _, s := range literalStringList(keysRef.Literal) {
							addKey(s)
						}
					}
					// "all: true" reads every supplied parameter.
					if allRef, ok := src.Inputs["all"]; ok && allRef != nil {
						if b, ok := allRef.Literal.(bool); ok && b {
							info.AcceptsAllParameters = true
						}
					}
					// If there are more sources after this one, the resolver
					// has a fallback default.
					if i < len(r.Resolve.With)-1 {
						info.HasDefault = true
					}
				}
			}
		}

		infos = append(infos, info)
	}

	return infos
}

// literalStringList normalizes a "keys" literal (a []string or []any of
// strings) into a []string, ignoring non-string entries.
func literalStringList(raw any) []string {
	switch v := raw.(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

// FormatResolverInputHelp generates a human-readable help section describing
// the resolvers in a solution. It shows resolver names, types, descriptions,
// and which resolvers accept CLI parameters.
//
// Example output:
//
//	Solution Resolvers (my-solution):
//	  PARAMETER     TYPE     RESOLVER     DESCRIPTION
//	  name          string   name         User name from CLI or default (has default)
//	  count         int      count        Repetition count (has default)
//	  -             string   greeting     Final greeting message (computed)
func FormatResolverInputHelp(sol *solution.Solution) string {
	infos := ExtractResolverInfo(sol)
	if len(infos) == 0 {
		return ""
	}

	var sb strings.Builder

	solutionName := sol.Metadata.Name
	if solutionName == "" {
		solutionName = "unknown"
	}

	fmt.Fprintf(&sb, "Solution Resolvers (%s):\n", solutionName)

	// Separate into parameter resolvers and computed resolvers
	var paramResolvers []ResolverInfo
	var computedResolvers []ResolverInfo
	for _, info := range infos {
		if info.AcceptsParameters() {
			paramResolvers = append(paramResolvers, info)
		} else {
			computedResolvers = append(computedResolvers, info)
		}
	}

	// Sort each group for deterministic output. Parameter resolvers sort by the
	// rendered PARAMETER cell, then by resolver name as a tiebreaker so that
	// resolvers producing the same cell (e.g. two "(all)" resolvers, or the same
	// key list) keep a stable order regardless of map-iteration order.
	slices.SortFunc(paramResolvers, func(a, b ResolverInfo) int {
		if c := strings.Compare(parameterCell(a), parameterCell(b)); c != 0 {
			return c
		}
		return strings.Compare(a.Name, b.Name)
	})
	slices.SortFunc(computedResolvers, func(a, b ResolverInfo) int {
		return strings.Compare(a.Name, b.Name)
	})

	// Calculate column widths across all resolvers
	maxParamLen := len("PARAMETER")
	maxTypeLen := len("TYPE")
	maxNameLen := len("RESOLVER")

	for _, info := range infos {
		paramKey := parameterCell(info)
		if paramKey == "" {
			paramKey = "-"
		}
		if len(paramKey) > maxParamLen {
			maxParamLen = len(paramKey)
		}
		typeStr := info.Type
		if typeStr == "" {
			typeStr = "any"
		}
		if len(typeStr) > maxTypeLen {
			maxTypeLen = len(typeStr)
		}
		if len(info.Name) > maxNameLen {
			maxNameLen = len(info.Name)
		}
	}

	// Print header
	header := fmt.Sprintf("  %-*s  %-*s  %-*s  %s",
		maxParamLen, "PARAMETER",
		maxTypeLen, "TYPE",
		maxNameLen, "RESOLVER",
		"DESCRIPTION")
	sb.WriteString(header + "\n")

	// Print parameter resolvers first (these accept CLI input)
	for _, info := range paramResolvers {
		typeStr := info.Type
		if typeStr == "" {
			typeStr = "any"
		}

		desc := info.Description
		if info.HasDefault {
			desc += " (has default)"
		}

		line := fmt.Sprintf("  %-*s  %-*s  %-*s  %s",
			maxParamLen, parameterCell(info),
			maxTypeLen, typeStr,
			maxNameLen, info.Name,
			desc)
		sb.WriteString(strings.TrimRight(line, " ") + "\n")
	}

	// Print computed resolvers (no CLI parameter)
	for _, info := range computedResolvers {
		typeStr := info.Type
		if typeStr == "" {
			typeStr = "any"
		}

		desc := info.Description
		if desc == "" {
			desc = "(computed)"
		} else {
			desc += " (computed)"
		}

		line := fmt.Sprintf("  %-*s  %-*s  %-*s  %s",
			maxParamLen, "-",
			maxTypeLen, typeStr,
			maxNameLen, info.Name,
			desc)
		sb.WriteString(strings.TrimRight(line, " ") + "\n")
	}

	return sb.String()
}

// parameterCell renders the PARAMETER column for a resolver: "(all)" when it
// reads every supplied parameter, the comma-joined key list when it reads
// named parameters, or "-" when it reads none.
func parameterCell(info ResolverInfo) string {
	if info.AcceptsAllParameters {
		return "(all)"
	}
	if len(info.ParameterKeys) > 0 {
		return strings.Join(info.ParameterKeys, ",")
	}
	return "-"
}
