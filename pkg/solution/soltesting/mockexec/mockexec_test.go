// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mockexec

import (
	"bytes"
	"context"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/shellexec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew_CompileError(t *testing.T) {
	_, err := New([]Rule{
		{Pattern: "[invalid"},
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid mock exec pattern")
}

func TestNew_ValidRules(t *testing.T) {
	m, err := New([]Rule{
		{Command: "echo hello"},
		{Pattern: "^curl "},
	})
	require.NoError(t, err)
	assert.NotNil(t, m)
}

func TestRule_Matches_ExactCommand(t *testing.T) {
	r := Rule{Command: "echo hello"}
	assert.True(t, r.Matches("echo hello"))
	assert.True(t, r.Matches("echo hello world")) // contains
	assert.False(t, r.Matches("echo goodbye"))
}

func TestRule_Matches_Pattern(t *testing.T) {
	r := Rule{Pattern: `^curl\s+`}
	require.NoError(t, r.Compile())
	assert.True(t, r.Matches("curl https://example.com"))
	assert.False(t, r.Matches("wget https://example.com"))
}

func TestMockExec_RunFunc_MatchedCommand(t *testing.T) {
	m, err := New([]Rule{
		{
			Command:  "echo hello",
			Stdout:   "mocked hello\n",
			ExitCode: 0,
		},
	})
	require.NoError(t, err)

	var stdout, stderr bytes.Buffer
	fn := m.RunFunc()
	result, err := fn(context.Background(), &shellexec.RunOptions{
		Command: "echo hello",
		Stdout:  &stdout,
		Stderr:  &stderr,
	})

	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode)
	assert.Equal(t, "mocked hello\n", stdout.String())
	assert.Empty(t, stderr.String())

	calls := m.Calls()
	require.Len(t, calls, 1)
	assert.True(t, calls[0].Matched)
	assert.Equal(t, "echo hello", calls[0].Command)
}

func TestMockExec_RunFunc_MatchedPattern(t *testing.T) {
	m, err := New([]Rule{
		{
			Pattern:  `^terraform\s+apply`,
			Stdout:   "Apply complete!\n",
			Stderr:   "Warning: something\n",
			ExitCode: 0,
		},
	})
	require.NoError(t, err)

	var stdout, stderr bytes.Buffer
	fn := m.RunFunc()
	result, err := fn(context.Background(), &shellexec.RunOptions{
		Command: "terraform apply -auto-approve",
		Stdout:  &stdout,
		Stderr:  &stderr,
	})

	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode)
	assert.Equal(t, "Apply complete!\n", stdout.String())
	assert.Equal(t, "Warning: something\n", stderr.String())
}

func TestMockExec_RunFunc_NonZeroExitCode(t *testing.T) {
	m, err := New([]Rule{
		{
			Command:  "failing-command",
			Stderr:   "error: something went wrong\n",
			ExitCode: 1,
		},
	})
	require.NoError(t, err)

	var stdout, stderr bytes.Buffer
	fn := m.RunFunc()
	result, err := fn(context.Background(), &shellexec.RunOptions{
		Command: "failing-command",
		Stdout:  &stdout,
		Stderr:  &stderr,
	})

	require.NoError(t, err) // error is in exit code, not returned
	assert.Equal(t, 1, result.ExitCode)
	assert.Equal(t, "error: something went wrong\n", stderr.String())
}

func TestMockExec_RunFunc_Unmatched_NoPassthrough(t *testing.T) {
	m, err := New([]Rule{
		{Command: "echo hello"},
	})
	require.NoError(t, err)

	fn := m.RunFunc()
	_, err = fn(context.Background(), &shellexec.RunOptions{
		Command: "unknown-command",
	})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no matching rule")
	assert.Contains(t, err.Error(), "unknown-command")

	calls := m.Calls()
	require.Len(t, calls, 1)
	assert.False(t, calls[0].Matched)
}

func TestMockExec_RunFunc_Unmatched_Passthrough(t *testing.T) {
	m, err := New([]Rule{
		{Command: "nonexistent-cmd"},
	}, WithPassthrough(true))
	require.NoError(t, err)

	var stdout bytes.Buffer
	fn := m.RunFunc()
	result, err := fn(context.Background(), &shellexec.RunOptions{
		Command: "echo passthrough-test",
		Stdout:  &stdout,
	})

	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode)
	assert.Contains(t, stdout.String(), "passthrough-test")
}

func TestMockExec_RunFunc_FirstMatchWins(t *testing.T) {
	m, err := New([]Rule{
		{Command: "echo", Stdout: "first\n"},
		{Command: "echo", Stdout: "second\n"},
	})
	require.NoError(t, err)

	var stdout bytes.Buffer
	fn := m.RunFunc()
	_, err = fn(context.Background(), &shellexec.RunOptions{
		Command: "echo test",
		Stdout:  &stdout,
	})

	require.NoError(t, err)
	assert.Equal(t, "first\n", stdout.String())
}

func TestMockExec_Reset(t *testing.T) {
	m, err := New([]Rule{
		{Command: "echo hello", Stdout: "hi\n"},
	})
	require.NoError(t, err)

	fn := m.RunFunc()
	_, _ = fn(context.Background(), &shellexec.RunOptions{
		Command: "echo hello",
	})

	assert.Len(t, m.Calls(), 1)
	m.Reset()
	assert.Len(t, m.Calls(), 0)
}

func TestMockExec_ContextIntegration(t *testing.T) {
	m, err := New([]Rule{
		{Command: "echo ctx-test", Stdout: "from mock\n"},
	})
	require.NoError(t, err)

	ctx := shellexec.WithRunFunc(context.Background(), m.RunFunc())

	var stdout bytes.Buffer
	result, err := shellexec.RunWithContext(ctx, &shellexec.RunOptions{
		Command: "echo ctx-test",
		Stdout:  &stdout,
	})

	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode)
	assert.Equal(t, "from mock\n", stdout.String())
}
