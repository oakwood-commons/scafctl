package resolver

import (
	"context"
	"sync"
	"time"
)

// contextKey is a private type for context keys
type contextKey int

const (
	resolverContextKey contextKey = iota
)

// ExecutionStatus represents the execution status of a resolver
type ExecutionStatus string

const (
	ExecutionStatusSuccess ExecutionStatus = "success"
	ExecutionStatusFailed  ExecutionStatus = "failed"
	ExecutionStatusSkipped ExecutionStatus = "skipped"
)

// PhaseMetrics contains timing information for a single phase
type PhaseMetrics struct {
	Phase    string        `json:"phase" yaml:"phase" doc:"Phase name (resolve, transform, validate)" example:"resolve"`
	Duration time.Duration `json:"duration" yaml:"duration" doc:"Time spent in this phase" example:"100ms"`
	Started  time.Time     `json:"started" yaml:"started" doc:"When phase started"`
	Ended    time.Time     `json:"ended" yaml:"ended" doc:"When phase ended"`
}

// ProviderAttempt records a single provider execution attempt
type ProviderAttempt struct {
	Provider   string        `json:"provider" yaml:"provider" doc:"Provider name" example:"http"`
	Phase      string        `json:"phase" yaml:"phase" doc:"Phase where provider was called" example:"resolve"`
	Error      string        `json:"error,omitempty" yaml:"error,omitempty" doc:"Error message if failed" maxLength:"500"`
	Duration   time.Duration `json:"duration" yaml:"duration" doc:"Time spent in this attempt" example:"50ms"`
	OnError    string        `json:"onError,omitempty" yaml:"onError,omitempty" doc:"Error handling behavior" example:"continue"`
	Timestamp  time.Time     `json:"timestamp" yaml:"timestamp" doc:"When the attempt occurred"`
	SourceStep int           `json:"sourceStep" yaml:"sourceStep" doc:"Source/step index in phase (0-based)" example:"0"`
}

// ExecutionResult contains both the value and execution metadata
type ExecutionResult struct {
	// Value is the actual resolved value
	Value any `json:"value" yaml:"value" doc:"The resolved value"`

	// Metadata (internal use only, not accessible in CEL)
	Status            ExecutionStatus   `json:"status" yaml:"status" doc:"Execution status" example:"success"`
	Phase             int               `json:"phase" yaml:"phase" doc:"Execution phase number (1-based)" example:"1"`
	TotalDuration     time.Duration     `json:"totalDuration" yaml:"totalDuration" doc:"Total execution time across all phases" example:"250ms"`
	StartTime         time.Time         `json:"startTime" yaml:"startTime" doc:"When resolver execution started"`
	EndTime           time.Time         `json:"endTime" yaml:"endTime" doc:"When resolver execution ended"`
	Error             error             `json:"error,omitempty" yaml:"error,omitempty" doc:"Error if failed"`
	PhaseMetrics      []PhaseMetrics    `json:"phaseMetrics" yaml:"phaseMetrics" doc:"Per-phase timing" maxItems:"3"`
	ProviderCallCount int               `json:"providerCallCount" yaml:"providerCallCount" doc:"Number of provider calls made" example:"2"`
	ValueSizeBytes    int64             `json:"valueSizeBytes" yaml:"valueSizeBytes" doc:"Size of the value in bytes" example:"1024"`
	DependencyCount   int               `json:"dependencyCount" yaml:"dependencyCount" doc:"Number of dependencies" example:"3"`
	FailedAttempts    []ProviderAttempt `json:"failedAttempts,omitempty" yaml:"failedAttempts,omitempty" doc:"Failed provider attempts (for debugging)" maxItems:"10"`
}

// Context is a thread-safe storage for resolver results and execution metadata
type Context struct {
	data    *sync.Map // map[string]any - stores actual values for CEL access
	results *sync.Map // map[string]*ExecutionResult - stores full results with metadata
}

// NewContext creates a new resolver context
func NewContext() *Context {
	return &Context{
		data:    &sync.Map{},
		results: &sync.Map{},
	}
}

// Set stores a resolver value (for backward compatibility)
func (c *Context) Set(name string, value any) {
	c.data.Store(name, value)
	// Also store in results with minimal metadata
	c.results.Store(name, &ExecutionResult{
		Value:  value,
		Status: ExecutionStatusSuccess,
	})
}

// SetResult stores a resolver result with full execution metadata
func (c *Context) SetResult(name string, result *ExecutionResult) {
	c.data.Store(name, result.Value)
	c.results.Store(name, result)
}

// Get retrieves a resolver value (for CEL evaluation)
func (c *Context) Get(name string) (any, bool) {
	return c.data.Load(name)
}

// GetResult retrieves the full resolver execution result including metadata
func (c *Context) GetResult(name string) (*ExecutionResult, bool) {
	val, ok := c.results.Load(name)
	if !ok {
		return nil, false
	}
	result, ok := val.(*ExecutionResult)
	return result, ok
}

// Has checks if a resolver exists
func (c *Context) Has(name string) bool {
	_, ok := c.data.Load(name)
	return ok
}

// ToMap returns all resolver data as a map (for CEL evaluation)
// Only returns values, not metadata
func (c *Context) ToMap() map[string]any {
	result := make(map[string]any)
	c.data.Range(func(key, value any) bool {
		if k, ok := key.(string); ok {
			result[k] = value
		}
		return true
	})
	return result
}

// GetAllResults returns all resolver execution results with metadata
// Used for observability, logging, and metrics
func (c *Context) GetAllResults() map[string]*ExecutionResult {
	result := make(map[string]*ExecutionResult)
	c.results.Range(func(key, value any) bool {
		if k, ok := key.(string); ok {
			if v, ok := value.(*ExecutionResult); ok {
				result[k] = v
			}
		}
		return true
	})
	return result
}

// WithContext adds resolver context to a Go context
func WithContext(ctx context.Context, rc *Context) context.Context {
	return context.WithValue(ctx, resolverContextKey, rc)
}

// FromContext retrieves resolver context from a Go context
func FromContext(ctx context.Context) (*Context, bool) {
	rc, ok := ctx.Value(resolverContextKey).(*Context)
	return rc, ok
}
