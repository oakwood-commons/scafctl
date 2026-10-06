// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package diffreport

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
)

func newTestWriter(t *testing.T) (*writer.Writer, *stringBuffers) {
	t.Helper()
	ioStreams, out, errOut := terminal.NewTestIOStreams()
	w := writer.New(ioStreams, &settings.Run{NoColor: true})
	return w, &stringBuffers{out: out, errOut: errOut}
}

type stringBuffers struct {
	out    interface{ String() string }
	errOut interface{ String() string }
}

func TestRender_EmptyReport(t *testing.T) {
	w, bufs := newTestWriter(t)
	Render(w, New("solution", "a.yaml", "b.yaml"))

	got := bufs.out.String()
	assert.Contains(t, got, "Diff: a.yaml -> b.yaml")
	assert.Contains(t, got, "No differences found.")
	assert.Empty(t, bufs.errOut.String())
}

func TestRender_UngroupedEntries(t *testing.T) {
	w, bufs := newTestWriter(t)
	r := New("solution", "v1", "v2")
	r.Add(Entry{Path: "metadata.name", Kind: ChangeModified, Before: "old", After: "new"})
	r.Add(Entry{Path: "spec.resolvers.x", Kind: ChangeAdded})
	r.Add(Entry{Path: "spec.resolvers.y", Kind: ChangeRemoved})
	Render(w, r)

	got := bufs.out.String()
	assert.Contains(t, got, "Diff: v1 -> v2")
	assert.Contains(t, got, "~ metadata.name: old -> new")
	assert.Contains(t, got, "+ spec.resolvers.x")
	assert.Contains(t, got, "- spec.resolvers.y")
	assert.Contains(t, got, "Summary: 3 total | 1 added | 1 removed | 1 modified")
}

func TestRender_GroupedEntriesPreserveOrder(t *testing.T) {
	w, bufs := newTestWriter(t)
	r := New("bundle", "x@1", "x@2")
	r.Add(Entry{Group: "resolvers", Path: "a", Kind: ChangeAdded})
	r.Add(Entry{Group: "files", Path: "f.txt", Kind: ChangeAdded, Detail: "12 B"})
	r.Add(Entry{Group: "files", Path: "g.txt", Kind: ChangeRemoved})
	Render(w, r)

	got := bufs.out.String()
	assert.Contains(t, got, "resolvers:")
	assert.Contains(t, got, "files:")
	assert.Contains(t, got, "+ f.txt (12 B)")
	// resolvers section must come before files section.
	assert.Less(t, indexOf(got, "resolvers:"), indexOf(got, "files:"))
}

func TestRender_ModifiedWithoutValuesUsesDetail(t *testing.T) {
	w, bufs := newTestWriter(t)
	r := New("bundle", "x@1", "x@2")
	r.Add(Entry{Group: "plugins", Path: "p", Kind: ChangeModified, Detail: "1.0.0 -> 2.0.0"})
	Render(w, r)

	assert.Contains(t, bufs.out.String(), "~ p (1.0.0 -> 2.0.0)")
}

func TestRender_UnchangedAndNilValue(t *testing.T) {
	w, bufs := newTestWriter(t)
	r := New("snapshot", "s@1", "s@2")
	r.Add(Entry{Path: "kept", Kind: ChangeUnchanged})
	r.Add(Entry{Path: "appended", Kind: ChangeModified, After: "new"}) // nil Before exercises fmtValue(<nil>)
	r.Add(Entry{Path: "weird", Kind: ChangeKind("bogus")})             // unknown kind hits the default branch
	Render(w, r)

	got := bufs.out.String()
	assert.Contains(t, got, "kept")
	assert.Contains(t, got, "~ appended: <nil> -> new")
	assert.Contains(t, got, "weird")
	// Unchanged entries count toward Total, so the summary must surface them.
	assert.Contains(t, got, "| 1 unchanged")
}

func TestRender_WithColorRendersStyledPath(t *testing.T) {
	ioStreams, out, _ := terminal.NewTestIOStreams()
	w := writer.New(ioStreams, &settings.Run{NoColor: false})
	r := New("solution", "v1", "v2")
	r.Add(Entry{Path: "x", Kind: ChangeAdded})
	Render(w, r)

	assert.Contains(t, out.String(), "x")
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
