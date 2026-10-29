// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package builder

import (
	"math"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
)

func TestParseByteSizeStrict(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in      string
		want    int64
		wantErr bool
	}{
		{in: "0", want: 0},
		{in: "100", want: 100},
		{in: "100B", want: 100},
		{in: "4KB", want: 4 * kilobyteUnit},
		{in: "50MB", want: 50 * megabyteUnit},
		{in: "2GB", want: 2 * gigabyteUnit},
		{in: "50mb", want: 50 * megabyteUnit},
		{in: " 10 MB ", want: 10 * megabyteUnit},
		{in: strconv.FormatInt(math.MaxInt64, 10), want: math.MaxInt64},
		{in: "", wantErr: true},
		{in: "MB", wantErr: true},
		{in: "1.5MB", wantErr: true},
		{in: "10abcMB", wantErr: true},
		{in: "10TB", wantErr: true},
		{in: "-5MB", wantErr: true},
		{in: "+5MB", wantErr: true},
		{in: "9999999999999GB", wantErr: true},
		{in: "99999999999999999999", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			got, err := parseByteSizeStrict(tt.in)
			if tt.wantErr {
				require.ErrorIs(t, err, ErrInvalidByteSize)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseBundleLimits(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		maxSize   string
		threshold string
		want      BundleLimits
		errMsg    string
	}{
		{
			name: "empty uses defaults",
			want: BundleLimits{MaxSize: bundler.DefaultMaxBundleSize, DedupeThreshold: bundler.DefaultDedupeThreshold},
		},
		{
			name:      "whitespace uses defaults",
			maxSize:   "  ",
			threshold: " ",
			want:      BundleLimits{MaxSize: bundler.DefaultMaxBundleSize, DedupeThreshold: bundler.DefaultDedupeThreshold},
		},
		{
			name:      "explicit values",
			maxSize:   "100MB",
			threshold: "8KB",
			want:      BundleLimits{MaxSize: 100 * megabyteUnit, DedupeThreshold: 8 * kilobyteUnit},
		},
		{
			name:      "zero threshold is allowed",
			threshold: "0",
			want:      BundleLimits{MaxSize: bundler.DefaultMaxBundleSize, DedupeThreshold: 0},
		},
		{name: "zero max size", maxSize: "0", errMsg: "bundle max size"},
		{name: "invalid max size", maxSize: "1.5MB", errMsg: "bundle max size"},
		{name: "negative max size", maxSize: "-1MB", errMsg: "bundle max size"},
		{name: "invalid threshold", threshold: "4kb!", errMsg: "dedupe threshold"},
		{name: "overflowing threshold", threshold: "9999999999999GB", errMsg: "dedupe threshold"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseBundleLimits(tt.maxSize, tt.threshold)
			if tt.errMsg != "" {
				require.ErrorIs(t, err, ErrInvalidByteSize)
				assert.Contains(t, err.Error(), tt.errMsg)
				assert.Equal(t, BundleLimits{}, got)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func BenchmarkParseBundleLimits(b *testing.B) {
	for b.Loop() {
		_, _ = ParseBundleLimits("50MB", "4KB")
	}
}
