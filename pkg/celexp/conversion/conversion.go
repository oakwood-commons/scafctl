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
		result[keyStr] = value.Value()
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

// CelValueToGo recursively converts a CEL ref.Val to a native Go value.
// This handles maps, lists, and primitive types.
func CelValueToGo(val ref.Val) any {
	// Get the underlying Go value
	goVal := val.Value()

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

	// Return the primitive value as-is
	return goVal
}

// GoToCelValue converts a native Go value to a CEL ref.Val.
// This uses the default type adapter to ensure proper conversion.
func GoToCelValue(val any) ref.Val {
	return types.DefaultTypeAdapter.NativeToValue(val)
}
