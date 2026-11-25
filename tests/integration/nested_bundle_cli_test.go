// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func assertNoLeakedBundleTempDirs(t *testing.T, tmpRoot string) {
	t.Helper()
	entries, err := os.ReadDir(tmpRoot)
	require.NoError(t, err)
	for _, entry := range entries {
		assert.NotContains(t, entry.Name(), "-bundle-",
			"ephemeral bundle extraction directory must be cleaned up: %s", entry.Name())
	}
}

func isolatedSubprocessEnv(catalogHome, tmpRoot string) map[string]string {
	return map[string]string{
		"XDG_DATA_HOME":  catalogHome,
		"XDG_CACHE_HOME": catalogHome,
		"TMPDIR":         tmpRoot,
		"TEMP":           tmpRoot,
		"TMP":            tmpRoot,
	}
}

func TestIntegration_NestedBundle_LocalAndThreeLevelPaths(t *testing.T) {
	t.Parallel()

	projectRoot := findProjectRoot()

	t.Run("nested-bundle example resolves child template without parent duplicate", func(t *testing.T) {
		t.Parallel()
		workDir := t.TempDir()
		examplePath := filepath.Join(projectRoot, "examples/solutions/nested-bundle/parent.yaml")

		// Confirm the duplicate template at parent level is absent so the child
		// must read from examples/solutions/nested-bundle/sub/templates/greeting.tmpl.
		assert.NoFileExists(t, filepath.Join(projectRoot, "examples/solutions/nested-bundle/templates/greeting.tmpl"))

		stdout, stderr, exitCode := runScafctlInDir(t, workDir,
			"run", "resolver", "-f", examplePath, "-o", "json",
		)
		require.Equalf(t, 0, exitCode, "run resolver failed: %s", stderr)

		var out map[string]any
		require.NoError(t, json.Unmarshal([]byte(stdout), &out))

		childData, ok := out["child_data"].(map[string]any)
		require.True(t, ok, "expected child_data map in output")
		assert.Equal(t, "success", childData["status"])

		childResolvers, ok := childData["resolvers"].(map[string]any)
		require.True(t, ok, "expected resolvers map in child_data")
		tmplObj, ok := childResolvers["greeting_template"].(map[string]any)
		require.True(t, ok, "expected greeting_template map")
		assert.Contains(t, tmplObj["content"], "Hello, {{ .Name }}!")
	})

	t.Run("nested-bundle functional suite passes including 3-level grandchild", func(t *testing.T) {
		t.Parallel()
		suitePath := filepath.Join(projectRoot, "tests/integration/solutions/nested-bundle/solution.yaml")
		stdout, stderr, exitCode := runScafctlLong(t,
			"test", "functional", "-f", suitePath,
		)
		require.Equalf(t, 0, exitCode, "functional suite failed:\nstdout: %s\nstderr: %s", stdout, stderr)
	})

	t.Run("local child workflow action reads relativeTo solution with and without output-dir", func(t *testing.T) {
		t.Parallel()
		rootDir := t.TempDir()
		childDir := filepath.Join(rootDir, "sub")
		require.NoError(t, os.MkdirAll(filepath.Join(childDir, "templates"), 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(childDir, "templates", "banner.tmpl"), []byte("local-action-banner"), 0o600))

		childYAML := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: local-action-child
  version: 1.0.0
spec:
  workflow:
    actions:
      check-banner:
        provider: file
        inputs:
          operation: exists
          path: "templates/banner.tmpl"
          relativeTo: solution
      read-banner:
        provider: file
        dependsOn: [check-banner]
        inputs:
          operation: read
          path: "templates/banner.tmpl"
          relativeTo: solution
      write-banner:
        provider: file
        dependsOn: [read-banner]
        inputs:
          operation: write
          path: "rendered/banner.txt"
          content:
            expr: "__actions['read-banner'].results.content + '|exists=' + string(__actions['check-banner'].results.exists)"
`
		require.NoError(t, os.WriteFile(filepath.Join(childDir, "child.yaml"), []byte(childYAML), 0o600))

		parentYAML := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: local-action-parent
  version: 1.0.0
spec:
  workflow:
    actions:
      run-child:
        provider: solution
        inputs:
          source: "./sub/child.yaml"
`
		parentPath := filepath.Join(rootDir, "parent.yaml")
		require.NoError(t, os.WriteFile(parentPath, []byte(parentYAML), 0o600))

		workDir := t.TempDir()
		_, stderr, code := runScafctlInDir(t, workDir, "run", "solution", "-f", parentPath)
		require.Equalf(t, 0, code, "local child action read without --output-dir failed: %s", stderr)
		gotCwd, err := os.ReadFile(filepath.Join(workDir, "rendered", "banner.txt"))
		require.NoError(t, err)
		assert.Equal(t, "local-action-banner|exists=true", string(gotCwd))

		outDir := t.TempDir()
		_, stderr, code = runScafctlInDir(t, workDir, "run", "solution", "-f", parentPath, "--output-dir", outDir)
		require.Equalf(t, 0, code, "local child action read with --output-dir failed: %s", stderr)
		gotOut, err := os.ReadFile(filepath.Join(outDir, "rendered", "banner.txt"))
		require.NoError(t, err)
		assert.Equal(t, "local-action-banner|exists=true", string(gotOut))
	})
}

