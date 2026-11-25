// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package inspect

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/spec"
)

// paramResolver builds a parameter-provider resolver with an optional literal default.
func paramResolver(key string, def any) *resolver.Resolver {
	inputs := map[string]*resolver.ValueRef{
		"key": {Literal: key},
	}
	if def != nil {
		inputs["default"] = &resolver.ValueRef{Literal: def}
	}
	return &resolver.Resolver{
		Type: "string",
		Resolve: &resolver.ResolvePhase{
			With: []resolver.ProviderSource{
				{Provider: "parameter", Inputs: inputs},
			},
		},
	}
}

func whenCondition(expr string) *spec.Condition {
	e := celexp.Expression(expr)
	return &spec.Condition{Expr: &e}
}

func TestBuildUsage(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	ver := semver.MustParse("1.2.3")
	sol := &solution.Solution{
		Metadata: solution.Metadata{
			Name:        "tf-registry",
			Version:     ver,
			Description: "Discovers Terraform modules and providers",
			Source:      "./solution.yaml",
			Usage: &spec.Usage{
				Synopsis: "Registry index for Terraform modules",
				Examples: []spec.UsageExample{
					{Description: "Refresh", Command: "scafctl run solution -r action=refresh"},
				},
			},
		},
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"action": paramResolver("action", "show"),
			},
			Workflow: &action.Workflow{
				Actions: map[string]*action.Action{
					"show": {
						Description: "Display registry summary",
						When:        whenCondition(`_.action == "show"`),
					},
					"refresh": {
						Description: "Fetch fresh data",
						When:        whenCondition(`_.action == "refresh"`),
					},
					"cleanup": {
						Description: "Explicit cleanup",
						Explicit:    true,
					},
				},
			},
		},
	}

	usage, err := BuildUsage(ctx, sol, "solution.yaml", "scafctl")
	require.NoError(t, err)

	// Synopsis prefers metadata.usage.synopsis.
	assert.Equal(t, "Registry index for Terraform modules", usage.Synopsis)
	assert.Equal(t, "tf-registry", usage.Name)
	assert.Equal(t, "1.2.3", usage.Version)
	assert.Equal(t, "scafctl run solution", usage.Run)
	require.Len(t, usage.Examples, 1)

	// Parameter 'action' has default "show" and discovered allowed values.
	require.Len(t, usage.Params, 1)
	p := usage.Params[0]
	assert.Equal(t, "action", p.Name)
	assert.Equal(t, "show", p.Default)
	assert.False(t, p.Required)
	assert.Equal(t, []any{"refresh", "show"}, p.AllowedValues)

	// Actions: sorted, with commands. cleanup is explicit -> run action; when-gated
	// actions get -r commands; none are "default" here (all gated or explicit).
	byName := map[string]ActionUsage{}
	for _, a := range usage.Actions {
		byName[a.Name] = a
	}
	assert.Equal(t, "scafctl run solution -f ./solution.yaml -r action=refresh", byName["refresh"].Command)
	assert.Equal(t, "scafctl run solution -f ./solution.yaml -r action=show", byName["show"].Command)
	assert.Equal(t, "scafctl run action cleanup -f ./solution.yaml", byName["cleanup"].Command)
	assert.False(t, byName["show"].Default)
	assert.False(t, byName["cleanup"].Default)
}

func TestBuildUsage_SynopsisFallsBackToDescription(t *testing.T) {
	t.Parallel()
	sol := &solution.Solution{
		Metadata: solution.Metadata{
			Name:        "app",
			Description: "A plain description",
		},
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"x": paramResolver("x", nil),
			},
		},
	}
	usage, err := BuildUsage(context.Background(), sol, "solution.yaml", "scafctl")
	require.NoError(t, err)
	assert.Equal(t, "A plain description", usage.Synopsis)
	// No default -> required.
	require.Len(t, usage.Params, 1)
	assert.True(t, usage.Params[0].Required)
}

