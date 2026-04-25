// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package execprovider

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

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
	assert.Contains(t, desc.Capabilities, provider.CapabilityTransform)

	// Verify schema
	assert.NotNil(t, desc.Schema)
	assert.NotNil(t, desc.Schema.Properties)
	assert.Contains(t, desc.Schema.Required, "command")
	assert.Equal(t, "string", desc.Schema.Properties["command"].Type)
	assert.Equal(t, "array", desc.Schema.Properties["args"].Type)
	assert.Equal(t, "string", desc.Schema.Properties["stdin"].Type)
	assert.Equal(t, "string", desc.Schema.Properties["workingDir"].Type)
	assert.Equal(t, "", desc.Schema.Properties["env"].Type)
	assert.Equal(t, "integer", desc.Schema.Properties["timeout"].Type)
	assert.Equal(t, "string", desc.Schema.Properties["shell"].Type)
	assert.NotEmpty(t, desc.Schema.Properties["shell"].Enum, "shell should have an enum constraint")

	// Verify output schemas
	// From and Transform return AnyProp (full map by default, string with raw: true)
	for _, cap := range []provider.Capability{provider.CapabilityFrom, provider.CapabilityTransform} {
		schema := desc.OutputSchemas[cap]
		require.NotNil(t, schema, "output schema for %s", cap)
	}
	// Action capability returns a full object schema
	actionSchema := desc.OutputSchemas[provider.CapabilityAction]
	require.NotNil(t, actionSchema)
	require.NotNil(t, actionSchema.Properties)
	assert.Equal(t, "string", actionSchema.Properties["stdout"].Type)
	assert.Equal(t, "string", actionSchema.Properties["stderr"].Type)
	assert.Equal(t, "integer", actionSchema.Properties["exitCode"].Type)
	assert.Equal(t, "string", actionSchema.Properties["command"].Type)
	assert.Equal(t, "string", actionSchema.Properties["shell"].Type)
	assert.Equal(t, "boolean", actionSchema.Properties["success"].Type)
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
	assert.Equal(t, "echo 'hello' 'world'", data["command"])
	assert.NotEmpty(t, data["shell"])
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
	assert.NotEmpty(t, data["shell"])
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
	assert.NotEmpty(t, data["shell"])
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
	assert.NotEmpty(t, data["shell"])
}

func TestExecProvider_Execute_WithEnv(t *testing.T) {
	p := NewExecProvider()
	ctx := context.Background()

	// The embedded shell supports variable expansion directly.
	inputs := map[string]any{
		"command": "echo $TEST_VAR",
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
	assert.NotEmpty(t, data["shell"])
}

func TestExecProvider_Execute_NonZeroExitCode(t *testing.T) {
	p := NewExecProvider()
	ctx := context.Background()

	// The embedded shell handles 'exit' as a builtin.
	inputs := map[string]any{
		"command": "exit 42",
	}

	output, err := p.Execute(ctx, inputs)

	require.NoError(t, err)
	require.NotNil(t, output)

	data, ok := output.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, 42, data["exitCode"])
	assert.Equal(t, false, data["success"])
	assert.NotEmpty(t, data["shell"])
}

func TestExecProvider_Execute_StderrOutput(t *testing.T) {
	p := NewExecProvider()
	ctx := context.Background()

	// The embedded shell supports redirections.
	inputs := map[string]any{
		"command": "echo error message >&2",
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
	assert.NotEmpty(t, data["shell"])
}

func TestExecProvider_Execute_WithShellAuto(t *testing.T) {
	p := NewExecProvider()
	ctx := context.Background()

	inputs := map[string]any{
		"command": "echo hello",
		"shell":   "auto",
	}

	output, err := p.Execute(ctx, inputs)

	require.NoError(t, err)
	require.NotNil(t, output)

	data, ok := output.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "hello\n", data["stdout"])
	assert.Equal(t, 0, data["exitCode"])
	assert.Equal(t, true, data["success"])
	assert.Equal(t, "auto", data["shell"])
}

func TestExecProvider_Execute_WithShellSh(t *testing.T) {
	p := NewExecProvider()
	ctx := context.Background()

	inputs := map[string]any{
		"command": "echo hello",
		"shell":   "sh",
	}

	output, err := p.Execute(ctx, inputs)

	require.NoError(t, err)
	require.NotNil(t, output)

	data, ok := output.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "hello\n", data["stdout"])
	assert.Equal(t, 0, data["exitCode"])
	assert.Equal(t, true, data["success"])
	// 'sh' is an alias for 'auto', both use the embedded shell
	assert.Equal(t, "sh", data["shell"])
}

