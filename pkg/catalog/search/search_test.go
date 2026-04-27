// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package search

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/stretchr/testify/assert"
)

// mockSolution is a test helper for building solution objects with resolvers.
type mockSolution struct {
	resolvers map[string]*resolver.Resolver
}

func newMockSolution() *mockSolution {
	return &mockSolution{resolvers: make(map[string]*resolver.Resolver)}
}

func (m *mockSolution) addResolver(name, provider string) {
	m.resolvers[name] = &resolver.Resolver{
		Name: name,
		Resolve: &resolver.ResolvePhase{
			With: []resolver.ProviderSource{{Provider: provider}},
		},
	}
}

func (m *mockSolution) solution() *solution.Solution {
	sol := &solution.Solution{}
	sol.Spec.Resolvers = m.resolvers
	return sol
}

func TestScoreQuery(t *testing.T) {
	entry := SolutionEntry{
		Name:        "cloud-run-deploy",
		DisplayName: "Cloud Run Deployment",
		Description: "Deploy a service to Google Cloud Run with autoscaling",
		Category:    "deployment",
		Tags:        []string{"gcp", "serverless", "cloud-run"},
		Providers:   []string{"http", "gcp"},
	}

	tests := []struct {
		name     string
		query    string
		expected float64
	}{
		{name: "exact name match", query: "cloud-run-deploy", expected: scoreExactName},
		{name: "name contains", query: "cloud-run", expected: scoreNameContains},
		{name: "display name match", query: "cloud run deployment", expected: scoreDisplayName},
		{name: "exact tag match", query: "gcp", expected: scoreTagExact},
		{name: "category match", query: "deployment", expected: scoreDisplayName}, // also matches displayName
		{name: "description match", query: "autoscaling", expected: scoreDescription},
		{name: "partial tag match", query: "cloud", expected: scoreNameContains}, // also matches name
		{name: "provider match", query: "http", expected: scoreProvider},
		{name: "no match", query: "terraform", expected: 0},
		{name: "multi-word all present", query: "gcp autoscaling", expected: scoreMultiWord},
		{name: "multi-word partial miss", query: "gcp kubernetes", expected: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := scoreQuery(entry, tt.query)
			assert.InDelta(t, tt.expected, score, 0.001, "query=%q", tt.query)
		})
	}
}

func TestMatchEntry(t *testing.T) {
	entry := SolutionEntry{
		Name:        "cloud-run-deploy",
		Category:    "deployment",
		Tags:        []string{"gcp", "serverless"},
		Providers:   []string{"http"},
		Description: "Deploy to GCP",
	}

	tests := []struct {
		name     string
		opts     Options
		wantZero bool
	}{
		{name: "no filters returns default", opts: Options{}, wantZero: false},
		{name: "category match", opts: Options{Category: "deployment"}, wantZero: false},
		{name: "category mismatch", opts: Options{Category: "infrastructure"}, wantZero: true},
		{name: "provider match", opts: Options{Provider: "http"}, wantZero: false},
		{name: "provider mismatch", opts: Options{Provider: "azure"}, wantZero: true},
		{name: "tags match", opts: Options{Tags: []string{"gcp"}}, wantZero: false},
		{name: "tags mismatch", opts: Options{Tags: []string{"aws"}}, wantZero: true},
		{name: "tags AND logic", opts: Options{Tags: []string{"gcp", "serverless"}}, wantZero: false},
		{name: "tags AND partial miss", opts: Options{Tags: []string{"gcp", "aws"}}, wantZero: true},
		{name: "query only", opts: Options{Query: "cloud"}, wantZero: false},
		{name: "query no match", opts: Options{Query: "terraform"}, wantZero: true},
		{name: "combined filters", opts: Options{Query: "deploy", Category: "deployment", Tags: []string{"gcp"}}, wantZero: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := matchEntry(entry, tt.opts)
			if tt.wantZero {
				assert.Equal(t, float64(0), score)
			} else {
				assert.Greater(t, score, float64(0))
			}
		})
	}
}

