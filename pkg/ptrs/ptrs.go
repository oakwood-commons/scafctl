// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package ptrs

var (
	// Int returns a pointer to the int value passed in.
	IntPtr = func(i int) *int { return &i }
	// Int8 returns a pointer to the int8 value passed in.
	Int8Ptr = func(i int8) *int8 { return &i }
	// Int16 returns a pointer to the int16 value passed in.
	Int16Ptr = func(i int16) *int16 { return &i }
	// Int32 returns a pointer to the int32 value passed in.
	Int32Ptr = func(i int32) *int32 { return &i }
	// Int64 returns a pointer to the int64 value passed in.
	Int64Ptr = func(i int64) *int64 { return &i }
	// Uint returns a pointer to the uint value passed in.
	UintPtr = func(i uint) *uint { return &i }
	// Uint8 returns a pointer to the uint8 value passed in.
	Uint8Ptr = func(i uint8) *uint8 { return &i }
	// Uint16 returns a pointer to the uint16 value passed in.
	Uint16Ptr = func(i uint16) *uint16 { return &i }
	// Uint32 returns a pointer to the uint32 value passed in.
	Uint32Ptr = func(i uint32) *uint32 { return &i }
	// Uint64 returns a pointer to the uint64 value passed in.
	Uint64Ptr = func(i uint64) *uint64 { return &i }
	// Float32 returns a pointer to the float32 value passed in.
	Float32Ptr = func(f float32) *float32 { return &f }
	// Float64 returns a pointer to the float64 value passed in.
	Float64Ptr = func(f float64) *float64 { return &f }
	// String returns a pointer to the string value passed in.
	StringPtr = func(s string) *string { return &s }
	// Bool returns a pointer to the bool value passed in.
	BoolPtr = func(b bool) *bool { return &b }

	// PtrInt64 dereferences an *int64 pointer and returns its value.
	// If the pointer is nil, it returns 0.
	PtrInt64 = func(i *int64) int64 {
		if i == nil {
			return 0
		}
		return *i
	}

	// PtrBool returns the value of the given *bool pointer.
	// If the pointer is nil, it returns false.
	PtrBool = func(i *bool) bool {
		if i == nil {
			return false
		}
		return *i
	}

	// PtrInt8 safely dereferences an *int8 pointer, returning its value.
	// If the pointer is nil, it returns 0.
	PtrInt8 = func(i *int8) int8 {
		if i == nil {
			return 0
		}
		return *i
	}
)