func TestExecProvider_Execute_InvalidShellType(t *testing.T) {
	p := NewExecProvider()
	ctx := context.Background()

	inputs := map[string]any{
		"command": "echo hello",
		"shell":   "zsh",
	}

	output, err := p.Execute(ctx, inputs)

	require.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "unsupported shell type")
}

func TestExecProvider_Execute_ShellNotString(t *testing.T) {
	p := NewExecProvider()
	ctx := context.Background()

	inputs := map[string]any{
		"command": "echo hello",
		"shell":   true,
	}

	output, err := p.Execute(ctx, inputs)

	require.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "shell must be a string")
}

func TestExecProvider_Execute_Pipeline(t *testing.T) {
	p := NewExecProvider()
	ctx := context.Background()

	// Pipes are handled natively by the embedded shell.
	inputs := map[string]any{
		"command": "echo 'hello world' | tr a-z A-Z",
	}

	output, err := p.Execute(ctx, inputs)

	require.NoError(t, err)
	require.NotNil(t, output)

	data, ok := output.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "HELLO WORLD\n", data["stdout"])
	assert.Equal(t, 0, data["exitCode"])
	assert.Equal(t, true, data["success"])
	assert.NotEmpty(t, data["shell"])
}

func TestExecProvider_Execute_WithTimeout(t *testing.T) {
	p := NewExecProvider()
	ctx := context.Background()

	// The embedded shell runs this POSIX script cross-platform.
	inputs := map[string]any{
		"command": "for i in 1 2 3 4 5 6 7 8 9 10; do echo $i; sleep 1; done",
		"timeout": 2,
	}

	output, err := p.Execute(ctx, inputs)

	// The command should error due to timeout (context deadline exceeded).
	if err != nil {
		t.Logf("Got error as expected: %v", err)
		assert.Nil(t, output)
	} else {
		// Command was killed by signal, check exit code is non-zero.
		require.NotNil(t, output)
		data, ok := output.Data.(map[string]any)
		require.True(t, ok)
		exitCode := data["exitCode"].(int)
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

	// The embedded shell returns exit code 127 for "command not found"
	// rather than returning a Go error.
	require.NoError(t, err)
	require.NotNil(t, output)

	data, ok := output.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, 127, data["exitCode"])
	assert.Equal(t, false, data["success"])
	assert.NotEmpty(t, data["shell"])
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
	assert.Equal(t, "echo 'hello'", data["command"])
	assert.Equal(t, true, data["_dryRun"])
	assert.Contains(t, data["_message"], "Would execute via auto shell: echo 'hello'")
	assert.Equal(t, "auto", data["shell"])
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
	assert.Contains(t, data["_message"], "Would execute via auto shell: pwd")
	assert.Contains(t, data["_message"], "in directory: /tmp")
	assert.Equal(t, "auto", data["shell"])
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
	assert.NotEmpty(t, data["shell"])
}

func TestExecProvider_Execute_ContextCancellation(t *testing.T) {
	p := NewExecProvider()

	// Use a context with timeout as a safety net, plus manual cancel.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// The embedded shell runs sleep cross-platform.
	inputs := map[string]any{
		"command": "sleep 30",
	}

	// Cancel after a short delay.
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	output, err := p.Execute(ctx, inputs)
	elapsed := time.Since(start)

	// When context is cancelled, the embedded shell returns a context error.
	if err != nil {
		errStr := err.Error()
		assert.True(t, strings.Contains(errStr, "context") || strings.Contains(errStr, "signal"),
			"Expected context or signal error, got: %s", errStr)
		assert.Nil(t, output)
	} else {
		// Process was killed by signal, returned exit info.
		require.NotNil(t, output)
		data, ok := output.Data.(map[string]any)
		require.True(t, ok)
		exitCode := data["exitCode"].(int)
		success := data["success"].(bool)
		if exitCode == 0 {
			assert.False(t, success, "Expected success=false when process was killed")
		}
	}

	// Verify command was killed quickly — much less than 30s.
	assert.Less(t, elapsed, 10*time.Second, "Context cancellation should kill the process promptly")
}

func TestExecProvider_Execute_InvalidInputType(t *testing.T) {
	p := NewExecProvider()
	ctx := context.Background()

	output, err := p.Execute(ctx, "not a map")

	require.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "expected map[string]any")
}

func TestExecProvider_Execute_CommandSubstitution(t *testing.T) {
	p := NewExecProvider()
	ctx := context.Background()

	inputs := map[string]any{
		"command": "echo $(echo nested)",
	}

	output, err := p.Execute(ctx, inputs)

	require.NoError(t, err)
	require.NotNil(t, output)

	data, ok := output.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "nested\n", data["stdout"])
	assert.Equal(t, 0, data["exitCode"])
	assert.Equal(t, true, data["success"])
}

