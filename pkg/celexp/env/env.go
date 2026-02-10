// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package env

import (
	"context"
	"sync"

	"github.com/google/cel-go/cel"
	"github.com/oakwood-commons/scafctl/pkg/celexp/ext"
	"github.com/oakwood-commons/scafctl/pkg/celexp/ext/debug"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
)

var (
	// baseEnvOnce ensures base environment is created only once
	baseEnvOnce sync.Once
	// baseEnvOpts contains all extension function options (BuiltIn + Custom), cached for reuse.
	// Note: debug.DebugOutFunc is NOT included here because it requires a Writer parameter.
	// It is added separately via NewWithWriter() or by callers using DebugOutEnvOptions().
	baseEnvOpts []cel.EnvOption
	// baseEnvErr stores any error from base environment initialization
	baseEnvErr error
)

// getBaseEnvOptions returns cached extension options, creating them once.
// This optimizes repeated calls to New() by avoiding repeated ext.All() calls.
// Context cancellation is checked before and after extension loading, but the
// loading itself is not context-dependent and will complete once started.
//
// Note: debug.DebugOutFunc is NOT included in the cached options because it
// requires a Writer parameter. Use DebugOutEnvOptions() to add it separately.
func getBaseEnvOptions(ctx context.Context) ([]cel.EnvOption, error) {
	// Check context before potentially waiting on sync.Once
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	baseEnvOnce.Do(func() {
		// Get all CEL extension functions (both built-in and custom)
		// Note: ext.All() excludes debug.DebugOutFunc which requires Writer
		extFuncs := ext.All()

		// Pre-allocate based on typical extension count
		baseEnvOpts = make([]cel.EnvOption, 0, len(extFuncs)*2)

		// Add all extension function EnvOptions
		for _, extFunc := range extFuncs {
			baseEnvOpts = append(baseEnvOpts, extFunc.EnvOptions...)
		}
		// baseEnvErr is intentionally left nil on success
	})

	// If sync.Once encountered an error during initialization, return it
	if baseEnvErr != nil {
		return nil, baseEnvErr
	}

	// Check context after sync.Once completes - this allows callers with cancelled
	// contexts to get an error even though the extensions were successfully cached
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return baseEnvOpts, nil
}

// DebugOutEnvOptions returns the CEL environment options for debug.out with the given Writer.
// This is useful for adding debug.out support to environments created via New().
// If Writer is nil, debug.out will silently skip output.
func DebugOutEnvOptions(w *writer.Writer) []cel.EnvOption {
	return debug.DebugOutFunc(w).EnvOptions
}

// New creates a new CEL environment with the provided declarations and all
// registered CEL extension functions from the ext package.
// It accepts variadic EnvOptions to allow for multiple declarations and other options.
//
// The function caches base extension options for performance, so repeated calls
// are much faster than the first call. The context is checked for cancellation
// before and during environment construction.
//
// debug.out is automatically included if a Writer is found in the context via
// writer.FromContext(ctx). If no Writer is in context, debug.out is not available.
// To explicitly control debug.out, use NewWithWriter() instead.
//
// Example:
//
//	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
//	defer cancel()
//	env, err := env.New(ctx, cel.Variable("x", cel.IntType))
func New(ctx context.Context, declarations ...cel.EnvOption) (*cel.Env, error) {
	// Get cached base extension options (excludes debug.DebugOutFunc)
	baseOpts, err := getBaseEnvOptions(ctx)
	if err != nil {
		return nil, err
	}

	// Check if Writer is available in context for debug.out support
	w := writer.FromContext(ctx)
	debugOpts := DebugOutEnvOptions(w) // nil-safe: debug.out will silently skip if w is nil

	// Combine base options, debug.out options, and user declarations
	envOpts := make([]cel.EnvOption, 0, len(baseOpts)+len(debugOpts)+len(declarations))
	envOpts = append(envOpts, baseOpts...)
	envOpts = append(envOpts, debugOpts...)
	envOpts = append(envOpts, declarations...)

	// Final context check before creating environment
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return cel.NewEnv(envOpts...)
}

// NewWithWriter creates a new CEL environment with debug.out support.
// This is a convenience wrapper around New() that includes debug.DebugOutFunc.
//
// The Writer parameter is used by debug.out for debug output. Pass nil if
// debug output should be silently skipped.
//
// Example:
//
//	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
//	defer cancel()
//	env, err := env.NewWithWriter(ctx, w, cel.Variable("x", cel.IntType))
func NewWithWriter(ctx context.Context, w *writer.Writer, declarations ...cel.EnvOption) (*cel.Env, error) {
	// Prepend debug.out options to user declarations
	debugOpts := DebugOutEnvOptions(w)
	allDeclarations := make([]cel.EnvOption, 0, len(debugOpts)+len(declarations))
	allDeclarations = append(allDeclarations, debugOpts...)
	allDeclarations = append(allDeclarations, declarations...)

	return New(ctx, allDeclarations...)
}
