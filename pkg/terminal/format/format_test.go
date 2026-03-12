// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package format

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestBytes(t *testing.T) {
	tests := []struct {
		name     string
		bytes    int64
		expected string
	}{
		{"zero", 0, "0 B"},
		{"small bytes", 500, "500 B"},
		{"one KB", 1024, "1.0 KB"},
		{"fractional KB", 1536, "1.5 KB"},
		{"one MB", 1024 * 1024, "1.0 MB"},
		{"fractional MB", 1572864, "1.5 MB"},
		{"one GB", 1024 * 1024 * 1024, "1.0 GB"},
		{"one TB", 1024 * 1024 * 1024 * 1024, "1.0 TB"},
		{"large size", 2684354560, "2.5 GB"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, Bytes(tt.bytes))
		})
	}
}

func TestDuration(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		expected string
	}{
		{"microseconds", 500 * time.Microsecond, "500µs"},
		{"milliseconds", 150 * time.Millisecond, "150ms"},
		{"one second", time.Second, "1.00s"},
		{"seconds with decimal", 1500 * time.Millisecond, "1.50s"},
		{"sub-millisecond", 100 * time.Microsecond, "100µs"},
		{"large duration", 65 * time.Second, "65.00s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, Duration(tt.duration))
		})
	}
}

func BenchmarkBytes(b *testing.B) {
	sizes := []int64{0, 500, 1024, 1572864, 1024 * 1024 * 1024}
	for _, size := range sizes {
		b.Run(Bytes(size), func(b *testing.B) {
			for b.Loop() {
				Bytes(size)
			}
		})
	}
}

func BenchmarkDuration(b *testing.B) {
	durations := []time.Duration{
		500 * time.Microsecond,
		150 * time.Millisecond,
		1500 * time.Millisecond,
		65 * time.Second,
	}
	for _, d := range durations {
		b.Run(Duration(d), func(b *testing.B) {
			for b.Loop() {
				Duration(d)
			}
		})
	}
}