func TestExecProvider_Execute_Conditionals(t *testing.T) {
	p := NewExecProvider()
	ctx := context.Background()

	inputs := map[string]any{
		"command": "if true; then echo yes; else echo no; fi",
	}

	output, err := p.Execute(ctx, inputs)

	require.NoError(t, err)
	require.NotNil(t, output)

	data, ok := output.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "yes\n", data["stdout"])
	assert.Equal(t, 0, data["exitCode"])
}

func TestExecProvider_Execute_MultilineCommand(t *testing.T) {
	p := NewExecProvider()
	ctx := context.Background()

	inputs := map[string]any{
		"command": "echo line1\necho line2",
	}

	output, err := p.Execute(ctx, inputs)

	require.NoError(t, err)
	require.NotNil(t, output)

	data, ok := output.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "line1\nline2\n", data["stdout"])
	assert.Equal(t, 0, data["exitCode"])
}

func TestExecProvider_Execute_DefaultFullMapInFromMode(t *testing.T) {
	p := NewExecProvider()
	ctx := provider.WithExecutionMode(context.Background(), provider.CapabilityFrom)

	inputs := map[string]any{
		"command": "echo",
		"args":    []any{"hello world"},
	}

	output, err := p.Execute(ctx, inputs)

	require.NoError(t, err)
	require.NotNil(t, output)

	// In from mode without raw, should return full map
	data, ok := output.Data.(map[string]any)
	require.True(t, ok, "expected map, got %T", output.Data)
	assert.Equal(t, "hello world\n", data["stdout"])
	assert.Equal(t, 0, data["exitCode"])
}

func TestExecProvider_Execute_DefaultFullMapInTransformMode(t *testing.T) {
	p := NewExecProvider()
	ctx := provider.WithExecutionMode(context.Background(), provider.CapabilityTransform)

	inputs := map[string]any{
		"command": "echo",
		"args":    []any{"hello world"},
	}

	output, err := p.Execute(ctx, inputs)

	require.NoError(t, err)
	require.NotNil(t, output)

	// In transform mode without raw, should return full map
	data, ok := output.Data.(map[string]any)
	require.True(t, ok, "expected map, got %T", output.Data)
	assert.Equal(t, "hello world\n", data["stdout"])
	assert.Equal(t, 0, data["exitCode"])
}

func TestExecProvider_Execute_RawTrue_ReturnsTrimmedString(t *testing.T) {
	p := NewExecProvider()
	ctx := provider.WithExecutionMode(context.Background(), provider.CapabilityFrom)

	inputs := map[string]any{
		"command": "echo",
		"args":    []any{"hello"},
		"raw":     true,
	}

	output, err := p.Execute(ctx, inputs)

	require.NoError(t, err)
	require.NotNil(t, output)

	// With raw: true, should return trimmed stdout string in from mode
	data, ok := output.Data.(string)
	require.True(t, ok, "expected string, got %T", output.Data)
	assert.Equal(t, "hello", data)
}

func TestExecProvider_Execute_ActionMode_ReturnsFullMap(t *testing.T) {
	p := NewExecProvider()
	ctx := provider.WithExecutionMode(context.Background(), provider.CapabilityAction)

	inputs := map[string]any{
		"command": "echo",
		"args":    []any{"hello"},
	}

	output, err := p.Execute(ctx, inputs)

	require.NoError(t, err)
	require.NotNil(t, output)

	// Action mode always returns the full map
	data, ok := output.Data.(map[string]any)
	require.True(t, ok, "expected map, got %T", output.Data)
	assert.Equal(t, "hello\n", data["stdout"])
	assert.Equal(t, 0, data["exitCode"])
	assert.Equal(t, true, data["success"])
}

func TestExecProvider_Execute_NoExecutionMode_ReturnsFullMap(t *testing.T) {
	p := NewExecProvider()
	ctx := context.Background() // No execution mode set

	inputs := map[string]any{
		"command": "echo",
		"args":    []any{"hello"},
	}

	output, err := p.Execute(ctx, inputs)

	require.NoError(t, err)
	require.NotNil(t, output)

	// No execution mode = falls through to full map
	data, ok := output.Data.(map[string]any)
	require.True(t, ok, "expected map, got %T", output.Data)
	assert.Equal(t, "hello\n", data["stdout"])
}

