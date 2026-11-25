// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/plugin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedLocalArtifact(t *testing.T, catalogDir string, kind catalog.ArtifactKind, name, version string, content []byte) {
	t.Helper()
	localCat, err := catalog.NewLocalCatalogAt(catalogDir, logr.Discard())
	require.NoError(t, err)
	_, err = localCat.Store(context.Background(), catalog.Reference{
		Kind:    kind,
		Name:    name,
		Version: semver.MustParse(version),
	}, content, nil, nil, false)
	require.NoError(t, err)
}

func seedRemoteArtifact(t *testing.T, registryAddr string, kind catalog.ArtifactKind, name, version string, content []byte) {
	t.Helper()
	remoteCat, err := catalog.NewRemoteCatalog(catalog.RemoteCatalogConfig{
		Name:       "seed",
		Registry:   registryAddr,
		Repository: "scafctl",
		Insecure:   true,
		Logger:     logr.Discard(),
	})
	require.NoError(t, err)
	_, err = remoteCat.Store(context.Background(), catalog.Reference{
		Kind:    kind,
		Name:    name,
		Version: semver.MustParse(version),
	}, content, nil, nil, false)
	require.NoError(t, err)
}

func TestIntegration_CatalogInspect_LocalKindAndPreRelease(t *testing.T) {
	t.Parallel()

	env := isolatedCatalogEnv(t)
	catalogDir := filepath.Join(env["XDG_DATA_HOME"], "scafctl", "catalog")

	seedLocalArtifact(t, catalogDir, catalog.ArtifactKindProvider, "exec", "0.4.0", []byte("exec-0.4.0"))
	seedLocalArtifact(t, catalogDir, catalog.ArtifactKindProvider, "exec", "0.5.0-dev", []byte("exec-0.5.0-dev"))
	seedLocalArtifact(t, catalogDir, catalog.ArtifactKindAuthHandler, "entra", "0.2.0", []byte("entra-0.2.0"))
	seedLocalArtifact(t, catalogDir, catalog.ArtifactKindSolution, "ambig-art", "1.0.0", []byte("sol-1.0.0"))
	seedLocalArtifact(t, catalogDir, catalog.ArtifactKindProvider, "ambig-art", "1.1.0", []byte("prov-1.1.0"))

	t.Run("omitted kind probes local kinds and selects stable version by default", func(t *testing.T) {
		t.Parallel()
		stdout, stderr, code := runScafctlWithEnv(t, env, "catalog", "inspect", "exec", "-o", "json")
		require.Equal(t, exitcode.Success, code, "stderr: %s", stderr)

		var detail map[string]any
		require.NoError(t, json.Unmarshal([]byte(stdout), &detail))
		assert.Equal(t, "exec", detail["name"])
		assert.Equal(t, "provider", detail["kind"])
		assert.Equal(t, "0.4.0", detail["version"])
		assert.Equal(t, "local", detail["catalog"])
	})

	t.Run("auth-handler inspected with and without --kind and with --catalog local", func(t *testing.T) {
		t.Parallel()
		stdout, stderr, code := runScafctlWithEnv(t, env, "catalog", "inspect", "entra", "--kind", "auth-handler", "--catalog", "local", "-o", "json")
		require.Equal(t, exitcode.Success, code, "stderr: %s", stderr)

		var detail map[string]any
		require.NoError(t, json.Unmarshal([]byte(stdout), &detail))
		assert.Equal(t, "entra", detail["name"])
		assert.Equal(t, "auth-handler", detail["kind"])
		assert.Equal(t, "0.2.0", detail["version"])
	})

	t.Run("--pre-release resolves pre-release for unpinned and --version range", func(t *testing.T) {
		t.Parallel()
		stdoutPre, stderrPre, codePre := runScafctlWithEnv(t, env, "catalog", "inspect", "exec", "--kind", "provider", "--pre-release", "-o", "json")
		require.Equal(t, exitcode.Success, codePre, "stderr: %s", stderrPre)
		var preDetail map[string]any
		require.NoError(t, json.Unmarshal([]byte(stdoutPre), &preDetail))
		assert.Equal(t, "0.5.0-dev", preDetail["version"])

		stdoutRangeStable, stderrRS, codeRS := runScafctlWithEnv(t, env, "catalog", "inspect", "exec", "--kind", "provider", "--version", ">=0.4.0", "-o", "json")
		require.Equal(t, exitcode.Success, codeRS, "stderr: %s", stderrRS)
		var rsDetail map[string]any
		require.NoError(t, json.Unmarshal([]byte(stdoutRangeStable), &rsDetail))
		assert.Equal(t, "0.4.0", rsDetail["version"])

		stdoutRangePre, stderrRP, codeRP := runScafctlWithEnv(t, env, "catalog", "inspect", "exec", "--kind", "provider", "--version", ">=0.4.0", "--pre-release", "-o", "json")
		require.Equal(t, exitcode.Success, codeRP, "stderr: %s", stderrRP)
		var rpDetail map[string]any
		require.NoError(t, json.Unmarshal([]byte(stdoutRangePre), &rpDetail))
		assert.Equal(t, "0.5.0-dev", rpDetail["version"])
	})

	t.Run("exact name@0.5.0-dev resolves without --pre-release", func(t *testing.T) {
		t.Parallel()
		stdout, stderr, code := runScafctlWithEnv(t, env, "catalog", "inspect", "exec@0.5.0-dev", "--kind", "provider", "-o", "json")
		require.Equal(t, exitcode.Success, code, "stderr: %s", stderr)

		var detail map[string]any
		require.NoError(t, json.Unmarshal([]byte(stdout), &detail))
		assert.Equal(t, "0.5.0-dev", detail["version"])
	})

	t.Run("multi-kind ambiguity requires --kind and exits InvalidInput", func(t *testing.T) {
		t.Parallel()
		_, stderr, code := runScafctlWithEnv(t, env, "catalog", "inspect", "ambig-art")
		assert.Equal(t, exitcode.InvalidInput, code)
		assert.Contains(t, stderr, `artifact "ambig-art" matches multiple kinds (solution, provider); specify --kind`)

		stdoutSol, stderrSol, codeSol := runScafctlWithEnv(t, env, "catalog", "inspect", "ambig-art", "--kind", "solution", "-o", "json")
		require.Equal(t, exitcode.Success, codeSol, "stderr: %s", stderrSol)
		var solDetail map[string]any
		require.NoError(t, json.Unmarshal([]byte(stdoutSol), &solDetail))
		assert.Equal(t, "solution", solDetail["kind"])
		assert.Equal(t, "1.0.0", solDetail["version"])
	})

	t.Run("invalid flag combinations exit InvalidInput", func(t *testing.T) {
		t.Parallel()
		_, stderrKind, codeKind := runScafctlWithEnv(t, env, "catalog", "inspect", "exec", "--kind", "bogus")
		assert.Equal(t, exitcode.InvalidInput, codeKind)
		assert.Contains(t, stderrKind, "invalid kind")

		_, stderrLocalRef, codeLocalRef := runScafctlWithEnv(t, env, "catalog", "inspect", "exec@0.4.0", "--referrers", "--catalog", "local")
		assert.Equal(t, exitcode.InvalidInput, codeLocalRef)
		assert.Contains(t, stderrLocalRef, "--referrers cannot be used with the local catalog")

		_, stderrPreRef, codePreRef := runScafctlWithEnv(t, env, "catalog", "inspect", "exec@0.4.0", "--referrers", "--pre-release", "--catalog", "example.com/org")
		assert.Equal(t, exitcode.InvalidInput, codePreRef)
		assert.Contains(t, stderrPreRef, "--pre-release cannot be used with --referrers")
	})
}

