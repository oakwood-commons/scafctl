package env

import (
	"context"

	"github.com/google/cel-go/cel"
)

func New(_ context.Context, declarations cel.EnvOption) (*cel.Env, error) {
	envOpts := []cel.EnvOption{
		declarations,
	}
	return cel.NewEnv(envOpts...)
}
