// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package refactor

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// insertExistingContainer has a spec.resolvers mapping with two entries; a new
// entry appends after the last one.
const insertExistingContainer = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: insert-test # keep this comment
spec:
  resolvers:
    environment:
      resolve:
        with:
          - provider: parameter
            inputs:
              value: dev
    region:
      resolve:
        with:
          - provider: parameter
            inputs:
              value: us
`

// insertAbsentContainer has a spec with NO resolvers key; inserting under
// spec.resolvers must create the container key first.
const insertAbsentContainer = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: insert-absent # keep this comment
spec:
  workflow:
    actions:
      show:
        provider: message
        inputs:
          message: hi
`

// stubEntry is a zero-indented keyed resolver block (as resolverStub emits it).
const stubEntry = `myRes:
  resolve:
    with:
      - provider: static
        inputs:
          value: ""
`

func TestInsertMappingEntry_ExistingContainerAppends(t *testing.T) {
	raw := []byte(insertExistingContainer)
	edit, err := InsertMappingEntry(raw, "spec.resolvers", stubEntry)
	require.NoError(t, err)
	// A zero-width insertion.
	assert.Equal(t, edit.Range.Start, edit.Range.End, "insertion must be zero-width")
	assert.True(t, strings.HasPrefix(edit.NewText, "\n"), "append text starts on a fresh line")

	out, err := Apply(raw, []TextEdit{edit})
	require.NoError(t, err)
	s := string(out)

	// The new resolver key lands at the same indent as the existing ones.
	assert.Contains(t, s, "    myRes:\n      resolve:\n        with:\n          - provider: static")
	assert.Contains(t, s, `          value: ""`)
	// Existing entries and comments are preserved verbatim.
	assert.Contains(t, s, "    environment:")
	assert.Contains(t, s, "    region:")
	assert.Contains(t, s, "# keep this comment")

	sol := reparseAndValidate(t, out)
	require.NotNil(t, sol.Spec.Resolvers["myRes"])
}

func TestInsertMappingEntry_AbsentContainerCreatesKey(t *testing.T) {
	raw := []byte(insertAbsentContainer)
	edit, err := InsertMappingEntry(raw, "spec.resolvers", stubEntry)
	require.NoError(t, err)
	assert.Equal(t, edit.Range.Start, edit.Range.End, "insertion must be zero-width")

	out, err := Apply(raw, []TextEdit{edit})
	require.NoError(t, err)
	s := string(out)

	// A new resolvers: key was created under spec, holding the entry.
	assert.Contains(t, s, "  resolvers:\n    myRes:\n      resolve:")
	// The pre-existing workflow block is preserved.
	assert.Contains(t, s, "  workflow:")
	assert.Contains(t, s, "# keep this comment")

	sol := reparseAndValidate(t, out)
	require.NotNil(t, sol.Spec.Resolvers["myRes"])
}

func TestInsertMappingEntry_ParentNotLocated(t *testing.T) {
	raw := []byte(insertAbsentContainer)
	// spec.nope does not exist, so its child "deep" has no locatable parent.
	_, err := InsertMappingEntry(raw, "spec.nope.deep", stubEntry)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "could not be located")
}

func TestInsertMappingEntry_NoParentSegment(t *testing.T) {
	raw := []byte(insertAbsentContainer)
	_, err := InsertMappingEntry(raw, "spec", stubEntry)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no parent mapping")
}

func TestInsertMappingEntry_DuplicateKeyRejected(t *testing.T) {
	// Appending an entry whose key already exists under the container would
	// produce a duplicate mapping key (invalid YAML); the helper must refuse.
	raw := []byte(insertExistingContainer)
	dup := "environment:\n  resolve:\n    with:\n      - provider: static\n        inputs:\n          value: \"\"\n"
	_, err := InsertMappingEntry(raw, "spec.resolvers", dup)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}

func TestInsertMappingEntry_MultipleInsertsReparse(t *testing.T) {
	// Insert two entries in sequence, re-parsing after each, to prove the helper
	// keeps producing valid YAML as the container grows.
	raw := []byte(insertAbsentContainer)

	edit, err := InsertMappingEntry(raw, "spec.resolvers", stubEntry)
	require.NoError(t, err)
	out, err := Apply(raw, []TextEdit{edit})
	require.NoError(t, err)
	reparseAndValidate(t, out)

	edit2, err := InsertMappingEntry(out, "spec.resolvers", "another:\n  resolve:\n    with:\n      - provider: static\n        inputs:\n          value: \"\"\n")
	require.NoError(t, err)
	out2, err := Apply(out, []TextEdit{edit2})
	require.NoError(t, err)
	sol := reparseAndValidate(t, out2)
	require.NotNil(t, sol.Spec.Resolvers["myRes"])
	require.NotNil(t, sol.Spec.Resolvers["another"])
}
