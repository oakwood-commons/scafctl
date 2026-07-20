// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"bytes"
	"context"
	"os"
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
	require.NoError(t, state.SaveToFile(path, "", sd))
}

func seedStateWithImmutable(t *testing.T, path string) {
	t.Helper()
	sd := state.NewData()
	sd.Parameters["env"] = "prod"
	sd.Parameters["project_id"] = "abc-123"
	sd.Resolvers["project_id"] = &state.PersistedEntry{
		Immutable: true,
		Value:     "abc-123",
		Type:      "string",
		CreatedAt: time.Now(),
	}
	require.NoError(t, state.SaveToFile(path, "", sd))
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
	assert.Contains(t, names, "fingerprints")
}

// ── List tests ────────────────────────────────────────────────────────────────

func TestCommandList_EmptyState(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "empty.json")
	// Create an empty state file (commands now require the file to exist).
	require.NoError(t, state.SaveToFile(path, "", state.NewData()))
	ctx, buf := newTestContext(t)

	cliParams := &settings.Run{BinaryName: "testcli"}
	ios := &terminal.IOStreams{Out: buf, ErrOut: buf}
	cmd := CommandList(cliParams, ios, "")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"--path", path})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "no persisted resolver values")
}

func TestCommandList_WithEntries(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "test.json")
	seedStateWithImmutable(t, path)

	ctx, buf := newTestContext(t)
	cliParams := &settings.Run{BinaryName: "testcli"}
	ios := &terminal.IOStreams{Out: buf, ErrOut: buf}
	cmd := CommandList(cliParams, ios, "")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"--path", path, "-o", "json"})

	err := cmd.Execute()
	require.NoError(t, err)
	// list returns only the persisted resolvers map -- not parameters/metadata.
	assert.Contains(t, buf.String(), "project_id")
	assert.NotContains(t, buf.String(), "metadata")
	assert.NotContains(t, buf.String(), "schemaVersion")
}

func TestCommandList_EmptyResolvers(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "empty-resolvers.json")
	seedState(t, path) // parameters only, no resolvers

	ctx, buf := newTestContext(t)
	cliParams := &settings.Run{BinaryName: "testcli"}
	ios := &terminal.IOStreams{Out: buf, ErrOut: buf}
	cmd := CommandList(cliParams, ios, "")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"--path", path})

	require.NoError(t, cmd.Execute())
	assert.Contains(t, buf.String(), "no persisted resolver values")
}

func TestCommandList_ImmutableIncludesValue(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "immutable.json")
	seedStateWithImmutable(t, path)

	ctx, buf := newTestContext(t)
	cliParams := &settings.Run{BinaryName: "testcli"}
	ios := &terminal.IOStreams{Out: buf, ErrOut: buf}
	cmd := CommandList(cliParams, ios, "")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"--path", path, "-o", "json"})

	err := cmd.Execute()
	require.NoError(t, err)
	// The immutable entry's locked value must appear in the listing,
	// not just its key and metadata.
	assert.Contains(t, buf.String(), "project_id")
	assert.Contains(t, buf.String(), "abc-123")
}

// TestCommandList_ExpressionDefaultFormat verifies that a CEL expression
// (-e/--expression) works without an explicit -o format. The filter runs
// against the resolvers map (list's scope), so a path like '_.project_id.value'
// resolves successfully.
func TestCommandList_ExpressionDefaultFormat(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "expr.json")
	seedStateWithImmutable(t, path)

	ctx, buf := newTestContext(t)
	cliParams := &settings.Run{BinaryName: "testcli"}
	ios := &terminal.IOStreams{Out: buf, ErrOut: buf}
	cmd := CommandList(cliParams, ios, "")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"--path", path, "-e", "_.project_id.value"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "abc-123")
	// The resolvers table header must NOT appear -- the expression selected a
	// single scalar, so only that value is emitted.
	assert.NotContains(t, buf.String(), "Resolvers")
}

