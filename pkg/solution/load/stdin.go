// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package load

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/source"
)

// StdinLoader loads a solution from standard input.
// Supported kinds: KindStdin.
type StdinLoader struct {
	in      io.Reader
	lenient bool
}

var _ Loader = (*StdinLoader)(nil)

// StdinOption configures a StdinLoader.
type StdinOption interface {
	applyStdin(*StdinLoader)
}

// NewStdinLoader returns a StdinLoader reading from in.
func NewStdinLoader(in io.Reader, opts ...StdinOption) *StdinLoader {
	l := &StdinLoader{in: in}
	for _, opt := range opts {
		opt.applyStdin(l)
	}
	return l
}

// Load reads the whole of the input and parses it as a solution.
//
// Compose entries are not applied: stdin has no file location to resolve them
// against, so sol.Compose is left as parsed and nothing is merged.
func (l *StdinLoader) Load(_ context.Context, c source.Classification) (*MaterializedSolution, error) {
	if c.Kind != source.KindStdin {
		return nil, fmt.Errorf("stdin loader: %w: %s", ErrUnsupportedKind, c.Kind)
	}
	if l.in == nil {
		return nil, errors.New("stdin requested but no reader provided")
	}

	data, err := io.ReadAll(l.in)
	if err != nil {
		return nil, fmt.Errorf("reading solution from stdin: %w", err)
	}

	sol := &solution.Solution{}
	if err := parseSolution(sol, data, l.lenient); err != nil {
		return nil, fmt.Errorf("parsing solution from stdin: %w", err)
	}

	return &MaterializedSolution{
		Solution: sol,
		Source:   c,
	}, nil
}
