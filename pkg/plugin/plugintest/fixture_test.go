// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugintest

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	sdkplugin "github.com/oakwood-commons/scafctl-plugin-sdk/plugin"
	"github.com/oakwood-commons/scafctl-plugin-sdk/plugin/proto"
	sdkprovider "github.com/oakwood-commons/scafctl-plugin-sdk/provider"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/plugin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	cleanTimeout = 10 * time.Second
	killTimeout  = 15 * time.Second
	callTimeout  = 10 * time.Second
)

// startHost starts the fixture through the host's own plugin client, the way
// scafctl does, and kills it when the test ends.
func startHost(t *testing.T, sb *Sandbox, opts ...plugin.ClientOption) *plugin.Client {
	t.Helper()
	client, err := plugin.NewClient(sb.Binary, opts...)
	require.NoError(t, err)
	t.Cleanup(client.Kill)
	return client
}

// run calls the fixture provider and returns the output data as a map.
func run(t *testing.T, client *plugin.Client, input map[string]any) (map[string]any, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	out, err := client.ExecuteProvider(ctx, ProviderName, input)
	if err != nil {
		return nil, err
	}
	data, ok := out.Data.(map[string]any)
	require.True(t, ok, "output data is %T, want a map", out.Data)
	return data, nil
}

func rawRequest(t *testing.T, input map[string]any) *proto.ExecuteProviderRequest {
	t.Helper()
	body, err := json.Marshal(input)
	require.NoError(t, err)
	return &proto.ExecuteProviderRequest{ProviderName: ProviderName, Input: body}
}

// killWithin fails the test if kill does not return in time, which would be a
// hung shutdown.
func killWithin(t *testing.T, kill func()) {
	t.Helper()
	done := make(chan struct{})
	go func() { kill(); close(done) }()
	select {
	case <-done:
	case <-time.After(killTimeout):
		t.Fatalf("Kill did not return within %s", killTimeout)
	}
}

func TestFixture_NormalLifecycle(t *testing.T) {
	sb := NewSandbox(t, Config{})
	client, err := plugin.NewClient(sb.Binary)
	require.NoError(t, err)

	// Once the client exists the plugin is a running process with a listening
	// socket, and the host has not made a single call yet.
	started := sb.Resources()
	require.Len(t, started.Processes, 1)
	require.Len(t, started.Sockets, 1)
	pid := started.Processes[0]
	assert.Empty(t, sb.CallNames(pid), "the channel is established before the first call")

	ctx := context.Background()
	names, err := client.GetProviders(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{ProviderName}, names)

	desc, err := client.GetProviderDescriptor(ctx, ProviderName)
	require.NoError(t, err)
	assert.Equal(t, ProviderName, desc.Name)
	assert.ElementsMatch(t, []sdkprovider.Capability{sdkprovider.CapabilityFrom, sdkprovider.CapabilityTransform}, desc.Capabilities)
	assert.NoError(t, sdkprovider.ValidateDescriptor(desc), "the fixture describes itself validly")

	require.NoError(t, client.ConfigureProvider(ctx, ProviderName, plugin.ProviderConfig{}))

	data, err := run(t, client, map[string]any{"greeting": "hello"})
	require.NoError(t, err)
	assert.EqualValues(t, pid, data["pid"], "the call was served by the process the sandbox saw")

	assert.Equal(t,
		[]string{"GetProviders", "GetProviderDescriptor", "ConfigureProvider", "ExecuteProvider"},
		sb.CallNames(pid))

	killWithin(t, client.Kill)
	sb.RequireClean(t, cleanTimeout)
	assert.Equal(t, ExitGraceful, sb.ExitReason(pid), "go-plugin's Kill asks the plugin to shut down, and the fixture does")
}