func TestIntegration_CatalogChild_BundleReadsAndParentOutputWrites(t *testing.T) {
	t.Parallel()

	catalogHome := t.TempDir()
	buildTmp := t.TempDir()

	// Stage a reusable catalog child (with its own template and a bundled
	// 3rd-level grandchild) in a temporary build directory, build it into the
	// isolated local catalog, and then remove the source files from disk so
	// runtime execution can only succeed by extracting the catalog bundle.
	childSrcDir := filepath.Join(t.TempDir(), "child-src")
	require.NoError(t, os.MkdirAll(filepath.Join(childSrcDir, "templates"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(childSrcDir, "grand", "templates"), 0o750))

	require.NoError(t, os.WriteFile(
		filepath.Join(childSrcDir, "templates", "banner.tmpl"),
		[]byte("Service={{ .service }} Env={{ .env }}"),
		0o600,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(childSrcDir, "templates", "action-note.txt"),
		[]byte("action-bundled-note"),
		0o600,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(childSrcDir, "grand", "templates", "footer.txt"),
		[]byte("grandchild-bundled-footer"),
		0o600,
	))

	grandchildYAML := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: reusable-grandchild
  version: 1.0.0
spec:
  resolvers:
    footer:
      type: string
      resolve:
        with:
          - provider: file
            inputs:
              operation: read
              path: "templates/footer.txt"
      transform:
        with:
          - provider: cel
            inputs:
              expression: "__self.content"
`
	require.NoError(t, os.WriteFile(filepath.Join(childSrcDir, "grand", "grandchild.yaml"), []byte(grandchildYAML), 0o600))

	childYAML := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: reusable-child
  version: 1.0.0
spec:
  resolvers:
    service:
      type: string
      resolve:
        with:
          - provider: parameter
            inputs:
              key: service
              default: "billing"
    env:
      type: string
      resolve:
        with:
          - provider: parameter
            inputs:
              key: env
              default: "prod"
    outPath:
      type: string
      resolve:
        with:
          - provider: parameter
            inputs:
              key: outPath
              default: "generated/manifest.txt"
    rawTemplate:
      type: string
      resolve:
        with:
          - provider: file
            inputs:
              operation: read
              path: "templates/banner.tmpl"
      transform:
        with:
          - provider: cel
            inputs:
              expression: "__self.content"
    grandData:
      type: any
      resolve:
        with:
          - provider: solution
            inputs:
              source: "./grand/grandchild.yaml"
    rendered:
      type: string
      dependsOn: [rawTemplate, service, env, grandData]
      resolve:
        with:
          - provider: go-template
            inputs:
              name: banner
              template:
                rslvr: rawTemplate
      transform:
        with:
          - provider: cel
            inputs:
              expression: "__self + ' | ' + _.grandData.resolvers.footer"
  workflow:
    actions:
      check-note:
        provider: file
        inputs:
          operation: exists
          path: "templates/action-note.txt"
          relativeTo: solution
      read-note:
        provider: file
        dependsOn: [check-note]
        inputs:
          operation: read
          path: "templates/action-note.txt"
          relativeTo: solution
      write-manifest:
        provider: file
        dependsOn: [read-note]
        inputs:
          operation: write
          path:
            rslvr: outPath
          content:
            expr: "_.rendered + ' | ' + __actions['read-note'].results.content + ' | exists=' + string(__actions['check-note'].results.exists)"
`
	require.NoError(t, os.WriteFile(filepath.Join(childSrcDir, "solution.yaml"), []byte(childYAML), 0o600))

	_, buildStderr, buildCode := runScafctlWithEnvInDir(t, childSrcDir, isolatedSubprocessEnv(catalogHome, buildTmp),
		"build", "solution", "-f", "solution.yaml", "--version", "1.0.0", "--force", "--skip-tests", "--no-cache",
	)
	require.Equalf(t, 0, buildCode, "building catalog child failed: %s", buildStderr)
	require.NoError(t, os.RemoveAll(childSrcDir))

	// Author parent solutions in a separate directory with no templates at all.
	parentDir := t.TempDir()
	parentResolverYAML := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: parent-from-mode
  version: 1.0.0
spec:
  resolvers:
    child:
      type: any
      resolve:
        with:
          - provider: solution
            inputs:
              source: "reusable-child@1.0.0"
              inputs:
                service: "orders"
                env: "staging"
`
	parentResolverPath := filepath.Join(parentDir, "parent-resolver.yaml")
	require.NoError(t, os.WriteFile(parentResolverPath, []byte(parentResolverYAML), 0o600))

	parentWorkflowYAML := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: parent-action-mode
  version: 1.0.0
spec:
  resolvers:
    targetPath:
      type: string
      resolve:
        with:
          - provider: parameter
            inputs:
              key: targetPath
              default: "generated/manifest.txt"
  workflow:
    actions:
      invoke-child:
        provider: solution
        inputs:
          source: "reusable-child@1.0.0"
          inputs:
            service: "payments"
            env: "prod"
            outPath:
              rslvr: targetPath
`
	parentWorkflowPath := filepath.Join(parentDir, "parent-workflow.yaml")
	require.NoError(t, os.WriteFile(parentWorkflowPath, []byte(parentWorkflowYAML), 0o600))

	t.Run("resolver mode extracts catalog child bundle and cleans up temp dir", func(t *testing.T) {
		t.Parallel()
		runTmp := t.TempDir()
		workDir := t.TempDir()

		stdout, stderr, exitCode := runScafctlWithEnvInDir(t, workDir, isolatedSubprocessEnv(catalogHome, runTmp),
			"run", "resolver", "-f", parentResolverPath, "-o", "json",
		)
		require.Equalf(t, 0, exitCode, "run resolver failed: %s", stderr)

		var out map[string]any
		require.NoError(t, json.Unmarshal([]byte(stdout), &out))

		childEnv, ok := out["child"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "success", childEnv["status"])

		childRes, ok := childEnv["resolvers"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "Service=orders Env=staging | grandchild-bundled-footer", childRes["rendered"])

		assertNoLeakedBundleTempDirs(t, runTmp)
	})

	t.Run("action mode writes to caller CWD when output-dir is unset", func(t *testing.T) {
		t.Parallel()
		runTmp := t.TempDir()
		workDir := t.TempDir()

		_, stderr, exitCode := runScafctlWithEnvInDir(t, workDir, isolatedSubprocessEnv(catalogHome, runTmp),
			"run", "solution", "-f", parentWorkflowPath,
		)
		require.Equalf(t, 0, exitCode, "run solution failed: %s", stderr)

		written, err := os.ReadFile(filepath.Join(workDir, "generated", "manifest.txt"))
		require.NoError(t, err)
		assert.Equal(t, "Service=payments Env=prod | grandchild-bundled-footer | action-bundled-note | exists=true", string(written))
		assert.NoFileExists(t, filepath.Join(parentDir, "generated", "manifest.txt"))

		assertNoLeakedBundleTempDirs(t, runTmp)
	})

	t.Run("action mode writes to parent output-dir and enforces containment", func(t *testing.T) {
		t.Parallel()
		runTmp := t.TempDir()
		workDir := t.TempDir()
		outputDir := t.TempDir()

		_, stderr, exitCode := runScafctlWithEnvInDir(t, workDir, isolatedSubprocessEnv(catalogHome, runTmp),
			"run", "solution", "-f", parentWorkflowPath, "--output-dir", outputDir,
		)
		require.Equalf(t, 0, exitCode, "run solution with --output-dir failed: %s", stderr)

		written, err := os.ReadFile(filepath.Join(outputDir, "generated", "manifest.txt"))
		require.NoError(t, err)
		assert.Equal(t, "Service=payments Env=prod | grandchild-bundled-footer | action-bundled-note | exists=true", string(written))
		assert.NoFileExists(t, filepath.Join(workDir, "generated", "manifest.txt"))
		assertNoLeakedBundleTempDirs(t, runTmp)

		// Escape attempt outside --output-dir must fail containment and still clean up temp dirs.
		escapeTmp := t.TempDir()
		escapeOutDir := filepath.Join(t.TempDir(), "sandbox")
		_, escapeStderr, escapeCode := runScafctlWithEnvInDir(t, workDir, isolatedSubprocessEnv(catalogHome, escapeTmp),
			"run", "solution", "-f", parentWorkflowPath,
			"--output-dir", escapeOutDir,
			"-r", "targetPath=../escaped.txt",
		)
		assert.NotEqual(t, 0, escapeCode, "path traversal outside --output-dir must fail")
		assert.True(t,
			strings.Contains(escapeStderr, "outside output directory") || strings.Contains(escapeStderr, "escapes base directory"),
			"expected containment error in stderr, got: %s", escapeStderr,
		)
		assert.NoFileExists(t, filepath.Join(filepath.Dir(escapeOutDir), "escaped.txt"))
		assertNoLeakedBundleTempDirs(t, escapeTmp)
	})
}
