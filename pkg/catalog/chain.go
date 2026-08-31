// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
)

type ChainMode int

const (
	// ChainModeStrict stops at the first indeterminate result: a success is
	// only returned when every higher-priority catalog DEFINITIVELY reported
	// not-found. Any unreachable/auth/malformed error halts the chain. This
	// guarantees deterministic provenance (the earlier dependency-confusion fix).
	ChainModeStrict ChainMode = iota

	// ChainModeBestEffort continues past every error and returns the first
	// success (today's behavior). Maximizes availability, sacrifices
	// deterministic source selection.
	ChainModeBestEffort
)

// ChainCatalog tries each catalog in order, returning the first successful
// result. It implements the Catalog interface for read operations (Fetch,
// Resolve, List, Exists). Write operations (Store, Delete) are forwarded
// to the first catalog in the chain.
//
// It also implements PlatformAwareCatalog by delegating to underlying catalogs
// that support platform-aware operations (e.g., LocalCatalog with OCI image
// indexes).
type ChainCatalog struct {
	catalogs []Catalog
	logger   logr.Logger
	mode     ChainMode
}

// Compile-time interface assertions.
var (
	_ Catalog              = (*ChainCatalog)(nil)
	_ PlatformAwareCatalog = (*ChainCatalog)(nil)
)

// NewChainCatalog creates a ChainCatalog that tries catalogs in order.
// At least one catalog must be provided.
func NewChainCatalog(logger logr.Logger, catalogs ...Catalog) (*ChainCatalog, error) {
	if len(catalogs) == 0 {
		return nil, fmt.Errorf("at least one catalog is required")
	}
	return &ChainCatalog{
		catalogs: catalogs,
		logger:   logger.WithName("chain-catalog"),
		mode:     ChainModeBestEffort,
	}, nil
}

func NewChainCatalogWithMode(logger logr.Logger, mode ChainMode, catalogs ...Catalog) (*ChainCatalog, error) {
	if len(catalogs) == 0 {
		return nil, fmt.Errorf("at least one catalog is required")
	}
	return &ChainCatalog{
		catalogs: catalogs,
		logger:   logger.WithName("chain-catalog"),
		mode:     mode,
	}, nil
}

// Name returns a composite name.
func (c *ChainCatalog) Name() string {
	return "chain"
}

// Catalogs returns the underlying catalogs.
func (c *ChainCatalog) Catalogs() []Catalog {
	return c.catalogs
}

// Store delegates to the first catalog.
func (c *ChainCatalog) Store(ctx context.Context, ref Reference, content, bundleData []byte, annotations map[string]string, force bool, extraLayers ...Layer) (ArtifactInfo, error) {
	return c.catalogs[0].Store(ctx, ref, content, bundleData, annotations, force, extraLayers...)
}

// catalogOutcome records the result of trying one catalog in a chain, used
// to build an aggregated error that distinguishes "unreachable" catalogs,
// catalogs that were "checked and reported not found", and catalogs that
// failed with some other (non-not-found, non-unreachable) error such as an
// auth failure or malformed manifest.
type catalogOutcome struct {
	catalog string
	err     error
}

