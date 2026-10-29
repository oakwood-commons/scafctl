// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package load defines loaders that materialize a solution from a classified
// source (catalog, local file, stdin, or URL).
package load

import (
	"context"
	"errors"
	"fmt"

	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
	"github.com/oakwood-commons/scafctl/pkg/solution/source"
)

var (
	// ErrNotImplemented is returned by loader stubs that are not yet implemented.
	ErrNotImplemented = errors.New("loader not implemented")
	// ErrUnsupportedKind is returned when a loader is given a source kind it
	// does not handle.
	ErrUnsupportedKind = errors.New("unsupported source kind")
)

// MaterializedSolution is a fully fetched solution plus its auxiliary OCI layers.
type MaterializedSolution struct {
	Solution *solution.Solution    `json:"solution" yaml:"solution" doc:"The parsed solution"`
	Bundle   []byte                `json:"-" yaml:"-" doc:"Raw bundle tar layer; nil when absent"`
	Lock     []byte                `json:"-" yaml:"-" doc:"JSON-encoded lock (OCI lock layer format, see bundler.ParseLockJSON); nil when absent"`
	Source   source.Classification `json:"source" yaml:"source" doc:"How the solution was located"`
	// Served is set for catalog sources only.
	Served *catalog.Reference `json:"served,omitempty" yaml:"served,omitempty" doc:"Reference the catalog actually served (resolved version, digest, origin); nil for non-catalog sources"`
	Pulled bool               `json:"pulled,omitempty" yaml:"pulled,omitempty" doc:"True when the artifact was pulled from a remote during this load rather than served from the local catalog"`
	// Discovery is set when the solution was found by auto-discovery.
	Discovery *Discovery `json:"discovery,omitempty" yaml:"discovery,omitempty" doc:"Auto-discovery details; nil when the source was explicit"`
	// OtherMatches lists other discovered solution files that were not loaded.
	OtherMatches []string `json:"otherMatches,omitempty" yaml:"otherMatches,omitempty" doc:"Other solution files found by auto-discovery that were not loaded"`
}

// LockFile parses the solution's lock layer. It returns nil, nil when the
// solution has no lock (for example a URL source, or an artifact that
// predates lock layers). A present but malformed or unsupported lock is an
// error.
func (m *MaterializedSolution) LockFile() (*bundler.LockFile, error) {
	if len(m.Lock) == 0 {
		return nil, nil
	}
	lf, err := bundler.ParseLockJSON(m.Lock)
	if err != nil {
		return nil, fmt.Errorf("solution lock: %w", err)
	}
	return lf, nil
}

// Loader fetches a solution for a classified source of the kinds it supports.
type Loader interface {
	Load(ctx context.Context, c source.Classification) (*MaterializedSolution, error)
}

// LenientOption makes a loader skip the fatal spec validation gate so advisory
// tooling (lint / validate) can report every problem in one pass. Parse errors
// are still returned. Execution paths (run / build) must NOT enable this.
// It is accepted by NewFileLoader, NewURLLoader, NewCatalogLoader,
// NewStdinLoader, and NewRouter.
type LenientOption struct{}

var (
	_ FileOption    = LenientOption{}
	_ URLOption     = LenientOption{}
	_ CatalogOption = LenientOption{}
	_ StdinOption   = LenientOption{}
	_ RouterOption  = LenientOption{}
)

// WithLenient returns a LenientOption.
func WithLenient() LenientOption {
	return LenientOption{}
}

func (LenientOption) applyFile(l *FileLoader) { l.lenient = true }

func (LenientOption) applyURL(l *URLLoader) { l.lenient = true }

func (LenientOption) applyCatalog(l *CatalogLoader) { l.lenient = true }

func (LenientOption) applyStdin(l *StdinLoader) { l.lenient = true }

func (LenientOption) applyRouter(c *routerConfig) { c.lenient = true }

// parseSolution parses data into sol, skipping the fatal spec validation gate
// when lenient is set.
func parseSolution(sol *solution.Solution, data []byte, lenient bool) error {
	if lenient {
		return sol.LoadFromBytesLenient(data)
	}
	return sol.LoadFromBytes(data)
}
