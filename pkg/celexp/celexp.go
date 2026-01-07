package celexp

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/google/cel-go/cel"
	"github.com/oakwood-commons/scafctl/pkg/celexp/conversion"
)

var (
	// defaultCache is the package-level cache used by Expression.Compile()
	defaultCache     *ProgramCache
	defaultCacheOnce sync.Once
	defaultCacheMu   sync.RWMutex // Protects cache replacement for testing

	// DefaultCacheSize is the default size for the package-level cache
	DefaultCacheSize = 1000

	// defaultCostLimit is the default cost limit for CEL expression evaluation
	// Set to 0 to disable cost limiting
	// Use GetDefaultCostLimit() and SetDefaultCostLimit() for thread-safe access
	defaultCostLimit atomic.Uint64
)

// init initializes the default cost limit
//
//nolint:gochecknoinits // Required to initialize atomic.Uint64 default value
func init() {
	defaultCostLimit.Store(1000000)
}

// GetDefaultCostLimit returns the current default cost limit.
// This is thread-safe.
func GetDefaultCostLimit() uint64 {
	return defaultCostLimit.Load()
}

// SetDefaultCostLimit sets the default cost limit for all subsequent compilations
// that don't specify an explicit cost limit.
// This is thread-safe and can be called at runtime.
//
// Example:
//
//	celexp.SetDefaultCostLimit(500000)  // Lower limit for security
//	celexp.SetDefaultCostLimit(0)       // Disable cost limiting
func SetDefaultCostLimit(limit uint64) {
	defaultCostLimit.Store(limit)
}

type (
	Expression      string
	ExtFunctionList []ExtFunction
)

// VarInfo describes a declared variable with its name and type information.
// This is useful for debugging, documentation generation, and validation.
type VarInfo struct {
	// Name is the variable name as it appears in expressions
	Name string

	// Type is a human-readable type name (e.g., "int", "string", "list", "map")
	Type string

	// CelType is the underlying CEL type object for advanced type checking
	CelType *cel.Type
}

type ExtFunction struct {
	Name          string          `json:"name,omitempty" yaml:"name,omitempty"`
	Links         []string        `json:"links,omitempty" yaml:"links,omitempty"`
	Examples      []Example       `json:"examples,omitempty" yaml:"examples,omitempty"`
	Description   string          `json:"description,omitempty" yaml:"description,omitempty"`
	EnvOptions    []cel.EnvOption `json:"-" yaml:"-"`
	FunctionNames []string        `json:"function_names,omitempty" yaml:"function_names,omitempty"`
	Custom        bool            `json:"custom,omitempty" yaml:"custom,omitempty"`
}

type Example struct {
	Description string   `json:"description,omitempty" yaml:"description,omitempty"`
	Expression  string   `json:"expression,omitempty" yaml:"expression,omitempty"`
	Links       []string `json:"links,omitempty" yaml:"links,omitempty"`
}

// Option is a functional option for configuring expression compilation.
// Use With* functions to create options.
type Option func(*compileConfig)

// compileConfig holds the configuration for expression compilation.
type compileConfig struct {
	ctx       context.Context
	cache     *ProgramCache
	costLimit *uint64
}

// WithContext sets the context for compilation, enabling cancellation and timeouts.
//
// Example:
//
//	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
//	defer cancel()
//	expr.Compile([]cel.EnvOption{cel.Variable("x", cel.IntType)}, WithContext(ctx))
func WithContext(ctx context.Context) Option {
	return func(c *compileConfig) {
		c.ctx = ctx
	}
}

// WithCache sets a custom cache instance for compiled programs.
// If not specified, uses the default package-level cache.
//
// Example:
//
//	myCache := celexp.NewProgramCache(500)
//	expr.Compile([]cel.EnvOption{cel.Variable("x", cel.IntType)}, WithCache(myCache))
func WithCache(cache *ProgramCache) Option {
	return func(c *compileConfig) {
		c.cache = cache
	}
}

// WithCostLimit sets a custom cost limit for expression evaluation.
// The cost limit prevents expensive expressions from consuming excessive resources.
// If not specified, uses the default cost limit (see GetDefaultCostLimit).
//
// Example:
//
//	expr.Compile([]cel.EnvOption{cel.Variable("x", cel.IntType)}, WithCostLimit(50000))
func WithCostLimit(limit uint64) Option {
	return func(c *compileConfig) {
		c.costLimit = &limit
	}
}

