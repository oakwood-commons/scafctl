// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lsp

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/logger"
	pkglsp "github.com/oakwood-commons/scafctl/pkg/lsp"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runDocumentSelectors executes the `document-selectors` subcommand with the
// given binary name and args, returning the captured stdout.
func runDocumentSelectors(t *testing.T, binaryName string, args ...string) string {
	t.Helper()

	cliParams := settings.NewCliParams()
	cliParams.ExitOnError = false
	if binaryName != "" {
		cliParams.BinaryName = binaryName
	}

	var out bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &out, &out, false)
	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)
	ctx = logger.WithLogger(ctx, logger.GetNoopLogger())

	cmd := commandDocumentSelectors(cliParams, ioStreams)
	cmd.SetContext(ctx)
	cmd.SetArgs(args)
	require.NoError(t, cmd.Execute())

	return out.String()
}

func TestCommandDocumentSelectors_Construction(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := commandDocumentSelectors(cliParams, ioStreams)

	assert.Equal(t, "document-selectors", cmd.Name())
	require.NotNil(t, cmd.RunE)
	assert.NoError(t, cmd.Args(cmd, []string{}))
	assert.Error(t, cmd.Args(cmd, []string{"unexpected"}), "takes no positional args")
}

func TestCommandDocumentSelectors_JSONOutput(t *testing.T) {
	out := runDocumentSelectors(t, "", "-o", "json")

	var got pkglsp.RecognizedFiles
	require.NoError(t, json.Unmarshal([]byte(out), &got), "output must be valid JSON: %s", out)

	assert.Equal(t, settings.CliBinaryName, got.BinaryName)
	assert.Contains(t, got.YAMLNames, "solution.yaml")
	assert.Contains(t, got.YAMLNames, "taskfile.yaml")
	assert.Contains(t, got.YAMLNames, "actions.yaml")
	assert.Contains(t, got.JSONNames, "solution.json")
	assert.NotContains(t, got.YAMLNames, "solution.json")
}

func TestCommandDocumentSelectors_EmbedderBinaryName(t *testing.T) {
	out := runDocumentSelectors(t, "mycli", "-o", "json")

	var got pkglsp.RecognizedFiles
	require.NoError(t, json.Unmarshal([]byte(out), &got), "output must be valid JSON: %s", out)

	assert.Equal(t, "mycli", got.BinaryName)
	assert.Contains(t, got.YAMLNames, "mycli.yaml")
	assert.Contains(t, got.YAMLNames, "mycli.yml")
	assert.Contains(t, got.JSONNames, "mycli.json")
	// Standard names still recognized alongside the embedder name.
	assert.Contains(t, got.YAMLNames, "solution.yaml")
}
