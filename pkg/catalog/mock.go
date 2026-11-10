// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"fmt"
	"strings"

	"github.com/Masterminds/semver/v3"
)

// mockCatalog implements Catalog for testing with pluggable return functions.
type mockCatalog struct {
	name                string
	artifacts           map[string]mockArtifact
	storeFunc           func(ctx context.Context, ref Reference, content, bundleData []byte, annotations map[string]string, force bool, extraLayers ...Layer) (ArtifactInfo, error)
	fetchFunc           func(ctx context.Context, ref Reference) ([]byte, ArtifactInfo, error)
	fetchWithBundleFunc func(ctx context.Context, ref Reference) ([]byte, []byte, ArtifactInfo, error)
	fetchWithLayerFunc  func(ctx context.Context, ref Reference, mediaTypes ...string) ([]byte, map[string][]byte, ArtifactInfo, error)
	resolveFunc         func(ctx context.Context, ref Reference) (ArtifactInfo, error)
	listFunc            func(ctx context.Context, kind ArtifactKind, name string) ([]ArtifactInfo, error)
	existsFunc          func(ctx context.Context, ref Reference) (bool, error)
	deleteFunc          func(ctx context.Context, ref Reference) error
}

type mockArtifact struct {
	content    []byte
	bundleData []byte
	info       ArtifactInfo
}

// MockOption configures a mock catalog.
type MockOption func(*mockCatalog)

// NewMockCatalog creates a Catalog for testing with the given name and options.
func NewMockCatalog(name string, opts ...MockOption) Catalog {
	m := &mockCatalog{
		name:      name,
		artifacts: make(map[string]mockArtifact),
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// WithResolveFunc sets the Resolve implementation.
func WithResolveFunc(fn func(ctx context.Context, ref Reference) (ArtifactInfo, error)) MockOption {
	return func(m *mockCatalog) { m.resolveFunc = fn }
}

// WithFetchFunc sets the Fetch implementation.
func WithFetchFunc(fn func(ctx context.Context, ref Reference) ([]byte, ArtifactInfo, error)) MockOption {
	return func(m *mockCatalog) { m.fetchFunc = fn }
}

// WithFetchBundleFunc sets the FetchWithBundle implementation.
func WithFetchBundleFunc(fn func(ctx context.Context, ref Reference) ([]byte, []byte, ArtifactInfo, error)) MockOption {
	return func(m *mockCatalog) { m.fetchWithBundleFunc = fn }
}

// WithFetchLayerFunc sets the FetchWithLayer implementation.
func WithFetchLayerFunc(fn func(ctx context.Context, ref Reference, mediaTypes ...string) ([]byte, map[string][]byte, ArtifactInfo, error)) MockOption {
	return func(m *mockCatalog) { m.fetchWithLayerFunc = fn }
}

// WithStoreFunc sets the Store implementation.
func WithStoreFunc(fn func(ctx context.Context, ref Reference, content, bundleData []byte, annotations map[string]string, force bool, extraLayers ...Layer) (ArtifactInfo, error)) MockOption {
	return func(m *mockCatalog) { m.storeFunc = fn }
}

// WithListFunc sets the List implementation.
func WithListFunc(fn func(ctx context.Context, kind ArtifactKind, name string) ([]ArtifactInfo, error)) MockOption {
	return func(m *mockCatalog) { m.listFunc = fn }
}

// WithExistsFunc sets the Exists implementation.
func WithExistsFunc(fn func(ctx context.Context, ref Reference) (bool, error)) MockOption {
	return func(m *mockCatalog) { m.existsFunc = fn }
}

// WithDeleteFunc sets the Delete implementation.
func WithDeleteFunc(fn func(ctx context.Context, ref Reference) error) MockOption {
	return func(m *mockCatalog) { m.deleteFunc = fn }
}

func newMockCatalog(name string) *mockCatalog {
	return &mockCatalog{
		name:      name,
		artifacts: make(map[string]mockArtifact),
	}
}

func (m *mockCatalog) addArtifact(ref Reference, content []byte) {
	m.artifacts[ref.String()] = mockArtifact{
		content: content,
		info: ArtifactInfo{
			Reference: ref,
			Digest:    fmt.Sprintf("sha256:mock-%s", ref.String()),
			Catalog:   m.name,
		},
	}
}

func (m *mockCatalog) Name() string { return m.name }

func (m *mockCatalog) Store(ctx context.Context, ref Reference, content, bundleData []byte, annotations map[string]string, force bool, extraLayers ...Layer) (ArtifactInfo, error) {
	if m.storeFunc != nil {
		return m.storeFunc(ctx, ref, content, bundleData, annotations, force, extraLayers...)
	}
	info := ArtifactInfo{Reference: ref, Catalog: m.name}
	m.artifacts[ref.String()] = mockArtifact{content: content, bundleData: bundleData, info: info}
	return info, nil
}

func (m *mockCatalog) Fetch(ctx context.Context, ref Reference) ([]byte, ArtifactInfo, error) {
	if m.fetchFunc != nil {
		return m.fetchFunc(ctx, ref)
	}
	a, ok := m.artifacts[ref.String()]
	if !ok {
		return nil, ArtifactInfo{}, ErrArtifactNotFound
	}
	return a.content, a.info, nil
}

func (m *mockCatalog) FetchWithBundle(ctx context.Context, ref Reference) ([]byte, []byte, ArtifactInfo, error) {
	if m.fetchWithBundleFunc != nil {
		return m.fetchWithBundleFunc(ctx, ref)
	}
	a, ok := m.artifacts[ref.String()]
	if !ok {
		return nil, nil, ArtifactInfo{}, ErrArtifactNotFound
	}
	return a.content, a.bundleData, a.info, nil
}

func (m *mockCatalog) FetchWithLayer(ctx context.Context, ref Reference, mediaTypes ...string) ([]byte, map[string][]byte, ArtifactInfo, error) {
	if m.fetchWithLayerFunc != nil {
		return m.fetchWithLayerFunc(ctx, ref, mediaTypes...)
	}
	a, ok := m.artifacts[ref.String()]
	if !ok {
		return nil, nil, ArtifactInfo{}, ErrArtifactNotFound
	}
	var layers map[string][]byte
	for _, mt := range mediaTypes {
		if mt == MediaTypeSolutionBundle && len(a.bundleData) > 0 {
			if layers == nil {
				layers = make(map[string][]byte, 1)
			}
			layers[MediaTypeSolutionBundle] = a.bundleData
		}
	}
	return a.content, layers, a.info, nil
}

func (m *mockCatalog) Resolve(ctx context.Context, ref Reference) (ArtifactInfo, error) {
	if m.resolveFunc != nil {
		return m.resolveFunc(ctx, ref)
	}
	a, ok := m.artifacts[ref.String()]
	if !ok {
		return ArtifactInfo{}, ErrArtifactNotFound
	}
	return a.info, nil
}

func (m *mockCatalog) List(ctx context.Context, kind ArtifactKind, name string) ([]ArtifactInfo, error) {
	if m.listFunc != nil {
		return m.listFunc(ctx, kind, name)
	}
	var results []ArtifactInfo
	for _, a := range m.artifacts {
		if name != "" && a.info.Reference.Name != name {
			continue
		}
		if a.info.Reference.Kind != kind {
			continue
		}
		results = append(results, a.info)
	}
	return results, nil
}

func (m *mockCatalog) Exists(ctx context.Context, ref Reference) (bool, error) {
	if m.existsFunc != nil {
		return m.existsFunc(ctx, ref)
	}
	_, ok := m.artifacts[ref.String()]
	return ok, nil
}

func (m *mockCatalog) Delete(ctx context.Context, ref Reference) error {
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, ref)
	}
	delete(m.artifacts, ref.String())
	return nil
}

