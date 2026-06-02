// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/state"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestContext(t *testing.T) (context.Context, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	ios := &terminal.IOStreams{Out: &buf, ErrOut: &buf}
	cliParams := &settings.Run{BinaryName: "testcli"}
	w := writer.New(ios, cliParams)
	ctx := writer.WithWriter(context.Background(), w)
	return ctx, &buf
}

func seedState(t *testing.T, path string) {
	t.Helper()
	sd := state.NewData()
	sd.Parameters["env"] = "prod"
	sd.Parameters["count"] = float64(42)
	require.NoError(t, state.SaveToFile(path, sd))
}

// ── CommandState tests ────────────────────────────────────────────────────────

func TestCommandState_HasSubcommands(t *testing.T) {
	t.Parallel()
	cliParams := &settings.Run{BinaryName: "testcli"}
	ios := &terminal.IOStreams{}
	cmd := CommandState(cliParams, ios, "testcli")

	names := make([]string, 0, len(cmd.Commands()))
	for _, sub := range cmd.Commands() {
		names = append(names, sub.Name())
	}
	assert.Contains(t, names, "list")
	assert.Contains(t, names, "get")
	assert.Contains(t, names, "set")
	assert.Contains(t, names, "delete")
	assert.Contains(t, names, "clear")
}

// ── List tests ────────────────────────────────────────────────────────────────

func TestCommandList_EmptyState(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "empty.json")
	// Create an empty state file (commands now require the file to exist).
	require.NoError(t, state.SaveToFile(path, state.NewData()))
	ctx, buf := newTestContext(t)

	cliParams := &settings.Run{BinaryName: "testcli"}
	ios := &terminal.IOStreams{Out: buf, ErrOut: buf}
	cmd := CommandList(cliParams, ios, "")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"--path", path})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "No state entries found")
}

func TestCommandList_WithEntries(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "test.json")
	seedState(t, path)

	ctx, buf := newTestContext(t)
	cliParams := &settings.Run{BinaryName: "testcli"}
	ios := &terminal.IOStreams{Out: buf, ErrOut: buf}
	cmd := CommandList(cliParams, ios, "")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"--path", path, "-o", "json"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "env")
	assert.Contains(t, buf.String(), "count")
}

// ── Get tests ─────────────────────────────────────────────────────────────────

func TestCommandGet_Found(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "test.json")
	seedState(t, path)

	ctx, buf := newTestContext(t)
	cmd := CommandGet(&settings.Run{BinaryName: "testcli"}, &terminal.IOStreams{Out: buf, ErrOut: buf}, "")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"--path", path, "--key", "env"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "prod")
}

func TestCommandGet_NotFound(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "test.json")
	seedState(t, path)

	ctx, buf := newTestContext(t)
	cmd := CommandGet(&settings.Run{BinaryName: "testcli"}, &terminal.IOStreams{Out: buf, ErrOut: buf}, "")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"--path", path, "--key", "missing"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, buf.String(), "not found")
}

func TestCommandGet_JSON(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "test.json")
	seedState(t, path)

	ctx, buf := newTestContext(t)
	cmd := CommandGet(&settings.Run{BinaryName: "testcli"}, &terminal.IOStreams{Out: buf, ErrOut: buf}, "")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"--path", path, "--key", "env", "-o", "json"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, buf.String(), `"value"`)
	assert.Contains(t, buf.String(), `"env"`)
}

// ── Set tests ─────────────────────────────────────────────────────────────────

func TestCommandSet_NewKey(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "test.json")

	ctx, buf := newTestContext(t)
	cmd := CommandSet(&settings.Run{BinaryName: "testcli"}, &terminal.IOStreams{Out: buf, ErrOut: buf}, "")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"--path", path, "--key", "region", "--value", "us-east-1"})

	err := cmd.Execute()
	require.NoError(t, err)

	sd, loadErr := state.LoadFromFile(path)
	require.NoError(t, loadErr)
	require.Contains(t, sd.Parameters, "region")
	assert.Equal(t, "us-east-1", sd.Parameters["region"])
}

func TestCommandSet_Immutable(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "test.json")
	sd := state.NewData()
	sd.Immutables["locked"] = &state.ImmutableEntry{Value: "v1", Type: "string", CreatedAt: time.Now().UTC()}
	require.NoError(t, state.SaveToFile(path, sd))

	ctx, buf := newTestContext(t)
	cmd := CommandSet(&settings.Run{BinaryName: "testcli"}, &terminal.IOStreams{Out: buf, ErrOut: buf}, "")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"--path", path, "--key", "locked", "--value", "v2"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, buf.String(), "immutable")
}

