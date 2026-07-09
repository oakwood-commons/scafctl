// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package login

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/kubeconfig"
)

// writeManagedKubeconfig writes a kubeconfig fixture with one scafctl-managed
// context (prod), one foreign context (staging, a plain token user), and a
// current-context of prod. It returns the file path. The managed entry is
// written for the "scafctl" binary.
func writeManagedKubeconfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	content := `apiVersion: v1
kind: Config
current-context: prod
clusters:
  - name: prod
    cluster:
      server: https://api.prod.example.com:6443
  - name: staging
    cluster:
      server: https://api.staging.example.com:6443
contexts:
  - name: prod
    context:
      cluster: prod
      user: prod
      namespace: team-a
  - name: staging
    context:
      cluster: staging
      user: staging
users:
  - name: prod
    user:
      exec:
        command: scafctl
        args:
          - auth
          - token
          - oidc
  - name: staging
    user:
      token: static-token
`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func TestListManaged_FindsOnlyManagedEntries(t *testing.T) {
	t.Parallel()

	path := writeManagedKubeconfig(t)
	entries, err := ListManaged("scafctl", path)
	require.NoError(t, err)
	require.Len(t, entries, 1, "only the scafctl exec-credential context is managed")
	assert.Equal(t, "prod", entries[0].ContextName)
	assert.Equal(t, "prod", entries[0].ClusterName)
	assert.Equal(t, "prod", entries[0].UserName)
	assert.Equal(t, "https://api.prod.example.com:6443", entries[0].Server)
}

func TestListManaged_DifferentBinaryNameNotManaged(t *testing.T) {
	t.Parallel()

	// The entry was written by "scafctl"; querying as "othercli" must not match.
	path := writeManagedKubeconfig(t)
	entries, err := ListManaged("othercli", path)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestListManaged_MissingKubeconfig(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "does-not-exist")
	entries, err := ListManaged("scafctl", path)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestCurrentContext_ReportsManagedCurrent(t *testing.T) {
	t.Parallel()

	path := writeManagedKubeconfig(t)
	st, err := CurrentContext("scafctl", path)
	require.NoError(t, err)
	assert.Equal(t, "prod", st.Context)
	assert.Equal(t, "prod", st.Cluster)
	assert.Equal(t, "prod", st.User)
	assert.Equal(t, "team-a", st.Namespace)
	assert.Equal(t, "https://api.prod.example.com:6443", st.Server)
	assert.True(t, st.Managed)
}

func TestCurrentContext_NoCurrentContext(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	require.NoError(t, os.WriteFile(path, []byte("apiVersion: v1\nkind: Config\n"), 0o600))

	st, err := CurrentContext("scafctl", path)
	require.NoError(t, err)
	assert.Empty(t, st.Context)
	assert.False(t, st.Managed)
}

func TestLogoutAll_RemovesManagedEntriesViaProvider(t *testing.T) {
	t.Parallel()

	path := writeManagedKubeconfig(t)
	kc := &stubKube{removeRes: kubeconfig.RemoveResult{Success: true, Removed: true}}
	deps := Deps{Kubeconfig: kc, BinaryName: "scafctl"}

	res, err := LogoutAll(context.Background(), deps, LogoutAllRequest{KubeconfigPath: path})
	require.NoError(t, err)
	require.Len(t, res.Removed, 1)
	assert.Equal(t, "prod", res.Removed[0])
	assert.False(t, res.UsedFallback)
	// The provider received the managed entry's names.
	assert.Equal(t, "prod", kc.removeIn.ClusterName)
}

func TestLogoutAll_FallbackWhenProviderUnavailable(t *testing.T) {
	t.Parallel()

	path := writeManagedKubeconfig(t)
	kc := &stubKube{removeErr: kubeconfig.ErrProviderUnavailable}
	deps := Deps{Kubeconfig: kc, BinaryName: "scafctl"}

	res, err := LogoutAll(context.Background(), deps, LogoutAllRequest{KubeconfigPath: path})
	require.NoError(t, err)
	assert.True(t, res.UsedFallback)
	require.Len(t, res.Removed, 1)
	assert.Equal(t, "prod", res.Removed[0])

	// The managed context is gone; the foreign staging context remains.
	remaining, err := ListManaged("scafctl", path)
	require.NoError(t, err)
	assert.Empty(t, remaining)
	data, err := os.ReadFile(path) //nolint:gosec // test-controlled path
	require.NoError(t, err)
	assert.Contains(t, string(data), "staging")
}

func TestLogoutAll_ProviderReportsFailure(t *testing.T) {
	t.Parallel()

	// The provider completes without a Go error but reports Success=false for an
	// entry ListManaged found: LogoutAll must surface it, not silently skip it.
	path := writeManagedKubeconfig(t)
	kc := &stubKube{removeRes: kubeconfig.RemoveResult{Success: false}}
	deps := Deps{Kubeconfig: kc, BinaryName: "scafctl"}

	res, err := LogoutAll(context.Background(), deps, LogoutAllRequest{KubeconfigPath: path})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "provider reported failure")
	assert.Empty(t, res.Removed)
}

func TestLogoutAll_SuccessNoMatchIsBenignSkip(t *testing.T) {
	t.Parallel()

	// Success=true but Removed=false (entry already gone / List->Remove race) is
	// a benign no-op: no error, nothing recorded as removed.
	path := writeManagedKubeconfig(t)
	kc := &stubKube{removeRes: kubeconfig.RemoveResult{Success: true, Removed: false}}
	deps := Deps{Kubeconfig: kc, BinaryName: "scafctl"}

	res, err := LogoutAll(context.Background(), deps, LogoutAllRequest{KubeconfigPath: path})
	require.NoError(t, err)
	assert.Empty(t, res.Removed)
}

func TestLogoutAll_NoManagedEntries(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	require.NoError(t, os.WriteFile(path, []byte("apiVersion: v1\nkind: Config\n"), 0o600))

	kc := &stubKube{}
	deps := Deps{Kubeconfig: kc, BinaryName: "scafctl"}
	res, err := LogoutAll(context.Background(), deps, LogoutAllRequest{KubeconfigPath: path})
	require.NoError(t, err)
	assert.Empty(t, res.Removed)
}

func TestLogoutAll_NoKubeconfigWriter(t *testing.T) {
	t.Parallel()

	_, err := LogoutAll(context.Background(), Deps{}, LogoutAllRequest{})
	assert.ErrorIs(t, err, ErrNoKubeconfigWriter)
}
