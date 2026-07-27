// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package packagecmd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fetchBundleCatalog is a fake Catalog that only implements FetchWithBundle;
// verifyBuiltBundle calls no other method.
type fetchBundleCatalog struct {
	catalog.Catalog
	content    []byte
	bundleData []byte
	fetchErr   error
}

func (f *fetchBundleCatalog) FetchWithBundle(_ context.Context, ref catalog.Reference) ([]byte, []byte, catalog.ArtifactInfo, error) {
	if f.fetchErr != nil {
		return nil, nil, catalog.ArtifactInfo{}, f.fetchErr
	}
	return f.content, f.bundleData, catalog.ArtifactInfo{Reference: ref}, nil
}

const solutionReferencingLocalFile = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: my-solution
  version: 1.0.0
spec:
  resolvers:
    myFile:
      resolve:
        with:
          - provider: file
            inputs:
              path: "templates/main.tmpl"
              operation: read
`

func newTestWriter() *writer.Writer {
	ioStreams, _, _ := terminal.NewTestIOStreams()
	return writer.New(ioStreams, settings.NewCliParams())
}

func chdirWithLocalFile(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "templates"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "templates", "main.tmpl"), []byte("hi"), 0o644))
	t.Chdir(dir)
}

func TestVerifyBuiltBundle_FetchErrorAlwaysFatal(t *testing.T) {
	w := newTestWriter()
	ctx := writer.WithWriter(context.Background(), w)
	lgr := logger.FromContext(ctx)

	cat := &fetchBundleCatalog{fetchErr: errors.New("boom")}
	// Producer fetch errors are fatal even without --strict.
	opts := &SolutionOptions{Strict: false}
	_, err := verifyBuiltBundle(ctx, cat, catalog.Reference{Name: "s", Kind: catalog.ArtifactKindSolution}, opts, w, lgr)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fetching built bundle")
}

func TestVerifyBuiltBundle_ParseErrorFatal(t *testing.T) {
	w := newTestWriter()
	ctx := writer.WithWriter(context.Background(), w)
	lgr := logger.FromContext(ctx)

	cat := &fetchBundleCatalog{content: []byte("::not yaml::")}
	opts := &SolutionOptions{}
	_, err := verifyBuiltBundle(ctx, cat, catalog.Reference{Name: "s", Kind: catalog.ArtifactKindSolution}, opts, w, lgr)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing built solution")
}

func TestVerifyBuiltBundle_BundleLessIncompleteStrictFails(t *testing.T) {
	chdirWithLocalFile(t)
	w := newTestWriter()
	ctx := writer.WithWriter(context.Background(), w)
	lgr := logger.FromContext(ctx)

	// Empty bundleData with a solution referencing a local file -> no-bundle
	// warning, which is fatal under producer --strict.
	cat := &fetchBundleCatalog{content: []byte(solutionReferencingLocalFile), bundleData: nil}
	opts := &SolutionOptions{Strict: true}
	vr, err := verifyBuiltBundle(ctx, cat, catalog.Reference{Name: "s", Kind: catalog.ArtifactKindSolution}, opts, w, lgr)
	require.Error(t, err, "bundle-less incomplete artifact must fail under --strict")
	assert.Contains(t, err.Error(), "warning(s) (strict)")
	// The verify result is returned even on a policy-decision failure so callers
	// can surface it in a machine-readable report.
	require.NotNil(t, vr, "verify result must accompany a policy-decision failure")
	assert.NotEmpty(t, vr.Warnings, "failure result should carry the triggering warnings")
}

func TestVerifyBuiltBundle_BundleLessNonStrictPasses(t *testing.T) {
	chdirWithLocalFile(t)
	w := newTestWriter()
	ctx := writer.WithWriter(context.Background(), w)
	lgr := logger.FromContext(ctx)

	cat := &fetchBundleCatalog{content: []byte(solutionReferencingLocalFile), bundleData: nil}
	opts := &SolutionOptions{Strict: false}
	_, err := verifyBuiltBundle(ctx, cat, catalog.Reference{Name: "s", Kind: catalog.ArtifactKindSolution}, opts, w, lgr)
	assert.NoError(t, err, "producer warnings are non-fatal without --strict")
}