// NewMockPluginSource creates a PluginSource for testing with the given name
// and options, suitable for NewPluginFetcher. It is also a Catalog.
func NewMockPluginSource(name string, opts ...MockOption) PluginSource {
	m, _ := NewMockCatalog(name, opts...).(*mockCatalog)
	return m
}

// FetchByPlatform implements PluginSource by delegating to Fetch; the mock
// stores a single binary per reference.
func (m *mockCatalog) FetchByPlatform(ctx context.Context, ref Reference, _ string) ([]byte, ArtifactInfo, error) {
	return m.Fetch(ctx, ref)
}

// ResolveContentDigest implements PluginSource, reporting the manifest digest
// from Resolve as the content digest.
func (m *mockCatalog) ResolveContentDigest(ctx context.Context, ref Reference, _, _ string) (ContentDigestInfo, error) {
	info, err := m.Resolve(ctx, ref)
	if err != nil {
		return ContentDigestInfo{}, err
	}
	return ContentDigestInfo{ArtifactInfo: info, ContentDigest: info.Digest}, nil
}

// Find implements PluginSource. With a resolve func configured it delegates
// to Resolve (pinning the version when query.Version is an exact semver);
// otherwise it returns the newest stored artifact matching query.Version
// ("" / "latest", an exact version, or a semver constraint).
func (m *mockCatalog) Find(ctx context.Context, query ArtifactQuery) (ArtifactInfo, error) {
	ref := Reference{Kind: query.Kind, Name: query.Name}
	if m.resolveFunc != nil {
		if v, err := semver.StrictNewVersion(query.Version); err == nil {
			ref.Version = v
		}
		return m.resolveFunc(ctx, ref)
	}
	infos, err := m.List(ctx, query.Kind, query.Name)
	if err != nil {
		return ArtifactInfo{}, err
	}
	if query.Version != "" && !strings.EqualFold(query.Version, "latest") {
		if infos, err = FilterByVersionConstraint(infos, query.Version); err != nil {
			return ArtifactInfo{}, fmt.Errorf("%w: %w", ErrInvalidQuery, err)
		}
	}
	if len(infos) == 0 {
		return ArtifactInfo{}, &ArtifactNotFoundError{Reference: ref, Catalog: m.name}
	}
	sortByVersionDesc(infos)
	return infos[0], nil
}
