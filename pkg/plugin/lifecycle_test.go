// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/plugin/identity"
	"github.com/oakwood-commons/scafctl/pkg/plugin/plugintest"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests exercise the life of a real plugin process: it starts, a channel
// is established, calls are made, and when the invocation ends the process and
// every file it created are gone, whether the invocation succeeded, failed, or
// timed out. They use a sandbox, so what they find is exactly what the test
// itself caused.

const (
	lifecycleTimeout = 10 * time.Second
	shortCallTimeout = 300 * time.Millisecond
	defaultCallLimit = 10 * time.Second
)

func fixtureDependency() solution.PluginDependency {
	return solution.PluginDependency{Name: "lifecycle-fixture", Kind: solution.PluginKindProvider}
}

// invoke runs one plugin invocation the way the commands do: start the plugin,
// make one call, and release the plugin when finished, whatever happened. It
// returns the start error and the call error separately so a test can tell
// which stage failed.
func invoke(t *testing.T, sb *plugintest.Sandbox, input map[string]any, callLimit time.Duration, opts ...ClientOption) (startErr, callErr error) {
	t.Helper()
	client, err := NewClient(sb.Binary, opts...)
	if err != nil {
		return err, nil
	}
	defer client.Kill()

	ctx, cancel := context.WithTimeout(context.Background(), callLimit)
	defer cancel()
	_, callErr = client.ExecuteProvider(ctx, plugintest.ProviderName, input)
	return nil, callErr
}

func TestLifecycle_ChannelEstablishedBeforeFirstCall(t *testing.T) {
	sb := plugintest.NewSandbox(t, plugintest.Config{})
	client, err := NewClient(sb.Binary)
	require.NoError(t, err)
	t.Cleanup(client.Kill)

	// Starting is finished before any call is made: the plugin is a running
	// process with a socket to be reached on, and it has served nothing yet.
	started := sb.Resources()
	require.Len(t, started.Processes, 1, "the plugin process is running")
	assert.Len(t, started.Sockets, 1, "the plugin is listening")
	assert.Empty(t, sb.CallNames(started.Processes[0]), "no call has been made yet")

	// The first call then goes straight through that channel.
	names, err := client.GetProviders(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{plugintest.ProviderName}, names)
	assert.Equal(t, []string{"GetProviders"}, sb.CallNames(started.Processes[0]))
}

func TestLifecycle_CompletedCallShutsDownGracefully(t *testing.T) {
	t.Run("one client", func(t *testing.T) {
		sb := plugintest.NewSandbox(t, plugintest.Config{})
		client, err := NewClient(sb.Binary)
		require.NoError(t, err)
		pid := sb.LivePIDs()[0]

		_, err = client.ExecuteProvider(context.Background(), plugintest.ProviderName, nil)
		require.NoError(t, err)
		client.Kill()

		sb.RequireClean(t, lifecycleTimeout)
		assert.Equal(t, plugintest.ExitGraceful, sb.ExitReason(pid), "the plugin shut down when asked rather than being killed")
	})

	t.Run("several clients released together", func(t *testing.T) {
		sb := plugintest.NewSandbox(t, plugintest.Config{})
		clients := make([]*Client, 0, 3)
		for range 3 {
			client, err := NewClient(sb.Binary)
			require.NoError(t, err)
			clients = append(clients, client)
		}
		pids := sb.LivePIDs()
		require.Len(t, pids, 3)

		KillAll(clients)

		sb.RequireClean(t, lifecycleTimeout)
		for _, pid := range pids {
			assert.Equal(t, plugintest.ExitGraceful, sb.ExitReason(pid), "process %d", pid)
		}
	})
}

func TestLifecycle_ShutdownIsGracefulEvenWhileACallIsBlocked(t *testing.T) {
	sb := plugintest.NewSandbox(t, plugintest.Config{})
	client, err := NewClient(sb.Binary)
	require.NoError(t, err)
	pid := sb.LivePIDs()[0]

	ctx, cancel := context.WithTimeout(context.Background(), shortCallTimeout)
	defer cancel()
	_, err = client.ExecuteProvider(ctx, plugintest.ProviderName, map[string]any{"action": plugintest.ActionHang})
	require.Error(t, err, "the call times out")
	require.Contains(t, sb.LivePIDs(), pid, "the plugin is still running, blocked in the call")

	start := time.Now()
	client.Kill()

	sb.RequireClean(t, lifecycleTimeout)
	assert.Equal(t, plugintest.ExitGraceful, sb.ExitReason(pid), "the plugin was asked to shut down and did, rather than being force-killed")
	assert.Less(t, time.Since(start), time.Second, "a force-kill would have waited out go-plugin's two-second grace period first")
}

