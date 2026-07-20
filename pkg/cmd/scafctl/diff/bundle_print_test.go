// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package diff

import (
	"bytes"
	"os"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
)

// newPrintWriter returns a Writer whose stdout and stderr are directed to the
// same buffer, so tests can assert on the combined text output of the print*
// helpers regardless of which stream each line targets.
func newPrintWriter() (*bytes.Buffer, *writer.Writer) {
	buf := &bytes.Buffer{}
	ioStreams := &terminal.IOStreams{
		In:     os.Stdin,
		Out:    buf,
		ErrOut: buf,
	}
	cliParams := settings.NewCliParams()
	cliParams.NoColor = true
	return buf, writer.New(ioStreams, cliParams)
}

func TestPrintSolutionDiff(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		diff        *SolutionDiff
		wantContain []string
		wantAbsent  []string
	}{
		{
			name: "additions removals and modifications",
			diff: &SolutionDiff{
				Resolvers: bundleDiffSets{
					Added:    []string{"resA"},
					Modified: []string{"resM"},
					Removed:  []string{"resR"},
				},
				Actions: bundleDiffSets{
					Added: []string{"actA"},
				},
			},
			wantContain: []string{
				"Solution YAML:",
				"resolvers:",
				"+ resA (added)",
				"~ resM (present in both)",
				"- resR (removed)",
				"workflow.actions:",
				"+ actA (added)",
			},
		},
		{
			name: "empty diff sets omit labels",
			diff: &SolutionDiff{},
			wantContain: []string{
				"Solution YAML:",
			},
			wantAbsent: []string{
				"resolvers:",
				"workflow.actions:",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			buf, w := newPrintWriter()
			printSolutionDiff(w, tc.diff)
			out := buf.String()
			for _, want := range tc.wantContain {
				assert.Contains(t, out, want)
			}
			for _, absent := range tc.wantAbsent {
				assert.NotContains(t, out, absent)
			}
		})
	}
}

func TestPrintFilesDiff(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		diff        *FilesDiff
		wantContain []string
	}{
		{
			name: "added modified and removed files",
			diff: &FilesDiff{
				Added:    []FileDiffEntry{{Path: "new.txt", Size: 1024}},
				Modified: []FileDiffEntry{{Path: "changed.txt"}},
				Removed:  []FileDiffEntry{{Path: "gone.txt", Size: 512}},
			},
			wantContain: []string{
				"Bundled files:",
				"+ new.txt (added,",
				"~ changed.txt (modified)",
				"- gone.txt (removed)",
			},
		},
		{
			name: "empty files diff prints header only",
			diff: &FilesDiff{},
			wantContain: []string{
				"Bundled files:",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			buf, w := newPrintWriter()
			printFilesDiff(w, tc.diff)
			out := buf.String()
			for _, want := range tc.wantContain {
				assert.Contains(t, out, want)
			}
		})
	}
}

func TestPrintFilesDiff_Nil(t *testing.T) {
	t.Parallel()
	buf, w := newPrintWriter()
	printFilesDiff(w, nil)
	assert.Empty(t, buf.String())
}

func TestPrintVendoredDiff(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		diff        *VendoredDiff
		wantContain []string
	}{
		{
			name: "added upgraded and removed dependencies",
			diff: &VendoredDiff{
				Added:    []VendoredEntry{{Name: "dep-a", Version: "1.0.0"}},
				Upgraded: []VendoredUpgrade{{Name: "dep-u", From: "1.0.0", To: "2.0.0"}},
				Removed:  []VendoredEntry{{Name: "dep-r", Version: "0.9.0"}},
			},
			wantContain: []string{
				"Vendored dependencies:",
				"+ dep-a@1.0.0 (added)",
				"~ dep-u: 1.0.0",
				"2.0.0 (upgraded)",
				"- dep-r@0.9.0 (removed)",
			},
		},
		{
			name: "empty vendored diff prints header only",
			diff: &VendoredDiff{},
			wantContain: []string{
				"Vendored dependencies:",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			buf, w := newPrintWriter()
			printVendoredDiff(w, tc.diff)
			out := buf.String()
			for _, want := range tc.wantContain {
				assert.Contains(t, out, want)
			}
		})
	}
}

func TestPrintVendoredDiff_Nil(t *testing.T) {
	t.Parallel()
	buf, w := newPrintWriter()
	printVendoredDiff(w, nil)
	assert.Empty(t, buf.String())
}

func TestPrintPluginsDiff(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		diff        *PluginsDiff
		wantContain []string
	}{
		{
			name: "added modified and removed plugins",
			diff: &PluginsDiff{
				Added:    []PluginDiffEntry{{Name: "plug-a", VersionTo: "1.0.0"}},
				Modified: []PluginDiffEntry{{Name: "plug-m", VersionFrom: "1.0.0", VersionTo: "2.0.0"}},
				Removed:  []PluginDiffEntry{{Name: "plug-r", VersionFrom: "0.9.0"}},
			},
			wantContain: []string{
				"Plugins:",
				"+ plug-a 1.0.0 (added)",
				"~ plug-m: 1.0.0",
				"2.0.0 (constraint changed)",
				"- plug-r 0.9.0 (removed)",
			},
		},
		{
			name: "empty plugins diff prints header only",
			diff: &PluginsDiff{},
			wantContain: []string{
				"Plugins:",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			buf, w := newPrintWriter()
			printPluginsDiff(w, tc.diff)
			out := buf.String()
			for _, want := range tc.wantContain {
				assert.Contains(t, out, want)
			}
		})
	}
}

func TestPrintPluginsDiff_Nil(t *testing.T) {
	t.Parallel()
	buf, w := newPrintWriter()
	printPluginsDiff(w, nil)
	assert.Empty(t, buf.String())
}

func TestPrintDiffSets_EmptyOmitsLabel(t *testing.T) {
	t.Parallel()
	buf, w := newPrintWriter()
	printDiffSets(w, "  resolvers:", bundleDiffSets{})
	assert.Empty(t, buf.String())
}

func TestPrintDiffSets_ModifiedOnly(t *testing.T) {
	t.Parallel()
	// Modified alone (no Added/Removed) still triggers the label because the
	// guard only checks Added and Removed lengths.
	buf, w := newPrintWriter()
	printDiffSets(w, "  resolvers:", bundleDiffSets{Added: []string{"a"}, Modified: []string{"m"}})
	out := buf.String()
	assert.Contains(t, out, "resolvers:")
	assert.Contains(t, out, "~ m (present in both)")
}
