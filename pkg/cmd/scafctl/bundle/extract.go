// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package bundle

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/MakeNowJust/heredoc/v2"
	actionpkg "github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
	"github.com/oakwood-commons/scafctl/pkg/spec"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// ExtractOptions holds options for the bundle extract command.
type ExtractOptions struct {
	ArtifactRef string
	OutputDir   string
	Resolvers   []string
	Actions     []string
	Include     []string
	ListOnly    bool
	Flatten     bool
	CliParams   *settings.Run
	IOStreams   *terminal.IOStreams
}

// CommandExtract creates the bundle extract command.
func CommandExtract(cliParams *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
	opts := &ExtractOptions{
		CliParams: cliParams,
		IOStreams: ioStreams,
	}

	cmd := &cobra.Command{
		Use:          "extract <artifact-ref>",
		Short:        "Extract files from a bundled solution artifact",
		SilenceUsage: true,
		Long: heredoc.Doc(`
			Extract files from a bundled solution artifact, optionally filtering
			by resolver, action, or glob patterns.

			Without filters, extracts all bundled files. With --resolver or
			--action, performs static analysis to determine which files are
			referenced by the specified component(s).

			Examples:
			  # Extract all files to current directory
			  scafctl bundle extract my-solution@1.0.0

			  # Extract to a specific directory
			  scafctl bundle extract my-solution@1.0.0 --output-dir ./extracted

			  # Extract only files needed by a resolver
			  scafctl bundle extract my-solution@1.0.0 --resolver mainTfTemplate

			  # List files without extracting
			  scafctl bundle extract my-solution@1.0.0 --list-only

			  # Extract files matching a glob pattern
			  scafctl bundle extract my-solution@1.0.0 --include "templates/*.tmpl"

			  # Flatten directory structure
			  scafctl bundle extract my-solution@1.0.0 --flatten
		`),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.ArtifactRef = args[0]
			return runExtract(cmd.Context(), opts)
		},
	}

	cmd.Flags().StringVar(&opts.OutputDir, "output-dir", ".", "Directory to extract files into")
	cmd.Flags().StringSliceVar(&opts.Resolvers, "resolver", nil, "Extract only files needed by this resolver (repeatable)")
	cmd.Flags().StringSliceVar(&opts.Actions, "action", nil, "Extract only files needed by this action (repeatable)")
	cmd.Flags().StringSliceVar(&opts.Include, "include", nil, "Additional glob patterns to extract (repeatable)")
	cmd.Flags().BoolVar(&opts.ListOnly, "list-only", false, "List files that would be extracted without extracting")
	cmd.Flags().BoolVar(&opts.Flatten, "flatten", false, "Extract all files to a flat directory (no subdirectories)")

	return cmd
}

