// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/go-logr/logr"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/oakwood-commons/scafctl/pkg/solution/inspect"
)

// solutionResourceSuffixes are the routing suffixes appended to a solution URI
// for its sub-resources. They are stripped when resolving the backing file so a
// subscription to any sub-resource watches the same solution file.
var solutionResourceSuffixes = []string{
	"/graph/action",
	"/graph/resolver",
	"/graph",
	"/schema",
	"/tests",
}

// registerSubscriptionHooks wires resource subscription lifecycle hooks onto the
// given hooks struct. The closures reference s.subscriptions lazily so they work
// once the manager is initialized after the mcp-go server is built.
func (s *Server) registerSubscriptionHooks(hooks *server.Hooks) {
	hooks.AddAfterSubscribe(func(ctx context.Context, _ any, message *mcp.SubscribeRequest, _ *mcp.EmptyResult) {
		if s.subscriptions == nil || message == nil {
			return
		}
		sess := server.ClientSessionFromContext(ctx)
		if sess == nil {
			return
		}
		s.subscriptions.Subscribe(sess.SessionID(), message.Params.URI)
	})

	hooks.AddAfterUnsubscribe(func(ctx context.Context, _ any, message *mcp.UnsubscribeRequest, _ *mcp.EmptyResult) {
		if s.subscriptions == nil || message == nil {
			return
		}
		sess := server.ClientSessionFromContext(ctx)
		if sess == nil {
			return
		}
		s.subscriptions.Unsubscribe(sess.SessionID(), message.Params.URI)
	})

	hooks.AddOnUnregisterSession(func(_ context.Context, session server.ClientSession) {
		if s.subscriptions == nil || session == nil {
			return
		}
		s.subscriptions.RemoveSession(session.SessionID())
	})
}

// resolveSubscriptionFile maps a solution:// resource URI to an absolute local
// file path for watching. It returns an empty string when the URI is not a
// solution resource or does not resolve to a watchable local file (e.g. a
// catalog name or URL), in which case the subscription is acknowledged but not
// watched.
func (s *Server) resolveSubscriptionFile(uri string) string {
	name := extractNameFromURI(uri, "solution://")
	if name == "" {
		return ""
	}
	for _, suffix := range solutionResourceSuffixes {
		if strings.HasSuffix(name, suffix) {
			name = strings.TrimSuffix(name, suffix)
			break
		}
	}
	if name == "" {
		return ""
	}

	// Subscriptions watch local files only. Avoid invoking the solution loader
	// (which performs catalog resolution and may reach out to remote catalogs)
	// for URIs that do not point at an existing local path, such as catalog
	// names or URLs.
	if _, err := os.Stat(name); err != nil {
		return ""
	}

	sol, err := inspect.LoadSolution(s.ctx, name)
	if err != nil {
		s.logger.V(1).Info("mcp subscription: cannot load solution for watch", "uri", uri, "error", err)
		return ""
	}

	path := sol.GetPath()
	if path == "" {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return ""
	}
	info, err := os.Stat(abs)
	if err != nil || info.IsDir() {
		return ""
	}
	return filepath.Clean(abs)
}

// subscriptionDebounceDuration is how long the subscription manager waits after
// the last file change before emitting a resources/updated notification. Rapid
// successive writes (e.g. an editor save-all) are collapsed into a single
// notification.
const subscriptionDebounceDuration = 300 * time.Millisecond

// resourceNotifier sends MCP notifications to a specific client session. It is
// satisfied by *server.MCPServer and abstracted here so the subscription
// manager can be unit tested without a live server.
type resourceNotifier interface {
	SendNotificationToSpecificClient(sessionID, method string, params map[string]any) error
}

// subscription is a single (session, uri) subscription and the local file that
// backs it. file is empty when the URI does not resolve to a watchable local
// file (e.g. a catalog or URL-backed solution).
type subscription struct {
	sessionID string
	uri       string
	file      string
}

