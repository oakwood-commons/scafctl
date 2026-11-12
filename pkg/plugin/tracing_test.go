// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"context"
	"errors"
	"testing"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/plugin/identity"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// newRecordingTracerProvider returns a tracer provider whose ended spans are
// captured by the returned recorder.
func newRecordingTracerProvider(t *testing.T) (*sdktrace.TracerProvider, *tracetest.SpanRecorder) {
	t.Helper()
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	return tp, sr
}

// spansNamed returns the ended spans with the given name.
func spansNamed(sr *tracetest.SpanRecorder, name string) []sdktrace.ReadOnlySpan {
	var out []sdktrace.ReadOnlySpan
	for _, s := range sr.Ended() {
		if s.Name() == name {
			out = append(out, s)
		}
	}
	return out
}

// onlySpan returns the single ended span with the given name.
func onlySpan(t *testing.T, sr *tracetest.SpanRecorder, name string) sdktrace.ReadOnlySpan {
	t.Helper()
	spans := spansNamed(sr, name)
	require.Len(t, spans, 1, "expected exactly one %s span", name)
	return spans[0]
}

// spanAttr returns the value of key on span.
func spanAttr(t *testing.T, span sdktrace.ReadOnlySpan, key attribute.Key) attribute.Value {
	t.Helper()
	for _, kv := range span.Attributes() {
		if kv.Key == key {
			return kv.Value
		}
	}
	require.Failf(t, "missing attribute", "span %s has no %s attribute", span.Name(), key)
	return attribute.Value{}
}

func eventNames(span sdktrace.ReadOnlySpan) []string {
	names := make([]string, 0, len(span.Events()))
	for _, e := range span.Events() {
		names = append(names, e.Name)
	}
	return names
}

// newTracedPool builds a VersionPool whose spawns use in-memory fakes and whose
// spans are recorded.
func newTracedPool(t *testing.T, ff pluginFetcher, client pluginClient, clientErr error) (*VersionPool, *tracetest.SpanRecorder) {
	t.Helper()
	tp, sr := newRecordingTracerProvider(t)
	vp := NewVersionPool(context.Background(), ff, &fakeProviderRegistry{}, WithVersionPoolTracerProvider(tp))
	t.Cleanup(vp.Shutdown)
	vp.opts.newClient = func(string, ...ClientOption) (pluginClient, error) { return client, clientErr }
	vp.opts.newWrapper = func(_ pluginClient, name string, _ ...WrapperOption) (registrableProvider, error) {
		return &fakeRegistrableProvider{name: name}, nil
	}
	return vp, sr
}

func TestVersionPool_Tracing_ColdPath(t *testing.T) {
	t.Parallel()

	ff := &fakePluginFetcher{results: []FetchResult{{Version: "1.2.3", Path: "/fake/plugin"}}}
	vp, sr := newTracedPool(t, ff, &fakePluginClient{providers: []string{"p1", "p2"}}, nil)

	release, err := vp.EnsureAndAcquire(context.Background(), []solution.PluginDependency{
		providerDep("plugin", "1.2.3"),
		{Name: "not-a-provider", Kind: solution.PluginKindAuthHandler},
	})
	require.NoError(t, err)
	release()

	ensure := onlySpan(t, sr, spanEnsure)
	acquire := onlySpan(t, sr, spanAcquire)
	spawn := onlySpan(t, sr, spanSpawn)

	// Hierarchy: ensure > acquire > spawn, all in one trace even though the
	// spawn runs on the pool's context in a separate goroutine.
	assert.Equal(t, ensure.SpanContext().SpanID(), acquire.Parent().SpanID())
	assert.Equal(t, acquire.SpanContext().SpanID(), spawn.Parent().SpanID())
	assert.Equal(t, ensure.SpanContext().TraceID(), spawn.SpanContext().TraceID())

	assert.Equal(t, int64(1), spanAttr(t, ensure, attrDependencyCount).AsInt64(), "only provider deps count")
	assert.Equal(t, outcomeSpawned, spanAttr(t, acquire, attrOutcome).AsString())
	assert.Equal(t, "1.2.3", spanAttr(t, acquire, attrResolvedVersion).AsString())
	assert.Equal(t, "plugin", spanAttr(t, acquire, attrName).AsString())
	assert.Equal(t, "official", spanAttr(t, acquire, attrCatalog).AsString())

	assert.Equal(t, int64(2), spanAttr(t, spawn, attrProvidersRegistered).AsInt64())
	assert.Equal(t, []string{eventProcessStarted, eventProvidersListed}, eventNames(spawn))
	assert.Equal(t, codes.Unset, spawn.Status().Code)
}

