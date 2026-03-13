// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package solutionprovider

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/provider"
)

type ancestorStackKey struct{}

// WithAncestorStack returns a new context with the given ancestor stack.
// This is used to track the chain of solution invocations for circular reference detection.
func WithAncestorStack(ctx context.Context, stack []string) context.Context {
	return context.WithValue(ctx, ancestorStackKey{}, stack)
}

// AncestorStackFromContext retrieves the ancestor stack from context.
// Returns nil if no stack is set (root-level execution).
func AncestorStackFromContext(ctx context.Context) []string {
	stack, _ := ctx.Value(ancestorStackKey{}).([]string)
	return stack
}

// PushAncestor adds a canonical name to the ancestor stack and returns the updated context.
// Returns an error if the name already exists in the stack (circular reference detected).
func PushAncestor(ctx context.Context, name string) (context.Context, error) {
	stack := AncestorStackFromContext(ctx)

	for _, ancestor := range stack {
		if ancestor == name {
			chain := make([]string, len(stack)+1)
			copy(chain, stack)
			chain[len(stack)] = name
			return ctx, fmt.Errorf("solution: circular reference detected: %s", strings.Join(chain, " \u2192 "))
		}
	}

	newStack := make([]string, len(stack), len(stack)+1)
	copy(newStack, stack)
	newStack = append(newStack, name)

	return WithAncestorStack(ctx, newStack), nil
}

// CheckDepth validates that the current nesting depth does not exceed the maximum allowed.
// Depth is derived from the length of the ancestor stack.
func CheckDepth(ctx context.Context, maxDepth int) error {
	stack := AncestorStackFromContext(ctx)
	if len(stack) >= maxDepth {
		return fmt.Errorf("solution: max nesting depth %d exceeded: %s", maxDepth, strings.Join(stack, " \u2192 "))
	}
	return nil
}

// Canonicalize normalizes a source reference into a canonical name for ancestor tracking.
// File paths are resolved to absolute paths, catalog references and URLs are used as-is.
func Canonicalize(ctx context.Context, source string) string {
	// URLs - use as-is
	if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {
		return source
	}

	// Relative or absolute file paths - resolve to absolute
	if strings.HasPrefix(source, ".") || strings.HasPrefix(source, "/") || strings.Contains(source, string(filepath.Separator)) {
		abs, err := provider.AbsFromContext(ctx, source)
		if err != nil {
			return source // fallback to raw value
		}
		return abs
	}

	// Catalog references (bare name or name@version) - use as-is
	return source
}
