// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/adrg/xdg"
	"github.com/go-logr/logr"
	catalogpkg "github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/require"
)

// newCatalogTestCtx creates a context with writer for catalog command tests.
func newCatalogTestCtx(tb testing.TB) context.Context {
	tb.Helper()
	var buf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &buf, &buf, false)
	w := writer.New(ioStreams, settings.NewCliParams())
	return writer.WithWriter(context.Background(), w)
}

// writerFromCtx retrieves the Writer from a test context.
func writerFromCtx(ctx context.Context) *writer.Writer {
	return writer.FromContext(ctx)
}

// newBufferedCatalogTestCtx is newCatalogTestCtx, also returning the buffer
// that receives all writer output.
func newBufferedCatalogTestCtx(tb testing.TB) (context.Context, *bytes.Buffer) {
	tb.Helper()
	buf := &bytes.Buffer{}
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)
	w := writer.New(ioStreams, settings.NewCliParams())
	return writer.WithWriter(context.Background(), w), buf
}

// testPulledOrigin is the source canonical used for pulled copies in tests.
const testPulledOrigin = "ghcr.io/myorg"

// useTempDataHome points XDG_DATA_HOME (and the cached xdg paths the default
// local catalog is opened from) at a fresh temp dir for the test, so commands
// under test never read or modify the developer's real local catalog. It
// cannot be used with t.Parallel (it sets env vars).
func useTempDataHome(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	// Cleanups run last-registered-first: registering the reload before
	// Setenv makes it run after the env var is restored, so xdg does not keep
	// pointing at the removed temp dir for later tests.
	t.Cleanup(xdg.Reload)
	t.Setenv("XDG_DATA_HOME", tmpDir)
	xdg.Reload()
	return tmpDir
}

// newSeededLocalCatalog points the default local catalog at a temp dir and
// stores a built and a pulled (testPulledOrigin) copy of solution app@1.0.0.
// It cannot be used with t.Parallel (it sets env vars).
func newSeededLocalCatalog(t *testing.T) *catalogpkg.LocalCatalog {
	t.Helper()
	tmpDir := useTempDataHome(t)

	cat, err := catalogpkg.NewLocalCatalogAt(filepath.Join(tmpDir, settings.CliBinaryName, "catalog"), logr.Discard())
	require.NoError(t, err)

	ref := catalogpkg.Reference{Kind: catalogpkg.ArtifactKindSolution, Name: "app", Version: semver.MustParse("1.0.0")}
	_, err = cat.Store(context.Background(), ref, []byte("built"), nil, nil, false)
	require.NoError(t, err)

	pulled := ref
	pulled.Origin = testPulledOrigin
	_, err = cat.Store(context.Background(), pulled, []byte("pulled"), nil,
		map[string]string{catalogpkg.AnnotationSourceCanonical: testPulledOrigin}, false)
	require.NoError(t, err)
	return cat
}

// reopenLocalCatalog opens a fresh handle on the default local catalog, so the
// in-memory index reflects writes made by commands under test.
func reopenLocalCatalog(t *testing.T) *catalogpkg.LocalCatalog {
	t.Helper()
	cat, err := catalogpkg.NewLocalCatalog(logr.Discard())
	require.NoError(t, err)
	return cat
}
