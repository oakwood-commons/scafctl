// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testDebounce = 60 * time.Millisecond
	// testSettle is a generous wait used to confirm that NO notification is
	// delivered. It must comfortably exceed testDebounce.
	testSettle = 300 * time.Millisecond
)

// notifyCall records a single notification delivered to the fake notifier.
type notifyCall struct {
	sessionID string
	method    string
	uri       string
}

// fakeNotifier is a thread-safe resourceNotifier that records every call.
type fakeNotifier struct {
	mu    sync.Mutex
	calls []notifyCall
}

func (f *fakeNotifier) SendNotificationToSpecificClient(sessionID, method string, params map[string]any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	uri, _ := params["uri"].(string)
	f.calls = append(f.calls, notifyCall{sessionID: sessionID, method: method, uri: uri})
	return nil
}

func (f *fakeNotifier) snapshot() []notifyCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]notifyCall, len(f.calls))
	copy(out, f.calls)
	return out
}

func (f *fakeNotifier) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// newTestManager creates a manager whose resolver maps the given URIs to files.
func newTestManager(notifier resourceNotifier, uriToFile map[string]string) *subscriptionManager {
	resolve := func(uri string) string {
		return uriToFile[uri]
	}
	return newSubscriptionManager(notifier, resolve, testDebounce, logr.Discard())
}

// writeFile writes content to path, failing the test on error.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

func TestSubscriptionManager_NotifiesOnFileChange(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "solution.yaml")
	writeFile(t, file, "v: 1")

	uri := "solution://" + file + "/graph"
	notifier := &fakeNotifier{}
	mgr := newTestManager(notifier, map[string]string{uri: file})
	defer mgr.Close()

	mgr.Subscribe("session-1", uri)
	time.Sleep(testDebounce) // let the directory watch register

	writeFile(t, file, "v: 2")

	require.Eventually(t, func() bool {
		return notifier.count() >= 1
	}, 2*time.Second, 10*time.Millisecond, "expected a resources/updated notification")

	calls := notifier.snapshot()
	require.NotEmpty(t, calls)
	assert.Equal(t, "session-1", calls[0].sessionID)
	assert.Equal(t, mcp.MethodNotificationResourceUpdated, calls[0].method)
	assert.Equal(t, uri, calls[0].uri)
}

func TestSubscriptionManager_UnsubscribeStopsNotifications(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "solution.yaml")
	writeFile(t, file, "v: 1")

	uri := "solution://" + file
	notifier := &fakeNotifier{}
	mgr := newTestManager(notifier, map[string]string{uri: file})
	defer mgr.Close()

	mgr.Subscribe("session-1", uri)
	time.Sleep(testDebounce)
	mgr.Unsubscribe("session-1", uri)

	writeFile(t, file, "v: 2")
	time.Sleep(testSettle)

	assert.Equal(t, 0, notifier.count(), "no notification expected after unsubscribe")
}

func TestSubscriptionManager_RemoveSessionStopsNotifications(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "solution.yaml")
	writeFile(t, file, "v: 1")

	uriGraph := "solution://" + file + "/graph"
	uriSchema := "solution://" + file + "/schema"
	notifier := &fakeNotifier{}
	mgr := newTestManager(notifier, map[string]string{uriGraph: file, uriSchema: file})
	defer mgr.Close()

	mgr.Subscribe("session-1", uriGraph)
	mgr.Subscribe("session-1", uriSchema)
	time.Sleep(testDebounce)

	mgr.RemoveSession("session-1")

	writeFile(t, file, "v: 2")
	time.Sleep(testSettle)

	assert.Equal(t, 0, notifier.count(), "no notification expected after session removal")
}

func TestSubscriptionManager_MultipleSessionsNotified(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "solution.yaml")
	writeFile(t, file, "v: 1")

	uri := "solution://" + file + "/graph"
	notifier := &fakeNotifier{}
	mgr := newTestManager(notifier, map[string]string{uri: file})
	defer mgr.Close()

	mgr.Subscribe("session-1", uri)
	mgr.Subscribe("session-2", uri)
	time.Sleep(testDebounce)

	writeFile(t, file, "v: 2")

	require.Eventually(t, func() bool {
		return notifier.count() >= 2
	}, 2*time.Second, 10*time.Millisecond, "expected both sessions notified")

	sessions := map[string]bool{}
	for _, c := range notifier.snapshot() {
		sessions[c.sessionID] = true
	}
	assert.True(t, sessions["session-1"])
	assert.True(t, sessions["session-2"])
}

func TestSubscriptionManager_NonFileURINotWatched(t *testing.T) {
	notifier := &fakeNotifier{}
	// resolveFile returns "" for every URI (catalog/URL-backed solution).
	mgr := newSubscriptionManager(notifier, func(string) string { return "" }, testDebounce, logr.Discard())
	defer mgr.Close()

	mgr.Subscribe("session-1", "solution://my-catalog-solution/graph")

	mgr.mu.Lock()
	watcher := mgr.watcher
	mgr.mu.Unlock()

	assert.Nil(t, watcher, "no file watcher should be created for non-file URIs")
	assert.Equal(t, 0, notifier.count())
}

