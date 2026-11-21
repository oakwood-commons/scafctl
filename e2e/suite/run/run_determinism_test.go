// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

//go:build linux || darwin

package runtest

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"regexp"

	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck // dot-import is the Ginkgo/Gomega house style.
	"github.com/onsi/gomega"

	"github.com/oakwood-commons/scafctl/e2e/internal/utils"
)

// determinismRuns is how many times each solution is run; every run must
// produce output identical to the first.
const determinismRuns = 5

// resolverOutput runs `run resolver -o json` (all resolvers) and returns the
// raw stdout, requiring success.
func resolverOutput(env utils.Isolation, dir string, extra ...string) []byte {
	GinkgoHelper()
	args := append([]string{"run", "resolver", "-f", fixturePath(dir), "-o", "json"}, extra...)
	s := run(env, dir, args...)
	utils.ExpectExitCode(s, exitSuccess)
	return s.Out.Contents()
}

var _ = Describe("run determinism", func() {
	var env utils.Isolation

	BeforeEach(func() {
		env = utils.NewIsolatedEnv()
	})

	It("produces identical solution output and files across repeated runs", func() {
		var (
			firstOut  string
			firstTree map[string]string
		)
		for i := range determinismRuns {
			dir := stageFixture("run-deterministic")
			s := run(env, dir, "run", "solution", "-f", fixturePath(dir), "-o", "json")
			utils.ExpectExitCode(s, exitSuccess)

			// Output embeds the (per-run) working directory, possibly symlink-resolved
			// (macOS /var -> /private/var); mask both spellings.
			raw := bytes.ReplaceAll(s.Out.Contents(), []byte(dir), []byte("<DIR>"))
			if resolved, err := filepath.EvalSymlinks(dir); err == nil {
				raw = bytes.ReplaceAll(raw, []byte(resolved), []byte("<DIR>"))
			}
			out := utils.NormalizeJSON(raw)
			tree := utils.SnapshotTree(dir)
			gomega.Expect(tree).To(gomega.HaveKey("out/message.txt"))
			gomega.Expect(tree).To(gomega.HaveKey("out/settings.txt"))

			if i == 0 {
				firstOut, firstTree = out, tree
				continue
			}
			gomega.Expect(out).To(gomega.Equal(firstOut), "run %d output differs from run 0", i)
			gomega.Expect(tree).To(gomega.Equal(firstTree), "run %d files differ from run 0", i)
		}
	})

	It("is not affected by --show-execution metadata once normalized", func() {
		dir := stageFixture("run-deterministic")
		plain := resolverOutput(env, dir)
		withMeta := resolverOutput(env, dir, "--show-execution")

		gomega.Expect(utils.NormalizeJSON(withMeta)).To(gomega.Equal(utils.NormalizeJSON(plain)))
	})

	Describe("non-deterministic providers", func() {
		var outputs [][]byte

		BeforeEach(func() {
			dir := stageFixture("run-nondeterministic")
			outputs = make([][]byte, 0, determinismRuns)
			for range determinismRuns {
				outputs = append(outputs, resolverOutput(env, dir))
			}
		})

		It("differ across runs when unmasked", func() {
			first := utils.NormalizeJSON(outputs[0])
			differs := false
			for _, o := range outputs[1:] {
				if utils.NormalizeJSON(o) != first {
					differs = true
				}
			}
			gomega.Expect(differs).To(gomega.BeTrue(),
				"fixture must actually be non-deterministic or the exclusion below proves nothing")
		})

		It("are identical across runs once their values are masked", func() {
			masks := []*regexp.Regexp{utils.UUIDMask, utils.TimestampMask}
			first := utils.NormalizeJSON(outputs[0], masks...)
			for i, o := range outputs[1:] {
				gomega.Expect(utils.NormalizeJSON(o, masks...)).To(gomega.Equal(first), "run %d", i+1)
			}

			var doc map[string]any
			gomega.Expect(json.Unmarshal(outputs[0], &doc)).To(gomega.Succeed())
			gomega.Expect(doc).To(gomega.HaveKeyWithValue("stable", "same-every-time"))
		})
	})
})