func TestFixture_StartupFailures(t *testing.T) {
	tests := map[string]struct {
		cfg     Config
		opts    []plugin.ClientOption
		wantErr string
	}{
		"exits before the handshake": {
			cfg:     Config{Startup: StartupExit, ExitCode: 7},
			wantErr: "failed to connect to plugin",
		},
		"refuses the magic cookie": {
			cfg:     Config{Startup: StartupBadCookie},
			wantErr: "failed to connect to plugin",
		},
		"never finishes starting": {
			cfg:     Config{Startup: StartupHang},
			opts:    []plugin.ClientOption{plugin.WithStartTimeout(500 * time.Millisecond)},
			wantErr: "timeout",
		},
		"speaks another handshake version": {
			cfg:     Config{HandshakeVersion: 99},
			wantErr: "incompatible",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			sb := NewSandbox(t, tt.cfg)

			client, err := plugin.NewClient(sb.Binary, tt.opts...)
			require.Error(t, err)
			assert.Nil(t, client)
			assert.Contains(t, strings.ToLower(err.Error()), tt.wantErr)
			t.Logf("start failed as expected: %v", err)

			pids := sb.PIDs()
			require.NotEmpty(t, pids, "the fixture must have started for this to mean anything")
			for _, pid := range pids {
				assert.True(t, sb.WaitForExit(pid, cleanTimeout), "process %d is still running after the failed start", pid)
			}
		})
	}
}

func TestFixture_Actions(t *testing.T) {
	t.Setenv("PLUGINTEST_PROBE", "visible")
	sb := NewSandbox(t, Config{})
	client := startHost(t, sb)

	t.Run("echo reports the call and shares state across calls", func(t *testing.T) {
		first, err := run(t, client, map[string]any{"id": "a"})
		require.NoError(t, err)
		second, err := run(t, client, map[string]any{"id": "b"})
		require.NoError(t, err)

		assert.Equal(t, ProviderName, first["provider"])
		assert.Equal(t, "a", first["input"].(map[string]any)["id"])
		assert.Equal(t, "b", second["input"].(map[string]any)["id"])
		assert.Equal(t, first["pid"], second["pid"], "one process serves both calls")
		assert.Less(t, first["seq"], second["seq"], "the process-wide counter advances")
	})

	t.Run("env reports requested variables and the sandbox temp dir", func(t *testing.T) {
		data, err := run(t, client, map[string]any{
			"action": ActionEnv,
			"keys":   []string{"PLUGINTEST_PROBE", "PLUGINTEST_NOT_SET"},
			"all":    true,
		})
		require.NoError(t, err)
		assert.Equal(t, map[string]any{"PLUGINTEST_PROBE": "visible"}, data["values"], "only variables that are set are reported")
		assert.Contains(t, data["environ"], "TMPDIR="+sb.Dir, "the plugin runs with the sandbox as its temp dir")
	})

	t.Run("sleep honors cancellation and leaves the plugin usable", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		start := time.Now()
		_, err := client.ExecuteProvider(ctx, ProviderName, map[string]any{"action": ActionSleep, "ms": 60000})
		require.Error(t, err)
		assert.Less(t, time.Since(start), callTimeout)

		_, err = run(t, client, nil)
		assert.NoError(t, err, "the plugin still answers after a cancelled call")
	})

	t.Run("error fails the call with its message and exit code", func(t *testing.T) {
		_, err := run(t, client, map[string]any{"action": ActionError, "message": "boom", "exitCode": 7})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "boom")
		assert.Equal(t, 7, exitcode.GetCode(err))
	})

	malformed := []string{MalformedInvalid, MalformedArray, MalformedEmpty, MalformedTruncated}
	for _, kind := range malformed {
		t.Run("malformed "+kind+" cannot be decoded", func(t *testing.T) {
			_, err := run(t, client, map[string]any{"action": ActionMalformed, "kind": kind})
			require.Error(t, err)
			assert.Contains(t, err.Error(), "failed to decode output")
		})
	}

	t.Run("stream delivers chunks then a result", func(t *testing.T) {
		var chunks []plugin.StreamChunk
		err := client.ExecuteProviderStream(context.Background(), ProviderName, map[string]any{"action": ActionStream}, func(c plugin.StreamChunk) {
			chunks = append(chunks, c)
		})
		require.NoError(t, err)
		require.Len(t, chunks, 4)
		assert.Equal(t, "chunk-1\n", string(chunks[0].Stdout))
		assert.Equal(t, "chunk-2\n", string(chunks[1].Stdout))
		assert.Equal(t, "note\n", string(chunks[2].Stderr))
		require.NotNil(t, chunks[3].Result)
	})

	t.Run("host callbacks report that no host service is wired", func(t *testing.T) {
		data, err := run(t, client, map[string]any{"action": ActionAuthToken, "handler": "github"})
		require.NoError(t, err)
		assert.Equal(t, "host service not available", data["error"])
	})
}

