// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package conversion

import (
	"fmt"

	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/common/types/traits"
)

// ListToStringSlice converts a CEL list to a Go string slice.
// Returns an error if the list contains non-string elements.
func ListToStringSlice(listVal ref.Val) ([]string, error) {
	// Type check the list
	list, ok := listVal.(traits.Lister)
	if !ok {
		return nil, fmt.Errorf("expected list, got %s", listVal.Type())
	}

	// Convert list to string slice
	iterator := list.Iterator()
	result := make([]string, 0)
	for iterator.HasNext() == types.True {
		item := iterator.Next()
		str, ok := item.Value().(string)
		if !ok {
			return nil, fmt.Errorf("list contains non-string element of type %s", item.Type())
		}
		result = append(result, str)
	}

	return result, nil
}

// ToObject converts a CEL map to a Go map[string]any.
// Returns an error if the input is not a map or if any key is not a string.
//
// Values are converted one level deep: each value is normalized via
// NullSafeValue (so a null value becomes Go nil, never a numeric 0), but nested
// maps/lists are returned in their immediate CEL representation. Use
// CelValueToGo when a fully-recursive conversion is required.
func ToObject(mapVal ref.Val) (map[string]any, error) {
	// Type check the map
	mapper, ok := mapVal.(traits.Mapper)
	if !ok {
		return nil, fmt.Errorf("expected map, got %s", mapVal.Type())
	}

	// Convert map to Go map
	result := make(map[string]any)
	iterator := mapper.Iterator()
	for iterator.HasNext() == types.True {
		key := iterator.Next()
		keyStr, ok := key.Value().(string)
		if !ok {
			return nil, fmt.Errorf("map contains non-string key of type %s", key.Type())
		}
		value := mapper.Get(key)
		result[keyStr] = NullSafeValue(value)
	}

	return result, nil
}

// ListToObjectSlice converts a CEL list of maps to a Go slice of map[string]any.
// Returns an error if the list contains non-map elements or if any map has non-string keys.
func ListToObjectSlice(listVal ref.Val) ([]map[string]any, error) {
	// Type check the list
	list, ok := listVal.(traits.Lister)
	if !ok {
		return nil, fmt.Errorf("expected list, got %s", listVal.Type())
	}

	// Convert list to slice of maps
	iterator := list.Iterator()
	result := make([]map[string]any, 0)
	for iterator.HasNext() == types.True {
		item := iterator.Next()
		obj, err := ToObject(item)
		if err != nil {
			return nil, fmt.Errorf("list contains non-map element: %w", err)
		}
		result = append(result, obj)
	}

	return result, nil
}

// NullSafeValue returns the native Go value of a CEL ref.Val, mapping CEL null
// to Go nil.
//
// It behaves like ref.Val.Value() for every type except null. This is required
// because cel-go's Null.Value() returns structpb.NullValue_NULL_VALUE, whose
// underlying value is the integer 0. Returning that raw value would silently
// corrupt an explicit null into a numeric 0 (for example when round-tripping
// through json.unmarshal or when serializing an evaluation result), so null is
// normalized to Go nil here. A nil ref.Val is likewise treated as nil.
//
// Note that for container values (maps, lists) this returns the value's
// immediate representation without recursing; use CelValueToGo for a deep
// conversion that normalizes nested nulls.
func NullSafeValue(val ref.Val) any {
	if val == nil {
		return nil
	}
	if _, ok := val.(types.Null); ok {
		return nil
	}
	return val.Value()
}

// CelValueToGo recursively converts a CEL ref.Val to a native Go value.
// This handles maps, lists, and primitive types.
func CelValueToGo(val ref.Val) any {
	// Handle maps
	if mapper, ok := val.(traits.Mapper); ok {
		result := make(map[string]any)
		iterator := mapper.Iterator()
		for iterator.HasNext() == types.True {
			key := iterator.Next()
			keyStr, ok := key.Value().(string)
			if !ok {
				// If key is not a string, skip it
				continue
			}
			value := mapper.Get(key)
			result[keyStr] = CelValueToGo(value)
		}
		return result
	}

	// Handle lists
	if lister, ok := val.(traits.Lister); ok {
		result := make([]any, 0)
		iterator := lister.Iterator()
		for iterator.HasNext() == types.True {
			item := iterator.Next()
			result = append(result, CelValueToGo(item))
		}
		return result
	}

	// Return the primitive value as-is (null is normalized to Go nil).
	return NullSafeValue(val)
}

// GoToCelValue converts a native Go value to a CEL ref.Val.
// This uses the default type adapter to ensure proper conversion.
func GoToCelValue(val any) ref.Val {
	return types.DefaultTypeAdapter.NativeToValue(val)
}
