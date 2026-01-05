package env

import (
	"context"

	"github.com/google/cel-go/cel"
	"github.com/oakwood-commons/scafctl/pkg/celexp/ext"
)

// New creates a new CEL environment with the provided declarations and all
// registered CEL extension functions from the ext package.
// It accepts variadic EnvOptions to allow for multiple declarations and other options.
func New(_ context.Context, declarations ...cel.EnvOption) (*cel.Env, error) {
	// Get all CEL extension functions (both built-in and custom)
	extFuncs := ext.All()

	// Build environment options starting with provided declarations
	envOpts := make([]cel.EnvOption, 0, len(declarations)+len(extFuncs))
	envOpts = append(envOpts, declarations...)

	// Add all extension function EnvOptions
	for _, extFunc := range extFuncs {
		envOpts = append(envOpts, extFunc.EnvOptions...)
	}

	return cel.NewEnv(envOpts...)
}