func TestSubscriptionManager_DebounceCollapsesBurst(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "solution.yaml")
	writeFile(t, file, "v: 1")

	uri := "solution://" + file + "/graph"
	notifier := &fakeNotifier{}
	mgr := newTestManager(notifier, map[string]string{uri: file})
	defer mgr.Close()

	mgr.Subscribe("session-1", uri)
	time.Sleep(testDebounce)

	for i := 0; i < 5; i++ {
		writeFile(t, file, "burst")
	}

	require.Eventually(t, func() bool {
		return notifier.count() >= 1
	}, 2*time.Second, 10*time.Millisecond)

	// Let any further (unexpected) debounced notifications settle.
	time.Sleep(testSettle)
	assert.Equal(t, 1, notifier.count(), "rapid writes should collapse into a single notification")
}

func TestSubscriptionManager_NotifiesOnAtomicSaveRename(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "solution.yaml")
	writeFile(t, file, "v: 1")

	uri := "solution://" + file + "/graph"
	notifier := &fakeNotifier{}
	mgr := newTestManager(notifier, map[string]string{uri: file})
	defer mgr.Close()

	mgr.Subscribe("session-1", uri)
	time.Sleep(testDebounce) // let the directory watch register

	// Simulate an editor's atomic save: write to a temp file in the same
	// directory, then rename it over the watched solution file.
	tmp := filepath.Join(dir, "solution.yaml.tmp")
	writeFile(t, tmp, "v: 2")
	require.NoError(t, os.Rename(tmp, file))

	require.Eventually(t, func() bool {
		return notifier.count() >= 1
	}, 2*time.Second, 10*time.Millisecond, "expected a resources/updated notification for the atomic save")

	// Let any further debounced notifications settle, then assert exactly one.
	time.Sleep(testSettle)
	require.Equal(t, 1, notifier.count(), "atomic save should yield exactly one notification")

	calls := notifier.snapshot()
	require.NotEmpty(t, calls)
	assert.Equal(t, "session-1", calls[0].sessionID)
	assert.Equal(t, mcp.MethodNotificationResourceUpdated, calls[0].method)
	assert.Equal(t, uri, calls[0].uri)
}

func TestSubscriptionManager_NotifyFileIgnoresStaleTimer(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "solution.yaml")
	writeFile(t, file, "v: 1")

	uri := "solution://" + file
	notifier := &fakeNotifier{}
	mgr := newTestManager(notifier, map[string]string{uri: file})
	defer mgr.Close()

	mgr.Subscribe("session-1", uri)
	clean := filepath.Clean(file)

	// Simulate the generation a newer event registered. A stale callback runs
	// with an older generation; the current callback runs with the matching one.
	mgr.mu.Lock()
	mgr.timerGen[clean] = 5
	mgr.mu.Unlock()

	// A stale callback (older generation) must not notify or clear state.
	mgr.notifyFile(clean, 4)
	assert.Equal(t, 0, notifier.count(), "stale generation must not deliver a notification")
	mgr.mu.Lock()
	assert.Equal(t, uint64(5), mgr.timerGen[clean], "stale callback must not change the current generation")
	mgr.mu.Unlock()

	// The current generation delivers the notification and clears the timer.
	mgr.notifyFile(clean, 5)
	assert.Equal(t, 1, notifier.count(), "current generation should deliver a notification")
	mgr.mu.Lock()
	_, ok := mgr.timers[clean]
	mgr.mu.Unlock()
	assert.False(t, ok, "timer entry should be cleared after notifying")
}

func TestSubscriptionManager_CloseIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "solution.yaml")
	writeFile(t, file, "v: 1")

	uri := "solution://" + file
	notifier := &fakeNotifier{}
	mgr := newTestManager(notifier, map[string]string{uri: file})

	mgr.Subscribe("session-1", uri)

	assert.NotPanics(t, func() {
		mgr.Close()
		mgr.Close()
	})

	// Subscriptions after close are no-ops and must not create a watcher.
	mgr.Subscribe("session-2", uri)
	mgr.mu.Lock()
	watcher := mgr.watcher
	mgr.mu.Unlock()
	assert.Nil(t, watcher)
}

func TestNewSubscriptionManager_DefaultDebounce(t *testing.T) {
	mgr := newSubscriptionManager(&fakeNotifier{}, func(string) string { return "" }, 0, logr.Discard())
	defer mgr.Close()
	assert.Equal(t, subscriptionDebounceDuration, mgr.debounce)
}

