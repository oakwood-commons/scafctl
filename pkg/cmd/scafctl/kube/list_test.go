// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package kube

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	kubeapi "github.com/oakwood-commons/scafctl/pkg/kube"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
)

func TestCommandList_NoResolver(t *testing.T) {
	t.Parallel()
	ctx, buf := newTestContext(t)

	cmd := CommandList(embedderParams(), terminal.NewIOStreams(nil, buf, buf, false), "mycli")
	require.NoError(t, runCmd(t, cmd, ctx))
	// Diagnostic keyed off the embedder binary name, not the "scafctl" brand.
	assert.Contains(t, buf.String(), "No cluster resolver configured")
	assert.Contains(t, buf.String(), "mycli ships no cluster data")
	assert.NotContains(t, buf.String(), "scafctl ships no cluster data")
}

func TestCommandList_NoResolverStructuredEmitsEmptyList(t *testing.T) {
	t.Parallel()
	ctx, _ := newTestContext(t)
	var out bytes.Buffer

	// With no resolver, structured output must still be a valid empty list on
	// stdout -- never the human diagnostic (which belongs on stderr).
	cmd := CommandList(embedderParams(), terminal.NewIOStreams(nil, &out, &out, false), "mycli")
	require.NoError(t, runCmd(t, cmd, ctx, "-o", "json"))
	assert.Equal(t, "[]", strings.TrimSpace(out.String()))
	assert.NotContains(t, out.String(), "No cluster resolver")
}

func TestCommandList_RendersClusters(t *testing.T) {
	t.Parallel()
	ctx, buf := newTestContext(t)
	resolver := &kubeapi.MockResolver{ListResult: []kubeapi.ClusterInfo{
		{Name: "prod", APIServerURL: "https://api.prod.example.com:6443", AuthType: kubeapi.AuthTypeOIDC, ConsoleURL: "https://console.prod"},
		{Name: "lab", APIServerURL: "https://api.lab.example.com:6443"},
	}}
	ctx = kubeapi.WithResolver(ctx, resolver)

	cmd := CommandList(embedderParams(), terminal.NewIOStreams(nil, buf, buf, false), "mycli")
	require.NoError(t, runCmd(t, cmd, ctx, "-o", "json"))
	out := buf.String()
	assert.Contains(t, out, "prod")
	assert.Contains(t, out, "lab")
	assert.Contains(t, out, "https://api.prod.example.com:6443")
}

func TestCommandList_EmptyResolver(t *testing.T) {
	t.Parallel()
	ctx, buf := newTestContext(t)
	ctx = kubeapi.WithResolver(ctx, &kubeapi.MockResolver{})

	cmd := CommandList(embedderParams(), terminal.NewIOStreams(nil, buf, buf, false), "mycli")
	require.NoError(t, runCmd(t, cmd, ctx))
	assert.Contains(t, buf.String(), "No clusters found")
}

func TestCommandList_HasAlias(t *testing.T) {
	t.Parallel()
	cmd := CommandList(embedderParams(), terminal.NewIOStreams(nil, nil, nil, false), "mycli")
	assert.Contains(t, cmd.Aliases, "ls")
}
