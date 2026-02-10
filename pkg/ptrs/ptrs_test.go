// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package ptrs

import (
	"testing"
)

func TestIntPtr(t *testing.T) {
	val := 42
	ptr := IntPtr(val)
	if *ptr != val {
		t.Errorf("IntPtr() = %v, want %v", *ptr, val)
	}
}

func TestInt8Ptr(t *testing.T) {
	val := int8(42)
	ptr := Int8Ptr(val)
	if *ptr != val {
		t.Errorf("Int8Ptr() = %v, want %v", *ptr, val)
	}
}

func TestInt16Ptr(t *testing.T) {
	val := int16(42)
	ptr := Int16Ptr(val)
	if *ptr != val {
		t.Errorf("Int16Ptr() = %v, want %v", *ptr, val)
	}
}

func TestInt32Ptr(t *testing.T) {
	val := int32(42)
	ptr := Int32Ptr(val)
	if *ptr != val {
		t.Errorf("Int32Ptr() = %v, want %v", *ptr, val)
	}
}

func TestInt64Ptr(t *testing.T) {
	val := int64(42)
	ptr := Int64Ptr(val)
	if *ptr != val {
		t.Errorf("Int64Ptr() = %v, want %v", *ptr, val)
	}
}

func TestUintPtr(t *testing.T) {
	val := uint(42)
	ptr := UintPtr(val)
	if *ptr != val {
		t.Errorf("UintPtr() = %v, want %v", *ptr, val)
	}
}

func TestUint8Ptr(t *testing.T) {
	val := uint8(42)
	ptr := Uint8Ptr(val)
	if *ptr != val {
		t.Errorf("Uint8Ptr() = %v, want %v", *ptr, val)
	}
}

func TestUint16Ptr(t *testing.T) {
	val := uint16(42)
	ptr := Uint16Ptr(val)
	if *ptr != val {
		t.Errorf("Uint16Ptr() = %v, want %v", *ptr, val)
	}
}

func TestUint32Ptr(t *testing.T) {
	val := uint32(42)
	ptr := Uint32Ptr(val)
	if *ptr != val {
		t.Errorf("Uint32Ptr() = %v, want %v", *ptr, val)
	}
}

func TestUint64Ptr(t *testing.T) {
	val := uint64(42)
	ptr := Uint64Ptr(val)
	if *ptr != val {
		t.Errorf("Uint64Ptr() = %v, want %v", *ptr, val)
	}
}

func TestFloat32Ptr(t *testing.T) {
	val := float32(42.0)
	ptr := Float32Ptr(val)
	if *ptr != val {
		t.Errorf("Float32Ptr() = %v, want %v", *ptr, val)
	}
}

func TestFloat64Ptr(t *testing.T) {
	val := float64(42.0)
	ptr := Float64Ptr(val)
	if *ptr != val {
		t.Errorf("Float64Ptr() = %v, want %v", *ptr, val)
	}
}

func TestStringPtr(t *testing.T) {
	val := "test"
	ptr := StringPtr(val)
	if *ptr != val {
		t.Errorf("StringPtr() = %v, want %v", *ptr, val)
	}
}

func TestBoolPtr(t *testing.T) {
	val := true
	ptr := BoolPtr(val)
	if *ptr != val {
		t.Errorf("BoolPtr() = %v, want %v", *ptr, val)
	}
}

func TestPtrInt64(t *testing.T) {
	val := int64(42)
	ptr := &val
	result := PtrInt64(ptr)
	if result != val {
		t.Errorf("PtrInt64() = %v, want %v", result, val)
	}

	var nilPtr *int64
	result = PtrInt64(nilPtr)
	if result != 0 {
		t.Errorf("PtrInt64(nil) = %v, want 0", result)
	}
}

func TestPtrBool(t *testing.T) {
	val := true
	ptr := &val
	result := PtrBool(ptr)
	if result != val {
		t.Errorf("PtrBool() = %v, want %v", result, val)
	}

	var nilPtr *bool
	result = PtrBool(nilPtr)
	if result != false {
		t.Errorf("PtrBool(nil) = %v, want false", result)
	}
}

func TestPtrInt8(t *testing.T) {
	val := int8(42)
	ptr := &val
	result := PtrInt8(ptr)
	if result != val {
		t.Errorf("PtrInt8() = %v, want %v", result, val)
	}

	var nilPtr *int8
	result = PtrInt8(nilPtr)
	if result != 0 {
		t.Errorf("PtrInt8(nil) = %v, want 0", result)
	}
}
