// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package parameterprovider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/oakwood-commons/scafctl/pkg/httpc"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/schemahelper"
	"github.com/oakwood-commons/scafctl/pkg/ptrs"
	"github.com/oakwood-commons/scafctl/pkg/settings"
)

const (
	// ProviderName is the name of the parameter provider
	ProviderName = "parameter"
	// Version is the version of the parameter provider
	Version = "1.0.0"

	// TypeAuto is the default value for the "type" input. It infers booleans,
	// numbers, JSON, and file:// sources, falling back to the literal string.
	// http://+https:// values are NOT fetched; use TypeFetch for that.
	// Comma-separated values are NOT auto-split; use TypeCSV for that.
	TypeAuto = "auto"
	// TypeString forces the value to a string, stripping surrounding quotes and
	// coercing non-string values to their string representation.
	TypeString = "string"
	// TypeRaw returns the value exactly as received, with no coercion or
	// quote-stripping (a numeric YAML default stays numeric, a CLI string stays
	// verbatim). This is the "disable inference" escape hatch.
	TypeRaw = "raw"
	// TypeInt forces integer parsing; a non-integer value is an error.
	TypeInt = "int"
	// TypeFloat forces float parsing; a non-numeric value is an error.
	TypeFloat = "float"
	// TypeBool forces boolean parsing; a value other than true/false is an error.
	TypeBool = "bool"
	// TypeJSON forces JSON parsing; invalid JSON is an error.
	TypeJSON = "json"
	// TypeCSV splits a comma-separated value into a list of trimmed strings.
	TypeCSV = "csv"
	// TypeFetch performs an SSRF-guarded HTTP(S) GET and returns the response
	// body. The value must be an http:// or https:// URL. This is the explicit
	// opt-in for network fetching; auto never fetches. For anything beyond a
	// plain GET (auth, headers, methods, retries), use the http provider.
	TypeFetch = "fetch"
)

// paramTypes lists every value accepted by the "type" input, in schema
// declaration order. It is the single source of truth for both the descriptor
// enum and runtime validation.
var paramTypes = []string{
	TypeAuto,
	TypeString,
	TypeRaw,
	TypeInt,
	TypeFloat,
	TypeBool,
	TypeJSON,
	TypeCSV,
	TypeFetch,
}

// paramTypeEnum is paramTypes converted to []any for the descriptor's enum
// option, keeping the schema enum and runtime validation from drifting apart.
var paramTypeEnum = func() []any {
	enum := make([]any, len(paramTypes))
	for i, t := range paramTypes {
		enum[i] = t
	}
	return enum
}()

// HTTPClient defines the interface for HTTP operations
type HTTPClient interface {
	Get(ctx context.Context, url string) (*http.Response, error)
}

// DefaultHTTPClient provides real HTTP operations backed by httpc.
type DefaultHTTPClient struct {
	client *httpc.Client
}

// Get performs an HTTP GET request
func (d *DefaultHTTPClient) Get(ctx context.Context, url string) (*http.Response, error) {
	return d.client.Get(ctx, url)
}

// FileOps defines the interface for file operations
type FileOps interface {
	ReadFile(path string) ([]byte, error)
}

// DefaultFileOps provides real file operations
type DefaultFileOps struct{}

// ReadFile reads a file from disk
func (d *DefaultFileOps) ReadFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

// ParameterProvider provides access to CLI parameters passed via -r/--resolver flags
type ParameterProvider struct {
	descriptor *provider.Descriptor
	httpClient HTTPClient
	fileOps    FileOps
}

// Option is a functional option for configuring ParameterProvider
type Option func(*ParameterProvider)

// WithHTTPClient sets custom HTTP client for the provider
func WithHTTPClient(client HTTPClient) Option {
	return func(p *ParameterProvider) {
		p.httpClient = client
	}
}

// WithFileOps sets custom file operations for the provider
func WithFileOps(ops FileOps) Option {
	return func(p *ParameterProvider) {
		p.fileOps = ops
	}
}

