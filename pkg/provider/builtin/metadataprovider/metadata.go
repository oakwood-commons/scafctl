// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package metadataprovider

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/schemahelper"
	"github.com/oakwood-commons/scafctl/pkg/settings"
)

// ProviderName is the name of this provider.
const ProviderName = "metadata"

// parentProcessNameFunc returns the parent process executable name.
// Points to the platform-specific parentProcessName by default.
// Overridable for testing.
var parentProcessNameFunc = parentProcessName

// goosFunc returns the current OS. Overridable for testing.
var goosFunc = func() string { return runtime.GOOS }

// MetadataProvider returns runtime metadata about the scafctl process and
// the currently-executing solution. It requires no inputs — all data is
// gathered from the execution context and process environment.
type MetadataProvider struct{}

// New creates a new metadata provider instance.
func New() *MetadataProvider {
	return &MetadataProvider{}
}

// Descriptor returns the provider's metadata and schema.
func (p *MetadataProvider) Descriptor() *provider.Descriptor {
	return &provider.Descriptor{
		Name:        ProviderName,
		DisplayName: "Metadata Provider",
		APIVersion:  "v1",
		Version:     semver.MustParse("3.1.0"),
		Description: "Returns runtime metadata about the scafctl process and the currently-executing solution. Provides the scafctl version, CLI arguments, working directory, entrypoint type (cli/api), command path, solution metadata, and platform information (os, arch), and the user's default shell. Requires no inputs.",
		Schema:      schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{}),
		OutputSchemas: map[provider.Capability]*jsonschema.Schema{
			provider.CapabilityFrom: schemahelper.ObjectSchema(
				[]string{"version", "args", "cwd", "entrypoint", "command", "solution", "os", "arch", "shell"},
				map[string]*jsonschema.Schema{
					"version": schemahelper.ObjectProp("Build version information", nil, map[string]*jsonschema.Schema{
						"buildVersion": schemahelper.StringProp("Semantic version of the scafctl build"),
						"commit":       schemahelper.StringProp("Git commit hash of the build"),
						"buildTime":    schemahelper.StringProp("Timestamp of the build"),
					}),
					"args":       schemahelper.ArrayProp("Command-line arguments passed to scafctl", schemahelper.WithItems(schemahelper.StringProp("A CLI argument"))),
					"cwd":        schemahelper.StringProp("Current working directory"),
					"entrypoint": schemahelper.StringProp("How scafctl was invoked", schemahelper.WithEnum("cli", "api", "unknown")),
					"command":    schemahelper.StringProp("The command path (e.g. scafctl/run/solution)"),
					"solution": schemahelper.ObjectProp("Metadata about the currently-running solution", nil, map[string]*jsonschema.Schema{
						"name":        schemahelper.StringProp("Solution name"),
						"version":     schemahelper.StringProp("Solution version"),
						"displayName": schemahelper.StringProp("Solution display name"),
						"description": schemahelper.StringProp("Solution description"),
						"category":    schemahelper.StringProp("Solution category"),
						"tags":        schemahelper.ArrayProp("Solution tags", schemahelper.WithItems(schemahelper.StringProp("A tag"))),
					}),
					"os":    schemahelper.StringProp("Operating system (runtime.GOOS)", schemahelper.WithEnum("aix", "android", "darwin", "dragonfly", "freebsd", "illumos", "ios", "js", "linux", "netbsd", "openbsd", "plan9", "solaris", "wasip1", "windows")),
					"arch":  schemahelper.StringProp("CPU architecture (runtime.GOARCH)", schemahelper.WithEnum("386", "amd64", "arm", "arm64", "loong64", "mips", "mips64", "mips64le", "mipsle", "ppc64", "ppc64le", "riscv64", "s390x", "wasm")),
					"shell": schemahelper.StringProp("User's shell (from $SHELL on Unix; PSModulePath/parent-process heuristic on Windows; %ComSpec% fallback)"),
				},
			),
		},
		Capabilities: []provider.Capability{
			provider.CapabilityFrom,
		},
		Category: "Core",
		Tags:     []string{"metadata", "solution", "introspection", "runtime", "platform"},
		Examples: []provider.Example{
			{
				Name:        "Runtime metadata",
				Description: "Return all runtime metadata about the scafctl process and current solution",
				YAML: `name: runtime-meta
type: metadata
from:
  inputs: {}`,
			},
		},
	}
}