func TestFixture_HostCallbacks(t *testing.T) {
	sb := NewSandbox(t, Config{})
	deps := &plugin.HostServiceDeps{
		AuthHandlersFunc: func(context.Context) ([]string, string, error) {
			return []string{"github", "entra"}, "github", nil
		},
		AuthTokenFunc: func(_ context.Context, handler, scope string, _ int64, _ bool) (*proto.GetAuthTokenResponse, error) {
			return &proto.GetAuthTokenResponse{AccessToken: "token-for-" + handler, Scope: scope}, nil
		},
	}
	client := startHost(t, sb, plugin.WithHostDeps(deps))
	require.NoError(t, client.ConfigureProvider(context.Background(), ProviderName, plugin.ProviderConfig{}), "configure relays the host service ID")

	t.Run("the plugin can request a token through the host", func(t *testing.T) {
		data, err := run(t, client, map[string]any{"action": ActionAuthToken, "handler": "github", "scope": "repo"})
		require.NoError(t, err)
		assert.Equal(t, "token-for-github", data["accessToken"])
	})

	t.Run("the plugin can list the host's auth handlers", func(t *testing.T) {
		data, err := run(t, client, map[string]any{"action": ActionAuthHandlers})
		require.NoError(t, err)
		assert.Equal(t, []any{"github", "entra"}, data["handlers"])
		assert.Equal(t, "github", data["default"])
	})

	t.Run("the plugin hears when the host has no secret store", func(t *testing.T) {
		data, err := run(t, client, map[string]any{"action": ActionSecret, "name": "anything"})
		require.NoError(t, err)
		assert.NotEmpty(t, data["error"])
		assert.Equal(t, false, data["found"])
	})
}

func TestFixture_ProcessActions(t *testing.T) {
	// Each of these ends or wedges the plugin process, so each gets its own.
	t.Run("crash mid-call kills the process and leaves no exit record", func(t *testing.T) {
		sb := NewSandbox(t, Config{})
		client := startHost(t, sb)
		pid := sb.LivePIDs()[0]

		_, err := run(t, client, map[string]any{"action": ActionCrash, "exitCode": 9})
		require.Error(t, err)

		assert.True(t, sb.WaitForExit(pid, cleanTimeout))
		assert.Empty(t, sb.ExitReason(pid), "a crash cannot record a graceful exit")
		killWithin(t, client.Kill)
	})

	t.Run("panic mid-call kills the process", func(t *testing.T) {
		sb := NewSandbox(t, Config{})
		client := startHost(t, sb)
		pid := sb.LivePIDs()[0]

		_, err := run(t, client, map[string]any{"action": ActionPanic})
		require.Error(t, err)

		assert.True(t, sb.WaitForExit(pid, cleanTimeout))
		assert.Empty(t, sb.ExitReason(pid))
		killWithin(t, client.Kill)
	})

	t.Run("a hung call outlives its timeout until the host kills the plugin", func(t *testing.T) {
		sb := NewSandbox(t, Config{})
		client := startHost(t, sb)
		pid := sb.LivePIDs()[0]

		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		defer cancel()
		_, err := client.ExecuteProvider(ctx, ProviderName, map[string]any{"action": ActionHang})
		require.Error(t, err)
		assert.Equal(t, codes.DeadlineExceeded, status.Code(err))
		assert.Contains(t, sb.LivePIDs(), pid, "the call is over but the wedged plugin is still running")

		killWithin(t, client.Kill)
		sb.RequireClean(t, cleanTimeout)
	})
}

