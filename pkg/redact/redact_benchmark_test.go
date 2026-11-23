// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package redact

import (
	"io"
	"testing"
)

func BenchmarkTracker_RedactString(b *testing.B) {
	tr := NewTracker()
	tr.Track("ghp_1234567890abcdefABCDEF1234567890")
	tr.Track("eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.payload.signature")
	tr.Track("npm_secret_publish_token_xyz987654321")

	sample := "Deploying package with Authorization: Bearer ghp_1234567890abcdefABCDEF1234567890 to registry"

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = tr.RedactString(sample)
	}
}

func BenchmarkStreamWriter_Write(b *testing.B) {
	tr := NewTracker()
	tr.Track("ghp_1234567890abcdefABCDEF1234567890")
	chunk := []byte("step output line with token ghp_1234567890abcdefABCDEF1234567890 and status ok\n")

	sw := NewStreamWriter(io.Discard, tr)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = sw.Write(chunk)
	}
	_ = sw.Flush()
}