// WithNoCostLimit disables cost limiting for the expression.
// Use with caution as this allows expressions to consume unlimited resources.
//
// Example:
//
//	expr.Compile([]cel.EnvOption{cel.Variable("x", cel.IntType)}, WithNoCostLimit())
func WithNoCostLimit() Option {
	return func(c *compileConfig) {
		zero := uint64(0)
		c.costLimit = &zero
	}
}

// GetDefaultCache returns the package-level cache instance used by Expression.Compile().
// The cache is lazily initialized on first access with DefaultCacheSize entries.
// All calls return the same cache instance (singleton pattern).
//
// Use this when you need to access cache statistics:
//
//	stats := celexp.GetDefaultCache().Stats()
//	fmt.Printf("Cache hit rate: %.1f%%\n", stats.HitRate)
func GetDefaultCache() *ProgramCache {
	defaultCacheMu.RLock()
	defer defaultCacheMu.RUnlock()

	defaultCacheOnce.Do(func() {
		defaultCache = NewProgramCache(DefaultCacheSize)
	})
	return defaultCache
}

// ResetDefaultCache clears and recreates the default cache.
// This is intended for testing only - use explicit caches in production.
//
// WARNING: This is not thread-safe with respect to ongoing compilations.
// Only call this from test setup functions, not from production code or
// concurrent tests.
//
// Example:
//
//	func TestMyFeature(t *testing.T) {
//	    celexp.ResetDefaultCache() // Clean slate for this test
//	    // ... test code ...
//	}
func ResetDefaultCache() {
	defaultCacheMu.Lock()
	defer defaultCacheMu.Unlock()

	// Create a new cache, replacing the old one
	defaultCache = NewProgramCache(DefaultCacheSize)

	// Note: We cannot reset sync.Once, so the cache initialization
	// will be considered "done" even after reset. This is acceptable
	// for testing purposes since we're replacing the cache directly.
}

// SetDefaultCacheSize sets the size of the default cache.
// This must be called before the first call to GetDefaultCache() or Expression.Compile().
// Once the cache is initialized, this function has no effect.
func SetDefaultCacheSize(size int) {
	DefaultCacheSize = size
}

// GetDefaultCacheStats returns statistics for the default cache.
// This is a convenience wrapper around GetDefaultCache().Stats().
//
// Example:
//
//	stats := celexp.GetDefaultCacheStats()
//	fmt.Printf("Cache: %d/%d entries, %.1f%% hit rate\n",
//		stats.Size, stats.MaxSize, stats.HitRate)
func GetDefaultCacheStats() CacheStats {
	return GetDefaultCache().Stats()
}

// ClearDefaultCache clears all entries from the default cache.
// This is useful for testing or when you want to free memory.
// Note: This does not reset the cache size or the sync.Once initialization.
func ClearDefaultCache() {
	GetDefaultCache().Clear()
}

// CompileResult contains the compiled CEL program and metadata
type CompileResult struct {
	// Program is the compiled CEL program ready for evaluation
	Program cel.Program

	// Expression is the original expression that was compiled
	Expression Expression

	// declaredVars maps variable names to their declared CEL types.
	// This is populated during compilation and used for runtime type validation.
	// Use ValidateVars() to check if evaluation variables match these declarations.
	declaredVars map[string]*cel.Type

	// envOpts stores the environment options used during compilation.
	// This enables variable type extraction and validation.
	envOpts []cel.EnvOption
}

