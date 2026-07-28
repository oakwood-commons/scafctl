// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package env

import (
	"context"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/celexp/ext"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestValidateSyntaxRuntimeParity asserts the core contract from the
// validate-expression optional-chaining bug: every expression the runtime
// evaluation environment can parse, celexp.ValidateSyntax must also accept.
//
// It registers the real runtime factory (New) -- the same environment used to
// evaluate expressions -- so celexp.ValidateSyntax parses through the production
// code path rather than a minimal fallback. The corpus is the documented set of
// optionalTypes examples (the syntax the bug incorrectly rejected), plus a few
// explicit optional-access and chaining cases.
func TestValidateSyntaxRuntimeParity(t *testing.T) {
	// Register the runtime factory so celexp.NewParseEnv (used by ValidateSyntax)
	// builds the same environment the evaluator uses. SetEnvFactory is guarded by
	// sync.Once; registering New here is the production configuration.
	celexp.SetEnvFactory(New)

	ctx := context.Background()

	// Gather the documented optional-types examples -- the exact syntax the bug
	// rejected during validation while accepting it during evaluation.
	var corpus []string
	for _, fn := range ext.All() {
		if fn.Name != "optionalTypes" {
			continue
		}
		for _, ex := range fn.Examples {
			if ex.Expression != "" {
				corpus = append(corpus, ex.Expression)
			}
		}
	}
	require.NotEmpty(t, corpus, "expected optionalTypes examples to form a parity corpus")

	// Explicit optional-access and chaining cases (regression for the reported
	// '.?' false negative), including the exact expression from the issue.
	corpus = append(corpus,
		`_.?name.orValue("fallback")`,
		`_.?config.?host.orValue("localhost")`,
		`_[?"name"].orValue("x")`,
	)

	// Build the runtime environment once for the parity comparison.
	runtimeEnv, err := New(ctx)
	require.NoError(t, err)

	for _, expr := range corpus {
		t.Run(expr, func(t *testing.T) {
			// The runtime environment must be able to parse the expression...
			_, issues := runtimeEnv.Parse(expr)
			require.NoError(t, issues.Err(), "runtime env failed to parse: %s", expr)

			// ...and so must ValidateSyntax, or the two have drifted apart.
			assert.NoError(t, celexp.ValidateSyntax(ctx, expr),
				"ValidateSyntax rejected an expression the runtime accepts: %s", expr)
		})
	}
}