// subscriptionManager tracks MCP resource subscriptions and delivers live
// resources/updated notifications when the backing solution file changes on
// disk. It watches the parent directory of each subscribed file so that
// atomic-save (rename) editors are handled robustly, and debounces change
// bursts before notifying.
type subscriptionManager struct {
	logger      logr.Logger
	notifier    resourceNotifier
	resolveFile func(uri string) string
	debounce    time.Duration

	mu      sync.Mutex
	watcher *fsnotify.Watcher
	closed  bool

	// subs maps sessionID -> uri -> subscription.
	subs map[string]map[string]subscription
	// fileRefs counts how many subscriptions watch each file.
	fileRefs map[string]int
	// dirRefs counts how many watched files live under each directory.
	dirRefs map[string]int
	// timers holds the active debounce timer per file.
	timers map[string]*time.Timer
	// timerGen is the generation of the active debounce timer per file. A
	// debounce callback captures its generation by value and only notifies when
	// it still matches, so a stale callback that fired after a newer event
	// replaced its timer is ignored.
	timerGen map[string]uint64

	wg sync.WaitGroup
}

// newSubscriptionManager creates a subscription manager. resolveFile maps a
// subscribed URI to an absolute local file path, returning an empty string when
// the URI is not backed by a watchable local file. A non-positive debounce
// falls back to subscriptionDebounceDuration.
func newSubscriptionManager(notifier resourceNotifier, resolveFile func(uri string) string, debounce time.Duration, lgr logr.Logger) *subscriptionManager {
	if debounce <= 0 {
		debounce = subscriptionDebounceDuration
	}
	return &subscriptionManager{
		logger:      lgr,
		notifier:    notifier,
		resolveFile: resolveFile,
		debounce:    debounce,
		subs:        make(map[string]map[string]subscription),
		fileRefs:    make(map[string]int),
		dirRefs:     make(map[string]int),
		timers:      make(map[string]*time.Timer),
		timerGen:    make(map[string]uint64),
	}
}

// Subscribe records a subscription for the given session and URI and begins
// watching the backing file when the URI resolves to a local file. Subscribing
// the same (session, uri) again refreshes the resolved file.
func (m *subscriptionManager) Subscribe(sessionID, uri string) {
	var file string
	if m.resolveFile != nil {
		file = m.resolveFile(uri)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return
	}

	if m.subs[sessionID] == nil {
		m.subs[sessionID] = make(map[string]subscription)
	}
	if prev, ok := m.subs[sessionID][uri]; ok {
		m.unwatchFileLocked(prev.file)
	}
	m.subs[sessionID][uri] = subscription{sessionID: sessionID, uri: uri, file: file}
	m.watchFileLocked(file)
}

// Unsubscribe removes a single (session, uri) subscription and stops watching
// the backing file when no other subscription references it.
func (m *subscriptionManager) Unsubscribe(sessionID, uri string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	uris := m.subs[sessionID]
	if uris == nil {
		return
	}
	if sub, ok := uris[uri]; ok {
		m.unwatchFileLocked(sub.file)
		delete(uris, uri)
		if len(uris) == 0 {
			delete(m.subs, sessionID)
		}
	}
}

// RemoveSession drops all subscriptions for a session, typically when the
// client session is unregistered.
func (m *subscriptionManager) RemoveSession(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, sub := range m.subs[sessionID] {
		m.unwatchFileLocked(sub.file)
	}
	delete(m.subs, sessionID)
}

// Close stops all watching and releases resources. It is safe to call multiple
// times and concurrently with the watch loop.
func (m *subscriptionManager) Close() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	for _, t := range m.timers {
		t.Stop()
	}
	m.timers = make(map[string]*time.Timer)
	m.timerGen = make(map[string]uint64)
	w := m.watcher
	m.watcher = nil
	m.mu.Unlock()

	if w != nil {
		if err := w.Close(); err != nil {
			m.logger.V(1).Info("mcp subscription: error closing file watcher", "error", err)
		}
	}
	m.wg.Wait()
}

// watchFileLocked begins watching the parent directory of file. Callers must
// hold m.mu.
func (m *subscriptionManager) watchFileLocked(file string) {
	if file == "" {
		return
	}
	if m.fileRefs[file] == 0 {
		dir := filepath.Dir(file)
		if m.dirRefs[dir] == 0 {
			if err := m.ensureWatcherLocked(); err != nil {
				m.logger.Error(err, "mcp subscription: failed to create file watcher", "file", file)
				return
			}
			if err := m.watcher.Add(dir); err != nil {
				m.logger.Error(err, "mcp subscription: failed to watch directory", "dir", dir)
				return
			}
		}
		m.dirRefs[dir]++
	}
	m.fileRefs[file]++
}

