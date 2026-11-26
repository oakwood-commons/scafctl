// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/mcp/initconfig"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

var initOutputFormats = []string{
	string(kvx.OutputFormatAuto),
	string(kvx.OutputFormatJSON),
	string(kvx.OutputFormatYAML),
	string(kvx.OutputFormatQuiet),
}

// InitOptions holds options for the `mcp init` command.
type InitOptions struct {
	Targets   []string
	Scope     string
	Print     bool
	SkillDir  string
	NoSkill   bool
	DryRun    bool
	Backup    bool
	CliParams *settings.Run
	IOStreams *terminal.IOStreams

	flags.KvxOutputFlags
}

func resolvedBinaryName(cliParams *settings.Run) string {
	if cliParams != nil && cliParams.BinaryName != "" {
		return cliParams.BinaryName
	}
	return settings.CliBinaryName
}

// CommandInit creates the `scafctl mcp init` command.
func CommandInit(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	opts := &InitOptions{
		CliParams: cliParams,
		IOStreams: ioStreams,
	}
	binName := resolvedBinaryName(cliParams)
	skillID := initconfig.SkillName(binName)

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Configure AI harnesses with the MCP server and authoring skill",
		Long: fmt.Sprintf(heredoc.Doc(`
			Idempotently register '%[1]s mcp serve' and install an authoring SKILL.md
			into supported AI coding harnesses.

			Supported targets (--target / -t, repeatable, or "all"):
			  - vscode   (.vscode/mcp.json + .github/skills/%[2]s/SKILL.md)
			  - claude   (.mcp.json + .claude/skills/%[2]s/SKILL.md)
			  - cursor   (.cursor/mcp.json + .cursor/skills/%[2]s/SKILL.md)
			  - opencode (opencode.jsonc + .opencode/skills/%[2]s/SKILL.md)
			  - agy      (.agents/mcp_config.json + .agents/skills/%[2]s/SKILL.md)

			When --target and --skill-dir are omitted, %[1]s auto-detects active
			harnesses in the working directory and defaults to vscode if none are found.
			Existing JSON/JSONC comments, key order, and neighbor MCP servers are preserved.
		`), binName, skillID),
		Example: fmt.Sprintf(heredoc.Doc(`
			# Auto-detect workspace AI harnesses and configure MCP + SKILL.md
			%[1]s mcp init

			# Configure specific harnesses (repeatable or "all")
			%[1]s mcp init --target vscode --target claude
			%[1]s mcp init --target all

			# Configure user-level profile instead of the current project
			%[1]s mcp init --target cursor --scope user

			# Preview planned file changes without writing to disk
			%[1]s mcp init --dry-run

			# Print generic MCP JSON snippet and SKILL.md to stdout for other editors
			%[1]s mcp init --print

			# Install SKILL.md into a custom skills directory only
			%[1]s mcp init --skill-dir .windsurf/skills
		`), binName),
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		PreRunE: func(_ *cobra.Command, _ []string) error {
			if _, err := initconfig.ParseScope(opts.Scope); err != nil {
				return err
			}
			if _, err := initconfig.ParseTargets(opts.Targets); err != nil {
				return err
			}
			if opts.Print && opts.SkillDir != "" {
				return fmt.Errorf("--skill-dir cannot be used with --print (which does not write files)")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			if opts.CliParams != nil {
				opts.CliParams.EntryPointSettings.Path = filepath.Join(path, cmd.Name())
			}
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			if opts.CliParams != nil {
				_, hasSettings := settings.FromContext(ctx)
				if opts.CliParams.BinaryName != "" || !hasSettings {
					ctx = settings.IntoContext(ctx, opts.CliParams)
				}
			}
			return runInit(ctx, opts)
		},
	}

	cmd.Flags().StringSliceVarP(&opts.Targets, "target", "t", nil, "Target AI harness: vscode, claude, cursor, opencode, agy, or all (repeatable; auto-detected when omitted)")
	cmd.Flags().StringVar(&opts.Scope, "scope", string(initconfig.DefaultScope), "Configuration scope: project or user")
	cmd.Flags().BoolVar(&opts.Print, "print", false, "Print MCP server JSON config and SKILL.md to stdout without writing files")
	cmd.Flags().StringVar(&opts.SkillDir, "skill-dir", "", fmt.Sprintf("Write <dir>/%s/SKILL.md to a custom skills directory", skillID))
	cmd.Flags().BoolVar(&opts.NoSkill, "no-skill", false, "Configure MCP server only without writing or printing SKILL.md")
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "Preview planned file changes without modifying the filesystem")
	cmd.Flags().BoolVar(&opts.Backup, "backup", false, "Create a .bak copy before updating existing files")
	cmd.MarkFlagsMutuallyExclusive("no-skill", "skill-dir")

	flags.AddKvxOutputFormatFlagToStructWithFormats(cmd, &opts.KvxOutputFlags, initOutputFormats)

	return cmd
}

