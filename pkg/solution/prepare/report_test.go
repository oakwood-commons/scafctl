// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package prepare

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/load"
	"github.com/oakwood-commons/scafctl/pkg/solution/source"
	"github.com/oakwood-commons/scafctl/pkg/state"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
)

func TestErrorExitCode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "invalid ref", err: fmt.Errorf("select: %w", source.ErrInvalidRef), want: exitcode.InvalidInput},
		{name: "legacy state config", err: fmt.Errorf("state: %w", state.ErrLegacyStateConfig), want: exitcode.InvalidInput},
		{name: "other", err: errors.New("boom"), want: exitcode.FileNotFound},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, ErrorExitCode(tc.err))
		})
	}
}

func TestReportLoaded(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		verbose  bool
		solName  string
		version  string
		path     string
		disc     load.Discovery
		want     []string
		notWant  []string
		wantNone bool
	}{
		{
			name:    "name version and path",
			verbose: true,
			solName: "demo",
			version: "1.2.3",
			path:    "/work/solution.yaml",
			want:    []string{"Solution loaded: demo (version=1.2.3, dir=/work)", "Solution: demo@1.2.3 (/work/solution.yaml)"},
		},
		{
			name:    "name and version",
			verbose: true,
			solName: "demo",
			version: "1.2.3",
			want:    []string{"Solution: demo@1.2.3"},
		},
		{
			name:    "name and path",
			verbose: true,
			solName: "demo",
			path:    "/work/solution.yaml",
			want:    []string{"Solution: demo (/work/solution.yaml)"},
		},
		{
			name:    "name only",
			verbose: true,
			solName: "demo",
			want:    []string{"Solution: demo"},
			notWant: []string{"also found at"},
		},
		{
			name:    "path only",
			verbose: true,
			path:    "/work/solution.yaml",
			want:    []string{"Solution: /work/solution.yaml"},
		},
		{
			name:    "actions file alternate",
			verbose: true,
			solName: "demo",
			disc:    load.Discovery{AlternatePath: "actions.yaml"},
			want:    []string{"(actions.yaml also found at actions.yaml; use 'mycli run action' to execute actions)"},
		},
		{
			name:    "solution file alternate",
			verbose: true,
			solName: "demo",
			disc:    load.Discovery{AlternatePath: "scafctl.yaml", IsActionFile: true},
			want:    []string{"(solution file also found at scafctl.yaml; use 'mycli run solution' to run it)"},
		},
		{
			name:     "verbose disabled",
			solName:  "demo",
			wantNone: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			cliParams := settings.NewCliParams()
			cliParams.Verbose = tt.verbose
			cliParams.NoColor = true
			cliParams.BinaryName = "mycli"
			ctx := writer.WithWriter(context.Background(), writer.New(terminal.NewIOStreams(nil, &buf, &buf, false), cliParams))
			ctx = settings.IntoContext(ctx, cliParams)

			sol := &solution.Solution{}
			sol.Metadata.Name = tt.solName
			if tt.version != "" {
				sol.Metadata.Version = semver.MustParse(tt.version)
			}
			sol.SetPath(tt.path)

			reportLoaded(ctx, &Result{Solution: sol, SolutionDir: "/work", DiscoveredFrom: tt.disc})

			if tt.wantNone {
				assert.Empty(t, buf.String())
				return
			}
			for _, s := range tt.want {
				assert.Contains(t, buf.String(), s)
			}
			for _, s := range tt.notWant {
				assert.NotContains(t, buf.String(), s)
			}
		})
	}

	t.Run("no writer in context", func(t *testing.T) {
		t.Parallel()
		assert.NotPanics(t, func() {
			reportLoaded(context.Background(), &Result{Solution: &solution.Solution{}})
		})
	})
}

func TestNewCLIWiring(t *testing.T) {
	t.Parallel()

	t.Run("nil params", func(t *testing.T) {
		t.Parallel()
		w := NewCLIWiring(nil)
		assert.Nil(t, w.PluginConfig)
		assert.False(t, w.DebugLogging)
	})

	t.Run("maps CLI params", func(t *testing.T) {
		t.Parallel()
		params := &settings.Run{IsQuiet: true, NoColor: true, BinaryName: "mycli", MinLogLevel: "debug"}
		w := NewCLIWiring(params)
		require.NotNil(t, w.PluginConfig)
		assert.True(t, w.PluginConfig.Quiet)
		assert.True(t, w.PluginConfig.NoColor)
		assert.Equal(t, "mycli", w.PluginConfig.BinaryName)
		assert.True(t, w.DebugLogging)
	})
}