// unwatchFileLocked decrements the reference counts for file and stops watching
// its parent directory once no files under it remain. Callers must hold m.mu.
func (m *subscriptionManager) unwatchFileLocked(file string) {
	if file == "" {
		return
	}
	if m.fileRefs[file] == 0 {
		return
	}
	m.fileRefs[file]--
	if m.fileRefs[file] > 0 {
		return
	}
	delete(m.fileRefs, file)
	if t := m.timers[file]; t != nil {
		t.Stop()
		delete(m.timers, file)
	}
	delete(m.timerGen, file)

	dir := filepath.Dir(file)
	if m.dirRefs[dir] == 0 {
		return
	}
	m.dirRefs[dir]--
	if m.dirRefs[dir] > 0 {
		return
	}
	delete(m.dirRefs, dir)
	if m.watcher != nil {
		if err := m.watcher.Remove(dir); err != nil {
			m.logger.V(1).Info("mcp subscription: failed to unwatch directory", "dir", dir, "error", err)
		}
	}
}

// ensureWatcherLocked lazily creates the fsnotify watcher and starts the watch
// loop. Callers must hold m.mu.
func (m *subscriptionManager) ensureWatcherLocked() error {
	if m.watcher != nil {
		return nil
	}
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	m.watcher = w
	m.wg.Add(1)
	go m.watchLoop(w)
	return nil
}

// watchLoop consumes filesystem events until the watcher is closed.
func (m *subscriptionManager) watchLoop(w *fsnotify.Watcher) {
	defer m.wg.Done()
	for {
		select {
		case event, ok := <-w.Events:
			if !ok {
				return
			}
			if !event.Has(fsnotify.Write) && !event.Has(fsnotify.Create) && !event.Has(fsnotify.Rename) {
				continue
			}
			m.handleFileEvent(event.Name)

		case err, ok := <-w.Errors:
			if !ok {
				return
			}
			m.logger.V(1).Info("mcp subscription: file watcher error", "error", err)
		}
	}
}

// handleFileEvent schedules a debounced notification for the changed file when
// it is currently being watched.
func (m *subscriptionManager) handleFileEvent(path string) {
	clean := filepath.Clean(path)

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return
	}
	if m.fileRefs[clean] == 0 {
		return
	}
	if t := m.timers[clean]; t != nil {
		t.Stop()
	}
	m.timerGen[clean]++
	gen := m.timerGen[clean]
	m.timers[clean] = time.AfterFunc(m.debounce, func() { m.notifyFile(clean, gen) })
}

// notifyFile sends a resources/updated notification to every session subscribed
// to a URI backed by the given file. gen is the debounce generation that
// scheduled this call; the notification is skipped if a newer event has already
// bumped the generation, so the debounced delivery is left to that newer timer.
func (m *subscriptionManager) notifyFile(file string, gen uint64) {
	m.mu.Lock()
	// Only act when this callback is still the current debounce generation for
	// the file. A newer event may have stored a fresh timer after this one fired
	// but before it acquired the lock; in that case do nothing and let the newer
	// timer deliver the debounced notification.
	if m.timerGen[file] != gen {
		m.mu.Unlock()
		return
	}
	delete(m.timers, file)
	if m.closed {
		m.mu.Unlock()
		return
	}
	var targets []subscription
	for _, uris := range m.subs {
		for _, sub := range uris {
			if sub.file == file {
				targets = append(targets, sub)
			}
		}
	}
	m.mu.Unlock()

	for _, t := range targets {
		err := m.notifier.SendNotificationToSpecificClient(
			t.sessionID,
			mcp.MethodNotificationResourceUpdated,
			map[string]any{"uri": t.uri},
		)
		if err != nil {
			m.logger.V(1).Info("mcp subscription: failed to send resource update",
				"sessionID", t.sessionID, "uri", t.uri, "error", err)
		}
	}
}
