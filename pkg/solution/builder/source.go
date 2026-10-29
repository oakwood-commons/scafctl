// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package builder

import (
	"context"
	"fmt"
	"io"
	"path/filepath"

	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution/load"
	"github.com/oakwood-commons/scafctl/pkg/solution/source"
	"gopkg.in/yaml.v3"
)

// StdinPath is the solution path that reads the document from stdin.
const StdinPath = "-"

// SolutionSource is a solution loaded for packaging and the directory its
// relative paths resolve against.
type SolutionSource struct {
	*load.MaterializedSolution

	// BundleRoot is the absolute directory that compose files, the lock file,
	// bundle file discovery, and vendored dependencies resolve against:
	// the base dir when set, else the solution file's directory, else (stdin)
	// the context working directory.
	BundleRoot string

	noBundle bool

	// composeFiles are the compose: parts merged in at load time.
	composeFiles []string

	// identityStamped is set when Stamp changed metadata.name or
	// metadata.version, so the raw document no longer matches the artifact.
	identityStamped bool
}

// SourceOption configures LoadSolutionSource.
type SourceOption func(*sourceConfig)

type sourceConfig struct {
	noBundle bool
}

// WithNoBundle applies --no-bundle: the solution is loaded as written, with
// no compose merge and no lock file, and the bundle pipeline is skipped.
func WithNoBundle(noBundle bool) SourceOption {
	return func(c *sourceConfig) { c.noBundle = noBundle }
}

// IsStdin reports whether the solution was read from stdin. A stdin source
// has no file to derive an artifact name from and is never composed.
func (s *SolutionSource) IsStdin() bool {
	return s.Source.Kind == source.KindStdin
}

// Path returns the absolute path of the loaded solution file; empty for stdin.
func (s *SolutionSource) Path() string {
	return s.Solution.GetPath()
}

// ShouldBundle reports whether the bundle pipeline (compose, file discovery,
// vendoring, lock, build cache, archive) runs for this source. It is off
// under WithNoBundle, and always off for stdin, which has no directory to
// discover files in. stdinForced is true when stdin turned off a bundle the
// caller asked for, so the CLI can tell the user.
func (s *SolutionSource) ShouldBundle() (bundle, stdinForced bool) {
	if s.noBundle {
		return false, false
	}
	if s.IsStdin() {
		return false, true
	}
	return true, false
}

// ComposeFiles returns the compose: parts merged into the solution at load
// time; empty when nothing was composed.
func (s *SolutionSource) ComposeFiles() []string {
	return s.composeFiles
}

// Stamp writes the resolved identity and git metadata into the solution (see
// StampSolution) and records whether the identity changed, which decides
// what Document returns.
func (s *SolutionSource) Stamp(id *Identity, git GitMetadata, opts StampOptions) (StampResult, error) {
	res, err := StampSolution(s.Solution, id, git, opts)
	if err != nil {
		return res, err
	}
	s.identityStamped = s.identityStamped || res.IdentityChanged()
	return res, nil
}

// Document returns the solution document to store. It is the document as
// written, unless the stored solution differs from it: a stamped identity,
// or a bundled solution that was composed or declares bundle.include. In
// that case the solution is serialized.
func (s *SolutionSource) Document() ([]byte, error) {
	raw := s.Solution.RawContent()
	if raw != nil && !s.differsFromRaw() {
		return raw, nil
	}
	data, err := s.Solution.ToYAML()
	if err != nil {
		return nil, fmt.Errorf("serializing solution: %w", err)
	}
	return data, nil
}

// differsFromRaw reports whether the solution to store no longer matches
// the document as written.
//
// TODO: account for build annotations. This gap predates the builder package:
// it exists on main as of e0b20d17 (pkg/cmd/scafctl/packagecmd/solution.go).
// Stamp scrubs and re-sets dev.scafctl.build.commit/.dirty in memory, but
// when identity is unchanged the raw bytes are stored, so a build annotation
// already in the source (e.g. a re-packaged extracted document) is published
// even under --no-git-metadata and disagrees with the OCI annotations. Have
// Stamp report when it changed a build annotation that was present in the
// source, and serialize in that case.
func (s *SolutionSource) differsFromRaw() bool {
	if s.identityStamped {
		return true
	}
	bundle, _ := s.ShouldBundle()
	return bundle && (len(s.composeFiles) > 0 || len(s.Solution.Bundle.Include) > 0)
}

