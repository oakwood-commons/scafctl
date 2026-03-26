// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package secretcrypto

import (
	"testing"
)

func BenchmarkEncrypt(b *testing.B) {
	if testing.Short() {
		b.Skip("skipping slow crypto benchmark in short mode")
	}

	password := "benchmark-password-123"

	sizes := []struct {
		name string
		size int
	}{
		{"100B", 100},
		{"1KB", 1024},
		{"10KB", 10 * 1024},
		{"100KB", 100 * 1024},
	}

	for _, sz := range sizes {
		data := make([]byte, sz.size)
		for i := range data {
			data[i] = byte(i % 256)
		}

		b.Run(sz.name, func(b *testing.B) {
			b.SetBytes(int64(sz.size))
			b.ResetTimer()
			for b.Loop() {
				_, err := Encrypt(data, password)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkDecrypt(b *testing.B) {
	if testing.Short() {
		b.Skip("skipping slow crypto benchmark in short mode")
	}

	password := "benchmark-password-123"

	sizes := []struct {
		name string
		size int
	}{
		{"100B", 100},
		{"1KB", 1024},
		{"10KB", 10 * 1024},
	}

	for _, sz := range sizes {
		data := make([]byte, sz.size)
		for i := range data {
			data[i] = byte(i % 256)
		}

		encrypted, err := Encrypt(data, password)
		if err != nil {
			b.Fatal(err)
		}

		b.Run(sz.name, func(b *testing.B) {
			b.SetBytes(int64(sz.size))
			b.ResetTimer()
			for b.Loop() {
				_, err := Decrypt(encrypted, password)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