// ── Show tests ────────────────────────────────────────────────────────────────

// TestCommandShow_FullDocument verifies show renders every populated section.
func TestCommandShow_FullDocument(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "show.json")
	seedStateWithImmutable(t, path)

	ctx, buf := newTestContext(t)
	cliParams := &settings.Run{BinaryName: "testcli"}
	ios := &terminal.IOStreams{Out: buf, ErrOut: buf}
	cmd := CommandShow(cliParams, ios, "")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"--path", path, "-o", "json"})

	require.NoError(t, cmd.Execute())
	out := buf.String()
	assert.Contains(t, out, "schemaVersion")
	assert.Contains(t, out, "metadata")
	assert.Contains(t, out, "parameters")
	assert.Contains(t, out, "resolvers")
}

// TestCommandShow_SummaryHeader verifies the human view leads with a compact
// identity + counts summary.
func TestCommandShow_SummaryHeader(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "summary.json")
	sd := state.NewData()
	sd.Metadata.Solution = "demo"
	sd.Metadata.Version = "9.9.9"
	sd.Parameters["env"] = "prod"
	sd.Resolvers["tok"] = &state.PersistedEntry{Value: "x", Type: "string"}
	require.NoError(t, state.SaveToFile(path, "", sd))

	ctx, buf := newTestContext(t)
	cliParams := &settings.Run{BinaryName: "testcli"}
	ios := &terminal.IOStreams{Out: buf, ErrOut: buf}
	cmd := CommandShow(cliParams, ios, "")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"--path", path})

	require.NoError(t, cmd.Execute())
	out := buf.String()
	assert.Contains(t, out, "demo@9.9.9")
	assert.Contains(t, out, "1 parameters, 1 resolvers, 0 fingerprints")
}

// TestCommandShow_Expression verifies a CEL expression runs against the whole
// document under show.
func TestCommandShow_Expression(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "show-expr.json")
	seedStateWithImmutable(t, path)

	ctx, buf := newTestContext(t)
	cliParams := &settings.Run{BinaryName: "testcli"}
	ios := &terminal.IOStreams{Out: buf, ErrOut: buf}
	cmd := CommandShow(cliParams, ios, "")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"--path", path, "-e", "_.resolvers.project_id.value"})

	require.NoError(t, cmd.Execute())
	assert.Contains(t, buf.String(), "abc-123")
	assert.NotContains(t, buf.String(), "Metadata")
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

	sd, loadErr := state.LoadFromFile(path, "")
	require.NoError(t, loadErr)
	require.Contains(t, sd.Parameters, "region")
	assert.Equal(t, "us-east-1", sd.Parameters["region"])
}

func TestCommandSet_Immutable(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "test.json")
	sd := state.NewData()
	sd.Resolvers["locked"] = &state.PersistedEntry{Immutable: true, Value: "v1", Type: "string", CreatedAt: time.Now().UTC()}
	require.NoError(t, state.SaveToFile(path, "", sd))

	ctx, buf := newTestContext(t)
	cmd := CommandSet(&settings.Run{BinaryName: "testcli"}, &terminal.IOStreams{Out: buf, ErrOut: buf}, "")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"--path", path, "--key", "locked", "--value", "v2"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, buf.String(), "immutable")
}

func TestCommandSet_Persist(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "test.json")

	ctx, buf := newTestContext(t)
	cmd := CommandSet(&settings.Run{BinaryName: "testcli"}, &terminal.IOStreams{Out: buf, ErrOut: buf}, "")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"--path", path, "--key", "token", "--value", "abc", "--persist"})

	require.NoError(t, cmd.Execute())

	sd, loadErr := state.LoadFromFile(path, "")
	require.NoError(t, loadErr)
	entry, ok := sd.Resolvers["token"]
	require.True(t, ok)
	assert.Equal(t, "abc", entry.Value)
	assert.False(t, entry.Immutable, "persist entry must not be immutable")
	assert.NotContains(t, sd.Parameters, "token")
}

