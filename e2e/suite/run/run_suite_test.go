// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

//go:build linux || darwin

// Package runtest contains e2e specs for `scafctl run solution|resolver|action`
// runtime execution: determinism, standalone-versus-full-graph equivalence,
// runtime injection (auth handlers, bundled data), exact exit codes, and
// dry-run side-effect freedom. Linux/macOS-only, matching the other e2e suites.
package runtest

import (
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck // dot-import is the Ginkgo/Gomega house style.
	"github.com/onsi/gomega"
	"github.com/onsi/gomega/gexec"

	"github.com/oakwood-commons/scafctl/e2e/internal/utils"
)

// solutionFile is the entry file name of every run fixture.
const solutionFile = "solution.yaml"

// Exit codes asserted by the suite. They mirror pkg/exitcode and are
// deliberately duplicated as literals: the suite pins the user-visible
// contract, so a renumbering in the product must fail these specs.
const (
	exitSuccess          = 0
	exitGeneralError     = 1
	exitValidationFailed = 2
	exitFileNotFound     = 4
	exitActionFailed     = 6
	exitPartialSuccess   = 12
)

func TestRun(t *testing.T) {
	gomega.RegisterFailHandler(Fail)
	RunSpecs(t, "Run Suite")
}

var _ = BeforeSuite(func() {
	utils.BuildScafctl()
})

// stageFixture copies a solution fixture into a fresh temp dir and returns it.
// Actions write relative to the working directory, so running from a copy keeps
// the checked-in fixture clean and gives each spec its own tree to inspect.
func stageFixture(name string) string {
	dir := GinkgoT().TempDir()
	gomega.Expect(os.CopyFS(dir, os.DirFS(utils.SolutionFixture(name)))).To(gomega.Succeed())
	return dir
}

// run executes scafctl from workDir under the isolated env, asserting nothing
// about the exit code.
func run(env utils.Isolation, workDir string, args ...string) *gexec.Session {
	GinkgoHelper()
	return utils.Scafctl(args...).WithEnv(env.Env).WithWorkDir(workDir).ExecAnyExit()
}

// fixturePath returns the solution file inside a staged dir.
func fixturePath(dir string) string {
	return filepath.Join(dir, solutionFile)
}
