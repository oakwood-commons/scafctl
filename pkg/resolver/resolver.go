// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package resolver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/spec"
)

// Type is an alias to spec.Type for backward compatibility.
type Type = spec.Type

// Type constants re-exported from spec for backward compatibility.
const (
	TypeString   = spec.TypeString
	TypeInt      = spec.TypeInt
	TypeFloat    = spec.TypeFloat
	TypeBool     = spec.TypeBool
	TypeArray    = spec.TypeArray
	TypeTime     = spec.TypeTime
	TypeDuration = spec.TypeDuration
	TypeAny      = spec.TypeAny
)

// ErrorBehavior is an alias to spec.OnErrorBehavior for backward compatibility.
// New code should use spec.OnErrorBehavior directly.
type ErrorBehavior = spec.OnErrorBehavior

// ErrorBehavior constants re-exported from spec for backward compatibility.
const (
	ErrorBehaviorFail     = spec.OnErrorFail
	ErrorBehaviorContinue = spec.OnErrorContinue
)

// Condition is a resolver-specific condition type with custom YAML/JSON unmarshalling.
// Unlike spec.Condition, this type only wraps the expr field, keeping backward
// compatibility with existing resolver YAML files.
//
// Supported YAML/JSON forms:
//   - String shorthand: when: "_.environment == 'prod'"
//   - Boolean literal:  when: true / when: false
//   - Object form:      when: { expr: "_.environment == 'prod'" }
//   - Expression alias: when: { expression: "_.environment == 'prod'" }
//   - Null:             when: null (treated as unset, nil Expr)
type Condition struct {
	Expr *celexp.Expression `json:"expr" yaml:"expr" doc:"CEL expression that must evaluate to boolean" example:"_.environment == 'prod'"`
}

// UnmarshalYAML supports shorthand forms for conditions.
//   - string → treated as a CEL expression
//   - bool   → converted to literal "true" or "false" CEL expression
//   - object → standard {expr: "..."} form
func (c *Condition) UnmarshalYAML(unmarshal func(any) error) error {
	// Unmarshal to an interface to inspect the type
	var raw any
	if err := unmarshal(&raw); err != nil {
		return fmt.Errorf("invalid condition: %w", err)
	}

	switch v := raw.(type) {
	case nil:
		// Null -> zero-value Condition (nil Expr), consistent with spec.Condition.
		c.Expr = nil
		return nil
	case string:
		expr := celexp.Expression(v)
		c.Expr = &expr
		return nil
	case bool:
		var exprStr string
		if v {
			exprStr = "true"
		} else {
			exprStr = "false"
		}
		expr := celexp.Expression(exprStr)
		c.Expr = &expr
		return nil
	case map[string]any:
		// Object form: extract "expr" or its "expression" alias.
		exprVal, hasExpr := v["expr"]
		expressionVal, hasExpression := v["expression"]
		if hasExpr && hasExpression {
			return fmt.Errorf("invalid condition object: specify either \"expr\" or \"expression\", not both")
		}
		if !hasExpr && !hasExpression {
			return fmt.Errorf("invalid condition object: missing \"expr\" field (valid forms: string, bool, or {expr: \"...\"} / {expression: \"...\"})")
		}
		val := exprVal
		key := "expr"
		if hasExpression {
			val = expressionVal
			key = "expression"
		}
		exprStr, ok := val.(string)
		if !ok {
			return fmt.Errorf("invalid condition object: \"%s\" must be a string, got %T", key, val)
		}
		expr := celexp.Expression(exprStr)
		c.Expr = &expr
		return nil
	default:
		return fmt.Errorf("invalid condition: expected string, bool, or {expr: \"...\"} / {expression: \"...\"} object, got %T", raw)
	}
}

