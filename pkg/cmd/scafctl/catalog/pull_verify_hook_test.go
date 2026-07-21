// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"errors"
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
// the verify hooks call no other method. Unimplemented methods panic so an
// accidental call is caught loudly in tests.
type fetchBundleCatalog struct {
	catalog.Catalog
	content    []byte
	bundleData []byte
	fetchErr   error
	gotRef     catalog.Reference
}

func (f *fetchBundleCatalog) FetchWithBundle(_ context.Context, ref catalog.Reference) ([]byte, []byte, catalog.ArtifactInfo, error) {
	f.gotRef = ref
	if f.fetchErr != nil {
		return nil, nil, catalog.ArtifactInfo{}, f.fetchErr
	}
	return f.content, f.bundleData, catalog.ArtifactInfo{Reference: ref}, nil
}

// solutionReferencingLocalFile is a minimal solution that references a local
// file, so a no-bundle verify surfaces a completeness warning.
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

func TestVerifyPulledBundle_FetchErrorStrictFailsClosed(t *testing.T) {
	w := newTestWriter()
	ctx := writer.WithWriter(context.Background(), w)
	lgr := logger.FromContext(ctx)

	cat := &fetchBundleCatalog{fetchErr: errors.New("boom")}
	opts := &PullOptions{Strict: true}

	err := verifyPulledBundle(ctx, cat, catalog.Reference{Name: "my-solution", Kind: catalog.ArtifactKindSolution}, opts, w, lgr)
	require.Error(t, err, "strict fetch error must fail closed")
	assert.Contains(t, err.Error(), "fetching pulled bundle")
}

func TestVerifyPulledBundle_FetchErrorNonStrictWarns(t *testing.T) {
	w := newTestWriter()
	ctx := writer.WithWriter(context.Background(), w)
	lgr := logger.FromContext(ctx)

	cat := &fetchBundleCatalog{fetchErr: errors.New("boom")}
	opts := &PullOptions{Strict: false}

	err := verifyPulledBundle(ctx, cat, catalog.Reference{Name: "my-solution", Kind: catalog.ArtifactKindSolution}, opts, w, lgr)
	assert.NoError(t, err, "non-strict fetch error must not break the pull")
}

func TestVerifyPulledBundle_BundleLessIncompleteStrictFails(t *testing.T) {
	w := newTestWriter()
	ctx := writer.WithWriter(context.Background(), w)
	lgr := logger.FromContext(ctx)

	// Empty bundleData with a solution that references a local file -> the
	// no-bundle case produces a warning (spec-based, so no file needs to exist
	// on disk and the result is independent of the CWD), which is fatal under
	// --strict.
	cat := &fetchBundleCatalog{content: []byte(solutionReferencingLocalFile), bundleData: nil}
	opts := &PullOptions{Strict: true}

	err := verifyPulledBundle(ctx, cat, catalog.Reference{Name: "my-solution", Kind: catalog.ArtifactKindSolution}, opts, w, lgr)
	require.Error(t, err, "bundle-less incomplete artifact must fail under --strict")
	assert.Contains(t, err.Error(), "warning(s) (strict)")
}

func TestVerifyPulledBundle_BundleLessNonStrictWarns(t *testing.T) {
	w := newTestWriter()
	ctx := writer.WithWriter(context.Background(), w)
	lgr := logger.FromContext(ctx)

	cat := &fetchBundleCatalog{content: []byte(solutionReferencingLocalFile), bundleData: nil}
	opts := &PullOptions{Strict: false}

	err := verifyPulledBundle(ctx, cat, catalog.Reference{Name: "my-solution", Kind: catalog.ArtifactKindSolution}, opts, w, lgr)
	assert.NoError(t, err, "non-strict bundle-less warning must not fail the pull")
}

func TestVerifyPulledBundle_VerifiesGivenReference(t *testing.T) {
	w := newTestWriter()
	ctx := writer.WithWriter(context.Background(), w)
	lgr := logger.FromContext(ctx)

	cat := &fetchBundleCatalog{content: []byte(solutionReferencingLocalFile), bundleData: nil}
	opts := &PullOptions{Strict: false}

	renamedRef := catalog.Reference{Name: "local-solution", Kind: catalog.ArtifactKindSolution}
	_ = verifyPulledBundle(ctx, cat, renamedRef, opts, w, lgr)
	assert.Equal(t, "local-solution", cat.gotRef.Name, "must verify against the passed (renamed) reference")
}

func TestVerifyPulledBundle_ParseErrorStrictFailsClosed(t *testing.T) {
	w := newTestWriter()
	ctx := writer.WithWriter(context.Background(), w)
	lgr := logger.FromContext(ctx)

	cat := &fetchBundleCatalog{content: []byte("::not yaml::")}
	opts := &PullOptions{Strict: true}

	err := verifyPulledBundle(ctx, cat, catalog.Reference{Name: "x", Kind: catalog.ArtifactKindSolution}, opts, w, lgr)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing pulled solution")
}
