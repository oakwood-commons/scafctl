package httpc

import (
	"compress/gzip"
	"io"
	"net/http"
	"strings"
)

// compressionTransport wraps an http.RoundTripper and adds automatic compression support
type compressionTransport struct {
	base http.RoundTripper
}

// newCompressionTransport creates a new compression transport
func newCompressionTransport(base http.RoundTripper) *compressionTransport {
	return &compressionTransport{
		base: base,
	}
}

// RoundTrip implements http.RoundTripper with compression support
func (t *compressionTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Add Accept-Encoding header if not present
	if req.Header.Get("Accept-Encoding") == "" {
		req.Header.Set("Accept-Encoding", "gzip, deflate")
	}

	// Execute the request
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}

	// Check if response is compressed
	encoding := resp.Header.Get("Content-Encoding")
	if encoding == "" {
		return resp, nil
	}

	// Decompress if needed
	if strings.ToLower(encoding) == "gzip" {
		gzipReader, gzipErr := gzip.NewReader(resp.Body)
		if gzipErr != nil {
			return resp, gzipErr
		}

		// Replace the body with a decompressed reader
		resp.Body = &gzipReadCloser{
			reader: gzipReader,
			closer: resp.Body,
		}

		// Remove Content-Encoding header since we're decompressing
		resp.Header.Del("Content-Encoding")
		// Remove Content-Length as it's no longer accurate
		resp.Header.Del("Content-Length")
		resp.ContentLength = -1
	}

	return resp, nil
}

// gzipReadCloser wraps a gzip reader and ensures both the reader and original closer are closed
type gzipReadCloser struct {
	reader io.ReadCloser
	closer io.Closer
}

func (g *gzipReadCloser) Read(p []byte) (n int, err error) {
	return g.reader.Read(p)
}

func (g *gzipReadCloser) Close() error {
	// Close both the gzip reader and the original body
	err1 := g.reader.Close()
	err2 := g.closer.Close()

	if err1 != nil {
		return err1
	}
	return err2
}
