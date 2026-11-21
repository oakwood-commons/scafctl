// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"encoding/json"
	"regexp"

	"github.com/onsi/gomega"
)

// ExecutionMetadataKey is the volatile key scafctl adds to run output when
// --show-execution is set; it carries timings and is never deterministic.
const ExecutionMetadataKey = "__execution"

// volatileRunKeys are top-level run-envelope fields that carry wall-clock
// timings and so never repeat across runs.
var volatileRunKeys = []string{ExecutionMetadataKey, "duration", "startTime", "endTime"}

// Masks for values that legitimately differ between runs. They are
// test-side only: the product has no "non-deterministic" provider flag.
var (
	// UUIDMask matches RFC 4122 UUIDs (e.g. from guid.new()).
	UUIDMask = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)
	// TimestampMask matches RFC 3339 timestamps with optional fractions.
	TimestampMask = regexp.MustCompile(`\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:?\d{2})?`)
)

// NormalizeJSON parses JSON run output, drops the volatile timing keys (ExecutionMetadataKey,
// duration, startTime, endTime) at the top level, re-marshals it with sorted keys (encoding/json sorts map keys) and
// replaces every match of the given masks with a fixed placeholder. Equal
// normalized strings mean semantically identical output.
func NormalizeJSON(raw []byte, masks ...*regexp.Regexp) string {
	var v any
	gomega.Expect(json.Unmarshal(raw, &v)).To(gomega.Succeed(), "output is not JSON: %s", raw)
	if m, ok := v.(map[string]any); ok {
		for _, k := range volatileRunKeys {
			delete(m, k)
		}
	}
	out, err := json.MarshalIndent(v, "", "  ")
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	s := string(out)
	for _, re := range masks {
		s = re.ReplaceAllString(s, "<MASKED>")
	}
	return s
}