func TestBuildUsage_EmbedderBinaryName(t *testing.T) {
	t.Parallel()
	sol := &solution.Solution{
		Metadata: solution.Metadata{Name: "app"},
		Spec: solution.Spec{
			Workflow: &action.Workflow{
				Actions: map[string]*action.Action{
					"build": {Description: "Build it"},
				},
			},
		},
	}
	usage, err := BuildUsage(context.Background(), sol, "solution.yaml", "mycli")
	require.NoError(t, err)
	assert.Equal(t, "mycli run solution", usage.Run)
	require.Len(t, usage.Actions, 1)
	// Default action (not explicit, no when) -> base run command (with source)
	// and the embedder binary name.
	assert.True(t, usage.Actions[0].Default)
	assert.Equal(t, "mycli run solution -f ./solution.yaml", usage.Actions[0].Command)
}

func TestBuildUsage_NoRunnableErrors(t *testing.T) {
	t.Parallel()
	sol := &solution.Solution{Metadata: solution.Metadata{Name: "empty"}}
	_, err := BuildUsage(context.Background(), sol, "solution.yaml", "scafctl")
	require.Error(t, err)
}

// Finally actions also contribute discovered allowed values from their when-clauses.
func TestBuildUsage_FinallyActionsDiscovered(t *testing.T) {
	t.Parallel()
	sol := &solution.Solution{
		Metadata: solution.Metadata{Name: "app"},
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"mode": paramResolver("mode", "normal"),
			},
			Workflow: &action.Workflow{
				Actions: map[string]*action.Action{
					"build": {Description: "Build"},
				},
				Finally: map[string]*action.Action{
					"cleanup": {
						Description: "Cleanup on teardown",
						When:        whenCondition(`_.mode == "teardown"`),
					},
				},
			},
		},
	}
	usage, err := BuildUsage(context.Background(), sol, "solution.yaml", "scafctl")
	require.NoError(t, err)
	require.Len(t, usage.Params, 1)
	// "teardown" is discovered from the finally action's when-clause.
	assert.Contains(t, usage.Params[0].AllowedValues, "teardown")
}

// A resolver-only solution (no workflow) yields no actions and no discovered values.
func TestBuildUsage_ResolverOnly(t *testing.T) {
	t.Parallel()
	sol := &solution.Solution{
		Metadata: solution.Metadata{Name: "app", Description: "desc"},
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"x": paramResolver("x", "default-x"),
			},
		},
	}
	usage, err := BuildUsage(context.Background(), sol, "solution.yaml", "scafctl")
	require.NoError(t, err)
	assert.Equal(t, "scafctl run resolver", usage.Run)
	assert.Empty(t, usage.Actions)
	require.Len(t, usage.Params, 1)
	assert.Empty(t, usage.Params[0].AllowedValues)
}

func BenchmarkBuildUsage(b *testing.B) {
	sol := &solution.Solution{
		Metadata: solution.Metadata{Name: "app"},
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"action": paramResolver("action", "show"),
			},
			Workflow: &action.Workflow{
				Actions: map[string]*action.Action{
					"show":    {Description: "Show", When: whenCondition(`_.action == "show"`)},
					"refresh": {Description: "Refresh", When: whenCondition(`_.action == "refresh"`)},
				},
			},
		},
	}
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		_, _ = BuildUsage(ctx, sol, "solution.yaml", "scafctl")
	}
}

// C: when the parameter provider's inputs.key differs from the resolver name,
// allowed values (discovered by resolver name via when-clauses) must attach to
// the CLI key, and the -r command must use the CLI key.
func TestBuildUsage_ParamKeyDiffersFromResolverName(t *testing.T) {
	t.Parallel()
	// Resolver named "environment" but CLI key is "env".
	envResolver := &resolver.Resolver{
		Type: "string",
		Resolve: &resolver.ResolvePhase{
			With: []resolver.ProviderSource{
				{Provider: "parameter", Inputs: map[string]*resolver.ValueRef{
					"key":     {Literal: "env"},
					"default": {Literal: "dev"},
				}},
			},
		},
	}
	sol := &solution.Solution{
		Metadata: solution.Metadata{Name: "app"},
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{"environment": envResolver},
			Workflow: &action.Workflow{
				Actions: map[string]*action.Action{
					"deploy": {
						Description: "Deploy",
						// when references the RESOLVER name, not the CLI key.
						When: whenCondition(`_.environment == "prod"`),
					},
				},
			},
		},
	}
	usage, err := BuildUsage(context.Background(), sol, "solution.yaml", "scafctl")
	require.NoError(t, err)

	// Parameter is displayed by its CLI key.
	require.Len(t, usage.Params, 1)
	assert.Equal(t, "env", usage.Params[0].Name)
	// Allowed value discovered from the resolver-name when-clause attaches to it.
	assert.Contains(t, usage.Params[0].AllowedValues, "prod")

	// The action command uses the CLI key (-r env=prod), not the resolver name.
	require.Len(t, usage.Actions, 1)
	assert.Equal(t, "scafctl run solution -f ./solution.yaml -r env=prod", usage.Actions[0].Command)
}

