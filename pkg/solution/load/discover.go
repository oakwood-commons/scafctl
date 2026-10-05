// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package load

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"gopkg.in/yaml.v3"

	"github.com/oakwood-commons/scafctl/pkg/fs"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/source"
	"github.com/oakwood-commons/scafctl/pkg/telemetry"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
)

var (
	// ErrNoSolutionFound is returned when auto-discovery finds no solution file.
	ErrNoSolutionFound = errors.New("no solution path provided and no solution file found in default locations")
	// ErrAmbiguousSolution is returned when auto-discovery finds several
	// solution files and the loader is configured with DiscoveryRiskHigh.
	ErrAmbiguousSolution = errors.New("multiple solution files found")
)

// actionFileNames are the default action file names, used to look for an
// actions file next to a discovered solution file in solution mode.
var actionFileNames = []string{"actions.yaml", "actions.yml"}

// DiscoveryRisk controls how DiscoverLoader handles multiple matches.
type DiscoveryRisk int

const (
	// DiscoveryRiskLow loads the first match. Suitable for run, lint, test.
	DiscoveryRiskLow DiscoveryRisk = iota
	// DiscoveryRiskHigh fails with ErrAmbiguousSolution when more than one
	// solution file is found. Suitable for build, push, refactor.
	DiscoveryRiskHigh
)

// Discovery describes a solution file found by auto-discovery.
type Discovery struct {
	Path          string `json:"path" yaml:"path" doc:"Absolute path of the discovered solution file" maxLength:"4096" example:"/work/solution.yaml"`
	IsActionFile  bool   `json:"isActionFile" yaml:"isActionFile" doc:"True when the discovered file is an action file"`
	AlternatePath string `json:"alternatePath,omitempty" yaml:"alternatePath,omitempty" doc:"A complementary solution or action file found alongside the discovered one" maxLength:"4096" example:"/work/actions.yaml"`
}

// DiscoverLoader finds a solution file in default locations and loads it via a
// FileLoader. Candidate paths are resolved against the context working
// directory (--cwd).
// Supported kinds: KindAutoDiscover.
type DiscoverLoader struct {
	files             *FileLoader
	statFunc          fs.StatFunc
	mode              settings.DiscoveryMode
	risk              DiscoveryRisk
	customActionFiles []string
	folders           []string
	fileNames         []string
}

var _ Loader = (*DiscoverLoader)(nil)

// DiscoverOption configures a DiscoverLoader.
type DiscoverOption func(*DiscoverLoader)

// WithStatFunc overrides the function used to check whether candidate files
// exist. Defaults to os.Stat.
func WithStatFunc(statFunc fs.StatFunc) DiscoverOption {
	return func(l *DiscoverLoader) {
		l.statFunc = statFunc
	}
}

// WithDiscoveryMode selects which file names are searched and how matches are
// ordered. Defaults to settings.DiscoveryModeDefault.
func WithDiscoveryMode(mode settings.DiscoveryMode) DiscoverOption {
	return func(l *DiscoverLoader) {
		l.mode = mode
	}
}

// WithCustomActionFiles replaces the default action file names in
// settings.DiscoveryModeAction. Matches are treated as action files.
func WithCustomActionFiles(names []string) DiscoverOption {
	return func(l *DiscoverLoader) {
		l.customActionFiles = names
	}
}

// WithDiscoveryRisk sets how multiple matches are handled. Defaults to
// DiscoveryRiskLow.
func WithDiscoveryRisk(risk DiscoveryRisk) DiscoverOption {
	return func(l *DiscoverLoader) {
		l.risk = risk
	}
}

// NewDiscoverLoader returns a DiscoverLoader that searches the default folders
// and file names for binaryName (see settings.SolutionFoldersFor and
// settings.FileNamesForMode) and loads the chosen match with files.
// A nil files uses NewFileLoader(); an empty binaryName uses
// settings.CliBinaryName.
func NewDiscoverLoader(files *FileLoader, binaryName string, opts ...DiscoverOption) *DiscoverLoader {
	if files == nil {
		files = NewFileLoader()
	}
	if binaryName == "" {
		binaryName = settings.CliBinaryName
	}
	l := &DiscoverLoader{
		files:    files,
		statFunc: os.Stat,
	}
	for _, opt := range opts {
		opt(l)
	}
	l.folders = settings.SolutionFoldersFor(binaryName)
	l.fileNames = settings.FileNamesForMode(l.mode, binaryName, l.customActionFiles)
	return l
}

