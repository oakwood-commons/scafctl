// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package plugintest is test support for exercising the host's plugin handling
// against a real plugin process: spawning, handshake, calls, shutdown, and
// everything that can go wrong along the way.
//
// It centers on a fixture plugin (pkg/plugin/testdata/lifecycleplugin) that
// behaves well by default and can be told to misbehave: fail to start, hang,
// crash or panic mid-call, return undecodable output, end a stream early, report
// a different protocol version, send fields the schema does not define, or echo
// its environment. See Config for process-level behavior and the Action
// constants for per-call behavior.
//
// A typical test builds a Sandbox, points the host at Sandbox.Binary, and
// asserts on what is left behind:
//
//	sb := plugintest.NewSandbox(t, plugintest.Config{})
//	client, err := plugin.NewClient(sb.Binary)
//	require.NoError(t, err)
//	// ... use client ...
//	client.Kill()
//	sb.RequireClean(t, 5*time.Second)
//
// The Sandbox is what makes leak assertions exact: it gives each test its own
// temp directory, so the processes and sockets found there are the ones that
// test caused. Use Sandbox.Dial instead of the host client to see the plugin's
// raw wire messages.
//
// The package deliberately does not import the host plugin package, so the
// host's own tests can use it.
package plugintest
