// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package oauth

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStartCallbackServer(t *testing.T) {
	ctx := context.Background()
	cs, err := StartCallbackServer(ctx, 0, "")
	require.NoError(t, err)
	defer cs.Close()

	assert.Contains(t, cs.RedirectURI, "http://localhost:")
}

func TestCallbackServer_ReceivesCode(t *testing.T) {
	ctx := context.Background()
	cs, err := StartCallbackServer(ctx, 0, "")
	require.NoError(t, err)
	defer cs.Close()

	// Simulate the OAuth redirect
	resp, err := http.Get(cs.RedirectURI + "/?code=test-auth-code") //nolint:noctx // test code
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	select {
	case result := <-cs.ResultChan():
		assert.NoError(t, result.Err)
		assert.Equal(t, "test-auth-code", result.Code)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for callback result")
	}
}

func TestCallbackServer_ReceivesError(t *testing.T) {
	ctx := context.Background()
	cs, err := StartCallbackServer(ctx, 0, "")
	require.NoError(t, err)
	defer cs.Close()

	// Simulate an OAuth error redirect
	resp, err := http.Get(cs.RedirectURI + "/?error=access_denied&error_description=user+cancelled") //nolint:noctx // test code
	require.NoError(t, err)
	defer resp.Body.Close()

	select {
	case result := <-cs.ResultChan():
		assert.Error(t, result.Err)
		assert.Contains(t, result.Err.Error(), "access_denied")
		assert.Contains(t, result.Err.Error(), "user cancelled")
		assert.Empty(t, result.Code)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for callback result")
	}
}

func TestCallbackServer_NoCode(t *testing.T) {
	ctx := context.Background()
	cs, err := StartCallbackServer(ctx, 0, "")
	require.NoError(t, err)
	defer cs.Close()

	// No code or error params
	resp, err := http.Get(cs.RedirectURI + "/") //nolint:noctx // test code
	require.NoError(t, err)
	defer resp.Body.Close()

	select {
	case result := <-cs.ResultChan():
		assert.Error(t, result.Err)
		assert.Contains(t, result.Err.Error(), "no authorization code received")
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for callback result")
	}
}

func TestCallbackServer_CancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := StartCallbackServer(ctx, 0, "")
	// Should fail because the context is already cancelled
	assert.Error(t, err)
}

func TestCallbackServer_Close(t *testing.T) {
	ctx := context.Background()
	cs, err := StartCallbackServer(ctx, 0, "")
	require.NoError(t, err)

	err = cs.Close()
	assert.NoError(t, err)

	// Verify the server is no longer accepting connections
	resp, err := http.Get(cs.RedirectURI + "/") //nolint:noctx // test code
	if resp != nil {
		resp.Body.Close()
	}
	assert.Error(t, err, "request should fail after server is closed")
}

func TestCallbackServer_HTMLEscapesErrors(t *testing.T) {
	ctx := context.Background()
	cs, err := StartCallbackServer(ctx, 0, "")
	require.NoError(t, err)
	defer cs.Close()

	// Send an error with HTML characters
	resp, err := http.Get(fmt.Sprintf("%s/?error=%s", cs.RedirectURI, "<script>alert('xss')</script>")) //nolint:noctx // test code
	require.NoError(t, err)
	defer resp.Body.Close()

	// The error channel should still receive the result
	select {
	case result := <-cs.ResultChan():
		assert.Error(t, result.Err)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for callback result")
	}
}

func TestStartCallbackServer_FixedPort(t *testing.T) {
	ctx := context.Background()

	// Use a fixed port; pick a high port unlikely to collide.
	cs, err := StartCallbackServer(ctx, 18947, "")
	require.NoError(t, err)
	defer cs.Close()

	assert.Equal(t, "http://localhost:18947", cs.RedirectURI)

	// Verify it actually serves on that port.
	resp, err := http.Get(cs.RedirectURI + "/?code=fixed-port-code") //nolint:noctx // test code
	require.NoError(t, err)
	defer resp.Body.Close()

	select {
	case result := <-cs.ResultChan():
		assert.NoError(t, result.Err)
		assert.Equal(t, "fixed-port-code", result.Code)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for callback result")
	}
}

func TestStartCallbackServer_FixedPortAlreadyInUse(t *testing.T) {
	ctx := context.Background()

	// Bind a port first.
	cs1, err := StartCallbackServer(ctx, 18948, "")
	require.NoError(t, err)
	defer cs1.Close()

	// Second attempt on the same port should fail.
	_, err = StartCallbackServer(ctx, 18948, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "18948")
}

func TestCallbackServer_StateValidation_Matches(t *testing.T) {
	ctx := context.Background()
	cs, err := StartCallbackServer(ctx, 0, "random-state-123")
	require.NoError(t, err)
	defer cs.Close()

	resp, err := http.Get(cs.RedirectURI + "/?code=test-code&state=random-state-123") //nolint:noctx // test code
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	select {
	case result := <-cs.ResultChan():
		assert.NoError(t, result.Err)
		assert.Equal(t, "test-code", result.Code)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for callback result")
	}
}

func TestCallbackServer_StateValidation_Mismatch(t *testing.T) {
	ctx := context.Background()
	cs, err := StartCallbackServer(ctx, 0, "expected-state")
	require.NoError(t, err)
	defer cs.Close()

	resp, err := http.Get(cs.RedirectURI + "/?code=test-code&state=wrong-state") //nolint:noctx // test code
	require.NoError(t, err)
	defer resp.Body.Close()

	select {
	case result := <-cs.ResultChan():
		assert.Error(t, result.Err)
		assert.Contains(t, result.Err.Error(), "state parameter mismatch")
		assert.Empty(t, result.Code)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for callback result")
	}
}

func TestCallbackServer_StateValidation_Missing(t *testing.T) {
	ctx := context.Background()
	cs, err := StartCallbackServer(ctx, 0, "expected-state")
	require.NoError(t, err)
	defer cs.Close()

	// Code present but state missing from callback
	resp, err := http.Get(cs.RedirectURI + "/?code=test-code") //nolint:noctx // test code
	require.NoError(t, err)
	defer resp.Body.Close()

	select {
	case result := <-cs.ResultChan():
		assert.Error(t, result.Err)
		assert.Contains(t, result.Err.Error(), "state parameter mismatch")
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for callback result")
	}
}

func TestCallbackServer_NoExpectedState_SkipsValidation(t *testing.T) {
	ctx := context.Background()
	cs, err := StartCallbackServer(ctx, 0, "")
	require.NoError(t, err)
	defer cs.Close()

	// Empty state skips validation — backward-compatible behavior
	resp, err := http.Get(cs.RedirectURI + "/?code=test-code") //nolint:noctx // test code
	require.NoError(t, err)
	defer resp.Body.Close()

	select {
	case result := <-cs.ResultChan():
		assert.NoError(t, result.Err)
		assert.Equal(t, "test-code", result.Code)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for callback result")
	}
}
