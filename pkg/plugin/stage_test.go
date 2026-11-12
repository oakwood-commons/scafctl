// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWithStage(t *testing.T) {
	t.Parallel()

	t.Run("nil error stays nil", func(t *testing.T) {
		t.Parallel()
		assert.NoError(t, withStage(StageDownload, nil))
	})

	t.Run("tags the error without changing its message", func(t *testing.T) {
		t.Parallel()
		base := errors.New("boom")
		err := withStage(StageDownload, base)

		assert.Equal(t, "boom", err.Error())
		assert.ErrorIs(t, err, base)
		stage, ok := ErrorStage(err)
		require.True(t, ok)
		assert.Equal(t, StageDownload, stage)
	})

	t.Run("innermost stage wins", func(t *testing.T) {
		t.Parallel()
		inner := withStage(StageDigest, errors.New("mismatch"))
		err := withStage(StageDownload, fmt.Errorf("fetching: %w", inner))

		stage, ok := ErrorStage(err)
		require.True(t, ok)
		assert.Equal(t, StageDigest, stage)
	})

	t.Run("stage survives outer wrapping", func(t *testing.T) {
		t.Parallel()
		err := fmt.Errorf("plugin %q is dead: %w", "p", withStage(StageSpawn, errors.New("exec failed")))

		stage, ok := ErrorStage(err)
		require.True(t, ok)
		assert.Equal(t, StageSpawn, stage)
	})
}

func TestErrorStage_Untagged(t *testing.T) {
	t.Parallel()

	_, ok := ErrorStage(errors.New("plain"))
	assert.False(t, ok)
	_, ok = ErrorStage(nil)
	assert.False(t, ok)
}

// TestPoolErrorHTTPStatus_StageTagged verifies stage tagging is transparent to
// the sentinel checks that map acquisition errors to HTTP status codes.
func TestPoolErrorHTTPStatus_StageTagged(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "catalog conflict", err: withStage(StageBind, fmt.Errorf("x: %w", ErrCatalogConflict)), want: 400},
		{name: "local build not allowed", err: withStage(StageBind, errLocalBuild("built")), want: 403},
		{name: "unconfigured registry", err: withStage(StageBind, fmt.Errorf("r: %w", ErrUnconfiguredRegistry)), want: 403},
		{name: "plugin not allowed", err: withStage(StageResolve, fmt.Errorf("p: %w", ErrPluginNotAllowed)), want: 403},
		{name: "untyped download failure", err: withStage(StageDownload, errors.New("registry down")), want: 502},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, PoolErrorHTTPStatus(fmt.Errorf("plugin p (provider): %w", tt.err)))
		})
	}
}
