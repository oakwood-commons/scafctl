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
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/schemahelper"
	"github.com/oakwood-commons/scafctl/pkg/ptrs"
)

const (
	// ProviderName is the name of the parameter provider
	ProviderName = "parameter"
	// Version is the version of the parameter provider
	Version = "1.0.0"
)

// HTTPClient defines the interface for HTTP operations
type HTTPClient interface {
	Get(ctx context.Context, url string) (*http.Response, error)
}

// DefaultHTTPClient provides real HTTP operations
type DefaultHTTPClient struct {
	client *http.Client
}

// Get performs an HTTP GET request
func (d *DefaultHTTPClient) Get(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	return d.client.Do(req)
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
			Name:         ProviderName,
			DisplayName:  "CLI Parameters",
			Description:  "Provider for accessing CLI parameters passed via -r/--resolver flags",
			Version:      version,
			APIVersion:   "v1",
			Category:     "system",
			MockBehavior: "Returns parameter value from context (same behavior in dry-run)",
			Capabilities: []provider.Capability{
				provider.CapabilityFrom,
			},
			Schema: schemahelper.ObjectSchema([]string{"key"}, map[string]*jsonschema.Schema{
				"key": schemahelper.StringProp("Name of the parameter to retrieve (exact match)",
					schemahelper.WithMaxLength(*ptrs.IntPtr(256)),
					schemahelper.WithPattern(`^[A-Za-z_][A-Za-z0-9_.\-]*$`),
					schemahelper.WithExample("env")),
			}),
			OutputSchemas: map[provider.Capability]*jsonschema.Schema{
				provider.CapabilityFrom: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
					"value":  schemahelper.AnyProp("The parameter value (typed based on parsing rules)", schemahelper.WithExample("prod")),
					"exists": schemahelper.BoolProp("Whether the parameter was provided via CLI", schemahelper.WithExample(true)),
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
			},
		},
		httpClient: &DefaultHTTPClient{client: http.DefaultClient},
		fileOps:    &DefaultFileOps{},
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

	key, ok := inputs["key"].(string)
	if !ok || key == "" {
		return nil, fmt.Errorf("%s: key is required and must be a string", ProviderName)
	}

	// Get parameters from context
	params, ok := provider.ParametersFromContext(ctx)
	if !ok {
		params = make(map[string]any)
	}

	// Check for dry-run mode
	if dryRun := provider.DryRunFromContext(ctx); dryRun {
		return p.executeDryRun(key, inputs)
	}

	// Look up the parameter
	rawValue, exists := params[key]
	if !exists {
		return nil, fmt.Errorf("%s: parameter %q not provided", ProviderName, key)
	}

	// Parse the value according to precedence rules
	parsedValue, err := p.parseValue(ctx, rawValue)
	if err != nil {
		return nil, fmt.Errorf("%s: failed to parse parameter %q: %w", ProviderName, key, err)
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

func (p *ParameterProvider) executeDryRun(key string, _ map[string]any) (*provider.Output, error) {
	return &provider.Output{
		Data: "[DRY-RUN] Not retrieved",
		Metadata: map[string]any{
			"dryRun": true,
			"key":    key,
			"exists": false,
			"type":   "unknown",
		},
	}, nil
}
