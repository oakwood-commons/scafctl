// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package fingerprint

import (
	"context"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChecker_Check(t *testing.T) {
	t.Parallel()

	t.Run("first run - no previous state", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		testWriteFile(t, dir, "main.go", "package main")

		checker := NewChecker(state.NewData())
		result, err := checker.Check(context.Background(), "build", []string{"*.go"}, nil, dir, nil)
		require.NoError(t, err)

		assert.True(t, result.Stale)
		assert.Equal(t, ReasonFirstRun, result.Reason)
		assert.NotEmpty(t, result.CurrentHash)
		assert.Empty(t, result.PreviousHash)
	})

	t.Run("sources unchanged and no inputs - up to date", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		testWriteFile(t, dir, "main.go", "package main")

		stateData := state.NewData()
		checker := NewChecker(stateData)

		// Record initial fingerprint
		err := checker.Record(context.Background(), "build", []string{"*.go"}, nil, dir, nil)
		require.NoError(t, err)

		// Check again without changes
		result, err := checker.Check(context.Background(), "build", []string{"*.go"}, nil, dir, nil)
		require.NoError(t, err)

		assert.False(t, result.Stale)
		assert.Equal(t, ReasonUpToDate, result.Reason)
	})

	t.Run("sources changed - stale", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		testWriteFile(t, dir, "main.go", "package main")

		stateData := state.NewData()
		checker := NewChecker(stateData)

		// Record initial fingerprint
		err := checker.Record(context.Background(), "build", []string{"*.go"}, nil, dir, nil)
		require.NoError(t, err)

		// Modify source file
		testWriteFile(t, dir, "main.go", "package main\n\nfunc main() {}")

		// Check again
		result, err := checker.Check(context.Background(), "build", []string{"*.go"}, nil, dir, nil)
		require.NoError(t, err)

		assert.True(t, result.Stale)
		assert.Equal(t, ReasonSourcesChanged, result.Reason)
	})

	t.Run("generates modified externally - stale", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		testWriteFile(t, dir, "main.go", "package main")
		testWriteFile(t, dir, "dist/app", "binary-v1")

		stateData := state.NewData()
		checker := NewChecker(stateData)

		// Record initial fingerprint (sources + generates)
		err := checker.Record(context.Background(), "build", []string{"*.go"}, []string{"dist/app"}, dir, nil)
		require.NoError(t, err)

		// Manually modify generated file (simulates user edit)
		testWriteFile(t, dir, "dist/app", "binary-manually-modified")

		// Check again - should detect modification
		result, err := checker.Check(context.Background(), "build", []string{"*.go"}, []string{"dist/app"}, dir, nil)
		require.NoError(t, err)

		assert.True(t, result.Stale)
		assert.Equal(t, ReasonGeneratesModified, result.Reason)
	})

	t.Run("generates missing - stale", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		testWriteFile(t, dir, "main.go", "package main")

		stateData := state.NewData()
		// Simulate previous run stored a sources hash
		SaveHashes(stateData, "build", "somehash", "somegenhash", "")

		checker := NewChecker(stateData)

		// Compute real sources hash then store it properly
		hash, err := HashFiles(dir, []string{"*.go"})
		require.NoError(t, err)
		SaveHashes(stateData, "build", hash, "somegenhash", "")

		// Check with generates that don't exist
		result, err := checker.Check(context.Background(), "build", []string{"*.go"}, []string{"dist/app"}, dir, nil)
		require.NoError(t, err)

		assert.True(t, result.Stale)
		assert.Equal(t, ReasonGeneratesMissing, result.Reason)
	})

	t.Run("sources and generates both match but no inputs hash - stale first run", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		testWriteFile(t, dir, "main.go", "package main")
		testWriteFile(t, dir, "dist/app", "binary-content")

		stateData := state.NewData()
		checker := NewChecker(stateData)

		// Record initial fingerprint without inputs
		err := checker.Record(context.Background(), "build", []string{"*.go"}, []string{"dist/app"}, dir, nil)
		require.NoError(t, err)

		// Check with inputs -- should be stale because no previous inputs hash
		inputs := map[string]any{"env": "staging"}
		result, err := checker.Check(context.Background(), "build", []string{"*.go"}, []string{"dist/app"}, dir, inputs)
		require.NoError(t, err)

		assert.True(t, result.Stale)
		assert.Equal(t, ReasonFirstRun, result.Reason)
	})

	t.Run("everything matches including inputs - up to date", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		testWriteFile(t, dir, "main.go", "package main")
		testWriteFile(t, dir, "dist/app", "binary-content")

		inputs := map[string]any{"env": "staging", "port": 8080}

		stateData := state.NewData()
		checker := NewChecker(stateData)

		// Record initial fingerprint with inputs
		err := checker.Record(context.Background(), "build", []string{"*.go"}, []string{"dist/app"}, dir, inputs)
		require.NoError(t, err)

		// Check again with same inputs
		result, err := checker.Check(context.Background(), "build", []string{"*.go"}, []string{"dist/app"}, dir, inputs)
		require.NoError(t, err)

		assert.False(t, result.Stale)
		assert.Equal(t, ReasonUpToDate, result.Reason)
	})

	t.Run("inputs changed - stale", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		testWriteFile(t, dir, "main.go", "package main")

		stateData := state.NewData()
		checker := NewChecker(stateData)

		// Record with initial inputs
		inputs1 := map[string]any{"env": "staging"}
		err := checker.Record(context.Background(), "build", []string{"*.go"}, nil, dir, inputs1)
		require.NoError(t, err)

		// Check with different inputs
		inputs2 := map[string]any{"env": "production"}
		result, err := checker.Check(context.Background(), "build", []string{"*.go"}, nil, dir, inputs2)
		require.NoError(t, err)

		assert.True(t, result.Stale)
		assert.Equal(t, ReasonInputsChanged, result.Reason)
	})

	t.Run("nil state data - always stale", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		testWriteFile(t, dir, "main.go", "package main")

		checker := NewChecker(nil)
		result, err := checker.Check(context.Background(), "build", []string{"*.go"}, nil, dir, nil)
		require.NoError(t, err)

		assert.True(t, result.Stale)
		assert.Equal(t, ReasonFirstRun, result.Reason)
	})
}