func TestLifecycle_SocketIsCreatedInAHostOwnedDirectory(t *testing.T) {
	tests := map[string][]ClientOption{
		"default environment":   nil,
		"sanitized environment": {WithSanitizedEnv()},
	}
	for name, opts := range tests {
		t.Run(name, func(t *testing.T) {
			sb := plugintest.NewSandbox(t, plugintest.Config{})
			client, err := NewClient(sb.Binary, opts...)
			require.NoError(t, err)
			t.Cleanup(client.Kill)

			dirs, sockets := sb.Dirs(), sb.Sockets()
			require.Len(t, dirs, 1, "the host made a directory for the plugin's socket")
			require.Len(t, sockets, 1)
			assert.Equal(t, dirs[0], filepath.Dir(sockets[0]), "the plugin put its socket in that directory")

			out, err := client.ExecuteProvider(context.Background(), plugintest.ProviderName,
				map[string]any{"action": plugintest.ActionEnv, "keys": []string{"PLUGIN_UNIX_SOCKET_DIR"}})
			require.NoError(t, err)
			values, _ := out.Data.(map[string]any)["values"].(map[string]any)
			assert.Equal(t, dirs[0], values["PLUGIN_UNIX_SOCKET_DIR"], "the plugin was told where through its environment")

			client.Kill()
			sb.RequireClean(t, lifecycleTimeout)
		})
	}
}

// An operator who sets PLUGIN_UNIX_SOCKET_DIR for scafctl has chosen where plugin
// sockets go. go-plugin appends the host environment after the host's own
// variables, so their value wins; the host must not make a directory that would
// sit unused, and must not delete the one the operator chose.
func TestLifecycle_HonorsASocketDirectoryChosenByTheOperator(t *testing.T) {
	sb := plugintest.NewSandbox(t, plugintest.Config{})
	operatorDir := filepath.Join(sb.Dir, "operator-sockets")
	require.NoError(t, os.Mkdir(operatorDir, 0o750))
	t.Setenv("PLUGIN_UNIX_SOCKET_DIR", operatorDir)

	client, err := NewClient(sb.Binary)
	require.NoError(t, err)
	t.Cleanup(client.Kill)

	assert.Empty(t, sb.Dirs(), "the host made no directory of its own")
	sockets := sb.Sockets()
	require.Len(t, sockets, 1)
	assert.Equal(t, operatorDir, filepath.Dir(sockets[0]), "the plugin put its socket where the operator said")

	client.Kill()

	sb.RequireClean(t, lifecycleTimeout)
	assert.DirExists(t, operatorDir, "the operator's directory is theirs, and is left in place")
}

// With a sanitized environment (the API server's pools) the operator's variable
// is not passed to plugins at all, so there is nothing to honor: the host makes
// its own directory and the plugin uses it.
func TestLifecycle_SanitizedEnvironmentDropsAnOperatorSocketDirectory(t *testing.T) {
	sb := plugintest.NewSandbox(t, plugintest.Config{})
	operatorDir := filepath.Join(sb.Dir, "operator-sockets")
	require.NoError(t, os.Mkdir(operatorDir, 0o750))
	t.Setenv("PLUGIN_UNIX_SOCKET_DIR", operatorDir)

	client, err := NewClient(sb.Binary, WithSanitizedEnv())
	require.NoError(t, err)
	t.Cleanup(client.Kill)

	dirs, sockets := sb.Dirs(), sb.Sockets()
	require.Len(t, dirs, 1, "the host made its own directory")
	require.Len(t, sockets, 1)
	assert.Equal(t, dirs[0], filepath.Dir(sockets[0]), "the plugin used the host's directory, not the operator's")

	out, err := client.ExecuteProvider(context.Background(), plugintest.ProviderName,
		map[string]any{"action": plugintest.ActionEnv, "keys": []string{"PLUGIN_UNIX_SOCKET_DIR"}})
	require.NoError(t, err)
	values, _ := out.Data.(map[string]any)["values"].(map[string]any)
	assert.Equal(t, dirs[0], values["PLUGIN_UNIX_SOCKET_DIR"], "the plugin was never shown the operator's value")

	client.Kill()
	sb.RequireClean(t, lifecycleTimeout)
}

