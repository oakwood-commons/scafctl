// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOCITarReader_Index_Nil(t *testing.T) {
	// An empty reader has no index.json parsed, so Index() should return error
	reader := &OCITarReader{
		blobs: make(map[string][]byte),
	}
	idx, err := reader.Index()
	require.Error(t, err)
	assert.Nil(t, idx)
	assert.Contains(t, err.Error(), "no index.json")
}

func TestOCITarWriter_RoundTrip(t *testing.T) {
	var buf bytes.Buffer
	w := NewOCITarWriter(&buf)

	// Write an oci-layout file
	err := w.WriteOCILayout()
	require.NoError(t, err)

	err = w.Close()
	require.NoError(t, err)

	// Read it back via NewOCITarReader
	r, err := NewOCITarReader(bytes.NewReader(buf.Bytes()))
	require.NoError(t, err)
	assert.NotNil(t, r)
	assert.NotNil(t, r.Blobs())
}

func TestOCITarReader_GetBlob(t *testing.T) {
	reader := &OCITarReader{
		blobs: map[string][]byte{
			"sha256:abc": []byte("content"),
		},
	}
	data, ok := reader.GetBlob("sha256:abc")
	assert.True(t, ok)
	assert.Equal(t, []byte("content"), data)

	_, ok = reader.GetBlob("sha256:missing")
	assert.False(t, ok)
}
