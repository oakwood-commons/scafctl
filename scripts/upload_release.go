// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

//go:build ignore

package main

import (
	"context"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strings"

	"cloud.google.com/go/storage"
)

func main() {
	distDir := envOrArg(1, "UPLOAD_DIST_DIR", "dist")
	bucket := envOrArg(2, "GCS_BUCKET", "")
	prefix := envOrArg(3, "GCS_PREFIX", "")

	if bucket == "" {
		fmt.Fprintf(os.Stderr, "Error: GCS bucket is required.\n")
		fmt.Fprintf(os.Stderr, "Usage: %s [dist-dir] <bucket> [prefix]\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  Or set GCS_BUCKET environment variable.\n")
		os.Exit(1)
	}

	files, err := discoverFiles(distDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error discovering files in %s: %v\n", distDir, err)
		os.Exit(1)
	}

	if len(files) == 0 {
		fmt.Fprintf(os.Stderr, "Error: no uploadable files found in %s\n", distDir)
		os.Exit(1)
	}

	ctx := context.Background()

	client, err := storage.NewClient(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating GCS client: %v\n", err)
		fmt.Fprintf(os.Stderr, "  Ensure credentials are configured (gcloud auth application-default login).\n")
		os.Exit(1)
	}
	defer client.Close()

	bkt := client.Bucket(bucket)

	fmt.Fprintf(os.Stderr, "Uploading %d files to gs://%s/%s\n", len(files), bucket, prefix)

	for _, file := range files {
		objectName := prefix + filepath.Base(file)
		contentType := detectContentType(file)

		if err := uploadFile(ctx, bkt, file, objectName, contentType); err != nil {
			fmt.Fprintf(os.Stderr, "Error uploading %s: %v\n", filepath.Base(file), err)
			os.Exit(1)
		}

		fmt.Fprintf(os.Stderr, "  ✓ %s → gs://%s/%s (%s)\n", filepath.Base(file), bucket, objectName, contentType)
	}

	fmt.Fprintf(os.Stderr, "\nSuccessfully uploaded %d files to gs://%s/%s\n", len(files), bucket, prefix)
}

// envOrArg returns the CLI argument at the given 1-based position if present,
// otherwise falls back to the named environment variable, then to the default.
func envOrArg(pos int, envVar, defaultVal string) string {
	if pos < len(os.Args) {
		return os.Args[pos]
	}
	if v := os.Getenv(envVar); v != "" {
		return v
	}
	return defaultVal
}

// discoverFiles scans the dist directory and returns paths to all uploadable
// release files: index.html, platform archives (.tar.gz, .zip), and SHA256SUMS.
func discoverFiles(distDir string) ([]string, error) {
	entries, err := os.ReadDir(distDir)
	if err != nil {
		return nil, fmt.Errorf("reading directory: %w", err)
	}

	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}

		name := e.Name()
		switch {
		case name == "index.html":
			files = append(files, filepath.Join(distDir, name))
		case strings.HasSuffix(name, ".tar.gz"):
			files = append(files, filepath.Join(distDir, name))
		case strings.HasSuffix(name, ".zip"):
			files = append(files, filepath.Join(distDir, name))
		case strings.HasSuffix(name, "_SHA256SUMS"):
			files = append(files, filepath.Join(distDir, name))
		}
	}

	return files, nil
}

// detectContentType returns the appropriate Content-Type for the given file path.
func detectContentType(path string) string {
	name := filepath.Base(path)

	switch {
	case strings.HasSuffix(name, ".tar.gz"):
		return "application/gzip"
	case strings.HasSuffix(name, ".zip"):
		return "application/zip"
	case strings.HasSuffix(name, "_SHA256SUMS"):
		return "text/plain; charset=utf-8"
	}

	if ct := mime.TypeByExtension(filepath.Ext(name)); ct != "" {
		return ct
	}

	return "application/octet-stream"
}

// uploadFile uploads a local file to the specified GCS object with the given content type.
func uploadFile(ctx context.Context, bkt *storage.BucketHandle, localPath, objectName, contentType string) error {
	f, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("opening file: %w", err)
	}
	defer f.Close()

	w := bkt.Object(objectName).NewWriter(ctx)
	w.ContentType = contentType

	if _, err := io.Copy(w, f); err != nil {
		w.Close()
		return fmt.Errorf("copying data: %w", err)
	}

	if err := w.Close(); err != nil {
		return fmt.Errorf("finalizing upload: %w", err)
	}

	return nil
}