// LoadSolutionSource loads the solution to package with the same loaders as
// `run`, restricted to local sources:
//   - StdinPath reads stdin (load.ErrSourceNotEnabled when stdin is nil);
//   - an empty path auto-discovers the solution file from the context
//     working directory, failing with load.ErrAmbiguousSolution when more
//     than one candidate matches;
//   - any other path is a local file, resolved against the context working
//     directory (--cwd).
//
// File sources are composed and their lock file read relative to baseDir,
// which defaults to the solution file's directory. A relative baseDir is
// resolved against the context working directory. Under WithNoBundle the
// document is loaded as written (no compose, no lock), as --no-bundle
// stores it; stdin is never composed and has no lock.
func LoadSolutionSource(ctx context.Context, path, baseDir string, stdin io.Reader, opts ...SourceOption) (*SolutionSource, error) {
	var cfg sourceConfig
	for _, opt := range opts {
		opt(&cfg)
	}

	absBaseDir, err := resolveBaseDir(ctx, baseDir)
	if err != nil {
		return nil, err
	}

	fileOpts := []load.FileOption{load.WithBaseDir(absBaseDir)}
	if cfg.noBundle {
		fileOpts = append(fileOpts, load.WithParseOnly())
	}

	router := load.NewRouter(settings.BinaryNameFromContext(ctx),
		load.WithStdin(stdin),
		load.WithFileOptions(fileOpts...),
		load.WithDiscoverOptions(load.WithDiscoveryRisk(load.DiscoveryRiskHigh)),
	)

	ms, err := router.Load(ctx, classifyLocal(path))
	if err != nil {
		return nil, fmt.Errorf("loading solution: %w", err)
	}

	src := &SolutionSource{MaterializedSolution: ms, BundleRoot: absBaseDir, noBundle: cfg.noBundle}
	if src.BundleRoot == "" {
		if src.BundleRoot, err = defaultBundleRoot(ctx, src); err != nil {
			return nil, err
		}
	}
	if bundle, _ := src.ShouldBundle(); bundle {
		if src.composeFiles, err = declaredComposeFiles(ms.Solution.RawContent()); err != nil {
			return nil, err
		}
	}
	return src, nil
}

// declaredComposeFiles returns the compose: list of a solution document as
// written. The loader clears it from the parsed solution once composed.
func declaredComposeFiles(raw []byte) ([]string, error) {
	var doc struct {
		Compose []string `yaml:"compose"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("reading compose files: %w", err)
	}
	return doc.Compose, nil
}

// classifyLocal maps a package -f value to a local source kind. Unlike
// source.Classify it never yields a catalog or URL kind: package only builds
// from local sources, so an extension-less name is a file, not a catalog ref.
func classifyLocal(path string) source.Classification {
	switch path {
	case StdinPath:
		return source.Classification{Kind: source.KindStdin, Ref: path}
	case "":
		return source.Classification{Kind: source.KindAutoDiscover}
	default:
		return source.Classification{Kind: source.KindLocalFile, Ref: path}
	}
}

// resolveBaseDir returns baseDir as an absolute path, or "" when unset.
func resolveBaseDir(ctx context.Context, baseDir string) (string, error) {
	if baseDir == "" {
		return "", nil
	}
	abs, err := provider.AbsFromContext(ctx, baseDir)
	if err != nil {
		return "", fmt.Errorf("resolving base dir %q: %w", baseDir, err)
	}
	return abs, nil
}

// defaultBundleRoot is the bundle root when no base dir is given: the
// solution file's directory, or the context working directory for stdin.
func defaultBundleRoot(ctx context.Context, src *SolutionSource) (string, error) {
	if !src.IsStdin() {
		return filepath.Dir(src.Path()), nil
	}
	cwd, err := provider.GetWorkingDirectory(ctx)
	if err != nil {
		return "", fmt.Errorf("determining working directory: %w", err)
	}
	return cwd, nil
}
