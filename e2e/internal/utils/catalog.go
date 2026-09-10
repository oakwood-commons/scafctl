// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"os"
	"path/filepath"

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
		},
	}
}

// CatalogBlobsDir returns the local catalog's content-addressed blob
// directory. Specs use this to inspect raw OCI blobs (e.g. to assert no lock
// layer was written) without hardcoding the catalog's internal layout inline.
func (i Isolation) CatalogBlobsDir() string {
	return filepath.Join(i.Root, "scafctl", "catalog", "blobs", "sha256")
}
