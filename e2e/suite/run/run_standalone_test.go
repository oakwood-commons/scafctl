// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

//go:build linux || darwin

package runtest

import (
	"encoding/json"

	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck // dot-import is the Ginkgo/Gomega house style.
	"github.com/onsi/gomega"

	"github.com/oakwood-commons/scafctl/e2e/internal/utils"
)

// parseDoc unmarshals JSON stdout into a map.
func parseDoc(raw []byte) map[string]any {
	GinkgoHelper()
	var doc map[string]any
	gomega.Expect(json.Unmarshal(raw, &doc)).To(gomega.Succeed(), "stdout: %s", raw)
	return doc
}

var _ = Describe("run standalone versus full graph", func() {
	var (
		env utils.Isolation
		dir string
	)

	BeforeEach(func() {
		env = utils.NewIsolatedEnv()
		dir = stageFixture("run-deterministic")
	})

	It("resolves a named resolver to the same value as the full graph", func() {
		full := parseDoc(resolverOutput(env, dir))

		// "message" depends on shout -> greeting and settings: its dependencies
		// must be pulled in transitively and yield identical values.
		for _, name := range []string{"standalone", "message", "shout"} {
			one := parseDoc(resolverOutput(env, dir, name))
			gomega.Expect(one).To(gomega.HaveKey(name))
			gomega.Expect(one[name]).To(gomega.Equal(full[name]), "resolver %q", name)
		}
	})

	It("omits unrelated resolvers from the output when one is named", func() {
		one := parseDoc(resolverOutput(env, dir, "standalone"))
		gomega.Expect(one).NotTo(gomega.HaveKey("message"))
		gomega.Expect(one).NotTo(gomega.HaveKey("settings"))
	})

	It("runs a named action and its dependencies to the same files as the full workflow", func() {
		fullDir := stageFixture("run-deterministic")
		utils.ExpectExitCode(run(env, fullDir, "run", "solution", "-f", fixturePath(fullDir)), exitSuccess)

		utils.ExpectExitCode(run(env, dir, "run", "action", "write-settings", "-f", fixturePath(dir)), exitSuccess)

		gomega.Expect(utils.SnapshotTree(dir)).To(gomega.Equal(utils.SnapshotTree(fullDir)))
	})

	It("runs only the named action when it has no dependencies", func() {
		utils.ExpectExitCode(run(env, dir, "run", "action", "write-message", "-f", fixturePath(dir)), exitSuccess)

		after := utils.SnapshotTree(dir)
		gomega.Expect(after).To(gomega.HaveKey("out/message.txt"))
		gomega.Expect(after).NotTo(gomega.HaveKey("out/settings.txt"))
	})
})