func TestCommandSet_PersistOverwritesPreservingCreatedAt(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "test.json")
	created := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	sd := state.NewData()
	sd.Resolvers["token"] = &state.PersistedEntry{Value: "old", Type: "string", CreatedAt: created, UpdatedAt: created}
	require.NoError(t, state.SaveToFile(path, "", sd))

	ctx, buf := newTestContext(t)
	cmd := CommandSet(&settings.Run{BinaryName: "testcli"}, &terminal.IOStreams{Out: buf, ErrOut: buf}, "")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"--path", path, "--key", "token", "--value", "new", "--persist"})

	require.NoError(t, cmd.Execute())

	reloaded, loadErr := state.LoadFromFile(path, "")
	require.NoError(t, loadErr)
	entry := reloaded.Resolvers["token"]
	assert.Equal(t, "new", entry.Value)
	assert.Equal(t, created, entry.CreatedAt.UTC(), "persist preserves CreatedAt")
}

func TestCommandSet_ImmutableAndPersistMutuallyExclusive(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "test.json")

	ctx, buf := newTestContext(t)
	cmd := CommandSet(&settings.Run{BinaryName: "testcli"}, &terminal.IOStreams{Out: buf, ErrOut: buf}, "")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"--path", path, "--key", "k", "--value", "v", "--immutable", "--persist"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, buf.String(), "mutually exclusive")
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

	sd, loadErr := state.LoadFromFile(path, "")
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

	sd, loadErr := state.LoadFromFile(path, "")
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

	sd, loadErr := state.LoadFromFile(path, "")
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

	sd, loadErr := state.LoadFromFile(path, "")
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

func TestCommandDelete_ImmutableWithoutForce(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "test.json")
	seedStateWithImmutable(t, path)

	ctx, buf := newTestContext(t)
	cmd := CommandDelete(&settings.Run{BinaryName: "testcli"}, &terminal.IOStreams{Out: buf, ErrOut: buf}, "")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"--path", path, "--key", "project_id"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, buf.String(), "immutable")
	assert.Contains(t, buf.String(), "--force")

	// Verify neither map was modified
	sd, loadErr := state.LoadFromFile(path, "")
	require.NoError(t, loadErr)
	assert.Contains(t, sd.Parameters, "project_id")
	assert.Contains(t, sd.Resolvers, "project_id")
}

func TestCommandDelete_ImmutableWithForce(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "test.json")
	seedStateWithImmutable(t, path)

	ctx, buf := newTestContext(t)
	cmd := CommandDelete(&settings.Run{BinaryName: "testcli"}, &terminal.IOStreams{Out: buf, ErrOut: buf}, "")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"--path", path, "--key", "project_id", "--force"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Deleted parameter and immutable key")

	// Verify key is gone from both maps
	sd, loadErr := state.LoadFromFile(path, "")
	require.NoError(t, loadErr)
	assert.NotContains(t, sd.Parameters, "project_id")
	assert.NotContains(t, sd.Resolvers, "project_id")
	// Other parameters should be untouched
	assert.Contains(t, sd.Parameters, "env")
}

func TestCommandDelete_ImmutableOnlyWithForce(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "test.json")

	// Seed state with key only in immutables, not in parameters
	sd := state.NewData()
	sd.Resolvers["generated_id"] = &state.PersistedEntry{
		Immutable: true,
		Value:     "xyz-789",
		Type:      "string",
		CreatedAt: time.Now(),
	}
	require.NoError(t, state.SaveToFile(path, "", sd))

	ctx, buf := newTestContext(t)
	cmd := CommandDelete(&settings.Run{BinaryName: "testcli"}, &terminal.IOStreams{Out: buf, ErrOut: buf}, "")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"--path", path, "--key", "generated_id", "--force"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Deleted immutable key")

	// Verify key is gone
	loaded, loadErr := state.LoadFromFile(path, "")
	require.NoError(t, loadErr)
	assert.NotContains(t, loaded.Resolvers, "generated_id")
}

