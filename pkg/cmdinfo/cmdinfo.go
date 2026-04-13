// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package cmdinfo collects structured information about CLI commands for
// kvx-powered discovery and exploration.
package cmdinfo

import (
	"sort"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// CommandInfo holds structured metadata about a single CLI command.
type CommandInfo struct {
	Name        string   `json:"name" yaml:"name" doc:"Full command path" maxLength:"256" example:"run solution"`
	Short       string   `json:"short" yaml:"short" doc:"Short description" maxLength:"512" example:"Run a solution workflow"`
	Group       string   `json:"group" yaml:"group" doc:"Command group" maxLength:"64" example:"core"`
	Aliases     []string `json:"aliases,omitempty" yaml:"aliases,omitempty" doc:"Command aliases" maxItems:"10"`
	Deprecated  bool     `json:"deprecated,omitempty" yaml:"deprecated,omitempty" doc:"Whether the command is deprecated"`
	HasChildren bool     `json:"hasChildren,omitempty" yaml:"hasChildren,omitempty" doc:"Whether the command has subcommands"`
	FlagCount   int      `json:"flagCount" yaml:"flagCount" doc:"Number of flags" minimum:"0" maximum:"1000" example:"8"`
}

// CollectCommands walks a Cobra command tree and returns structured info about
// all available commands. If leafOnly is true, only commands without children
// are returned (the actionable commands).
func CollectCommands(root *cobra.Command, leafOnly bool) []CommandInfo {
	var commands []CommandInfo
	walkCommands(root, "", "", leafOnly, &commands)
	sort.Slice(commands, func(i, j int) bool {
		return commands[i].Name < commands[j].Name
	})
	return commands
}

func walkCommands(cmd *cobra.Command, parentPath, parentGroup string, leafOnly bool, out *[]CommandInfo) {
	// Skip hidden commands
	if cmd.Hidden {
		return
	}

	fullPath := cmd.Name()
	if parentPath != "" {
		fullPath = parentPath + " " + cmd.Name()
	}

	// Inherit group from parent if not set directly
	group := cmd.GroupID
	if group == "" {
		group = parentGroup
	}

	children := cmd.Commands()
	hasChildren := len(children) > 0

	if !leafOnly || !hasChildren {
		info := CommandInfo{
			Name:        fullPath,
			Short:       cmd.Short,
			Group:       group,
			HasChildren: hasChildren,
			FlagCount:   countFlags(cmd),
		}
		if len(cmd.Aliases) > 0 {
			info.Aliases = cmd.Aliases
		}
		if cmd.Deprecated != "" {
			info.Deprecated = true
		}
		*out = append(*out, info)
	}

	for _, child := range children {
		walkCommands(child, fullPath, group, leafOnly, out)
	}
}

// countFlags counts the number of flags visible on a command.
// This uses Flags().VisitAll which avoids triggering mergePersistentFlags
// (can panic on shorthand conflicts). The count covers only flags that
// have already been added to the command's FlagSet; persistent flags
// from parent commands are included only if Cobra has already merged them.
func countFlags(cmd *cobra.Command) int {
	n := 0
	cmd.Flags().VisitAll(func(_ *pflag.Flag) { n++ })
	return n
}
