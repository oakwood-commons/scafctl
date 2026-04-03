// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package inspect

import (
	"fmt"
	"sort"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/solution"
)

// RunCommandInfo holds the structured result of analyzing a solution to
// determine how to run it. This is the domain representation used by
// CLI, MCP, and future API consumers.
type RunCommandInfo struct {
	// Command is the full CLI command string with all flags.
	Command string `json:"command" yaml:"command" doc:"Full CLI command to run the solution" maxLength:"2048" example:"scafctl run solution -f ./my-solution.yaml -r name=hello"`

	// Subcommand is the base command (e.g., "scafctl run solution").
	Subcommand string `json:"subcommand" yaml:"subcommand" doc:"Base CLI subcommand" maxLength:"128" example:"scafctl run solution"`

	// Explanation describes why this command variant was chosen.
	Explanation string `json:"explanation" yaml:"explanation" doc:"Why this command variant was chosen" maxLength:"512" example:"Solution has a workflow with actions"`

	// Parameters lists the parameter-type resolvers that need values.
	Parameters []ParamInfo `json:"parameters" yaml:"parameters" doc:"Parameter-type resolvers requiring values" maxItems:"100"`

	// HasWorkflow indicates whether the solution has a workflow.
	HasWorkflow bool `json:"hasWorkflow" yaml:"hasWorkflow" doc:"Whether the solution has a workflow"`

	// HasResolvers indicates whether the solution has resolvers.
	HasResolvers bool `json:"hasResolvers" yaml:"hasResolvers" doc:"Whether the solution has resolvers"`
}

// ParamInfo describes a parameter-type resolver that requires a user-provided value.
type ParamInfo struct {
	Name        string `json:"name" yaml:"name" doc:"Parameter name" maxLength:"256" example:"projectName"`
	Type        string `json:"type,omitempty" yaml:"type,omitempty" doc:"Parameter type" maxLength:"64" example:"string"`
	Description string `json:"description,omitempty" yaml:"description,omitempty" doc:"Parameter description" maxLength:"512" example:"Name of the project to create"`
	Example     any    `json:"example,omitempty" yaml:"example,omitempty" doc:"Example value"`
}

// BuildRunCommand analyzes a solution and returns the exact CLI command to run it,
// including any parameter-type resolvers that need values passed via -r flags.
// Returns nil and an error message if the solution has nothing to run.
func BuildRunCommand(sol *solution.Solution, path string) (*RunCommandInfo, error) {
	hasResolvers := sol.Spec.HasResolvers()
	hasWorkflow := sol.Spec.HasWorkflow()

	var command, explanation string
	switch {
	case hasWorkflow:
		command = "scafctl run solution"
		explanation = "Solution has a workflow with actions — use 'run solution'"
	case hasResolvers:
		command = "scafctl run resolver"
		explanation = "Solution has resolvers but no workflow — use 'run resolver'"
	default:
		return nil, fmt.Errorf("solution has neither resolvers nor a workflow")
	}

	// Find parameter-type resolvers
	var parameters []ParamInfo
	if hasResolvers {
		names := make([]string, 0, len(sol.Spec.Resolvers))
		for name := range sol.Spec.Resolvers {
			names = append(names, name)
		}
		sort.Strings(names)

		for _, name := range names {
			rslvr := sol.Spec.Resolvers[name]
			if rslvr.Resolve == nil || len(rslvr.Resolve.With) == 0 {
				continue
			}
			if rslvr.Resolve.With[0].Provider == "parameter" {
				parameters = append(parameters, ParamInfo{
					Name:        name,
					Type:        string(rslvr.Type),
					Description: rslvr.Description,
					Example:     rslvr.Example,
				})
			}
		}
	}

	// Build the full command string.
	// Ensure relative paths have "./" prefix so VS Code chat does not
	// auto-linkify bare filenames into content-reference URLs.
	cmdPath := path
	if !strings.HasPrefix(cmdPath, "/") && !strings.HasPrefix(cmdPath, "./") && !strings.HasPrefix(cmdPath, "../") && !strings.Contains(cmdPath, "://") {
		cmdPath = "./" + cmdPath
	}
	fullCommand := fmt.Sprintf("%s -f %s", command, cmdPath)
	for _, p := range parameters {
		exampleVal := "<value>"
		if p.Example != nil {
			exampleVal = fmt.Sprintf("%v", p.Example)
		}
		if hasWorkflow {
			// run solution uses -r flags
			fullCommand += fmt.Sprintf(" -r %s=%s", p.Name, exampleVal)
		} else {
			// run resolver uses positional key=value
			fullCommand += fmt.Sprintf(" %s=%s", p.Name, exampleVal)
		}
	}

	return &RunCommandInfo{
		Command:      fullCommand,
		Subcommand:   command,
		Explanation:  explanation,
		Parameters:   parameters,
		HasWorkflow:  hasWorkflow,
		HasResolvers: hasResolvers,
	}, nil
}
