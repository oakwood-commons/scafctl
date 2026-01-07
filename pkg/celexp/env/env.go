package env

import (
	"context"
	"sync"

	"github.com/google/cel-go/cel"
	"github.com/oakwood-commons/scafctl/pkg/celexp/ext"
)

var (
	// baseEnvOnce ensures base environment is created only once
	baseEnvOnce sync.Once
	// baseEnvOpts contains all extension function options, cached for reuse
	baseEnvOpts []cel.EnvOption
	// baseEnvErr stores any error from base environment initialization
	baseEnvErr error
)

// getBaseEnvOptions returns cached extension options, creating them once.
// This optimizes repeated calls to New() by avoiding repeated ext.All() calls.
// Context cancellation is checked before and after extension loading, but the
// loading itself is not context-dependent and will complete once started.
func getBaseEnvOptions(ctx context.Context) ([]cel.EnvOption, error) {
	// Check context before potentially waiting on sync.Once
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	baseEnvOnce.Do(func() {
		// Get all CEL extension functions (both built-in and custom)
		// This operation is not context-dependent and should complete once started
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

// New creates a new CEL environment with the provided declarations and all
// registered CEL extension functions from the ext package.
// It accepts variadic EnvOptions to allow for multiple declarations and other options.
//
// The function caches base extension options for performance, so repeated calls
// are much faster than the first call. The context is checked for cancellation
// before and during environment construction.
//
// Example:
//
//	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
//	defer cancel()
//	env, err := env.New(ctx, cel.Variable("x", cel.IntType))
func New(ctx context.Context, declarations ...cel.EnvOption) (*cel.Env, error) {
	// Get cached base extension options
	baseOpts, err := getBaseEnvOptions(ctx)
	if err != nil {
		return nil, err
	}

	// Combine base options with user declarations
	envOpts := make([]cel.EnvOption, 0, len(baseOpts)+len(declarations))
	envOpts = append(envOpts, baseOpts...)
	envOpts = append(envOpts, declarations...)

	// Final context check before creating environment
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return cel.NewEnv(envOpts...)
}
