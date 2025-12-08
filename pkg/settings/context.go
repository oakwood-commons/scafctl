package settings

import (
	"context"
)

type contextKey string

const (
	settingsContextKey contextKey = "settings"
)

// IntoContext stores a Settings object in the context
func IntoContext(ctx context.Context, s *Run) context.Context {
	return context.WithValue(ctx, settingsContextKey, s)
}

// FromContext retrieves a Settings object from the context
func FromContext(ctx context.Context) (*Run, bool) {
	val := ctx.Value(settingsContextKey)
	s, ok := val.(*Run)
	return s, ok
}
