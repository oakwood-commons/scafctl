// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"context"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"slices"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/plugin"
	"github.com/oakwood-commons/scafctl/pkg/plugin/plugintest"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A server that stops must not leave the plugin processes it started running.
// Plugins ignore SIGINT, so once the server process exits nothing else will ever
// stop them. These tests start a server holding real plugin processes, shut it
// down the ways a server is shut down in practice, and check what is left.

const (
	serverStopTimeout  = 15 * time.Second
	pluginCleanTimeout = 10 * time.Second
)

// freeAddr returns a loopback address that was free a moment ago.
func freeAddr(t *testing.T) (host string, port int) {
	t.Helper()
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()
	return "127.0.0.1", ln.Addr().(*net.TCPAddr).Port
}

// newTestServer builds a server on a free port and returns it with its address.
func newTestServer(t *testing.T, ctx context.Context, opts ...ServerOption) (*Server, string) {
	t.Helper()
	host, port := freeAddr(t)
	cfg := &config.Config{APIServer: config.APIServerConfig{Host: host, Port: port}}
	srv, err := NewServer(append([]ServerOption{WithServerConfig(cfg), WithServerContext(ctx)}, opts...)...)
	require.NoError(t, err)
	srv.InitAPI()
	return srv, net.JoinHostPort(host, strconv.Itoa(port))
}

// runServer starts srv and returns once it accepts connections. Start's result
// is delivered on the returned channel.
func runServer(t *testing.T, srv *Server, addr string) <-chan error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- srv.Start() }()

	require.Eventually(t, func() bool {
		conn, err := (&net.Dialer{Timeout: 100 * time.Millisecond}).DialContext(t.Context(), "tcp", addr)
		if err != nil {
			return false
		}
		_ = conn.Close()
		return true
	}, serverStopTimeout, 20*time.Millisecond, "the server never started accepting connections")
	return done
}

// waitStopped waits for Start to return and returns its result.
func waitStopped(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(serverStopTimeout):
		t.Fatal("the server did not stop in time")
		return nil
	}
}

// startFixturePlugin starts a plugin process that is killed when the test ends.
func startFixturePlugin(t *testing.T, sb *plugintest.Sandbox) *plugin.Client {
	t.Helper()
	client, err := plugin.NewClient(sb.Binary)
	require.NoError(t, err)
	t.Cleanup(client.Kill)
	require.Len(t, sb.LivePIDs(), 1, "the plugin is running before the server stops")
	return client
}

// pluginHolder gives the server a running plugin the way a real server holds
// one.
type pluginHolder func(t *testing.T, client *plugin.Client) ServerOption

func holdAsPreloadedClient(_ *testing.T, client *plugin.Client) ServerOption {
	return WithServerPluginClients([]*plugin.Client{client})
}

func holdInPool(t *testing.T, client *plugin.Client) ServerOption {
	t.Helper()
	pool := plugin.NewPool(context.Background(), nil, provider.NewRegistry(), logr.Discard(), plugin.WithIdleTimeout(0))
	t.Cleanup(pool.Shutdown)
	pool.Adopt("lifecycle-fixture", client, solution.PluginDependency{Name: "lifecycle-fixture", Kind: solution.PluginKindProvider}, nil)
	return WithServerPluginPool(pool)
}

// stopTrigger makes a running server begin to shut down and returns Start's
// result.
type stopTrigger func(t *testing.T, cancel context.CancelFunc, done <-chan error) error

func stopByCancellingContext(t *testing.T, cancel context.CancelFunc, done <-chan error) error {
	t.Helper()
	cancel()
	return waitStopped(t, done)
}