// NewParameterProvider creates a new parameter provider
func NewParameterProvider(opts ...Option) *ParameterProvider {
	version, _ := semver.NewVersion(Version)

	p := &ParameterProvider{
		descriptor: &provider.Descriptor{
			Name:        ProviderName,
			DisplayName: "CLI Parameters",
			Description: "Provider for accessing CLI parameters passed via -r/--resolver flags",
			Version:     version,
			APIVersion:  "v1",
			Category:    "system",
			Capabilities: []provider.Capability{
				provider.CapabilityFrom,
			},
			Schema: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
				"key": schemahelper.StringProp("Name of the parameter to retrieve (exact match). Provide either key or keys; key takes precedence over keys when both are set.",
					schemahelper.WithMaxLength(*ptrs.IntPtr(256)),
					schemahelper.WithPattern(`^[A-Za-z_][A-Za-z0-9_.\-]*$`),
					schemahelper.WithExample("env")),
				"keys": schemahelper.ArrayProp("Ordered list of parameter names (aliases) for a single logical parameter. The first name that was provided via CLI wins. Evaluated after key. Use this to accept a parameter under any of several flag names (e.g. environment, e, env).",
					schemahelper.WithItems(schemahelper.StringProp("Parameter name (exact match)",
						schemahelper.WithMaxLength(*ptrs.IntPtr(256)),
						schemahelper.WithPattern(`^[A-Za-z_][A-Za-z0-9_.\-]*$`))),
					schemahelper.WithMaxItems(50)),
				"default": schemahelper.AnyProp("Default value to return when the parameter is not provided via CLI. Must be a literal value -- ValueRef expressions are resolved by the executor before Execute is called, so a ValueRef default would be evaluated even when the parameter exists.",
					schemahelper.WithExample("fallback")),
				"type": schemahelper.StringProp("Controls how the parameter value is coerced. \"auto\" (default) infers booleans, numbers, JSON, and file:// sources, falling back to the literal string. \"string\" coerces to a string (stripping surrounding quotes). \"raw\" returns the value untouched. \"int\", \"float\", \"bool\", \"json\", and \"csv\" force that specific coercion and error if the value does not match. Comma-separated values are split into a string list only when type is \"csv\". http:// and https:// values are NOT fetched under \"auto\"; use \"fetch\" to perform an SSRF-guarded HTTP GET, or the http provider for anything beyond a plain GET.",
					schemahelper.WithEnum(paramTypeEnum...),
					schemahelper.WithExample(TypeString)),
			}),
			OutputSchemas: map[provider.Capability]*jsonschema.Schema{
				provider.CapabilityFrom: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
					"value":  schemahelper.AnyProp("The parameter value (typed based on parsing rules)", schemahelper.WithExample("prod")),
					"exists": schemahelper.BoolProp("Whether the value came from a CLI-provided parameter (false when using the default value)", schemahelper.WithExample(true)),
					"type":   schemahelper.StringProp("Detected type of the value", schemahelper.WithExample("string")),
				}),
			},
			Examples: []provider.Example{
				{
					Name:        "Get string parameter",
					Description: "Retrieve a string parameter from CLI",
					YAML: `provider: parameter
inputs:
  key: env`,
				},
				{
					Name:        "Get parameter with default",
					Description: "Retrieve a parameter, falling back to a default when not provided",
					YAML: `provider: parameter
inputs:
  key: env
  default: development`,
				},
				{
					Name:        "Accept a parameter under any of several names (aliases)",
					Description: "Resolve a single logical parameter from the first provided alias (first-match-wins), falling back to a default",
					YAML: `provider: parameter
inputs:
  keys: [environment, e, env]
  default: development`,
				},
				{
					Name:        "Get array parameter",
					Description: "Split a comma-separated value into a string list (opt in with type: csv)",
					YAML: `provider: parameter
inputs:
  key: regions
  type: csv`,
				},
				{
					Name:        "Get boolean parameter",
					Description: "Retrieve a boolean parameter from CLI",
					YAML: `provider: parameter
inputs:
  key: dryRun`,
				},
				{
					Name:        "Force a value to stay a string",
					Description: "Return the value as a string, suppressing inference (e.g. keep a numeric ID with leading zeros)",
					YAML: `provider: parameter
inputs:
  key: billingId
  type: string`,
				},
				{
					Name:        "Parse a value as an integer",
					Description: "Force integer parsing; a non-integer value is an error",
					YAML: `provider: parameter
inputs:
  key: port
  type: int`,
				},
				{
					Name:        "Keep a value exactly as provided",
					Description: "Return the value untouched, with no coercion or quote-stripping",
					YAML: `provider: parameter
inputs:
  key: token
  type: raw`,
				},
				{
					Name:        "Fetch remote content over HTTP",
					Description: "Perform an SSRF-guarded HTTP GET and store the response body (opt in with type: fetch). auto never fetches; a URL value stays a literal string.",
					YAML: `provider: parameter
inputs:
  key: configUrl
  type: fetch`,
				},
			},
		},
		httpClient: &DefaultHTTPClient{client: httpc.NewClient(&httpc.ClientConfig{
			Timeout:           settings.DefaultHTTPTimeout,
			RetryMax:          settings.DefaultHTTPRetryMax,
			RetryWaitMin:      settings.DefaultHTTPRetryWaitMinimum,
			RetryWaitMax:      settings.DefaultHTTPRetryWaitMaximum,
			EnableCache:       false,
			EnableCompression: true,
		})},
		fileOps: &DefaultFileOps{},
	}

	// Apply options
	for _, opt := range opts {
		opt(p)
	}

	return p
}

