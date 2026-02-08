// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package resolver

import (
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

// Condition is an alias to spec.Condition for backward compatibility.
// Note: The resolver package uses a custom Condition that only wraps the expr field,
// keeping backward compatibility with existing resolver YAML files.
type Condition struct {
	Expr *celexp.Expression `json:"expr" yaml:"expr" doc:"CEL expression that must evaluate to boolean" example:"_.environment == 'prod'"`
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
	Sensitive   bool   `json:"sensitive,omitempty" yaml:"sensitive,omitempty" doc:"Whether value should be redacted in logs" example:"false"`
	Example     any    `json:"example,omitempty" yaml:"example,omitempty" doc:"Example value for documentation"`

	// Type declaration
	Type Type `json:"type,omitempty" yaml:"type,omitempty" doc:"Expected type of the resolved value" example:"string"`

	// Conditional execution
	When *Condition `json:"when,omitempty" yaml:"when,omitempty" doc:"Condition for executing this resolver"`

	// Explicit dependencies
	DependsOn []string `json:"dependsOn,omitempty" yaml:"dependsOn,omitempty" doc:"Explicit resolver dependencies (merged with auto-extracted dependencies)" maxItems:"100" example:"[\"config\", \"credentials\"]"`

	// Timeout
	Timeout *time.Duration `json:"timeout,omitempty" yaml:"timeout,omitempty" doc:"Maximum execution time (default: 30s)" example:"30s"`

	// Phases
	Resolve   *ResolvePhase   `json:"resolve" yaml:"resolve" doc:"Value resolution phase"`
	Transform *TransformPhase `json:"transform,omitempty" yaml:"transform,omitempty" doc:"Value transformation phase"`
	Validate  *ValidatePhase  `json:"validate,omitempty" yaml:"validate,omitempty" doc:"Value validation phase"`
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
	Provider string               `json:"provider" yaml:"provider" doc:"Provider name" example:"parameter" maxLength:"100" pattern:"^[a-zA-Z][a-zA-Z0-9_-]*$" patternDescription:"Must start with a letter, followed by letters, numbers, underscores, or hyphens"`
	Inputs   map[string]*ValueRef `json:"inputs" yaml:"inputs" doc:"Provider inputs"`
	When     *Condition           `json:"when,omitempty" yaml:"when,omitempty" doc:"Source-level condition"`
	OnError  ErrorBehavior        `json:"onError,omitempty" yaml:"onError,omitempty" doc:"Behavior when provider fails (continue, fail)" example:"fail" default:"fail"`
}

// ProviderTransform represents a single transform step
type ProviderTransform struct {
	Provider string               `json:"provider" yaml:"provider" doc:"Provider name" example:"cel" maxLength:"100" pattern:"^[a-zA-Z][a-zA-Z0-9_-]*$" patternDescription:"Must start with a letter, followed by letters, numbers, underscores, or hyphens"`
	Inputs   map[string]*ValueRef `json:"inputs" yaml:"inputs" doc:"Provider inputs"`
	When     *Condition           `json:"when,omitempty" yaml:"when,omitempty" doc:"Step-level condition"`
	OnError  ErrorBehavior        `json:"onError,omitempty" yaml:"onError,omitempty" doc:"Behavior when provider fails (continue, fail)" example:"fail" default:"fail"`
	ForEach  *ForEachClause       `json:"forEach,omitempty" yaml:"forEach,omitempty" doc:"Iterate over array, executing provider for each element"`
}

// ProviderValidation represents a single validation rule
type ProviderValidation struct {
	Provider string               `json:"provider" yaml:"provider" doc:"Provider name" example:"validation" maxLength:"100" pattern:"^[a-zA-Z][a-zA-Z0-9_-]*$" patternDescription:"Must start with a letter, followed by letters, numbers, underscores, or hyphens"`
	Inputs   map[string]*ValueRef `json:"inputs" yaml:"inputs" doc:"Provider inputs"`
	Message  *ValueRef            `json:"message,omitempty" yaml:"message,omitempty" doc:"Error message on validation failure"`
}