func TestIntegration_CatalogInspect_RemoteAndPluginsInstallPreRelease(t *testing.T) {
	t.Parallel()

	registryAddr, _ := startOCIRegistry(t)
	seedRemoteArtifact(t, registryAddr, catalog.ArtifactKindProvider, "exec", "0.4.0", []byte("remote-exec-0.4.0"))
	seedRemoteArtifact(t, registryAddr, catalog.ArtifactKindProvider, "exec", "0.5.0-dev", []byte("remote-exec-0.5.0-dev"))
	seedRemoteArtifact(t, registryAddr, catalog.ArtifactKindSolution, "multi", "1.0.0", []byte("remote-sol-1.0.0"))
	seedRemoteArtifact(t, registryAddr, catalog.ArtifactKindProvider, "multi", "1.2.0", []byte("remote-prov-1.2.0"))

	makeRemoteEnv := func(t *testing.T) map[string]string {
		t.Helper()
		tmpDir := t.TempDir()
		cfgDir := filepath.Join(tmpDir, "scafctl")
		require.NoError(t, os.MkdirAll(cfgDir, 0o755))
		cfgYAML := fmt.Sprintf(`catalogs:
  - name: local
    type: filesystem
  - name: test-remote
    type: oci
    url: oci://%s/scafctl
    insecure: true
settings:
  disableOfficialCatalog: true
`, registryAddr)
		require.NoError(t, os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte(cfgYAML), 0o600))
		return map[string]string{
			"XDG_DATA_HOME":   tmpDir,
			"XDG_CACHE_HOME":  tmpDir,
			"XDG_CONFIG_HOME": tmpDir,
		}
	}

	t.Run("remote catalog inspect supports --kind, omitted kind, --pre-release, exact 0.5.0-dev, and --version range", func(t *testing.T) {
		t.Parallel()
		env := makeRemoteEnv(t)

		// Omitted --kind against --catalog test-remote resolves provider exec@0.4.0.
		stdout, stderr, code := runScafctlWithEnv(t, env, "catalog", "inspect", "exec", "--catalog", "test-remote", "--insecure", "-o", "json")
		require.Equal(t, exitcode.Success, code, "stderr: %s", stderr)
		var detail map[string]any
		require.NoError(t, json.Unmarshal([]byte(stdout), &detail))
		assert.Equal(t, "provider", detail["kind"])
		assert.Equal(t, "0.4.0", detail["version"])

		// --pre-release resolves 0.5.0-dev.
		stdoutPre, stderrPre, codePre := runScafctlWithEnv(t, env, "catalog", "inspect", "exec", "--kind", "provider", "--catalog", "test-remote", "--pre-release", "--insecure", "-o", "json")
		require.Equal(t, exitcode.Success, codePre, "stderr: %s", stderrPre)
		var preDetail map[string]any
		require.NoError(t, json.Unmarshal([]byte(stdoutPre), &preDetail))
		assert.Equal(t, "0.5.0-dev", preDetail["version"])

		// Exact exec@0.5.0-dev resolves without --pre-release.
		stdoutExact, stderrExact, codeExact := runScafctlWithEnv(t, env, "catalog", "inspect", "exec@0.5.0-dev", "--catalog", "test-remote", "--insecure", "-o", "json")
		require.Equal(t, exitcode.Success, codeExact, "stderr: %s", stderrExact)
		var exactDetail map[string]any
		require.NoError(t, json.Unmarshal([]byte(stdoutExact), &exactDetail))
		assert.Equal(t, "0.5.0-dev", exactDetail["version"])

		// --version range without and with --pre-release.
		stdoutRangeStable, stderrRangeStable, codeRangeStable := runScafctlWithEnv(t, env, "catalog", "inspect", "exec", "--catalog", "test-remote", "--version", ">=0.4.0", "--insecure", "-o", "json")
		require.Equal(t, exitcode.Success, codeRangeStable, "stderr: %s", stderrRangeStable)
		var rangeStableDetail map[string]any
		require.NoError(t, json.Unmarshal([]byte(stdoutRangeStable), &rangeStableDetail))
		assert.Equal(t, "0.4.0", rangeStableDetail["version"])

		stdoutRange, stderrRange, codeRange := runScafctlWithEnv(t, env, "catalog", "inspect", "exec", "--catalog", "test-remote", "--version", ">=0.4.0", "--pre-release", "--insecure", "-o", "json")
		require.Equal(t, exitcode.Success, codeRange, "stderr: %s", stderrRange)
		var rangeDetail map[string]any
		require.NoError(t, json.Unmarshal([]byte(stdoutRange), &rangeDetail))
		assert.Equal(t, "0.5.0-dev", rangeDetail["version"])

		// Multi-kind ambiguity on remote both unpinned and with --version when --kind is omitted.
		_, stderrAmbigUnpinned, codeAmbigUnpinned := runScafctlWithEnv(t, env, "catalog", "inspect", "multi", "--catalog", "test-remote", "--insecure")
		assert.Equal(t, exitcode.InvalidInput, codeAmbigUnpinned)
		assert.Contains(t, stderrAmbigUnpinned, `artifact "multi" matches multiple kinds (solution, provider); specify --kind`)

		_, stderrAmbig, codeAmbig := runScafctlWithEnv(t, env, "catalog", "inspect", "multi", "--catalog", "test-remote", "--version", ">=1.0.0", "--insecure")
		assert.Equal(t, exitcode.InvalidInput, codeAmbig)
		assert.Contains(t, stderrAmbig, `artifact "multi" matches multiple kinds (solution, provider); specify --kind`)

		// Full OCI reference with conflicting --kind fails with InvalidInput.
		fullRef := registryAddr + "/scafctl/providers/exec@0.4.0"
		_, stderrConflict, codeConflict := runScafctlWithEnv(t, env, "catalog", "inspect", fullRef, "--kind", "solution", "--insecure")
		assert.Equal(t, exitcode.InvalidInput, codeConflict)
		assert.Contains(t, stderrConflict, "conflicts with artifact kind")

		// Full OCI reference with matching --kind and --pre-release succeeds.
		fullUnpinnedRef := registryAddr + "/scafctl/providers/exec"
		stdoutFull, stderrFull, codeFull := runScafctlWithEnv(t, env, "catalog", "inspect", fullUnpinnedRef, "--kind", "provider", "--pre-release", "--insecure", "-o", "json")
		require.Equal(t, exitcode.Success, codeFull, "stderr: %s", stderrFull)
		var fullDetail map[string]any
		require.NoError(t, json.Unmarshal([]byte(stdoutFull), &fullDetail))
		assert.Equal(t, "0.5.0-dev", fullDetail["version"])
	})

	t.Run("plugins install --pre-release resolves pre-release for unpinned and range constraints", func(t *testing.T) {
		t.Parallel()
		env := makeRemoteEnv(t)
		plat := plugin.CurrentPlatform()

		// Unpinned without and with --pre-release.
		stdoutUnpinnedStable, stderrUnpinnedStable, codeUnpinnedStable := runScafctlWithEnv(t, env, "plugins", "install", "exec", "--platform", plat, "--cache-dir", t.TempDir())
		require.Equal(t, exitcode.Success, codeUnpinnedStable, "stderr: %s", stderrUnpinnedStable)
		assert.Contains(t, stdoutUnpinnedStable, "exec@0.4.0")

		stdoutUnpinnedPre, stderrUnpinnedPre, codeUnpinnedPre := runScafctlWithEnv(t, env, "plugins", "install", "exec", "--pre-release", "--platform", plat, "--cache-dir", t.TempDir())
		require.Equal(t, exitcode.Success, codeUnpinnedPre, "stderr: %s", stderrUnpinnedPre)
		assert.Contains(t, stdoutUnpinnedPre, "exec@0.5.0-dev")

		// Range without --pre-release installs 0.4.0.
		stdoutStable, stderrStable, codeStable := runScafctlWithEnv(t, env, "plugins", "install", "exec", "--version", ">=0.4.0", "--platform", plat, "--cache-dir", t.TempDir())
		require.Equal(t, exitcode.Success, codeStable, "stderr: %s", stderrStable)
		assert.Contains(t, stdoutStable, "exec@0.4.0")

		// With --pre-release, installs 0.5.0-dev.
		stdoutPre, stderrPre, codePre := runScafctlWithEnv(t, env, "plugins", "install", "exec", "--version", ">=0.4.0", "--pre-release", "--platform", plat, "--cache-dir", t.TempDir())
		require.Equal(t, exitcode.Success, codePre, "stderr: %s", stderrPre)
		assert.Contains(t, stdoutPre, "exec@0.5.0-dev")

		// Exact exec@0.5.0-dev installs without --pre-release.
		stdoutExact, stderrExact, codeExact := runScafctlWithEnv(t, env, "plugins", "install", "exec@0.5.0-dev", "--platform", plat, "--cache-dir", t.TempDir())
		require.Equal(t, exitcode.Success, codeExact, "stderr: %s", stderrExact)
		assert.Contains(t, stdoutExact, "exec@0.5.0-dev")
	})
}