// Descriptor returns the provider's descriptor
func (p *ParameterProvider) Descriptor() *provider.Descriptor {
	return p.descriptor
}

// Execute retrieves a parameter from the context
func (p *ParameterProvider) Execute(ctx context.Context, input any) (*provider.Output, error) {
	lgr := logger.FromContext(ctx)

	inputs, ok := input.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: expected map[string]any, got %T", ProviderName, input)
	}

	lgr.V(1).Info("executing provider", "provider", ProviderName)

	// Build the ordered list of parameter names to look up. "key" (when set)
	// takes precedence, followed by each name in "keys" in declared order.
	// Duplicates are removed so a name is only attempted once.
	candidates, err := candidateKeys(inputs)
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("%s: at least one non-empty parameter name is required (provide key or keys)", ProviderName)
	}

	// Determine how the caller wants the value coerced (defaults to auto).
	paramType, err := resolveParamType(inputs)
	if err != nil {
		return nil, err
	}

	// Get parameters from context
	params, ok := provider.ParametersFromContext(ctx)
	if !ok {
		params = make(map[string]any)
	}

	// Check for dry-run mode
	if dryRun := provider.DryRunFromContext(ctx); dryRun {
		return p.executeDryRun(candidates[0], paramType)
	}

	// Look up the parameter, trying each candidate name in order. The first
	// name that was provided via CLI wins (first-match-wins).
	matchedKey, rawValue, exists := firstProvided(candidates, params)
	if !exists {
		if def, hasDefault := inputs["default"]; hasDefault {
			parsedDefault, err := p.resolveValue(ctx, def, paramType)
			if err != nil {
				return nil, fmt.Errorf("%s: failed to parse default for parameter %q: %w", ProviderName, candidates[0], err)
			}
			lgr.V(1).Info("provider completed", "provider", ProviderName)
			return &provider.Output{
				Data: parsedDefault,
				Metadata: map[string]any{
					"exists": false,
					"type":   detectType(parsedDefault),
				},
			}, nil
		}
		return nil, fmt.Errorf("%s: parameter %q not provided", ProviderName, candidates[0])
	}

	// Parse the value according to the requested type.
	parsedValue, err := p.resolveValue(ctx, rawValue, paramType)
	if err != nil {
		return nil, fmt.Errorf("%s: failed to parse parameter %q: %w", ProviderName, matchedKey, err)
	}

	lgr.V(1).Info("provider completed", "provider", ProviderName)
	return &provider.Output{
		Data: parsedValue,
		Metadata: map[string]any{
			"exists": true,
			"type":   detectType(parsedValue),
		},
	}, nil
}

