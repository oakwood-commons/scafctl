package execprovider

import (
	"context"
	"runtime"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewExecProvider(t *testing.T) {
	p := NewExecProvider()

	require.NotNil(t, p)
	require.NotNil(t, p.Descriptor())

	desc := p.Descriptor()
	assert.Equal(t, "exec", desc.Name)
	assert.Equal(t, "Exec Provider", desc.DisplayName)
	assert.Equal(t, "v1", desc.APIVersion)
	assert.NotNil(t, desc.Version)
	assert.Contains(t, desc.Capabilities, provider.CapabilityAction)
	assert.Contains(t, desc.Capabilities, provider.CapabilityFrom)

	// Verify schema
	assert.NotNil(t, desc.Schema.Properties)
	assert.True(t, desc.Schema.Properties["command"].Required)
	assert.Equal(t, provider.PropertyTypeString, desc.Schema.Properties["command"].Type)
	assert.Equal(t, provider.PropertyTypeArray, desc.Schema.Properties["args"].Type)
	assert.Equal(t, provider.PropertyTypeString, desc.Schema.Properties["stdin"].Type)
	assert.Equal(t, provider.PropertyTypeString, desc.Schema.Properties["workingDir"].Type)
	assert.Equal(t, provider.PropertyTypeAny, desc.Schema.Properties["env"].Type)
	assert.Equal(t, provider.PropertyTypeInt, desc.Schema.Properties["timeout"].Type)
	assert.Equal(t, provider.PropertyTypeBool, desc.Schema.Properties["shell"].Type)

	// Verify output schema
	assert.NotNil(t, desc.OutputSchema.Properties)
	assert.Equal(t, provider.PropertyTypeString, desc.OutputSchema.Properties["stdout"].Type)
	assert.Equal(t, provider.PropertyTypeString, desc.OutputSchema.Properties["stderr"].Type)
	assert.Equal(t, provider.PropertyTypeInt, desc.OutputSchema.Properties["exitCode"].Type)
	assert.Equal(t, provider.PropertyTypeBool, desc.OutputSchema.Properties["success"].Type)
	assert.Equal(t, provider.PropertyTypeString, desc.OutputSchema.Properties["command"].Type)
}

func TestExecProvider_Execute_SimpleCommand(t *testing.T) {
	p := NewExecProvider()
	ctx := context.Background()

	inputs := map[string]any{
		"command": "echo",
		"args":    []any{"hello", "world"},
	}

	output, err := p.Execute(ctx, inputs)

	require.NoError(t, err)
	require.NotNil(t, output)

	data, ok := output.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "hello world\n", data["stdout"])
	assert.Equal(t, "", data["stderr"])
	assert.Equal(t, 0, data["exitCode"])
	assert.Equal(t, true, data["success"])
	assert.Equal(t, "echo hello world", data["command"])
}

func TestExecProvider_Execute_NoArgs(t *testing.T) {
	p := NewExecProvider()
	ctx := context.Background()

	inputs := map[string]any{
		"command": "pwd",
	}

	output, err := p.Execute(ctx, inputs)

	require.NoError(t, err)
	require.NotNil(t, output)

	data, ok := output.Data.(map[string]any)
	require.True(t, ok)
	assert.NotEmpty(t, data["stdout"])
	assert.Equal(t, "", data["stderr"])
	assert.Equal(t, 0, data["exitCode"])
	assert.Equal(t, true, data["success"])
	assert.Equal(t, "pwd", data["command"])
}

func TestExecProvider_Execute_WithStdin(t *testing.T) {
	p := NewExecProvider()
	ctx := context.Background()

	inputs := map[string]any{
		"command": "cat",
		"stdin":   "test input",
	}

	output, err := p.Execute(ctx, inputs)

	require.NoError(t, err)
	require.NotNil(t, output)

	data, ok := output.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "test input", data["stdout"])
	assert.Equal(t, "", data["stderr"])
	assert.Equal(t, 0, data["exitCode"])
	assert.Equal(t, true, data["success"])
}

func TestExecProvider_Execute_WithWorkingDir(t *testing.T) {
	p := NewExecProvider()
	ctx := context.Background()

	inputs := map[string]any{
		"command":    "pwd",
		"workingDir": "/tmp",
	}

	output, err := p.Execute(ctx, inputs)

	require.NoError(t, err)
	require.NotNil(t, output)

	data, ok := output.Data.(map[string]any)
	require.True(t, ok)
	assert.Contains(t, data["stdout"], "/tmp")
	assert.Equal(t, 0, data["exitCode"])
	assert.Equal(t, true, data["success"])
}

func TestExecProvider_Execute_WithEnv(t *testing.T) {
	p := NewExecProvider()
	ctx := context.Background()

	inputs := map[string]any{
		"command": "sh",
		"args":    []any{"-c", "echo $TEST_VAR"},
		"env": map[string]any{
			"TEST_VAR": "test_value",
		},
	}

	output, err := p.Execute(ctx, inputs)

	require.NoError(t, err)
	require.NotNil(t, output)

	data, ok := output.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "test_value\n", data["stdout"])
	assert.Equal(t, 0, data["exitCode"])
	assert.Equal(t, true, data["success"])
}

func TestExecProvider_Execute_NonZeroExitCode(t *testing.T) {
	p := NewExecProvider()
	ctx := context.Background()

	inputs := map[string]any{
		"command": "sh",
		"args":    []any{"-c", "exit 42"},
	}

	output, err := p.Execute(ctx, inputs)

	require.NoError(t, err)
	require.NotNil(t, output)

	data, ok := output.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, 42, data["exitCode"])
	assert.Equal(t, false, data["success"])
}

