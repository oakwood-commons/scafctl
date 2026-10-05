// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package builder

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
)

// Byte-size unit multipliers accepted by ParseBundleLimits.
const (
	byteUnit     int64 = 1
	kilobyteUnit       = 1024 * byteUnit
	megabyteUnit       = 1024 * kilobyteUnit
	gigabyteUnit       = 1024 * megabyteUnit
)

// byteSizeSuffixes lists unit suffixes longest first so "KB" is not matched
// as "B".
var byteSizeSuffixes = []struct {
	suffix string
	mult   int64
}{
	{"GB", gigabyteUnit},
	{"MB", megabyteUnit},
	{"KB", kilobyteUnit},
	{"B", byteUnit},
}

// ErrInvalidByteSize is returned when a byte size is not a non-negative whole
// number with an optional B, KB, MB, or GB suffix, or does not fit in int64.
var ErrInvalidByteSize = errors.New("invalid byte size")

// BundleLimits are the size limits applied while building a bundle.
type BundleLimits struct {
	// MaxSize is the maximum total size of bundled files, in bytes. Always > 0.
	MaxSize int64
	// DedupeThreshold is the minimum file size, in bytes, for a file to get
	// its own layer in a deduplicated bundle; smaller files share one tar
	// layer. Always >= 0.
	DedupeThreshold int64
}

// ParseBundleLimits parses human-readable sizes such as "50MB" or "4KB".
// An empty value uses the bundler default. Sizes must be whole numbers;
// fractions, signs, trailing characters, and values that overflow int64 are
// rejected. A zero max size is rejected because nothing could be bundled.
func ParseBundleLimits(maxSize, dedupeThreshold string) (BundleLimits, error) {
	limits := BundleLimits{
		MaxSize:         bundler.DefaultMaxBundleSize,
		DedupeThreshold: bundler.DefaultDedupeThreshold,
	}

	if strings.TrimSpace(maxSize) != "" {
		n, err := parseByteSizeStrict(maxSize)
		if err != nil {
			return BundleLimits{}, fmt.Errorf("bundle max size: %w", err)
		}
		if n == 0 {
			return BundleLimits{}, fmt.Errorf("bundle max size: %w %q: must be greater than 0", ErrInvalidByteSize, maxSize)
		}
		limits.MaxSize = n
	}

	if strings.TrimSpace(dedupeThreshold) != "" {
		n, err := parseByteSizeStrict(dedupeThreshold)
		if err != nil {
			return BundleLimits{}, fmt.Errorf("dedupe threshold: %w", err)
		}
		limits.DedupeThreshold = n
	}

	return limits, nil
}

// parseByteSizeStrict parses a non-negative whole number with an optional,
// case-insensitive B/KB/MB/GB suffix. Unlike ParseByteSize it consumes the
// whole input, so "1.5MB" and "10abcMB" are errors rather than 1MB and 10MB.
func parseByteSizeStrict(s string) (int64, error) {
	num := strings.ToUpper(strings.TrimSpace(s))
	mult := byteUnit
	for _, entry := range byteSizeSuffixes {
		if strings.HasSuffix(num, entry.suffix) {
			num = strings.TrimSpace(strings.TrimSuffix(num, entry.suffix))
			mult = entry.mult
			break
		}
	}

	if num == "" || num[0] == '+' || num[0] == '-' {
		return 0, fmt.Errorf("%w %q", ErrInvalidByteSize, s)
	}
	n, err := strconv.ParseInt(num, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%w %q", ErrInvalidByteSize, s)
	}
	if n > math.MaxInt64/mult {
		return 0, fmt.Errorf("%w %q: too large", ErrInvalidByteSize, s)
	}
	return n * mult, nil
}