// Discover returns every solution file found, in priority order: folder
// order first, then file name order. In settings.DiscoveryModeAction, action
// files are moved ahead of solution files. AlternatePath is set on the first
// result only. Taskfiles (taskfile.yaml/yml) that are not scafctl solutions,
// such as go-task files, are skipped. Returns ErrNoSolutionFound when nothing
// matches.
func (l *DiscoverLoader) Discover(ctx context.Context) ([]Discovery, error) {
	_, span := telemetry.Tracer(telemetry.TracerSolution).Start(
		ctx, "solution.load.Discover",
		trace.WithAttributes(attribute.String("solution.discovery.mode", l.mode.String())),
	)
	defer span.End()

	// Search from the logical working directory (--cwd), not the process
	// working directory. --cwd never calls os.Chdir; it only sets this context
	// value, and everything else that resolves paths (resolvers, __cwd, state)
	// reads from it. Using os.Stat on relative paths would
	// make `--cwd X` discover a solution from wherever the shell happens to be.
	// The returned paths are absolute so callers don't have to know which base
	// directory was used.
	base, err := provider.AbsFromContext(ctx, ".")
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("resolving discovery base directory: %w", err)
	}

	log := logger.FromContext(ctx).WithValues("base", base, "mode", l.mode.String())
	log.V(1).Info("searching for solution files", "folders", l.folders, "fileNames", l.fileNames)

	var found []Discovery
	seen := make(map[string]struct{})
	for _, folder := range l.folders {
		for _, name := range l.fileNames {
			path := filepath.Join(base, folder, name)
			if _, dup := seen[path]; dup || !l.isFile(path) {
				continue
			}
			seen[path] = struct{}{}
			if settings.IsTaskFile(name) && !l.mayBeSolution(path) {
				log.V(1).Info("skipping taskfile that is not a solution", "path", path)
				continue
			}
			found = append(found, Discovery{Path: path, IsActionFile: l.isActionFile(name)})
		}
	}

	if len(found) == 0 {
		err := fmt.Errorf("%w in %s (searched: %s)", ErrNoSolutionFound, base, strings.Join(l.candidates(), ", "))
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	if l.mode == settings.DiscoveryModeAction {
		slices.SortStableFunc(found, func(a, b Discovery) int {
			switch {
			case a.IsActionFile == b.IsActionFile:
				return 0
			case a.IsActionFile:
				return -1
			default:
				return 1
			}
		})
	}
	found[0].AlternatePath = l.alternatePath(found)

	span.SetAttributes(
		attribute.String("solution.path", found[0].Path),
		attribute.Int("solution.discovery.matches", len(found)),
	)
	log.V(1).Info("found solution files", "paths", discoveryPaths(found))
	return found, nil
}

// Load discovers the solution file and loads it with the FileLoader. With
// DiscoveryRiskHigh, more than one match fails with ErrAmbiguousSolution.
// The result's Source is the loaded local file; Discovery and OtherMatches
// describe what auto-discovery found.
func (l *DiscoverLoader) Load(ctx context.Context, c source.Classification) (*MaterializedSolution, error) {
	if c.Kind != source.KindAutoDiscover {
		return nil, fmt.Errorf("discover loader: %w: %s", ErrUnsupportedKind, c.Kind)
	}

	found, err := l.Discover(ctx)
	if err != nil {
		return nil, err
	}
	return l.loadFound(ctx, found)
}

// loadFound loads the first of found, a non-empty result of Discover.
func (l *DiscoverLoader) loadFound(ctx context.Context, found []Discovery) (*MaterializedSolution, error) {
	if l.risk == DiscoveryRiskHigh && len(found) > 1 {
		return nil, ambiguous(found)
	}

	m, err := l.files.Load(ctx, source.Classification{Kind: source.KindLocalFile, Ref: found[0].Path})
	if err != nil {
		return nil, err
	}
	m.Discovery = &found[0]
	if len(found) > 1 {
		m.OtherMatches = discoveryPaths(found[1:])
	}
	return m, nil
}

// DiscoverPath returns the path of the solution file auto-discovery finds
// from the context working directory (--cwd), for commands that need the file
// itself rather than the loaded solution. It searches the default folders and
// file names for the binary name in ctx. With DiscoveryRiskHigh, more than one
// match fails with ErrAmbiguousSolution; with DiscoveryRiskLow the first match
// wins. The choice is reported in verbose mode, as ReportDiscovery does.
// Returns an error wrapping ErrNoSolutionFound when nothing is found.
func DiscoverPath(ctx context.Context, risk DiscoveryRisk) (string, error) {
	found, err := NewDiscoverLoader(nil, settings.BinaryNameFromContext(ctx)).Discover(ctx)
	if err != nil {
		return "", err
	}
	if risk == DiscoveryRiskHigh && len(found) > 1 {
		return "", ambiguous(found)
	}
	reportDiscovery(ctx, found[0].Path, discoveryPaths(found[1:]))
	return found[0].Path, nil
}

