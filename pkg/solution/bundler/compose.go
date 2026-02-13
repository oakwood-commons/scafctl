// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package bundler

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/soltesting"
	"gopkg.in/yaml.v3"
)

// composePart represents a partial YAML file that contributes resolvers,
// workflow actions, tests, and/or bundle includes to the parent solution.
type composePart struct {
	Spec struct {
		Resolvers  map[string]*resolver.Resolver   `yaml:"resolvers"`
		Workflow   *action.Workflow                `yaml:"workflow"`
		Tests      map[string]*soltesting.TestCase `yaml:"tests"`
		TestConfig *soltesting.TestConfig          `yaml:"testConfig"`
	} `yaml:"spec"`
	Bundle struct {
		Include []string `yaml:"include"`
	} `yaml:"bundle"`
}

// ComposeOption configures Compose behavior.
type ComposeOption func(*composeConfig)

type composeConfig struct {
	readFile func(string) ([]byte, error)
}

// WithReadFileFunc overrides the function used to read composed files.
// Useful for testing without touching the filesystem.
func WithReadFileFunc(fn func(string) ([]byte, error)) ComposeOption {
	return func(c *composeConfig) {
		c.readFile = fn
	}
}

// Compose loads and merges all composed files referenced by the solution.
// The composed files are expected to contain partial YAML with spec.resolvers,
// spec.workflow.actions, and/or bundle.include sections.
//
// Returns a new Solution with all parts merged. The original is not modified.
// bundleRoot is the directory containing the root solution YAML — composed file
// paths are resolved relative to it.
//
// Merge rules:
//   - Resolvers: merged by name. Duplicate resolver names across files are rejected.
//   - Actions: merged by name. Duplicate action names across files are rejected.
//   - Finally actions: merged by name. Same duplicate rules apply.
//   - Tests: merged by name. Duplicate test names across files are rejected.
//   - TestConfig: skipBuiltins (true-wins for bool, union for lists), env (last wins),
//     setup/cleanup (appended in compose-file order).
//   - bundle.include: unioned (deduplicated).
//   - Circular compose references are detected and rejected.
func Compose(sol *solution.Solution, bundleRoot string, opts ...ComposeOption) (*solution.Solution, error) {
	if sol == nil {
		return nil, fmt.Errorf("solution is nil")
	}
	if len(sol.Compose) == 0 {
		return sol, nil
	}

	cfg := &composeConfig{
		readFile: os.ReadFile,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	// Deep-copy the solution so the original is not modified.
	merged, err := deepCopySolution(sol)
	if err != nil {
		return nil, fmt.Errorf("failed to copy solution for compose: %w", err)
	}

	// Track visited files for circular reference detection.
	visited := map[string]bool{}
	if sol.GetPath() != "" {
		visited[filepath.Clean(sol.GetPath())] = true
	}

	for _, relPath := range sol.Compose {
		absPattern := filepath.Join(bundleRoot, relPath)

		// Expand glob patterns (e.g., "tests/*.yaml").
		matches, globErr := doublestar.FilepathGlob(absPattern)
		if globErr != nil {
			return nil, fmt.Errorf("failed to expand compose glob %s: %w", relPath, globErr)
		}

		// If no glob metacharacters, treat as a literal path.
		if len(matches) == 0 {
			matches = []string{absPattern}
		}

		// Sort matches for deterministic ordering.
		sort.Strings(matches)

		for _, absPath := range matches {
			cleanPath := filepath.Clean(absPath)

			if visited[cleanPath] {
				return nil, fmt.Errorf("circular compose reference detected: %s", relPath)
			}
			visited[cleanPath] = true

			data, err := cfg.readFile(absPath)
			if err != nil {
				return nil, fmt.Errorf("failed to read composed file %s: %w", relPath, err)
			}

			var part composePart
			if err := yaml.Unmarshal(data, &part); err != nil {
				return nil, fmt.Errorf("failed to parse composed file %s: %w", relPath, err)
			}

			if err := mergeResolvers(merged, part.Spec.Resolvers, relPath); err != nil {
				return nil, err
			}

			if err := mergeWorkflow(merged, part.Spec.Workflow, relPath); err != nil {
				return nil, err
			}

			if err := mergeTests(merged, part.Spec.Tests, relPath); err != nil {
				return nil, err
			}

			mergeTestConfig(merged, part.Spec.TestConfig)

			mergeIncludes(merged, part.Bundle.Include)
		}
	}

	// Clear compose from the merged output — the result is fully composed.
	merged.Compose = nil

	return merged, nil
}

// mergeResolvers adds resolvers from a composed file into the merged solution.
// Duplicate resolver names are rejected.
func mergeResolvers(merged *solution.Solution, resolvers map[string]*resolver.Resolver, sourceFile string) error {
	if len(resolvers) == 0 {
		return nil
	}

	if merged.Spec.Resolvers == nil {
		merged.Spec.Resolvers = make(map[string]*resolver.Resolver)
	}

	for name, r := range resolvers {
		if _, exists := merged.Spec.Resolvers[name]; exists {
			return fmt.Errorf("duplicate resolver %q: defined in both root solution and composed file %s", name, sourceFile)
		}
		merged.Spec.Resolvers[name] = r
	}
	return nil
}

// mergeWorkflow adds actions from a composed workflow into the merged solution.
// Duplicate action names are rejected.
func mergeWorkflow(merged *solution.Solution, workflow *action.Workflow, sourceFile string) error {
	if workflow == nil {
		return nil
	}

	if merged.Spec.Workflow == nil {
		merged.Spec.Workflow = &action.Workflow{}
	}

	// Merge regular actions
	if len(workflow.Actions) > 0 {
		if merged.Spec.Workflow.Actions == nil {
			merged.Spec.Workflow.Actions = make(map[string]*action.Action)
		}
		for name, a := range workflow.Actions {
			if _, exists := merged.Spec.Workflow.Actions[name]; exists {
				return fmt.Errorf("duplicate action %q: defined in both root solution and composed file %s", name, sourceFile)
			}
			merged.Spec.Workflow.Actions[name] = a
		}
	}

	// Merge finally actions
	if len(workflow.Finally) > 0 {
		if merged.Spec.Workflow.Finally == nil {
			merged.Spec.Workflow.Finally = make(map[string]*action.Action)
		}
		for name, a := range workflow.Finally {
			if _, exists := merged.Spec.Workflow.Finally[name]; exists {
				return fmt.Errorf("duplicate finally action %q: defined in both root solution and composed file %s", name, sourceFile)
			}
			merged.Spec.Workflow.Finally[name] = a
		}
	}

	return nil
}

// mergeIncludes unions bundle.include patterns from a composed file, deduplicating.
func mergeIncludes(merged *solution.Solution, includes []string) {
	if len(includes) == 0 {
		return
	}

	existing := make(map[string]bool, len(merged.Bundle.Include))
	for _, inc := range merged.Bundle.Include {
		existing[inc] = true
	}

	for _, inc := range includes {
		if !existing[inc] {
			merged.Bundle.Include = append(merged.Bundle.Include, inc)
			existing[inc] = true
		}
	}
}

// mergeTests adds tests from a composed file into the merged solution.
// Duplicate test names are rejected (reject-duplicates strategy).
func mergeTests(merged *solution.Solution, tests map[string]*soltesting.TestCase, sourceFile string) error {
	if len(tests) == 0 {
		return nil
	}

	if merged.Spec.Tests == nil {
		merged.Spec.Tests = make(map[string]*soltesting.TestCase)
	}

	for name, tc := range tests {
		if _, exists := merged.Spec.Tests[name]; exists {
			return fmt.Errorf("duplicate test %q: defined in both root solution and composed file %s", name, sourceFile)
		}
		// Set the Name field from the map key
		tc.Name = name
		merged.Spec.Tests[name] = tc
	}
	return nil
}

// mergeTestConfig merges test configuration from a composed file into the merged solution.
// Merge rules:
//   - SkipBuiltins: true-wins for bool; union (deduplicated) for lists.
//   - Env: merged map, last compose file wins on key conflict.
//   - Setup: appended in compose-file order.
//   - Cleanup: appended in compose-file order.
func mergeTestConfig(merged *solution.Solution, tc *soltesting.TestConfig) {
	if tc == nil {
		return
	}

	if merged.Spec.TestConfig == nil {
		merged.Spec.TestConfig = &soltesting.TestConfig{}
	}

	// SkipBuiltins: true-wins for bool; union for lists
	if tc.SkipBuiltins.All {
		merged.Spec.TestConfig.SkipBuiltins.All = true
	}
	if len(tc.SkipBuiltins.Names) > 0 && !merged.Spec.TestConfig.SkipBuiltins.All {
		existing := make(map[string]bool, len(merged.Spec.TestConfig.SkipBuiltins.Names))
		for _, n := range merged.Spec.TestConfig.SkipBuiltins.Names {
			existing[n] = true
		}
		for _, n := range tc.SkipBuiltins.Names {
			if !existing[n] {
				merged.Spec.TestConfig.SkipBuiltins.Names = append(merged.Spec.TestConfig.SkipBuiltins.Names, n)
				existing[n] = true
			}
		}
	}

	// Env: merged map, last file wins on key conflict
	if len(tc.Env) > 0 {
		if merged.Spec.TestConfig.Env == nil {
			merged.Spec.TestConfig.Env = make(map[string]string)
		}
		for k, v := range tc.Env {
			merged.Spec.TestConfig.Env[k] = v
		}
	}

	// Setup: appended in compose-file order
	merged.Spec.TestConfig.Setup = append(merged.Spec.TestConfig.Setup, tc.Setup...)

	// Cleanup: appended in compose-file order
	merged.Spec.TestConfig.Cleanup = append(merged.Spec.TestConfig.Cleanup, tc.Cleanup...)
}

// deepCopySolution creates a deep copy of a solution by marshaling to YAML and back.
func deepCopySolution(sol *solution.Solution) (*solution.Solution, error) {
	data, err := sol.ToYAML()
	if err != nil {
		return nil, fmt.Errorf("failed to marshal solution: %w", err)
	}
	var cp solution.Solution
	if err := cp.UnmarshalFromBytes(data); err != nil {
		return nil, fmt.Errorf("failed to unmarshal solution copy: %w", err)
	}
	cp.SetPath(sol.GetPath())
	return &cp, nil
}