func TestExecProvider_Execute_StderrOutput(t *testing.T) {
	p := NewExecProvider()
	ctx := context.Background()

	inputs := map[string]any{
		"command": "sh",
		"args":    []any{"-c", "echo error message >&2"},
	}

	output, err := p.Execute(ctx, inputs)

	require.NoError(t, err)
	require.NotNil(t, output)

	data, ok := output.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "error message\n", data["stderr"])
	assert.Equal(t, "", data["stdout"])
	assert.Equal(t, 0, data["exitCode"])
	assert.Equal(t, true, data["success"])
}

func TestExecProvider_Execute_WithShellFlag(t *testing.T) {
	p := NewExecProvider()
	ctx := context.Background()

	inputs := map[string]any{
		"command": "echo",
		"args":    []any{"hello"},
		"shell":   true,
	}

	output, err := p.Execute(ctx, inputs)

	require.NoError(t, err)
	require.NotNil(t, output)

	data, ok := output.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "hello\n", data["stdout"])
	assert.Equal(t, 0, data["exitCode"])
	assert.Equal(t, true, data["success"])
}

func TestExecProvider_Execute_WithTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping timeout test on Windows")
	}

	p := NewExecProvider()
	ctx := context.Background()

	inputs := map[string]any{
		"command": "sh",
		"args":    []any{"-c", "for i in 1 2 3 4 5 6 7 8 9 10; do echo $i; sleep 1; done"},
		"timeout": 2,
	}

	output, err := p.Execute(ctx, inputs)

	// The command should either error due to timeout or exit with non-zero due to signal
	if err != nil {
		// Direct timeout error
		t.Logf("Got error as expected: %v", err)
		assert.Nil(t, output)
	} else {
		// Command was killed by signal, check exit code
		require.NotNil(t, output)
		data, ok := output.Data.(map[string]any)
		require.True(t, ok)
		exitCode := data["exitCode"].(int)
		// Killed processes typically have non-zero exit codes (often 127+ for signals)
		assert.NotEqual(t, 0, exitCode, "Expected non-zero exit code from killed process")
		t.Logf("Command killed with exit code: %v", exitCode)
	}
}

func TestExecProvider_Execute_CommandNotFound(t *testing.T) {
	p := NewExecProvider()
	ctx := context.Background()

	inputs := map[string]any{
		"command": "nonexistentcommand12345",
	}

	output, err := p.Execute(ctx, inputs)

	require.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "failed to execute command")
}

func TestExecProvider_Execute_MissingCommand(t *testing.T) {
	p := NewExecProvider()
	ctx := context.Background()

	inputs := map[string]any{}

	output, err := p.Execute(ctx, inputs)

	require.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "command is required")
}

func TestExecProvider_Execute_EmptyCommand(t *testing.T) {
	p := NewExecProvider()
	ctx := context.Background()

	inputs := map[string]any{
		"command": "",
	}

	output, err := p.Execute(ctx, inputs)

	require.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "command is required")
}

func TestExecProvider_Execute_InvalidArgs(t *testing.T) {
	p := NewExecProvider()
	ctx := context.Background()

	inputs := map[string]any{
		"command": "echo",
		"args":    "not an array",
	}

	output, err := p.Execute(ctx, inputs)

	require.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "args must be an array")
}

func TestExecProvider_Execute_InvalidEnv(t *testing.T) {
	p := NewExecProvider()
	ctx := context.Background()

	inputs := map[string]any{
		"command": "echo",
		"env":     "not an object",
	}

	output, err := p.Execute(ctx, inputs)

	require.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "env must be an object")
}

func TestExecProvider_Execute_InvalidTimeout(t *testing.T) {
	p := NewExecProvider()
	ctx := context.Background()

	inputs := map[string]any{
		"command": "echo",
		"timeout": "not a number",
	}

	output, err := p.Execute(ctx, inputs)

	require.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "timeout must be an integer")
}

func TestExecProvider_Execute_DryRun(t *testing.T) {
	p := NewExecProvider()
	ctx := provider.WithDryRun(context.Background(), true)

	inputs := map[string]any{
		"command": "echo",
		"args":    []any{"hello"},
	}

	output, err := p.Execute(ctx, inputs)

	require.NoError(t, err)
	require.NotNil(t, output)

	data, ok := output.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "", data["stdout"])
	assert.Equal(t, "", data["stderr"])
	assert.Equal(t, 0, data["exitCode"])
	assert.Equal(t, true, data["success"])
	assert.Equal(t, "echo hello", data["command"])
	assert.Equal(t, true, data["_dryRun"])
	assert.Contains(t, data["_message"], "Would execute command: echo hello")
}

func TestExecProvider_Execute_DryRun_WithWorkingDir(t *testing.T) {
	p := NewExecProvider()
	ctx := provider.WithDryRun(context.Background(), true)

	inputs := map[string]any{
		"command":    "pwd",
		"workingDir": "/tmp",
	}

	output, err := p.Execute(ctx, inputs)

	require.NoError(t, err)
	require.NotNil(t, output)

	data, ok := output.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, true, data["_dryRun"])
	assert.Contains(t, data["_message"], "Would execute command: pwd")
	assert.Contains(t, data["_message"], "in directory: /tmp")
}

func TestExecProvider_Execute_ArgsWithStrings(t *testing.T) {
	p := NewExecProvider()
	ctx := context.Background()

	inputs := map[string]any{
		"command": "echo",
		"args":    []string{"hello", "world"},
	}

	output, err := p.Execute(ctx, inputs)

	require.NoError(t, err)
	require.NotNil(t, output)

	data, ok := output.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "hello world\n", data["stdout"])
	assert.Equal(t, 0, data["exitCode"])
	assert.Equal(t, true, data["success"])
}