// D: a compound when-clause that is not fully reducible must NOT produce a
// misleading -r command (it would not actually satisfy the whole gate).
func TestBuildUsage_CompoundWhenNoMisleadingCommand(t *testing.T) {
	t.Parallel()
	sol := &solution.Solution{
		Metadata: solution.Metadata{Name: "app"},
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"mode": paramResolver("mode", "normal"),
			},
			Workflow: &action.Workflow{
				Actions: map[string]*action.Action{
					"go": {
						Description: "Go",
						When:        whenCondition(`_.mode == "prod" && _.enabled`),
					},
				},
			},
		},
	}
	usage, err := BuildUsage(context.Background(), sol, "solution.yaml", "scafctl")
	require.NoError(t, err)
	require.Len(t, usage.Actions, 1)
	// Falls back to the base command (no misleading -r mode=prod).
	assert.Equal(t, "scafctl run solution -f ./solution.yaml", usage.Actions[0].Command)
}

// E: generated -r values with spaces/metacharacters are shell-quoted.
func TestBuildUsage_ShellQuotesValues(t *testing.T) {
	t.Parallel()
	sol := &solution.Solution{
		Metadata: solution.Metadata{Name: "app"},
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"mode": paramResolver("mode", "normal"),
			},
			Workflow: &action.Workflow{
				Actions: map[string]*action.Action{
					"run": {
						Description: "Run",
						When:        whenCondition(`_.mode == "dry run"`),
					},
				},
			},
		},
	}
	usage, err := BuildUsage(context.Background(), sol, "solution.yaml", "scafctl")
	require.NoError(t, err)
	require.Len(t, usage.Actions, 1)
	assert.Equal(t, "scafctl run solution -f ./solution.yaml -r mode='dry run'", usage.Actions[0].Command)
}

// #345 completeness: the usage view surfaces displayName, tags, links, and
// per-parameter example.
func TestBuildUsage_MetadataFields(t *testing.T) {
	t.Parallel()

	// parameter resolver with an example.
	actionResolver := &resolver.Resolver{
		Type:        "string",
		Description: "What to do",
		Example:     "refresh",
		Resolve: &resolver.ResolvePhase{
			With: []resolver.ProviderSource{
				{Provider: "parameter", Inputs: map[string]*resolver.ValueRef{
					"key":     {Literal: "action"},
					"default": {Literal: "show"},
				}},
			},
		},
	}
	sol := &solution.Solution{
		Metadata: solution.Metadata{
			Name:        "tf-registry",
			DisplayName: "Terraform Registry",
			Description: "Discovers modules",
			Tags:        []string{"terraform", "registry"},
			Links: []solution.Link{
				{Name: "Docs", URL: "https://example.com/docs"},
				{Name: "Source", URL: "https://github.com/example/tf"},
				{URL: "https://example.com/nameless"}, // nameless link
			},
			Usage: &spec.Usage{
				Details: "Long-form prose about how this works.",
			},
		},
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{"action": actionResolver},
			Workflow: &action.Workflow{
				Actions: map[string]*action.Action{
					"go": {Description: "Go", When: whenCondition(`_.action == "go"`)},
				},
			},
		},
	}

	usage, err := BuildUsage(context.Background(), sol, "solution.yaml", "scafctl")
	require.NoError(t, err)

	assert.Equal(t, "Terraform Registry", usage.DisplayName)
	assert.Equal(t, []string{"terraform", "registry"}, usage.Tags)
	require.Len(t, usage.Links, 3)
	assert.Equal(t, "Docs", usage.Links[0].Name)
	assert.Equal(t, "https://example.com/docs", usage.Links[0].URL)
	// Nameless link is preserved with an empty name.
	assert.Empty(t, usage.Links[2].Name)
	assert.Equal(t, "https://example.com/nameless", usage.Links[2].URL)

	require.Len(t, usage.Params, 1)
	assert.Equal(t, "refresh", usage.Params[0].Example)

	assert.Equal(t, "Long-form prose about how this works.", usage.Details)
}

