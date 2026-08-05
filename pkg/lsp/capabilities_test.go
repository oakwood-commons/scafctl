// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lsp

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

// TestDefaultFeatures_DeterministicOrder guards the registration order so the
// seam is predictable for two developers adding entries. It also enforces the
// documented alphabetical ordering so a mis-placed insertion is caught even if
// the exact-literal list below is updated to match.
func TestDefaultFeatures_DeterministicOrder(t *testing.T) {
	features := defaultFeatures()
	got := make([]string, 0, len(features))
	for _, f := range features {
		got = append(got, f.name)
		require.NotNil(t, f.wire, "every feature must wire a handler")
	}
	assert.Equal(t, []string{"documentSync", "hover", "navigation", "rename", "symbols"}, got)
	assert.True(t, sort.StringsAreSorted(got), "features must be registered in alphabetical order")
}

// TestWireFeatures_WiresEveryFeature proves N registered features attach N sets
// of handler callbacks, in order.
func TestWireFeatures_WiresEveryFeature(t *testing.T) {
	var order []string
	s := &Server{features: []feature{
		{name: "a", wire: func(_ *protocol.Handler, _ *Server) { order = append(order, "a") }},
		{name: "b", wire: func(_ *protocol.Handler, _ *Server) { order = append(order, "b") }},
		{name: "c", wire: func(_ *protocol.Handler, _ *Server) { order = append(order, "c") }},
	}}

	s.wireFeatures(&protocol.Handler{})
	assert.Equal(t, []string{"a", "b", "c"}, order, "features wire in registration order")
}

// TestAdvertiseFeatures_SkipsNilAdvertise proves advertise runs for each feature
// that has one, in order, and nil-advertise features are skipped without panic.
func TestAdvertiseFeatures_SkipsNilAdvertise(t *testing.T) {
	var order []string
	s := &Server{features: []feature{
		{name: "a", wire: noopWire, advertise: func(_ *protocol.ServerCapabilities) { order = append(order, "a") }},
		{name: "b", wire: noopWire}, // no advertise
		{name: "c", wire: noopWire, advertise: func(_ *protocol.ServerCapabilities) { order = append(order, "c") }},
	}}

	require.NotPanics(t, func() { s.advertiseFeatures(&protocol.ServerCapabilities{}) })
	assert.Equal(t, []string{"a", "c"}, order, "only features with an advertise func run, in order")
}

// TestHandler_WiresDefaultFeatureCallbacks asserts the default feature set
// attaches every existing handler callback (behavioral-parity anchor for the
// migration to the table).
func TestHandler_WiresDefaultFeatureCallbacks(t *testing.T) {
	s := newTestServer(t)
	h := s.Handler()

	assert.NotNil(t, h.TextDocumentDidOpen, "documentSync: didOpen")
	assert.NotNil(t, h.TextDocumentDidChange, "documentSync: didChange")
	assert.NotNil(t, h.TextDocumentDidSave, "documentSync: didSave")
	assert.NotNil(t, h.TextDocumentDidClose, "documentSync: didClose")
	assert.NotNil(t, h.TextDocumentDefinition, "navigation: definition")
	assert.NotNil(t, h.TextDocumentReferences, "navigation: references")
	assert.NotNil(t, h.TextDocumentPrepareRename, "rename: prepareRename")
	assert.NotNil(t, h.TextDocumentRename, "rename: rename")
}

// TestDocumentSyncFeature_AdvertisesFullSync checks the computed capability set
// by the documentSync feature independent of the whole Initialize flow.
func TestDocumentSyncFeature_AdvertisesFullSync(t *testing.T) {
	f := documentSyncFeature()
	require.NotNil(t, f.advertise)

	full := protocol.TextDocumentSyncKindNone
	caps := protocol.ServerCapabilities{
		TextDocumentSync: &protocol.TextDocumentSyncOptions{Change: &full},
	}
	f.advertise(&caps)

	sync, ok := caps.TextDocumentSync.(*protocol.TextDocumentSyncOptions)
	require.True(t, ok)
	require.NotNil(t, sync.Change)
	assert.Equal(t, protocol.TextDocumentSyncKindFull, *sync.Change)
}

// TestRenameFeature_AdvertisesPrepare checks the rename feature advertises
// prepare support as a computed RenameOptions value.
func TestRenameFeature_AdvertisesPrepare(t *testing.T) {
	f := renameFeature()
	require.NotNil(t, f.advertise)

	var caps protocol.ServerCapabilities
	f.advertise(&caps)

	rename, ok := caps.RenameProvider.(*protocol.RenameOptions)
	require.True(t, ok)
	require.NotNil(t, rename.PrepareProvider)
	assert.True(t, *rename.PrepareProvider)
}

func noopWire(_ *protocol.Handler, _ *Server) {}