// stopBySIGTERM delivers a real SIGTERM to this process. The server installs
// its signal handler from a goroutine, so the signal is repeated until Start
// returns rather than raced against that registration. A handler of the test's
// own is registered first so that a signal arriving before the server's is
// delivered to it instead of terminating the test binary.
func stopBySIGTERM(t *testing.T, _ context.CancelFunc, done <-chan error) error {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("SIGTERM cannot be delivered to a process on Windows")
	}
	guard := make(chan os.Signal, 1)
	signal.Notify(guard, syscall.SIGTERM)
	defer signal.Stop(guard)

	self, err := os.FindProcess(os.Getpid())
	require.NoError(t, err)

	deadline := time.After(serverStopTimeout)
	for {
		require.NoError(t, self.Signal(syscall.SIGTERM))
		select {
		case err := <-done:
			return err
		case <-deadline:
			t.Fatal("the server did not stop after SIGTERM")
			return nil
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func TestServer_Start_StopsPluginsWhenShutdownIsTriggered(t *testing.T) {
	holders := map[string]pluginHolder{
		"preloaded clients": holdAsPreloadedClient,
		"plugin pool":       holdInPool,
	}
	triggers := map[string]stopTrigger{
		"context cancelled": stopByCancellingContext,
		"SIGTERM":           stopBySIGTERM,
	}
	for holderName, hold := range holders {
		for triggerName, stop := range triggers {
			t.Run(holderName+"/"+triggerName, func(t *testing.T) {
				sb := plugintest.NewSandbox(t, plugintest.Config{})
				client := startFixturePlugin(t, sb)

				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				srv, addr := newTestServer(t, ctx, hold(t, client))
				done := runServer(t, srv, addr)

				assert.NoError(t, stop(t, cancel, done))
				sb.RequireClean(t, pluginCleanTimeout)
			})
		}
	}
}

// When the listener cannot be opened, Start returns at once without waiting for
// the shutdown goroutine, so the plugins must be released on that path too.
func TestServer_Start_StopsPluginsWhenItCannotListen(t *testing.T) {
	sb := plugintest.NewSandbox(t, plugintest.Config{})
	client := startFixturePlugin(t, sb)

	srv, addr := newTestServer(t, context.Background(), WithServerPluginClients([]*plugin.Client{client}))
	occupied, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", addr)
	require.NoError(t, err)
	defer occupied.Close()

	require.Error(t, srv.Start(), "the port is taken")

	sb.RequireClean(t, pluginCleanTimeout)
}

// Start also returns before it serves anything when its configuration is
// invalid, and the plugins it was given must be released there too.
func TestServer_Start_StopsPluginsWhenTLSIsInvalid(t *testing.T) {
	sb := plugintest.NewSandbox(t, plugintest.Config{})
	client := startFixturePlugin(t, sb)

	cfg := &config.Config{APIServer: config.APIServerConfig{TLS: config.APITLSConfig{Enabled: true}}}
	srv, err := NewServer(WithServerConfig(cfg), WithServerPluginClients([]*plugin.Client{client}))
	require.NoError(t, err)

	err = srv.Start()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "TLS enabled but cert or key path is empty")

	sb.RequireClean(t, pluginCleanTimeout)
}

// Stopping drains requests that are already running before it stops the
// plugins they may be using, so a request is not cut off mid-call.
func TestServer_Start_DrainsRequestsBeforeStoppingPlugins(t *testing.T) {
	sb := plugintest.NewSandbox(t, plugintest.Config{})
	client := startFixturePlugin(t, sb)
	pid := sb.LivePIDs()[0]

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv, addr := newTestServer(t, ctx, WithServerPluginClients([]*plugin.Client{client}))

	entered := make(chan struct{})
	release := make(chan struct{})
	srv.Router().Get("/test/slow", func(w http.ResponseWriter, _ *http.Request) {
		close(entered)
		<-release
		w.WriteHeader(http.StatusNoContent)
	})
	done := runServer(t, srv, addr)

	statusCode := make(chan int, 1)
	go func() {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+addr+"/test/slow", http.NoBody)
		if err != nil {
			statusCode <- -1
			return
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			statusCode <- -1
			return
		}
		_ = resp.Body.Close()
		statusCode <- resp.StatusCode
	}()
	<-entered

	cancel() // begin shutting down while the request is still running
	require.Never(t, func() bool { return !slices.Contains(sb.LivePIDs(), pid) }, 500*time.Millisecond, 20*time.Millisecond,
		"the plugin was stopped while a request was still being served")

	close(release)
	assert.Equal(t, http.StatusNoContent, <-statusCode, "the in-flight request completed")
	assert.NoError(t, waitStopped(t, done))
	sb.RequireClean(t, pluginCleanTimeout)
}

func TestServer_Shutdown_StopsPreloadedPlugins(t *testing.T) {
	sb := plugintest.NewSandbox(t, plugintest.Config{})
	client := startFixturePlugin(t, sb)

	srv, err := NewServer(WithServerPluginClients([]*plugin.Client{client}))
	require.NoError(t, err)

	require.NoError(t, srv.Shutdown(context.Background()))

	sb.RequireClean(t, pluginCleanTimeout)
	assert.True(t, srv.IsShuttingDown())
}