func TestCommandSet_TypedInt(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "test.json")

	ctx, buf := newTestContext(t)
	cmd := CommandSet(&settings.Run{BinaryName: "testcli"}, &terminal.IOStreams{Out: buf, ErrOut: buf}, "")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"--path", path, "--key", "port", "--value", "8080", "--type", "int"})

	err := cmd.Execute()
	require.NoError(t, err)

	sd, loadErr := state.LoadFromFile(path)
	require.NoError(t, loadErr)
	// JSON round-trips int64 as float64
	assert.Equal(t, float64(8080), sd.Parameters["port"])
}

func TestCommandSet_TypedBool(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "test.json")

	ctx, buf := newTestContext(t)
	cmd := CommandSet(&settings.Run{BinaryName: "testcli"}, &terminal.IOStreams{Out: buf, ErrOut: buf}, "")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"--path", path, "--key", "enabled", "--value", "true", "--type", "bool"})

	err := cmd.Execute()
	require.NoError(t, err)

	sd, loadErr := state.LoadFromFile(path)
	require.NoError(t, loadErr)
	assert.Equal(t, true, sd.Parameters["enabled"])
}

func TestCommandSet_TypedFloat(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "test.json")

	ctx, buf := newTestContext(t)
	cmd := CommandSet(&settings.Run{BinaryName: "testcli"}, &terminal.IOStreams{Out: buf, ErrOut: buf}, "")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"--path", path, "--key", "ratio", "--value", "3.14", "--type", "float"})

	err := cmd.Execute()
	require.NoError(t, err)

	sd, loadErr := state.LoadFromFile(path)
	require.NoError(t, loadErr)
	assert.Equal(t, 3.14, sd.Parameters["ratio"])
}

func TestCommandSet_TypedInt_Invalid(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "test.json")

	ctx, buf := newTestContext(t)
	cmd := CommandSet(&settings.Run{BinaryName: "testcli"}, &terminal.IOStreams{Out: buf, ErrOut: buf}, "")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"--path", path, "--key", "port", "--value", "abc", "--type", "int"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, buf.String(), "cannot parse")
}

// ── coerceValue unit tests ────────────────────────────────────────────────────

func TestCoerceValue(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		raw      string
		typ      string
		expected any
		wantErr  bool
	}{
		{name: "string default", raw: "hello", typ: "string", expected: "hello"},
		{name: "int valid", raw: "42", typ: "int", expected: int64(42)},
		{name: "int invalid", raw: "abc", typ: "int", wantErr: true},
		{name: "bool true", raw: "true", typ: "bool", expected: true},
		{name: "bool false", raw: "false", typ: "bool", expected: false},
		{name: "bool invalid", raw: "nope", typ: "bool", wantErr: true},
		{name: "float valid", raw: "2.71", typ: "float", expected: 2.71},
		{name: "float invalid", raw: "abc", typ: "float", wantErr: true},
		{name: "unknown type", raw: "x", typ: "custom", expected: "x"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := coerceValue(tt.raw, tt.typ)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expected, got)
		})
	}
}

// ── Delete tests ──────────────────────────────────────────────────────────────

func TestCommandDelete_Found(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "test.json")
	seedState(t, path)

	ctx, buf := newTestContext(t)
	cmd := CommandDelete(&settings.Run{BinaryName: "testcli"}, &terminal.IOStreams{Out: buf, ErrOut: buf}, "")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"--path", path, "--key", "env"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Deleted")

	sd, loadErr := state.LoadFromFile(path)
	require.NoError(t, loadErr)
	assert.NotContains(t, sd.Parameters, "env")
	assert.Contains(t, sd.Parameters, "count")
}

func TestCommandDelete_NotFound(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "test.json")
	seedState(t, path)

	ctx, buf := newTestContext(t)
	cmd := CommandDelete(&settings.Run{BinaryName: "testcli"}, &terminal.IOStreams{Out: buf, ErrOut: buf}, "")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"--path", path, "--key", "missing"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, buf.String(), "not found")
}

// ── Clear tests ───────────────────────────────────────────────────────────────

func TestCommandClear(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "test.json")
	seedState(t, path)

	ctx, buf := newTestContext(t)
	cmd := CommandClear(&settings.Run{BinaryName: "testcli"}, &terminal.IOStreams{Out: buf, ErrOut: buf}, "")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"--path", path})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Cleared 2 entries")

	sd, loadErr := state.LoadFromFile(path)
	require.NoError(t, loadErr)
	assert.Empty(t, sd.Parameters)
	assert.Empty(t, sd.Immutables)
}

func TestCommandClear_EmptyState(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "empty.json")
	// Create an empty state file (commands now require the file to exist).
	require.NoError(t, state.SaveToFile(path, state.NewData()))

	ctx, buf := newTestContext(t)
	cmd := CommandClear(&settings.Run{BinaryName: "testcli"}, &terminal.IOStreams{Out: buf, ErrOut: buf}, "")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"--path", path})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Cleared 0 entries")
}
