// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package refactor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	pkgrefactor "github.com/oakwood-commons/scafctl/pkg/refactor"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/get"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// renameResolverOptions holds flags for the rename resolver command.
type renameResolverOptions struct {
	File      string
	DryRun    bool
	CliParams *settings.Run
}

// CommandRenameResolver creates the `refactor rename resolver` command.
func CommandRenameResolver(cliParams *settings.Run, _ *terminal.IOStreams, path string) *cobra.Command {
	opts := &renameResolverOptions{CliParams: cliParams}

	binaryName := cliParams.BinaryName
	if binaryName == "" {
		binaryName = settings.CliBinaryName
	}

	cmd := &cobra.Command{
		Use:   "resolver <old-name> <new-name>",
		Short: "Rename a resolver and update every reference to it",
		Long: heredoc.Doc(`
			Rename a resolver and rewrite every reference to it -- dependsOn
			entries, rslvr values, CEL '_.name' uses, explicit template
			'._.name' uses, and the definition itself -- preserving comments,
			key order, and formatting.

			The rename refuses to run when any reference to the target resolver
			cannot be located byte-exact (for example a context-dependent bare
			'{{ .field }}' template reference, a '$'-rooted reference, or an
			inline reference nested inside a literal), so it never performs a
			partial rewrite that would silently break the solution.
		`),
		Example: strings.ReplaceAll(heredoc.Doc(`
			# Rename resolver 'environment' to 'env' in the discovered solution
			scafctl refactor rename resolver environment env

			# Target a specific file and preview the change without writing
			scafctl refactor rename resolver environment env -f ./solution.yaml --dry-run
		`), settings.CliBinaryName, binaryName),
		Args:         cobra.ExactArgs(2),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cliParams.EntryPointSettings.Path = filepath.Join(path, cmd.Name())
			ctx := settings.IntoContext(cmd.Context(), cliParams)
			ctx = logger.WithLogger(ctx, logger.FromContext(cmd.Context()))
			return runRenameResolver(ctx, opts, args[0], args[1])
		},
	}

	cmd.Flags().StringVarP(&opts.File, "file", "f", "", "Solution file path (auto-discovered if not provided)")
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "Show what would change without modifying the file")

	return cmd
}

// runRenameResolver resolves the target file, computes the rename edits via
// pkg/refactor, and either writes the result or previews it (--dry-run).
func runRenameResolver(ctx context.Context, opts *renameResolverOptions, oldName, newName string) error {
	w := writer.FromContext(ctx)

	getter := get.NewGetterFromContext(ctx)
	// Rename mutates a file, so ambiguous auto-discovery must error (high risk)
	// rather than silently pick one of several matches.
	resolvedPath, err := get.Resolve(ctx, getter, opts.File, "", get.ResolveOptions{Risk: get.DiscoveryRiskHigh})
	if err != nil {
		return fail(w, exitcode.FileNotFound, fmt.Errorf("failed to resolve solution: %w", err))
	}
	if resolvedPath == "-" {
		return fail(w, exitcode.ValidationFailed, errors.New("refactor rename requires a file path; stdin ('-') is not supported"))
	}

	raw, err := os.ReadFile(resolvedPath) //nolint:gosec // path resolved from user-provided solution reference
	if err != nil {
		return fail(w, exitcode.FileNotFound, fmt.Errorf("failed to read %s: %w", resolvedPath, err))
	}

	sol := &solution.Solution{}
	if err := sol.UnmarshalFromBytes(raw); err != nil {
		return fail(w, exitcode.FileNotFound, fmt.Errorf("failed to parse %s: %w", resolvedPath, err))
	}

	result, err := pkgrefactor.RenameResolver(sol, oldName, newName)
	if err != nil {
		return fail(w, exitcode.ValidationFailed, err)
	}

	newContent, err := result.Apply(raw)
	if err != nil {
		return fail(w, exitcode.GeneralError, fmt.Errorf("failed to apply rename: %w", err))
	}

	if opts.DryRun {
		printRenamePreview(w, resolvedPath, result)
		return nil
	}

	mode := os.FileMode(0o644)
	if fi, statErr := os.Stat(resolvedPath); statErr == nil {
		mode = fi.Mode().Perm()
	}
	if err := os.WriteFile(resolvedPath, newContent, mode); err != nil { //nolint:gosec // path resolved from user-provided solution reference
		return fail(w, exitcode.GeneralError, fmt.Errorf("failed to write %s: %w", resolvedPath, err))
	}

	if w != nil {
		w.Successf("Renamed resolver %q to %q (%d occurrence(s)) in %s", oldName, newName, len(result.Edits), resolvedPath)
	}
	return nil
}

// fail prints err to stderr (when a writer is present) and returns it wrapped
// with the given exit code. The root command silences returned errors, so
// commands must surface their own messages.
func fail(w *writer.Writer, code int, err error) error {
	if w != nil {
		w.Error(err.Error())
	}
	return exitcode.WithCode(err, code)
}

// printRenamePreview lists the planned edits without modifying the file.
func printRenamePreview(w *writer.Writer, path string, res *pkgrefactor.RenameResult) {
	if w == nil {
		return
	}
	w.Plainlnf("Would rename resolver %q to %q (%d occurrence(s)):", res.OldName, res.NewName, len(res.Edits))
	for _, e := range res.Edits {
		w.Plainlnf("  %s:%d:%d  %s -> %s", path, e.Range.Start.Line, e.Range.Start.Column, res.OldName, res.NewName)
	}
}