func TestChecker_Record(t *testing.T) {
	t.Parallel()

	t.Run("stores sources hash", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		testWriteFile(t, dir, "main.go", "package main")

		stateData := state.NewData()
		checker := NewChecker(stateData)

		err := checker.Record(context.Background(), "build", []string{"*.go"}, nil, dir, nil)
		require.NoError(t, err)

		assert.NotEmpty(t, LoadSourcesHash(stateData, "build"))
		assert.Empty(t, LoadGeneratesHash(stateData, "build"))
		assert.Empty(t, LoadInputsHash(stateData, "build"))
	})

	t.Run("stores both sources and generates hash", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		testWriteFile(t, dir, "main.go", "package main")
		testWriteFile(t, dir, "dist/app", "binary")

		stateData := state.NewData()
		checker := NewChecker(stateData)

		err := checker.Record(context.Background(), "build", []string{"*.go"}, []string{"dist/app"}, dir, nil)
		require.NoError(t, err)

		assert.NotEmpty(t, LoadSourcesHash(stateData, "build"))
		assert.NotEmpty(t, LoadGeneratesHash(stateData, "build"))
		assert.Empty(t, LoadInputsHash(stateData, "build"))
	})

	t.Run("stores sources generates and inputs hash", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		testWriteFile(t, dir, "main.go", "package main")
		testWriteFile(t, dir, "dist/app", "binary")

		stateData := state.NewData()
		checker := NewChecker(stateData)

		inputs := map[string]any{"env": "staging", "port": 8080}
		err := checker.Record(context.Background(), "build", []string{"*.go"}, []string{"dist/app"}, dir, inputs)
		require.NoError(t, err)

		assert.NotEmpty(t, LoadSourcesHash(stateData, "build"))
		assert.NotEmpty(t, LoadGeneratesHash(stateData, "build"))
		assert.NotEmpty(t, LoadInputsHash(stateData, "build"))
	})
}

