// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package flags

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAddKvxOutputFlags(t *testing.T) {
	var outputFormat string
	var interactive bool
	var expression string

	cmd := &cobra.Command{Use: "test"}
	AddKvxOutputFlags(cmd, &outputFormat, &interactive, &expression)

	// Check flags were added
	outputFlag := cmd.Flags().Lookup("output")
	require.NotNil(t, outputFlag)
	assert.Equal(t, "o", outputFlag.Shorthand)
	assert.Equal(t, "auto", outputFlag.DefValue)

	interactiveFlag := cmd.Flags().Lookup("interactive")
	require.NotNil(t, interactiveFlag)
	assert.Equal(t, "i", interactiveFlag.Shorthand)
	assert.Equal(t, "false", interactiveFlag.DefValue)

	expressionFlag := cmd.Flags().Lookup("expression")
	require.NotNil(t, expressionFlag)
	assert.Equal(t, "e", expressionFlag.Shorthand)
	assert.Empty(t, expressionFlag.DefValue)
}

func TestAddKvxOutputFlagsToStruct(t *testing.T) {
	flags := &KvxOutputFlags{}

	cmd := &cobra.Command{Use: "test"}
	AddKvxOutputFlagsToStruct(cmd, flags)

	// Simulate flag parsing
	err := cmd.ParseFlags([]string{"-o", "json", "-i", "-e", "_.name"})
	require.NoError(t, err)

	assert.Equal(t, "json", flags.Output)
	assert.True(t, flags.Interactive)
	assert.Equal(t, "_.name", flags.Expression)
}

