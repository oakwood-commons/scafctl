// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lsp

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

const cacheURI = protocol.DocumentUri("file:///cache.yaml")

const validCacheSolution = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: cache
spec:
  resolvers:
    environment:
      resolve:
        with:
          - provider: parameter
            inputs:
              value: dev
`

// malformedCacheSolution is not valid YAML (unclosed flow mapping), so even the
// pure node-map parse fails.
const malformedCacheSolution = `apiVersion: scafctl.io/v1
kind: Solution
metadata: {name: cache
spec:
`

func TestDocumentCache_SetGetHit(t *testing.T) {
	c := NewDocumentCache()
	entry := c.Set(cacheURI, 1, validCacheSolution)

	require.NotNil(t, entry)
	assert.Equal(t, cacheURI, entry.URI)
	assert.Equal(t, int32(1), entry.Version)
	assert.Equal(t, []byte(validCacheSolution), entry.Raw)
	assert.NoError(t, entry.ParseErr)
	assert.NotNil(t, entry.Sol)
	assert.NotNil(t, entry.Index)
	assert.NotNil(t, entry.Lines)
	assert.NotEmpty(t, entry.Nodes)

	got, ok := c.Get(cacheURI)
	require.True(t, ok)
	assert.Same(t, entry, got, "Get returns the same cached snapshot without re-parsing")
}

func TestDocumentCache_GetMiss(t *testing.T) {
	c := NewDocumentCache()
	_, ok := c.Get(cacheURI)
	assert.False(t, ok)
}

func TestDocumentCache_VersionBumpInvalidates(t *testing.T) {
	c := NewDocumentCache()
	first := c.Set(cacheURI, 1, validCacheSolution)
	second := c.Set(cacheURI, 2, validCacheSolution)

	assert.NotSame(t, first, second, "a new version replaces the snapshot")
	assert.Equal(t, int32(2), second.Version)

	got, ok := c.Get(cacheURI)
	require.True(t, ok)
	assert.Same(t, second, got)
	assert.Equal(t, int32(2), got.Version)
}

func TestDocumentCache_Delete(t *testing.T) {
	c := NewDocumentCache()
	c.Set(cacheURI, 1, validCacheSolution)
	c.Delete(cacheURI)

	_, ok := c.Get(cacheURI)
	assert.False(t, ok)

	// Deleting an absent URI is a no-op.
	assert.NotPanics(t, func() { c.Delete(cacheURI) })
}

func TestDocumentCache_ParseErrorStoredNotPanicked(t *testing.T) {
	c := NewDocumentCache()
	var entry *DocEntry
	require.NotPanics(t, func() { entry = c.Set(cacheURI, 1, malformedCacheSolution) })

	require.NotNil(t, entry)
	assert.Error(t, entry.ParseErr, "a malformed document records ParseErr instead of panicking")
	assert.Nil(t, entry.Sol)
	assert.Nil(t, entry.Index)
	// Raw and Lines are always populated so raw-text features still work.
	assert.Equal(t, []byte(malformedCacheSolution), entry.Raw)
	assert.NotNil(t, entry.Lines)

	// The failed entry is still cached (features look it up and degrade).
	got, ok := c.Get(cacheURI)
	require.True(t, ok)
	assert.Same(t, entry, got)
}

func TestDocumentCache_ConcurrentGetDuringSet(t *testing.T) {
	c := NewDocumentCache()
	c.Set(cacheURI, 1, validCacheSolution)

	var wg sync.WaitGroup
	const workers = 8
	for i := 0; i < workers; i++ {
		wg.Add(2)
		go func(v int32) {
			defer wg.Done()
			c.Set(cacheURI, v, validCacheSolution)
		}(int32(i + 2))
		go func() {
			defer wg.Done()
			if e, ok := c.Get(cacheURI); ok {
				_ = e.Version
			}
		}()
	}
	wg.Wait()

	_, ok := c.Get(cacheURI)
	assert.True(t, ok)
}