func TestCommandDelete_SaveError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.json")
	seedState(t, path)

	// Make directory read-only so SaveToFile fails
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Skip("cannot restrict permissions on this platform")
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })

	// Verify the restriction is actually enforced (e.g. not running as root)
	probe := filepath.Join(dir, ".probe")
	if err := os.WriteFile(probe, []byte("x"), 0o644); err == nil {
		os.Remove(probe)
		t.Skip("filesystem does not enforce permission restrictions")
	}

	ctx, buf := newTestContext(t)
	cmd := CommandDelete(&settings.Run{BinaryName: "testcli"}, &terminal.IOStreams{Out: buf, ErrOut: buf}, "")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"--path", path, "--key", "env"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, buf.String(), "failed to save state")
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

	sd, loadErr := state.LoadFromFile(path, "")
	require.NoError(t, loadErr)
	assert.Empty(t, sd.Parameters)
	assert.Empty(t, sd.Resolvers)
}

func TestCommandClear_EmptyState(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "empty.json")
	// Create an empty state file (commands now require the file to exist).
	require.NoError(t, state.SaveToFile(path, "", state.NewData()))

	ctx, buf := newTestContext(t)
	cmd := CommandClear(&settings.Run{BinaryName: "testcli"}, &terminal.IOStreams{Out: buf, ErrOut: buf}, "")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"--path", path})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Cleared 0 entries")
}

// ── CommandFingerprints tests ────────────────────────────────────────────────

func seedFingerprintState(t *testing.T, path string) {
	t.Helper()
	sd := state.NewData()
	now := time.Now().UTC()
	sd.Fingerprints["__fingerprint:build:sources"] = &state.FingerprintEntry{Value: "abc123", UpdatedAt: now}
	sd.Fingerprints["__fingerprint:build:generates"] = &state.FingerprintEntry{Value: "def456", UpdatedAt: now}
	sd.Fingerprints["__fingerprint:deploy:sources"] = &state.FingerprintEntry{Value: "ghi789", UpdatedAt: now}
	require.NoError(t, state.SaveToFile(path, "", sd))
}

func TestCommandFingerprints_WithEntries(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "fp.json")
	seedFingerprintState(t, path)

	ctx, buf := newTestContext(t)
	cliParams := &settings.Run{BinaryName: "testcli"}
	ios := &terminal.IOStreams{Out: buf, ErrOut: buf}
	cmd := CommandFingerprints(cliParams, ios, "")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"--path", path, "-o", "json"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "build")
	assert.Contains(t, buf.String(), "deploy")
	assert.Contains(t, buf.String(), "sources")
}

func TestCommandFingerprints_FilterAction(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "fp.json")
	seedFingerprintState(t, path)

	ctx, buf := newTestContext(t)
	cliParams := &settings.Run{BinaryName: "testcli"}
	ios := &terminal.IOStreams{Out: buf, ErrOut: buf}
	cmd := CommandFingerprints(cliParams, ios, "")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"--path", path, "--action", "build", "-o", "json"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "build")
	assert.NotContains(t, buf.String(), "deploy")
}

func TestCommandFingerprints_NoEntries(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "empty.json")
	require.NoError(t, state.SaveToFile(path, "", state.NewData()))

	ctx, buf := newTestContext(t)
	cliParams := &settings.Run{BinaryName: "testcli"}
	ios := &terminal.IOStreams{Out: buf, ErrOut: buf}
	cmd := CommandFingerprints(cliParams, ios, "")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"--path", path})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "No fingerprint entries found")
}

