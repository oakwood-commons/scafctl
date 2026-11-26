// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ensureGitHTTPBackendAvailable(t *testing.T) {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), "git", "--exec-path").Output()
	if err != nil {
		t.Skipf("git --exec-path unavailable: %v", err)
	}
	candidate := filepath.Join(strings.TrimSpace(string(out)), "git-http-backend")
	if runtime.GOOS == "windows" {
		candidate += ".exe"
	}
	if _, statErr := os.Stat(candidate); statErr != nil {
		t.Skipf("git-http-backend not found at %s: %v", candidate, statErr)
	}
}

func serveGitHTTPBackend(w http.ResponseWriter, r *http.Request, reposRoot, cgiHome, remoteUser string) {
	cmd := exec.CommandContext(r.Context(), "git", "http-backend")
	cmd.Stdin = r.Body
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"SYSTEMROOT=" + os.Getenv("SYSTEMROOT"),
		"GIT_PROJECT_ROOT=" + reposRoot,
		"GIT_HTTP_EXPORT_ALL=1",
		"GIT_CONFIG_NOSYSTEM=1",
		"HOME=" + cgiHome,
		"USERPROFILE=" + cgiHome,
		"REQUEST_METHOD=" + r.Method,
		"PATH_INFO=" + r.URL.Path,
		"QUERY_STRING=" + r.URL.RawQuery,
		"CONTENT_TYPE=" + r.Header.Get("Content-Type"),
		"CONTENT_LENGTH=" + r.Header.Get("Content-Length"),
		"HTTP_CONTENT_ENCODING=" + r.Header.Get("Content-Encoding"),
		"HTTP_GIT_PROTOCOL=" + r.Header.Get("Git-Protocol"),
		"REMOTE_USER=" + remoteUser,
		"REMOTE_ADDR=127.0.0.1",
	}
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		http.Error(w, errBuf.String(), http.StatusInternalServerError)
		return
	}

	br := bufio.NewReader(&outBuf)
	statusCode := http.StatusOK
	for {
		line, err := br.ReadString('\n')
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if k, v, ok := strings.Cut(line, ":"); ok {
			key := strings.TrimSpace(k)
			val := strings.TrimSpace(v)
			if strings.EqualFold(key, "Status") {
				fields := strings.Fields(val)
				if len(fields) > 0 {
					if parsed, parseErr := strconv.Atoi(fields[0]); parseErr == nil {
						statusCode = parsed
					}
				}
			} else {
				w.Header().Add(key, val)
			}
		}
		if err != nil {
			break
		}
	}
	w.WriteHeader(statusCode)
	_, _ = io.Copy(w, br)
}

func initSeededBareRepo(t *testing.T, bareDir, seedFileName, seedContent string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(bareDir), 0o700))

	runGitCmd(t, "", nil, "init", "--bare", bareDir)
	runGitCmd(t, bareDir, nil, "config", "http.receivepack", "true")
	runGitCmd(t, bareDir, nil, "symbolic-ref", "HEAD", "refs/heads/main")

	seedWork := t.TempDir()
	runGitCmd(t, seedWork, nil, "init", "-b", "main")
	runGitCmd(t, seedWork, nil, "config", "user.name", "Seed Author")
	runGitCmd(t, seedWork, nil, "config", "user.email", "seed@example.com")
	runGitCmd(t, seedWork, nil, "config", "commit.gpgsign", "false")
	require.NoError(t, os.WriteFile(filepath.Join(seedWork, seedFileName), []byte(seedContent), 0o600))
	runGitCmd(t, seedWork, nil, "add", seedFileName)
	runGitCmd(t, seedWork, nil, "commit", "-m", "chore: initial seed")
	runGitCmd(t, seedWork, nil, "push", bareDir, "HEAD:refs/heads/main")
}

func runGitCmd(t *testing.T, dir string, extraEnv map[string]string, args ...string) string {
	t.Helper()
	return runGitCmdWithStdin(t, dir, nil, extraEnv, args...)
}