func TestFixture_WireBehavior(t *testing.T) {
	ctx := context.Background()

	t.Run("unknown fields are sent only when asked for, and the host ignores them", func(t *testing.T) {
		sb := NewSandbox(t, Config{UnknownFields: true})

		raw := sb.Dial(t)
		resp, err := raw.GetProviders(ctx, &proto.GetProvidersRequest{})
		require.NoError(t, err)
		assert.NotEmpty(t, resp.ProtoReflect().GetUnknown(), "the response carries fields outside the schema")
		assert.Equal(t, []string{ProviderName}, resp.GetProviderNames(), "the schema fields are intact")

		client := startHost(t, sb)
		names, err := client.GetProviders(ctx)
		require.NoError(t, err)
		assert.Equal(t, []string{ProviderName}, names)
		_, err = run(t, client, nil)
		assert.NoError(t, err)

		plain := NewSandbox(t, Config{})
		resp, err = plain.Dial(t).GetProviders(ctx, &proto.GetProvidersRequest{})
		require.NoError(t, err)
		assert.Empty(t, resp.ProtoReflect().GetUnknown(), "by default there are none")
	})

	t.Run("the reported protocol version follows the control file", func(t *testing.T) {
		tests := map[string]struct {
			cfg  Config
			want int32
		}{
			"default is the SDK version": {cfg: Config{}, want: sdkplugin.PluginProtocolVersion},
			"older plugin":               {cfg: Config{ProtocolVersion: 1}, want: 1},
			"newer plugin":               {cfg: Config{ProtocolVersion: 99}, want: 99},
		}
		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				sb := NewSandbox(t, tt.cfg)
				resp, err := sb.Dial(t).ConfigureProvider(ctx, &proto.ConfigureProviderRequest{
					ProviderName:    ProviderName,
					ProtocolVersion: sdkplugin.PluginProtocolVersion,
				})
				require.NoError(t, err)
				assert.Equal(t, tt.want, resp.GetProtocolVersion())
			})
		}
	})

	t.Run("the host sends its protocol version when it configures", func(t *testing.T) {
		sb := NewSandbox(t, Config{})
		client := startHost(t, sb)
		pid := sb.LivePIDs()[0]
		require.NoError(t, client.ConfigureProvider(ctx, ProviderName, plugin.ProviderConfig{}))

		calls := sb.Calls(pid)
		require.Len(t, calls, 1)
		assert.Equal(t, "ConfigureProvider", calls[0].RPC)
		assert.Contains(t, calls[0].Detail, "protocolVersion=3")
	})

	t.Run("configure can be unimplemented, as in an old plugin", func(t *testing.T) {
		sb := NewSandbox(t, Config{Configure: ConfigureUnimplemented})
		_, err := sb.Dial(t).ConfigureProvider(ctx, &proto.ConfigureProviderRequest{ProviderName: ProviderName})
		assert.Equal(t, codes.Unimplemented, status.Code(err))

		client := startHost(t, sb)
		assert.NoError(t, client.ConfigureProvider(ctx, ProviderName, plugin.ProviderConfig{}), "the host tolerates a plugin that predates ConfigureProvider")
	})

	t.Run("configure can fail in the response", func(t *testing.T) {
		sb := NewSandbox(t, Config{Configure: ConfigureError})
		client := startHost(t, sb)
		err := client.ConfigureProvider(ctx, ProviderName, plugin.ProviderConfig{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "configure provider failed")
	})

	t.Run("one process can serve several providers", func(t *testing.T) {
		sb := NewSandbox(t, Config{Providers: []string{"alpha", "beta"}})
		client := startHost(t, sb)

		names, err := client.GetProviders(ctx)
		require.NoError(t, err)
		assert.Equal(t, []string{"alpha", "beta"}, names)

		for _, name := range names {
			desc, err := client.GetProviderDescriptor(ctx, name)
			require.NoError(t, err)
			assert.Equal(t, name, desc.Name)
		}
		_, err = client.GetProviderDescriptor(ctx, "gamma")
		assert.Error(t, err)

		alpha, err := client.ExecuteProvider(ctx, "alpha", map[string]any{})
		require.NoError(t, err)
		beta, err := client.ExecuteProvider(ctx, "beta", map[string]any{})
		require.NoError(t, err)
		alphaData, betaData := alpha.Data.(map[string]any), beta.Data.(map[string]any)
		assert.Equal(t, "alpha", alphaData["provider"])
		assert.Equal(t, "beta", betaData["provider"])
		assert.Equal(t, alphaData["pid"], betaData["pid"], "the providers share one process")
	})

	t.Run("a stream can end without its result", func(t *testing.T) {
		sb := NewSandbox(t, Config{})
		stream, err := sb.Dial(t).ExecuteProviderStream(ctx, rawRequest(t, map[string]any{"action": ActionPartialStream, "kind": PartialNoResult}))
		require.NoError(t, err)

		chunk, err := stream.Recv()
		require.NoError(t, err)
		assert.Equal(t, "partial\n", string(chunk.GetStdout()))

		_, err = stream.Recv()
		assert.ErrorIs(t, err, io.EOF, "the stream closes cleanly with no result message")
	})

	t.Run("a stream can be aborted part-way", func(t *testing.T) {
		sb := NewSandbox(t, Config{})
		stream, err := sb.Dial(t).ExecuteProviderStream(ctx, rawRequest(t, map[string]any{"action": ActionPartialStream, "kind": PartialAbort}))
		require.NoError(t, err)

		_, err = stream.Recv()
		require.NoError(t, err)
		_, err = stream.Recv()
		assert.Equal(t, codes.Internal, status.Code(err))
	})

	t.Run("the process can die in the middle of a stream", func(t *testing.T) {
		sb := NewSandbox(t, Config{})
		raw := sb.Dial(t)
		pid := raw.PID()
		stream, err := raw.ExecuteProviderStream(ctx, rawRequest(t, map[string]any{"action": ActionPartialStream, "kind": PartialCrash}))
		require.NoError(t, err)

		for {
			if _, err = stream.Recv(); err != nil {
				break
			}
		}
		assert.NotErrorIs(t, err, io.EOF, "a crash is an error, not a clean end of stream")
		assert.True(t, sb.WaitForExit(pid, cleanTimeout))
	})

	t.Run("stop is recorded and does not end the process", func(t *testing.T) {
		sb := NewSandbox(t, Config{})
		raw := sb.Dial(t)
		pid := raw.PID()
		_, err := raw.StopProvider(ctx, &proto.StopProviderRequest{ProviderName: ProviderName})
		require.NoError(t, err)

		assert.Equal(t, []string{"StopProvider"}, sb.CallNames(pid))
		assert.Contains(t, sb.LivePIDs(), pid, "StopProvider aborts work; the host's Kill ends the process")
	})

	t.Run("a raw client's Kill shuts the plugin down gracefully and is safe to repeat", func(t *testing.T) {
		sb := NewSandbox(t, Config{})
		raw := sb.Dial(t)
		pid := raw.PID()
		require.NotZero(t, pid)

		killWithin(t, raw.Kill)
		killWithin(t, raw.Kill)

		sb.RequireClean(t, cleanTimeout)
		assert.Equal(t, ExitGraceful, sb.ExitReason(pid))
	})
}