func TestCommandFingerprints_MissingPath(t *testing.T) {
	t.Parallel()
	ctx, buf := newTestContext(t)
	cliParams := &settings.Run{BinaryName: "testcli"}
	ios := &terminal.IOStreams{Out: buf, ErrOut: buf}
	cmd := CommandFingerprints(cliParams, ios, "")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "required flag(s) \"path\" not set")
}

// ── CommandClear selective tests ─────────────────────────────────────────────

func TestCommandClear_FingerprintsOnly(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "fp.json")
	sd := state.NewData()
	now := time.Now().UTC()
	sd.Parameters["env"] = "prod"
	sd.Fingerprints["__fingerprint:build:sources"] = &state.FingerprintEntry{Value: "abc", UpdatedAt: now}
	require.NoError(t, state.SaveToFile(path, "", sd))

	ctx, buf := newTestContext(t)
	cmd := CommandClear(&settings.Run{BinaryName: "testcli"}, &terminal.IOStreams{Out: buf, ErrOut: buf}, "")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"--path", path, "--fingerprints-only"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Cleared 1 entries")

	reloaded, loadErr := state.LoadFromFile(path, "")
	require.NoError(t, loadErr)
	assert.Len(t, reloaded.Parameters, 1)
	assert.Empty(t, reloaded.Fingerprints)
}

func TestCommandClear_Action(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "fp.json")
	seedFingerprintState(t, path)

	ctx, buf := newTestContext(t)
	cmd := CommandClear(&settings.Run{BinaryName: "testcli"}, &terminal.IOStreams{Out: buf, ErrOut: buf}, "")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"--path", path, "--action", "build"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Cleared 2 entries")

	reloaded, loadErr := state.LoadFromFile(path, "")
	require.NoError(t, loadErr)
	assert.Len(t, reloaded.Fingerprints, 1)
	assert.Contains(t, reloaded.Fingerprints, "__fingerprint:deploy:sources")
}

func TestCommandClear_ActionAndFingerprintsOnlyMutuallyExclusive(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "fp.json")
	seedFingerprintState(t, path)

	ctx, buf := newTestContext(t)
	cmd := CommandClear(&settings.Run{BinaryName: "testcli"}, &terminal.IOStreams{Out: buf, ErrOut: buf}, "")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"--path", path, "--action", "build", "--fingerprints-only"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "if any flags in the group [action fingerprints-only] are set none of the others can be")
}

// ── clearEntries unit tests ──────────────────────────────────────────────────

func TestClearEntries_Default(t *testing.T) {
	t.Parallel()
	sd := state.NewData()
	now := time.Now().UTC()
	sd.Parameters["env"] = "prod"
	sd.Resolvers["key"] = &state.PersistedEntry{Immutable: true, Value: "val", CreatedAt: now}
	sd.Fingerprints["__fingerprint:build:sources"] = &state.FingerprintEntry{Value: "abc", UpdatedAt: now}

	count := clearEntries(sd, &clearOptions{})
	assert.Equal(t, 3, count)
	assert.Empty(t, sd.Parameters)
	assert.Empty(t, sd.Resolvers)
	assert.Empty(t, sd.Fingerprints)
}

func TestClearEntries_FingerprintsOnly(t *testing.T) {
	t.Parallel()
	sd := state.NewData()
	now := time.Now().UTC()
	sd.Parameters["env"] = "prod"
	sd.Fingerprints["__fingerprint:build:sources"] = &state.FingerprintEntry{Value: "abc", UpdatedAt: now}
	sd.Fingerprints["__fingerprint:deploy:sources"] = &state.FingerprintEntry{Value: "def", UpdatedAt: now}

	count := clearEntries(sd, &clearOptions{FingerprintsOnly: true})
	assert.Equal(t, 2, count)
	assert.Len(t, sd.Parameters, 1)
	assert.Empty(t, sd.Fingerprints)
}

