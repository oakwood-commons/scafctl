// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package conversion_test

import (
	"testing"

	"github.com/google/cel-go/common/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	conversion "github.com/oakwood-commons/scafctl/pkg/celexp/conversion"
)

// Smoke tests locking the re-export contract of the conversion adapter. These
// are thin delegations to the upstream library (which tests them thoroughly);
// the point here is to catch a broken/removed re-export, not to re-test the
// conversion logic.
func TestConversionReExports(t *testing.T) {
	assert.Equal(t, "hi", conversion.CelValueToGo(types.String("hi")))

	v := conversion.GoToCelValue("hi")
	require.NotNil(t, v)
	assert.Equal(t, "hi", v.Value())

	assert.Equal(t, "x", conversion.NullSafeValue(types.String("x")))
	assert.Nil(t, conversion.NullSafeValue(types.NullValue))

	list := types.DefaultTypeAdapter.NativeToValue([]any{"a", "b"})
	strs, err := conversion.ListToStringSlice(list)
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b"}, strs)

	objs := types.DefaultTypeAdapter.NativeToValue([]any{map[string]any{"k": "v"}})
	objSlice, err := conversion.ListToObjectSlice(objs)
	require.NoError(t, err)
	require.Len(t, objSlice, 1)
	assert.Equal(t, "v", objSlice[0]["k"])

	m := types.DefaultTypeAdapter.NativeToValue(map[string]any{"k": "v"})
	obj, err := conversion.ToObject(m)
	require.NoError(t, err)
	assert.Equal(t, "v", obj["k"])
}
