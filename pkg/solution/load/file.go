// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package load

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/oakwood-commons/scafctl/pkg/fs"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
	"github.com/oakwood-commons/scafctl/pkg/solution/source"
	"github.com/oakwood-commons/scafctl/pkg/telemetry"
)

// FileLoader loads a solution from a known local filesystem path.
// Auto-discovery is handled by DiscoverLoader.
// Supported kinds: KindLocalFile.
type FileLoader struct {
	readFile  fs.ReadFileFunc
	lenient   bool
	baseDir   string
	parseOnly bool
}

var _ Loader = (*FileLoader)(nil)

// FileOption configures a FileLoader.
type FileOption interface {
	applyFile(*FileLoader)
}

type fileOptionFunc func(*FileLoader)

func (f fileOptionFunc) applyFile(l *FileLoader) { f(l) }

// WithReadFile overrides the function used to read the solution file.
// Defaults to os.ReadFile.
func WithReadFile(readFile fs.ReadFileFunc) FileOption {
	return fileOptionFunc(func(l *FileLoader) {
		l.readFile = readFile
	})
}

// WithBaseDir sets the directory that compose files and the
// bundler.DefaultLockFileName are resolved against, instead of the solution
// file's directory. A relative dir is resolved against the context working
// directory (--cwd). An empty dir keeps the default.
func WithBaseDir(dir string) FileOption {
	return fileOptionFunc(func(l *FileLoader) {
		l.baseDir = dir
	})
}

// WithParseOnly loads the solution document as written: compose files are
// not merged and the lock file is not read, so neither can fail the load.
func WithParseOnly() FileOption {
	return fileOptionFunc(func(l *FileLoader) {
		l.parseOnly = true
	})
}

// NewFileLoader returns a FileLoader configured with opts.
func NewFileLoader(opts ...FileOption) *FileLoader {
	l := &FileLoader{readFile: os.ReadFile}
	for _, opt := range opts {
		opt.applyFile(l)
	}
	return l
}

// Load reads and parses the solution file referenced by c, then merges any
// compose files and reads the lock file (both resolved relative to the base dir,
// defaulting to the solution's directory). Relative paths are resolved against
// the context working directory (--cwd).
func (l *FileLoader) Load(ctx context.Context, c source.Classification) (*MaterializedSolution, error) {
	if c.Kind != source.KindLocalFile {
		return nil, fmt.Errorf("file loader: %w: %s", ErrUnsupportedKind, c.Kind)
	}

	path, err := provider.AbsFromContext(ctx, c.Ref)
	if err != nil {
		return nil, fmt.Errorf("resolving solution path %q: %w", c.Ref, err)
	}

	baseDir := filepath.Dir(path)
	if l.baseDir != "" {
		if baseDir, err = provider.AbsFromContext(ctx, l.baseDir); err != nil {
			return nil, fmt.Errorf("resolving base dir %q: %w", l.baseDir, err)
		}
	}

	_, span := telemetry.Tracer(telemetry.TracerSolution).Start(
		ctx, "solution.load.File",
		trace.WithAttributes(attribute.String("solution.path", path)),
	)
	defer span.End()

	log := logger.FromContext(ctx).WithValues("path", path)
	log.V(1).Info("reading solution from local filesystem")

	data, err := l.readFile(path)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("reading solution %q: %w", path, err)
	}

	sol := &solution.Solution{}
	// Set the path before parsing so the source map built during
	// unmarshalling records the correct file in finding positions.
	sol.SetPath(path)
	if err := parseSolution(sol, data, l.lenient); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("loading solution %q: %w", path, err)
	}

	if l.parseOnly {
		log.V(1).Info("loaded solution from local filesystem (parse only)", "size", len(data))
		return &MaterializedSolution{Solution: sol, Source: c}, nil
	}

	if len(sol.Compose) > 0 {
		composed, err := bundler.Compose(sol, baseDir, bundler.WithReadFileFunc(l.readFile))
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return nil, fmt.Errorf("composing solution %q: %w", path, err)
		}
		composed.SetPath(path)
		log.V(1).Info("composed solution", "composeFiles", len(sol.Compose))
		sol = composed
	}

	lock, err := l.readLock(baseDir)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	log.V(1).Info("loaded solution from local filesystem", "size", len(data), "hasLock", lock != nil)

	return &MaterializedSolution{
		Solution: sol,
		Lock:     lock,
		Source:   c,
	}, nil
}

// readLock reads the bundler.DefaultLockFileName in dir and returns it in the
// JSON lock-layer encoding used by catalog artifacts, so
// MaterializedSolution.Lock has one format regardless of source. It returns
// (nil, nil) when no lock file exists. An unreadable, malformed, or
// unsupported-version lock is an error so its pins are never silently dropped.
func (l *FileLoader) readLock(dir string) ([]byte, error) {
	lockPath := filepath.Join(dir, bundler.DefaultLockFileName)

	data, err := l.readFile(lockPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading lock file %q: %w", lockPath, err)
	}

	lf, err := bundler.ParseLockYAML(data)
	if err != nil {
		return nil, fmt.Errorf("loading lock file %q: %w", lockPath, err)
	}

	lock, err := bundler.MarshalLockJSON(lf)
	if err != nil {
		return nil, fmt.Errorf("encoding lock file %q: %w", lockPath, err)
	}
	return lock, nil
}