func TestValidateKvxOutputFormat(t *testing.T) {
	tests := []struct {
		format  string
		wantErr bool
	}{
		{"", false},        // Empty defaults to auto
		{"auto", false},    //
		{"table", false},   //
		{"list", false},    //
		{"json", false},    //
		{"yaml", false},    //
		{"quiet", false},   //
		{"invalid", true},  //
		{"JSON", true},     // Case sensitive
		{"Table", true},    //
		{"  json  ", true}, // No whitespace trimming
	}

	for _, tt := range tests {
		t.Run(tt.format, func(t *testing.T) {
			err := ValidateKvxOutputFormat(tt.format)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "invalid output format")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestToKvxOutputOptions(t *testing.T) {
	flags := &KvxOutputFlags{
		Output:      "json",
		Interactive: true,
		Expression:  "_.items",
	}

	opts := ToKvxOutputOptions(flags)

	assert.Equal(t, kvx.OutputFormatJSON, opts.Format)
	assert.True(t, opts.Interactive)
	assert.Equal(t, "_.items", opts.Expression)
	assert.True(t, opts.PrettyPrint) // Default
}

func TestToKvxOutputOptions_AppNameFromFlags(t *testing.T) {
	flags := &KvxOutputFlags{
		Output:  "table",
		AppName: "mycli",
	}

	opts := ToKvxOutputOptions(flags)

	assert.Equal(t, "mycli", opts.AppName)
}

func TestToKvxOutputOptions_AppNameOverriddenByOption(t *testing.T) {
	flags := &KvxOutputFlags{
		Output:  "table",
		AppName: "mycli",
	}

	opts := ToKvxOutputOptions(flags,
		kvx.WithOutputAppName("mycli run solution"),
	)

	assert.Equal(t, "mycli run solution", opts.AppName)
}

func TestToKvxOutputOptions_WithOptions(t *testing.T) {
	flags := &KvxOutputFlags{
		Output:      "yaml",
		Interactive: false,
		Expression:  "",
	}

	opts := ToKvxOutputOptions(flags,
		kvx.WithOutputNoColor(true),
		kvx.WithOutputAppName("test-app"),
	)

	assert.Equal(t, kvx.OutputFormatYAML, opts.Format)
	assert.False(t, opts.Interactive)
	assert.True(t, opts.NoColor)
	assert.Equal(t, "test-app", opts.AppName)
}

func TestToKvxOutputOptions_InvalidFormat(t *testing.T) {
	flags := &KvxOutputFlags{
		Output: "invalid",
	}

	opts := ToKvxOutputOptions(flags)

	// Invalid formats should default to auto
	assert.Equal(t, kvx.OutputFormatAuto, opts.Format)
}

func TestNewKvxOutputOptionsFromFlags(t *testing.T) {
	opts := NewKvxOutputOptionsFromFlags(
		"json",
		true,
		"_.name",
		kvx.WithOutputNoColor(true),
		kvx.WithOutputAppName("my-app"),
	)

	assert.Equal(t, kvx.OutputFormatJSON, opts.Format)
	assert.True(t, opts.Interactive)
	assert.Equal(t, "_.name", opts.Expression)
	assert.True(t, opts.NoColor)
	assert.Equal(t, "my-app", opts.AppName)
	assert.True(t, opts.PrettyPrint) // Default
}

func TestNewKvxOutputOptionsFromFlags_AllFormats(t *testing.T) {
	tests := []struct {
		format   string
		expected kvx.OutputFormat
	}{
		{"auto", kvx.OutputFormatAuto},
		{"table", kvx.OutputFormatTable},
		{"list", kvx.OutputFormatList},
		{"json", kvx.OutputFormatJSON},
		{"yaml", kvx.OutputFormatYAML},
		{"quiet", kvx.OutputFormatQuiet},
		{"", kvx.OutputFormatAuto}, // Empty defaults to auto
	}

	for _, tt := range tests {
		t.Run(tt.format, func(t *testing.T) {
			opts := NewKvxOutputOptionsFromFlags(tt.format, false, "")
			assert.Equal(t, tt.expected, opts.Format)
		})
	}
}

func TestKvxOutputFlags_Zero_Value(t *testing.T) {
	flags := KvxOutputFlags{}

	assert.Empty(t, flags.Output)
	assert.False(t, flags.Interactive)
	assert.Empty(t, flags.Expression)

	// Converting zero value flags should result in auto format
	opts := ToKvxOutputOptions(&flags)
	assert.Equal(t, kvx.OutputFormatAuto, opts.Format)
}

func TestAddKvxOutputFlags_PreRunE_RejectsInvalid(t *testing.T) {
	t.Parallel()
	f := &KvxOutputFlags{}
	cmd := &cobra.Command{Use: "test", RunE: func(_ *cobra.Command, _ []string) error { return nil }}
	AddKvxOutputFlagsToStruct(cmd, f)

	cmd.SetArgs([]string{"-o", "bogus"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid output format")
}

func TestAddKvxOutputFlags_PreRunE_AcceptsValid(t *testing.T) {
	t.Parallel()
	f := &KvxOutputFlags{}
	ran := false
	cmd := &cobra.Command{Use: "test", RunE: func(_ *cobra.Command, _ []string) error {
		ran = true
		return nil
	}}
	AddKvxOutputFlagsToStruct(cmd, f)

	cmd.SetArgs([]string{"-o", "json"})
	err := cmd.Execute()
	require.NoError(t, err)
	assert.True(t, ran)
}

func TestAddKvxOutputFlags_PreRunE_ChainsExisting(t *testing.T) {
	t.Parallel()
	f := &KvxOutputFlags{}
	preRan := false
	cmd := &cobra.Command{
		Use:     "test",
		PreRunE: func(_ *cobra.Command, _ []string) error { preRan = true; return nil },
		RunE:    func(_ *cobra.Command, _ []string) error { return nil },
	}
	AddKvxOutputFlagsToStruct(cmd, f)

	cmd.SetArgs([]string{"-o", "yaml"})
	err := cmd.Execute()
	require.NoError(t, err)
	assert.True(t, preRan, "existing PreRunE should have been called")
}

func TestAddKvxOutputFlags_PreRunE_ChainsExistingPreRun(t *testing.T) {
	t.Parallel()
	f := &KvxOutputFlags{}
	preRan := false
	cmd := &cobra.Command{
		Use:    "test",
		PreRun: func(_ *cobra.Command, _ []string) { preRan = true },
		RunE:   func(_ *cobra.Command, _ []string) error { return nil },
	}
	AddKvxOutputFlagsToStruct(cmd, f)

	cmd.SetArgs([]string{"-o", "json"})
	err := cmd.Execute()
	require.NoError(t, err)
	assert.True(t, preRan, "existing PreRun should have been called via chained PreRunE")
}

func TestToKvxOutputOptionsFromCmd_ExplicitFormat(t *testing.T) {
	t.Parallel()
	f := &KvxOutputFlags{}
	cmd := &cobra.Command{
		Use:  "test",
		RunE: func(_ *cobra.Command, _ []string) error { return nil },
	}
	AddKvxOutputFlagsToStruct(cmd, f)

	// Simulate user passing -o json
	cmd.SetArgs([]string{"-o", "json"})
	err := cmd.Execute()
	require.NoError(t, err)

	opts := ToKvxOutputOptionsFromCmd(cmd, f)
	assert.True(t, opts.FormatExplicit, "should be explicit when -o was passed")
	assert.Equal(t, kvx.OutputFormatJSON, opts.Format)
}

func TestToKvxOutputOptionsFromCmd_DefaultFormat(t *testing.T) {
	t.Parallel()
	f := &KvxOutputFlags{}
	cmd := &cobra.Command{
		Use:  "test",
		RunE: func(_ *cobra.Command, _ []string) error { return nil },
	}
	AddKvxOutputFlagsToStruct(cmd, f)

	// No -o flag passed
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	require.NoError(t, err)

	opts := ToKvxOutputOptionsFromCmd(cmd, f)
	assert.False(t, opts.FormatExplicit, "should not be explicit when -o was not passed")
	assert.Equal(t, kvx.OutputFormatAuto, opts.Format)
}
