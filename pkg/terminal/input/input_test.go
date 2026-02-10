// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package input

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// NewTestInput creates an Input instance for testing with pre-configured responses.
func NewTestInput(responses ...string) *Input {
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()

	return &Input{
		ioStreams:     ioStreams,
		cliParams:     cliParams,
		testMode:      true,
		testResponses: responses,
		testIndex:     0,
	}
}

func TestConfirm_YesResponse(t *testing.T) {
	input := NewTestInput("yes")
	opts := NewConfirmOptions().WithPrompt("Delete file?")
	confirmed, err := input.Confirm(opts)
	require.NoError(t, err)
	assert.True(t, confirmed)
}

func TestConfirm_NoResponse(t *testing.T) {
	input := NewTestInput("no")
	opts := NewConfirmOptions().WithPrompt("Delete file?")
	confirmed, err := input.Confirm(opts)
	require.NoError(t, err)
	assert.False(t, confirmed)
}

func TestConfirm_EmptyUsesDefault(t *testing.T) {
	tests := []struct {
		name     string
		response string
		defVal   bool
		expected bool
	}{
		{"empty with default true", "", true, true},
		{"empty with default false", "", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := NewTestInput(tt.response)
			opts := NewConfirmOptions().WithPrompt("Continue?").WithDefault(tt.defVal)
			confirmed, err := input.Confirm(opts)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, confirmed)
		})
	}
}

func TestConfirm_QuietMode(t *testing.T) {
	input := NewTestInput()
	input.cliParams.IsQuiet = true
	opts := NewConfirmOptions().WithDefault(true)
	confirmed, err := input.Confirm(opts)
	require.NoError(t, err)
	assert.True(t, confirmed)
}

func TestConfirm_SkipCondition(t *testing.T) {
	input := NewTestInput()
	opts := NewConfirmOptions().WithDefault(false).WithSkipCondition(true)
	confirmed, err := input.Confirm(opts)
	require.NoError(t, err)
	assert.False(t, confirmed)
}

func TestReadPassword_Simple(t *testing.T) {
	input := NewTestInput("mypassword")
	opts := NewPasswordOptions().WithPrompt("Enter password: ")
	password, err := input.ReadPassword(opts)
	require.NoError(t, err)
	assert.Equal(t, "mypassword", password)
}

func TestReadPassword_WithConfirmation(t *testing.T) {
	t.Run("matching passwords", func(t *testing.T) {
		input := NewTestInput("mypassword", "mypassword")
		opts := NewPasswordOptions().WithConfirmation(true)
		password, err := input.ReadPassword(opts)
		require.NoError(t, err)
		assert.Equal(t, "mypassword", password)
	})

	t.Run("non-matching passwords", func(t *testing.T) {
		input := NewTestInput("password1", "password2")
		opts := NewPasswordOptions().WithConfirmation(true)
		_, err := input.ReadPassword(opts)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "passwords do not match")
	})
}

func TestReadPassword_MinLength(t *testing.T) {
	t.Run("meets minimum", func(t *testing.T) {
		input := NewTestInput("password123")
		opts := NewPasswordOptions().WithMinLength(8)
		password, err := input.ReadPassword(opts)
		require.NoError(t, err)
		assert.Equal(t, "password123", password)
	})

	t.Run("below minimum", func(t *testing.T) {
		input := NewTestInput("pass")
		opts := NewPasswordOptions().WithMinLength(8)
		_, err := input.ReadPassword(opts)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must be at least 8 characters")
	})
}

func TestReadLine_Simple(t *testing.T) {
	input := NewTestInput("hello world")
	opts := NewLineOptions().WithPrompt("Enter value: ")
	line, err := input.ReadLine(opts)
	require.NoError(t, err)
	assert.Equal(t, "hello world", line)
}

func TestReadLine_WithDefault(t *testing.T) {
	t.Run("empty input uses default", func(t *testing.T) {
		input := NewTestInput("")
		opts := NewLineOptions().WithPrompt("Enter name: ").WithDefault("John")
		line, err := input.ReadLine(opts)
		require.NoError(t, err)
		assert.Equal(t, "John", line)
	})
}

func TestContext(t *testing.T) {
	t.Run("WithInput and FromContext", func(t *testing.T) {
		input := NewTestInput()
		ctx := WithInput(context.Background(), input)
		retrieved := FromContext(ctx)
		assert.NotNil(t, retrieved)
		assert.Equal(t, input, retrieved)
	})

	t.Run("FromContext with no Input returns nil", func(t *testing.T) {
		retrieved := FromContext(context.Background())
		assert.Nil(t, retrieved)
	})

	t.Run("MustFromContext panics with no Input", func(t *testing.T) {
		assert.Panics(t, func() {
			MustFromContext(context.Background())
		})
	})
}

func TestNew(t *testing.T) {
	ioStreams := &terminal.IOStreams{
		In:     io.NopCloser(bytes.NewReader([]byte{})),
		Out:    &bytes.Buffer{},
		ErrOut: &bytes.Buffer{},
	}
	cliParams := settings.NewCliParams()
	input := New(ioStreams, cliParams)
	assert.NotNil(t, input)
	assert.Equal(t, ioStreams, input.ioStreams)
	assert.Equal(t, cliParams, input.cliParams)
	assert.False(t, input.testMode)
}
