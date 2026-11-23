// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/plugin/plugintest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegration_PluginLifecycle_RunProviderLeavesNothingBehind runs the real
// scafctl binary against a plugin process and checks that, however the
// invocation ends, no plugin process, socket or socket directory outlives it.
// The binary is the plugin host here, so this tests the lifecycle end to end
// through the same startup and cleanup code a user's command goes through.
func TestIntegration_PluginLifecycle_RunProviderLeavesNothingBehind(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("plugin sockets and temp-directory accounting are unix-only")
	}

	tests := map[string]struct {
		cfg      plugintest.Config
		inputs   []string
		wantExit bool // the command is expected to succeed
	}{
		"succeeds": {
			inputs:   []string{"action=echo"},
			wantExit: true,
		},
		"the provider reports an error": {
			inputs: []string{"action=error", "message=boom"},
		},
		"the output cannot be decoded": {
			inputs: []string{"action=malformed"},
		},
		"the plugin crashes mid-call": {
			inputs: []string{"action=crash"},
		},
		"the plugin panics mid-call": {
			inputs: []string{"action=panic"},
		},
		"the plugin fails to start": {
			cfg:    plugintest.Config{Startup: plugintest.StartupExit},
			inputs: []string{"action=echo"},
		},
		"the plugin speaks an incompatible handshake version": {
			cfg:    plugintest.Config{HandshakeVersion: 99},
			inputs: []string{"action=echo"},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			sb := plugintest.NewDetachedSandbox(t, tt.cfg)

			args := make([]string, 0, 9+len(tt.inputs))
			args = append(args,
				"run", "provider", plugintest.ProviderName,
				"--capability", "transform",
				"--plugin-dir", filepath.Dir(sb.Binary),
			)
			args = append(args, tt.inputs...)
			args = append(args, "-o", "json")

			stdout, stderr, exitCode := runScafctlWithEnv(t, sb.Env(), args...)

			if tt.wantExit {
				assert.Equal(t, 0, exitCode, "stdout: %s\nstderr: %s", stdout, stderr)
				assert.Contains(t, stdout, plugintest.ProviderName)
			} else {
				assert.NotEqual(t, 0, exitCode, "the command should fail; stdout: %s", stdout)
			}
			require.NotEmpty(t, sb.PIDs(), "the command started the plugin")
			sb.RequireClean(t, 10*time.Second)
		})
	}
}