// ReportDiscovery writes verbose auto-discovery details to the writer in ctx:
// the other matches that were not loaded, then the chosen path. No-op when
// the source was explicit or ctx has no writer.
func ReportDiscovery(ctx context.Context, ms *MaterializedSolution) {
	if ms == nil || ms.Discovery == nil {
		return
	}
	reportDiscovery(ctx, ms.Discovery.Path, ms.OtherMatches)
}

// reportDiscovery writes the verbose lines for the chosen path and the other
// matches. No-op when ctx has no writer.
func reportDiscovery(ctx context.Context, path string, others []string) {
	w := writer.FromContext(ctx)
	if w == nil {
		return
	}
	if len(others) > 0 {
		w.Verbosef("Multiple solution files found (also: %s); using first match", strings.Join(others, ", "))
	}
	w.Verbosef("Using %s", path)
}

// ReportSource writes the verbose "Loading solution from" line for c to the
// writer in ctx. Auto-discovered sources are skipped: ReportDiscovery reports
// the chosen path once loaded. No-op when ctx has no writer.
func ReportSource(ctx context.Context, c source.Classification) {
	w := writer.FromContext(ctx)
	if w == nil {
		return
	}
	switch c.Kind {
	case source.KindStdin:
		w.Verbose("Loading solution from stdin")
	case source.KindAutoDiscover:
	case source.KindUnknown, source.KindURL, source.KindRemoteRef, source.KindCatalogQualified,
		source.KindBareName, source.KindLocalFile:
		w.Verbosef("Loading solution from: %s", c.Ref)
	}
}

// ambiguous returns the ErrAmbiguousSolution error for several matches.
func ambiguous(found []Discovery) error {
	return fmt.Errorf("%w: %s; use -f/--file to specify which one",
		ErrAmbiguousSolution, strings.Join(discoveryPaths(found), ", "))
}

// isFile reports whether path exists and is not a directory.
func (l *DiscoverLoader) isFile(path string) bool {
	info, err := l.statFunc(path)
	return err == nil && info != nil && !info.IsDir()
}

// mayBeSolution reports whether the file at path may be a solution. It is
// false only when the file parses as YAML and has none of a solution's
// apiVersion, kind, or spec, so other tools' files that share a discovery name
// (go-task's taskfile.yaml) are skipped while a broken solution still loads
// and reports its real error.
func (l *DiscoverLoader) mayBeSolution(path string) bool {
	data, err := l.files.readFile(path)
	if err != nil {
		return true
	}
	var doc struct {
		APIVersion string `yaml:"apiVersion"`
		Kind       string `yaml:"kind"`
		Spec       any    `yaml:"spec"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return true
	}
	return doc.APIVersion == solution.DefaultAPIVersion || doc.Kind == solution.SolutionKind || doc.Spec != nil
}

// isActionFile reports whether name is a default or custom action file name.
func (l *DiscoverLoader) isActionFile(name string) bool {
	return settings.IsActionFile(name) || slices.Contains(l.customActionFiles, name)
}

// alternatePath returns a complementary file for the first match: in action
// mode, the highest-priority solution file; in solution mode, an actions file
// in the same directory.
func (l *DiscoverLoader) alternatePath(found []Discovery) string {
	switch l.mode {
	case settings.DiscoveryModeAction:
		if !found[0].IsActionFile {
			return ""
		}
		for _, d := range found[1:] {
			if !d.IsActionFile {
				return d.Path
			}
		}
	case settings.DiscoveryModeSolution:
		// TODO: make this symmetric with action mode. Action mode reports the
		// file `run solution` would pick (any folder), but this only checks the
		// solution's own folder, so e.g. scafctl/solution.yaml + ./actions.yaml
		// gives no hint even though `run action` would find ./actions.yaml.
		// It should report the first action file across all search folders.
		dir := filepath.Dir(found[0].Path)
		for _, name := range actionFileNames {
			if path := filepath.Join(dir, name); l.isFile(path) {
				return path
			}
		}
	case settings.DiscoveryModeDefault:
	}
	return ""
}

// candidates lists every relative path searched, for error messages.
func (l *DiscoverLoader) candidates() []string {
	out := make([]string, 0, len(l.folders)*len(l.fileNames))
	for _, folder := range l.folders {
		for _, name := range l.fileNames {
			out = append(out, filepath.Join(folder, name))
		}
	}
	return out
}

func discoveryPaths(found []Discovery) []string {
	paths := make([]string, len(found))
	for i, d := range found {
		paths[i] = d.Path
	}
	return paths
}
