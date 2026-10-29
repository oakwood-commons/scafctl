// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package builder

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin"
	"github.com/oakwood-commons/scafctl/pkg/provider/official"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
)

const (
	// composedOutFilePerm is the mode of the --composed-out file.
	composedOutFilePerm = 0o600
	// jsonExt selects JSON output for --composed-out; anything else is YAML.
	jsonExt = ".json"
)

// runPreflight runs lint and functional tests; errors found block the build.
func (p *Packager) runPreflight(ctx context.Context, st *pipelineState) (bool, error) {
	pf, err := RunPreflight(ctx, p.src.Solution, p.src.Path(), *p.preflight)
	if err != nil {
		return false, fmt.Errorf("running pre-flight checks: %w", err)
	}
	st.result.Preflight = pf
	if pf.Blocked {
		return false, ErrPreflightBlocked
	}
	return false, nil
}

// reportCompose reports the compose: parts merged in at load time, plus the
// solution file itself.
func (p *Packager) reportCompose(_ context.Context, st *pipelineState) (bool, error) {
	st.addMessagef("Composed %d file(s) into solution", len(p.src.ComposeFiles())+1)
	return false, nil
}

// discover finds the local files and catalog references to bundle.
func (p *Packager) discover(ctx context.Context, st *pipelineState) (bool, error) {
	discovery, err := discoverBundleFiles(p.src.Solution, p.src.BundleRoot)
	if err != nil {
		return false, err
	}
	logger.FromContext(ctx).V(1).Info("discovered files",
		"localFiles", len(discovery.LocalFiles),
		"catalogRefs", len(discovery.CatalogRefs))
	st.discovery = discovery
	st.result.Build = &BuildResult{Discovery: discovery}
	return false, nil
}

// discoverBundleFiles loads .scafctlignore and runs static analysis + glob
// expansion from bundleRoot.
func discoverBundleFiles(sol *solution.Solution, bundleRoot string) (*bundler.DiscoveryResult, error) {
	ignoreChecker, err := bundler.LoadScafctlIgnore(bundleRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to load .scafctlignore: %w", err)
	}
	discovery, err := bundler.DiscoverFiles(sol, bundleRoot, bundler.WithIgnoreChecker(ignoreChecker))
	if err != nil {
		return nil, fmt.Errorf("failed to discover files: %w", err)
	}
	return discovery, nil
}

// checkPlugins validates bundle.plugins, applies plugin defaults, and checks
// every referenced provider is built in, official, or declared as a plugin.
func (p *Packager) checkPlugins(ctx context.Context, _ *pipelineState) (bool, error) {
	sol := p.src.Solution
	if len(sol.Bundle.Plugins) > 0 {
		if err := bundler.ValidatePlugins(sol); err != nil {
			return false, fmt.Errorf("plugin validation failed: %w", err)
		}
		bundler.MergePluginDefaults(sol)
	}
	return false, validateProviderCoverage(sol, builtin.ProviderNames(), official.RegistryFromContext(ctx))
}

// stopDryRun ends the pipeline before the first write.
func (p *Packager) stopDryRun(_ context.Context, st *pipelineState) (bool, error) {
	st.result.DryRun = true
	return true, nil
}

// writeComposedOut writes the composed solution to the --composed-out path:
// JSON for a .json path, YAML otherwise.
func (p *Packager) writeComposedOut(_ context.Context, st *pipelineState) (bool, error) {
	data, err := marshalComposed(p.src.Solution, p.composedOut)
	if err != nil {
		return false, fmt.Errorf("serializing composed solution: %w", err)
	}
	if err := os.WriteFile(p.composedOut, data, composedOutFilePerm); err != nil {
		return false, fmt.Errorf("writing composed solution: %w", err)
	}
	st.result.ComposedOut = p.composedOut
	return false, nil
}

// marshalComposed encodes sol in the format the path's extension selects.
func marshalComposed(sol *solution.Solution, path string) ([]byte, error) {
	if strings.EqualFold(filepath.Ext(path), jsonExt) {
		return sol.ToJSONPretty()
	}
	return sol.ToYAML()
}

// render produces the solution document to store.
func (p *Packager) render(_ context.Context, st *pipelineState) (bool, error) {
	content, err := p.src.Document()
	if err != nil {
		return false, err
	}
	st.content = content
	return false, nil
}

// addMessagef records a progress message for the caller.
func (st *pipelineState) addMessagef(format string, args ...any) {
	st.result.Messages = append(st.result.Messages, fmt.Sprintf(format, args...))
}