func TestAllActionsExplicit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		workflow *action.Workflow
		want     bool
	}{
		{
			name:     "nil workflow",
			workflow: nil,
			want:     false,
		},
		{
			name:     "empty actions",
			workflow: &action.Workflow{},
			want:     false,
		},
		{
			name: "finally-only workflow",
			workflow: &action.Workflow{
				Finally: map[string]*action.Action{
					"cleanup": {Explicit: true},
				},
			},
			want: false,
		},
		{
			name: "only nil action entries",
			workflow: &action.Workflow{
				Actions: map[string]*action.Action{
					"ghost": nil,
				},
			},
			want: false,
		},
		{
			name: "mixed explicit and default",
			workflow: &action.Workflow{
				Actions: map[string]*action.Action{
					"build":   {Explicit: false},
					"release": {Explicit: true},
				},
			},
			want: false,
		},
		{
			name: "when-gated non-explicit action",
			workflow: &action.Workflow{
				Actions: map[string]*action.Action{
					"show":    {When: whenCondition(`_.action == "show"`)},
					"cleanup": {Explicit: true},
				},
			},
			want: false,
		},
		{
			name: "all non-nil actions explicit",
			workflow: &action.Workflow{
				Actions: map[string]*action.Action{
					"build": {Explicit: true},
					"lint":  {Explicit: true},
					"nil":   nil,
				},
			},
			want: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, AllActionsExplicit(tc.workflow))
		})
	}
}