// aggregateChainError builds a final error from the per-catalog outcomes of
// a failed chain operation, classifying each outcome into one of three
// buckets: unreachable (network/transport failure), not-found (the catalog
// was reached and reported errors.Is(err, ErrArtifactNotFound)), or other
// (any other error, e.g. auth failure or malformed data). When one or more
// catalogs were unreachable, the message calls that out explicitly and
// separately from catalogs that genuinely reported the artifact does not
// exist — this is the fix for the misleading "not found" message when the
// real problem is that a catalog (often the official one, always last in
// the chain) could not be contacted. When there are "other" errors and no
// unreachable ones, the representative other error is surfaced directly
// instead of being folded into "not found". When every outcome is a genuine
// not-found, the returned error wraps ErrArtifactNotFound so callers using
// errors.Is/IsNotFound still recognize an ordinary chain miss. When no
// catalog was unreachable and none had an "other" error, the message is
// unchanged from before this behavior existed, so existing "not found"
// tests continue to pass unmodified.
// so existing "not found" tests continue to pass unmodified.
func aggregateChainError(subject string, outcomes []catalogOutcome) error {
	var unreachable []string
	var notFound []string
	var other []string
	var firstUnreachableErr error
	var firstOtherErr error

	for _, o := range outcomes {
		if o.err == nil {
			continue
		}
		if _, ok := IsCatalogUnreachable(o.err); ok {
			unreachable = append(unreachable, o.catalog)
			if firstUnreachableErr == nil {
				firstUnreachableErr = o.err
			}
			continue
		}
		if errors.Is(o.err, ErrArtifactNotFound) {
			notFound = append(notFound, o.catalog)
			continue
		}
		other = append(other, o.catalog)
		if firstOtherErr == nil {
			firstOtherErr = o.err
		}
	}

	if len(unreachable) == 0 && len(other) == 0 {
		return fmt.Errorf("artifact %q not found in any catalog: %w", subject, ErrArtifactNotFound)
	}

	if len(unreachable) == 0 {
		// Only "other" (non-not-found, non-unreachable) errors: surface the
		// representative cause instead of masking it as a plain not-found.
		return fmt.Errorf("%q failed in %s: %w", subject, strings.Join(other, ", "), firstOtherErr)
	}

	if len(notFound) == 0 && len(other) == 0 {
		return fmt.Errorf("%q unavailable: %s: %w", subject, strings.Join(unreachable, "; "), firstUnreachableErr)
	}

	checked := make([]string, 0, len(notFound)+len(other))
	checked = append(checked, notFound...)
	checked = append(checked, other...)
	return fmt.Errorf("%q unavailable: %s; not found in %s: %w",
		subject, strings.Join(unreachable, "; "), strings.Join(checked, ", "), firstUnreachableErr)
}

// Fetch tries each catalog in order, returning the first successful result.
func (c *ChainCatalog) Fetch(ctx context.Context, ref Reference) ([]byte, ArtifactInfo, error) {
	type fetchResult struct {
		content []byte
		info    ArtifactInfo
	}
	r, err := loopAndExecute(c, ref.String(), func(cat Catalog) (fetchResult, error) {
		content, info, err := cat.Fetch(ctx, ref)
		return fetchResult{content: content, info: info}, err
	})
	return r.content, r.info, err
}

// FetchWithBundle tries each catalog in order.
func (c *ChainCatalog) FetchWithBundle(ctx context.Context, ref Reference) ([]byte, []byte, ArtifactInfo, error) {
	type bundleResult struct {
		content []byte
		bundle  []byte
		info    ArtifactInfo
	}
	r, err := loopAndExecute(c, ref.String(), func(cat Catalog) (bundleResult, error) {
		content, bundle, info, err := cat.FetchWithBundle(ctx, ref)
		return bundleResult{content: content, bundle: bundle, info: info}, err
	})
	return r.content, r.bundle, r.info, err
}

// FetchWithLayer tries each catalog in order.
func (c *ChainCatalog) FetchWithLayer(ctx context.Context, ref Reference, mediaTypes ...string) ([]byte, map[string][]byte, ArtifactInfo, error) {
	type layerResult struct {
		content []byte
		layers  map[string][]byte
		info    ArtifactInfo
	}
	r, err := loopAndExecute(c, ref.String(), func(cat Catalog) (layerResult, error) {
		content, layers, info, err := cat.FetchWithLayer(ctx, ref, mediaTypes...)
		return layerResult{content: content, layers: layers, info: info}, err
	})
	return r.content, r.layers, r.info, err
}