// resolveValue returns the value for a parameter coerced according to the
// requested type. TypeAuto applies the standard inference pipeline; every other
// type performs exactly one coercion and returns a descriptive error when the
// value cannot be represented as that type.
func (p *ParameterProvider) resolveValue(ctx context.Context, value any, paramType string) (any, error) {
	switch paramType {
	case TypeString:
		return coerceString(value), nil
	case TypeRaw:
		return value, nil
	case TypeInt:
		return coerceInt(value)
	case TypeFloat:
		return coerceFloat(value)
	case TypeBool:
		return coerceBool(value)
	case TypeJSON:
		return coerceJSON(value)
	case TypeCSV:
		return coerceCSV(value), nil
	case TypeFetch:
		str, ok := value.(string)
		if !ok || !isHTTPURL(str) {
			return nil, fmt.Errorf("%s: type %q requires an http:// or https:// value, got %q (%T)", ProviderName, TypeFetch, fmt.Sprintf("%v", value), value)
		}
		return p.fetchURL(ctx, str)
	case TypeAuto:
		return p.parseValue(ctx, value)
	default:
		return nil, fmt.Errorf("%s: unsupported type %q", ProviderName, paramType)
	}
}

// unquote removes a single pair of surrounding double quotes, if present.
func unquote(s string) string {
	if isQuoted(s) {
		return s[1 : len(s)-1]
	}
	return s
}

// coerceString returns value as a string, stripping surrounding quotes from
// string inputs and formatting non-string inputs with their default
// representation.
func coerceString(value any) string {
	str, ok := value.(string)
	if !ok {
		return fmt.Sprintf("%v", value)
	}
	return unquote(str)
}

// coerceInt parses value as a 64-bit integer.
func coerceInt(value any) (any, error) {
	switch v := value.(type) {
	case string:
		n, err := strconv.ParseInt(strings.TrimSpace(unquote(v)), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("cannot parse %q as int", v)
		}
		return n, nil
	case int:
		return int64(v), nil
	case int8:
		return int64(v), nil
	case int16:
		return int64(v), nil
	case int32:
		return int64(v), nil
	case int64:
		return v, nil
	case uint:
		if uint64(v) > math.MaxInt64 {
			return nil, fmt.Errorf("cannot represent %v as int without loss", v)
		}
		return int64(v), nil
	case uint8:
		return int64(v), nil
	case uint16:
		return int64(v), nil
	case uint32:
		return int64(v), nil
	case uint64:
		if v > math.MaxInt64 {
			return nil, fmt.Errorf("cannot represent %v as int without loss", v)
		}
		return int64(v), nil
	case float64:
		// Require a whole number that round-trips through int64 unchanged.
		// The round-trip guard rejects out-of-range values (e.g. 1e300),
		// whose int64 conversion is implementation-defined.
		if v == math.Trunc(v) && float64(int64(v)) == v {
			return int64(v), nil
		}
		return nil, fmt.Errorf("cannot represent %v as int without loss", v)
	default:
		return nil, fmt.Errorf("cannot coerce %T to int", value)
	}
}

// coerceFloat parses value as a 64-bit float.
func coerceFloat(value any) (any, error) {
	switch v := value.(type) {
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(unquote(v)), 64)
		if err != nil {
			return nil, fmt.Errorf("cannot parse %q as float", v)
		}
		return f, nil
	case float64:
		return v, nil
	case float32:
		return float64(v), nil
	case int:
		return float64(v), nil
	case int8:
		return float64(v), nil
	case int16:
		return float64(v), nil
	case int32:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case uint:
		return float64(v), nil
	case uint8:
		return float64(v), nil
	case uint16:
		return float64(v), nil
	case uint32:
		return float64(v), nil
	case uint64:
		return float64(v), nil
	default:
		return nil, fmt.Errorf("cannot coerce %T to float", value)
	}
}