// UnmarshalJSON supports shorthand forms for conditions.
//   - string → treated as a CEL expression
//   - bool   → converted to literal "true" or "false" CEL expression
//   - object → standard {expr: "..."} form
func (c *Condition) UnmarshalJSON(data []byte) error {
	// Null -> zero-value Condition (nil Expr), consistent with spec.Condition.
	if string(bytes.TrimSpace(data)) == "null" {
		c.Expr = nil
		return nil
	}

	// Try string first
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		expr := celexp.Expression(s)
		c.Expr = &expr
		return nil
	}

	// Try bool
	var b bool
	if err := json.Unmarshal(data, &b); err == nil {
		var exprStr string
		if b {
			exprStr = "true"
		} else {
			exprStr = "false"
		}
		expr := celexp.Expression(exprStr)
		c.Expr = &expr
		return nil
	}

	// Try object form {expr: "..."} or its {expression: "..."} alias.
	var obj struct {
		Expr       *celexp.Expression `json:"expr"`
		Expression *celexp.Expression `json:"expression"`
	}
	if err := json.Unmarshal(data, &obj); err != nil {
		return fmt.Errorf("invalid condition: expected string, bool, or {\"expr\": \"...\"} / {\"expression\": \"...\"} object: %w", err)
	}
	if obj.Expr != nil && obj.Expression != nil {
		return fmt.Errorf("invalid condition object: specify either \"expr\" or \"expression\", not both")
	}
	expr := obj.Expr
	if obj.Expression != nil {
		expr = obj.Expression
	}
	if expr == nil {
		return fmt.Errorf("invalid condition object: missing \"expr\" field (valid forms: string, bool, or {\"expr\": \"...\"} / {\"expression\": \"...\"})")
	}
	c.Expr = expr
	return nil
}

// ForEachClause is an alias to spec.ForEachClause for backward compatibility.
type ForEachClause = spec.ForEachClause

// CoerceType is re-exported from spec for backward compatibility.
var CoerceType = spec.CoerceType

// Config contains global resolver configuration
type Config struct {
	MaxValueSizeBytes  int64         `json:"maxValueSizeBytes,omitempty" yaml:"maxValueSizeBytes,omitempty" doc:"Maximum size in bytes for resolver values (default: 10MB)" example:"10485760"`
	WarnValueSizeBytes int64         `json:"warnValueSizeBytes,omitempty" yaml:"warnValueSizeBytes,omitempty" doc:"Warn when resolver value exceeds this size (default: 1MB)" example:"1048576"`
	MaxConcurrency     int           `json:"maxConcurrency,omitempty" yaml:"maxConcurrency,omitempty" doc:"Maximum number of resolvers executing concurrently per phase (default: unlimited)" example:"10"`
	PhaseTimeout       time.Duration `json:"phaseTimeout,omitempty" yaml:"phaseTimeout,omitempty" doc:"Maximum time for an entire phase to complete (default: 5m)" example:"300s"`
}

