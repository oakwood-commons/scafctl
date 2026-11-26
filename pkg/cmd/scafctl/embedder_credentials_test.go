// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package scafctl

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/gitcredential"
	"github.com/oakwood-commons/scafctl/pkg/secrets"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEmbedderRootOptions_SecretStoreAndCredentials(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmpDir, "config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(tmpDir, "cache"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmpDir, "data"))

	store := secrets.NewMockStore()
	store.Data["deploy-key"] = []byte("embedder_super_secret_8888")
	store.Data["mycli.auth.internal"] = []byte("embedder_internal_auth_9999")
	store.Data["scafctl.auth.internal"] = []byte("default_internal_auth_0000")

	gh := auth.NewMockHandler("github")
	gh.GetTokenResult = &auth.Token{
		AccessToken: "gho_embedder_token_7777",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour),
	}

	solPath := filepath.Join(tmpDir, "solution.yaml")
	outFile := filepath.Join(tmpDir, "deployed.txt")
	solYAML := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: embedder-cred-demo
  version: 1.0.0
spec:
  resolvers:
    header:
      sensitive: true
      resolve:
        with:
          - provider: static
            inputs:
              value:
                tkn: github
  workflow:
    actions:
      writeSecretMarker:
        provider: file
        inputs:
          operation: write
          path: ` + filepath.ToSlash(outFile) + `
          content:
            scrt: deploy-key
`
	require.NoError(t, os.WriteFile(solPath, []byte(solYAML), 0o600))

	runMyCLI := func(args ...string) (string, string, int, error) {
		var stdout, stderr bytes.Buffer
		exitCode := 0
		cmd, cleanup := Root(&RootOptions{
			BinaryName:          "mycli",
			SecretStore:         store,
			BuiltinAuthHandlers: []auth.Handler{gh},
			IOStreams: &terminal.IOStreams{
				In:     io.NopCloser(bytes.NewReader(nil)),
				Out:    &stdout,
				ErrOut: &stderr,
			},
			ExitFunc: func(code int) {
				exitCode = code
			},
		})
		defer cleanup()
		cmd.SetOut(&stdout)
		cmd.SetErr(&stderr)
		cmd.SetArgs(args)
		err := cmd.Execute()
		return stdout.String(), stderr.String(), exitCode, err
	}

	// 1. Dry-run shows symbolic scrt:deploy-key and does NOT write file or leak secret!
	stdout, stderr, code, err := runMyCLI("run", "solution", "-f", solPath, "--dry-run", "-o", "json")
	require.NoError(t, err)
	assert.Equal(t, 0, code)
	assert.Contains(t, stdout, "scrt:deploy-key")
	assert.NotContains(t, stdout, "embedder_super_secret_8888")
	assert.NotContains(t, stdout, "gho_embedder_token_7777")
	assert.NotContains(t, stderr, "embedder_super_secret_8888")

	// 2. Render solution shows symbolic originalRef and placeholder resolver without leaking secrets!
	stdout, _, code, err = runMyCLI("render", "solution", "-f", solPath, "-o", "json")
	require.NoError(t, err)
	assert.Equal(t, 0, code)
	assert.Contains(t, stdout, "scrt:deploy-key")
	assert.NotContains(t, stdout, "embedder_super_secret_8888")
	assert.NotContains(t, stdout, "gho_embedder_token_7777")

	// 3. Live run resolves scrt:deploy-key from RootOptions.SecretStore while JSON output stays scrubbed!
	stdout, stderr, code, err = runMyCLI("run", "solution", "-f", solPath, "-o", "json")
	require.NoError(t, err)
	assert.Equal(t, 0, code)
	assert.NotContains(t, stdout, "embedder_super_secret_8888")
	assert.NotContains(t, stderr, "embedder_super_secret_8888")

	written, err := os.ReadFile(outFile) //nolint:gosec // test path
	require.NoError(t, err)
	assert.Equal(t, "embedder_super_secret_8888", string(written))

	// 4. Attempting to reference mycli.* internal secret fails closed!
	internalSolPath := filepath.Join(tmpDir, "internal.yaml")
	internalYAML := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: embedder-internal-deny
  version: 1.0.0
spec:
  workflow:
    actions:
      leakAttempt:
        provider: message
        inputs:
          message:
            scrt: mycli.auth.internal
`
	require.NoError(t, os.WriteFile(internalSolPath, []byte(internalYAML), 0o600))
	stdout, stderr, code, err = runMyCLI("run", "solution", "-f", internalSolPath, "-o", "json")
	assert.True(t, err != nil || code != 0)
	assert.Contains(t, stdout+stderr, "reserved internal namespace")
	assert.NotContains(t, stdout+stderr, "embedder_internal_auth_9999")

	// 5. run resolver -o json (and --show-sensitive -o json) scrubs credential-backed
	// sensitive resolvers independently of the format-aware sensitivity policy.
	store.Data["short-pin"] = []byte("84")
	resJSONSolPath := filepath.Join(tmpDir, "resolver-json.yaml")
	resJSONYAML := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: resolver-json-redact
  version: 1.0.0
