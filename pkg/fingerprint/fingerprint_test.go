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
