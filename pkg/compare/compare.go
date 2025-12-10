package compare

import "fmt"

// Values compares two values and returns:
//   - -1 if a < b
//   - 0 if a == b
//   - 1 if a > b
//
// Handles strings, numbers (int, int64, float64, etc.), and booleans.
// For incompatible types, compares by type name.
func Values(a, b any) int {
	// Handle nil values
	if a == nil && b == nil {
		return 0
	}
	if a == nil {
		return -1
	}
	if b == nil {
		return 1
	}

	// Try numeric comparison first
	aNum, aIsNum := ToFloat64(a)
	bNum, bIsNum := ToFloat64(b)
	if aIsNum && bIsNum {
		if aNum < bNum {
			return -1
		}
		if aNum > bNum {
			return 1
		}
		return 0
	}

	// Try string comparison
	aStr, aIsStr := a.(string)
	bStr, bIsStr := b.(string)
	if aIsStr && bIsStr {
		if aStr < bStr {
			return -1
		}
		if aStr > bStr {
			return 1
		}
		return 0
	}

	// Try boolean comparison
	aBool, aIsBool := a.(bool)
	bBool, bIsBool := b.(bool)
	if aIsBool && bIsBool {
		if !aBool && bBool {
			return -1
		}
		if aBool && !bBool {
			return 1
		}
		return 0
	}

	// Fall back to comparing type names for incompatible types
	aType := fmt.Sprintf("%T", a)
	bType := fmt.Sprintf("%T", b)
	if aType < bType {
		return -1
	}
	if aType > bType {
		return 1
	}
	return 0
}

// ToFloat64 converts various numeric types to float64.
// Returns the float64 value and true if successful, 0 and false otherwise.
func ToFloat64(v any) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case float32:
		return float64(val), true
	case int:
		return float64(val), true
	case int8:
		return float64(val), true
	case int16:
		return float64(val), true
	case int32:
		return float64(val), true
	case int64:
		return float64(val), true
	case uint:
		return float64(val), true
	case uint8:
		return float64(val), true
	case uint16:
		return float64(val), true
	case uint32:
		return float64(val), true
	case uint64:
		return float64(val), true
	default:
		return 0, false
	}
}