func TestServer_ResolveSubscriptionFile(t *testing.T) {
	srv, err := NewServer(WithServerVersion("test"))
	require.NoError(t, err)
	defer srv.Close()

	dir := t.TempDir()
	solPath := filepath.Join(dir, "solution.yaml")
	solYAML := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: sub-solution
  version: 1.0.0
  description: A solution for subscription resolution testing
spec:
  resolvers:
    greeting:
      type: string
      resolve:
        with:
          - provider: cel
            inputs:
              expression: "'hi'"
`
	require.NoError(t, os.WriteFile(solPath, []byte(solYAML), 0o600))
	wantPath := filepath.Clean(solPath)

	t.Run("resolves bare solution URI to file", func(t *testing.T) {
		got := srv.resolveSubscriptionFile("solution://" + solPath)
		assert.Equal(t, wantPath, got)
	})

	t.Run("strips sub-resource suffixes", func(t *testing.T) {
		for _, suffix := range []string{"/graph", "/schema", "/tests", "/graph/action", "/graph/resolver"} {
			got := srv.resolveSubscriptionFile("solution://" + solPath + suffix)
			assert.Equalf(t, wantPath, got, "suffix %q should resolve to the solution file", suffix)
		}
	})

	t.Run("returns empty for non-solution URI", func(t *testing.T) {
		assert.Empty(t, srv.resolveSubscriptionFile("provider://exec"))
	})

	t.Run("returns empty for unloadable solution", func(t *testing.T) {
		assert.Empty(t, srv.resolveSubscriptionFile("solution:///nonexistent/solution.yaml"))
	})

	t.Run("returns empty for empty name", func(t *testing.T) {
		assert.Empty(t, srv.resolveSubscriptionFile("solution://"))
	})
}

// fakeSession is a minimal server.ClientSession for exercising hooks.
type fakeSession struct {
	id string
}

func (f *fakeSession) Initialize()       {}
func (f *fakeSession) Initialized() bool { return true }
func (f *fakeSession) NotificationChannel() chan<- mcp.JSONRPCNotification {
	return make(chan mcp.JSONRPCNotification, 1)
}
func (f *fakeSession) SessionID() string { return f.id }

func TestServer_SubscriptionHooks(t *testing.T) {
	srv, err := NewServer(WithServerVersion("test"))
	require.NoError(t, err)
	defer srv.Close()

	hooks := &server.Hooks{}
	srv.registerSubscriptionHooks(hooks)
	require.Len(t, hooks.OnAfterSubscribe, 1)
	require.Len(t, hooks.OnAfterUnsubscribe, 1)
	require.Len(t, hooks.OnUnregisterSession, 1)

	dir := t.TempDir()
	file := filepath.Join(dir, "solution.yaml")
	writeFile(t, file, "v: 1")
	uri := "solution://" + file

	// Force the manager to watch our temp file regardless of solution loading.
	srv.subscriptions.resolveFile = func(string) string { return filepath.Clean(file) }

	session := &fakeSession{id: "hook-session"}
	ctx := srv.mcpServer.WithContext(context.Background(), session)

	subReq := &mcp.SubscribeRequest{}
	subReq.Params.URI = uri
	hooks.OnAfterSubscribe[0](ctx, nil, subReq, &mcp.EmptyResult{})

	srv.subscriptions.mu.Lock()
	_, subscribed := srv.subscriptions.subs["hook-session"][uri]
	srv.subscriptions.mu.Unlock()
	assert.True(t, subscribed, "subscribe hook should record the subscription")

	unsubReq := &mcp.UnsubscribeRequest{}
	unsubReq.Params.URI = uri
	hooks.OnAfterUnsubscribe[0](ctx, nil, unsubReq, &mcp.EmptyResult{})

	srv.subscriptions.mu.Lock()
	_, stillSubscribed := srv.subscriptions.subs["hook-session"][uri]
	srv.subscriptions.mu.Unlock()
	assert.False(t, stillSubscribed, "unsubscribe hook should remove the subscription")

	// Subscribe again, then unregister the session to exercise cleanup.
	hooks.OnAfterSubscribe[0](ctx, nil, subReq, &mcp.EmptyResult{})
	hooks.OnUnregisterSession[0](ctx, session)

	srv.subscriptions.mu.Lock()
	_, sessionGone := srv.subscriptions.subs["hook-session"]
	srv.subscriptions.mu.Unlock()
	assert.False(t, sessionGone, "unregister hook should drop all session subscriptions")
}

func TestServer_SubscriptionHooks_Guards(t *testing.T) {
	srv, err := NewServer(WithServerVersion("test"))
	require.NoError(t, err)
	defer srv.Close()

	hooks := &server.Hooks{}
	srv.registerSubscriptionHooks(hooks)
	ctx := context.Background()

	assert.NotPanics(t, func() {
		// nil message and missing session must be safe no-ops.
		hooks.OnAfterSubscribe[0](ctx, nil, nil, &mcp.EmptyResult{})
		hooks.OnAfterSubscribe[0](ctx, nil, &mcp.SubscribeRequest{}, &mcp.EmptyResult{})
		hooks.OnAfterUnsubscribe[0](ctx, nil, nil, &mcp.EmptyResult{})
		hooks.OnAfterUnsubscribe[0](ctx, nil, &mcp.UnsubscribeRequest{}, &mcp.EmptyResult{})
		hooks.OnUnregisterSession[0](ctx, nil)
	})
}
