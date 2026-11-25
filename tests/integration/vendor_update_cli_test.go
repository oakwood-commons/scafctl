// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type publishedPluginArtifact struct {
	kind         string // "providers" or "auth-handlers"
	artifactType string // "provider" or "auth-handler"
	configMedia  string
	layerMedia   string
	name         string
	version      string
	linuxDigest  string
	darwinDigest string
	indexBytes   []byte
	indexDigest  string
	manifests    map[string]struct {
		mediaType string
		data      []byte
	}
}

type mutablePluginRegistry struct {
	mu      sync.RWMutex
	byRepo  map[string]map[string]*publishedPluginArtifact // repo -> version -> artifact
	tagList map[string][]string                            // repo -> ordered tags
}

func newMutablePluginRegistry(t *testing.T) (host string, reg *mutablePluginRegistry) {
	t.Helper()
	reg = &mutablePluginRegistry{
		byRepo:  make(map[string]map[string]*publishedPluginArtifact),
		tagList: make(map[string][]string),
	}

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path == "/v2/" || path == "/v2" {
			w.WriteHeader(http.StatusOK)
			return
		}
		if !strings.HasPrefix(path, "/v2/") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		rest := strings.TrimPrefix(path, "/v2/")

		if idx := strings.Index(rest, "/tags/list"); idx > 0 {
			repo := rest[:idx]
			reg.mu.RLock()
			tags, ok := reg.tagList[repo]
			reg.mu.RUnlock()
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"name": repo,
				"tags": tags,
			})
			return
		}

		if idx := strings.Index(rest, "/manifests/"); idx > 0 {
			repo := rest[:idx]
			ref := rest[idx+len("/manifests/"):]
			reg.mu.RLock()
			versions := reg.byRepo[repo]
			var foundMedia string
			var foundData []byte
			var foundDigest string
			for _, art := range versions {
				if ref == art.version {
					foundMedia = "application/vnd.oci.image.index.v1+json"
					foundData = art.indexBytes
					foundDigest = art.indexDigest
					break
				}
				if m, ok := art.manifests[ref]; ok {
					foundMedia = m.mediaType
					foundData = m.data
					foundDigest = ref
					break
				}
			}
			reg.mu.RUnlock()
			if foundData == nil {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", foundMedia)
			w.Header().Set("Docker-Content-Digest", foundDigest)
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(foundData)))
			if r.Method == http.MethodGet {
				_, _ = w.Write(foundData)
			}
			return
		}

		if idx := strings.Index(rest, "/blobs/"); idx > 0 {
			repo := rest[:idx]
			dgst := rest[idx+len("/blobs/"):]
			reg.mu.RLock()
			versions := reg.byRepo[repo]
			var blobData []byte
			for _, art := range versions {
				if m, ok := art.manifests[dgst]; ok {
					blobData = m.data
					break
				}
			}
			reg.mu.RUnlock()
			if blobData != nil {
				w.Header().Set("Docker-Content-Digest", dgst)
				w.Header().Set("Content-Length", fmt.Sprintf("%d", len(blobData)))
				if r.Method == http.MethodGet {
					_, _ = w.Write(blobData)
				}
				return
			}
			// Binary layer blobs intentionally return 500 to prove vendor update
			// resolves plugin digests strictly from OCI manifests without downloading binaries.
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	return strings.TrimPrefix(srv.URL, "https://"), reg
}

