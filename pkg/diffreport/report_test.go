// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package diffreport

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNew_EmptyReportHasNonNilEntries(t *testing.T) {
	r := New("solution", "a.yaml", "b.yaml")
	assert.Equal(t, "solution", r.Kind)
	assert.Equal(t, "a.yaml", r.LeftRef)
	assert.Equal(t, "b.yaml", r.RightRef)
	assert.NotNil(t, r.Entries)
	assert.Empty(t, r.Entries)
	assert.Equal(t, Summary{}, r.Summary)
}

func TestReport_Add_UpdatesSummary(t *testing.T) {
	r := New("bundle", "x@1", "x@2")
	r.Add(Entry{Path: "a", Kind: ChangeAdded})
	r.Add(Entry{Path: "b", Kind: ChangeRemoved})
	r.Add(Entry{Path: "c", Kind: ChangeModified, Before: 1, After: 2})
	r.Add(Entry{Path: "d", Kind: ChangeUnchanged})

	assert.Len(t, r.Entries, 4)
	assert.Equal(t, Summary{Total: 4, Added: 1, Removed: 1, Modified: 1, Unchanged: 1}, r.Summary)
}