// loopAndExecute runs action against each catalog in priority order, returning
// the first success. On a per-catalog error it records the outcome and consults
// haltOn: in strict mode any indeterminate error (anything but a definitive
// not-found) stops the chain so a lower-priority catalog is never silently
// substituted; in best-effort mode it continues. When the loop ends without a
// success, the aggregated error classifies the outcomes (unreachable vs
// not-found vs other).
func loopAndExecute[T any](c *ChainCatalog, subject string, action func(Catalog) (T, error)) (T, error) {
	var zero T
	var outcomes []catalogOutcome
	for _, cat := range c.catalogs {
		result, err := action(cat)
		if err == nil {
			c.logger.V(1).Info("chain resolved artifact", "catalog", cat.Name(), "subject", subject)
			return result, nil
		}
		if !errors.Is(err, ErrArtifactNotFound) {
			c.logger.V(1).Info("catalog error (non-404)", "catalog", cat.Name(), "subject", subject, "error", err)
		}
		outcomes = append(outcomes, catalogOutcome{catalog: cat.Name(), err: err})
		if c.haltOn(err) {
			return zero, aggregateChainError(subject, outcomes)
		}
	}
	return zero, aggregateChainError(subject, outcomes)
}

// haltOn reports whether an indeterminate per-catalog error must stop the
// chain. Only a definitive not-found is ever safe to skip past; in strict mode
// everything else (unreachable, auth, malformed) halts. In best-effort mode
// nothing halts and the chain falls through to the next catalog.
func (c *ChainCatalog) haltOn(err error) bool {
	if c.mode == ChainModeBestEffort {
		return false
	}
	return !errors.Is(err, ErrArtifactNotFound)
}

// Resolve tries each catalog in order, returning the first successful result.
func (c *ChainCatalog) Resolve(ctx context.Context, ref Reference) (ArtifactInfo, error) {
	return loopAndExecute(c, ref.String(), func(cat Catalog) (ArtifactInfo, error) {
		return cat.Resolve(ctx, ref)
	})
}

// List returns artifacts from all catalogs (deduplicated by name+version).
func (c *ChainCatalog) List(ctx context.Context, kind ArtifactKind, name string) ([]ArtifactInfo, error) {
	seen := make(map[string]bool)
	var results []ArtifactInfo

	for _, cat := range c.catalogs {
		items, err := cat.List(ctx, kind, name)
		if err != nil {
			c.logger.V(1).Info("catalog list error", "catalog", cat.Name(), "error", err)
			continue
		}
		for _, item := range items {
			key := item.Reference.String()
			if !seen[key] {
				seen[key] = true
				results = append(results, item)
			}
		}
	}

	return results, nil
}

// Exists returns true if the artifact exists in any catalog. If every
// catalog errors, the aggregated error distinguishes unreachable catalogs
// from ones that could not confirm existence.
func (c *ChainCatalog) Exists(ctx context.Context, ref Reference) (bool, error) {
	var outcomes []catalogOutcome
	for _, cat := range c.catalogs {
		ok, err := cat.Exists(ctx, ref)
		if err != nil {
			outcomes = append(outcomes, catalogOutcome{catalog: cat.Name(), err: err})
			continue
		}
		if ok {
			return true, nil
		}
	}
	if len(outcomes) == 0 {
		return false, nil
	}
	// At least one catalog couldn't determine existence; surface that if any
	// were unreachable, otherwise treat as a clean "not found" (false, nil)
	// to preserve prior behavior for benign resolve errors.
	for _, o := range outcomes {
		if _, ok := IsCatalogUnreachable(o.err); ok {
			return false, aggregateChainError(ref.String(), outcomes)
		}
	}
	return false, nil
}

// Delete delegates to the first catalog.
func (c *ChainCatalog) Delete(ctx context.Context, ref Reference) error {
	return c.catalogs[0].Delete(ctx, ref)
}

