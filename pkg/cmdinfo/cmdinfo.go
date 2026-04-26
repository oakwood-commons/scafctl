// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package cmdinfo collects structured information about CLI commands for
// kvx-powered discovery and exploration.
package cmdinfo

import (
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// CommandInfo holds structured metadata about a single CLI command.
type CommandInfo struct {
	Name        string   `json:"name" yaml:"name" doc:"Full command path including root" maxLength:"256" example:"mycli run solution"`
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

// FlagInfo holds structured metadata about a single CLI flag.
type FlagInfo struct {
	Name        string `json:"name"                  yaml:"name"                  doc:"Flag name (without --)"`
	Shorthand   string `json:"shorthand,omitempty"   yaml:"shorthand,omitempty"   doc:"Single-letter shorthand"`
	Type        string `json:"type"                  yaml:"type"                  doc:"Value type" example:"string"`
	Default     string `json:"default,omitempty"      yaml:"default,omitempty"      doc:"Default value"`
	Description string `json:"description"           yaml:"description"           doc:"Flag description"`
	Required    bool   `json:"required,omitempty"    yaml:"required,omitempty"    doc:"Whether the flag is required"`
}

// CommandDetail holds full structured help for a single CLI command,
// including flags, usage, and examples.
type CommandDetail struct {
	Name        string     `json:"name"                    yaml:"name"                    doc:"Full command path"`
	Short       string     `json:"short"                   yaml:"short"                   doc:"Short description"`
	Long        string     `json:"long,omitempty"          yaml:"long,omitempty"          doc:"Long description"`
	Usage       string     `json:"usage"                   yaml:"usage"                   doc:"Usage string"`
	Examples    string     `json:"examples,omitempty"      yaml:"examples,omitempty"      doc:"Example invocations"`
	Aliases     []string   `json:"aliases,omitempty"       yaml:"aliases,omitempty"       doc:"Command aliases"`
	Flags       []FlagInfo `json:"flags,omitempty"         yaml:"flags,omitempty"         doc:"Available flags"`
	Subcommands []string   `json:"subcommands,omitempty"   yaml:"subcommands,omitempty"   doc:"Child command names"`
	Deprecated  string     `json:"deprecated,omitempty"    yaml:"deprecated,omitempty"    doc:"Deprecation message"`
}

// FindCommand walks the command tree to find a command by its space-separated
// path (e.g. "run solution"). Returns nil if no matching command is found.
func FindCommand(root *cobra.Command, path string) *cobra.Command {
	if path == "" {
		return root
	}

	parts := strings.Fields(path)

	// If the first part matches the root command name, skip it.
	if len(parts) > 0 && parts[0] == root.Name() {
		parts = parts[1:]
	}

	cmd := root
	for _, part := range parts {
		found := false
		for _, child := range cmd.Commands() {
			if child.Name() == part || hasAlias(child, part) {
				cmd = child
				found = true
				break
			}
		}
		if !found {
			return nil
		}
	}
	return cmd
}

// GetCommandDetail returns structured help for a single command.
func GetCommandDetail(cmd *cobra.Command) CommandDetail {
	detail := CommandDetail{
		Name:  cmd.CommandPath(),
		Short: cmd.Short,
		Long:  cmd.Long,
		Usage: cmd.UseLine(),
	}

	if cmd.Example != "" {
		detail.Examples = cmd.Example
	}
	if len(cmd.Aliases) > 0 {
		detail.Aliases = cmd.Aliases
	}
	if cmd.Deprecated != "" {
		detail.Deprecated = cmd.Deprecated
	}

	// Collect flags.
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		if f.Hidden {
			return
		}
		fi := FlagInfo{
			Name:        f.Name,
			Shorthand:   f.Shorthand,
			Type:        f.Value.Type(),
			Default:     f.DefValue,
			Description: f.Usage,
		}
		if ann := f.Annotations; ann != nil {
			if _, ok := ann[cobra.BashCompOneRequiredFlag]; ok {
				fi.Required = true
			}
		}
		detail.Flags = append(detail.Flags, fi)
	})

	// Collect visible subcommands.
	for _, child := range cmd.Commands() {
		if !child.Hidden {
			detail.Subcommands = append(detail.Subcommands, child.Name())
		}
	}

	return detail
}

// hasAlias checks whether a command has the given alias.
func hasAlias(cmd *cobra.Command, alias string) bool {
	for _, a := range cmd.Aliases {
		if a == alias {
			return true
		}
	}
	return false
}
