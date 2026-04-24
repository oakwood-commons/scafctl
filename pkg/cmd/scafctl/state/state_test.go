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
	sd.Values["env"] = &state.Entry{Value: "prod", Type: "string", UpdatedAt: time.Now().UTC()}
	sd.Values["count"] = &state.Entry{Value: float64(42), Type: "int", UpdatedAt: time.Now().UTC()}
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
	assert.Contains(t, buf.String(), `"type"`)
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
	require.Contains(t, sd.Values, "region")
	assert.Equal(t, "us-east-1", sd.Values["region"].Value)
}

func TestCommandSet_Immutable(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "test.json")
	sd := state.NewData()
	sd.Values["locked"] = &state.Entry{Value: "v1", Type: "string", Immutable: true, UpdatedAt: time.Now().UTC()}
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
	assert.Equal(t, float64(8080), sd.Values["port"].Value)
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
	assert.Equal(t, true, sd.Values["enabled"].Value)
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
	assert.Equal(t, 3.14, sd.Values["ratio"].Value)
}

// ── coerceValue unit tests ────────────────────────────────────────────────────

func TestCoerceValue(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		raw      string
		typ      string
		expected any
	}{
		{name: "string default", raw: "hello", typ: "string", expected: "hello"},
		{name: "int valid", raw: "42", typ: "int", expected: int64(42)},
		{name: "int invalid fallback", raw: "abc", typ: "int", expected: "abc"},
		{name: "bool true", raw: "true", typ: "bool", expected: true},
		{name: "bool false", raw: "false", typ: "bool", expected: false},
		{name: "bool invalid fallback", raw: "nope", typ: "bool", expected: "nope"},
		{name: "float valid", raw: "2.71", typ: "float", expected: 2.71},
		{name: "float invalid fallback", raw: "abc", typ: "float", expected: "abc"},
		{name: "unknown type", raw: "x", typ: "custom", expected: "x"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, coerceValue(tt.raw, tt.typ))
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
	assert.NotContains(t, sd.Values, "env")
	assert.Contains(t, sd.Values, "count")
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
	assert.Empty(t, sd.Values)
}

func TestCommandClear_EmptyState(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "empty.json")

	ctx, buf := newTestContext(t)
	cmd := CommandClear(&settings.Run{BinaryName: "testcli"}, &terminal.IOStreams{Out: buf, ErrOut: buf}, "")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"--path", path})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Cleared 0 entries")
}