// coerceBool parses value as a boolean, accepting only true/false
// (case-insensitive) from strings.
func coerceBool(value any) (any, error) {
	switch v := value.(type) {
	case bool:
		return v, nil
	case string:
		switch strings.ToLower(strings.TrimSpace(unquote(v))) {
		case "true":
			return true, nil
		case "false":
			return false, nil
		}
		return nil, fmt.Errorf("cannot parse %q as bool (expected true or false)", v)
	default:
		return nil, fmt.Errorf("cannot coerce %T to bool", value)
	}
}

// coerceJSON parses a string value as JSON. Non-string values (e.g. an
// already-structured default) are returned unchanged.
func coerceJSON(value any) (any, error) {
	str, ok := value.(string)
	if !ok {
		return value, nil
	}
	str = strings.TrimSpace(str)
	var result any
	if err := json.Unmarshal([]byte(str), &result); err == nil {
		return result, nil
	}
	// Fall back to stripping surrounding quotes added by the CLI/YAML layer
	// (e.g. a quote-wrapped JSON payload like "{\"a\":1}"), matching the
	// unquote normalization the other coercions apply.
	if isQuoted(str) {
		if err := json.Unmarshal([]byte(unquote(str)), &result); err == nil {
			return result, nil
		}
	}
	return nil, fmt.Errorf("cannot parse value as JSON: %q", str)
}

// coerceCSV splits a string value on commas into a list of trimmed strings.
// Non-string values (e.g. an already-structured list default) are returned
// unchanged.
func coerceCSV(value any) any {
	str, ok := value.(string)
	if !ok {
		return value
	}
	parts := strings.Split(unquote(str), ",")
	result := make([]string, len(parts))
	for i, part := range parts {
		result[i] = strings.TrimSpace(part)
	}
	return result
}

// parseValue applies the auto inference pipeline to a parameter value. It is
// purely local (no network I/O); http(s):// values are not fetched under auto
// -- use TypeFetch for that. The context parameter is retained for signature
// symmetry with the rest of the value-resolution pipeline.
func (p *ParameterProvider) parseValue(_ context.Context, value any) (any, error) {
	// If value is already parsed (not a string), return as-is
	str, ok := value.(string)
	if !ok {
		return value, nil
	}

	// 1. Stdin check (should already be resolved at CLI init, but handle just in case)
	if str == "-" {
		return nil, fmt.Errorf("stdin value '-' should have been resolved during CLI initialization")
	}

	// 2. File protocol
	if strings.HasPrefix(str, "file://") {
		path := strings.TrimPrefix(str, "file://")
		content, err := p.fileOps.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("failed to read file %q: %w", path, err)
		}
		return string(content), nil
	}

	// 3. JSON parse
	if strings.HasPrefix(str, "{") || strings.HasPrefix(str, "[") {
		var result any
		if err := json.Unmarshal([]byte(str), &result); err == nil {
			return result, nil
		}
		// If JSON parsing fails, continue to next rule
	}

	// 4. Boolean parse
	lowerStr := strings.ToLower(str)
	if lowerStr == "true" {
		return true, nil
	}
	if lowerStr == "false" {
		return false, nil
	}

	// 5. Number parse
	// Try integer first
	if intVal, err := strconv.ParseInt(str, 10, 64); err == nil {
		return intVal, nil
	}
	// Try float
	if floatVal, err := strconv.ParseFloat(str, 64); err == nil {
		return floatVal, nil
	}

	// 6. Literal string (fallback)
	// Remove surrounding quotes if present
	if isQuoted(str) {
		return strings.Trim(str, `"`), nil
	}

	return str, nil
}

// isHTTPURL reports whether s is an http:// or https:// URL.
func isHTTPURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

