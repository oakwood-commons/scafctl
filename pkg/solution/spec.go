// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package solution

import (
	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/solution/soltesting"
)

// Spec defines the execution specification for a solution.
// It contains resolvers that perform data resolution, transformation, and validation,
// and optionally a workflow that defines actions to execute.
type Spec struct {
	// Resolvers is a map of resolver definitions keyed by their name.
	// Resolver names must be unique and cannot start with "__" (reserved for internal use).
	Resolvers map[string]*resolver.Resolver `json:"resolvers,omitempty" yaml:"resolvers,omitempty" doc:"Resolver definitions keyed by name"`

	// Workflow defines the action execution specification.
	// Actions consume resolved data from resolvers and perform side-effect operations.
	// All resolvers are evaluated before any action execution begins.
	Workflow *action.Workflow `json:"workflow,omitempty" yaml:"workflow,omitempty" doc:"Action workflow specification"`

	// Testing groups all test-related configuration (test cases and config) under one property.
	Testing *soltesting.TestSuite `json:"testing,omitempty" yaml:"testing,omitempty" doc:"Test suite configuration"`
}

// ResolversToSlice converts the Resolvers map to a slice for execution.
// It ensures each resolver's Name field is set to match its map key.
func (s *Spec) ResolversToSlice() []*resolver.Resolver {
	if s == nil || s.Resolvers == nil {
		return nil
	}

	result := make([]*resolver.Resolver, 0, len(s.Resolvers))
	for name, r := range s.Resolvers {
		if r == nil {
			continue
		}
		// Ensure the resolver's Name matches its key in the map
		r.Name = name
		result = append(result, r)
	}
	return result
}

// HasResolvers returns true if the spec contains any resolver definitions.
func (s *Spec) HasResolvers() bool {
	return s != nil && len(s.Resolvers) > 0
}

// HasWorkflow returns true if the spec contains a workflow definition.
func (s *Spec) HasWorkflow() bool {
	return s != nil && s.Workflow != nil
}

// HasActions returns true if the workflow contains any actions (regular or finally).
func (s *Spec) HasActions() bool {
	if !s.HasWorkflow() {
		return false
	}
	return len(s.Workflow.Actions) > 0 || len(s.Workflow.Finally) > 0
}

// HasTesting returns true if the spec has a testing suite defined.
func (s *Spec) HasTesting() bool {
	return s != nil && s.Testing != nil
}

// HasTests returns true if the spec contains any test definitions.
func (s *Spec) HasTests() bool {
	return s != nil && s.Testing.HasCases()
}

// HasTestConfig returns true if the spec has test configuration.
func (s *Spec) HasTestConfig() bool {
	return s != nil && s.Testing.HasConfig()
}