func TestVersionPool_Tracing_WarmPath(t *testing.T) {
	t.Parallel()

	ff := &fakePluginFetcher{}
	vp, sr := newTracedPool(t, ff, &fakePluginClient{}, nil)
	dep := providerDep("plugin", "1.2.3")
	seedEntry(vp, identity.DependencyToPluginIdentity(dep), "1.2.3", dep)

	release, err := vp.EnsureAndAcquire(context.Background(), []solution.PluginDependency{dep})
	require.NoError(t, err)
	release()

	ensure := onlySpan(t, sr, spanEnsure)
	acquire := onlySpan(t, sr, spanAcquire)
	assert.Empty(t, spansNamed(sr, spanSpawn), "a pool hit must not spawn")
	assert.Equal(t, ensure.SpanContext().SpanID(), acquire.Parent().SpanID())
	assert.Equal(t, outcomePoolHit, spanAttr(t, acquire, attrOutcome).AsString())
	assert.Equal(t, int64(1), spanAttr(t, ensure, attrPoolIdle).AsInt64())
	assert.Equal(t, int32(0), ff.callCount())
}

func TestExistingEntryOutcome(t *testing.T) {
	t.Parallel()

	starting := &poolEntry{ready: make(chan struct{})}
	assert.Equal(t, outcomeWaited, existingEntryOutcome(starting))

	ready := make(chan struct{})
	close(ready)
	assert.Equal(t, outcomePoolHit, existingEntryOutcome(&poolEntry{ready: ready}))
}

func TestVersionPool_Tracing_ErrorStage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		version   string
		fetcher   *fakePluginFetcher
		client    pluginClient
		clientErr error
		want      Stage
	}{
		{
			name:      "process start failure",
			version:   "1.2.3",
			fetcher:   &fakePluginFetcher{results: []FetchResult{{Version: "1.2.3"}}},
			clientErr: errors.New("exec failed"),
			want:      StageSpawn,
		},
		{
			name:    "provider listing failure",
			version: "1.2.3",
			fetcher: &fakePluginFetcher{results: []FetchResult{{Version: "1.2.3"}}},
			client:  &fakePluginClient{provErr: errors.New("handshake failed")},
			want:    StageHandshake,
		},
		{
			name:    "fetch failure inside the spawn goroutine",
			version: "1.2.3",
			fetcher: &fakePluginFetcher{err: withStage(StageDownload, errors.New("registry down"))},
			want:    StageDownload,
		},
		{
			name:    "fetch failure on the constraint path",
			version: "^1.0.0",
			fetcher: &fakePluginFetcher{err: withStage(StageDigest, errors.New("mismatch"))},
			want:    StageDigest,
		},
		{
			name:    "latest resolution failure",
			version: "latest",
			fetcher: &fakePluginFetcher{resolveErr: errors.New("catalog down")},
			want:    StageResolve,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			vp, sr := newTracedPool(t, tt.fetcher, tt.client, tt.clientErr)

			_, err := vp.EnsureAndAcquire(context.Background(), []solution.PluginDependency{providerDep("plugin", tt.version)})
			require.Error(t, err)
			stage, ok := ErrorStage(err)
			require.True(t, ok, "the returned error must carry its stage")
			assert.Equal(t, tt.want, stage)

			for _, name := range []string{spanEnsure, spanAcquire} {
				span := onlySpan(t, sr, name)
				assert.Equal(t, codes.Error, span.Status().Code, name)
				assert.Equal(t, string(tt.want), spanAttr(t, span, attrErrorStage).AsString(), name)
			}
		})
	}
}

