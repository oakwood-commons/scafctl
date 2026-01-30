// Package resolver provides types for defining and executing data resolvers.
//
// This file re-exports types from pkg/spec for backward compatibility.
// New code should import from pkg/spec directly when possible.
package resolver

import (
	"github.com/oakwood-commons/scafctl/pkg/spec"
)

// ValueRef is an alias to spec.ValueRef for backward compatibility.
// It represents a value that can be literal, resolver reference, expression, or template.
type ValueRef = spec.ValueRef

// IterationContext is an alias to spec.IterationContext for backward compatibility.
// It holds the context for forEach iteration variables.
type IterationContext = spec.IterationContext