// A socket path may not exceed the platform limit, and a directory of the
// host's own adds to it. When that would push the path over, the host must not
// make one, or a plugin that used to start would stop starting.
func TestLifecycle_FallsBackToThePluginsOwnSocketWhenTheDirectoryPathIsTooLong(t *testing.T) {
	sb := plugintest.NewSandbox(t, plugintest.Config{})

	// A temp directory long enough that the host's directory would not fit, but
	// short enough that the plugin's own socket still does.
	const tempDirLen = 70
	pad := tempDirLen - len(sb.Dir) - 1
	require.Positive(t, pad)
	deep := filepath.Join(sb.Dir, strings.Repeat("d", pad))
	require.NoError(t, os.Mkdir(deep, 0o750))
	t.Setenv("TMPDIR", deep)
	deepView := &plugintest.Sandbox{Binary: sb.Binary, Dir: deep}

	client, err := NewClient(sb.Binary)
	require.NoError(t, err, "the plugin still starts")
	t.Cleanup(client.Kill)

	assert.Empty(t, deepView.Dirs(), "the host did not make a directory it could not use")
	assert.Len(t, deepView.Sockets(), 1, "the plugin created its own socket")

	client.Kill()
	deepView.RequireClean(t, lifecycleTimeout)
}

func TestLifecycle_FailedInvocationsLeaveNothingBehind(t *testing.T) {
	tests := map[string]struct {
		cfg          plugintest.Config
		input        map[string]any
		callLimit    time.Duration
		opts         []ClientOption
		wantStartErr bool
		wantCallErr  bool
	}{
		"fails to start: exits before the handshake": {
			cfg:          plugintest.Config{Startup: plugintest.StartupExit},
			wantStartErr: true,
		},
		"fails to start: refuses the magic cookie": {
			cfg:          plugintest.Config{Startup: plugintest.StartupBadCookie},
			wantStartErr: true,
		},
		"fails to start: never finishes starting": {
			cfg:          plugintest.Config{Startup: plugintest.StartupHang},
			opts:         []ClientOption{WithStartTimeout(500 * time.Millisecond)},
			wantStartErr: true,
		},
		"fails to start: incompatible handshake version": {
			cfg:          plugintest.Config{HandshakeVersion: 99},
			wantStartErr: true,
		},
		"call fails: the provider reports an error": {
			input:       map[string]any{"action": plugintest.ActionError, "message": "boom"},
			wantCallErr: true,
		},
		"call fails: the output cannot be decoded": {
			input:       map[string]any{"action": plugintest.ActionMalformed},
			wantCallErr: true,
		},
		"call fails: the plugin crashes": {
			input:       map[string]any{"action": plugintest.ActionCrash},
			wantCallErr: true,
		},
		"call fails: the plugin panics": {
			input:       map[string]any{"action": plugintest.ActionPanic},
			wantCallErr: true,
		},
		"call times out: the plugin hangs": {
			input:       map[string]any{"action": plugintest.ActionHang},
			callLimit:   shortCallTimeout,
			wantCallErr: true,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			sb := plugintest.NewSandbox(t, tt.cfg)
			limit := tt.callLimit
			if limit == 0 {
				limit = defaultCallLimit
			}

			startErr, callErr := invoke(t, sb, tt.input, limit, tt.opts...)

			require.NotEmpty(t, sb.PIDs(), "the plugin was started")
			if tt.wantStartErr {
				require.Error(t, startErr)
			} else {
				require.NoError(t, startErr)
			}
			if tt.wantCallErr {
				require.Error(t, callErr)
			}
			sb.RequireClean(t, lifecycleTimeout)
		})
	}
}

func TestLifecycle_RepeatedInvocationsReturnToBaseline(t *testing.T) {
	sb := plugintest.NewSandbox(t, plugintest.Config{})
	baseline := sb.Resources()
	require.True(t, baseline.Empty(), "the sandbox starts with nothing running")

	scenarios := []struct {
		name      string
		cfg       plugintest.Config
		input     map[string]any
		callLimit time.Duration
		opts      []ClientOption
	}{
		{name: "succeeds", input: map[string]any{"id": "ok"}},
		{name: "provider error", input: map[string]any{"action": plugintest.ActionError, "message": "boom"}},
		{name: "malformed output", input: map[string]any{"action": plugintest.ActionMalformed}},
		{name: "crash mid-call", input: map[string]any{"action": plugintest.ActionCrash}},
		{name: "panic mid-call", input: map[string]any{"action": plugintest.ActionPanic}},
		{name: "hung call times out", input: map[string]any{"action": plugintest.ActionHang}, callLimit: shortCallTimeout},
		{name: "exits during startup", cfg: plugintest.Config{Startup: plugintest.StartupExit}},
		{name: "incompatible handshake", cfg: plugintest.Config{HandshakeVersion: 99}},
		{
			name: "startup times out",
			cfg:  plugintest.Config{Startup: plugintest.StartupHang},
			opts: []ClientOption{WithStartTimeout(300 * time.Millisecond)},
		},
	}

	const rounds = 3
	invocations := 0
	for round := 1; round <= rounds; round++ {
		for _, sc := range scenarios {
			sb.SetConfig(t, sc.cfg)
			limit := sc.callLimit
			if limit == 0 {
				limit = defaultCallLimit
			}

			// Every outcome is acceptable here; only what is left behind matters.
			_, _ = invoke(t, sb, sc.input, limit, sc.opts...)
			invocations++

			t.Run(sc.name, func(t *testing.T) {
				sb.RequireClean(t, lifecycleTimeout)
			})
			require.Equal(t, baseline, sb.Resources(), "round %d, %q: counts did not return to the baseline", round, sc.name)
		}
	}
	assert.Len(t, sb.PIDs(), invocations, "every invocation really started a plugin process")
}