func TestBuildUsage_AllActionsExplicitAndAlias(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	sol := &solution.Solution{
		Metadata: solution.Metadata{Name: "ci-tasks"},
		Spec: solution.Spec{
			Workflow: &action.Workflow{
				Actions: map[string]*action.Action{
					"build": {
						Alias:       "b",
						Description: "Compile binary",
						Explicit:    true,
					},
					"lint": {
						Description: "Run linters",
						Explicit:    true,
					},
				},
			},
		},
	}

	// BuildRunCommand keeps "run solution" for non-usage callers (inspect solution, get_run_command).
	runCmd, err := BuildRunCommand(sol, "actions.yaml", "scafctl")
	require.NoError(t, err)
	assert.Equal(t, "scafctl run solution", runCmd.Subcommand)

	usage, err := BuildUsage(ctx, sol, "actions.yaml", "scafctl")
	require.NoError(t, err)
	assert.Equal(t, "scafctl run action <name>", usage.Run)
	require.Len(t, usage.Actions, 2)
	assert.Equal(t, "build", usage.Actions[0].Name)
	assert.Equal(t, "b", usage.Actions[0].Alias)
	assert.False(t, usage.Actions[0].Default)
	assert.Equal(t, "scafctl run action build -f ./actions.yaml", usage.Actions[0].Command)
	assert.Equal(t, "lint", usage.Actions[1].Name)
	assert.Empty(t, usage.Actions[1].Alias)
	assert.Equal(t, "scafctl run action lint -f ./actions.yaml", usage.Actions[1].Command)

	// Embedder binary name and stdin (-f -) source preservation.
	stdinUsage, err := BuildUsage(ctx, sol, "-", "mycli")
	require.NoError(t, err)
	assert.Equal(t, "mycli run action <name>", stdinUsage.Run)
	assert.Equal(t, "mycli run action build -f -", stdinUsage.Actions[0].Command)

	// URL, catalog, and remote OCI references convert to "-f <ref>" for explicit actions.
	for _, tc := range []struct {
		resolvedPath string
		rawPath      string
		wantCmd      string
	}{
		{
			resolvedPath: "https://example.com/actions.yaml",
			rawPath:      "https://example.com/actions.yaml",
			wantCmd:      "scafctl run action build -f https://example.com/actions.yaml",
		},
		{
			resolvedPath: "catalog:my-app@1.0.0",
			rawPath:      "my-app@1.0.0",
			wantCmd:      "scafctl run action build -f my-app@1.0.0",
		},
		{
			resolvedPath: "remote:ghcr.io/org/app@1.0.0",
			rawPath:      "ghcr.io/org/app@1.0.0",
			wantCmd:      "scafctl run action build -f ghcr.io/org/app@1.0.0",
		},
	} {
		refSol := *sol
		refSol.SetPath(tc.resolvedPath)
		refUsage, refErr := BuildUsage(ctx, &refSol, tc.rawPath, "scafctl")
		require.NoError(t, refErr)
		assert.Equal(t, tc.wantCmd, refUsage.Actions[0].Command)
	}

	// Catalog, version constraint, and pre-release selectors are preserved on both
	// default ("run solution ...") and explicit ("run action <name> -f ...") commands.
	scopedSol := &solution.Solution{
		Metadata: solution.Metadata{Name: "scoped-app"},
		Spec: solution.Spec{
			Workflow: &action.Workflow{
				Actions: map[string]*action.Action{
					"check":   {Description: "Default check"},
					"release": {Description: "Explicit release", Explicit: true},
				},
			},
		},
	}
	scopedSol.SetPath("catalog:official:scoped-app")
	scopedEntries := BuildActionList(
		ctx,
		scopedSol,
		"scoped-app",
		"scafctl",
		WithCatalog("official"),
		WithVersionConstraint("^1.0.0"),
		WithPreRelease(true),
	)
	require.Len(t, scopedEntries, 2)
	assert.Equal(t, "scafctl run solution scoped-app --catalog official --version '^1.0.0' --pre-release", scopedEntries[0].Command)
	assert.Equal(t, "scafctl run action release -f scoped-app --catalog official --version '^1.0.0' --pre-release", scopedEntries[1].Command)

	// Paths containing spaces, trailing whitespace, and URL query metacharacters
	// are shell-quoted consistently across default, when-gated, and explicit actions.
	mixedSol := &solution.Solution{
		Metadata: solution.Metadata{Name: "mixed-tasks"},
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"mode": paramResolver("mode", "dev"),
			},
			Workflow: &action.Workflow{
				Actions: map[string]*action.Action{
					"check":   {Description: "Default check"},
					"deploy":  {Description: "Gated deploy", When: whenCondition(`_.mode == "prod"`)},
					"release": {Description: "Explicit release", Explicit: true},
				},
			},
		},
	}
	for _, tc := range []struct {
		name        string
		resolved    string
		rawPath     string
		wantCheck   string
		wantDeploy  string
		wantRelease string
	}{
		{
			name:        "relative path with spaces",
			resolved:    "/workspace/task files/actions.yaml",
			rawPath:     "task files/actions.yaml",
			wantCheck:   "scafctl run solution -f './task files/actions.yaml'",
			wantDeploy:  "scafctl run solution -f './task files/actions.yaml' -r mode=prod",
			wantRelease: "scafctl run action release -f './task files/actions.yaml'",
		},
		{
			name:        "filename with trailing whitespace",
			resolved:    "/tmp/actions.yaml ",
			rawPath:     "/tmp/actions.yaml ",
			wantCheck:   "scafctl run solution -f '/tmp/actions.yaml '",
			wantDeploy:  "scafctl run solution -f '/tmp/actions.yaml ' -r mode=prod",
			wantRelease: "scafctl run action release -f '/tmp/actions.yaml '",
		},
		{
			name:        "URL with query metacharacters",
			resolved:    "https://example.com/actions.yaml?a=1&b=2",
			rawPath:     "https://example.com/actions.yaml?a=1&b=2",
			wantCheck:   "scafctl run solution 'https://example.com/actions.yaml?a=1&b=2'",
			wantDeploy:  "scafctl run solution 'https://example.com/actions.yaml?a=1&b=2' -r mode=prod",
			wantRelease: "scafctl run action release -f 'https://example.com/actions.yaml?a=1&b=2'",
		},
	} {
		s := *mixedSol
		s.SetPath(tc.resolved)
		entries := BuildActionList(ctx, &s, tc.rawPath, "scafctl")
		require.Len(t, entries, 3, tc.name)
		assert.Equal(t, tc.wantCheck, entries[0].Command, tc.name)
		assert.Equal(t, tc.wantDeploy, entries[1].Command, tc.name)
		assert.Equal(t, tc.wantRelease, entries[2].Command, tc.name)
	}
}

