// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lsp

import (
	"sync"

	"github.com/oakwood-commons/scafctl/pkg/refindex"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/sourcepos"
	protocol "github.com/tliron/glsp/protocol_3_16"
	"gopkg.in/yaml.v3"
)

// DocEntry is an immutable snapshot of a single open document's parsed state. It
// is computed once per (URI, version) by DocumentCache.Set and shared by every
// editor feature that fires on the same keystroke (definition, references,
// hover, completion, signature help), so the document is parsed and indexed only
// once per edit instead of once per request.
//
// Every field except URI, Version, and Raw may be zero when the document fails
// to parse: features must degrade gracefully while the user is mid-edit. ParseErr
// is non-nil in that case and callers should fall back to raw-text behavior (or
// no result) rather than dereferencing Sol/Index.
type DocEntry struct {
	URI      protocol.DocumentUri
	Version  int32
	Raw      []byte
	Sol      *solution.Solution
	Index    *refindex.Index
	Lines    *sourcepos.LineIndex
	Nodes    map[string]*yaml.Node // path -> value node (refindex.NodeMap)
	ParseErr error                 // non-nil when the doc failed to parse
}

// DocumentCache stores one DocEntry per open document, keyed by URI. Entries are
// immutable once returned; Set atomically replaces the entry for a URI under a
// write lock, and Get returns the current snapshot under a read lock. Readers
// never mutate the returned *DocEntry.
type DocumentCache struct {
	mu      sync.RWMutex
	entries map[protocol.DocumentUri]*DocEntry
}

// NewDocumentCache constructs an empty document cache.
func NewDocumentCache() *DocumentCache {
	return &DocumentCache{entries: make(map[protocol.DocumentUri]*DocEntry)}
}

// Set parses and indexes text for uri at the given version, stores the resulting
// snapshot, and returns it. Parsing is done once here; every feature reuses the
// snapshot. A parse failure is captured in the entry's ParseErr (and the entry
// is still stored) so the cache never panics on malformed, mid-edit content.
func (c *DocumentCache) Set(uri protocol.DocumentUri, version int32, text string) *DocEntry {
	entry := buildDocEntry(uri, version, text)
	c.mu.Lock()
	c.entries[uri] = entry
	c.mu.Unlock()
	return entry
}

// Get returns the current snapshot for uri, if the document is open.
func (c *DocumentCache) Get(uri protocol.DocumentUri) (*DocEntry, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.entries[uri]
	return e, ok
}

// Delete drops the snapshot for uri (on document close).
func (c *DocumentCache) Delete(uri protocol.DocumentUri) {
	c.mu.Lock()
	delete(c.entries, uri)
	c.mu.Unlock()
}

// buildDocEntry performs the one-time parse+index for a document version. It is
// resilient: each stage records the first error into ParseErr and continues to
// populate whatever downstream artifacts remain computable, so features can use
// partial information (e.g. the node map) even when the solution model fails to
// build.
func buildDocEntry(uri protocol.DocumentUri, version int32, text string) *DocEntry {
	raw := []byte(text)
	entry := &DocEntry{
		URI:     uri,
		Version: version,
		Raw:     raw,
		Lines:   sourcepos.NewLineIndex(raw, uriToPath(uri)),
	}

	// The value-node map is a pure YAML parse independent of the solution model;
	// compute it even when the solution fails to unmarshal so cursor-context
	// features still have something to work with.
	if nodes, err := refindex.NodeMap(raw); err != nil {
		entry.ParseErr = err
	} else {
		entry.Nodes = nodes
	}

	sol := &solution.Solution{}
	if err := sol.UnmarshalFromBytes(raw); err != nil {
		if entry.ParseErr == nil {
			entry.ParseErr = err
		}
		return entry
	}
	entry.Sol = sol

	idx, err := refindex.Build(sol)
	if err != nil {
		if entry.ParseErr == nil {
			entry.ParseErr = err
		}
		return entry
	}
	entry.Index = idx

	return entry
}