// fetchURL performs an SSRF-guarded HTTP GET and returns the response body as a
// string. It backs TypeFetch; auto never fetches. Requests to
// private/loopback/link-local addresses are blocked unless explicitly permitted.
func (p *ParameterProvider) fetchURL(ctx context.Context, url string) (string, error) {
	if !httpc.PrivateIPsAllowed(ctx) {
		if err := httpc.ValidateURLNotPrivate(url); err != nil {
			return "", fmt.Errorf("resolver parameter URL blocked: %w", err)
		}
	}
	resp, err := p.httpClient.Get(ctx, url)
	if err != nil {
		return "", fmt.Errorf("failed to fetch URL %q: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP request to %q failed with status %d", url, resp.StatusCode)
	}

	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response from %q: %w", url, err)
	}
	return string(content), nil
}

// isQuoted checks if a string is surrounded by double quotes
func isQuoted(s string) bool {
	return len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"'
}

// detectType returns a string describing the type of a value
func detectType(value any) string {
	if value == nil {
		return "null"
	}

	switch value.(type) {
	case bool:
		return "boolean"
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return "integer"
	case float32, float64:
		return "float"
	case string:
		return "string"
	case []any, []string:
		return "array"
	case map[string]any:
		return "object"
	default:
		return "unknown"
	}
}

func (p *ParameterProvider) executeDryRun(key, paramType string) (*provider.Output, error) {
	return &provider.Output{
		Data: "[DRY-RUN] Not retrieved",
		Metadata: map[string]any{
			"dryRun": true,
			"key":    key,
			"exists": false,
			"type":   paramType,
		},
	}, nil
}

// resolveParamType returns the requested parameter value type from the inputs,
// defaulting to TypeAuto when "type" is absent. The provider schema constrains
// "type" to the values in paramTypes, so this mirrors that validation for
// callers that bypass schema validation (e.g. direct resolver execution).
func resolveParamType(inputs map[string]any) (string, error) {
	raw, present := inputs["type"]
	if !present {
		return TypeAuto, nil
	}
	t, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("%s: type must be a string, got %T", ProviderName, raw)
	}
	for _, valid := range paramTypes {
		if t == valid {
			return t, nil
		}
	}
	return "", fmt.Errorf("%s: unsupported type %q (valid: %s)", ProviderName, t, strings.Join(paramTypes, ", "))
}

// candidateKeys returns the ordered list of parameter names to look up for a
// single logical parameter. "key" (when a non-empty string) comes first,
// followed by each name in "keys" in declared order. Duplicates are removed so
// each name is attempted at most once. An error is returned when "key" or
// "keys" is present but not the expected type.
func candidateKeys(inputs map[string]any) ([]string, error) {
	seen := make(map[string]struct{})
	candidates := make([]string, 0, 4)

	add := func(name string) {
		if name == "" {
			return
		}
		if _, dup := seen[name]; dup {
			return
		}
		seen[name] = struct{}{}
		candidates = append(candidates, name)
	}

	if raw, present := inputs["key"]; present {
		key, ok := raw.(string)
		if !ok {
			return nil, fmt.Errorf("%s: key must be a string, got %T", ProviderName, raw)
		}
		add(key)
	}

	if raw, present := inputs["keys"]; present {
		switch list := raw.(type) {
		case []string:
			for _, name := range list {
				add(name)
			}
		case []any:
			for i, item := range list {
				name, ok := item.(string)
				if !ok {
					return nil, fmt.Errorf("%s: keys[%d] must be a string, got %T", ProviderName, i, item)
				}
				add(name)
			}
		default:
			return nil, fmt.Errorf("%s: keys must be a list of strings, got %T", ProviderName, raw)
		}
	}

	return candidates, nil
}

// firstProvided returns the first candidate name that exists in params, along
// with its raw value. The returned name is the one that matched, enabling
// accurate error messages.
func firstProvided(candidates []string, params map[string]any) (string, any, bool) {
	for _, name := range candidates {
		if v, exists := params[name]; exists {
			return name, v, true
		}
	}
	return "", nil, false
}