func TestFetcher_Tracing(t *testing.T) {
	t.Parallel()

	binary := []byte("the-binary")
	dep := solution.PluginDependency{Name: "my-plugin", Kind: solution.PluginKindProvider, Version: "1.0.0"}
	lockWith := func(l bundler.LockPlugin) []bundler.LockPlugin {
		l.Name, l.Kind, l.ResolvedFrom = "my-plugin", "provider", "test"
		return []bundler.LockPlugin{l}
	}
	newFetcher := func(t *testing.T, platform string) (*Fetcher, *tracetest.SpanRecorder) {
		t.Helper()
		cat := newMockCatalog()
		cat.addArtifact(testRef("my-plugin", "1.0.0"), binary)
		tp, sr := newRecordingTracerProvider(t)
		return NewFetcher(FetcherConfig{
			CatalogFetcher: cat,
			Cache:          NewCache(t.TempDir()),
			Platform:       platform,
			Logger:         logr.Discard(),
			TracerProvider: tp,
		}), sr
	}

	t.Run("successful registry fetch", func(t *testing.T) {
		t.Parallel()
		f, sr := newFetcher(t, "linux/amd64")

		_, err := f.FetchPlugins(context.Background(), []solution.PluginDependency{dep},
			lockWith(bundler.LockPlugin{Version: "1.0.0", Digest: binaryDigest(binary)}))
		require.NoError(t, err)

		span := onlySpan(t, sr, spanFetch)
		assert.Equal(t, "my-plugin", spanAttr(t, span, attrName).AsString())
		assert.Equal(t, "1.0.0", spanAttr(t, span, attrVersion).AsString())
		assert.Equal(t, fetchSourceRegistry, spanAttr(t, span, attrSource).AsString())
		assert.True(t, spanAttr(t, span, attrFromLock).AsBool())
		assert.Equal(t, codes.Unset, span.Status().Code)
	})

	t.Run("cached fetch reports the cache source", func(t *testing.T) {
		t.Parallel()
		f, sr := newFetcher(t, "linux/amd64")
		lock := lockWith(bundler.LockPlugin{Version: "1.0.0", Digest: binaryDigest(binary)})

		// The first fetch populates the cache; the second is served from it.
		for range 2 {
			results, err := f.FetchPlugins(context.Background(), []solution.PluginDependency{dep}, lock)
			require.NoError(t, err)
			releaseResults(results)
		}

		spans := spansNamed(sr, spanFetch)
		require.Len(t, spans, 2)
		assert.Equal(t, fetchSourceCache, spanAttr(t, spans[1], attrSource).AsString())
	})

	failures := []struct {
		name     string
		platform string
		lock     bundler.LockPlugin
		want     Stage
		// wantVersion and wantCatalog, when set, are the resolved artifact
		// attributes expected on the failed span.
		wantVersion string
		wantCatalog string
	}{
		{
			name:        "digest mismatch",
			platform:    "linux/amd64",
			lock:        bundler.LockPlugin{Version: "1.0.0", Digest: "sha256:tampered"},
			want:        StageDigest,
			wantVersion: "1.0.0",
			wantCatalog: "test",
		},
		{
			name:     "stale lock",
			platform: "linux/amd64",
			lock:     bundler.LockPlugin{Version: "2.0.0", Constraint: "^2.0.0", Digest: binaryDigest(binary)},
			want:     StageLock,
		},
		{
			name:     "lock lacks the runtime platform",
			platform: "windows/amd64",
			lock: bundler.LockPlugin{
				Version: "1.0.0", Digest: binaryDigest(binary),
				Digests: map[string]string{"linux/amd64": binaryDigest(binary)},
			},
			want: StageResolve,
		},
	}
	for _, tt := range failures {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f, sr := newFetcher(t, tt.platform)

			_, err := f.FetchPlugins(context.Background(), []solution.PluginDependency{dep}, lockWith(tt.lock))
			require.Error(t, err)
			stage, ok := ErrorStage(err)
			require.True(t, ok)
			assert.Equal(t, tt.want, stage)

			span := onlySpan(t, sr, spanFetch)
			assert.Equal(t, codes.Error, span.Status().Code)
			assert.Equal(t, string(tt.want), spanAttr(t, span, attrErrorStage).AsString())
			if tt.wantVersion != "" {
				assert.Equal(t, tt.wantVersion, spanAttr(t, span, attrVersion).AsString())
			}
			if tt.wantCatalog != "" {
				assert.Equal(t, tt.wantCatalog, spanAttr(t, span, attrCatalog).AsString())
			}
		})
	}
}

func TestFetcher_Tracing_BindFailure(t *testing.T) {
	t.Parallel()

	tp, sr := newRecordingTracerProvider(t)
	f := NewFetcher(FetcherConfig{
		CatalogFetcher: newMockCatalog(),
		Cache:          NewCache(t.TempDir()),
		Platform:       "linux/amd64",
		Logger:         logr.Discard(),
		TracerProvider: tp,
	})
	dep := solution.PluginDependency{
		Name: "my-plugin", Kind: solution.PluginKindProvider, Version: "1.0.0",
		Source: &solution.PluginSource{Registry: "ghcr.io/unconfigured"},
	}

	_, err := f.FetchPlugin(context.Background(), dep, nil)
	require.ErrorIs(t, err, ErrUnconfiguredRegistry)
	stage, ok := ErrorStage(err)
	require.True(t, ok)
	assert.Equal(t, StageBind, stage)
	assert.Equal(t, string(StageBind), spanAttr(t, onlySpan(t, sr, spanFetch), attrErrorStage).AsString())
}

// TestPluginTracer_NilUsesGlobal guards the zero-value Fetcher path used by
// callers that construct the struct directly.
func TestPluginTracer_NilUsesGlobal(t *testing.T) {
	t.Parallel()
	assert.NotNil(t, pluginTracer(nil))
}