// Resolver represents a single resolver definition
type Resolver struct {
	// Metadata
	Name        string `json:"name" yaml:"name" doc:"Resolver name (must be unique)" example:"environment" pattern:"^[a-zA-Z_][a-zA-Z0-9_-]*$" patternDescription:"Must start with a letter or underscore, followed by letters, numbers, underscores, or hyphens"`
	Description string `json:"description,omitempty" yaml:"description,omitempty" doc:"Human-readable description" maxLength:"500" example:"Resolves the target deployment environment"`
	DisplayName string `json:"displayName,omitempty" yaml:"displayName,omitempty" doc:"Display name for UI" maxLength:"80" example:"Environment"`
	Sensitive   bool   `json:"sensitive,omitempty" yaml:"sensitive,omitempty" doc:"Whether value should be redacted in table output and logs (JSON/YAML output reveals values for machine consumption)" example:"false"`
	Internal    bool   `json:"internal,omitempty" yaml:"internal,omitempty" doc:"Whether this resolver is internal (excluded from default output, included in structured output)"`
	Explicit    bool   `json:"explicit,omitempty" yaml:"explicit,omitempty" doc:"If true, resolver only runs when explicitly named on the CLI"`
	Example     any    `json:"example,omitempty" yaml:"example,omitempty" doc:"Example value for documentation"`

	// Type declaration
	Type Type `json:"type,omitempty" yaml:"type,omitempty" doc:"Expected type of the resolved value" example:"string"`

	// Conditional execution
	When *Condition `json:"when,omitempty" yaml:"when,omitempty" doc:"Condition for executing this resolver"`

	// Explicit dependencies
	DependsOn []string `json:"dependsOn,omitempty" yaml:"dependsOn,omitempty" doc:"Explicit resolver dependencies (merged with auto-extracted dependencies)" maxItems:"100" example:"[\"config\", \"credentials\"]"`

	// Timeout
	Timeout *time.Duration `json:"timeout,omitempty" yaml:"timeout,omitempty" doc:"Maximum execution time (default: 30s)" example:"30s"`

	// Immutable locks the resolver's value in state after first execution. On subsequent
	// runs, the resolver still executes but its value is compared against the stored value.
	// If the values differ, execution fails with an error. Use this for non-deterministic
	// values (e.g., UUIDs) that must remain stable across runs. Requires the solution to
	// have a state block configured and enabled. Setting Immutable implies Persist.
	Immutable bool `json:"immutable,omitempty" yaml:"immutable,omitempty" doc:"Lock resolver value in state after first execution" example:"true"`

	// Persist records the resolver's value in state after each successful run so it can be
	// read back on a later run via the state provider. Unlike Immutable, the value is not
	// locked or verified -- it is overwritten with the latest value every run. Persistence
	// does not affect replay, does not skip execution, and does not feed the value back into
	// resolver inputs automatically. Requires the solution to have a state block configured
	// and enabled.
	Persist bool `json:"persist,omitempty" yaml:"persist,omitempty" doc:"Record resolver value in state after each run for later retrieval via the state provider" example:"true"`

	// Phases
	Resolve   *ResolvePhase   `json:"resolve" yaml:"resolve" doc:"Value resolution phase"`
	Transform *TransformPhase `json:"transform,omitempty" yaml:"transform,omitempty" doc:"Value transformation phase"`
	Validate  *ValidatePhase  `json:"validate,omitempty" yaml:"validate,omitempty" doc:"Value validation phase"`

	// Messages contains user-defined messages shown on resolver outcomes.
	Messages *Messages `json:"messages,omitempty" yaml:"messages,omitempty" doc:"Custom messages for resolver outcomes"`
}

// Messages holds user-defined messages displayed on resolver outcomes.
type Messages struct {
	// Error is shown when the resolver fails (resolve, transform, or validate phase).
	// Supports static strings, CEL expressions (expr:), and Go templates (tmpl:).
	// The resolver data map is available as _ and the error message as __error.
	Error *ValueRef `json:"error,omitempty" yaml:"error,omitempty" doc:"Custom message on resolver failure"`
}

// ResolvePhase defines how to obtain an initial value
type ResolvePhase struct {
	With  []ProviderSource `json:"with" yaml:"with" doc:"Ordered list of value sources" minItems:"1" maxItems:"50"`
	Until *Condition       `json:"until,omitempty" yaml:"until,omitempty" doc:"Stop condition (default: first non-null)"`
	When  *Condition       `json:"when,omitempty" yaml:"when,omitempty" doc:"Phase-level condition"`
}

// TransformPhase defines how to derive a new value
type TransformPhase struct {
	With []ProviderTransform `json:"with" yaml:"with" doc:"Ordered list of transformations" minItems:"1" maxItems:"50"`
	When *Condition          `json:"when,omitempty" yaml:"when,omitempty" doc:"Phase-level condition"`
}

// ValidatePhase defines validation constraints
type ValidatePhase struct {
	With []ProviderValidation `json:"with" yaml:"with" doc:"Validation rules" minItems:"1" maxItems:"20"`
	When *Condition           `json:"when,omitempty" yaml:"when,omitempty" doc:"Phase-level condition"`
}

