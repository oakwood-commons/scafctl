// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package bundle

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// DiffOptions holds options for the bundle diff command.
type DiffOptions struct {
	RefA         string
	RefB         string
	FilesOnly    bool
	SolutionOnly bool
	Ignore       []string
	CliParams    *settings.Run
	IOStreams    *terminal.IOStreams

	flags.KvxOutputFlags
}

// CommandDiff creates the bundle diff command.
func CommandDiff(cliParams *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
	opts := &DiffOptions{
		CliParams: cliParams,
		IOStreams: ioStreams,
	}

	cmd := &cobra.Command{
		Use:          "diff <ref-a> <ref-b>",
		Short:        "Show changes between two bundled solution versions",
		SilenceUsage: true,
		Long: heredoc.Doc(`
			Show what changed between two versions of a bundled artifact,
			enabling informed upgrade decisions and change auditing.

			Compares:
			  - Solution YAML (resolvers added/removed/modified, actions changed)
			  - Bundled files (files added/removed/modified)
			  - Vendored dependencies (added/removed/upgraded)
			  - Plugin dependencies (version or defaults changed)

			Examples:
			  # Compare two versions
			  scafctl bundle diff my-solution@1.0.0 my-solution@2.0.0

			  # Show only file changes
			  scafctl bundle diff my-solution@1.0.0 my-solution@2.0.0 --files-only

			  # Show only solution structure changes
			  scafctl bundle diff my-solution@1.0.0 my-solution@2.0.0 --solution-only

			  # JSON output for scripting
			  scafctl bundle diff my-solution@1.0.0 my-solution@2.0.0 -o json
		`),
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.RefA = args[0]
			opts.RefB = args[1]
			return runDiff(cmd.Context(), opts)
		},
	}

	cmd.Flags().BoolVar(&opts.FilesOnly, "files-only", false, "Show only file changes, skip solution YAML diff")
	cmd.Flags().BoolVar(&opts.SolutionOnly, "solution-only", false, "Show only solution YAML diff, skip file changes")
	cmd.Flags().StringSliceVar(&opts.Ignore, "ignore", nil, "Glob patterns to exclude from diff (repeatable)")
	flags.AddKvxOutputFlagsToStruct(cmd, &opts.KvxOutputFlags)

	return cmd
}

// DiffResult is the structured output of a bundle diff.
type DiffResult struct {
	RefA     string        `json:"refA" yaml:"refA"`
	RefB     string        `json:"refB" yaml:"refB"`
	Solution *SolutionDiff `json:"solution,omitempty" yaml:"solution,omitempty"`
	Files    *FilesDiff    `json:"files,omitempty" yaml:"files,omitempty"`
	Vendored *VendoredDiff `json:"vendoredDependencies,omitempty" yaml:"vendoredDependencies,omitempty"`
	Plugins  *PluginsDiff  `json:"plugins,omitempty" yaml:"plugins,omitempty"`
}

// SolutionDiff holds changes to the solution structure.
type SolutionDiff struct {
	Resolvers DiffSets `json:"resolvers,omitempty" yaml:"resolvers,omitempty"`
	Actions   DiffSets `json:"actions,omitempty" yaml:"actions,omitempty"`
}

// DiffSets contains added/removed/modified name lists.
type DiffSets struct {
	Added    []string `json:"added,omitempty" yaml:"added,omitempty"`
	Removed  []string `json:"removed,omitempty" yaml:"removed,omitempty"`
	Modified []string `json:"modified,omitempty" yaml:"modified,omitempty"`
}

// FilesDiff holds changes to bundled files.
type FilesDiff struct {
	Added    []FileDiffEntry `json:"added,omitempty" yaml:"added,omitempty"`
	Removed  []FileDiffEntry `json:"removed,omitempty" yaml:"removed,omitempty"`
	Modified []FileDiffEntry `json:"modified,omitempty" yaml:"modified,omitempty"`
}

// FileDiffEntry describes a changed file.
type FileDiffEntry struct {
	Path string `json:"path" yaml:"path"`
	Size int64  `json:"size,omitempty" yaml:"size,omitempty"`
}

