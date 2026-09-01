// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/oakwood-commons/kvx/pkg/tui"
	catalogpkg "github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandTags(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandTags(cliParams, ioStreams, "scafctl/catalog")

	require.NotNil(t, cmd)
	assert.Equal(t, "tags <registry/repository[/kind]/name>", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotNil(t, cmd.RunE)
}

func TestCommandTags_Flags(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandTags(cliParams, ioStreams, "scafctl/catalog")

	flagTests := []struct {
		name     string
		defValue string
	}{
		{"kind", ""},
		{"insecure", "false"},
	}

	for _, tt := range flagTests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := cmd.Flags().Lookup(tt.name)
			require.NotNil(t, f, "flag %q should exist", tt.name)
			assert.Equal(t, tt.defValue, f.DefValue, "flag %q default value", tt.name)
		})
	}
}

func TestCommandTags_RequiresExactlyOneArg(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandTags(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "accepts 1 arg(s)")
}

func TestCommandTags_InvalidReference(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandTags(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetContext(newCatalogTestCtx(t))
	cmd.SetArgs([]string{":::invalid"})

	err := cmd.Execute()
	require.Error(t, err)
}

func TestCommandTags_InvalidKind(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandTags(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetContext(newCatalogTestCtx(t))
	cmd.SetArgs([]string{"ghcr.io/myorg/my-solution", "--kind", "not-a-valid-kind"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid kind")
}

func TestTagsListItem_Fields(t *testing.T) {
	t.Parallel()

	item := TagsListItem{
		Tag:      "1.0.0",
		IsSemver: true,
		Version:  "1.0.0",
	}

	assert.Equal(t, "1.0.0", item.Tag)
	assert.True(t, item.IsSemver)
	assert.Equal(t, "1.0.0", item.Version)
}

func TestTagsListItem_AliasTag(t *testing.T) {
	t.Parallel()

	item := TagsListItem{
		Tag:      "stable",
		IsSemver: false,
	}

	assert.Equal(t, "stable", item.Tag)
	assert.False(t, item.IsSemver)
	assert.Empty(t, item.Version)
}

func BenchmarkCommandTags(b *testing.B) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		CommandTags(cliParams, ioStreams, "scafctl/catalog")
	}
}

func TestTagsSchema_ValidJSON(t *testing.T) {
	t.Parallel()

	var schema map[string]any
	err := json.Unmarshal(tagsSchemaJSON, &schema)
	require.NoError(t, err, "tags_schema.json must be valid JSON")

	items, ok := schema["items"].(map[string]any)
	require.True(t, ok, "schema must have items object")

	props, ok := items["properties"].(map[string]any)
	require.True(t, ok, "items must have properties")

	for _, field := range []string{"tag", "isSemver", "version"} {
		_, exists := props[field]
		assert.True(t, exists, "schema missing field %q", field)
	}
}

func TestTagsSchema_ParsesWithDisplay(t *testing.T) {
	t.Parallel()

	hints, ds, err := tui.ParseSchemaWithDisplay(tagsSchemaJSON)
	require.NoError(t, err, "tags_schema.json must parse without error")
	assert.NotNil(t, hints, "should produce column hints")
	assert.NotNil(t, ds, "should produce display schema")
}

func TestTagsColumnHints_Fields(t *testing.T) {
	t.Parallel()

	for _, field := range []string{"tag", "version", "isSemver"} {
		_, ok := tagsColumnHints[field]
		assert.True(t, ok, "column hint missing for field %q", field)
	}
}

func TestWriteTags_JSONOutput(t *testing.T) {
	t.Parallel()

	ioStreams, out, errOut := terminal.NewTestIOStreams()
	outputOpts := kvx.NewOutputOptions(ioStreams)
	outputOpts.Format = "json"
	w := writer.New(ioStreams, &settings.Run{})

	tags := []catalogpkg.TagInfo{
		{Tag: "1.0.0", IsSemver: true, Version: "1.0.0"},
		{Tag: "stable", IsSemver: false},
	}
	require.NoError(t, writeTags(w, outputOpts, "solutions/my-solution", tags))

	var items []TagsListItem
	require.NoError(t, json.Unmarshal(out.Bytes(), &items))
	require.Len(t, items, 2)
	assert.Equal(t, "1.0.0", items[0].Tag)
	assert.True(t, items[0].IsSemver)
	assert.Equal(t, "stable", items[1].Tag)
	// The progress/empty notices must never leak into structured stdout or stderr here.
	assert.Empty(t, errOut.String())
}

func TestWriteTags_EmptyStructuredEmitsArray(t *testing.T) {
	t.Parallel()

	ioStreams, out, errOut := terminal.NewTestIOStreams()
	outputOpts := kvx.NewOutputOptions(ioStreams)
	outputOpts.Format = "json"
	w := writer.New(ioStreams, &settings.Run{})

	require.NoError(t, writeTags(w, outputOpts, "solutions/none", nil))

	assert.Equal(t, "[]", strings.TrimSpace(out.String()), "structured empty output must be a parseable []")
	assert.Empty(t, errOut.String(), "no human text on empty structured output")
}

func TestWriteTags_EmptyHumanWarnsOnStderr(t *testing.T) {
	t.Parallel()

	ioStreams, out, errOut := terminal.NewTestIOStreams()
	outputOpts := kvx.NewOutputOptions(ioStreams)
	outputOpts.Format = "table"
	w := writer.New(ioStreams, &settings.Run{})

	require.NoError(t, writeTags(w, outputOpts, "solutions/none", nil))

	assert.Empty(t, out.String(), "no output on stdout when there are no tags")
	assert.Contains(t, errOut.String(), "No tags found for solutions/none")
}

func TestWriteTags_EmptyQuietStaysSilent(t *testing.T) {
	t.Parallel()

	ioStreams, out, errOut := terminal.NewTestIOStreams()
	outputOpts := kvx.NewOutputOptions(ioStreams)
	outputOpts.Format = "quiet"
	w := writer.New(ioStreams, &settings.Run{})

	require.NoError(t, writeTags(w, outputOpts, "solutions/none", nil))

	assert.Empty(t, out.String())
	assert.Empty(t, errOut.String())
}

// TestWriteTags_TextColumnsNotKeyValue guards against the KEY/VALUE fallback:
// a mix of semver and alias tags must still render as a homogeneous columnar
// table (field-named columns), not a two-column key/value dump.
func TestWriteTags_TextColumnsNotKeyValue(t *testing.T) {
	t.Parallel()

	ioStreams, out, _ := terminal.NewTestIOStreams()
	outputOpts := kvx.NewOutputOptions(ioStreams)
	outputOpts.Format = "text"
	outputOpts.ColumnOrder = []string{"tag", "version", "isSemver"}
	w := writer.New(ioStreams, &settings.Run{})

	tags := []catalogpkg.TagInfo{
		{Tag: "1.0.0", IsSemver: true, Version: "1.0.0"},
		{Tag: "stable", IsSemver: false},
	}
	require.NoError(t, writeTags(w, outputOpts, "solutions/my-solution", tags))

	upper := strings.ToUpper(out.String())
	assert.Contains(t, upper, "TAG")
	assert.Contains(t, upper, "VERSION")
	assert.NotContains(t, upper, "KEY", "must not fall back to KEY/VALUE rendering")
	// Both tags appear as their own rows.
	assert.Contains(t, out.String(), "1.0.0")
	assert.Contains(t, out.String(), "stable")
}