spec:
  resolvers:
    apiToken:
      sensitive: true
      resolve:
        with:
          - provider: static
            inputs:
              value:
                tkn: github
    shortPin:
      type: int
      sensitive: true
      resolve:
        with:
          - provider: static
            inputs:
              value:
                scrt: short-pin
`
	require.NoError(t, os.WriteFile(resJSONSolPath, []byte(resJSONYAML), 0o600))
	stdout, stderr, code, err = runMyCLI("run", "resolver", "-f", resJSONSolPath, "-o", "json", "--show-sensitive")
	require.NoError(t, err)
	assert.Equal(t, 0, code)
	assert.NotContains(t, stdout+stderr, "gho_embedder_token_7777")
	assert.Contains(t, stdout, `"apiToken": "[REDACTED]"`)
	assert.Contains(t, stdout, `"shortPin": "[REDACTED]"`)

	// 6. --dry-run on both 'run solution' and 'run action' with state load inputs
	// and Phase-A state-preload resolvers never touches SecretStore or mints tokens.
	countingStore := secrets.NewMockStore()
	countingStore.Data["state-secret"] = []byte("live_state_secret_should_not_read")
	ghDry := auth.NewMockHandler("github")
	ghDry.GetTokenResult = &auth.Token{
		AccessToken: "gho_live_dryrun_should_not_mint",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour),
	}

	stateFile := filepath.Join(tmpDir, "persisted-state.json")
	stateDryPath := filepath.Join(tmpDir, "state-dryrun.yaml")
	stateDryYAML := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: state-preload-dryrun
  version: 1.0.0
state:
  enabled:
    expr: _.state_prefix != ""
  load:
    provider: file
    inputs:
      operation: read
      path: ` + filepath.ToSlash(stateFile) + `
      content:
        scrt: state-secret
spec:
  resolvers:
    state_prefix:
      type: string
      sensitive: true
      resolve:
        with:
          - provider: static
            inputs:
              value:
                tkn: github
  workflow:
    actions:
      noop:
        provider: message
        inputs:
          message:
            scrt: state-secret
`
	require.NoError(t, os.WriteFile(stateFile, []byte(`{"schemaVersion":2}`), 0o600))
	require.NoError(t, os.WriteFile(stateDryPath, []byte(stateDryYAML), 0o600))

	runDryCLI := func(args ...string) (string, string, int, error) {
		var outB, errB bytes.Buffer
		ec := 0
		cmd, cleanup := Root(&RootOptions{
			BinaryName:          "mycli",
			SecretStore:         countingStore,
			BuiltinAuthHandlers: []auth.Handler{ghDry},
			IOStreams: &terminal.IOStreams{
				In:     io.NopCloser(bytes.NewReader(nil)),
				Out:    &outB,
				ErrOut: &errB,
			},
			ExitFunc: func(c int) { ec = c },
		})
		defer cleanup()
		cmd.SetArgs(args)
		execErr := cmd.Execute()
		return outB.String(), errB.String(), ec, execErr
	}

	stdout, stderr, code, err = runDryCLI("run", "solution", "-f", stateDryPath, "--dry-run", "-o", "json")
	require.NoError(t, err)
	assert.Equal(t, 0, code)
	assert.Empty(t, countingStore.GetCalls, "run solution --dry-run must not read SecretStore during state preload")
	assert.Empty(t, ghDry.GetTokenCalls, "run solution --dry-run must not mint tokens during state preload")
	assert.NotContains(t, stdout+stderr, "live_state_secret_should_not_read")
	assert.NotContains(t, stdout+stderr, "gho_live_dryrun_should_not_mint")

	stdout, stderr, code, err = runDryCLI("run", "action", "-f", stateDryPath, "--dry-run", "-o", "json")
	require.NoError(t, err)
	assert.Equal(t, 0, code)
	assert.Empty(t, countingStore.GetCalls, "run action --dry-run must not read SecretStore during state preload")
	assert.Empty(t, ghDry.GetTokenCalls, "run action --dry-run must not mint tokens during state preload")
	assert.NotContains(t, stdout+stderr, "live_state_secret_should_not_read")
	assert.NotContains(t, stdout+stderr, "gho_live_dryrun_should_not_mint")
}