// VendoredDiff lists vendored dependency changes.
type VendoredDiff struct {
	Added    []VendoredEntry   `json:"added,omitempty" yaml:"added,omitempty"`
	Removed  []VendoredEntry   `json:"removed,omitempty" yaml:"removed,omitempty"`
	Upgraded []VendoredUpgrade `json:"upgraded,omitempty" yaml:"upgraded,omitempty"`
}

// VendoredEntry describes a vendored dependency.
type VendoredEntry struct {
	Name    string `json:"name" yaml:"name"`
	Version string `json:"version,omitempty" yaml:"version,omitempty"`
}

// VendoredUpgrade describes a vendored dependency version change.
type VendoredUpgrade struct {
	Name string `json:"name" yaml:"name"`
	From string `json:"from" yaml:"from"`
	To   string `json:"to" yaml:"to"`
}

// PluginsDiff lists plugin dependency changes.
type PluginsDiff struct {
	Added    []PluginDiffEntry `json:"added,omitempty" yaml:"added,omitempty"`
	Removed  []PluginDiffEntry `json:"removed,omitempty" yaml:"removed,omitempty"`
	Modified []PluginDiffEntry `json:"modified,omitempty" yaml:"modified,omitempty"`
}

// PluginDiffEntry describes a plugin change.
type PluginDiffEntry struct {
	Name        string `json:"name" yaml:"name"`
	VersionFrom string `json:"versionFrom,omitempty" yaml:"versionFrom,omitempty"`
	VersionTo   string `json:"versionTo,omitempty" yaml:"versionTo,omitempty"`
}