func TestParseTags(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want []string
	}{
		{name: "single tag", raw: "gcp", want: []string{"gcp"}},
		{name: "multiple tags", raw: "gcp,aws,azure", want: []string{"gcp", "aws", "azure"}},
		{name: "with spaces", raw: " gcp , aws , azure ", want: []string{"gcp", "aws", "azure"}},
		{name: "empty string", raw: "", want: []string{}},
		{name: "only commas", raw: ",,", want: []string{}},
		{name: "trailing comma", raw: "gcp,", want: []string{"gcp"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseTags(tt.raw)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestExtractParameterNames(t *testing.T) {
	tests := []struct {
		name string
		sol  func() *mockSolution
		want []string
	}{
		{
			name: "no resolvers",
			sol:  func() *mockSolution { return newMockSolution() },
			want: nil,
		},
		{
			name: "parameter resolver",
			sol: func() *mockSolution {
				s := newMockSolution()
				s.addResolver("project_id", "parameter")
				s.addResolver("region", "parameter")
				s.addResolver("computed", "cel")
				return s
			},
			want: []string{"project_id", "region"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractParameterNames(tt.sol().solution())
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestExtractProviderNames(t *testing.T) {
	tests := []struct {
		name string
		sol  func() *mockSolution
		want []string
	}{
		{
			name: "excludes static and parameter",
			sol: func() *mockSolution {
				s := newMockSolution()
				s.addResolver("a", "parameter")
				s.addResolver("b", "static")
				s.addResolver("c", "http")
				s.addResolver("d", "gcp")
				return s
			},
			want: []string{"gcp", "http"},
		},
		{
			name: "deduplicates",
			sol: func() *mockSolution {
				s := newMockSolution()
				s.addResolver("a", "http")
				s.addResolver("b", "http")
				return s
			},
			want: []string{"http"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractProviderNames(tt.sol().solution())
			assert.Equal(t, tt.want, got)
		})
	}
}

func BenchmarkScoreQuery(b *testing.B) {
	b.ReportAllocs()

	entry := SolutionEntry{
		Name:        "cloud-run-deploy",
		DisplayName: "Cloud Run Deployment",
		Description: "Deploy a service to Google Cloud Run with autoscaling",
		Category:    "deployment",
		Tags:        []string{"gcp", "serverless", "cloud-run"},
		Providers:   []string{"http", "gcp"},
	}

	b.ResetTimer()
	for b.Loop() {
		scoreQuery(entry, "cloud run")
	}
}

func BenchmarkMatchEntry(b *testing.B) {
	b.ReportAllocs()

	entry := SolutionEntry{
		Name:        "cloud-run-deploy",
		DisplayName: "Cloud Run Deployment",
		Description: "Deploy a service to Google Cloud Run",
		Category:    "deployment",
		Tags:        []string{"gcp", "serverless"},
		Providers:   []string{"http"},
	}

	opts := Options{
		Query:    "cloud",
		Category: "deployment",
		Tags:     []string{"gcp"},
	}

	b.ResetTimer()
	for b.Loop() {
		matchEntry(entry, opts)
	}
}

func TestLatestByName(t *testing.T) {
	t.Parallel()

	v1 := semver.MustParse("1.0.0")
	v2 := semver.MustParse("2.0.0")

	tests := []struct {
		name  string
		items []catalog.ArtifactInfo
		want  map[string]*semver.Version // name → expected version (nil for unversioned)
	}{
		{
			name: "keeps highest version",
			items: []catalog.ArtifactInfo{
				{Reference: catalog.Reference{Name: "app", Version: v1}},
				{Reference: catalog.Reference{Name: "app", Version: v2}},
			},
			want: map[string]*semver.Version{"app": v2},
		},
		{
			name: "versioned beats unversioned",
			items: []catalog.ArtifactInfo{
				{Reference: catalog.Reference{Name: "app"}},
				{Reference: catalog.Reference{Name: "app", Version: v1}},
			},
			want: map[string]*semver.Version{"app": v1},
		},
		{
			name: "unversioned kept when no versioned exists",
			items: []catalog.ArtifactInfo{
				{Reference: catalog.Reference{Name: "app"}},
			},
			want: map[string]*semver.Version{"app": nil},
		},
		{
			name: "multiple names deduplicated independently",
			items: []catalog.ArtifactInfo{
				{Reference: catalog.Reference{Name: "a", Version: v1}},
				{Reference: catalog.Reference{Name: "b", Version: v2}},
				{Reference: catalog.Reference{Name: "a", Version: v2}},
			},
			want: map[string]*semver.Version{"a": v2, "b": v2},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := latestByName(tt.items)
			assert.Len(t, result, len(tt.want))
			for _, item := range result {
				expected, ok := tt.want[item.Reference.Name]
				assert.True(t, ok, "unexpected name %s", item.Reference.Name)
				assert.Equal(t, expected, item.Reference.Version)
			}
		})
	}
}

// fetchCountingCatalog is a mock catalog that counts Fetch calls and returns
// a minimal solution YAML with the given metadata.
type fetchCountingCatalog struct {
	fetchCount atomic.Int32
	solutions  map[string]string // name -> YAML content
}

func (c *fetchCountingCatalog) Name() string { return "test" }

func (c *fetchCountingCatalog) Fetch(_ context.Context, ref catalog.Reference) ([]byte, catalog.ArtifactInfo, error) {
	c.fetchCount.Add(1)
	yaml, ok := c.solutions[ref.Name]
	if !ok {
		return nil, catalog.ArtifactInfo{}, fmt.Errorf("not found: %s", ref.Name)
	}
	return []byte(yaml), catalog.ArtifactInfo{Reference: ref}, nil
}

func (c *fetchCountingCatalog) FetchWithBundle(context.Context, catalog.Reference) ([]byte, []byte, catalog.ArtifactInfo, error) {
	return nil, nil, catalog.ArtifactInfo{}, fmt.Errorf("not implemented")
}

func (c *fetchCountingCatalog) Store(context.Context, catalog.Reference, []byte, []byte, map[string]string, bool) (catalog.ArtifactInfo, error) {
	return catalog.ArtifactInfo{}, fmt.Errorf("not implemented")
}

func (c *fetchCountingCatalog) Resolve(context.Context, catalog.Reference) (catalog.ArtifactInfo, error) {
	return catalog.ArtifactInfo{}, fmt.Errorf("not implemented")
}

func (c *fetchCountingCatalog) List(context.Context, catalog.ArtifactKind, string) ([]catalog.ArtifactInfo, error) {
	return nil, fmt.Errorf("not implemented")
}

func (c *fetchCountingCatalog) Exists(context.Context, catalog.Reference) (bool, error) {
	return false, fmt.Errorf("not implemented")
}

func (c *fetchCountingCatalog) Delete(context.Context, catalog.Reference) error {
	return fmt.Errorf("not implemented")
}

func TestEnrichArtifacts_SkipsUnchanged(t *testing.T) {
	t.Parallel()

	solYAML := `apiVersion: scafctl/v1alpha
kind: Solution
metadata:
  name: my-app
  displayName: My Application
  description: A test app
  category: deployment
  tags: [go, cloud]
spec:
  resolvers: {}
`

	rc := &fetchCountingCatalog{
		solutions: map[string]string{"my-app": solYAML},
	}

	artifacts := []catalog.DiscoveredArtifact{
		{Kind: catalog.ArtifactKindSolution, Name: "my-app", LatestVersion: "1.0.0"},
	}

	existingIndex := []catalog.DiscoveredArtifact{
		{
			Kind:          catalog.ArtifactKindSolution,
			Name:          "my-app",
			LatestVersion: "1.0.0",
			DisplayName:   "Cached Display Name",
			Description:   "Cached description",
			Category:      "cached-category",
			Tags:          []string{"cached"},
		},
	}

	EnrichArtifacts(context.Background(), logr.Discard(), rc, artifacts, existingIndex)

	// Should NOT have fetched -- reused cached metadata.
	assert.Equal(t, int32(0), rc.fetchCount.Load())
	assert.Equal(t, "Cached Display Name", artifacts[0].DisplayName)
	assert.Equal(t, "Cached description", artifacts[0].Description)
	assert.Equal(t, "cached-category", artifacts[0].Category)
	assert.Equal(t, []string{"cached"}, artifacts[0].Tags)
}

func TestEnrichArtifacts_FetchesWhenVersionChanged(t *testing.T) {
	t.Parallel()

	solYAML := `apiVersion: scafctl/v1alpha
kind: Solution
metadata:
  name: my-app
  displayName: Fresh Name
  description: Fresh description
  category: fresh-category
  tags: [fresh]
spec:
  resolvers: {}
`

	rc := &fetchCountingCatalog{
		solutions: map[string]string{"my-app": solYAML},
	}

	artifacts := []catalog.DiscoveredArtifact{
		{Kind: catalog.ArtifactKindSolution, Name: "my-app", LatestVersion: "2.0.0"},
	}

	existingIndex := []catalog.DiscoveredArtifact{
		{
			Kind:          catalog.ArtifactKindSolution,
			Name:          "my-app",
			LatestVersion: "1.0.0",
			DisplayName:   "Old Name",
		},
	}

	EnrichArtifacts(context.Background(), logr.Discard(), rc, artifacts, existingIndex)

	// Should have fetched -- version changed.
	assert.Equal(t, int32(1), rc.fetchCount.Load())
	assert.Equal(t, "Fresh Name", artifacts[0].DisplayName)
	assert.Equal(t, "Fresh description", artifacts[0].Description)
	assert.Equal(t, "fresh-category", artifacts[0].Category)
}

func TestEnrichArtifacts_FetchesWhenNoExistingIndex(t *testing.T) {
	t.Parallel()

	solYAML := `apiVersion: scafctl/v1alpha
kind: Solution
metadata:
  name: new-app
  displayName: New App
spec:
  resolvers: {}
`

	rc := &fetchCountingCatalog{
		solutions: map[string]string{"new-app": solYAML},
	}

	artifacts := []catalog.DiscoveredArtifact{
		{Kind: catalog.ArtifactKindSolution, Name: "new-app", LatestVersion: "1.0.0"},
	}

	// nil existing index -- no cache available.
	EnrichArtifacts(context.Background(), logr.Discard(), rc, artifacts, nil)

	assert.Equal(t, int32(1), rc.fetchCount.Load())
	assert.Equal(t, "New App", artifacts[0].DisplayName)
}

func TestEnrichArtifacts_SkipsNonSolutionArtifacts(t *testing.T) {
	t.Parallel()

	rc := &fetchCountingCatalog{solutions: map[string]string{}}

	artifacts := []catalog.DiscoveredArtifact{
		{Kind: catalog.ArtifactKindProvider, Name: "my-provider", LatestVersion: "1.0.0"},
	}

	EnrichArtifacts(context.Background(), logr.Discard(), rc, artifacts, nil)

	assert.Equal(t, int32(0), rc.fetchCount.Load())
}

func TestBuildIndexMap(t *testing.T) {
	t.Parallel()

	index := []catalog.DiscoveredArtifact{
		{Kind: catalog.ArtifactKindSolution, Name: "sol-a", LatestVersion: "1.0.0"},
		{Kind: catalog.ArtifactKindProvider, Name: "prov-b", LatestVersion: "2.0.0"},
		{Kind: catalog.ArtifactKindSolution, Name: "sol-c", LatestVersion: "3.0.0"},
	}

	m := buildIndexMap(index)

	assert.Len(t, m, 2)
	assert.Contains(t, m, "sol-a")
	assert.Contains(t, m, "sol-c")
	assert.NotContains(t, m, "prov-b")
}

func TestCopyEnrichedMetadata(t *testing.T) {
	t.Parallel()

	src := catalog.DiscoveredArtifact{
		DisplayName: "Source Name",
		Description: "Source desc",
		Category:    "source-cat",
		Tags:        []string{"a", "b"},
		Maintainers: []string{"alice"},
		Links:       []catalog.DiscoveredLink{{Name: "docs", URL: "https://example.com"}},
		Providers:   []string{"http"},
		Parameters:  []string{"env"},
	}

	dst := &catalog.DiscoveredArtifact{Name: "target"}
	copyEnrichedMetadata(dst, src)

	assert.Equal(t, "Source Name", dst.DisplayName)
	assert.Equal(t, "Source desc", dst.Description)
	assert.Equal(t, "source-cat", dst.Category)
	assert.Equal(t, []string{"a", "b"}, dst.Tags)
	assert.Equal(t, []string{"alice"}, dst.Maintainers)
	assert.Equal(t, []catalog.DiscoveredLink{{Name: "docs", URL: "https://example.com"}}, dst.Links)
	assert.Equal(t, []string{"http"}, dst.Providers)
	assert.Equal(t, []string{"env"}, dst.Parameters)
}