func TestBuildActionList(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("empty and non-workflow solutions return non-nil empty slice", func(t *testing.T) {
		t.Parallel()
		for _, sol := range []*solution.Solution{
			nil,
			{Metadata: solution.Metadata{Name: "empty"}},
			{
				Metadata: solution.Metadata{Name: "resolver-only"},
				Spec: solution.Spec{
					Resolvers: map[string]*resolver.Resolver{"x": paramResolver("x", "val")},
				},
			},
			{
				Metadata: solution.Metadata{Name: "finally-only"},
				Spec: solution.Spec{
					Workflow: &action.Workflow{
						Finally: map[string]*action.Action{"cleanup": {Description: "Clean"}},
					},
				},
			},
		} {
			entries := BuildActionList(ctx, sol, "solution.yaml", "scafctl")
			require.NotNil(t, entries)
			assert.Empty(t, entries)
			raw, err := json.Marshal(entries)
			require.NoError(t, err)
			assert.Equal(t, "[]", string(raw))
		}
	})

	t.Run("entries carry all keys without omitempty in JSON and YAML", func(t *testing.T) {
		t.Parallel()
		sol := &solution.Solution{
			Metadata: solution.Metadata{Name: "tasks"},
			Spec: solution.Spec{
				Workflow: &action.Workflow{
					Actions: map[string]*action.Action{
						"build": {
							Alias:       "b",
							Description: "Build project",
						},
						"release": {
							Explicit: true,
						},
					},
				},
			},
		}

		entries := BuildActionList(ctx, sol, "actions.yaml", "mycli")
		require.Len(t, entries, 2)

		assert.Equal(t, ActionListEntry{
			Name:        "build",
			Alias:       "b",
			Default:     true,
			Description: "Build project",
			Command:     "mycli run solution -f ./actions.yaml",
		}, entries[0])
		assert.Equal(t, ActionListEntry{
			Name:        "release",
			Alias:       "",
			Default:     false,
			Description: "",
			Command:     "mycli run action release -f ./actions.yaml",
		}, entries[1])

		rawJSON, err := json.Marshal(entries)
		require.NoError(t, err)
		var decodedJSON []map[string]any
		require.NoError(t, json.Unmarshal(rawJSON, &decodedJSON))
		for _, item := range decodedJSON {
			assert.Contains(t, item, "name")
			assert.Contains(t, item, "alias")
			assert.Contains(t, item, "default")
			assert.Contains(t, item, "description")
			assert.Contains(t, item, "command")
		}

		rawYAML, err := yaml.Marshal(entries)
		require.NoError(t, err)
		var decodedYAML []map[string]any
		require.NoError(t, yaml.Unmarshal(rawYAML, &decodedYAML))
		for _, item := range decodedYAML {
			assert.Contains(t, item, "name")
			assert.Contains(t, item, "alias")
			assert.Contains(t, item, "default")
			assert.Contains(t, item, "description")
			assert.Contains(t, item, "command")
		}
	})
}

func TestFormatAvailableActions(t *testing.T) {
	t.Parallel()

	assert.Empty(t, FormatAvailableActions(nil))

	lines := FormatAvailableActions([]ActionUsage{
		{Name: "build", Alias: "b", Description: "Compile\n\tthe \x1b[31mbinary\x1b[0m"},
		{Name: "check", Default: true, Description: "Run checks"},
		{Name: "clean"},
	})
	assert.Equal(t, []string{
		"  build (b)        Compile the binary",
		"  check (default)  Run checks",
		"  clean",
	}, lines)
}

func BenchmarkBuildActionList(b *testing.B) {
	sol := &solution.Solution{
		Metadata: solution.Metadata{Name: "tasks"},
		Spec: solution.Spec{
			Workflow: &action.Workflow{
				Actions: map[string]*action.Action{
					"build":   {Alias: "b", Description: "Build binary", Explicit: true},
					"lint":    {Alias: "l", Description: "Run linters", Explicit: true},
					"test":    {Alias: "t", Description: "Run tests", Explicit: true},
					"release": {Description: "Publish release", Explicit: true},
				},
			},
		},
	}
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = BuildActionList(ctx, sol, "actions.yaml", "scafctl")
	}
}