func (r *mutablePluginRegistry) publishPlugin(artifactType, name, version string) (linuxDigest, darwinDigest string) {
	sha256Hex := func(b []byte) string {
		sum := sha256.Sum256(b)
		return "sha256:" + hex.EncodeToString(sum[:])
	}

	repoKind := "providers"
	configMedia := "application/vnd.scafctl.provider.config.v1+json"
	layerMedia := "application/vnd.scafctl.provider.v1+binary"
	if artifactType == "auth-handler" {
		repoKind = "auth-handlers"
		configMedia = "application/vnd.scafctl.auth-handler.config.v1+json"
		layerMedia = "application/vnd.scafctl.auth-handler.v1+binary"
	}

	linuxBin := []byte(fmt.Sprintf("fake-%s-%s-%s-linux-amd64", artifactType, name, version))
	darwinBin := []byte(fmt.Sprintf("fake-%s-%s-%s-darwin-arm64", artifactType, name, version))
	linuxDigest = sha256Hex(linuxBin)
	darwinDigest = sha256Hex(darwinBin)

	cfgBytes := []byte(`{}`)
	cfgDigest := sha256Hex(cfgBytes)

	buildManifest := func(plat, layerDigest string, layerSize int) ([]byte, string) {
		doc := fmt.Sprintf(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"mediaType":%q,"digest":%q,"size":%d},"layers":[{"mediaType":%q,"digest":%q,"size":%d}],"annotations":{"dev.scafctl.artifact.name":%q,"dev.scafctl.artifact.type":%q,"org.opencontainers.image.version":%q,"dev.scafctl.artifact.platform":%q}}`,
			configMedia, cfgDigest, len(cfgBytes), layerMedia, layerDigest, layerSize, name, artifactType, version, plat)
		b := []byte(doc)
		return b, sha256Hex(b)
	}

	linuxManifest, linuxManifestDigest := buildManifest("linux/amd64", linuxDigest, len(linuxBin))
	darwinManifest, darwinManifestDigest := buildManifest("darwin/arm64", darwinDigest, len(darwinBin))

	indexDoc := fmt.Sprintf(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.index.v1+json","manifests":[{"mediaType":"application/vnd.oci.image.manifest.v1+json","digest":%q,"size":%d,"platform":{"os":"linux","architecture":"amd64"}},{"mediaType":"application/vnd.oci.image.manifest.v1+json","digest":%q,"size":%d,"platform":{"os":"darwin","architecture":"arm64"}}],"annotations":{"dev.scafctl.artifact.name":%q,"dev.scafctl.artifact.type":%q,"org.opencontainers.image.version":%q}}`,
		linuxManifestDigest, len(linuxManifest), darwinManifestDigest, len(darwinManifest), name, artifactType, version)
	indexBytes := []byte(indexDoc)
	indexDigest := sha256Hex(indexBytes)

	art := &publishedPluginArtifact{
		kind:         repoKind,
		artifactType: artifactType,
		configMedia:  configMedia,
		layerMedia:   layerMedia,
		name:         name,
		version:      version,
		linuxDigest:  linuxDigest,
		darwinDigest: darwinDigest,
		indexBytes:   indexBytes,
		indexDigest:  indexDigest,
		manifests: map[string]struct {
			mediaType string
			data      []byte
		}{
			indexDigest:          {"application/vnd.oci.image.index.v1+json", indexBytes},
			linuxManifestDigest:  {"application/vnd.oci.image.manifest.v1+json", linuxManifest},
			darwinManifestDigest: {"application/vnd.oci.image.manifest.v1+json", darwinManifest},
			cfgDigest:            {configMedia, cfgBytes},
		},
	}

	repo := fmt.Sprintf("scafctl/%s/%s", repoKind, name)
	r.mu.Lock()
	if r.byRepo[repo] == nil {
		r.byRepo[repo] = make(map[string]*publishedPluginArtifact)
	}
	if _, exists := r.byRepo[repo][version]; !exists {
		r.tagList[repo] = append(r.tagList[repo], version)
	}
	r.byRepo[repo][version] = art
	r.mu.Unlock()

	return linuxDigest, darwinDigest
}