// ProviderSource represents a single source in the resolve phase
type ProviderSource struct {
	Provider        string               `json:"provider" yaml:"provider" doc:"Provider name" example:"parameter" maxLength:"100" pattern:"^[a-zA-Z][a-zA-Z0-9_-]*$" patternDescription:"Must start with a letter, followed by letters, numbers, underscores, or hyphens"`
	Inputs          map[string]*ValueRef `json:"inputs,omitempty" yaml:"inputs,omitempty" doc:"Provider inputs" required:"false"`
	When            *Condition           `json:"when,omitempty" yaml:"when,omitempty" doc:"Source-level condition"`
	ContinueOnError *Condition           `json:"continueOnError,omitempty" yaml:"continueOnError,omitempty" doc:"Whether to continue (recover) when the provider fails. Accepts a boolean or a CEL expression evaluated with the error text bound as __error. Truthy skips this source and continues the fallback chain; falsy fails the resolver. Overrides the deprecated onError field."`
	OnError         ErrorBehavior        `json:"onError,omitempty" yaml:"onError,omitempty" deprecated:"true" deprecatedReplacement:"continueOnError" doc:"DEPRECATED: use continueOnError instead. Behavior when provider fails (continue, fail). Defaults to continue (fallback chain semantics). Use fail to stop on first error." example:"continue" default:"continue"`
	ForEach         *ForEachClause       `json:"forEach,omitempty" yaml:"forEach,omitempty" doc:"Iterate over array, executing provider for each element. Requires forEach.in (no __self in resolve phase)."`
}

// ProviderTransform represents a single transform step
type ProviderTransform struct {
	Provider        string               `json:"provider" yaml:"provider" doc:"Provider name" example:"cel" maxLength:"100" pattern:"^[a-zA-Z][a-zA-Z0-9_-]*$" patternDescription:"Must start with a letter, followed by letters, numbers, underscores, or hyphens"`
	Inputs          map[string]*ValueRef `json:"inputs,omitempty" yaml:"inputs,omitempty" doc:"Provider inputs" required:"false"`
	When            *Condition           `json:"when,omitempty" yaml:"when,omitempty" doc:"Step-level condition"`
	ContinueOnError *Condition           `json:"continueOnError,omitempty" yaml:"continueOnError,omitempty" doc:"Whether to continue (recover) when the provider fails. Accepts a boolean or a CEL expression evaluated with the error text bound as __error. Truthy skips this step and keeps the current value; falsy fails the resolver. Overrides the deprecated onError field."`
	OnError         ErrorBehavior        `json:"onError,omitempty" yaml:"onError,omitempty" deprecated:"true" deprecatedReplacement:"continueOnError" doc:"DEPRECATED: use continueOnError instead. Behavior when provider fails (continue, fail)" example:"fail" default:"fail"`
	ForEach         *ForEachClause       `json:"forEach,omitempty" yaml:"forEach,omitempty" doc:"Iterate over array, executing provider for each element"`
}

// ProviderValidation represents a single validation rule
type ProviderValidation struct {
	Provider string               `json:"provider" yaml:"provider" doc:"Provider name" example:"validation" maxLength:"100" pattern:"^[a-zA-Z][a-zA-Z0-9_-]*$" patternDescription:"Must start with a letter, followed by letters, numbers, underscores, or hyphens"`
	Inputs   map[string]*ValueRef `json:"inputs,omitempty" yaml:"inputs,omitempty" doc:"Provider inputs" required:"false"`
	When     *Condition           `json:"when,omitempty" yaml:"when,omitempty" doc:"Rule-level condition; the rule is skipped when this evaluates to false"`
	Message  *ValueRef            `json:"message,omitempty" yaml:"message,omitempty" doc:"Error message on validation failure"`
}
