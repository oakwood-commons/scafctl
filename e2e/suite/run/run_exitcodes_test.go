// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

//go:build linux || darwin

package runtest

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck // dot-import is the Ginkgo/Gomega house style.
	"github.com/onsi/gomega"

	"github.com/oakwood-commons/scafctl/e2e/internal/utils"
)

// slowServerHold is how long the slow endpoint stalls; it only needs to
// outlast the fixture's 1s action timeout.
const slowServerHold = 15 * time.Second

var _ = Describe("run exit codes", func() {
	var (
		env utils.Isolation
		dir string
	)

	BeforeEach(func() {
		env = utils.NewIsolatedEnv()
		dir = stageFixture("run-failures")
	})

	// args builds `run <cmd> -f <fixture> -r mode=<mode> ...`.
	args := func(cmd, mode string, extra ...string) []string {
		a := []string{"run", cmd, "-f", fixturePath(dir)}
		if mode != "" {
			a = append(a, "-r", "mode="+mode)
		}
		return append(a, extra...)
	}

	It("exits 0 on a clean run of every run command", func() {
		for _, cmd := range []string{"solution", "resolver", "action"} {
			utils.ExpectExitCode(run(env, dir, args(cmd, "")...), exitSuccess)
		}
	})

	DescribeTable("failure class by command",
		func(cmd, mode string, want int, cause string) {
			s := run(env, dir, args(cmd, mode)...)
			utils.ExpectExitCode(s, want)
			// Guard against failing for an unrelated reason with the same code.
			gomega.Expect(string(s.Err.Contents())).To(gomega.ContainSubstring(cause))
		},
		Entry("validation failure: run solution", "solution", "validation", exitValidationFailed, "must be lowercase"),
		Entry("validation failure: run action", "action", "validation", exitValidationFailed, "must be lowercase"),
		Entry("resolver error: run solution", "solution", "resolve", exitGeneralError, "division by zero"),
		Entry("action failure: run solution", "solution", "action", exitActionFailed, "does-not-exist"),
		Entry("action failure: run action", "action", "action", exitActionFailed, "does-not-exist"),
	)

	It("exits 6 when an action exceeds its timeout", func() {
		slow := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			select {
			case <-r.Context().Done():
			case <-time.After(slowServerHold):
			}
		}))
		DeferCleanup(slow.Close)
		allowLoopbackHTTP(env) // otherwise the SSRF policy fails the action before any timeout.

		// An action timeout surfaces as an action failure, not TimeoutError (9).
		s := run(env, dir, args("solution", "timeout", "-r", "slowUrl="+slow.URL)...)
		utils.ExpectExitCode(s, exitActionFailed)
		gomega.Expect(string(s.Err.Contents())).To(gomega.ContainSubstring("timed out"))
	})

	It("exits 2 for a malformed -r input", func() {
		s := run(env, dir, "run", "solution", "-f", fixturePath(dir), "-r", "no-equals-sign")
		utils.ExpectExitCode(s, exitValidationFailed)
		gomega.Expect(string(s.Err.Contents())).To(gomega.ContainSubstring("missing '=' separator"))
	})

	It("exits 4 when the solution file does not exist", func() {
		s := run(env, dir, "run", "solution", "-f", filepath.Join(dir, "missing.yaml"))
		utils.ExpectExitCode(s, exitFileNotFound)
	})

	Describe("partial success", func() {
		It("exits 0 by default when a continueOnError action fails", func() {
			utils.ExpectExitCode(run(env, dir, args("solution", "partial")...), exitSuccess)
		})

		It("exits 12 with --detailed-exit-code", func() {
			utils.ExpectExitCode(run(env, dir, args("solution", "partial", "--detailed-exit-code")...), exitPartialSuccess)
		})
	})

	Describe("--on-validation-error", func() {
		It("error aborts with a validation exit code", func() {
			utils.ExpectExitCode(run(env, dir, args("resolver", "validation", "--on-validation-error", "error")...), exitValidationFailed)
		})

		It("warn continues and reports success", func() {
			utils.ExpectExitCode(run(env, dir, args("solution", "validation", "--on-validation-error", "warn")...), exitSuccess)
		})

		It("ignore skips validation entirely", func() {
			utils.ExpectExitCode(run(env, dir, args("solution", "validation", "--on-validation-error", "ignore")...), exitSuccess)
		})
	})

	It("emits a failure envelope on stdout for -o json failures", func() {
		s := run(env, dir, args("solution", "action", "-o", "json")...)
		utils.ExpectExitCode(s, exitActionFailed)
		gomega.Expect(parseDoc(s.Out.Contents())).To(gomega.HaveKeyWithValue("status", "failed"))
	})
})
