// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package builder

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
)

// stageInMemory assembles the artifact and holds it in memory until it is
// verified.
func (p *Packager) stageInMemory(ctx context.Context, st *pipelineState) (bool, error) {
	annotations := solutionAnnotations(p.src.Solution, p.provenance())
	staged, err := stageArtifact(ctx, solutionArtifact(p.ref, st.content, st.result.Build, annotations))
	if err != nil {
		return false, err
	}
	st.staged = staged
	return false, nil
}

// provenance is the source annotation recorded when metadata.source is
// empty: the solution file name, or StdinPath for stdin. The file name
// rather than its path keeps local directory layout out of the artifact.
func (p *Packager) provenance() string {
	if p.src.IsStdin() {
		return StdinPath
	}
	return filepath.Base(p.src.Path())
}

// verifyStaged reads the staged artifact back as the catalog would serve it
// and checks the bundle is complete, so an incomplete artifact is never
// stored.
func (p *Packager) verifyStaged(ctx context.Context, st *pipelineState) (bool, error) {
	vr, err := verifyArtifact(ctx, st.staged)
	if err != nil {
		return false, err
	}
	st.result.Verify = vr
	return false, p.verify.check(vr)
}

// verifyArtifact verifies the bundle of a staged artifact against its
// solution document.
func verifyArtifact(ctx context.Context, staged *stagedArtifact) (*bundler.VerifyResult, error) {
	doc, bundle, err := staged.read(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading built artifact for verification: %w", err)
	}
	var sol solution.Solution
	if err := sol.LoadFromBytes(doc); err != nil {
		return nil, fmt.Errorf("parsing built solution for verification: %w", err)
	}
	vr, err := bundler.VerifyBundle(ctx, &sol, bundle, *logger.FromContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("verifying built bundle: %w", err)
	}
	return vr, nil
}

// verifyPolicyError is a bundle that failed the completeness policy. It
// matches ErrVerifyFailed.
type verifyPolicyError struct {
	msg string
}

func (e *verifyPolicyError) Error() string { return e.msg }

func (e *verifyPolicyError) Is(target error) bool { return target == ErrVerifyFailed }

// check applies the completeness policy: errors always fail, warnings fail
// under Strict.
func (o VerifyOptions) check(vr *bundler.VerifyResult) error {
	if !vr.Passed() {
		return &verifyPolicyError{msg: fmt.Sprintf("built bundle is incomplete: %d error(s)", len(vr.Errors))}
	}
	if o.Strict && len(vr.Warnings) > 0 {
		return &verifyPolicyError{msg: fmt.Sprintf("built bundle has %d warning(s) (strict)", len(vr.Warnings))}
	}
	return nil
}

// publish stores the staged artifact in the catalog.
func (p *Packager) publish(ctx context.Context, st *pipelineState) (bool, error) {
	info, err := publishArtifact(ctx, p.catalog, st.staged, len(st.content), p.force)
	if err != nil {
		return false, fmt.Errorf("%w: %w", ErrStoreFailed, err)
	}
	// TODO: no-op; remove with the artifact cache (see cache.ArtifactCache).
	invalidateArtifactCache(ctx, info.Reference)
	st.result.Stored = &StoreResult{Info: info}
	return false, nil
}