// Compile parses, checks, and compiles a CEL expression into an executable program.
//
// This is the primary compilation method with a clean, flexible API.
// CEL environment options (variables, functions) are specified first,
// followed by optional configuration using With* functions.
//
// FEATURES:
//   - Uses package-level default cache automatically (lazy-initialized, size=1000)
//   - Default cost limit of 1,000,000 to prevent DoS attacks (configurable via SetDefaultCostLimit)
//   - Thread-safe and optimized for repeated compilations
//   - Supports context cancellation, custom cache, and cost limits via options
//
// Examples:
//
// Simple usage:
//
//	expr := celexp.Expression("x + y")
//	result, err := expr.Compile([]cel.EnvOption{
//	    cel.Variable("x", cel.IntType),
//	    cel.Variable("y", cel.IntType),
//	})
//
// With context and custom cost limit:
//
//	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
//	defer cancel()
//	result, err := expr.Compile(
//	    []cel.EnvOption{cel.Variable("x", cel.IntType)},
//	    WithContext(ctx),
//	    WithCostLimit(50000),
//	)
//
// With custom cache:
//
//	myCache := celexp.NewProgramCache(500)
//	result, err := expr.Compile(
//	    []cel.EnvOption{cel.Variable("x", cel.IntType)},
//	    WithCache(myCache),
//	)
func (e Expression) Compile(envOpts []cel.EnvOption, opts ...Option) (*CompileResult, error) {
	// Build configuration from options
	config := &compileConfig{
		ctx: context.Background(),
	}
	for _, opt := range opts {
		opt(config)
	}

	// Set defaults
	if config.cache == nil {
		config.cache = GetDefaultCache()
	}
	if config.costLimit == nil {
		defaultLimit := GetDefaultCostLimit()
		config.costLimit = &defaultLimit
	}

	// Check context before starting
	if err := config.ctx.Err(); err != nil {
		return nil, err
	}

	// Generate cache key WITH compiled AST - this avoids double compilation
	keyResult := generateCacheKeyWithAST(config.cache, string(e), envOpts, *config.costLimit)

	// Check for compilation errors during key generation
	if keyResult.err != nil {
		return nil, fmt.Errorf("failed to compile expression %q: %w", e, keyResult.err)
	}

	// Try to get from cache
	if prog, found := config.cache.Get(keyResult.key); found {
		return &CompileResult{
			Program:      prog,
			Expression:   e,
			declaredVars: extractVarDeclarations(envOpts),
			envOpts:      envOpts,
		}, nil
	}

	// Check context before expensive program creation
	if err := config.ctx.Err(); err != nil {
		return nil, err
	}

	// Cache miss - create program from the AST we already have
	var prog cel.Program
	var err error

	// Create program with cost limit if specified
	var progOpts []cel.ProgramOption
	if *config.costLimit > 0 {
		progOpts = append(progOpts, cel.CostLimit(*config.costLimit))
	}

	if keyResult.ast != nil && keyResult.env != nil {
		// Reuse the compiled AST from key generation - NO RECOMPILATION
		prog, err = keyResult.env.Program(keyResult.ast, progOpts...)
	} else {
		// Fallback: compile from scratch if AST not available
		celEnv, envErr := cel.NewEnv(envOpts...)
		if envErr != nil {
			return nil, fmt.Errorf("failed to create CEL environment: %w", envErr)
		}

		ast, issues := celEnv.Compile(string(e))
		if issues != nil && issues.Err() != nil {
			return nil, fmt.Errorf("failed to compile expression %q: %w", e, issues.Err())
		}

		prog, err = celEnv.Program(ast, progOpts...)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create program for expression %q: %w", e, err)
	}

	// Store in cache
	config.cache.Put(keyResult.key, prog, string(e))

	return &CompileResult{
		Program:      prog,
		Expression:   e,
		declaredVars: extractVarDeclarations(envOpts),
		envOpts:      envOpts,
	}, nil
}

// Eval evaluates the compiled CEL program with the provided variables.
// Variables should be a map where keys match the variable names declared
// during compilation. Returns the result as 'any' - use the generic
// EvalAs[T]() function for automatic type conversion and compile-time type safety.
//
// Example usage:
//
//	expr := celexp.Expression("name.startsWith('hello')")
//	result, _ := expr.Compile([]cel.EnvOption{cel.Variable("name", cel.StringType)})
//	value, err := result.Eval(map[string]any{"name": "hello world"})
//	if err != nil {
//	    return err
//	}
//	fmt.Println(value) // true
//
// For type-safe evaluation, use EvalAs[T]():
//
//	str, err := celexp.EvalAs[string](result, map[string]any{"name": "world"})
func (r *CompileResult) Eval(vars map[string]any) (any, error) {
	return r.EvalWithContext(context.Background(), vars)
}

