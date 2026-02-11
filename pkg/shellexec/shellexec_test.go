// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package shellexec

import (
	"bytes"
	"context"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShellType_IsValid(t *testing.T) {
	tests := []struct {
		shell ShellType
		valid bool
	}{
		{ShellAuto, true},
		{ShellSh, true},
		{ShellBash, true},
		{ShellPwsh, true},
		{ShellCmd, true},
		{"invalid", false},
		{"", false},
		{"zsh", false},
	}
	for _, tt := range tests {
		t.Run(string(tt.shell), func(t *testing.T) {
			assert.Equal(t, tt.valid, tt.shell.IsValid())
		})
	}
}

func TestValidShellTypes(t *testing.T) {
	types := ValidShellTypes()
	assert.Contains(t, types, "auto")
	assert.Contains(t, types, "sh")
	assert.Contains(t, types, "bash")
	assert.Contains(t, types, "pwsh")
	assert.Contains(t, types, "cmd")
}

func TestRun_EmbeddedShell_Echo(t *testing.T) {
	var stdout, stderr bytes.Buffer
	result, err := Run(context.Background(), &RunOptions{
		Command: "echo hello world",
		Shell:   ShellAuto,
		Stdout:  &stdout,
		Stderr:  &stderr,
	})
	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode)
	assert.Equal(t, ShellAuto, result.Shell)
	assert.Equal(t, "hello world\n", stdout.String())
	assert.Equal(t, "", stderr.String())
}

func TestRun_EmbeddedShell_WithArgs(t *testing.T) {
	var stdout bytes.Buffer
	result, err := Run(context.Background(), &RunOptions{
		Command: "echo",
		Args:    []string{"hello", "world"},
		Shell:   ShellAuto,
		Stdout:  &stdout,
	})
	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode)
	assert.Equal(t, "hello world\n", stdout.String())
}

func TestRun_EmbeddedShell_Pipes(t *testing.T) {
	var stdout bytes.Buffer
	result, err := Run(context.Background(), &RunOptions{
		Command: "echo 'hello world' | tr a-z A-Z",
		Shell:   ShellAuto,
		Stdout:  &stdout,
	})
	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode)
	assert.Equal(t, "HELLO WORLD\n", stdout.String())
}

func TestRun_EmbeddedShell_VariableExpansion(t *testing.T) {
	var stdout bytes.Buffer
	result, err := Run(context.Background(), &RunOptions{
		Command: "echo $MY_VAR",
		Shell:   ShellAuto,
		Stdout:  &stdout,
		Env:     MergeEnv(map[string]any{"MY_VAR": "test_value"}),
	})
	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode)
	assert.Equal(t, "test_value\n", stdout.String())
}

func TestRun_EmbeddedShell_Redirect(t *testing.T) {
	var stderr bytes.Buffer
	result, err := Run(context.Background(), &RunOptions{
		Command: "echo error_msg >&2",
		Shell:   ShellAuto,
		Stderr:  &stderr,
	})
	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode)
	assert.Equal(t, "error_msg\n", stderr.String())
}

func TestRun_EmbeddedShell_NonZeroExit(t *testing.T) {
	result, err := Run(context.Background(), &RunOptions{
		Command: "exit 42",
		Shell:   ShellAuto,
	})
	require.NoError(t, err)
	assert.Equal(t, 42, result.ExitCode)
}

func TestRun_EmbeddedShell_DevNull(t *testing.T) {
	var stdout bytes.Buffer
	result, err := Run(context.Background(), &RunOptions{
		Command: "echo 'discarded' > /dev/null && echo 'kept'",
		Shell:   ShellAuto,
		Stdout:  &stdout,
	})
	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode)
	assert.Equal(t, "kept\n", stdout.String())
}

func TestRun_EmbeddedShell_WorkingDir(t *testing.T) {
	var stdout bytes.Buffer
	tmpDir := t.TempDir()
	result, err := Run(context.Background(), &RunOptions{
		Command: "pwd",
		Shell:   ShellAuto,
		Dir:     tmpDir,
		Stdout:  &stdout,
	})
	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode)
	assert.Contains(t, stdout.String(), tmpDir)
}

func TestRun_EmbeddedShell_Stdin(t *testing.T) {
	var stdout bytes.Buffer
	result, err := Run(context.Background(), &RunOptions{
		Command: "cat",
		Shell:   ShellAuto,
		Stdin:   strings.NewReader("stdin_input"),
		Stdout:  &stdout,
	})
	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode)
	assert.Equal(t, "stdin_input", stdout.String())
}

func TestRun_EmbeddedShell_Errexit(t *testing.T) {
	var stdout bytes.Buffer
	result, err := Run(context.Background(), &RunOptions{
		Command: "false; echo 'should not print'",
		Shell:   ShellAuto,
		Stdout:  &stdout,
	})
	require.NoError(t, err)
	assert.NotEqual(t, 0, result.ExitCode)
	assert.Equal(t, "", stdout.String())
}

func TestRun_EmbeddedShell_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := Run(ctx, &RunOptions{
		Command: "sleep 30",
		Shell:   ShellAuto,
	})
	elapsed := time.Since(start)
	assert.Less(t, elapsed, 10*time.Second)
	if err != nil {
		assert.Contains(t, err.Error(), "interrupt")
	}
}

