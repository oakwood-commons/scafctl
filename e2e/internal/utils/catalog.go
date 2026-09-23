// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck // dot-import is the Ginkgo/Gomega house style.
	"github.com/onsi/gomega"
)

// isolatedConfigContent is a minimal scafctl config that disables the
// official remote catalog, preventing any network calls during specs that
// only exercise local catalog operations (build/inspect/extract).
const isolatedConfigContent = `# e2e test config — no remote catalogs
catalogs:
  - name: local
    type: filesystem
settings:
  disableOfficialCatalog: true
`

// Secret-store isolation env vars. scafctl's secret store gives
// SCAFCTL_SECRETS_DIR precedence over XDG_DATA_HOME (see
// pkg/secrets/storage.go) and reads its master key from the OS keyring, then
// SCAFCTL_SECRET_KEY, then a file. Since ExecOption.WithEnv layers overrides
// on top of the inherited host environment, a host-set SCAFCTL_SECRETS_DIR (or
// a shared OS-keyring master key) would let specs read/write a store outside
// this temp dir while EncSecretFiles scans the temp path. Pinning both to
// spec-local, deterministic values keeps the auth suite fully isolated.
const (
	secretsDirEnvVar = "SCAFCTL_SECRETS_DIR" //nolint:gosec // env var name, not a credential
	secretKeyEnvVar  = "SCAFCTL_SECRET_KEY"  //nolint:gosec // env var name, not a credential

	// testMasterKey is a fixed, non-secret base64-encoded 32-byte key used only
	// to make the encrypted secret store deterministic and independent of the
	// host OS keyring during e2e runs. It is the bytes 0x00..0x1f and must never
	// be used outside tests.
	testMasterKey = "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8=" //nolint:gosec // fixed test-only key, not a real credential
)

// Isolation bundles the temp-dir root and derived env var overrides for one
// spec's fully isolated scafctl invocations.
type Isolation struct {
	// Root is the temp directory backing XDG_DATA_HOME/XDG_CACHE_HOME/
	// XDG_CONFIG_HOME. Use it to locate catalog internals for assertions
	// (see CatalogBlobsDir).
	Root string

	// Env holds the "KEY=VALUE" environment overrides to pass to Exec's
	// WithEnv, isolating scafctl from the host environment and any other
	// spec's state.
	Env []string
}

// NewIsolatedEnv returns environment isolation for a spec's scafctl
// invocations: a fresh temp dir backs XDG_DATA_HOME, XDG_CACHE_HOME, and
// XDG_CONFIG_HOME, and a minimal config disables the official remote
// catalog, preventing any network calls during specs that only exercise
// local catalog operations (build/inspect/extract). Each call gets its own
// temp dir via GinkgoT().TempDir(), so specs remain independent even when
// run in parallel.
func NewIsolatedEnv() Isolation {
	tmpDir := GinkgoT().TempDir()

	configDir := filepath.Join(tmpDir, "scafctl")
	gomega.Expect(os.MkdirAll(configDir, 0o755)).To(gomega.Succeed())

	configPath := filepath.Join(configDir, "config.yaml")
	gomega.Expect(os.WriteFile(configPath, []byte(isolatedConfigContent), 0o600)).To(gomega.Succeed())

	return Isolation{
		Root: tmpDir,
		Env: []string{
			"XDG_DATA_HOME=" + tmpDir,
			"XDG_CACHE_HOME=" + tmpDir,
			"XDG_CONFIG_HOME=" + tmpDir,
			// Pin the secret store to this temp dir and a deterministic
			// master key so a host-set SCAFCTL_SECRETS_DIR / OS-keyring key
			// cannot leak the suite out of its sandbox (see the const block).
			secretsDirEnvVar + "=" + filepath.Join(tmpDir, "scafctl", "secrets"),
			secretKeyEnvVar + "=" + testMasterKey,
		},
	}
}

// CatalogBlobsDir returns the local catalog's content-addressed blob
// directory. Specs use this to inspect raw OCI blobs (e.g. to assert no lock
// layer was written) without hardcoding the catalog's internal layout inline.
func (i Isolation) CatalogBlobsDir() string {
	return filepath.Join(i.Root, "scafctl", "catalog", "blobs", "sha256")
}

// SecretsDir returns the encrypted secret-store directory for this isolated
// environment (XDG_DATA_HOME/scafctl/secrets). Auth handlers persist access
// tokens, refresh tokens, and metadata here as encrypted ".enc" files.
func (i Isolation) SecretsDir() string {
	return filepath.Join(i.Root, "scafctl", "secrets")
}

// EncSecretFiles returns the paths of encrypted secret files (".enc") currently
// present in the secret store. Specs use it to assert that credential material
// was actually persisted (so a plaintext-absence scan is not vacuous).
func (i Isolation) EncSecretFiles() []string {
	entries, err := os.ReadDir(i.SecretsDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".enc") {
			files = append(files, filepath.Join(i.SecretsDir(), e.Name()))
		}
	}
	return files
}

// ExpectNoPlaintextSecrets walks every regular file under the isolated root and
// asserts that none contains any of the given sensitive values as a byte
// substring. It proves that secrets (e.g. access/refresh tokens) are never
// persisted in plaintext anywhere in the environment -- only inside encrypted
// secret-store blobs.
func (i Isolation) ExpectNoPlaintextSecrets(values ...string) {
	gomega.Expect(values).NotTo(gomega.BeEmpty())
	needles := make([][]byte, len(values))
	for idx, v := range values {
		gomega.Expect(v).NotTo(gomega.BeEmpty())
		needles[idx] = []byte(v)
	}

	err := filepath.WalkDir(i.Root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		content, readErr := os.ReadFile(path) //nolint:gosec // scanning test-owned isolated files
		if readErr != nil {
			return readErr
		}
		for idx, needle := range needles {
			gomega.Expect(bytes.Contains(content, needle)).To(gomega.BeFalse(),
				"sensitive value %q found in plaintext in %s", values[idx], path)
		}
		return nil
	})
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
}