func TestClearEntries_Action(t *testing.T) {
	t.Parallel()
	sd := state.NewData()
	now := time.Now().UTC()
	sd.Fingerprints["__fingerprint:build:sources"] = &state.FingerprintEntry{Value: "abc", UpdatedAt: now}
	sd.Fingerprints["__fingerprint:build:generates"] = &state.FingerprintEntry{Value: "def", UpdatedAt: now}
	sd.Fingerprints["__fingerprint:deploy:sources"] = &state.FingerprintEntry{Value: "ghi", UpdatedAt: now}

	count := clearEntries(sd, &clearOptions{Action: "build"})
	assert.Equal(t, 2, count)
	assert.Len(t, sd.Fingerprints, 1)
	assert.Contains(t, sd.Fingerprints, "__fingerprint:deploy:sources")
}

// ── buildFingerprintRows unit tests ──────────────────────────────────────────

func TestBuildFingerprintRows(t *testing.T) {
	t.Parallel()
	sd := state.NewData()
	now := time.Now().UTC()
	sd.Fingerprints["__fingerprint:build:sources"] = &state.FingerprintEntry{Value: "abc123", UpdatedAt: now}
	sd.Fingerprints["__fingerprint:build:generates"] = &state.FingerprintEntry{Value: "def456", UpdatedAt: now}

	rows := buildFingerprintRows(sd, []string{"build"})
	require.Len(t, rows, 2)
	assert.Equal(t, "build", rows[0]["action"])
	assert.Equal(t, "generates", rows[0]["type"]) // sorted by key, generates < sources
	assert.Equal(t, "def456", rows[0]["hash"])
	assert.Equal(t, "build", rows[1]["action"])
	assert.Equal(t, "sources", rows[1]["type"])
	assert.Equal(t, "abc123", rows[1]["hash"])
	_, hasUpdatedAt := rows[0]["updatedAt"]
	assert.True(t, hasUpdatedAt, "non-zero timestamp should include updatedAt")
}

func TestBuildFingerprintRows_ZeroTimestamp(t *testing.T) {
	t.Parallel()
	sd := state.NewData()
	sd.Fingerprints["__fingerprint:build:sources"] = &state.FingerprintEntry{Value: "abc123"}

	rows := buildFingerprintRows(sd, []string{"build"})
	require.Len(t, rows, 1)
	assert.Equal(t, "abc123", rows[0]["hash"])
	_, hasUpdatedAt := rows[0]["updatedAt"]
	assert.False(t, hasUpdatedAt, "zero timestamp should omit updatedAt")
}

func TestBuildFingerprintRows_Empty(t *testing.T) {
	t.Parallel()
	sd := state.NewData()
	rows := buildFingerprintRows(sd, nil)
	assert.Nil(t, rows)
}

func TestSplitFingerprintKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		key      string
		wantName string
		wantType string
	}{
		{"__fingerprint:build:sources", "build", "sources"},
		{"__fingerprint:deploy:generates", "deploy", "generates"},
		{"__fingerprint:test:inputs", "test", "inputs"},
		{"not-a-key", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			t.Parallel()
			name, typ := splitFingerprintKey(tt.key)
			assert.Equal(t, tt.wantName, name)
			assert.Equal(t, tt.wantType, typ)
		})
	}
}

// TestCommandState_UnknownSubcommandErrors verifies that an unknown
// subcommand errors (non-zero) while a bare invocation shows help and exits 0.
func TestCommandState_UnknownSubcommandErrors(t *testing.T) {
	t.Parallel()
	cliParams := &settings.Run{BinaryName: "testcli"}
	ios := &terminal.IOStreams{}

	cmd := CommandState(cliParams, ios, "testcli")
	cmd.SetArgs([]string{"bogus-xyz"})
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown command")

	cmd2 := CommandState(cliParams, ios, "testcli")
	cmd2.SetArgs([]string{})
	cmd2.SilenceErrors = true
	cmd2.SilenceUsage = true
	assert.NoError(t, cmd2.Execute())
}