// FetchByPlatform tries each catalog that implements PlatformAwareCatalog in
// order, returning the first successful result. Catalogs that do not implement
// PlatformAwareCatalog are skipped. If a catalog explicitly reports the
// platform is not found (PlatformNotFoundError), the chain stops immediately
// because the artifact is known to be multi-platform and the platform is
// genuinely unavailable.
func (c *ChainCatalog) FetchByPlatform(ctx context.Context, ref Reference, platform string) ([]byte, ArtifactInfo, error) {
	subject := fmt.Sprintf("%s (%s)", ref.String(), platform)
	var outcomes []catalogOutcome
	supported := false
	for _, cat := range c.catalogs {
		pac, ok := cat.(PlatformAwareCatalog)
		if !ok {
			continue
		}
		supported = true
		data, info, err := pac.FetchByPlatform(ctx, ref, platform)
		if err == nil {
			c.logger.V(1).Info("fetched platform artifact", "catalog", cat.Name(), "ref", ref.String(), "platform", platform)
			return data, info, nil
		}
		// Definitive negative about the platform: halt in BOTH modes and return
		// the typed error, because the artifact is known multi-platform and the
		// requested platform is genuinely unavailable.
		if IsPlatformNotFound(err) {
			return nil, ArtifactInfo{}, err
		}
		if !errors.Is(err, ErrArtifactNotFound) {
			c.logger.V(1).Info("catalog platform fetch error (non-404)", "catalog", cat.Name(), "ref", ref.String(), "platform", platform, "error", err)
		}
		outcomes = append(outcomes, catalogOutcome{catalog: cat.Name(), err: err})
		// Strict mode: an indeterminate result stops the chain rather than
		// silently falling through to a lower-priority catalog.
		if c.haltOn(err) {
			return nil, ArtifactInfo{}, aggregateChainError(subject, outcomes)
		}
	}
	if !supported {
		return nil, ArtifactInfo{}, fmt.Errorf("no catalog supports platform-aware fetch for %q", ref.String())
	}
	return nil, ArtifactInfo{}, aggregateChainError(subject, outcomes)
}

// ResolveContentDigest tries each catalog that implements PlatformAwareCatalog
// in order, returning the first successful result. Catalogs that do not
// implement PlatformAwareCatalog are skipped.
func (c *ChainCatalog) ResolveContentDigest(ctx context.Context, ref Reference, platform, mediaType string) (ContentDigestInfo, error) {
	subject := fmt.Sprintf("%s (%s)", ref.String(), platform)
	var outcomes []catalogOutcome
	supported := false
	for _, cat := range c.catalogs {
		pac, ok := cat.(PlatformAwareCatalog)
		if !ok {
			continue
		}
		supported = true
		info, err := pac.ResolveContentDigest(ctx, ref, platform, mediaType)
		if err == nil {
			c.logger.V(1).Info("chain resolved content digest", "catalog", cat.Name(), "ref", ref.String(), "platform", platform)
			return info, nil
		}
		if IsPlatformNotFound(err) {
			return ContentDigestInfo{}, err
		}
		if !errors.Is(err, ErrArtifactNotFound) {
			c.logger.V(1).Info("catalog content digest error (non-404)", "catalog", cat.Name(), "ref", ref.String(), "platform", platform, "error", err)
		}
		outcomes = append(outcomes, catalogOutcome{catalog: cat.Name(), err: err})
		if c.haltOn(err) {
			return ContentDigestInfo{}, aggregateChainError(subject, outcomes)
		}
	}
	if !supported {
		return ContentDigestInfo{}, fmt.Errorf("no catalog supports content digest resolution for %q", ref.String())
	}
	return ContentDigestInfo{}, aggregateChainError(subject, outcomes)
}

// ListPlatforms tries each catalog that implements PlatformAwareCatalog in
// order, returning the first successful result.
func (c *ChainCatalog) ListPlatforms(ctx context.Context, ref Reference) ([]string, error) {
	var outcomes []catalogOutcome
	supported := false
	for _, cat := range c.catalogs {
		pac, ok := cat.(PlatformAwareCatalog)
		if !ok {
			continue
		}
		supported = true
		platforms, err := pac.ListPlatforms(ctx, ref)
		if err == nil {
			return platforms, nil
		}
		if !errors.Is(err, ErrArtifactNotFound) {
			c.logger.V(1).Info("catalog list platforms error (non-404)", "catalog", cat.Name(), "ref", ref.String(), "error", err)
		}
		outcomes = append(outcomes, catalogOutcome{catalog: cat.Name(), err: err})
		// Strict mode: an indeterminate result stops the chain rather than
		// silently falling through to a lower-priority catalog.
		if c.haltOn(err) {
			return nil, aggregateChainError(ref.String(), outcomes)
		}
	}
	if !supported {
		return nil, fmt.Errorf("no catalog supports platform listing for %q", ref.String())
	}
	return nil, aggregateChainError(ref.String(), outcomes)
}
