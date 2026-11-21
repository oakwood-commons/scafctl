// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

//go:build linux || darwin

package runtest

import (
	"path/filepath"

	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck // dot-import is the Ginkgo/Gomega house style.
	"github.com/onsi/gomega"

	"github.com/oakwood-commons/scafctl/e2e/internal/utils"
)

var _ = Describe("run --dry-run", func() {
	var (
		env utils.Isolation
		dir string
	)

	BeforeEach(func() {
		env = utils.NewIsolatedEnv()
		dir = stageFixture("run-dryrun")
	})

	It("leaves the working tree and scafctl state untouched", func() {
		// Warm up scafctl's own lazily-created state (catalog layout, caches) with
		// a first dry run so the snapshot isolates what --dry-run itself does.
		utils.ExpectExitCode(run(env, dir, "run", "solution", "-f", fixturePath(dir), "--dry-run"), exitSuccess)
		treeBefore := utils.SnapshotTree(dir)
		stateBefore := utils.SnapshotTree(env.Root)

		s := run(env, dir, "run", "solution", "-f", fixturePath(dir), "--dry-run")
		utils.ExpectExitCode(s, exitSuccess)

		gomega.Expect(utils.SnapshotTree(dir)).To(gomega.Equal(treeBefore), "working tree changed")
		gomega.Expect(filepath.Join(dir, "out", "greeting.txt")).NotTo(gomega.BeAnExistingFile())
		gomega.Expect(filepath.Join(dir, "marker.txt")).NotTo(gomega.BeAnExistingFile())
		gomega.Expect(utils.SnapshotTree(env.Root)).To(gomega.Equal(stateBefore), "isolated state dirs changed")
	})

	It("describes what would run (WhatIf) instead of running it", func() {
		s := run(env, dir, "run", "solution", "-f", fixturePath(dir), "--dry-run")
		utils.ExpectExitCode(s, exitSuccess)

		out := string(s.Out.Contents()) + string(s.Err.Contents())
		gomega.Expect(out).To(gomega.ContainSubstring("write-file"))
		gomega.Expect(out).To(gomega.ContainSubstring("What if"))
		gomega.Expect(out).To(gomega.ContainSubstring("write-marker"))
	})

	It("also has no side effects for run action", func() {
		treeBefore := utils.SnapshotTree(dir)
		utils.ExpectExitCode(run(env, dir, "run", "action", "-f", fixturePath(dir), "--dry-run"), exitSuccess)
		gomega.Expect(utils.SnapshotTree(dir)).To(gomega.Equal(treeBefore))
	})

	It("does perform the side effects without --dry-run (control)", func() {
		utils.ExpectExitCode(run(env, dir, "run", "solution", "-f", fixturePath(dir)), exitSuccess)
		gomega.Expect(filepath.Join(dir, "out", "greeting.txt")).To(gomega.BeAnExistingFile())
		gomega.Expect(filepath.Join(dir, "marker.txt")).To(gomega.BeAnExistingFile())
	})
})