func runExtract(ctx context.Context, opts *ExtractOptions) error {
	lgr := logger.FromContext(ctx)
	w := writer.FromContext(ctx)

	localCatalog, err := catalog.NewLocalCatalog(*lgr)
	if err != nil {
		w.Errorf("failed to open catalog: %v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	ref, err := catalog.ParseReference(catalog.ArtifactKindSolution, opts.ArtifactRef)
	if err != nil {
		w.Errorf("invalid artifact reference: %v", err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	content, bundleData, _, err := localCatalog.FetchWithBundle(ctx, ref)
	if err != nil {
		w.Errorf("failed to fetch artifact: %v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	var sol solution.Solution
	if err := sol.LoadFromBytes(content); err != nil {
		w.Errorf("failed to parse solution: %v", err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	if len(bundleData) == 0 {
		w.Warningf("artifact has no bundle layer")
		return nil
	}

	// Extract to temp dir first
	tmpDir, err := os.MkdirTemp("", "scafctl-extract-*")
	if err != nil {
		return fmt.Errorf("creating temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	manifest, err := bundler.ExtractBundleTar(bundleData, tmpDir)
	if err != nil {
		w.Errorf("failed to extract bundle: %v", err)
		return exitcode.WithCode(err, exitcode.GeneralError)
	}

	// Determine which files to extract
	var filesToExtract []bundler.BundleFileEntry
	hasFilter := len(opts.Resolvers) > 0 || len(opts.Actions) > 0 || len(opts.Include) > 0

	if !hasFilter {
		// Extract all files
		filesToExtract = manifest.Files
	} else {
		// Build filtered file set
		needed := make(map[string]bool)

		// Trace resolver file dependencies
		for _, resolverName := range opts.Resolvers {
			_, exists := sol.Spec.Resolvers[resolverName]
			if !exists {
				w.Warningf("resolver %q not found in solution", resolverName)
				continue
			}
			traced := traceResolverDeps(resolverName, &sol, make(map[string]bool))
			for _, f := range traced {
				needed[f] = true
			}
		}

		// Trace action file dependencies
		if sol.Spec.Workflow != nil {
			for _, actionName := range opts.Actions {
				a, exists := sol.Spec.Workflow.Actions[actionName]
				if !exists {
					if a2, exists2 := sol.Spec.Workflow.Finally[actionName]; exists2 {
						a = a2
					} else {
						w.Warningf("action %q not found in solution", actionName)
						continue
					}
				}
				traced := extractActionFiles(a)
				for _, f := range traced {
					needed[f] = true
				}
			}
		}

		// Add include globs
		for _, pattern := range opts.Include {
			for _, entry := range manifest.Files {
				if matchGlob(pattern, entry.Path) {
					needed[entry.Path] = true
				}
			}
		}

		// Filter manifest files
		for _, entry := range manifest.Files {
			if needed[entry.Path] {
				filesToExtract = append(filesToExtract, entry)
			}
		}
	}

	if opts.ListOnly {
		printFileList(w, filesToExtract, opts.Resolvers, opts.Actions)
		return nil
	}

	// Create output dir
	outDir := opts.OutputDir
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}

	// Copy files from temp extraction to output
	var totalSize int64
	for _, entry := range filesToExtract {
		srcPath := filepath.Join(tmpDir, entry.Path)

		var destPath string
		if opts.Flatten {
			destPath = filepath.Join(outDir, filepath.Base(entry.Path))
		} else {
			destPath = filepath.Join(outDir, entry.Path)
		}

		if err := copyFile(srcPath, destPath); err != nil {
			w.Warningf("failed to extract %s: %v", entry.Path, err)
			continue
		}
		totalSize += entry.Size
	}

	w.Successf("Extracted %d file(s) (%s) to %s", len(filesToExtract), formatSize(totalSize), outDir)
	return nil
}

// traceResolverDeps performs static analysis on a resolver to find referenced files,
// following rslvr: bindings transitively.
func traceResolverDeps(name string, sol *solution.Solution, visited map[string]bool) []string {
	if visited[name] {
		return nil
	}
	visited[name] = true

	r, exists := sol.Spec.Resolvers[name]
	if !exists || r == nil {
		return nil
	}

	var files []string

	// Check resolve.with
	if r.Resolve != nil {
		for _, ps := range r.Resolve.With {
			files = append(files, extractProviderStepFiles(ps.Provider, ps.Inputs)...)
			// Follow rslvr: bindings in inputs
			for _, vr := range ps.Inputs {
				if vr != nil && vr.Resolver != nil {
					depName := extractResolverName(*vr.Resolver)
					files = append(files, traceResolverDeps(depName, sol, visited)...)
				}
			}
		}
	}

	// Check transform.with
	if r.Transform != nil {
		for _, pt := range r.Transform.With {
			files = append(files, extractProviderStepFiles(pt.Provider, pt.Inputs)...)
			for _, vr := range pt.Inputs {
				if vr != nil && vr.Resolver != nil {
					depName := extractResolverName(*vr.Resolver)
					files = append(files, traceResolverDeps(depName, sol, visited)...)
				}
			}
		}
	}

	// Check validate.with
	if r.Validate != nil {
		for _, pv := range r.Validate.With {
			files = append(files, extractProviderStepFiles(pv.Provider, pv.Inputs)...)
			for _, vr := range pv.Inputs {
				if vr != nil && vr.Resolver != nil {
					depName := extractResolverName(*vr.Resolver)
					files = append(files, traceResolverDeps(depName, sol, visited)...)
				}
			}
		}
	}

	return files
}

// extractActionFiles traces files referenced by an action.
func extractActionFiles(a *actionpkg.Action) []string {
	if a == nil {
		return nil
	}
	return extractProviderStepFiles(a.Provider, a.Inputs)
}

func extractProviderStepFiles(provider string, inputs map[string]*spec.ValueRef) []string {
	var files []string
	switch provider {
	case "file":
		if path := extractLiteralFromInputs(inputs, "path"); path != "" && isLocalFilePath(path) {
			files = append(files, path)
		}
	case "solution":
		if source := extractLiteralFromInputs(inputs, "source"); source != "" && isLocalFilePath(source) {
			files = append(files, source)
		}
	}
	return files
}

func extractLiteralFromInputs(inputs map[string]*spec.ValueRef, key string) string {
	if inputs == nil {
		return ""
	}
	vr := inputs[key]
	if vr == nil {
		return ""
	}
	if vr.Expr != nil || vr.Tmpl != nil || vr.Resolver != nil {
		return ""
	}
	s, ok := vr.Literal.(string)
	if !ok {
		return ""
	}
	return s
}

func isLocalFilePath(path string) bool {
	if path == "" {
		return false
	}
	if strings.Contains(path, "://") {
		return false
	}
	if strings.Contains(path, "@") {
		return false
	}
	if filepath.IsAbs(path) {
		return false
	}
	return true
}

func extractResolverName(name string) string {
	// Strip any selector, e.g. "myResolver.result" → "myResolver"
	if idx := strings.Index(name, "."); idx != -1 {
		return name[:idx]
	}
	return name
}

func printFileList(w *writer.Writer, files []bundler.BundleFileEntry, resolvers, actions []string) {
	if len(resolvers) > 0 {
		w.Infof("Files needed for resolver(s): %s", strings.Join(resolvers, ", "))
	}
	if len(actions) > 0 {
		w.Infof("Files needed for action(s): %s", strings.Join(actions, ", "))
	}

	var totalSize int64
	for _, f := range files {
		w.Plain(fmt.Sprintf("  %-40s (%s)", f.Path, formatSize(f.Size)))
		totalSize += f.Size
	}

	w.Plain("")
	w.Infof("Total: %d file(s), %s", len(files), formatSize(totalSize))
}

func copyFile(src, dst string) error {
	// Create parent directories
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	return err
}