// Execute gathers runtime metadata from the process environment and context.
func (p *MetadataProvider) Execute(ctx context.Context, _ any) (*provider.Output, error) {
	lgr := logger.FromContext(ctx)
	lgr.V(1).Info("executing provider", "provider", ProviderName)

	// Build version info from the global settings.
	versionInfo := settings.VersionInformation
	version := map[string]any{
		"buildVersion": versionInfo.BuildVersion,
		"commit":       versionInfo.Commit,
		"buildTime":    versionInfo.BuildTime,
	}

	// CLI arguments.
	args := os.Args

	// Current working directory (context-aware).
	cwd, _ := provider.GetWorkingDirectory(ctx)

	// Entrypoint and command path from settings context.
	entrypoint := "unknown"
	command := ""
	if run, ok := settings.FromContext(ctx); ok && run != nil {
		ep := run.EntryPointSettings
		switch {
		case ep.FromCli:
			entrypoint = "cli"
		case ep.FromAPI:
			entrypoint = "api"
		}
		command = ep.Path
	}

	// Solution metadata from provider context.
	var solData map[string]any
	if meta, ok := provider.SolutionMetadataFromContext(ctx); ok && meta != nil {
		solData = map[string]any{
			"name":        meta.Name,
			"version":     meta.Version,
			"displayName": meta.DisplayName,
			"description": meta.Description,
			"category":    meta.Category,
			"tags":        meta.Tags,
		}
	} else {
		solData = map[string]any{}
	}

	result := map[string]any{
		"version":    version,
		"args":       args,
		"cwd":        cwd,
		"entrypoint": entrypoint,
		"command":    command,
		"solution":   solData,
		"os":         runtime.GOOS,
		"arch":       runtime.GOARCH,
		"shell":      detectShell(),
	}

	lgr.V(1).Info("provider completed", "provider", ProviderName)
	return &provider.Output{Data: result}, nil
}

// detectShell returns the base name of the user's shell.
//
// On Unix, $SHELL is the canonical source (user's configured login shell).
//
// On Windows, %ComSpec% always points to cmd.exe regardless of the running
// shell, so we use a multi-step heuristic:
//  1. $SHELL set → authoritative on Unix; also covers Git Bash / MSYS2 / Cygwin on Windows.
//  2. PSModulePath set (Windows only) → PowerShell session. Inspect the parent
//     process name to distinguish "pwsh" (PowerShell 7+) from "powershell"
//     (Windows PowerShell 5.x). Falls back to "pwsh" if introspection fails.
//  3. %ComSpec% → last resort on Windows (almost always cmd.exe).
//
// Returns an empty string if nothing is detected.
func detectShell() string {
	// Unix fast path: $SHELL is authoritative.
	if shell := os.Getenv("SHELL"); shell != "" {
		return filepath.Base(shell)
	}

	// Windows heuristic: PSModulePath is set in every PowerShell session.
	if goosFunc() == "windows" {
		if os.Getenv("PSModulePath") != "" {
			return detectPowerShellVariant()
		}
	}

	// Fallback: %ComSpec% on Windows (cmd.exe), empty on Unix.
	if comspec := os.Getenv("ComSpec"); comspec != "" {
		return filepath.Base(comspec)
	}
	return ""
}

// detectPowerShellVariant inspects the parent process to distinguish
// "pwsh" (PowerShell 7+) from "powershell" (Windows PowerShell 5.x).
// Returns "pwsh" if the parent cannot be determined.
func detectPowerShellVariant() string {
	name := parentProcessNameFunc()
	if name == "" {
		return "pwsh"
	}

	base := filepath.Base(name)
	base = strings.TrimSuffix(base, ".exe")
	switch base {
	case "powershell":
		return "powershell"
	case "pwsh":
		return "pwsh"
	}

	// Default to modern PowerShell if parent is something unexpected.
	return "pwsh"
}