func TestExecProvider_Execute_Passthrough(t *testing.T) {
	p := NewExecProvider()

	var termOut bytes.Buffer
	var termErr bytes.Buffer
	ctx := provider.WithIOStreams(context.Background(), &provider.IOStreams{
		Out:    &termOut,
		ErrOut: &termErr,
	})

	inputs := map[string]any{
		"command":     "echo",
		"args":        []any{"passthrough-test"},
		"passthrough": true,
	}

	output, err := p.Execute(ctx, inputs)

	require.NoError(t, err)
	require.NotNil(t, output)

	// Passthrough: output streamed to terminal, not captured in result
	data, ok := output.Data.(map[string]any)
	require.True(t, ok, "expected map, got %T", output.Data)
	assert.Empty(t, data["stdout"], "passthrough should not capture stdout")
	assert.Equal(t, 0, data["exitCode"])
	assert.Equal(t, true, data["success"])

	// Terminal should have received the output
	assert.Contains(t, termOut.String(), "passthrough-test")
}

func TestExecProvider_Execute_Passthrough_ExitCode(t *testing.T) {
	p := NewExecProvider()

	var termOut bytes.Buffer
	ctx := provider.WithIOStreams(context.Background(), &provider.IOStreams{
		Out:    &termOut,
		ErrOut: &termOut,
	})

	inputs := map[string]any{
		"command":     "exit 42",
		"passthrough": true,
	}

	output, err := p.Execute(ctx, inputs)

	require.NoError(t, err)
	require.NotNil(t, output)

	data, ok := output.Data.(map[string]any)
	require.True(t, ok, "expected map, got %T", output.Data)
	assert.Equal(t, 42, data["exitCode"])
	assert.Equal(t, false, data["success"])
}

func TestExecProvider_Execute_PassthroughFalse_StillCaptures(t *testing.T) {
	p := NewExecProvider()

	var termOut bytes.Buffer
	ctx := provider.WithIOStreams(context.Background(), &provider.IOStreams{
		Out:    &termOut,
		ErrOut: &termOut,
	})

	inputs := map[string]any{
		"command":     "echo",
		"args":        []any{"captured"},
		"passthrough": false,
	}

	output, err := p.Execute(ctx, inputs)

	require.NoError(t, err)
	require.NotNil(t, output)

	data, ok := output.Data.(map[string]any)
	require.True(t, ok, "expected map, got %T", output.Data)
	// Default behavior: stdout is captured
	assert.Contains(t, data["stdout"], "captured")
}

func TestExecProvider_Execute_InjectsNoColor(t *testing.T) {
	p := NewExecProvider()
	ctx := context.Background()

	// Without user-provided env, NO_COLOR and TERM should still be injected.
	inputs := map[string]any{
		"command": "echo NO_COLOR=$NO_COLOR TERM=$TERM",
	}

	output, err := p.Execute(ctx, inputs)

	require.NoError(t, err)
	require.NotNil(t, output)

	data, ok := output.Data.(map[string]any)
	require.True(t, ok)
	assert.Contains(t, data["stdout"], "NO_COLOR=1")
	assert.Contains(t, data["stdout"], "TERM=dumb")
}

func TestExecProvider_Execute_InjectsNoColor_WithUserEnv(t *testing.T) {
	p := NewExecProvider()
	ctx := context.Background()

	// User provides env but not NO_COLOR — it should be injected.
	inputs := map[string]any{
		"command": "echo NO_COLOR=$NO_COLOR TERM=$TERM MY_VAR=$MY_VAR",
		"env": map[string]any{
			"MY_VAR": "hello",
		},
	}

	output, err := p.Execute(ctx, inputs)

	require.NoError(t, err)
	require.NotNil(t, output)

	data, ok := output.Data.(map[string]any)
	require.True(t, ok)
	assert.Contains(t, data["stdout"], "NO_COLOR=1")
	assert.Contains(t, data["stdout"], "TERM=dumb")
	assert.Contains(t, data["stdout"], "MY_VAR=hello")
}

func TestExecProvider_Execute_UserCanOverrideNoColor(t *testing.T) {
	p := NewExecProvider()
	ctx := context.Background()

	// User explicitly sets NO_COLOR — it should not be overridden.
	inputs := map[string]any{
		"command": "echo NO_COLOR=$NO_COLOR",
		"env": map[string]any{
			"NO_COLOR": "",
		},
	}

	output, err := p.Execute(ctx, inputs)

	require.NoError(t, err)
	require.NotNil(t, output)

	data, ok := output.Data.(map[string]any)
	require.True(t, ok)
	assert.Contains(t, data["stdout"], "NO_COLOR=")
	// Ensure it's the user's empty value, not "1"
	assert.NotContains(t, data["stdout"], "NO_COLOR=1")
}
