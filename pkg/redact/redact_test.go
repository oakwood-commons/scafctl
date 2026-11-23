// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package redact

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type customExitErr struct {
	code int
	msg  string
}

func (e *customExitErr) Error() string { return e.msg }

func TestTracker_NilAndEmptySafety(t *testing.T) {
	var nilTracker *Tracker
	nilTracker.Track("super-secret-value")
	nilTracker.TrackValue("another-secret")
	assert.Equal(t, 0, nilTracker.Count())
	assert.Equal(t, 0, nilTracker.MaxSecretLen())
	assert.False(t, nilTracker.HasMultiline())
	assert.Equal(t, "plain", nilTracker.RedactString("plain"))
	assert.NoError(t, nilTracker.RedactError(nil))

	origErr := errors.New("boom")
	assert.Same(t, origErr, nilTracker.RedactError(origErr))

	m := map[string]any{"k": "v"}
	assert.Equal(t, m, nilTracker.RedactMap(m))
	assert.Equal(t, []string{"a"}, nilTracker.RedactSlice([]string{"a"}))
	assert.Equal(t, "x", nilTracker.RedactAny("x"))

	var buf bytes.Buffer
	nilTracker.EmitGitHubActionsMasks(&buf)
	assert.Empty(t, buf.String())

	assert.Nil(t, FromContext(nil)) //nolint:staticcheck // intentional nil ctx check
	assert.Nil(t, FromContext(context.Background()))
	ctx := WithTracker(context.Background(), nil)
	assert.Nil(t, FromContext(ctx))
}

func TestTracker_ShortStringsIgnored(t *testing.T) {
	tr := NewTracker()
	tr.Track("")
	tr.Track("   ")
	tr.Track("abc") // < MinSecretLength (4)
	assert.Equal(t, 0, tr.Count())
	assert.Equal(t, "abc value", tr.RedactString("abc value"))
}

func TestTracker_LongestFirstOverlap(t *testing.T) {
	tr := NewTracker()
	tr.Track("ghp_short")
	tr.Track("ghp_short_extended_token")

	got := tr.RedactString("header=ghp_short_extended_token fallback=ghp_short")
	assert.Equal(t, "header=[REDACTED] fallback=[REDACTED]", got)
}

func TestTracker_MultilinePEMSecrets(t *testing.T) {
	pem := "-----BEGIN PRIVATE KEY-----\nMIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcwggSjAgEAAoIBAQC7\n{\n}\nabc\n-----END PRIVATE KEY-----\n"
	tr := NewTracker()
	tr.Track(pem)
	assert.True(t, tr.HasMultiline())

	// Individual PEM body line (>= 4 chars) is scrubbed, while short lines ("{", "}", "abc") are not tracked.
	out := tr.RedactString(`{"line": "MIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcwggSjAgEAAoIBAQC7", "keep": "abc"}`)
	assert.Equal(t, `{"line": "[REDACTED]", "keep": "abc"}`, out)
}

func TestTracker_TrackValueAndRedactAnyClone(t *testing.T) {
	tr := NewTracker()
	tr.TrackValue(map[string]any{
		"nested": []any{
			"derived-secret-1234",
			[]byte("byte-secret-5678"),
			map[string]string{"inner": "inner-secret-9012"},
		},
		"list": []string{"slice-secret-3456"},
	})

	raw := map[string]any{
		"msg":   "leaked derived-secret-1234 and byte-secret-5678",
		"items": []any{"ok", "inner-secret-9012"},
		"tags":  []string{"slice-secret-3456", "public"},
		"kv":    map[string]string{"k": "derived-secret-1234"},
		"count": 42,
	}

	redacted := tr.RedactMap(raw)
	// Original map must not be mutated.
	assert.Equal(t, "leaked derived-secret-1234 and byte-secret-5678", raw["msg"])

	assert.Equal(t, "leaked [REDACTED] and [REDACTED]", redacted["msg"])
	assert.Equal(t, []any{"ok", "[REDACTED]"}, redacted["items"])
	assert.Equal(t, []string{"[REDACTED]", "public"}, redacted["tags"])
	assert.Equal(t, map[string]string{"k": "[REDACTED]"}, redacted["kv"])
	assert.Equal(t, 42, redacted["count"])
}

