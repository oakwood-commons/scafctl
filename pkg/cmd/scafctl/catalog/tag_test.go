package catalog

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateAlias(t *testing.T) {
	tests := []struct {
		name    string
		alias   string
		wantErr string
	}{
		{
			name:  "valid alias - stable",
			alias: "stable",
		},
		{
			name:  "valid alias - latest",
			alias: "latest",
		},
		{
			name:  "valid alias - production",
			alias: "production",
		},
		{
			name:  "valid alias - with dots",
			alias: "v1.release",
		},
		{
			name:  "valid alias - with hyphens",
			alias: "pre-release",
		},
		{
			name:  "valid alias - with underscores",
			alias: "staging_v2",
		},
		{
			name:  "valid alias - uppercase",
			alias: "STABLE",
		},
		{
			name:    "empty alias",
			alias:   "",
			wantErr: "cannot be empty",
		},
		{
			name:    "semver version - rejected",
			alias:   "1.0.0",
			wantErr: "looks like a semver version",
		},
		{
			name:    "semver with v prefix - rejected",
			alias:   "1.2.3-alpha.1",
			wantErr: "looks like a semver version",
		},
		{
			name:    "contains slash",
			alias:   "foo/bar",
			wantErr: "invalid character",
		},
		{
			name:    "contains space",
			alias:   "foo bar",
			wantErr: "invalid character",
		},
		{
			name:    "contains colon",
			alias:   "foo:bar",
			wantErr: "invalid character",
		},
		{
			name:    "starts with dot",
			alias:   ".hidden",
			wantErr: "must start with",
		},
		{
			name:    "starts with hyphen",
			alias:   "-flag",
			wantErr: "must start with",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateAlias(tt.alias)
			if tt.wantErr != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestIsValidTagChar(t *testing.T) {
	valid := []rune{'a', 'z', 'A', 'Z', '0', '9', '_', '.', '-'}
	for _, ch := range valid {
		assert.True(t, isValidTagChar(ch), "expected %q to be valid", string(ch))
	}

	invalid := []rune{'/', ':', ' ', '@', '#', '!', '(', ')'}
	for _, ch := range invalid {
		assert.False(t, isValidTagChar(ch), "expected %q to be invalid", string(ch))
	}
}