func runDiff(ctx context.Context, opts *DiffOptions) error {
	lgr := logger.FromContext(ctx)
	w := writer.FromContext(ctx)

	localCatalog, err := catalog.NewLocalCatalog(*lgr)
	if err != nil {
		w.Errorf("failed to open catalog: %v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	// Fetch both artifacts
	solA, manifestA, err := fetchAndExtract(ctx, localCatalog, opts.RefA)
	if err != nil {
		w.Errorf("failed to fetch %s: %v", opts.RefA, err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}
	defer solA.cleanup()

	solB, manifestB, err := fetchAndExtract(ctx, localCatalog, opts.RefB)
	if err != nil {
		w.Errorf("failed to fetch %s: %v", opts.RefB, err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}
	defer solB.cleanup()

	result := &DiffResult{
		RefA: opts.RefA,
		RefB: opts.RefB,
	}

	// Solution YAML diff
	if !opts.FilesOnly {
		result.Solution = diffSolutions(solA.sol, solB.sol)
	}

	// Files diff
	if !opts.SolutionOnly {
		result.Files = diffFiles(manifestA, manifestB, opts.Ignore)
		result.Vendored = diffVendored(manifestA, manifestB)
	}

	// Plugin diff
	if !opts.FilesOnly {
		result.Plugins = diffPlugins(manifestA, manifestB)
	}

	// Output
	format := kvx.OutputFormat(opts.Output)
	if kvx.IsStructuredFormat(format) {
		out := flags.ToKvxOutputOptions(&opts.KvxOutputFlags,
			kvx.WithIOStreams(opts.IOStreams),
			kvx.WithOutputContext(ctx),
		)
		return out.Write(result)
	}

	// Text output
	w.Infof("Comparing %s → %s", opts.RefA, opts.RefB)

	if result.Solution != nil {
		printSolutionDiff(w, result.Solution)
	}
	if result.Files != nil {
		printFilesDiff(w, result.Files)
	}
	if result.Vendored != nil {
		printVendoredDiff(w, result.Vendored)
	}
	if result.Plugins != nil {
		printPluginsDiff(w, result.Plugins)
	}

	// Summary
	w.Plain("")
	changes := countChanges(result)
	w.Infof("Summary: %s", changes)

	return nil
}

type extractedArtifact struct {
	sol    *solution.Solution
	tmpDir string
}

func (e *extractedArtifact) cleanup() {
	if e.tmpDir != "" {
		os.RemoveAll(e.tmpDir)
	}
}

func fetchAndExtract(ctx context.Context, cat *catalog.LocalCatalog, refStr string) (*extractedArtifact, *bundler.BundleManifest, error) {
	ref, err := catalog.ParseReference(catalog.ArtifactKindSolution, refStr)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid reference %q: %w", refStr, err)
	}

	content, bundleData, _, err := cat.FetchWithBundle(ctx, ref)
	if err != nil {
		return nil, nil, err
	}

	var sol solution.Solution
	if err := sol.LoadFromBytes(content); err != nil {
		return nil, nil, fmt.Errorf("failed to parse solution: %w", err)
	}

	result := &extractedArtifact{sol: &sol}
	var manifest *bundler.BundleManifest

	if len(bundleData) > 0 {
		tmpDir, err := os.MkdirTemp("", "scafctl-diff-*")
		if err != nil {
			return nil, nil, err
		}
		result.tmpDir = tmpDir

		manifest, err = bundler.ExtractBundleTar(bundleData, tmpDir)
		if err != nil {
			os.RemoveAll(tmpDir)
			return nil, nil, err
		}
	}

	if manifest == nil {
		manifest = &bundler.BundleManifest{Version: 1, Root: "."}
	}

	return result, manifest, nil
}

func diffSolutions(a, b *solution.Solution) *SolutionDiff {
	diff := &SolutionDiff{}

	// Compare resolvers
	resolversA := mapKeys(a.Spec.Resolvers)
	resolversB := mapKeys(b.Spec.Resolvers)
	diff.Resolvers = computeDiffSets(resolversA, resolversB)

	// Compare actions
	var actionsA, actionsB map[string]bool
	if a.Spec.Workflow != nil {
		actionsA = make(map[string]bool)
		for name := range a.Spec.Workflow.Actions {
			actionsA[name] = true
		}
	}
	if b.Spec.Workflow != nil {
		actionsB = make(map[string]bool)
		for name := range b.Spec.Workflow.Actions {
			actionsB[name] = true
		}
	}
	diff.Actions = computeDiffSetsFromBool(actionsA, actionsB)

	if len(diff.Resolvers.Added) == 0 && len(diff.Resolvers.Removed) == 0 &&
		len(diff.Resolvers.Modified) == 0 && len(diff.Actions.Added) == 0 &&
		len(diff.Actions.Removed) == 0 && len(diff.Actions.Modified) == 0 {
		return nil
	}

	return diff
}

func diffFiles(a, b *bundler.BundleManifest, ignore []string) *FilesDiff {
	diff := &FilesDiff{}

	filesA := make(map[string]bundler.BundleFileEntry)
	for _, f := range a.Files {
		filesA[f.Path] = f
	}
	filesB := make(map[string]bundler.BundleFileEntry)
	for _, f := range b.Files {
		filesB[f.Path] = f
	}

	// Ignore vendor and manifest paths for files diff
	isInternal := func(path string) bool {
		return strings.HasPrefix(path, ".scafctl/")
	}

	for path, fb := range filesB {
		if isInternal(path) {
			continue
		}
		if shouldIgnore(path, ignore) {
			continue
		}
		if _, exists := filesA[path]; !exists {
			diff.Added = append(diff.Added, FileDiffEntry{Path: path, Size: fb.Size})
		}
	}

	for path := range filesA {
		if isInternal(path) {
			continue
		}
		if shouldIgnore(path, ignore) {
			continue
		}
		if _, exists := filesB[path]; !exists {
			diff.Removed = append(diff.Removed, FileDiffEntry{Path: path})
		}
	}

	for path, fb := range filesB {
		if isInternal(path) {
			continue
		}
		if shouldIgnore(path, ignore) {
			continue
		}
		fa, exists := filesA[path]
		if exists && fa.Digest != fb.Digest {
			diff.Modified = append(diff.Modified, FileDiffEntry{Path: path, Size: fb.Size})
		}
	}

	sort.Slice(diff.Added, func(i, j int) bool { return diff.Added[i].Path < diff.Added[j].Path })
	sort.Slice(diff.Removed, func(i, j int) bool { return diff.Removed[i].Path < diff.Removed[j].Path })
	sort.Slice(diff.Modified, func(i, j int) bool { return diff.Modified[i].Path < diff.Modified[j].Path })

	if len(diff.Added) == 0 && len(diff.Removed) == 0 && len(diff.Modified) == 0 {
		return nil
	}
	return diff
}

func diffVendored(a, b *bundler.BundleManifest) *VendoredDiff {
	diff := &VendoredDiff{}

	vendorA := make(map[string]bundler.BundleFileEntry)
	vendorB := make(map[string]bundler.BundleFileEntry)

	for _, f := range a.Files {
		if strings.HasPrefix(f.Path, ".scafctl/vendor/") {
			name := strings.TrimPrefix(f.Path, ".scafctl/vendor/")
			name = strings.TrimSuffix(name, ".yaml")
			vendorA[name] = f
		}
	}
	for _, f := range b.Files {
		if strings.HasPrefix(f.Path, ".scafctl/vendor/") {
			name := strings.TrimPrefix(f.Path, ".scafctl/vendor/")
			name = strings.TrimSuffix(name, ".yaml")
			vendorB[name] = f
		}
	}

	for name := range vendorB {
		if _, exists := vendorA[name]; !exists {
			n, v := splitNameVersion(name)
			diff.Added = append(diff.Added, VendoredEntry{Name: n, Version: v})
		}
	}

	for name := range vendorA {
		if _, exists := vendorB[name]; !exists {
			n, v := splitNameVersion(name)
			diff.Removed = append(diff.Removed, VendoredEntry{Name: n, Version: v})
		}
	}

	// Check for version upgrades by comparing digests
	for name, fb := range vendorB {
		fa, exists := vendorA[name]
		if exists && fa.Digest != fb.Digest {
			n, v := splitNameVersion(name)
			_, vOld := splitNameVersion(name)
			diff.Upgraded = append(diff.Upgraded, VendoredUpgrade{Name: n, From: vOld, To: v})
		}
	}

	if len(diff.Added) == 0 && len(diff.Removed) == 0 && len(diff.Upgraded) == 0 {
		return nil
	}
	return diff
}

func diffPlugins(a, b *bundler.BundleManifest) *PluginsDiff {
	diff := &PluginsDiff{}

	pluginsA := make(map[string]bundler.BundlePluginEntry)
	for _, p := range a.Plugins {
		pluginsA[p.Name] = p
	}
	pluginsB := make(map[string]bundler.BundlePluginEntry)
	for _, p := range b.Plugins {
		pluginsB[p.Name] = p
	}

	for _, pb := range b.Plugins {
		if _, exists := pluginsA[pb.Name]; !exists {
			diff.Added = append(diff.Added, PluginDiffEntry{Name: pb.Name, VersionTo: pb.Version})
		}
	}

	for _, pa := range a.Plugins {
		if _, exists := pluginsB[pa.Name]; !exists {
			diff.Removed = append(diff.Removed, PluginDiffEntry{Name: pa.Name, VersionFrom: pa.Version})
		}
	}

	for _, pb := range b.Plugins {
		pa, exists := pluginsA[pb.Name]
		if exists && (pa.Version != pb.Version || pa.Kind != pb.Kind) {
			diff.Modified = append(diff.Modified, PluginDiffEntry{
				Name:        pb.Name,
				VersionFrom: pa.Version,
				VersionTo:   pb.Version,
			})
		}
	}

	if len(diff.Added) == 0 && len(diff.Removed) == 0 && len(diff.Modified) == 0 {
		return nil
	}
	return diff
}

// Helper functions

func mapKeys[V any](m map[string]V) map[string]bool {
	result := make(map[string]bool, len(m))
	for k := range m {
		result[k] = true
	}
	return result
}

func computeDiffSets(a, b map[string]bool) DiffSets {
	return computeDiffSetsFromBool(a, b)
}

func computeDiffSetsFromBool(a, b map[string]bool) DiffSets {
	var ds DiffSets
	for name := range b {
		if !a[name] {
			ds.Added = append(ds.Added, name)
		}
	}
	for name := range a {
		if !b[name] {
			ds.Removed = append(ds.Removed, name)
		}
	}
	// Items present in both are potentially modified (structural compare would be needed)
	for name := range b {
		if a[name] {
			ds.Modified = append(ds.Modified, name)
		}
	}
	sort.Strings(ds.Added)
	sort.Strings(ds.Removed)
	sort.Strings(ds.Modified)
	return ds
}

func splitNameVersion(s string) (string, string) {
	if idx := strings.LastIndex(s, "@"); idx != -1 {
		return s[:idx], s[idx+1:]
	}
	return s, ""
}

func shouldIgnore(path string, patterns []string) bool {
	for _, p := range patterns {
		if matchGlob(p, path) {
			return true
		}
	}
	return false
}

func printSolutionDiff(w *writer.Writer, diff *SolutionDiff) {
	w.Plain("")
	w.Plain("Solution YAML:")
	printDiffSets(w, "  resolvers:", diff.Resolvers)
	printDiffSets(w, "  workflow.actions:", diff.Actions)
}

func printDiffSets(w *writer.Writer, label string, ds DiffSets) {
	if len(ds.Added) == 0 && len(ds.Removed) == 0 {
		return
	}
	w.Plain(label)
	for _, name := range ds.Added {
		w.Successf("    + %s (added)", name)
	}
	for _, name := range ds.Modified {
		w.Infof("    ~ %s (present in both)", name)
	}
	for _, name := range ds.Removed {
		w.Errorf("    - %s (removed)", name)
	}
}

func printFilesDiff(w *writer.Writer, diff *FilesDiff) {
	if diff == nil {
		return
	}
	w.Plain("")
	w.Plain("Bundled files:")
	for _, f := range diff.Added {
		w.Successf("    + %s (added, %s)", f.Path, formatSize(f.Size))
	}
	for _, f := range diff.Modified {
		w.Infof("    ~ %s (modified)", f.Path)
	}
	for _, f := range diff.Removed {
		w.Errorf("    - %s (removed)", f.Path)
	}
}

func printVendoredDiff(w *writer.Writer, diff *VendoredDiff) {
	if diff == nil {
		return
	}
	w.Plain("")
	w.Plain("Vendored dependencies:")
	for _, v := range diff.Added {
		w.Successf("    + %s@%s (added)", v.Name, v.Version)
	}
	for _, v := range diff.Upgraded {
		w.Infof("    ~ %s: %s → %s (upgraded)", v.Name, v.From, v.To)
	}
	for _, v := range diff.Removed {
		w.Errorf("    - %s@%s (removed)", v.Name, v.Version)
	}
}

func printPluginsDiff(w *writer.Writer, diff *PluginsDiff) {
	if diff == nil {
		return
	}
	w.Plain("")
	w.Plain("Plugins:")
	for _, p := range diff.Added {
		w.Successf("    + %s %s (added)", p.Name, p.VersionTo)
	}
	for _, p := range diff.Modified {
		w.Infof("    ~ %s: %s → %s (constraint changed)", p.Name, p.VersionFrom, p.VersionTo)
	}
	for _, p := range diff.Removed {
		w.Errorf("    - %s %s (removed)", p.Name, p.VersionFrom)
	}
}

func countChanges(result *DiffResult) string {
	var parts []string
	if result.Solution != nil {
		resolverChanges := len(result.Solution.Resolvers.Added) + len(result.Solution.Resolvers.Removed)
		if resolverChanges > 0 {
			parts = append(parts, fmt.Sprintf("%d resolver(s) changed", resolverChanges))
		}
		actionChanges := len(result.Solution.Actions.Added) + len(result.Solution.Actions.Removed)
		if actionChanges > 0 {
			parts = append(parts, fmt.Sprintf("%d action(s) changed", actionChanges))
		}
	}
	if result.Files != nil {
		fileChanges := len(result.Files.Added) + len(result.Files.Removed) + len(result.Files.Modified)
		if fileChanges > 0 {
			parts = append(parts, fmt.Sprintf("%d file(s) changed", fileChanges))
		}
	}
	if result.Vendored != nil {
		vendorChanges := len(result.Vendored.Added) + len(result.Vendored.Removed) + len(result.Vendored.Upgraded)
		if vendorChanges > 0 {
			parts = append(parts, fmt.Sprintf("%d dependency(ies) changed", vendorChanges))
		}
	}
	if result.Plugins != nil {
		pluginChanges := len(result.Plugins.Added) + len(result.Plugins.Removed) + len(result.Plugins.Modified)
		if pluginChanges > 0 {
			parts = append(parts, fmt.Sprintf("%d plugin(s) changed", pluginChanges))
		}
	}
	if len(parts) == 0 {
		return "no changes"
	}
	return strings.Join(parts, ", ")
}

func formatSize(b int64) string {
	switch {
	case b >= 1024*1024:
		return fmt.Sprintf("%.1f MB", float64(b)/(1024*1024))
	case b >= 1024:
		return fmt.Sprintf("%.1f KB", float64(b)/1024)
	default:
		return fmt.Sprintf("%d B", b)
	}
}
