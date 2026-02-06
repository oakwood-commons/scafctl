package input

import "context"

type contextKey struct{}

// WithInput returns a new context with the Input attached.
func WithInput(ctx context.Context, i *Input) context.Context {
	return context.WithValue(ctx, contextKey{}, i)
}

// FromContext retrieves the Input from the context.
// Returns nil if no Input is present.
func FromContext(ctx context.Context) *Input {
	i, _ := ctx.Value(contextKey{}).(*Input)
	return i
}

// MustFromContext retrieves the Input from the context.
// Panics if no Input is present.
func MustFromContext(ctx context.Context) *Input {
	i := FromContext(ctx)
	if i == nil {
		panic("input: no Input in context")
	}
	return i
}