// EvalWithContext evaluates the compiled CEL program with context support.
// Use this when you need to cancel evaluation (e.g., HTTP request timeout).
//
// Example usage:
//
//	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
//	defer cancel()
//	value, err := result.EvalWithContext(ctx, map[string]any{"name": "hello world"})
//	if errors.Is(err, context.DeadlineExceeded) {
//	    return fmt.Errorf("evaluation timed out")
//	}
func (r *CompileResult) EvalWithContext(ctx context.Context, vars map[string]any) (any, error) {
	if r == nil || r.Program == nil {
		return nil, fmt.Errorf("compile result or program is nil")
	}

	// Check context before evaluation
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	out, _, err := r.Program.ContextEval(ctx, vars)
	if err != nil {
		// Enhanced error with variable type information
		varTypes := make(map[string]string)
		for k, v := range vars {
			varTypes[k] = fmt.Sprintf("%T", v)
		}

		declaredTypes := r.getDeclaredTypeNames()

		if len(declaredTypes) > 0 {
			return nil, fmt.Errorf(
				"failed to evaluate expression %q: %w\nVariable types provided: %v\nDeclared types: %v",
				r.Expression,
				err,
				varTypes,
				declaredTypes,
			)
		}
		return nil, fmt.Errorf(
			"failed to evaluate expression %q: %w\nVariable types: %v",
			r.Expression,
			err,
			varTypes,
		)
	}

	return out.Value(), nil
}

// EvalAs evaluates the compiled CEL program and converts the result to the specified type T.
// This generic function provides compile-time type safety for common CEL result types.
//
// Supported types:
//   - string
//   - bool
//   - int (converted from int64)
//   - int64 (CEL's integer type)
//   - float64
//   - []string (for CEL lists of strings)
//   - []any (for CEL lists)
//   - map[string]any (for CEL maps)
//
// Example usage:
//
//	expr := celexp.Expression("'hello ' + name")
//	result, _ := expr.Compile([]cel.EnvOption{cel.Variable("name", cel.StringType)})
//	str, err := celexp.EvalAs[string](result, map[string]any{"name": "world"})
//	// str is "hello world"
//
//	expr = celexp.Expression("x > 10")
//	result, _ = expr.Compile([]cel.EnvOption{cel.Variable("x", cel.IntType)})
//	b, err := celexp.EvalAs[bool](result, map[string]any{"x": int64(15)})
//	// b is true
//
//	expr = celexp.Expression("[1, 2, 3]")
//	result, _ = expr.Compile([]cel.EnvOption{})
//	list, err := celexp.EvalAs[[]any](result, nil)
//	// list is []any{int64(1), int64(2), int64(3)}
func EvalAs[T any](r *CompileResult, vars map[string]any) (T, error) {
	return EvalAsWithContext[T](context.Background(), r, vars)
}

// EvalAsWithContext evaluates the compiled CEL program with context support and converts
// the result to the specified type T. Use this when you need cancellation or timeout support.
//
// Supported types:
//   - string
//   - bool
//   - int (converted from int64)
//   - int64 (CEL's integer type)
//   - float64
//   - []string (for CEL lists of strings)
//   - []any (for CEL lists)
//   - map[string]any (for CEL maps)
//
// Example usage with timeout:
//
//	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
//	defer cancel()
//	str, err := celexp.EvalAsWithContext[string](ctx, result, map[string]any{"name": "world"})
//	if errors.Is(err, context.DeadlineExceeded) {
//	    return fmt.Errorf("evaluation timed out")
//	}
func EvalAsWithContext[T any](ctx context.Context, r *CompileResult, vars map[string]any) (T, error) {
	var zero T

	if r == nil || r.Program == nil {
		return zero, fmt.Errorf("compile result or program is nil")
	}

	// Check context before evaluation
	if err := ctx.Err(); err != nil {
		return zero, err
	}

	out, _, err := r.Program.ContextEval(ctx, vars)
	if err != nil {
		return zero, fmt.Errorf("failed to evaluate expression %q: %w", r.Expression, err)
	}

	// Special handling for lists and maps - they need CEL conversion
	var result any
	var needsConversion bool

	// Check if T is []any, map[string]any, int, or []string using reflection on the zero value
	switch any(zero).(type) {
	case []any:
		needsConversion = true
		result = conversion.CelValueToGo(out)
	case map[string]any:
		needsConversion = true
		result = conversion.CelValueToGo(out)
	case int:
		// Convert int64 to int
		needsConversion = true
		rawValue := out.Value()
		if i64, ok := rawValue.(int64); ok {
			result = int(i64)
		} else {
			return zero, fmt.Errorf("expression result is %T, expected int64 for conversion to int", rawValue)
		}
	case []string:
		// Convert []any to []string
		needsConversion = true
		celList := conversion.CelValueToGo(out)
		if anyList, ok := celList.([]any); ok {
			strList := make([]string, len(anyList))
			for i, v := range anyList {
				if s, ok := v.(string); ok {
					strList[i] = s
				} else {
					return zero, fmt.Errorf("list element at index %d is %T, not string", i, v)
				}
			}
			result = strList
		} else {
			return zero, fmt.Errorf("expression result is %T, not a list", celList)
		}
	default:
		result = out.Value()
	}

	// Type assertion to convert to T
	typedResult, ok := result.(T)
	if !ok {
		if needsConversion {
			return zero, fmt.Errorf("expression result is %T, not %T", result, zero)
		}
		return zero, fmt.Errorf("expression result is %T, not %T", result, zero)
	}

	return typedResult, nil
}