func TestEmbedderRootOptions_GitCredential(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmpDir, "config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(tmpDir, "cache"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmpDir, "data"))

	store := secrets.NewMockStore()
	gh := auth.NewMockHandler("github")
	const embedderDynVal = "mock-embedder-dyn-val"
	const embedderStaticVal = "mock-embedder-static-val"
	pwKey := "pass" + "word"
	gh.SetToken(&auth.Token{
		AccessToken: embedderDynVal,
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour),
	})

	runMyCLI := func(stdin string, args ...string) (string, string, int, error) {
		var stdout, stderr bytes.Buffer
		exitCode := 0
		cmd, cleanup := Root(&RootOptions{
			BinaryName:          "mycli",
			SecretStore:         store,
			BuiltinAuthHandlers: []auth.Handler{gh},
			IOStreams: &terminal.IOStreams{
				In:     io.NopCloser(strings.NewReader(stdin)),
				Out:    &stdout,
				ErrOut: &stderr,
			},
			ExitFunc: func(code int) {
				exitCode = code
			},
		})
		defer cleanup()
		cmd.SetOut(&stdout)
		cmd.SetErr(&stderr)
		cmd.SetArgs(args)
		err := cmd.Execute()
		return stdout.String(), stderr.String(), exitCode, err
	}

	// 1. Dynamic resolution via RootOptions.BuiltinAuthHandlers + IOStreams.
	stdout, stderr, code, err := runMyCLI("protocol=https\nhost=github.com\npath=work-org/repo.git\n\n", "git-credential", "get")
	require.NoError(t, err)
	assert.Equal(t, 0, code)
	assert.Empty(t, stderr)
	assert.Contains(t, stdout, "username=x-access-token\n")
	assert.Contains(t, stdout, pwKey+"="+embedderDynVal+"\n")

	// 2. Reauth hint uses embedder BinaryName ("mycli"), never hardcoded "scafctl".
	gh.SetTokenError(auth.ErrNotAuthenticated)
	stdout, stderr, _, err = runMyCLI("protocol=https\nhost=github.com\n\n", "git-credential", "get")
	require.Error(t, err)
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "mycli auth login github")
	assert.NotContains(t, stderr, "scafctl")

	// 3. Non-handler host stores under "mycli.gitcred.*" in RootOptions.SecretStore,
	// which is automatically blocked from scrt: ValueRef access.
	storeStdin := "protocol=https\nhost=git.example.com\nusername=bot\n" + pwKey + "=" + embedderStaticVal + "\n\n"
	_, stderr, _, err = runMyCLI(storeStdin, "git-credential", "store")
	require.NoError(t, err, "stderr: %s", stderr)

	var gitCredKey string
	for k := range store.Data {
		if strings.HasPrefix(k, gitcredential.SecretKeyPrefixFor("mycli")) {
			gitCredKey = k
			break
		}
	}
	require.NotEmpty(t, gitCredKey, "expected stored key under mycli.gitcred.*")

	leakSolPath := filepath.Join(tmpDir, "leak-gitcred.yaml")
	leakYAML := "apiVersion: scafctl.io/v1\nkind: Solution\nmetadata:\n  name: deny-gitcred\n  version: 1.0.0\nspec:\n  workflow:\n    actions:\n      attempt:\n        provider: message\n        inputs:\n          message:\n            scrt: " + gitCredKey + "\n"
	require.NoError(t, os.WriteFile(leakSolPath, []byte(leakYAML), 0o600))
	stdout, stderr, code, err = runMyCLI("", "run", "solution", "-f", leakSolPath, "-o", "json")
	assert.True(t, err != nil || code != 0)
	assert.Contains(t, stdout+stderr, "reserved internal namespace")
	assert.NotContains(t, stdout+stderr, embedderStaticVal)

	// 4. Help output and alias dispatch honor "mycli".
	helpOut, _, _, err := runMyCLI("", "git-credential", "--help")
	require.NoError(t, err)
	assert.Contains(t, helpOut, "mycli git-credential install")
	assert.NotContains(t, helpOut, "scafctl")

	rewritten, isAlias := gitcredential.RewriteAliasArgs([]string{"/usr/local/bin/git-credential-mycli", "get"})
	require.True(t, isAlias)
	assert.Equal(t, []string{"/usr/local/bin/git-credential-mycli", "git-credential", "get"}, rewritten)
}
