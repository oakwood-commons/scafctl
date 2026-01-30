package validate_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/flags/validate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateJSON(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:    "valid object",
			input:   `{"key":"value"}`,
			wantErr: false,
		},
		{
			name:    "valid array",
			input:   `[1,2,3]`,
			wantErr: false,
		},
		{
			name:    "valid nested",
			input:   `{"user":{"name":"Alice","age":30}}`,
			wantErr: false,
		},
		{
			name:    "invalid json",
			input:   `{key:value}`,
			wantErr: true,
		},
		{
			name:    "incomplete json",
			input:   `{"key":"value"`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validate.ValidateJSON(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateYAML(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:    "valid yaml",
			input:   "key: value",
			wantErr: false,
		},
		{
			name:    "valid array",
			input:   "items:\n  - a\n  - b",
			wantErr: false,
		},
		{
			name:    "valid inline array",
			input:   "items: [a, b, c]",
			wantErr: false,
		},
		{
			name:    "invalid yaml - tabs",
			input:   "key: value\n\tbad: tabs",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validate.ValidateYAML(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateBase64(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:    "valid base64 with padding",
			input:   "SGVsbG8sIFdvcmxkIQ==",
			wantErr: false,
		},
		{
			name:    "valid base64 properly padded",
			input:   "SGVsbG8=",
			wantErr: false,
		},
		{
			name:    "invalid characters",
			input:   "Hello, World!",
			wantErr: true,
		},
		{
			name:    "completely invalid",
			input:   "not@valid#base64$",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validate.ValidateBase64(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateFile(t *testing.T) {
	// Create a temporary file for testing
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.txt")
	err := os.WriteFile(tmpFile, []byte("test content"), 0o600)
	require.NoError(t, err)

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{
			name:    "valid existing file",
			path:    tmpFile,
			wantErr: false,
		},
		{
			name:    "non-existent file",
			path:    filepath.Join(tmpDir, "nonexistent.txt"),
			wantErr: true,
		},
		{
			name:    "directory instead of file",
			path:    tmpDir,
			wantErr: true,
		},
		{
			name:    "empty path",
			path:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validate.ValidateFile(tt.path)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateURL(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:    "valid http url",
			input:   "http://example.com",
			wantErr: false,
		},
		{
			name:    "valid https url",
			input:   "https://example.com/path?query=value",
			wantErr: false,
		},
		{
			name:    "invalid scheme",
			input:   "ftp://example.com",
			wantErr: true,
		},
		{
			name:    "missing host",
			input:   "http://",
			wantErr: true,
		},
		{
			name:    "malformed url",
			input:   "ht!tp://example.com",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validate.ValidateURL(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateValue(t *testing.T) {
	// Create a temporary file for file:// tests
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.txt")
	err := os.WriteFile(tmpFile, []byte("test"), 0o600)
	require.NoError(t, err)

	tests := []struct {
		name        string
		key         string
		value       string
		wantValue   string
		wantErr     bool
		errContains string
	}{
		{
			name:      "valid json scheme",
			key:       "config",
			value:     `json://{"key":"value"}`,
			wantValue: `json://{"key":"value"}`,
			wantErr:   false,
		},
		{
			name:        "invalid json scheme",
			key:         "config",
			value:       `json://{key:value}`,
			wantErr:     true,
			errContains: "invalid JSON",
		},
		{
			name:      "valid yaml scheme",
			key:       "config",
			value:     `yaml://key: value`,
			wantValue: `yaml://key: value`,
			wantErr:   false,
		},
		{
			name:      "valid base64 scheme",
			key:       "token",
			value:     `base64://SGVsbG8=`,
			wantValue: `base64://SGVsbG8=`,
			wantErr:   false,
		},
		{
			name:      "valid file scheme",
			key:       "path",
			value:     "file://" + tmpFile,
			wantValue: "file://" + tmpFile,
			wantErr:   false,
		},
		{
			name:        "invalid file scheme - nonexistent",
			key:         "path",
			value:       "file:///nonexistent/file.txt",
			wantErr:     true,
			errContains: "does not exist",
		},
		{
			name:      "valid http scheme",
			key:       "url",
			value:     `http://example.com`,
			wantValue: `http://example.com`,
			wantErr:   false,
		},
		{
			name:      "valid https scheme",
			key:       "url",
			value:     `https://example.com/path`,
			wantValue: `https://example.com/path`,
			wantErr:   false,
		},
		{
			name:      "no scheme - no validation",
			key:       "name",
			value:     `just a regular value`,
			wantValue: `just a regular value`,
			wantErr:   false,
		},
		{
			name:      "unknown scheme - no validation",
			key:       "data",
			value:     `custom://something`,
			wantValue: `custom://something`,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := validate.ValidateValue(tt.key, tt.value)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantValue, got)
		})
	}
}

func TestValidateAll(t *testing.T) {
	// Create a temporary file for file:// tests
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.txt")
	err := os.WriteFile(tmpFile, []byte("test"), 0o600)
	require.NoError(t, err)

	tests := []struct {
		name        string
		input       map[string][]string
		want        map[string][]string
		wantErr     bool
		errContains string
	}{
		{
			name: "all valid",
			input: map[string][]string{
				"config": {`json://{"key":"value"}`},
				"data":   {`yaml://key: value`},
				"token":  {`base64://SGVsbG8=`},
			},
			want: map[string][]string{
				"config": {`json://{"key":"value"}`},
				"data":   {`yaml://key: value`},
				"token":  {`base64://SGVsbG8=`},
			},
			wantErr: false,
		},
		{
			name: "multiple values same key",
			input: map[string][]string{
				"config": {
					`json://[1,2,3]`,
					`json://{"a":1}`,
				},
			},
			want: map[string][]string{
				"config": {
					`json://[1,2,3]`,
					`json://{"a":1}`,
				},
			},
			wantErr: false,
		},
		{
			name: "invalid json",
			input: map[string][]string{
				"config": {`json://{invalid}`},
			},
			wantErr:     true,
			errContains: "invalid JSON",
		},
		{
			name: "mixed valid and no scheme",
			input: map[string][]string{
				"config": {`json://{"key":"value"}`},
				"name":   {"Alice"},
				"env":    {"prod"},
			},
			want: map[string][]string{
				"config": {`json://{"key":"value"}`},
				"name":   {"Alice"},
				"env":    {"prod"},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := validate.ValidateAll(tt.input)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