func runGitCmdWithStdin(t *testing.T, dir string, stdin io.Reader, extraEnv map[string]string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	if stdin != nil {
		cmd.Stdin = stdin
	}

	envMap := make(map[string]string)
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			envMap[k] = v
		}
	}
	// Always isolate from host ~/.gitconfig, /etc/gitconfig, and credential prompts.
	delete(envMap, "GIT_ASKPASS")
	delete(envMap, "SSH_ASKPASS")
	isolatedHome := t.TempDir()
	envMap["HOME"] = isolatedHome
	envMap["USERPROFILE"] = isolatedHome
	envMap["GIT_CONFIG_NOSYSTEM"] = "1"
	envMap["GIT_CONFIG_GLOBAL"] = filepath.Join(isolatedHome, ".gitconfig")
	envMap["GIT_TERMINAL_PROMPT"] = "0"

	for k, v := range extraEnv {
		envMap[k] = v
	}
	cmd.Env = make([]string, 0, len(envMap))
	for k, v := range envMap {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	require.NoError(t, err, "git %v failed:\nstdout: %s\nstderr: %s", args, stdout.String(), stderr.String())
	return stdout.String()
}

func TestIntegration_GitCredential_CloneAndPushMultiOrgHTTPBackend(t *testing.T) {
	t.Parallel()

	ensureGitHTTPBackendAvailable(t)

	reposRoot := t.TempDir()
	workBare := filepath.Join(reposRoot, "work-org", "repo.git")
	publicBare := filepath.Join(reposRoot, "public-org", "repo.git")
	initSeededBareRepo(t, workBare, "README.md", "work-org initial\n")
	initSeededBareRepo(t, publicBare, "README.md", "public-org initial\n")

	const (
		workMarkerAuth   = "mock-work-org-bearer-alpha"
		publicMarkerAuth = "mock-public-org-bearer-beta"
	)

	cgiHome := t.TempDir()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var expectedToken string
		switch {
		case strings.HasPrefix(r.URL.Path, "/work-org/"):
			expectedToken = workMarkerAuth
		case strings.HasPrefix(r.URL.Path, "/public-org/"):
			expectedToken = publicMarkerAuth
		default:
			http.NotFound(w, r)
			return
		}

		user, pass, ok := r.BasicAuth()
		if !ok || user != "x-access-token" || pass != expectedToken {
			w.Header().Set("WWW-Authenticate", `Basic realm="Git Test Server"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		serveGitHTTPBackend(w, r, reposRoot, cgiHome, user)
	}))
	defer srv.Close()

	srvURL, err := url.Parse(srv.URL)
	require.NoError(t, err)
	loopbackHostPort := srvURL.Host

	xdgDir := t.TempDir()
	fakeHome := t.TempDir()
	gitConfigGlobal := filepath.Join(fakeHome, ".gitconfig")
	scafctlCfgDir := filepath.Join(xdgDir, "scafctl")
	require.NoError(t, os.MkdirAll(scafctlCfgDir, 0o700))

	scafctlCfg := `version: 1
catalogs:
  - name: local
    type: filesystem
settings:
  disableOfficialCatalog: true
auth:
  customOAuth2:
    - name: work-git
      defaultFlow: pat
      token: env://TEST_WORK_GIT_TOKEN
      gitUsername: x-access-token
    - name: public-git
      defaultFlow: pat
      token: env://TEST_PUBLIC_GIT_TOKEN
      gitUsername: x-access-token
  git:
    rules:
      - host: "` + loopbackHostPort + `"
        pathPrefix: work-org
        handler: work-git
      - host: "` + loopbackHostPort + `"
        pathPrefix: public-org
        handler: public-git
`
	require.NoError(t, os.WriteFile(filepath.Join(scafctlCfgDir, "config.yaml"), []byte(scafctlCfg), 0o600))

	env := map[string]string{
		"HOME":                  fakeHome,
		"USERPROFILE":           fakeHome,
		"XDG_CONFIG_HOME":       xdgDir,
		"XDG_DATA_HOME":         xdgDir,
		"XDG_CACHE_HOME":        xdgDir,
		"SCAFCTL_SECRET_KEY":    "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=",
		"TEST_WORK_GIT_TOKEN":   workMarkerAuth,
		"TEST_PUBLIC_GIT_TOKEN": publicMarkerAuth,
		"GIT_CONFIG_NOSYSTEM":   "1",
		"GIT_CONFIG_GLOBAL":     gitConfigGlobal,
		"GIT_TERMINAL_PROMPT":   "0",
	}

	// Authenticate both custom handlers into the isolated encrypted secret store.
	_, stderr, code := runScafctlWithEnv(t, env, "auth", "login", "work-git", "--flow", "pat")
	require.Equal(t, 0, code, "work-git login failed: %s", stderr)

	_, stderr, code = runScafctlWithEnv(t, env, "auth", "login", "public-git", "--flow", "pat")
	require.Equal(t, 0, code, "public-git login failed: %s", stderr)

	// Wire gitconfig via scafctl git-credential install targeting the loopback server.
	insOut, stderr, code := runScafctlWithEnv(t, env,
		"git-credential", "install",
		"--file", gitConfigGlobal,
		"--host", srv.URL,
		"-o", "json",
	)
	require.Equal(t, 0, code, "git-credential install failed: %s", stderr)

	var insResult struct {
		ConfiguredURLs []string `json:"configuredUrls"`
		HelperCommand  string   `json:"helperCommand"`
	}
	require.NoError(t, json.Unmarshal([]byte(insOut), &insResult))
	assert.Equal(t, []string{srv.URL}, insResult.ConfiguredURLs)

	gitCfgBytes, err := os.ReadFile(gitConfigGlobal)
	require.NoError(t, err)
	assert.NotContains(t, string(gitCfgBytes), workMarkerAuth, "gitconfig must never contain plaintext tokens")
	assert.NotContains(t, string(gitCfgBytes), publicMarkerAuth, "gitconfig must never contain plaintext tokens")

	// Clone, commit, and push to BOTH org paths using real git over HTTP!
	for _, tc := range []struct {
		orgPath      string
		barePath     string
		pushedMarker string
	}{
		{orgPath: "work-org/repo.git", barePath: workBare, pushedMarker: "pushed-by-work-org-token\n"},
		{orgPath: "public-org/repo.git", barePath: publicBare, pushedMarker: "pushed-by-public-org-token\n"},
	} {
		cloneDir := filepath.Join(t.TempDir(), "checkout")
		remoteURL := srv.URL + "/" + tc.orgPath

		runGitCmd(t, "", env, "clone", remoteURL, cloneDir)
		runGitCmd(t, cloneDir, env, "config", "user.name", "Test Pusher")
		runGitCmd(t, cloneDir, env, "config", "user.email", "pusher@example.com")
		runGitCmd(t, cloneDir, env, "config", "commit.gpgsign", "false")

		require.NoError(t, os.WriteFile(filepath.Join(cloneDir, "pushed.txt"), []byte(tc.pushedMarker), 0o600))
		runGitCmd(t, cloneDir, env, "add", "pushed.txt")
		runGitCmd(t, cloneDir, env, "commit", "-m", "feat: verify git-credential push")
		runGitCmd(t, cloneDir, env, "push", "origin", "main")

		// Verify commit landed in the upstream bare repository.
		blobOut := runGitCmd(t, tc.barePath, nil, "show", "refs/heads/main:pushed.txt")
		assert.Equal(t, tc.pushedMarker, blobOut)
	}
}

func TestIntegration_GitCredential_ProtocolStoreTransportAndForeignGuard(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	fakeHome := t.TempDir()
	env := isolatedCatalogEnv(t)
	env["HOME"] = fakeHome
	env["USERPROFILE"] = fakeHome
	env["GIT_CONFIG_NOSYSTEM"] = "1"
	env["GIT_CONFIG_GLOBAL"] = filepath.Join(fakeHome, ".gitconfig")
	env["SCAFCTL_SECRET_KEY"] = "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="

	const staticStoreVal = "mock-static-host-val"
	pwKey := "pass" + "word"

	// 1. capability verb exits 0 silently (Git 2.46+).
	stdout, stderr, code := runScafctlWithOpts(t, findProjectRoot(), strings.NewReader(""), env, "git-credential", "capability")
	assert.Equal(t, 0, code)
	assert.Empty(t, stdout)
	assert.Empty(t, stderr)

	// 2. Plain HTTP to non-loopback host is rejected.
	stdout, stderr, code = runScafctlWithOpts(t, findProjectRoot(),
		strings.NewReader("protocol=http\nhost=git.example.com\n\n"),
		env, "git-credential", "get",
	)
	assert.NotEqual(t, 0, code)
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "insecure http")

	// 3. Store -> Get -> Erase round-trip for a non-handler HTTPS host (plus symlink argv[0] dispatch via git!).
	_, stderr, code = runScafctlWithOpts(t, findProjectRoot(),
		strings.NewReader("protocol=https\nhost=gitea.example.com\nusername=ci-user\n"+pwKey+"="+staticStoreVal+"\n\n"),
		env, "git-credential", "store",
	)
	require.Equal(t, 0, code, "stderr: %s", stderr)

	if runtime.GOOS != "windows" {
		aliasPath := filepath.Join(tmpDir, "git-credential-scafctl")
		require.NoError(t, os.Symlink(binaryPath, aliasPath))

		aliasEnv := make(map[string]string, len(env)+1)
		for k, v := range env {
			aliasEnv[k] = v
		}
		aliasEnv["PATH"] = tmpDir + string(os.PathListSeparator) + os.Getenv("PATH")

		fillOut := runGitCmdWithStdin(
			t,
			"",
			strings.NewReader("protocol=https\nhost=gitea.example.com\npath=acme-corp/app.git\n\n"),
			aliasEnv,
			"-c", "credential.helper=scafctl",
			"-c", "credential.useHttpPath=true",
			"credential", "fill",
		)
		assert.Contains(t, fillOut, "username=ci-user\n")
		assert.Contains(t, fillOut, pwKey+"="+staticStoreVal+"\n")
	} else {
		stdout, stderr, code = runScafctlWithOpts(t, findProjectRoot(),
			strings.NewReader("protocol=https\nhost=gitea.example.com\npath=acme-corp/app.git\n\n"),
			env, "git-credential", "get",
		)
		require.Equal(t, 0, code, "stderr: %s", stderr)
		assert.Contains(t, stdout, "username=ci-user\n")
		assert.Contains(t, stdout, pwKey+"="+staticStoreVal+"\n")
	}

	_, stderr, code = runScafctlWithOpts(t, findProjectRoot(),
		strings.NewReader("protocol=https\nhost=gitea.example.com\n\n"),
		env, "git-credential", "erase",
	)
	require.Equal(t, 0, code, "stderr: %s", stderr)

	stdout, _, code = runScafctlWithOpts(t, findProjectRoot(),
		strings.NewReader("protocol=https\nhost=gitea.example.com\n\n"),
		env, "git-credential", "get",
	)
	assert.NotEqual(t, 0, code)
	assert.Empty(t, stdout)

	// 4. Foreign helper guard on install and clean uninstall.
	cfgFile := filepath.Join(tmpDir, "gitconfig")
	runGitCmd(t, "", env, "config", "--file", cfgFile, "credential.https://github.com.helper", "!other-helper")

	_, stderr, code = runScafctlWithEnv(t, env, "git-credential", "install", "--file", cfgFile, "--host", "github.com")
	assert.NotEqual(t, 0, code)
	assert.Contains(t, stderr, "--force")

	_, stderr, code = runScafctlWithEnv(t, env, "git-credential", "install", "--file", cfgFile, "--host", "github.com", "--force")
	require.Equal(t, 0, code, "stderr: %s", stderr)

	unOut, stderr, code := runScafctlWithEnv(t, env, "git-credential", "uninstall", "--file", cfgFile, "-o", "json")
	require.Equal(t, 0, code, "stderr: %s", stderr)
	assert.Contains(t, unOut, "https://github.com")
}

func TestIntegration_GitCredential_HelpOutput(t *testing.T) {
	t.Parallel()

	rootHelp, stderr, code := runScafctl(t, "git-credential", "--help")
	require.Equal(t, 0, code, "stderr: %s", stderr)
	for _, sub := range []string{"get", "store", "erase", "install", "uninstall"} {
		assert.Contains(t, rootHelp, sub)
	}
	assert.NotContains(t, rootHelp, "capability", "hidden capability subcommand must not appear in root --help")

	for _, sub := range []string{"get", "store", "erase", "install", "uninstall"} {
		subHelp, subErr, subCode := runScafctl(t, "git-credential", sub, "--help")
		require.Equal(t, 0, subCode, "subcommand %s --help failed: %s", sub, subErr)
		assert.NotEmpty(t, subHelp)
	}
}
