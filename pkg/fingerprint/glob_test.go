// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package fingerprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExpandGlobs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		setup    func(t *testing.T, dir string)
		patterns []string
		want     []string
		wantErr  error
	}{
		{
			name:     "empty patterns returns nil",
			patterns: nil,
			want:     nil,
		},
		{
			name: "simple wildcard",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				testWriteFile(t, dir, "a.go", "package a")
				testWriteFile(t, dir, "b.go", "package b")
				testWriteFile(t, dir, "c.txt", "text")
			},
			patterns: []string{"*.go"},
			want:     []string{"a.go", "b.go"},
		},
		{
			name: "recursive doublestar",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				testWriteFile(t, dir, "main.go", "package main")
				testWriteFile(t, dir, "pkg/foo/foo.go", "package foo")
				testWriteFile(t, dir, "pkg/bar/bar.go", "package bar")
				testWriteFile(t, dir, "pkg/bar/bar_test.go", "package bar")
			},
			patterns: []string{"**/*.go"},
			want:     []string{"main.go", "pkg/bar/bar.go", "pkg/bar/bar_test.go", "pkg/foo/foo.go"},
		},
		{
			name: "multiple patterns deduplicated",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				testWriteFile(t, dir, "a.go", "package a")
				testWriteFile(t, dir, "b.go", "package b")
			},
			patterns: []string{"*.go", "a.go"},
			want:     []string{"a.go", "b.go"},
		},
		{
			name: "no matches returns error",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				testWriteFile(t, dir, "a.txt", "text")
			},
			patterns: []string{"*.go"},
			wantErr:  ErrNoMatches,
		},
		{
			name:     "invalid pattern returns error",
			patterns: []string{"[invalid"},
			wantErr:  ErrPatternInvalid,
		},
		{
			name:     "absolute pattern rejected",
			patterns: []string{"/etc/passwd"},
			wantErr:  ErrPatternInvalid,
		},
		{
			name:     "path traversal rejected",
			patterns: []string{"../../../etc/passwd"},
			wantErr:  ErrPatternInvalid,
		},
		{
			name:     "embedded traversal rejected",
			patterns: []string{"foo/../../etc/passwd"},
			wantErr:  ErrPatternInvalid,
		},
		{
			name: "filename starting with double dots is allowed",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				testWriteFile(t, dir, "..config", "data")
			},
			patterns: []string{"..config"},
			want:     []string{"..config"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			if tt.setup != nil {
				tt.setup(t, dir)
			}

			got, err := ExpandGlobs(dir, tt.patterns)
			if tt.wantErr != nil {
				require.Error(t, err)
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
