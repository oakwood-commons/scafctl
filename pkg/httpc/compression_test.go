// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package httpc

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompressionTransport_Gzip(t *testing.T) {
	// Create test data
	testData := []byte("This is test data that should be compressed")

	// Create a server that returns gzipped data
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify Accept-Encoding header
		assert.Contains(t, r.Header.Get("Accept-Encoding"), "gzip")

		// Compress the response
		w.Header().Set("Content-Encoding", "gzip")
		gzipWriter := gzip.NewWriter(w)
		gzipWriter.Write(testData)
		gzipWriter.Close()
	}))
	defer server.Close()

	// Create transport with compression
	transport := newCompressionTransport(http.DefaultTransport)

	// Make request
	req, err := http.NewRequestWithContext(context.Background(), "GET", server.URL, nil)
	require.NoError(t, err)

	resp, err := transport.RoundTrip(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Verify response is decompressed
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, testData, body)

	// Verify Content-Encoding header is removed after decompression
	assert.Empty(t, resp.Header.Get("Content-Encoding"))
}

func TestCompressionTransport_NoCompression(t *testing.T) {
	testData := []byte("This is uncompressed data")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(testData)
	}))
	defer server.Close()

	transport := newCompressionTransport(http.DefaultTransport)

	req, err := http.NewRequestWithContext(context.Background(), "GET", server.URL, nil)
	require.NoError(t, err)

	resp, err := transport.RoundTrip(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, testData, body)
}

func TestCompressionTransport_AcceptEncodingPreserved(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Should use custom Accept-Encoding if provided
		assert.Equal(t, "custom-encoding", r.Header.Get("Accept-Encoding"))
		w.Write([]byte("ok"))
	}))
	defer server.Close()

	transport := newCompressionTransport(http.DefaultTransport)

	req, err := http.NewRequestWithContext(context.Background(), "GET", server.URL, nil)
	require.NoError(t, err)
	req.Header.Set("Accept-Encoding", "custom-encoding")

	resp, err := transport.RoundTrip(req)
	require.NoError(t, err)
	defer resp.Body.Close()
}

func TestGzipReadCloser_Close(t *testing.T) {
	// Create gzipped data
	var buf bytes.Buffer
	gzipWriter := gzip.NewWriter(&buf)
	gzipWriter.Write([]byte("test data"))
	gzipWriter.Close()

	// Create readers
	gzipReader, err := gzip.NewReader(&buf)
	require.NoError(t, err)

	closer := io.NopCloser(bytes.NewReader(buf.Bytes()))

	grc := &gzipReadCloser{
		reader: gzipReader,
		closer: closer,
	}

	// Read data
	data, err := io.ReadAll(grc)
	require.NoError(t, err)
	assert.Equal(t, []byte("test data"), data)

	// Close should not error
	err = grc.Close()
	assert.NoError(t, err)
}

func TestCompressionTransport_InvalidGzip(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Encoding", "gzip")
		// Write invalid gzip data
		w.Write([]byte("not gzipped data"))
	}))
	defer server.Close()

	transport := newCompressionTransport(http.DefaultTransport)

	req, err := http.NewRequestWithContext(context.Background(), "GET", server.URL, nil)
	require.NoError(t, err)

	resp, err := transport.RoundTrip(req)
	// The RoundTrip itself can fail when trying to create the gzip reader
	// This is expected behavior for invalid gzip data
	if err != nil {
		assert.Contains(t, err.Error(), "gzip")
		return
	}

	// Or the error might occur when reading the body
	_, err = io.ReadAll(resp.Body)
	if err != nil {
		assert.Contains(t, err.Error(), "gzip")
	}
	resp.Body.Close()
}
