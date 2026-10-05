// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package load

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
	"github.com/oakwood-commons/scafctl/pkg/solution/source"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
)

// ErrFetch wraps a failure to fetch a URL, registry, or catalog solution and
// write it to the local filesystem in Local. It covers both the load and the
// local write (temporary directory, bundle extraction, solution file).
var ErrFetch = errors.New("fetch solution")

const (
	// fetchedFileName is the file a fetched solution is written to.
	//
	// TODO: the bundle is extracted into the same directory first, so a
	// bundled top-level file with this name (e.g. a sub-solution next to a
	// differently named parent) is overwritten. Pre-existing behavior; fix by
	// writing to a non-colliding name or erroring on collision.
	fetchedFileName = "solution.yaml"
	// fetchedDirSuffix is appended to the binary name to form the
	// os.MkdirTemp pattern for a fetched solution.
	fetchedDirSuffix = "-solution-*"
)

// Selector chooses a solution source and returns the loader for it.
// *Router implements it.
type Selector interface {
	Select(ctx context.Context, explicit string, args []string, policy ArgPolicy, opts ...source.Option) (Selection, error)
	SelectionLoader(s Selection) Loader
}

var _ Selector = (*Router)(nil)

// LocalSolution is a solution file or directory on the local filesystem.
type LocalSolution struct {
	// Path is the solution file or directory.
	Path string
	// dir is the temporary directory holding a fetched solution; "" when
	// Path was already local.
	dir string
}

// Fetched reports whether Path is a temporary copy of a fetched solution.
func (l *LocalSolution) Fetched() bool {
	return l != nil && l.dir != ""
}

// Close removes the temporary copy of a fetched solution. It is a no-op for
// a solution that was already local.
func (l *LocalSolution) Close() error {
	if !l.Fetched() {
		return nil
	}
	return os.RemoveAll(l.dir)
}

// Local returns ref as a solution on the local filesystem, for commands that
// read solution files rather than a loaded solution:
//
//   - an existing file or directory is used, resolved against the context
//     working directory (see provider.AbsFromContext);
//   - "" auto-discovers a solution file; risk decides whether several matches
//     are an error (DiscoveryRiskHigh) or the first one wins;
//   - a URL, registry, or catalog reference is fetched through sel, and the
//     solution plus its extracted bundle are written to a temporary directory
//     that Close removes.
//
// A ref that is not an existing path is classified: a path that does not
// exist is returned (resolved) for the caller to report. Stdin is not
// supported. Fetch failures wrap ErrFetch.
func Local(ctx context.Context, sel Selector, ref string, risk DiscoveryRisk) (*LocalSolution, error) {
	if ref != "" {
		if path, err := provider.AbsFromContext(ctx, ref); err == nil {
			if _, err := os.Stat(path); err == nil {
				return &LocalSolution{Path: path}, nil
			}
		}
	}

	s, err := sel.Select(ctx, ref, nil, ArgsAreNames)
	if err != nil {
		return nil, err
	}

	switch s.Source.Kind {
	case source.KindAutoDiscover:
		path, err := s.DiscoveredPath(risk)
		if err != nil {
			return nil, err
		}
		if path == "" {
			return nil, fmt.Errorf("%w: auto-discovery returned no matches", ErrNoSolutionFound)
		}
		reportDiscovery(ctx, path, discoveryPaths(s.Discovered[1:]))
		return &LocalSolution{Path: path}, nil
	case source.KindLocalFile:
		path, err := provider.AbsFromContext(ctx, s.Source.Ref)
		if err != nil {
			return nil, fmt.Errorf("resolve solution path %q: %w", s.Source.Ref, err)
		}
		return &LocalSolution{Path: path}, nil
	case source.KindURL, source.KindRemoteRef, source.KindCatalogQualified, source.KindBareName:
		return fetchLocal(ctx, sel.SelectionLoader(s), s.Source)
	case source.KindUnknown, source.KindStdin:
	}
	return nil, fmt.Errorf("%w: %s source %q; a solution file is required", ErrUnsupportedKind, s.Source.Kind, s.Source.Ref)
}

// fetchLocal loads c and writes it to a new temporary directory.
func fetchLocal(ctx context.Context, l Loader, c source.Classification) (*LocalSolution, error) {
	ReportSource(ctx, c)
	ms, err := l.Load(ctx, c)
	if err != nil {
		return nil, fmt.Errorf("%w %q: %w", ErrFetch, c.Ref, err)
	}

	dir, err := os.MkdirTemp("", settings.BinaryNameFromContext(ctx)+fetchedDirSuffix)
	if err != nil {
		return nil, fmt.Errorf("%w %q: temporary directory: %w", ErrFetch, c.Ref, err)
	}
	local := &LocalSolution{Path: filepath.Join(dir, fetchedFileName), dir: dir}
	if err := writeLocal(local.Path, ms); err != nil {
		_ = local.Close()
		return nil, fmt.Errorf("%w %q: %w", ErrFetch, c.Ref, err)
	}

	if w := writer.FromContext(ctx); w != nil {
		name := ms.Solution.Metadata.Name
		if v := ms.Solution.Metadata.Version; v != nil {
			name += nameVersionSeparator + v.String()
		}
		w.Verbosef("Fetched %s to %s", name, dir)
	}
	return local, nil
}

// writeLocal extracts the bundle (templates, static files) of ms next to
// path, then writes the solution YAML to path.
func writeLocal(path string, ms *MaterializedSolution) error {
	if len(ms.Bundle) > 0 {
		if _, err := bundler.ExtractBundleTar(ms.Bundle, filepath.Dir(path)); err != nil {
			return fmt.Errorf("extract bundle: %w", err)
		}
	}

	content := ms.Solution.RawContent()
	if len(content) == 0 {
		var err error
		if content, err = ms.Solution.ToYAML(); err != nil {
			return fmt.Errorf("marshal solution: %w", err)
		}
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		return fmt.Errorf("write solution: %w", err)
	}
	return nil
}
