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

// renameOptions holds flags for a `refactor rename <kind>` command.
type renameOptions struct {
	File      string
	DryRun    bool
	CliParams *settings.Run
}

// renameCommandConfig captures the kind-specific pieces of a `refactor rename
// <kind>` subcommand; the wiring is shared by newRenameCommand.
type renameCommandConfig struct {
	use     string // cobra Use line, e.g. "resolver <old-name> <new-name>"
	short   string
	long    string
	example string // uses settings.CliBinaryName as a placeholder for the binary name
	kind    string // symbol label used in output ("resolver"/"action")
	rename  renameFunc
}

// newRenameCommand builds a `refactor rename <kind>` subcommand. All rename
// subcommands share identical flag/context wiring and differ only by the
// kind-specific config, so the logic lives here once.
func newRenameCommand(cliParams *settings.Run, path string, cfg renameCommandConfig) *cobra.Command {
	opts := &renameOptions{CliParams: cliParams}

	binaryName := cliParams.BinaryName
	if binaryName == "" {
		binaryName = settings.CliBinaryName
	}

	cmd := &cobra.Command{
		Use:          cfg.use,
		Short:        cfg.short,
		Long:         cfg.long,
		Example:      strings.ReplaceAll(cfg.example, settings.CliBinaryName, binaryName),
		Args:         cobra.ExactArgs(2),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cliParams.EntryPointSettings.Path = filepath.Join(path, cmd.Name())
			ctx := settings.IntoContext(cmd.Context(), cliParams)
			ctx = logger.WithLogger(ctx, logger.FromContext(cmd.Context()))
			return runRename(ctx, opts, cfg.kind, cfg.rename, args[0], args[1])
		},
	}

	cmd.Flags().StringVarP(&opts.File, "file", "f", "", "Solution file path (auto-discovered if not provided)")
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "Show what would change without modifying the file")

	return cmd
}

// CommandRenameResolver creates the `refactor rename resolver` command.
func CommandRenameResolver(cliParams *settings.Run, _ *terminal.IOStreams, path string) *cobra.Command {
	return newRenameCommand(cliParams, path, renameCommandConfig{
		use:   "resolver <old-name> <new-name>",
		short: "Rename a resolver and update every reference to it",
		long: heredoc.Doc(`
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
		example: heredoc.Doc(`
			# Rename resolver 'environment' to 'env' in the discovered solution
			scafctl refactor rename resolver environment env

			# Target a specific file and preview the change without writing
			scafctl refactor rename resolver environment env -f ./solution.yaml --dry-run
		`),
		kind:   "resolver",
		rename: pkgrefactor.RenameResolver,
	})
}

// renameFunc computes rename edits for a symbol of a specific kind.
type renameFunc func(sol *solution.Solution, oldName, newName string) (*pkgrefactor.RenameResult, error)

// runRename resolves the target file, computes the rename edits via pkg/refactor,
// and either writes the result or previews it (--dry-run). kind labels the symbol
// ("resolver", "action") in output.
func runRename(ctx context.Context, opts *renameOptions, kind string, rename renameFunc, oldName, newName string) error {
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

	result, err := rename(sol, oldName, newName)
	if err != nil {
		return fail(w, exitcode.ValidationFailed, err)
	}

	newContent, err := result.Apply(raw)
	if err != nil {
		return fail(w, exitcode.GeneralError, fmt.Errorf("failed to apply rename: %w", err))
	}

	if opts.DryRun {
		printRenamePreview(w, resolvedPath, kind, result)
		return nil
	}

	mode := os.FileMode(0o644)
	if fi, statErr := os.Stat(resolvedPath); statErr == nil {
		mode = fi.Mode().Perm()
	}
	if err := writeFileAtomic(resolvedPath, newContent, mode); err != nil {
		return fail(w, exitcode.GeneralError, fmt.Errorf("failed to write %s: %w", resolvedPath, err))
	}

	if w != nil {
		w.Successf("Renamed %s %q to %q (%d occurrence(s)) in %s", kind, oldName, newName, len(result.Edits), resolvedPath)
	}
	return nil
}

// writeFileAtomic writes data to path atomically by writing to a temporary file
// in the same directory and renaming it over path. os.Rename is atomic on the
// same filesystem, so a crash mid-write cannot leave the solution file
// truncated or partially written.
func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*") //nolint:gosec // temp file created in the target's own directory
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	// Best-effort cleanup if we return before a successful rename; a no-op once
	// the temp file has been renamed away.
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, mode); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil { //nolint:gosec // path resolved from user-provided solution reference
		return err
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
func printRenamePreview(w *writer.Writer, path, kind string, res *pkgrefactor.RenameResult) {
	if w == nil {
		return
	}
	w.Plainlnf("Would rename %s %q to %q (%d occurrence(s)):", kind, res.OldName, res.NewName, len(res.Edits))
	for _, e := range res.Edits {
		w.Plainlnf("  %s:%d:%d  %s -> %s", path, e.Range.Start.Line, e.Range.Start.Column, res.OldName, res.NewName)
	}
}
