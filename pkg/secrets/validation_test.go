package secrets

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateName(t *testing.T) {
	tests := []struct {
		name        string
		secretName  string
		wantErr     bool
		errContains string
	}{
		// Valid names
		{
			name:       "simple lowercase",
			secretName: "mysecret",
			wantErr:    false,
		},
		{
			name:       "simple uppercase",
			secretName: "MYSECRET",
			wantErr:    false,
		},
		{
			name:       "mixed case",
			secretName: "MySecret",
			wantErr:    false,
		},
		{
			name:       "with numbers",
			secretName: "secret123",
			wantErr:    false,
		},
		{
			name:       "starts with number",
			secretName: "123secret",
			wantErr:    false,
		},
		{
			name:       "with dashes",
			secretName: "my-secret-name",
			wantErr:    false,
		},
		{
			name:       "with underscores",
			secretName: "my_secret_name",
			wantErr:    false,
		},
		{
			name:       "with dots",
			secretName: "my.secret.name",
			wantErr:    false,
		},
		{
			name:       "mixed separators",
			secretName: "my-secret_name.v2",
			wantErr:    false,
		},
		{
			name:       "single character",
			secretName: "a",
			wantErr:    false,
		},
		{
			name:       "single number",
			secretName: "1",
			wantErr:    false,
		},
		{
			name:       "max length (255 chars)",
			secretName: strings.Repeat("a", 255),
			wantErr:    false,
		},

		// Invalid names
		{
			name:        "empty name",
			secretName:  "",
			wantErr:     true,
			errContains: "cannot be empty",
		},
		{
			name:        "starts with dot",
			secretName:  ".hidden",
			wantErr:     true,
			errContains: "cannot start with '.'",
		},
		{
			name:        "starts with dash",
			secretName:  "-invalid",
			wantErr:     true,
			errContains: "cannot start with '-'",
		},
		{
			name:        "contains double dots",
			secretName:  "my..secret",
			wantErr:     true,
			errContains: "cannot contain '..'",
		},
		{
			name:        "exceeds max length",
			secretName:  strings.Repeat("a", 256),
			wantErr:     true,
			errContains: "exceeds maximum length",
		},
		{
			name:        "contains space",
			secretName:  "my secret",
			wantErr:     true,
			errContains: "invalid characters",
		},
		{
			name:        "contains slash",
			secretName:  "my/secret",
			wantErr:     true,
			errContains: "invalid characters",
		},
		{
			name:        "contains backslash",
			secretName:  "my\\secret",
			wantErr:     true,
			errContains: "invalid characters",
		},
		{
			name:        "contains colon",
			secretName:  "my:secret",
			wantErr:     true,
			errContains: "invalid characters",
		},
		{
			name:        "contains asterisk",
			secretName:  "my*secret",
			wantErr:     true,
			errContains: "invalid characters",
		},
		{
			name:        "contains question mark",
			secretName:  "my?secret",
			wantErr:     true,
			errContains: "invalid characters",
		},
		{
			name:        "contains angle brackets",
			secretName:  "my<secret>",
			wantErr:     true,
			errContains: "invalid characters",
		},
		{
			name:        "contains pipe",
			secretName:  "my|secret",
			wantErr:     true,
			errContains: "invalid characters",
		},
		{
			name:        "contains newline",
			secretName:  "my\nsecret",
			wantErr:     true,
			errContains: "invalid characters",
		},
		{
			name:        "contains tab",
			secretName:  "my\tsecret",
			wantErr:     true,
			errContains: "invalid characters",
		},
		{
			name:        "contains unicode",
			secretName:  "my🔐secret",
			wantErr:     true,
			errContains: "invalid characters",
		},
		{
			name:        "only dot",
			secretName:  ".",
			wantErr:     true,
			errContains: "cannot start with '.'",
		},
		{
			name:        "only dash",
			secretName:  "-",
			wantErr:     true,
			errContains: "cannot start with '-'",
		},
		{
			name:        "ends with double dots",
			secretName:  "secret..",
			wantErr:     true,
			errContains: "cannot contain '..'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateName(tt.secretName)

			if tt.wantErr {
				require.Error(t, err)
				assert.True(t, errors.Is(err, ErrInvalidName), "error should be ErrInvalidName")
				assert.Contains(t, err.Error(), tt.errContains)

				// Verify InvalidNameError type
				var invalidNameErr *InvalidNameError
				require.True(t, errors.As(err, &invalidNameErr))
				assert.Equal(t, tt.secretName, invalidNameErr.Name)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestIsValidName(t *testing.T) {
	tests := []struct {
		name       string
		secretName string
		want       bool
	}{
		{
			name:       "valid name",
			secretName: "my-secret",
			want:       true,
		},
		{
			name:       "invalid name - starts with dot",
			secretName: ".hidden",
			want:       false,
		},
		{
			name:       "invalid name - empty",
			secretName: "",
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsValidName(tt.secretName)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestInvalidNameError(t *testing.T) {
	t.Run("Error message format", func(t *testing.T) {
		err := NewInvalidNameError(".hidden", "cannot start with '.'")
		assert.Equal(t, `invalid secret name ".hidden": cannot start with '.'`, err.Error())
	})

	t.Run("Is ErrInvalidName", func(t *testing.T) {
		err := NewInvalidNameError("test", "reason")
		assert.True(t, errors.Is(err, ErrInvalidName))
	})

	t.Run("errors.As works correctly", func(t *testing.T) {
		err := ValidateName(".hidden")
		var invalidNameErr *InvalidNameError
		require.True(t, errors.As(err, &invalidNameErr))
		assert.Equal(t, ".hidden", invalidNameErr.Name)
		assert.Equal(t, "name cannot start with '.'", invalidNameErr.Reason)
	})
}