// TestIntegration_VendorUpdate_PluginsAndRemoteSolutions verifies end-to-end
// scafctl vendor update across remote sub-solutions (via startOCIRegistry) and
// multi-platform plugins, covering --dry-run, -o json/-o yaml, --dependency,
// --pre-release, non-destructive vendor file retention, and no-op lock stability.
func TestIntegration_VendorUpdate_PluginsAndRemoteSolutions(t *testing.T) {
	t.Parallel()
	if (runtime.GOOS != "linux" || runtime.GOARCH != "amd64") && (runtime.GOOS != "darwin" || runtime.GOARCH != "arm64") {
		t.Skip("fixture publishes linux/amd64 and darwin/arm64")
	}

	solRegistryAddr, _ := startOCIRegistry(t)
	pluginHost, pluginReg := newMutablePluginRegistry(t)
	v015Linux, v015Darwin := pluginReg.publishPlugin("provider", "directory", "0.1.5")
	authV010Linux, _ := pluginReg.publishPlugin("auth-handler", "acme-auth", "0.1.0")

	// Publish child-sol@1.0.0 to the remote OCI solution registry from an isolated publisher env.
	pubEnv := isolatedCatalogEnv(t)
	pubDir := t.TempDir()
	childV100 := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: child-sol
  version: 1.0.0
spec:
  resolvers:
    msg:
      resolve:
        with:
          - provider: static
            inputs:
              value: "v1.0.0"
`
	require.NoError(t, os.WriteFile(filepath.Join(pubDir, "solution.yaml"), []byte(childV100), 0o600))
	_, stderr, exitCode := runScafctlWithEnvInDir(t, pubDir, pubEnv,
		"package", "solution", "-f", "solution.yaml", "--version", "1.0.0", "--skip-tests")
	require.Equal(t, 0, exitCode, "package child 1.0.0 failed: %s", stderr)

	_, stderr, exitCode = runScafctlWithEnv(t, pubEnv,
		"catalog", "push", "solution", "child-sol@1.0.0",
		"--catalog", solRegistryAddr+"/scafctl", "--insecure")
	require.Equal(t, 0, exitCode, "push child 1.0.0 failed: %s", stderr)

	// Consumer workspace has BOTH remote catalogs configured.
	xdgRoot := t.TempDir()
	cfgDir := filepath.Join(xdgRoot, "config", "scafctl")
	require.NoError(t, os.MkdirAll(cfgDir, 0o700))
	cfgYAML := fmt.Sprintf(`catalogs:
  - name: local
    type: filesystem
  - name: solutions-remote
    type: oci
    url: oci://%s/scafctl
    insecure: true
    artifactKinds:
      - solution
  - name: plugins-remote
    type: oci
    url: oci://%s/scafctl
    insecure: true
    artifactKinds:
      - provider
      - auth-handler
settings:
  disableOfficialCatalog: true
`, solRegistryAddr, pluginHost)
	require.NoError(t, os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte(cfgYAML), 0o600))

	consumerEnv := map[string]string{
		"XDG_CONFIG_HOME": filepath.Join(xdgRoot, "config"),
		"XDG_DATA_HOME":   filepath.Join(xdgRoot, "data"),
		"XDG_CACHE_HOME":  filepath.Join(xdgRoot, "cache"),
	}

	// Pull child-sol@1.0.0 once so initial `package solution` vendors it, then package parent.
	_, stderr, exitCode = runScafctlWithEnv(t, consumerEnv,
		"catalog", "pull", "solution", "child-sol@1.0.0",
		"--catalog", solRegistryAddr+"/scafctl", "--insecure")
	require.Equal(t, 0, exitCode, "pull child 1.0.0 failed: %s", stderr)

	solDir := t.TempDir()
	parentYAML := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: parent-app
  version: 0.1.0
bundle:
  plugins:
    - name: directory
      kind: provider
      version: ^0.1.0
    - name: acme-auth
      kind: auth-handler
      version: ^0.1.0
spec:
  resolvers:
    sub:
      resolve:
        with:
          - provider: solution
            inputs:
              source: child-sol@^1.0.0
    files:
      resolve:
        with:
          - provider: directory
            inputs:
              path: "."
`
	require.NoError(t, os.WriteFile(filepath.Join(solDir, "solution.yaml"), []byte(parentYAML), 0o600))

	stdout, stderr, exitCode := runScafctlWithEnvInDir(t, solDir, consumerEnv,
		"package", "solution", "-f", "solution.yaml", "--version", "0.1.0", "--skip-tests", "--no-verify")
	require.Equal(t, 0, exitCode, "initial package parent failed: out=%s err=%s", stdout, stderr)

	require.FileExists(t, filepath.Join(solDir, ".scafctl", "vendor", "child-sol@1.0.0.yaml"))
	initialLock, err := bundler.LoadLockFile(filepath.Join(solDir, "solution.lock"))
	require.NoError(t, err)
	require.Len(t, initialLock.Plugins, 2)
	assert.Equal(t, "0.1.5", initialLock.Plugins[0].Version)
	assert.Equal(t, v015Linux, initialLock.Plugins[0].Digests["linux/amd64"])
	assert.Equal(t, v015Darwin, initialLock.Plugins[0].Digests["darwin/arm64"])
	assert.Equal(t, "0.1.0", initialLock.Plugins[1].Version)
	assert.Equal(t, authV010Linux, initialLock.Plugins[1].Digests["linux/amd64"])

	// Now publish child-sol@1.1.0 ONLY to remote OCI registry (not pulled locally!)
	// and directory@0.1.6 + directory@0.2.0-rc.1 + acme-auth@0.1.1 to the remote plugin registry.
	childV110 := strings.ReplaceAll(childV100, "1.0.0", "1.1.0")
	require.NoError(t, os.WriteFile(filepath.Join(pubDir, "solution.yaml"), []byte(childV110), 0o600))
	_, stderr, exitCode = runScafctlWithEnvInDir(t, pubDir, pubEnv,
		"package", "solution", "-f", "solution.yaml", "--version", "1.1.0", "--skip-tests")
	require.Equal(t, 0, exitCode, "package child 1.1.0 failed: %s", stderr)
	_, stderr, exitCode = runScafctlWithEnv(t, pubEnv,
		"catalog", "push", "solution", "child-sol@1.1.0",
		"--catalog", solRegistryAddr+"/scafctl", "--insecure")
	require.Equal(t, 0, exitCode, "push child 1.1.0 failed: %s", stderr)

	v016Linux, v016Darwin := pluginReg.publishPlugin("provider", "directory", "0.1.6")
	v020RCLinux, _ := pluginReg.publishPlugin("provider", "directory", "0.2.0-rc.1")
	authV011Linux, _ := pluginReg.publishPlugin("auth-handler", "acme-auth", "0.1.1")

	// 1. Dry-run with -o json previews all 3 upgrades on stdout while routing human text to stderr.
	stdout, stderr, exitCode = runScafctlWithEnvInDir(t, solDir, consumerEnv,
		"vendor", "update", "--dry-run", "-o", "json")
	require.Equal(t, 0, exitCode, "dry-run -o json failed: out=%s err=%s", stdout, stderr)
	assert.Contains(t, stderr, "Checking vendored dependencies")
	assert.True(t, json.Valid([]byte(strings.TrimSpace(stdout))), "stdout must be clean JSON: %s", stdout)

	var dryReport bundler.VendorUpdateResult
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(stdout)), &dryReport))
	assert.True(t, dryReport.DryRun)
	assert.Equal(t, 3, dryReport.UpdateCount)
	require.Len(t, dryReport.Entries, 1)
	assert.Equal(t, "1.0.0", dryReport.Entries[0].LockedVersion)
	assert.Equal(t, "1.1.0", dryReport.Entries[0].LatestVersion)
	require.Len(t, dryReport.PluginEntries, 2)
	assert.Equal(t, "0.1.5", dryReport.PluginEntries[0].LockedVersion)
	assert.Equal(t, "0.1.6", dryReport.PluginEntries[0].LatestVersion)
	assert.Equal(t, "0.1.0", dryReport.PluginEntries[1].LockedVersion)
	assert.Equal(t, "0.1.1", dryReport.PluginEntries[1].LatestVersion)
	assert.NoFileExists(t, filepath.Join(solDir, ".scafctl", "vendor", "child-sol@1.1.0.yaml"), "dry-run must not write vendor files")

	// 2. Selective --dependency directory updates ONLY the directory provider, leaving acme-auth at 0.1.0 and child-sol at 1.0.0.
	stdout, stderr, exitCode = runScafctlWithEnvInDir(t, solDir, consumerEnv,
		"vendor", "update", "--dependency", "directory")
	require.Equal(t, 0, exitCode, "selective plugin update failed: out=%s err=%s", stdout, stderr)

	afterPluginOnly, err := bundler.LoadLockFile(filepath.Join(solDir, "solution.lock"))
	require.NoError(t, err)
	require.Len(t, afterPluginOnly.Plugins, 2)
	assert.Equal(t, "0.1.6", afterPluginOnly.Plugins[0].Version)
	assert.Equal(t, "^0.1.0", afterPluginOnly.Plugins[0].Constraint)
	assert.Equal(t, v016Linux, afterPluginOnly.Plugins[0].Digests["linux/amd64"])
	assert.Equal(t, v016Darwin, afterPluginOnly.Plugins[0].Digests["darwin/arm64"])
	assert.Equal(t, "0.1.0", afterPluginOnly.Plugins[1].Version, "unselected auth-handler stays at 0.1.0")
	require.Len(t, afterPluginOnly.Dependencies, 1)
	assert.Equal(t, "1.0.0", afterPluginOnly.Dependencies[0].ResolvedVersion, "unselected sub-solution stays at 1.0.0")
	assert.FileExists(t, filepath.Join(solDir, ".scafctl", "vendor", "child-sol@1.0.0.yaml"))

	// 3. Full vendor update upgrades child-sol@1.0.0 -> 1.1.0 + acme-auth@0.1.0 -> 0.1.1 and prunes superseded vendor file.
	stdout, stderr, exitCode = runScafctlWithEnvInDir(t, solDir, consumerEnv,
		"vendor", "update")
	require.Equal(t, 0, exitCode, "full vendor update failed: out=%s err=%s", stdout, stderr)
	assert.FileExists(t, filepath.Join(solDir, ".scafctl", "vendor", "child-sol@1.1.0.yaml"))
	assert.NoFileExists(t, filepath.Join(solDir, ".scafctl", "vendor", "child-sol@1.0.0.yaml"), "superseded vendor file must be pruned")

	afterFull, err := bundler.LoadLockFile(filepath.Join(solDir, "solution.lock"))
	require.NoError(t, err)
	require.Len(t, afterFull.Dependencies, 1)
	assert.Equal(t, "1.1.0", afterFull.Dependencies[0].ResolvedVersion)
	assert.Equal(t, "^1.0.0", afterFull.Dependencies[0].Constraint)
	assert.Equal(t, "solutions-remote", afterFull.Dependencies[0].ResolvedFrom)
	require.Len(t, afterFull.Plugins, 2)
	assert.Equal(t, "0.1.1", afterFull.Plugins[1].Version)
	assert.Equal(t, authV011Linux, afterFull.Plugins[1].Digests["linux/amd64"])

	// 4. Re-running when everything is up to date skips rewriting solution.lock.
	lockPath := filepath.Join(solDir, "solution.lock")
	past := time.Now().Add(-5 * time.Minute).Truncate(time.Second)
	require.NoError(t, os.Chtimes(lockPath, past, past))
	statBefore, err := os.Stat(lockPath)
	require.NoError(t, err)

	stdout, stderr, exitCode = runScafctlWithEnvInDir(t, solDir, consumerEnv, "vendor", "update")
	require.Equal(t, 0, exitCode, "no-op vendor update failed: out=%s err=%s", stdout, stderr)
	assert.Contains(t, stdout, "All dependencies are up to date")
	statAfter, err := os.Stat(lockPath)
	require.NoError(t, err)
	assert.Equal(t, statBefore.ModTime(), statAfter.ModTime(), "no-op vendor update must not rewrite solution.lock")

	// 5. Switching plugin version to 'latest': without --pre-release stays on 0.1.6;
	// with --pre-release picks 0.2.0-rc.1.
	latestParentYAML := strings.Replace(parentYAML, "version: ^0.1.0", "version: latest", 1)
	require.NoError(t, os.WriteFile(filepath.Join(solDir, "solution.yaml"), []byte(latestParentYAML), 0o600))

	stdout, stderr, exitCode = runScafctlWithEnvInDir(t, solDir, consumerEnv,
		"vendor", "update", "--dependency", "provider/directory")
	require.Equal(t, 0, exitCode, "latest stable update failed: out=%s err=%s", stdout, stderr)
	afterStableLatest, err := bundler.LoadLockFile(lockPath)
	require.NoError(t, err)
	require.Len(t, afterStableLatest.Plugins, 2)
	assert.Equal(t, "0.1.6", afterStableLatest.Plugins[0].Version, "latest without --pre-release must stay on stable 0.1.6")
	assert.Equal(t, "latest", afterStableLatest.Plugins[0].Constraint)
	assert.Equal(t, "0.1.1", afterStableLatest.Plugins[1].Version)

	stdout, stderr, exitCode = runScafctlWithEnvInDir(t, solDir, consumerEnv,
		"vendor", "update", "--dependency", "provider/directory", "--pre-release", "-o", "yaml")
	require.Equal(t, 0, exitCode, "pre-release update failed: out=%s err=%s", stdout, stderr)
	assert.Contains(t, stdout, "latestVersion: 0.2.0-rc.1")

	afterPre, err := bundler.LoadLockFile(lockPath)
	require.NoError(t, err)
	require.Len(t, afterPre.Plugins, 2)
	assert.Equal(t, "0.2.0-rc.1", afterPre.Plugins[0].Version)
	assert.Equal(t, v020RCLinux, afterPre.Plugins[0].Digests["linux/amd64"])
	assert.Equal(t, "0.1.1", afterPre.Plugins[1].Version)

	// 6. Changing sub-solution source to '@latest' ('source: child-sol@latest')
	// resolves the latest published stable version (1.2.0) and prunes 1.1.0.
	childV120 := strings.ReplaceAll(childV100, "1.0.0", "1.2.0")
	require.NoError(t, os.WriteFile(filepath.Join(pubDir, "solution.yaml"), []byte(childV120), 0o600))
	_, stderr, exitCode = runScafctlWithEnvInDir(t, pubDir, pubEnv,
		"package", "solution", "-f", "solution.yaml", "--version", "1.2.0", "--skip-tests")
	require.Equal(t, 0, exitCode, "package child 1.2.0 failed: %s", stderr)
	_, stderr, exitCode = runScafctlWithEnv(t, pubEnv,
		"catalog", "push", "solution", "child-sol@1.2.0",
		"--catalog", solRegistryAddr+"/scafctl", "--insecure")
	require.Equal(t, 0, exitCode, "push child 1.2.0 failed: %s", stderr)

	latestChildYAML := strings.Replace(latestParentYAML, "source: child-sol@^1.0.0", "source: child-sol@latest", 1)
	require.NoError(t, os.WriteFile(filepath.Join(solDir, "solution.yaml"), []byte(latestChildYAML), 0o600))

	stdout, stderr, exitCode = runScafctlWithEnvInDir(t, solDir, consumerEnv,
		"vendor", "update", "--dependency", "child-sol")
	require.Equal(t, 0, exitCode, "latest catalog source update failed: out=%s err=%s", stdout, stderr)
	assert.FileExists(t, filepath.Join(solDir, ".scafctl", "vendor", "child-sol@1.2.0.yaml"))
	assert.NoFileExists(t, filepath.Join(solDir, ".scafctl", "vendor", "child-sol@1.1.0.yaml"))

	afterLatestRef, err := bundler.LoadLockFile(lockPath)
	require.NoError(t, err)
	require.Len(t, afterLatestRef.Dependencies, 1)
	assert.Equal(t, "child-sol@latest", afterLatestRef.Dependencies[0].Ref)
	assert.Equal(t, "1.2.0", afterLatestRef.Dependencies[0].ResolvedVersion)
	assert.Equal(t, "latest", afterLatestRef.Dependencies[0].Constraint)

	// 7. End-to-end --lock-only -> package solution materializes the exact locked remote version
	// even when .scafctl/vendor/ has no 1.3.0 file on disk and 1.4.0 is published later!
	childV130 := strings.ReplaceAll(childV100, "1.0.0", "1.3.0")
	require.NoError(t, os.WriteFile(filepath.Join(pubDir, "solution.yaml"), []byte(childV130), 0o600))
	_, stderr, exitCode = runScafctlWithEnvInDir(t, pubDir, pubEnv,
		"package", "solution", "-f", "solution.yaml", "--version", "1.3.0", "--skip-tests")
	require.Equal(t, 0, exitCode, "package child 1.3.0 failed: %s", stderr)
	_, stderr, exitCode = runScafctlWithEnv(t, pubEnv,
		"catalog", "push", "solution", "child-sol@1.3.0",
		"--catalog", solRegistryAddr+"/scafctl", "--insecure")
	require.Equal(t, 0, exitCode, "push child 1.3.0 failed: %s", stderr)

	// Constrain back to ^1.0.0 in a clean consumer directory with no local pull-through cache.
	lockOnlyConsumerRoot := t.TempDir()
	lockOnlyCfgDir := filepath.Join(lockOnlyConsumerRoot, "config", "scafctl")
	require.NoError(t, os.MkdirAll(lockOnlyCfgDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(lockOnlyCfgDir, "config.yaml"), []byte(cfgYAML), 0o600))
	lockOnlyEnv := map[string]string{
		"XDG_CONFIG_HOME": filepath.Join(lockOnlyConsumerRoot, "config"),
		"XDG_DATA_HOME":   filepath.Join(lockOnlyConsumerRoot, "data"),
		"XDG_CACHE_HOME":  filepath.Join(lockOnlyConsumerRoot, "cache"),
	}

	lockOnlyDir := t.TempDir()
	lockOnlySolYAML := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: lock-only-app
  version: 0.2.0
