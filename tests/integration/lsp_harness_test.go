// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// lspSessionURI is the document URI that runLSPSession opens docText under.
// Requests passed to the harness (rename, hover, definition, ...) must target
// this same URI in their params so they resolve against the opened document.
const lspSessionURI = "file:///session.yaml"

// runLSPSession spawns `scafctl lsp`, performs the standard LSP handshake
// (initialize + initialized), opens docText via textDocument/didOpen at
// lspSessionURI, then sends each request in order and returns the accumulated
// server stdout.
//
// Each request is an already-shaped JSON-RPC payload (a map like
// {"jsonrpc":"2.0","id":2,"method":"textDocument/rename","params":{...}}); the
// harness frames it with lspFrame. The initialize request uses id 1, so give
// request payloads ids >= 2 to avoid collisions.
//
// The helper returns once the server's stdout has been quiet for a short
// settle window (or a max deadline elapses), rather than requiring each caller
// to hard-code a stop condition. This lets one harness serve very different
// request types. Callers assert on the returned stdout; because the transport
// is line-delimited JSON-RPC frames, assertions typically use substring or
// JSON checks against the accumulated output.
func runLSPSession(t *testing.T, docText string, requests ...map[string]any) string {
	t.Helper()

	frames := make([][]byte, 0, 3+len(requests))
	frames = append(frames,
		lspFrame(t, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"capabilities": map[string]any{}}}),
		lspFrame(t, map[string]any{"jsonrpc": "2.0", "method": "initialized", "params": map[string]any{}}),
		lspFrame(t, map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{
			"textDocument": map[string]any{"uri": lspSessionURI, "languageId": "yaml", "version": 1, "text": docText},
		}}),
	)
	for _, r := range requests {
		frames = append(frames, lspFrame(t, r))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binaryPath, "lsp")
	cmd.Dir = findProjectRoot()
	stdin, err := cmd.StdinPipe()
	require.NoError(t, err)
	out := &lockedBuffer{}
	cmd.Stdout = out
	require.NoError(t, cmd.Start())

	for _, m := range frames {
		_, werr := stdin.Write(m)
		require.NoError(t, werr, "write LSP frame to server stdin")
	}

	// Return once stdout has stopped growing for `quiet`, or after `maxWait`.
	// Polling for a settle window (rather than a fixed sleep) keeps the helper
	// fast in the common case and non-flaky under load.
	const (
		quiet   = 400 * time.Millisecond
		maxWait = 8 * time.Second
		tick    = 20 * time.Millisecond
	)
	start := time.Now()
	lastLen := -1
	lastChange := time.Now()
	for time.Since(start) < maxWait {
		cur := len(out.String())
		if cur != lastLen {
			lastLen = cur
			lastChange = time.Now()
		} else if cur > 0 && time.Since(lastChange) >= quiet {
			break
		}
		time.Sleep(tick)
	}

	_ = stdin.Close()
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	return out.String()
}