func TestTracker_RedactErrorPreservesIsAndAs(t *testing.T) {
	tr := NewTracker()
	tr.Track("secret-bearer-xyz")

	inner := &customExitErr{code: 6, msg: "request failed with Authorization: Bearer secret-bearer-xyz"}
	wrapped := fmt.Errorf("http call: %w: %w", context.DeadlineExceeded, inner)

	scrubbed := tr.RedactError(wrapped)
	require.Error(t, scrubbed)
	assert.NotContains(t, scrubbed.Error(), "secret-bearer-xyz")
	assert.Contains(t, scrubbed.Error(), "[REDACTED]")
	assert.ErrorIs(t, scrubbed, context.DeadlineExceeded)

	var target *customExitErr
	require.ErrorAs(t, scrubbed, &target)
	assert.Equal(t, 6, target.code)

	// Unchanged error returns exact same instance.
	cleanErr := errors.New("connection refused")
	assert.Same(t, cleanErr, tr.RedactError(cleanErr))
}

func TestTracker_EmitGitHubActionsMasks(t *testing.T) {
	tr := NewTracker()
	tr.Track("token-alpha-123")
	tr.Track("line-one-secret\nline-two-secret")

	t.Setenv("GITHUB_ACTIONS", "false")
	var buf bytes.Buffer
	tr.EmitGitHubActionsMasks(&buf)
	assert.Empty(t, buf.String())

	t.Setenv("GITHUB_ACTIONS", "true")
	tr.EmitGitHubActionsMasks(&buf)
	out := buf.String()
	assert.Contains(t, out, "::add-mask::token-alpha-123\n")
	assert.Contains(t, out, "::add-mask::line-one-secret\n")
	assert.Contains(t, out, "::add-mask::line-two-secret\n")
	// Multi-line raw string must NOT be emitted as a single ::add-mask:: command.
	assert.NotContains(t, out, "::add-mask::line-one-secret\nline-two-secret")

	// Second call is idempotent for already-masked secrets, emits only newly tracked ones.
	buf.Reset()
	tr.Track("token-beta-456")
	tr.EmitGitHubActionsMasks(&buf)
	assert.Equal(t, "::add-mask::token-beta-456\n", buf.String())
}

func TestStreamWriter_ChunkBoundarySplitting(t *testing.T) {
	secret := "ghp_superSecretToken12345"
	tr := NewTracker()
	tr.Track(secret)

	fullInput := "prefix text ghp_superSecretToken12345 middle ghp_superSecretToken12345 suffix\nfinal tail ghp_superSecretToken12345"
	expected := "prefix text [REDACTED] middle [REDACTED] suffix\nfinal tail [REDACTED]"

	// Test every single chunk size from 1 byte up to len(fullInput)+5!
	for chunkSize := 1; chunkSize <= len(fullInput)+5; chunkSize++ {
		var out bytes.Buffer
		sw := NewStreamWriter(&out, tr)
		for i := 0; i < len(fullInput); i += chunkSize {
			end := i + chunkSize
			if end > len(fullInput) {
				end = len(fullInput)
			}
			n, err := sw.Write([]byte(fullInput[i:end]))
			require.NoError(t, err)
			assert.Equal(t, end-i, n)
		}
		require.NoError(t, sw.Flush())
		assert.Equal(t, expected, out.String(), "failed at chunkSize=%d", chunkSize)
	}
}

func TestStreamWriter_OverlappingPrefixAcrossChunks(t *testing.T) {
	tr := NewTracker()
	tr.Track("ghp_short")
	tr.Track("ghp_short_extended")

	var out bytes.Buffer
	sw := NewStreamWriter(&out, tr)

	// Chunk 1 stops mid-way through the longer secret AFTER completing the shorter prefix!
	_, err := sw.Write([]byte("auth=ghp_short_ext"))
	require.NoError(t, err)
	_, err = sw.Write([]byte("ended!"))
	require.NoError(t, err)
	require.NoError(t, sw.Flush())

	assert.Equal(t, "auth=[REDACTED]!", out.String())
}

func TestTracker_TrailingNewlinePreservesLineBreaks(t *testing.T) {
	tr := NewTracker()
	tr.Track("my-secret-token-99\n")

	assert.False(t, tr.HasMultiline(), "trailing newline on single-line secret must not mark tracker multiline")
	assert.Equal(t, "Sending [REDACTED]\nNext line\n", tr.RedactString("Sending my-secret-token-99\nNext line\n"))

	var out bytes.Buffer
	sw := NewStreamWriter(&out, tr)
	_, err := sw.Write([]byte("Sending my-secret-token-99\nNext line\n"))
	require.NoError(t, err)
	require.NoError(t, sw.Flush())
	assert.Equal(t, "Sending [REDACTED]\nNext line\n", out.String())
}

