# Package spec

The `spec` package provides shared types used across both the resolver and action systems in scafctl.

## Overview

This package extracts common types that were originally in the `pkg/resolver` package but are now shared between resolvers and actions. This ensures consistent behavior and reduces code duplication.

## Types

### ValueRef

`ValueRef` represents a value that can be:
- A literal value (string, number, boolean, array, map)
- A resolver reference (`rslvr: resolverName`)
- A CEL expression (`expr: _.environment == 'prod'`)
- A Go template (`tmpl: "{{ ._.name }}"`)

```go
// Literal value
value := &ValueRef{Literal: "hello"}

// Resolver reference
resolver := "config"
value := &ValueRef{Resolver: &resolver}

// CEL expression
expr := celexp.Expression("_.count + 1")
value := &ValueRef{Expr: &expr}

// Go template  
tmpl := gotmpl.GoTemplatingContent("Hello {{ ._.name }}")
value := &ValueRef{Tmpl: &tmpl}
```

### IterationContext

`IterationContext` holds context for forEach iteration variables:

```go
iterCtx := &IterationContext{
    Item:       currentElement,
    Index:      currentIndex,
    ItemAlias:  "region",    // Custom name for __item
    IndexAlias: "i",         // Custom name for __index
}
```

### Condition

`Condition` wraps a CEL expression for conditional execution:

```go
cond := &Condition{
    Expr: celexp.NewExpression("_.environment == 'prod'"),
}
```

### ForEachClause

`ForEachClause` defines iteration over an array:

```go
forEach := &ForEachClause{
    Item:        "region",   // Alias for __item
    Index:       "i",        // Alias for __index
    In:          &ValueRef{Literal: []string{"us-east", "eu-west"}},
    Concurrency: 5,          // Max parallel iterations
    OnError:     OnErrorContinue, // For actions only
}
```

### OnErrorBehavior

`OnErrorBehavior` defines how to handle errors:

```go
const (
    OnErrorFail     OnErrorBehavior = "fail"     // Stop and return error (default)
    OnErrorContinue OnErrorBehavior = "continue" // Continue execution
)
```

### Type Coercion

Type coercion utilities for converting values between types:

```go
// Coerce to string
str, err := CoerceType(42, TypeString) // "42"

// Coerce to int
num, err := CoerceType("42", TypeInt)  // 42

// Coerce to time
t, err := CoerceType("2026-01-14T12:00:00Z", TypeTime)
```

## Usage

Import the package:

```go
import "github.com/oakwood-commons/scafctl/pkg/spec"
```

The `pkg/resolver` package re-exports these types as aliases for backward compatibility:

```go
// In pkg/resolver
type ValueRef = spec.ValueRef
type Condition = spec.Condition
// etc.
```