func TestRun_ShellSh_IsAliasForAuto(t *testing.T) {
	var stdout bytes.Buffer
	result, err := Run(context.Background(), &RunOptions{
		Command: "echo 'sh mode'",
		Shell:   ShellSh,
		Stdout:  &stdout,
	})
	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode)
	assert.Equal(t, ShellSh, result.Shell)
	assert.Equal(t, "sh mode\n", stdout.String())
}

func TestRun_ExternalBash(t *testing.T) {
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}
	var stdout bytes.Buffer
	result, err := Run(context.Background(), &RunOptions{
		Command: "echo 'bash mode'",
		Shell:   ShellBash,
		Stdout:  &stdout,
	})
	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode)
	assert.Equal(t, ShellBash, result.Shell)
	assert.Equal(t, "bash mode\n", stdout.String())
}

func TestRun_ExternalBash_WithArgs(t *testing.T) {
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("bash not available")
	}
	var stdout bytes.Buffer
	result, err := Run(context.Background(), &RunOptions{
		Command: "echo",
		Args:    []string{"arg1", "arg2"},
		Shell:   ShellBash,
		Stdout:  &stdout,
	})
	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode)
	assert.Equal(t, "arg1 arg2\n", stdout.String())
}

func TestRun_CmdOnNonWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("This test verifies cmd is rejected on non-Windows")
	}
	_, err := Run(context.Background(), &RunOptions{
		Command: "echo hello",
		Shell:   ShellCmd,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "only available on Windows")
}

func TestRun_InvalidShellType(t *testing.T) {
	_, err := Run(context.Background(), &RunOptions{
		Command: "echo hello",
		Shell:   "invalid",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported shell type")
}

func TestRun_NilOptions(t *testing.T) {
	_, err := Run(context.Background(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil options")
}

func TestRun_EmptyCommand(t *testing.T) {
	_, err := Run(context.Background(), &RunOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty command")
}

func TestRun_DefaultShell(t *testing.T) {
	var stdout bytes.Buffer
	result, err := Run(context.Background(), &RunOptions{
		Command: "echo 'default'",
		Stdout:  &stdout,
	})
	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode)
	assert.Equal(t, ShellAuto, result.Shell)
	assert.Equal(t, "default\n", stdout.String())
}

func TestMergeEnv(t *testing.T) {
	env := MergeEnv(map[string]any{
		"FOO": "bar",
		"NUM": 42,
	})
	assert.True(t, len(env) > 2)
	found := 0
	for _, e := range env {
		if e == "FOO=bar" {
			found++
		}
		if e == "NUM=42" {
			found++
		}
	}
	assert.Equal(t, 2, found, "Expected to find both FOO=bar and NUM=42")
}

func TestRunSimple(t *testing.T) {
	stdout, stderr, exitCode, err := RunSimple(context.Background(), ShellAuto, "echo hello", nil)
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
	assert.Equal(t, "hello\n", stdout)
	assert.Equal(t, "", stderr)
}

func TestBuildFullCommand(t *testing.T) {
	tests := []struct {
		name    string
		command string
		args    []string
		want    string
	}{
		{"no args", "echo", nil, "echo"},
		{"empty args", "echo", []string{}, "echo"},
		{"simple args", "echo", []string{"hello", "world"}, "echo 'hello' 'world'"},
		{"args with spaces", "echo", []string{"hello world"}, "echo 'hello world'"},
		{"args with quotes", "echo", []string{"it's"}, "echo 'it'\\''s'"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildFullCommand(tt.command, tt.args)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestShellQuote(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"hello", "'hello'"},
		{"hello world", "'hello world'"},
		{"it's", "'it'\\''s'"},
		{"", "''"},
		{"$HOME", "'$HOME'"},
		{"; rm -rf /", "'; rm -rf /'"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.want, ShellQuote(tt.input))
		})
	}
}

func TestRun_EmbeddedShell_CommandNotFound(t *testing.T) {
	var stderr bytes.Buffer
	result, err := Run(context.Background(), &RunOptions{
		Command: "nonexistentcommand12345",
		Shell:   ShellAuto,
		Stderr:  &stderr,
	})
	if err != nil {
		assert.Contains(t, err.Error(), "execute")
	} else {
		assert.NotEqual(t, 0, result.ExitCode)
	}
}

func TestRun_EmbeddedShell_MultilineCommand(t *testing.T) {
	var stdout bytes.Buffer
	result, err := Run(context.Background(), &RunOptions{
		Command: "first=\"hello\"\nsecond=\"world\"\necho \"$first $second\"",
		Shell:   ShellAuto,
		Stdout:  &stdout,
	})
	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode)
	assert.Equal(t, "hello world\n", stdout.String())
}

func TestRun_EmbeddedShell_CommandSubstitution(t *testing.T) {
	var stdout bytes.Buffer
	result, err := Run(context.Background(), &RunOptions{
		Command: "echo $(echo nested)",
		Shell:   ShellAuto,
		Stdout:  &stdout,
	})
	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode)
	assert.Equal(t, "nested\n", stdout.String())
}

func TestRun_EmbeddedShell_Conditionals(t *testing.T) {
	var stdout bytes.Buffer
	result, err := Run(context.Background(), &RunOptions{
		Command: "if true; then echo \"yes\"; else echo \"no\"; fi",
		Shell:   ShellAuto,
		Stdout:  &stdout,
	})
	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode)
	assert.Equal(t, "yes\n", stdout.String())
}