func TestIntegration_CatalogInspect_EmbedderBinaryName(t *testing.T) {
	t.Parallel()

	registryAddr, _ := startOCIRegistry(t)
	seedRemoteArtifact(t, registryAddr, catalog.ArtifactKindProvider, "exec", "0.4.0", []byte("embedder-exec-0.4.0"))
	seedRemoteArtifact(t, registryAddr, catalog.ArtifactKindProvider, "exec", "0.5.0-dev", []byte("embedder-exec-0.5.0-dev"))

	xdgRoot := t.TempDir()
	cfgDir := filepath.Join(xdgRoot, "config", "mycli")
	require.NoError(t, os.MkdirAll(cfgDir, 0o755))
	cfgYAML := fmt.Sprintf(`catalogs:
  - name: local
    type: filesystem
  - name: embedder-remote
    type: oci
    url: oci://%s/scafctl
    insecure: true
settings:
  disableOfficialCatalog: true
`, registryAddr)
	require.NoError(t, os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte(cfgYAML), 0o600))

	testBin, err := os.Executable()
	require.NoError(t, err)

	runMyCLI := func(args ...string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		cmdArgs := append([]string{"--"}, args...)
		cmd := exec.CommandContext(ctx, testBin, cmdArgs...)
		cmd.Dir = findProjectRoot()
		cmd.Env = append(os.Environ(),
			"SCAFCTL_TEST_EMBEDDER_HELPER=1",
			"XDG_CONFIG_HOME="+filepath.Join(xdgRoot, "config"),
			"XDG_DATA_HOME="+filepath.Join(xdgRoot, "data"),
			"XDG_CACHE_HOME="+filepath.Join(xdgRoot, "cache"),
		)
		out, runErr := cmd.CombinedOutput()
		require.NoError(t, runErr, "mycli %v failed: %s", args, string(out))
		return string(out)
	}

	helpOut := runMyCLI("catalog", "inspect", "--help")
	assert.Contains(t, helpOut, "mycli catalog inspect")
	assert.Contains(t, helpOut, "--kind")
	assert.Contains(t, helpOut, "--pre-release")

	inspectOut := runMyCLI("catalog", "inspect", "exec", "--kind", "provider", "--catalog", "embedder-remote", "--pre-release", "--insecure", "-o", "json")
	var detail map[string]any
	require.NoError(t, json.Unmarshal([]byte(inspectOut), &detail))
	assert.Equal(t, "provider", detail["kind"])
	assert.Equal(t, "0.5.0-dev", detail["version"])

	installOut := runMyCLI("plugins", "install", "exec", "--version", ">=0.4.0", "--pre-release", "--platform", plugin.CurrentPlatform())
	assert.Contains(t, installOut, "exec@0.5.0-dev")
	assert.DirExists(t, filepath.Join(xdgRoot, "cache", "mycli", "plugins"))
}
