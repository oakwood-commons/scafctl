// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package solution

import (
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/authorfuncs"
)

// TemplateFuncBinder compiles the solution's author-defined template functions
// (spec.functions) into a binder that can be injected into go-template provider
// execution. It returns (nil, nil) when the solution declares no functions, and
// an error when the declarations are invalid.
//
// Callers wire the returned binder into resolver/action execution once at setup
// (mirroring how spec.calls are wired), so template rendering can invoke the
// author's helpers as {{ name arg... }}.
func (s *Solution) TemplateFuncBinder() (*authorfuncs.Library, error) {
	if s == nil || !s.Spec.HasFunctions() {
		return nil, nil
	}
	return authorfuncs.Compile(s.Spec.Functions)
}

// validateFunctions validates the solution's author-defined template functions,
// returning a problem string per validation failure. It reuses the authorfuncs
// compiler so the validation surfaced here matches what execution enforces.
func (s *Solution) validateFunctions() []string {
	if s == nil || !s.Spec.HasFunctions() {
		return nil
	}

	_, err := authorfuncs.Compile(s.Spec.Functions)
	if err == nil {
		return nil
	}

	// Compile aggregates problems into a single error prefixed with
	// "invalid spec.functions: " and joined by "; ". Split it back into
	// individual problems so they align with the surrounding validation output.
	msg := strings.TrimPrefix(err.Error(), "invalid spec.functions: ")
	return strings.Split(msg, "; ")
}
