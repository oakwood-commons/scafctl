// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package parameterprovider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	// TypeString is the value for the "type" input that forces a parameter
	// value to be returned verbatim as a string, suppressing type inference.
	TypeString = "string"
)

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
				"type": schemahelper.StringProp("Force the parameter value to be returned verbatim as a string, suppressing automatic type inference (boolean, number, JSON, CSV, file://, http://). The only supported value is \"string\".",
					schemahelper.WithEnum(TypeString),
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
					Description: "Retrieve a comma-separated list as an array",
					YAML: `provider: parameter
inputs:
  key: regions`,
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
					Description: "Return the value verbatim, suppressing type inference (e.g. keep a numeric ID as a string)",
					YAML: `provider: parameter
inputs:
  key: billingId
  type: string`,
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

	// Determine whether the caller wants to suppress type coercion and keep
	// the value verbatim as a string.
	forceString := wantsStringType(inputs)

	// Get parameters from context
	params, ok := provider.ParametersFromContext(ctx)
	if !ok {
		params = make(map[string]any)
	}

	// Check for dry-run mode
	if dryRun := provider.DryRunFromContext(ctx); dryRun {
		return p.executeDryRun(candidates[0], forceString)
	}

	// Look up the parameter, trying each candidate name in order. The first
	// name that was provided via CLI wins (first-match-wins).
	matchedKey, rawValue, exists := firstProvided(candidates, params)
	if !exists {
		if def, hasDefault := inputs["default"]; hasDefault {
			parsedDefault, err := p.resolveValue(ctx, def, forceString)
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

	// Parse the value according to precedence rules
	parsedValue, err := p.resolveValue(ctx, rawValue, forceString)
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

// resolveValue returns the value for a parameter. When forceString is true the
// value is returned verbatim as a string (with any surrounding double quotes
// removed) and all type-inference rules are skipped; non-string inputs are
// coerced to their string representation so the output type stays consistent
// with the requested type: string. Otherwise the standard parsing precedence
// rules are applied.
func (p *ParameterProvider) resolveValue(ctx context.Context, value any, forceString bool) (any, error) {
	if forceString {
		str, ok := value.(string)
		if !ok {
			return fmt.Sprintf("%v", value), nil
		}
		if isQuoted(str) {
			return strings.Trim(str, `"`), nil
		}
		return str, nil
	}
	return p.parseValue(ctx, value)
}

// parseValue applies the parsing precedence rules to a parameter value
func (p *ParameterProvider) parseValue(ctx context.Context, value any) (any, error) {
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

	// 3. HTTP protocol
	if strings.HasPrefix(str, "http://") || strings.HasPrefix(str, "https://") {
		// Block requests to private/loopback/link-local IP addresses unless explicitly permitted.
		if !httpc.PrivateIPsAllowed(ctx) {
			if err := httpc.ValidateURLNotPrivate(str); err != nil {
				return nil, fmt.Errorf("resolver parameter URL blocked: %w", err)
			}
		}
		resp, err := p.httpClient.Get(ctx, str)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch URL %q: %w", str, err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("HTTP request to %q failed with status %d", str, resp.StatusCode)
		}

		content, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read response from %q: %w", str, err)
		}
		return string(content), nil
	}

	// 4. JSON parse
	if strings.HasPrefix(str, "{") || strings.HasPrefix(str, "[") {
		var result any
		if err := json.Unmarshal([]byte(str), &result); err == nil {
			return result, nil
		}
		// If JSON parsing fails, continue to next rule
	}

	// 5. Boolean parse
	lowerStr := strings.ToLower(str)
	if lowerStr == "true" {
		return true, nil
	}
	if lowerStr == "false" {
		return false, nil
	}

	// 6. Number parse
	// Try integer first
	if intVal, err := strconv.ParseInt(str, 10, 64); err == nil {
		return intVal, nil
	}
	// Try float
	if floatVal, err := strconv.ParseFloat(str, 64); err == nil {
		return floatVal, nil
	}

	// 7. CSV detection (no surrounding quotes and contains comma)
	if strings.Contains(str, ",") && !isQuoted(str) {
		parts := strings.Split(str, ",")
		result := make([]string, len(parts))
		for i, part := range parts {
			result[i] = strings.TrimSpace(part)
		}
		return result, nil
	}

	// 8. Literal string (fallback)
	// Remove surrounding quotes if present
	if isQuoted(str) {
		return strings.Trim(str, `"`), nil
	}

	return str, nil
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

func (p *ParameterProvider) executeDryRun(key string, forceString bool) (*provider.Output, error) {
	typeName := "unknown"
	if forceString {
		typeName = "string"
	}
	return &provider.Output{
		Data: "[DRY-RUN] Not retrieved",
		Metadata: map[string]any{
			"dryRun": true,
			"key":    key,
			"exists": false,
			"type":   typeName,
		},
	}, nil
}

// wantsStringType reports whether the inputs request that the parameter value
// be returned verbatim as a string (type: string), suppressing type inference.
// The provider schema constrains the "type" input to the exact value
// TypeString, so the comparison is an exact, case-sensitive match to mirror
// what the executor validates before calling Execute. Surrounding whitespace is
// not trimmed, so callers that bypass schema validation (e.g. direct resolver
// execution) stay consistent with the descriptor's declared enum.
func wantsStringType(inputs map[string]any) bool {
	t, ok := inputs["type"].(string)
	if !ok {
		return false
	}
	return t == TypeString
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