func TestStreamWriter_MultibyteUTF8Safe(t *testing.T) {
	tr := NewTracker()
	tr.Track("secret-token-9999")

	input := "status: 🚀✨🔥 deploying with secret-token-9999 done 🎉"
	for chunkSize := 1; chunkSize <= 11; chunkSize++ {
		var out bytes.Buffer
		sw := NewStreamWriter(&out, tr)
		raw := []byte(input)
		for i := 0; i < len(raw); i += chunkSize {
			end := i + chunkSize
			if end > len(raw) {
				end = len(raw)
			}
			_, err := sw.Write(raw[i:end])
			require.NoError(t, err)
		}
		require.NoError(t, sw.Flush())
		assert.Equal(t, "status: 🚀✨🔥 deploying with [REDACTED] done 🎉", out.String())
	}
}

func TestTracker_ConcurrentAccess(t *testing.T) {
	tr := NewTracker()
	ctx := WithTracker(context.Background(), tr)
	require.Same(t, tr, FromContext(ctx))

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			sec := fmt.Sprintf("concurrent-secret-%d", id)
			tr.Track(sec)
			res := tr.RedactString("value=" + sec)
			assert.Equal(t, "value=[REDACTED]", res)
		}(i)
	}
	wg.Wait()
}

func TestChildTracker_AndByteSliceRedaction(t *testing.T) {
	root := NewTracker()
	child := NewChildTracker(root)
	assert.Same(t, root, child.Root())
	assert.False(t, child.HadCredential())

	// Short secrets (< MinSecretLength) still record HadCredential() on the child
	// without adding over-broad 2-byte patterns to the substring replacer.
	child.Track("42")
	assert.True(t, child.HadCredential())
	assert.Equal(t, 0, root.Count())

	// Full-length secrets tracked on child populate root and scrub []byte values.
	child.Track("secret-bytes-value-999")
	assert.Equal(t, 1, child.Count())
	assert.True(t, root.ContainsTracked([]byte("prefix secret-bytes-value-999 suffix")))
	assert.False(t, root.ContainsTracked([]byte("harmless payload")))

	origBytes := []byte("payload=secret-bytes-value-999")
	scrubbed := root.RedactAny(origBytes).([]byte)
	assert.Equal(t, "payload=[REDACTED]", string(scrubbed))
	assert.Equal(t, "payload=secret-bytes-value-999", string(origBytes))

	ctx := WithSuppressGitHubActionsMasks(context.Background())
	assert.True(t, SuppressGitHubActionsMasksFromContext(ctx))
	assert.False(t, SuppressGitHubActionsMasksFromContext(context.Background()))

	t.Setenv("GITHUB_ACTIONS", "true")
	var out bytes.Buffer
	sw := NewStreamWriterWithMasks(&out, child, true)
	_, err := sw.Write([]byte("streaming secret-bytes-value-999\n"))
	require.NoError(t, err)
	require.NoError(t, sw.Flush())
	assert.Contains(t, out.String(), "::add-mask::secret-bytes-value-999\n")
	assert.Contains(t, out.String(), "streaming [REDACTED]\n")
}

func TestStreamWriter_GitHubActionsMasksAtColumnZeroAndPercentEscaped(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "true")

	tr := NewTracker()
	var out bytes.Buffer
	sw := NewStreamWriterWithMasks(&out, tr, true)

	// 1. Write mid-line output BEFORE any secret is tracked (leaves cursor mid-line).
	_, err := sw.Write([]byte("progress..."))
	require.NoError(t, err)
	assert.Equal(t, "progress...", out.String())

	// 2. Dynamically track a secret containing '%' (must be escaped as %25 and start on column 0).
	tr.Track("tok%40percent%25val")
	_, err = sw.Write([]byte(" using tok%40percent%25val"))
	require.NoError(t, err)

	// 3. Subsequent write with NO new secrets must not insert extra blank lines.
	_, err = sw.Write([]byte(" done\n"))
	require.NoError(t, err)
	require.NoError(t, sw.Flush())

	got := out.String()
	assert.Equal(t, "progress...\n::add-mask::tok%2540percent%2525val\n using [REDACTED] done\n", got)
}
