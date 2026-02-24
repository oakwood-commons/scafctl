// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"sort"
	"strings"

	"github.com/go-logr/logr"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/celexp/ext"
	"github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/lint"
	"github.com/oakwood-commons/scafctl/pkg/examples"
	"github.com/oakwood-commons/scafctl/pkg/provider"
)

// promptCompletionProvider provides auto-completion for prompt arguments.
type promptCompletionProvider struct {
	registry *provider.Registry
}

// CompletePromptArgument returns completions for a prompt argument.
func (p *promptCompletionProvider) CompletePromptArgument(
	_ context.Context,
	promptName string,
	argument mcp.CompleteArgument,
	_ mcp.CompleteContext,
) (*mcp.Completion, error) {
	prefix := strings.ToLower(argument.Value)

	switch {
	case argument.Name == "provider":
		return p.completeProviderNames(prefix)
	case argument.Name == "migration" && promptName == "migrate_solution":
		return completeMigrationTypes(prefix)
	case argument.Name == "features":
		return completeFeatures(prefix)
	}

	return &mcp.Completion{}, nil
}

func (p *promptCompletionProvider) completeProviderNames(prefix string) (*mcp.Completion, error) {
	if p.registry == nil {
		return &mcp.Completion{}, nil
	}

	var matches []string
	for _, name := range p.registry.List() {
		if prefix == "" || strings.HasPrefix(strings.ToLower(name), prefix) {
			matches = append(matches, name)
		}
	}
	sort.Strings(matches)
	if len(matches) > 100 {
		matches = matches[:100]
	}

	return &mcp.Completion{
		Values:  matches,
		HasMore: false,
	}, nil
}

func completeMigrationTypes(prefix string) (*mcp.Completion, error) {
	types := []string{"composition", "templates", "split", "tests", "upgrade"}
	var matches []string
	for _, t := range types {
		if prefix == "" || strings.HasPrefix(t, prefix) {
			matches = append(matches, t)
		}
	}
	return &mcp.Completion{
		Values:  matches,
		HasMore: false,
	}, nil
}

func completeFeatures(prefix string) (*mcp.Completion, error) {
	features := []string{"resolvers", "actions", "transforms", "validation", "parameters", "composition", "tests"}
	var matches []string
	for _, f := range features {
		if prefix == "" || strings.HasPrefix(f, prefix) {
			matches = append(matches, f)
		}
	}
	return &mcp.Completion{
		Values:  matches,
		HasMore: false,
	}, nil
}

// resourceCompletionProvider provides auto-completion for resource template URIs.
type resourceCompletionProvider struct {
	registry *provider.Registry
	logger   logr.Logger
	ctx      context.Context
}

// CompleteResourceArgument returns completions for a resource template argument.
func (r *resourceCompletionProvider) CompleteResourceArgument(
	_ context.Context,
	uri string,
	argument mcp.CompleteArgument,
	_ mcp.CompleteContext,
) (*mcp.Completion, error) {
	prefix := strings.ToLower(argument.Value)

	switch {
	case strings.HasPrefix(uri, "provider://"):
		return r.completeProviderNames(prefix)
	case strings.HasPrefix(uri, "solution://"):
		return r.completeSolutionNames(prefix)
	}

	return &mcp.Completion{}, nil
}

func (r *resourceCompletionProvider) completeProviderNames(prefix string) (*mcp.Completion, error) {
	if r.registry == nil {
		return &mcp.Completion{}, nil
	}

	var matches []string
	for _, name := range r.registry.List() {
		if prefix == "" || strings.HasPrefix(strings.ToLower(name), prefix) {
			matches = append(matches, name)
		}
	}
	sort.Strings(matches)
	if len(matches) > 100 {
		matches = matches[:100]
	}

	return &mcp.Completion{
		Values:  matches,
		HasMore: false,
	}, nil
}

func (r *resourceCompletionProvider) completeSolutionNames(prefix string) (*mcp.Completion, error) {
	cat, err := catalog.NewLocalCatalog(r.logger)
	if err != nil {
		// Return empty completions when catalog is unavailable — not an error for completions.
		return &mcp.Completion{}, nil //nolint:nilerr // gracefully degrade
	}

	entries, err := cat.List(r.ctx, catalog.ArtifactKindSolution, "")
	if err != nil {
		// Return empty completions on list failure — not an error for completions.
		return &mcp.Completion{}, nil //nolint:nilerr // gracefully degrade
	}

	var matches []string
	for _, entry := range entries {
		name := strings.ToLower(entry.Reference.Name)
		if prefix == "" || strings.HasPrefix(name, prefix) {
			matches = append(matches, entry.Reference.Name)
		}
	}
	sort.Strings(matches)
	if len(matches) > 100 {
		matches = matches[:100]
	}

	return &mcp.Completion{
		Values:  matches,
		HasMore: len(entries) > 100,
	}, nil
}

// toolArgCompletions returns common completions used by tool arguments.
// These are used in tool descriptions to hint at valid values.
type toolArgCompletions struct {
	registry *provider.Registry
}

// ProviderNames returns available provider names.
func (c *toolArgCompletions) ProviderNames() []string {
	if c.registry == nil {
		return nil
	}
	names := c.registry.List()
	sort.Strings(names)
	return names
}

// LintRuleNames returns available lint rule names.
func (c *toolArgCompletions) LintRuleNames() []string {
	rules := lint.ListRules()
	names := make([]string, 0, len(rules))
	for _, r := range rules {
		names = append(names, r.Rule)
	}
	sort.Strings(names)
	return names
}

// CELFunctionNames returns available CEL function names.
func (c *toolArgCompletions) CELFunctionNames() []string {
	functions := ext.All()
	names := make([]string, 0, len(functions))
	for _, f := range functions {
		names = append(names, f.Name)
	}
	sort.Strings(names)
	return names
}

// ExampleNames returns available example file names.
func (c *toolArgCompletions) ExampleNames() []string {
	files, err := examples.Scan("")
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(files))
	for _, f := range files {
		names = append(names, f.Path)
	}
	sort.Strings(names)
	return names
}