func runInit(ctx context.Context, opts *InitOptions) error {
	w := writer.FromContext(ctx)
	if w == nil {
		w = writer.New(opts.IOStreams, opts.CliParams)
		ctx = writer.WithWriter(ctx, w)
	}
	binName := resolvedBinaryName(opts.CliParams)
	if opts.CliParams == nil || opts.CliParams.BinaryName == "" {
		binName = settings.BinaryNameFromContext(ctx)
	}
	ctx = settings.IntoContext(ctx, &settings.Run{
		BinaryName: binName,
		NoColor:    opts.CliParams != nil && opts.CliParams.NoColor,
		IsQuiet:    opts.CliParams != nil && opts.CliParams.IsQuiet,
		Verbose:    opts.CliParams != nil && opts.CliParams.Verbose,
	})

	var env initconfig.Env
	if cwd, ok := provider.WorkingDirectoryFromContext(ctx); ok && cwd != "" {
		env.WorkDir = cwd
	}

	domainOpts := initconfig.Options{
		Targets:  opts.Targets,
		Scope:    opts.Scope,
		SkillDir: opts.SkillDir,
		NoSkill:  opts.NoSkill,
		DryRun:   opts.DryRun,
		Backup:   opts.Backup,
		Env:      env,
	}

	kvxOpts := flags.ToKvxOutputOptions(&opts.KvxOutputFlags,
		kvx.WithOutputContext(ctx),
		kvx.WithOutputNoColor(opts.CliParams != nil && opts.CliParams.NoColor),
		kvx.WithOutputAppName(binName+" mcp init"),
		kvx.WithIOStreams(opts.IOStreams),
	)

	if opts.Print {
		printOut, err := initconfig.RenderPrint(ctx, domainOpts)
		if err != nil {
			w.Errorf("%v", err)
			return exitcode.WithCode(err, exitcode.InvalidInput)
		}
		switch kvxOpts.Format { //nolint:exhaustive // initOutputFormats restricts allowed formats at PreRunE
		case kvx.OutputFormatJSON, kvx.OutputFormatYAML:
			return kvxOpts.Write(printOut)
		case kvx.OutputFormatQuiet:
			return nil
		default:
			w.Plain(printOut.Text())
			return nil
		}
	}

	res, err := initconfig.Run(ctx, domainOpts)
	if err != nil {
		w.Errorf("%v", err)
		if errors.Is(err, initconfig.ErrInvalidConfig) {
			return exitcode.WithCode(err, exitcode.ConfigError)
		}
		return exitcode.WithCode(err, exitcode.GeneralError)
	}

	switch kvxOpts.Format { //nolint:exhaustive // initOutputFormats restricts allowed formats at PreRunE
	case kvx.OutputFormatJSON, kvx.OutputFormatYAML:
		return kvxOpts.Write(res)
	case kvx.OutputFormatQuiet:
		return nil
	default:
		renderHumanResult(w, res)
		return nil
	}
}

func renderHumanResult(w *writer.Writer, res *initconfig.Result) {
	for _, f := range res.Files {
		prefix := ""
		if res.DryRun {
			prefix = "[dry-run] "
		}
		switch f.Status {
		case initconfig.StatusCreated, initconfig.StatusUpdated:
			msg := fmt.Sprintf("%s%s %s (%s)", prefix, f.Status, f.Path, f.Kind)
			if f.BackupPath != "" {
				msg += fmt.Sprintf(" [backup: %s]", f.BackupPath)
			}
			w.Success(msg)
		default:
			w.Infof("%s%s %s (%s)", prefix, f.Status, f.Path, f.Kind)
		}
	}
}
