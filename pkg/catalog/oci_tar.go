// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"archive/tar"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// OCILayoutVersion is the OCI image layout version.
const OCILayoutVersion = "1.0.0"

// OCITarWriter writes OCI Image Layout format to a tar archive.
type OCITarWriter struct {
	tw *tar.Writer
}

// NewOCITarWriter creates a new OCI tar writer.
func NewOCITarWriter(w io.Writer) *OCITarWriter {
	return &OCITarWriter{
		tw: tar.NewWriter(w),
	}
}

// WriteOCILayout writes the oci-layout file.
func (w *OCITarWriter) WriteOCILayout() error {
	layout := ocispec.ImageLayout{
		Version: OCILayoutVersion,
	}
	data, err := json.Marshal(layout)
	if err != nil {
		return fmt.Errorf("failed to marshal oci-layout: %w", err)
	}

	return w.writeFile("oci-layout", data)
}

// WriteIndex writes the index.json file.
func (w *OCITarWriter) WriteIndex(index ocispec.Index) error {
	data, err := json.Marshal(index)
	if err != nil {
		return fmt.Errorf("failed to marshal index: %w", err)
	}

	return w.writeFile("index.json", data)
}

// WriteBlob writes a blob to the blobs/sha256/ directory.
func (w *OCITarWriter) WriteBlob(digestStr string, data []byte) error {
	// Parse digest to get algorithm and hash
	// Format: sha256:abc123...
	parts := strings.SplitN(digestStr, ":", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid digest format: %s", digestStr)
	}
	algorithm := parts[0]
	hash := parts[1]

	blobPath := path.Join("blobs", algorithm, hash)
	return w.writeFile(blobPath, data)
}

func (w *OCITarWriter) writeFile(name string, data []byte) error {
	header := &tar.Header{
		Name: name,
		Mode: 0o644,
		Size: int64(len(data)),
	}

	if err := w.tw.WriteHeader(header); err != nil {
		return fmt.Errorf("failed to write tar header for %s: %w", name, err)
	}

	if _, err := w.tw.Write(data); err != nil {
		return fmt.Errorf("failed to write tar content for %s: %w", name, err)
	}

	return nil
}

// Close closes the tar writer.
func (w *OCITarWriter) Close() error {
	return w.tw.Close()
}

// OCITarReader reads OCI Image Layout format from a tar archive.
type OCITarReader struct {
	layout *ocispec.ImageLayout
	index  *ocispec.Index
	blobs  map[string][]byte // digest -> data
}

// NewOCITarReader reads an OCI tar archive into memory.
func NewOCITarReader(r io.Reader) (*OCITarReader, error) {
	reader := &OCITarReader{
		blobs: make(map[string][]byte),
	}

	tr := tar.NewReader(r)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read tar: %w", err)
		}

		// Read file content
		data, err := io.ReadAll(tr)
		if err != nil {
			return nil, fmt.Errorf("failed to read file %s: %w", header.Name, err)
		}

		switch {
		case header.Name == "oci-layout":
			var layout ocispec.ImageLayout
			if err := json.Unmarshal(data, &layout); err != nil {
				return nil, fmt.Errorf("failed to parse oci-layout: %w", err)
			}
			reader.layout = &layout

		case header.Name == "index.json":
			var index ocispec.Index
			if err := json.Unmarshal(data, &index); err != nil {
				return nil, fmt.Errorf("failed to parse index.json: %w", err)
			}
			reader.index = &index

		case strings.HasPrefix(header.Name, "blobs/"):
			// Extract digest from path: blobs/sha256/abc123... -> sha256:abc123...
			parts := strings.Split(header.Name, "/")
			if len(parts) == 3 {
				digestStr := parts[1] + ":" + parts[2]
				reader.blobs[digestStr] = data
			}
		}
	}

	return reader, nil
}

// HasValidLayout returns true if the archive has a valid oci-layout file.
func (r *OCITarReader) HasValidLayout() bool {
	return r.layout != nil && r.layout.Version == OCILayoutVersion
}

// Index returns the parsed index.json.
func (r *OCITarReader) Index() (*ocispec.Index, error) {
	if r.index == nil {
		return nil, fmt.Errorf("archive has no index.json")
	}
	return r.index, nil
}

// Blobs returns all blobs in the archive.
func (r *OCITarReader) Blobs() map[string][]byte {
	return r.blobs
}

// GetBlob returns a specific blob by digest.
func (r *OCITarReader) GetBlob(digest string) ([]byte, bool) {
	data, ok := r.blobs[digest]
	return data, ok
}
