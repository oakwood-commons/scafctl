// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package flags

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequireArg(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name: "one arg succeeds",
			args: []string{"gcp"},
		},
		{
			name:    "zero args fails with descriptive message",
			args:    []string{},
			wantErr: "missing required argument: <handler>",
		},
		{
			name:    "two args fails",
			args:    []string{"gcp", "extra"},
			wantErr: "expected 1 argument <handler>, got 2",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			validator := RequireArg("handler", "scafctl auth token gcp")
			cmd := &cobra.Command{Use: "token"}

			err := validator(cmd, tc.args)
			if tc.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				assert.Contains(t, err.Error(), "Example:")
			}
		})
	}
}

func TestRequireArgs(t *testing.T) {
	t.Parallel()

	validator := RequireArgs(2, "<name> <version>", "scafctl snapshot diff a.yaml b.yaml")
	cmd := &cobra.Command{Use: "diff"}

	t.Run("correct count succeeds", func(t *testing.T) {
		t.Parallel()
		err := validator(cmd, []string{"a.yaml", "b.yaml"})
		require.NoError(t, err)
	})

	t.Run("wrong count fails with descriptive message", func(t *testing.T) {
		t.Parallel()
		err := validator(cmd, []string{"a.yaml"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "expected 2 argument(s)")
		assert.Contains(t, err.Error(), "Example:")
	})
}