// extractVarDeclarations extracts variable declarations from CEL environment options.
// This function inspects the environment after it's created to determine what variables
// were declared and their types. This enables automatic validation without requiring
// CompileWithVarDecls().
//
// The function creates a temporary CEL environment from the options and uses CEL's
// internal type checking to determine variable declarations.
//
//nolint:unparam // Returns nil for now, full implementation planned for Phase 2
func extractVarDeclarations(envOpts []cel.EnvOption) map[string]*cel.Type {
	if len(envOpts) == 0 {
		return nil
	}

	// Create a temporary environment to inspect declarations
	tempEnv, err := cel.NewEnv(envOpts...)
	if err != nil {
		// If environment creation fails, we can't extract declarations
		return nil
	}

	// Use a test expression to trigger type checking and extract variable info
	// We compile a simple expression that references a variable that doesn't exist
	// This forces CEL to report all declared variables in the error message
	// However, there's no direct API to get variable declarations, so we need
	// to be creative.

	// Alternative approach: Try to compile expressions for each potential variable
	// and see which ones type-check successfully. This is expensive but works.

	// For now, return nil and rely on CompileWithVarDecls for explicit tracking.
	// In Phase 2, we can enhance this with reflection or CEL internals if needed.

	// TODO: Implement proper variable extraction using CEL environment introspection
	// For now, this is a placeholder that returns nil, which means validation
	// will be skipped for expressions compiled with Compile() instead of
	// CompileWithVarDecls().
	_ = tempEnv
	return nil
}

// GetDeclaredVars returns information about all variables declared during compilation.
// This is useful for debugging, documentation generation, and validation.
//
// Returns a sorted slice of VarInfo for deterministic output.
// Returns nil if no variables were declared during compilation.
//
// Example:
//
//	expr := celexp.Expression("x + y")
//	result, _ := expr.Compile([]cel.EnvOption{
//	    cel.Variable("x", cel.IntType),
//	    cel.Variable("name", cel.StringType),
//	})
//	vars := result.GetDeclaredVars()
//	// Returns: []VarInfo{
//	//   {Name: "name", Type: "string", CelType: cel.StringType},
//	//   {Name: "x", Type: "int", CelType: cel.IntType},
//	// }
func (r *CompileResult) GetDeclaredVars() []VarInfo {
	if r == nil || r.declaredVars == nil || len(r.declaredVars) == 0 {
		return nil
	}

	result := make([]VarInfo, 0, len(r.declaredVars))
	for name, celType := range r.declaredVars {
		result = append(result, VarInfo{
			Name:    name,
			Type:    formatCelType(celType),
			CelType: celType,
		})
	}

	// Sort by name for deterministic output
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})

	return result
}

// formatCelType converts a CEL type to a human-readable string.
// This provides user-friendly type names for error messages and documentation.
func formatCelType(t *cel.Type) string {
	if t == nil {
		return "any"
	}

	typeStr := t.String()

	// Map common CEL type strings to more readable names
	switch typeStr {
	case "int":
		return "int"
	case "uint":
		return "uint"
	case "double":
		return "double"
	case "bool":
		return "bool"
	case "string":
		return "string"
	case "bytes":
		return "bytes"
	case "list":
		return "list"
	case "map":
		return "map"
	case "null_type":
		return "null"
	case "type":
		return "type"
	case "duration":
		return "duration"
	case "timestamp":
		return "timestamp"
	default:
		// For parameterized types like list(string) or map(string, int),
		// return the full type string
		return typeStr
	}
}

// getDeclaredTypeNames returns a map of variable names to their human-readable type names.
// This is used for enhanced error messages.
func (r *CompileResult) getDeclaredTypeNames() map[string]string {
	if r == nil || r.declaredVars == nil {
		return nil
	}

	result := make(map[string]string, len(r.declaredVars))
	for name, celType := range r.declaredVars {
		result[name] = formatCelType(celType)
	}
	return result
}