func TestChecker_CheckFiles(t *testing.T) {
	t.Parallel()

	t.Run("first run - stale", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		testWriteFile(t, dir, "main.go", "package main")

		checker := NewChecker(state.NewData())
		result, err := checker.CheckFiles(context.Background(), "build", []string{"*.go"}, nil, dir)
		require.NoError(t, err)

		assert.True(t, result.Stale)
		assert.Equal(t, ReasonFirstRun, result.Reason)
	})

	t.Run("sources unchanged - files fresh", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		testWriteFile(t, dir, "main.go", "package main")

		stateData := state.NewData()
		checker := NewChecker(stateData)

		err := checker.Record(context.Background(), "build", []string{"*.go"}, nil, dir, nil)
		require.NoError(t, err)

		result, err := checker.CheckFiles(context.Background(), "build", []string{"*.go"}, nil, dir)
		require.NoError(t, err)

		assert.False(t, result.Stale)
		assert.Equal(t, ReasonUpToDate, result.Reason)
	})

	t.Run("sources changed - stale", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		testWriteFile(t, dir, "main.go", "package main")

		stateData := state.NewData()
		checker := NewChecker(stateData)

		err := checker.Record(context.Background(), "build", []string{"*.go"}, nil, dir, nil)
		require.NoError(t, err)

		testWriteFile(t, dir, "main.go", "package main\n\nfunc main() {}")

		result, err := checker.CheckFiles(context.Background(), "build", []string{"*.go"}, nil, dir)
		require.NoError(t, err)

		assert.True(t, result.Stale)
		assert.Equal(t, ReasonSourcesChanged, result.Reason)
	})

	// Regression for #522: a source glob referencing a non-existent file must
	// NOT disable fingerprinting. The action should still be fingerprinted on
	// its other sources and report up-to-date on the second run.
	t.Run("partial no-match preserves idempotency", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		testWriteFile(t, dir, "main.go", "package main")

		stateData := state.NewData()
		checker := NewChecker(stateData)
		sources := []string{"*.go", "NonExistentFile.txt"}

		err := checker.Record(context.Background(), "build", sources, nil, dir, nil)
		require.NoError(t, err)

		result, err := checker.CheckFiles(context.Background(), "build", sources, nil, dir)
		require.NoError(t, err)

		assert.False(t, result.Stale, "action must be treated as up-to-date despite the missing source")
		assert.Equal(t, ReasonUpToDate, result.Reason)
		assert.Equal(t, []string{"NonExistentFile.txt"}, result.SourcesEmptyPatterns)
		assert.False(t, result.SourcesAllEmpty)
	})

	// Regression for #522: when every source glob matches nothing, CheckFiles
	// must not return an error; it reports SourcesAllEmpty and marks the action
	// stale (no trackable inputs) so it re-runs rather than being silently
	// skipped on the deterministic empty-set hash.
	t.Run("total no-match reports AllEmpty and forces stale", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		testWriteFile(t, dir, "a.txt", "text")

		stateData := state.NewData()
		checker := NewChecker(stateData)
		sources := []string{"*.go", "missing.txt"}

		// Record then check: an all-empty sources set must remain stale even
		// after a prior run recorded the empty-set hash.
		require.NoError(t, checker.Record(context.Background(), "build", sources, nil, dir, nil))

		result, err := checker.CheckFiles(context.Background(), "build", sources, nil, dir)
		require.NoError(t, err)
		assert.True(t, result.SourcesAllEmpty)
		assert.True(t, result.Stale, "no trackable sources must be treated as always stale")
		assert.Equal(t, ReasonNoSources, result.Reason)
		assert.Equal(t, sources, result.SourcesEmptyPatterns)
	})

	// A declared output that does not exist must mark the action stale even when
	// other declared outputs are present (strict generates semantics).
	t.Run("partial-missing generates marks stale", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		testWriteFile(t, dir, "main.go", "package main")
		testWriteFile(t, dir, "app", "binary")

		stateData := state.NewData()
		checker := NewChecker(stateData)
		sources := []string{"*.go"}
		generates := []string{"app", "missing-output"}

		require.NoError(t, checker.Record(context.Background(), "build", sources, generates, dir, nil))

		result, err := checker.CheckFiles(context.Background(), "build", sources, generates, dir)
		require.NoError(t, err)
		assert.True(t, result.Stale, "a missing declared output must force a rebuild")
		assert.Equal(t, ReasonGeneratesMissing, result.Reason)
	})
}

func TestChecker_CheckInputs(t *testing.T) {
	t.Parallel()

	t.Run("no previous inputs hash - stale first run", func(t *testing.T) {
		t.Parallel()
		checker := NewChecker(state.NewData())

		inputs := map[string]any{"env": "staging"}
		result, err := checker.CheckInputs(context.Background(), "build", inputs)
		require.NoError(t, err)

		assert.True(t, result.Stale)
		assert.Equal(t, ReasonFirstRun, result.Reason)
		assert.NotEmpty(t, result.InputsHash)
		assert.Empty(t, result.PreviousInputsHash)
	})

	t.Run("inputs unchanged - up to date", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		testWriteFile(t, dir, "main.go", "package main")

		stateData := state.NewData()
		checker := NewChecker(stateData)

		inputs := map[string]any{"env": "staging", "port": 8080}
		err := checker.Record(context.Background(), "build", []string{"*.go"}, nil, dir, inputs)
		require.NoError(t, err)

		result, err := checker.CheckInputs(context.Background(), "build", inputs)
		require.NoError(t, err)

		assert.False(t, result.Stale)
		assert.Equal(t, ReasonUpToDate, result.Reason)
	})

	t.Run("inputs changed - stale", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		testWriteFile(t, dir, "main.go", "package main")

		stateData := state.NewData()
		checker := NewChecker(stateData)

		inputs1 := map[string]any{"env": "staging"}
		err := checker.Record(context.Background(), "build", []string{"*.go"}, nil, dir, inputs1)
		require.NoError(t, err)

		inputs2 := map[string]any{"env": "production"}
		result, err := checker.CheckInputs(context.Background(), "build", inputs2)
		require.NoError(t, err)

		assert.True(t, result.Stale)
		assert.Equal(t, ReasonInputsChanged, result.Reason)
	})

	t.Run("nil inputs with previous hash - stale", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		testWriteFile(t, dir, "main.go", "package main")

		stateData := state.NewData()
		checker := NewChecker(stateData)

		// Record with inputs
		inputs := map[string]any{"env": "staging"}
		err := checker.Record(context.Background(), "build", []string{"*.go"}, nil, dir, inputs)
		require.NoError(t, err)

		// Check with nil inputs -- hash will differ
		result, err := checker.CheckInputs(context.Background(), "build", nil)
		require.NoError(t, err)

		assert.True(t, result.Stale)
		assert.Equal(t, ReasonInputsChanged, result.Reason)
	})
}
