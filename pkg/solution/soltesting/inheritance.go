// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package soltesting

import (
	"fmt"
	"strings"
)

// ResolveExtends resolves all extends chains in the tests map in-place.
// For each test with extends, fields are merged from the referenced templates
// left-to-right using the following merge strategy:
//
//   - command: child wins if set
//   - args: appended (base first, then child)
//   - assertions: appended (base first, then child)
//   - files: appended, deduplicated
//   - init: base steps prepended before child steps
//   - cleanup: base steps appended after child steps
//   - tags: appended, deduplicated
//   - env: merged map, child wins on key conflict
//   - Scalar fields: child wins if set
//
// Returns an error if a circular dependency is detected, an extends reference
// does not exist, or the inheritance depth exceeds MaxExtendsDepth.
func ResolveExtends(tests map[string]*TestCase) error {
	// Validate all extends references exist
	for name, tc := range tests {
		for _, ext := range tc.Extends {
			if _, ok := tests[ext]; !ok {
				return fmt.Errorf("test %q extends %q which does not exist", name, ext)
			}
		}
	}

	// Validate depth limits before resolution
	for name := range tests {
		depth := computeExtendsDepth(name, tests, make(map[string]bool))
		if depth > MaxExtendsDepth {
			return fmt.Errorf("extends depth exceeds maximum of %d for test %q", MaxExtendsDepth, name)
		}
	}

	resolved := make(map[string]bool)
	resolving := make(map[string]bool)

	for name := range tests {
		if err := resolveOne(name, tests, resolved, resolving, 0); err != nil {
			return err
		}
	}

	return nil
}

// resolveOne recursively resolves a single test's extends chain.
func resolveOne(name string, tests map[string]*TestCase, resolved, resolving map[string]bool, depth int) error {
	if resolved[name] {
		return nil
	}
	if resolving[name] {
		return fmt.Errorf("circular extends dependency detected involving %q", name)
	}

	tc := tests[name]
	if len(tc.Extends) == 0 {
		resolved[name] = true
		return nil
	}

	resolving[name] = true

	// Resolve all parents first (left-to-right)
	for _, ext := range tc.Extends {
		if err := resolveOne(ext, tests, resolved, resolving, depth+1); err != nil {
			return err
		}
	}

	// Apply templates left-to-right, building a merged base
	merged := &TestCase{Name: tc.Name}
	for _, ext := range tc.Extends {
		parent := tests[ext]
		mergeTestCase(merged, parent)
	}

	// Now merge child (tc) on top of the accumulated base
	mergeTestCase(merged, tc)

	// Copy the merged result back, preserving name and clearing extends
	merged.Extends = nil
	*tc = *merged

	delete(resolving, name)
	resolved[name] = true

	return nil
}

// computeExtendsDepth computes the maximum extends chain depth for a test.
// It follows the first extends reference at each level to compute the longest chain.
func computeExtendsDepth(name string, tests map[string]*TestCase, visited map[string]bool) int {
	if visited[name] {
		return 0 // circular — handled separately
	}
	visited[name] = true

	tc, ok := tests[name]
	if !ok || len(tc.Extends) == 0 {
		return 0
	}

	maxDepth := 0
	for _, ext := range tc.Extends {
		d := computeExtendsDepth(ext, tests, visited)
		if d > maxDepth {
			maxDepth = d
		}
	}
	return maxDepth + 1
}

// mergeTestCase merges src into dst following the defined merge strategy.
// src fields take precedence according to the merge rules.
func mergeTestCase(dst, src *TestCase) {
	// command: src wins if set
	if len(src.Command) > 0 {
		dst.Command = src.Command
	}

	// args: appended (dst first, then src)
	if len(src.Args) > 0 {
		dst.Args = append(dst.Args, src.Args...)
	}

	// assertions: appended (dst first, then src)
	if len(src.Assertions) > 0 {
		dst.Assertions = append(dst.Assertions, src.Assertions...)
	}

	// files: appended, deduplicated
	if len(src.Files) > 0 {
		dst.Files = appendDedup(dst.Files, src.Files)
	}

	// init: src steps come after dst steps (base prepended, child appended)
	if len(src.Init) > 0 {
		dst.Init = append(dst.Init, src.Init...)
	}

	// cleanup: src steps appended after dst steps
	if len(src.Cleanup) > 0 {
		dst.Cleanup = append(dst.Cleanup, src.Cleanup...)
	}

	// tags: appended, deduplicated
	if len(src.Tags) > 0 {
		dst.Tags = appendDedup(dst.Tags, src.Tags)
	}

	// env: merged map, src wins on key conflict
	if len(src.Env) > 0 {
		if dst.Env == nil {
			dst.Env = make(map[string]string)
		}
		for k, v := range src.Env {
			dst.Env[k] = v
		}
	}

	// Scalar fields: src wins if set
	if src.Description != "" {
		dst.Description = src.Description
	}
	if src.Timeout != nil {
		dst.Timeout = src.Timeout
	}
	if src.ExpectFailure {
		dst.ExpectFailure = true
	}
	if src.ExitCode != nil {
		dst.ExitCode = src.ExitCode
	}
	if !src.Skip.IsZero() {
		dst.Skip = src.Skip
	}
	if src.SkipReason != "" {
		dst.SkipReason = src.SkipReason
	}
	if src.InjectFile != nil {
		dst.InjectFile = src.InjectFile
	}
	if src.Snapshot != "" {
		dst.Snapshot = src.Snapshot
	}
	if src.Retries > 0 {
		dst.Retries = src.Retries
	}
}

// appendDedup appends items from src to dst, skipping duplicates.
func appendDedup(dst, src []string) []string {
	seen := make(map[string]bool, len(dst))
	for _, s := range dst {
		seen[s] = true
	}
	for _, s := range src {
		if !seen[s] {
			dst = append(dst, s)
			seen[s] = true
		}
	}
	return dst
}

// ExtendsChainString returns a human-readable representation of the extends chain
// for diagnostic purposes.
func ExtendsChainString(tests map[string]*TestCase, name string) string {
	var chain []string
	visited := make(map[string]bool)

	current := name
	for {
		if visited[current] {
			chain = append(chain, current+" (circular)")
			break
		}
		visited[current] = true
		chain = append(chain, current)

		tc, ok := tests[current]
		if !ok || len(tc.Extends) == 0 {
			break
		}
		current = tc.Extends[0]
	}

	return strings.Join(chain, " -> ")
}