func TestLifecycle_PoolShutdownStopsAdoptedPlugins(t *testing.T) {
	sb := plugintest.NewSandbox(t, plugintest.Config{})
	client, err := NewClient(sb.Binary)
	require.NoError(t, err)
	pid := sb.LivePIDs()[0]

	pool := NewPool(context.Background(), nil, provider.NewRegistry(), logr.Discard(), WithIdleTimeout(0))
	pool.Adopt("lifecycle-fixture", client, fixtureDependency(), nil)
	require.Contains(t, sb.LivePIDs(), pid, "adopting does not stop the plugin")

	pool.Shutdown()

	sb.RequireClean(t, lifecycleTimeout)
	assert.Equal(t, plugintest.ExitGraceful, sb.ExitReason(pid))
}

func TestLifecycle_AdoptingIntoAClosedPoolStopsTheClient(t *testing.T) {
	sb := plugintest.NewSandbox(t, plugintest.Config{})
	client, err := NewClient(sb.Binary)
	require.NoError(t, err)
	require.Len(t, sb.LivePIDs(), 1)

	pool := NewPool(context.Background(), nil, provider.NewRegistry(), logr.Discard(), WithIdleTimeout(0))
	pool.Shutdown()

	// Adopting hands the client to the pool. A pool that can no longer manage
	// it must release it, because nothing else holds a reference to kill it.
	pool.Adopt("lifecycle-fixture", client, fixtureDependency(), nil)

	sb.RequireClean(t, lifecycleTimeout)
}

func TestLifecycle_VersionPoolShutdownStopsAdoptedPlugins(t *testing.T) {
	sb := plugintest.NewSandbox(t, plugintest.Config{})
	client, err := NewClient(sb.Binary)
	require.NoError(t, err)
	pid := sb.LivePIDs()[0]

	vp := NewVersionPool(context.Background(), nil, nil)
	vp.Adopt(identity.NewPluginIdentity("lifecycle-fixture", "official"), semver.MustParse("1.0.0"), client, fixtureDependency(), nil)
	require.Contains(t, sb.LivePIDs(), pid)

	vp.Shutdown()

	sb.RequireClean(t, lifecycleTimeout)
	assert.Equal(t, plugintest.ExitGraceful, sb.ExitReason(pid))
}

func TestLifecycle_AdoptingIntoAClosedVersionPoolStopsTheClient(t *testing.T) {
	sb := plugintest.NewSandbox(t, plugintest.Config{})
	client, err := NewClient(sb.Binary)
	require.NoError(t, err)
	require.Len(t, sb.LivePIDs(), 1)

	vp := NewVersionPool(context.Background(), nil, nil)
	vp.Shutdown()

	vp.Adopt(identity.NewPluginIdentity("lifecycle-fixture", "official"), semver.MustParse("1.0.0"), client, fixtureDependency(), nil)

	sb.RequireClean(t, lifecycleTimeout)
}

// Shutdown closes the pool and then snapshots its entries under the pool lock,
// so Adopt must re-check that the pool is open under that same lock: a check
// made before taking it can pass, lose the race to a complete Shutdown, and
// then insert an entry that nothing will ever kill. A real Shutdown rarely lands
// in that window, so this test makes it: it holds the lock so Adopt waits at it,
// closes the pool as Shutdown does, and then lets Adopt through.
//
// It is white-box because the property is about lock ordering, which the public
// API cannot hold still.
func TestLifecycle_VersionPoolAdoptRechecksClosedUnderTheLock(t *testing.T) {
	sb := plugintest.NewSandbox(t, plugintest.Config{})
	client, err := NewClient(sb.Binary)
	require.NoError(t, err)
	require.Len(t, sb.LivePIDs(), 1)

	vp := NewVersionPool(context.Background(), nil, nil)
	vp.mu.Lock()
	adopted := make(chan struct{})
	go func() {
		defer close(adopted)
		vp.Adopt(identity.NewPluginIdentity("lifecycle-fixture", "official"), semver.MustParse("1.0.0"), client, fixtureDependency(), nil)
	}()
	time.Sleep(200 * time.Millisecond) // let Adopt reach the lock
	vp.closed.Store(true)
	vp.mu.Unlock()
	<-adopted

	sb.RequireClean(t, lifecycleTimeout)
}
