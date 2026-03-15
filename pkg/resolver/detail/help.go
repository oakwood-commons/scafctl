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
	Name         string
	Type         string
	Description  string
	HasDefault   bool
	ParameterKey string // The parameter key if this resolver uses the "parameter" provider.
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
			for i, src := range r.Resolve.With {
				if src.Provider == "parameter" {
					// Extract the key input (literal string value)
					if keyRef, ok := src.Inputs["key"]; ok && keyRef != nil {
						if s, ok := keyRef.Literal.(string); ok {
							info.ParameterKey = s
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
		if info.ParameterKey != "" {
			paramResolvers = append(paramResolvers, info)
		} else {
			computedResolvers = append(computedResolvers, info)
		}
	}

	// Sort each group by name for deterministic output
	slices.SortFunc(paramResolvers, func(a, b ResolverInfo) int {
		return strings.Compare(a.ParameterKey, b.ParameterKey)
	})
	slices.SortFunc(computedResolvers, func(a, b ResolverInfo) int {
		return strings.Compare(a.Name, b.Name)
	})

	// Calculate column widths across all resolvers
	maxParamLen := len("PARAMETER")
	maxTypeLen := len("TYPE")
	maxNameLen := len("RESOLVER")

	for _, info := range infos {
		paramKey := info.ParameterKey
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
			maxParamLen, info.ParameterKey,
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
