// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package utils provides shared helpers for scafctl's Ginkgo/Gomega e2e
// suites: binary resolution, environment isolation, and CLI execution.
package utils

import (
	"fmt"
	"os"

	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck // dot-import is the Ginkgo/Gomega house style.
	"github.com/onsi/gomega"
	"github.com/onsi/gomega/gexec"
)

// scafctlModule is the build target resolved via the go.work workspace,
// which includes both the root module (".") and this e2e module ("./e2e").
// Building this path always compiles the local checkout's cmd/scafctl, never
// a version pinned in e2e/go.sum.
const scafctlModule = "github.com/oakwood-commons/scafctl/cmd/scafctl"

// ScafctlPathEnvVar lets a pre-built scafctl binary be used instead of
// building one, for faster local iteration when only test code changed.
const ScafctlPathEnvVar = "SCAFCTL_PATH"

// ScafctlPath points to the to-be-tested scafctl binary. It is set by
// BuildScafctl, which must be called from a BeforeSuite.
var ScafctlPath string

// BuildScafctl resolves the scafctl binary under test: it honors
// SCAFCTL_PATH if set (pointing at a pre-built binary), otherwise builds one
// via gexec.Build and registers cleanup of the build artifacts. Call this
// from a suite's BeforeSuite.
func BuildScafctl() {
	if pre := os.Getenv(ScafctlPathEnvVar); pre != "" {
		fmt.Fprintf(os.Stderr, "using pre-built scafctl binary at %q\n", pre)
		ScafctlPath = pre
		return
	}

	path, err := gexec.Build(scafctlModule)
	gomega.Expect(err).NotTo(gomega.HaveOccurred(), "failed to build scafctl binary")
	DeferCleanup(gexec.CleanupBuildArtifacts)

	ScafctlPath = path
	fmt.Fprintf(os.Stderr, "testing against freshly-built scafctl binary at %q\n", ScafctlPath)
}
