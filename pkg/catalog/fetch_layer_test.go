// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/content"
)

// descriptorFor builds an OCI descriptor matching the supplied content.
func descriptorFor(data []byte) ocispec.Descriptor {
	return ocispec.Descriptor{
		MediaType: "application/octet-stream",
		Digest:    digest.FromBytes(data),
		Size:      int64(len(data)),
	}
}

// fetcherReturning adapts a byte slice into a content.Fetcher that serves it.
func fetcherReturning(data []byte) content.Fetcher {
	return content.FetcherFunc(func(_ context.Context, _ ocispec.Descriptor) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(string(data))), nil
	})
}

func TestFetchLayerContent_Success(t *testing.T) {
	data := []byte("hello plugin binary content")
	desc := descriptorFor(data)

	got, err := fetchLayerContent(context.Background(), fetcherReturning(data), desc)
	require.NoError(t, err)
	assert.Equal(t, data, got)
}

func TestFetchLayerContent_BypassesOras32MiBCap(t *testing.T) {
	// oras-go's content.FetchAll/ReadAll caps reads at 32 MiB. fetchLayerContent
	// must read larger layers. Use 33 MiB to prove the cap is bypassed.
	data := make([]byte, 33<<20)
	for i := range data {
		data[i] = byte(i % 251)
	}
	desc := descriptorFor(data)

	got, err := fetchLayerContent(context.Background(), fetcherReturning(data), desc)
	require.NoError(t, err)
	assert.Len(t, got, len(data))
	assert.Equal(t, data, got)
}

func TestFetchLayerContent_NegativeSize(t *testing.T) {
	desc := ocispec.Descriptor{
		Digest: digest.FromBytes([]byte("x")),
		Size:   -1,
	}

	_, err := fetchLayerContent(context.Background(), fetcherReturning([]byte("x")), desc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid layer size")
}

func TestFetchLayerContent_ExceedsMaxSize(t *testing.T) {
	// Temporarily lower the active bound so we can exercise the size guard
	// without allocating a gigabyte-scale buffer.
	original := maxLayerSize
	maxLayerSize = 8
	defer func() { maxLayerSize = original }()

	data := []byte("this content is definitely longer than eight bytes")
	desc := descriptorFor(data)

	_, err := fetchLayerContent(context.Background(), fetcherReturning(data), desc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds maximum allowed size")
}

func TestFetchLayerContent_DisabledBoundAllowsAnySize(t *testing.T) {
	// A non-positive bound disables the size check.
	original := maxLayerSize
	maxLayerSize = 0
	defer func() { maxLayerSize = original }()

	data := []byte("unbounded read is permitted")
	desc := descriptorFor(data)

	got, err := fetchLayerContent(context.Background(), fetcherReturning(data), desc)
	require.NoError(t, err)
	assert.Equal(t, data, got)
}

func TestFetchLayerContent_FetcherError(t *testing.T) {
	wantErr := errors.New("registry unreachable")
	fetcher := content.FetcherFunc(func(_ context.Context, _ ocispec.Descriptor) (io.ReadCloser, error) {
		return nil, wantErr
	})

	_, err := fetchLayerContent(context.Background(), fetcher, descriptorFor([]byte("x")))
	require.Error(t, err)
	assert.ErrorIs(t, err, wantErr)
}

func TestFetchLayerContent_DigestMismatch(t *testing.T) {
	data := []byte("the real content served by the registry")
	// Descriptor advertises a digest for different content of the same length.
	tampered := []byte("the fake content of an exact equal len.")
	require.Equal(t, len(data), len(tampered))
	desc := descriptorFor(tampered)

	_, err := fetchLayerContent(context.Background(), fetcherReturning(data), desc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "verifying layer content")
}

func TestFetchLayerContent_TruncatedContent(t *testing.T) {
	full := []byte("the descriptor claims more bytes than the reader provides")
	desc := descriptorFor(full)
	// Serve fewer bytes than the descriptor's declared size.
	short := full[:len(full)-10]

	_, err := fetchLayerContent(context.Background(), fetcherReturning(short), desc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading layer content")
}