func TestFixture_ConcurrentCallsKeepTheirOwnResults(t *testing.T) {
	sb := NewSandbox(t, Config{})
	client := startHost(t, sb)

	const calls = 20
	results := make([]map[string]any, calls)
	errs := make([]error, calls)
	var wg sync.WaitGroup
	for i := range calls {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
			defer cancel()
			out, err := client.ExecuteProvider(ctx, ProviderName, map[string]any{"action": ActionSleep, "ms": 30, "id": i})
			if err != nil {
				errs[i] = err
				return
			}
			results[i], _ = out.Data.(map[string]any)
		}()
	}
	wg.Wait()

	seen := map[any]bool{}
	for i := range calls {
		require.NoError(t, errs[i])
		require.NotNil(t, results[i])
		assert.EqualValues(t, i, results[i]["input"].(map[string]any)["id"], "call %d got another call's answer", i)
		assert.False(t, seen[results[i]["seq"]], "sequence number %v was handed out twice", results[i]["seq"])
		seen[results[i]["seq"]] = true
	}
}

// A test that forgets to stop its plugin must not leave a process behind. The
// subtest leaks on purpose; the sandbox cleanup is what is under test.
func TestSandbox_CleanupKillsLeakedPlugin(t *testing.T) {
	var leaked int
	t.Run("leaks a plugin", func(t *testing.T) {
		sb := NewSandbox(t, Config{})
		_, err := plugin.NewClient(sb.Binary) // never killed
		require.NoError(t, err)
		live := sb.LivePIDs()
		require.Len(t, live, 1)
		leaked = live[0]

		rec := &recordingTB{TB: t}
		sb.RequireClean(rec, 100*time.Millisecond)
		assert.NotEmpty(t, rec.errs, "the leak is visible to the test")
	})

	require.NotZero(t, leaked)
	assert.False(t, processAlive(leaked), "the sandbox cleanup killed the leaked plugin")
}
