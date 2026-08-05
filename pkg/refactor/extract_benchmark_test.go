// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package refactor

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/solution"
)

// BenchmarkExtractCall measures a full Extract Call over a representative
// solution: build the node map, locate and span the step block, render the
// calls entry, and produce the edits. Apply is included to capture the
// end-to-end rewrite cost.
func BenchmarkExtractCall(b *testing.B) {
	sol := &solution.Solution{}
	if err := sol.UnmarshalFromBytes([]byte(extractFixture)); err != nil {
		b.Fatal(err)
	}
	raw := sol.RawContent()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		res, err := ExtractCall(sol, "spec.resolvers.environment.resolve.with[0]", "getEnv")
		if err != nil {
			b.Fatal(err)
		}
		if _, err := Apply(raw, res.Edits); err != nil {
			b.Fatal(err)
		}
	}
}