spec:
  resolvers:
    sub:
      resolve:
        with:
          - provider: solution
            inputs:
              source: child-sol@^1.0.0
`
	require.NoError(t, os.WriteFile(filepath.Join(lockOnlyDir, "solution.yaml"), []byte(lockOnlySolYAML), 0o600))
	require.NoError(t, bundler.WriteLockFile(filepath.Join(lockOnlyDir, "solution.lock"), &bundler.LockFile{
		Version: bundler.LockFileVersion,
		Dependencies: []bundler.LockDependency{
			{
				Ref:             "child-sol@^1.0.0",
				ResolvedVersion: "1.0.0",
				Constraint:      "^1.0.0",
				Digest:          "sha256:old",
				ResolvedFrom:    "solutions-remote",
				VendoredAt:      ".scafctl/vendor/child-sol@1.0.0.yaml",
			},
		},
	}))

	stdout, stderr, exitCode = runScafctlWithEnvInDir(t, lockOnlyDir, lockOnlyEnv,
		"vendor", "update", "--lock-only")
	require.Equal(t, 0, exitCode, "vendor update --lock-only failed: out=%s err=%s", stdout, stderr)
	assert.NoFileExists(t, filepath.Join(lockOnlyDir, ".scafctl", "vendor", "child-sol@1.3.0.yaml"), "--lock-only must not write vendor files")

	// Publish 1.4.0 to remote registry AFTER --lock-only locked 1.3.0, and wipe local XDG data cache
	// so package solution must materialize pinned 1.3.0 straight from the remote OCI catalog!
	childV140 := strings.ReplaceAll(childV100, "1.0.0", "1.4.0")
	require.NoError(t, os.WriteFile(filepath.Join(pubDir, "solution.yaml"), []byte(childV140), 0o600))
	_, stderr, exitCode = runScafctlWithEnvInDir(t, pubDir, pubEnv,
		"package", "solution", "-f", "solution.yaml", "--version", "1.4.0", "--skip-tests")
	require.Equal(t, 0, exitCode, "package child 1.4.0 failed: %s", stderr)
	_, stderr, exitCode = runScafctlWithEnv(t, pubEnv,
		"catalog", "push", "solution", "child-sol@1.4.0",
		"--catalog", solRegistryAddr+"/scafctl", "--insecure")
	require.Equal(t, 0, exitCode, "push child 1.4.0 failed: %s", stderr)

	freshBuildEnv := map[string]string{
		"XDG_CONFIG_HOME": filepath.Join(lockOnlyConsumerRoot, "config"),
		"XDG_DATA_HOME":   filepath.Join(t.TempDir(), "data"),
		"XDG_CACHE_HOME":  filepath.Join(t.TempDir(), "cache"),
	}
	stdout, stderr, exitCode = runScafctlWithEnvInDir(t, lockOnlyDir, freshBuildEnv,
		"package", "solution", "-f", "solution.yaml", "--version", "0.2.0", "--skip-tests", "--no-verify")
	require.Equal(t, 0, exitCode, "package solution after --lock-only failed: out=%s err=%s", stdout, stderr)
	assert.FileExists(t, filepath.Join(lockOnlyDir, ".scafctl", "vendor", "child-sol@1.3.0.yaml"), "package solution must materialize pinned 1.3.0")
	assert.NoFileExists(t, filepath.Join(lockOnlyDir, ".scafctl", "vendor", "child-sol@1.4.0.yaml"), "package solution must replay locked 1.3.0 rather than drifting to 1.4.0")
	afterPkgLock, err := bundler.LoadLockFile(filepath.Join(lockOnlyDir, "solution.lock"))
	require.NoError(t, err)
	require.Len(t, afterPkgLock.Dependencies, 1)
	assert.Equal(t, "1.3.0", afterPkgLock.Dependencies[0].ResolvedVersion)
}

// TestIntegration_VendorUpdate_MissingLockAndEmbedderBinaryName verifies that
// missing solution.lock points users to '<binary> package solution' for both
// scafctl and an embedder binary ('mycli').
func TestIntegration_VendorUpdate_MissingLockAndEmbedderBinaryName(t *testing.T) {
	t.Parallel()

	solDir := t.TempDir()
	solYAML := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: embedder-vendor-hint
  version: 0.1.0
spec: {}
`
	require.NoError(t, os.WriteFile(filepath.Join(solDir, "solution.yaml"), []byte(solYAML), 0o600))

	env := isolatedCatalogEnv(t)
	helpOut, helpErr, helpCode := runScafctlWithEnvInDir(t, solDir, env, "vendor", "update", "--help")
	assert.Equal(t, 0, helpCode, "vendor update --help failed: %s", helpErr)
	assert.Contains(t, helpOut, "--output")
	assert.Contains(t, helpOut, "-o")

	_, stderr, exitCode := runScafctlWithEnvInDir(t, solDir, env, "vendor", "update", "-f", "solution.yaml")
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "run 'scafctl package solution' first")
	assert.NotContains(t, stderr, "build solution")

	testBin, err := os.Executable()
	require.NoError(t, err)
	xdgRoot := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, testBin, "--", "vendor", "update", "-f", "solution.yaml")
	cmd.Dir = solDir
	cmd.Env = append(os.Environ(),
		"SCAFCTL_TEST_EMBEDDER_HELPER=1",
		"XDG_CONFIG_HOME="+filepath.Join(xdgRoot, "config"),
		"XDG_DATA_HOME="+filepath.Join(xdgRoot, "data"),
		"XDG_CACHE_HOME="+filepath.Join(xdgRoot, "cache"),
	)
	out, runErr := cmd.CombinedOutput()
	assert.Error(t, runErr)
	assert.Contains(t, string(out), "run 'mycli package solution' first")
}
