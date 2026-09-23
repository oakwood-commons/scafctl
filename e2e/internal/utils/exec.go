// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/onsi/gomega/gexec"
)

// DefaultTimeout bounds how long Exec waits for the command to finish.
const DefaultTimeout = 30 * time.Second

// ExecOption builds up a scafctl invocation and its expectations before
// running it. Modeled on ORAS's e2e ExecOption
// (test/e2e/internal/utils/exec.go), trimmed to what this repo's build/catalog
// specs need: no HTTP-header or registry-status matchers, since this phase
// never talks to a remote registry.
type ExecOption struct {
	binary  string
	args    []string
	env     []string
	workDir string
	timeout time.Duration

	expectFailure bool

	stdoutKeywords []string
	stderrKeywords []string
}

// Scafctl returns a default execution option for the scafctl binary under
// test (as resolved by BuildScafctl).
func Scafctl(args ...string) *ExecOption {
	return &ExecOption{
		binary:  ScafctlPath,
		args:    args,
		timeout: DefaultTimeout,
	}
}

// WithEnv sets "KEY=VALUE" environment overrides for the execution, layered
// on top of the current process's environment. Typically the Env field of an
// Isolation from NewIsolatedEnv.
func (opts *ExecOption) WithEnv(env []string) *ExecOption {
	opts.env = env
	return opts
}

// WithWorkDir sets the working directory for the execution.
func (opts *ExecOption) WithWorkDir(dir string) *ExecOption {
	opts.workDir = dir
	return opts
}

// WithTimeout overrides the default timeout for the execution.
func (opts *ExecOption) WithTimeout(timeout time.Duration) *ExecOption {
	opts.timeout = timeout
	return opts
}

// ExpectFailure asserts the command exits with a non-zero status.
func (opts *ExecOption) ExpectFailure() *ExecOption {
	opts.expectFailure = true
	return opts
}

// MatchStdout asserts stdout contains every given keyword.
func (opts *ExecOption) MatchStdout(keywords ...string) *ExecOption {
	opts.stdoutKeywords = append(opts.stdoutKeywords, keywords...)
	return opts
}

// MatchStderr asserts stderr contains every given keyword.
func (opts *ExecOption) MatchStderr(keywords ...string) *ExecOption {
	opts.stderrKeywords = append(opts.stderrKeywords, keywords...)
	return opts
}

// Exec runs the command and returns the completed gexec.Session, having
// already asserted the configured exit-code expectation and keyword matches.
// Use session.Out.Contents() / session.Err.Contents() to inspect output for
// assertions beyond simple keyword matching (e.g. JSON parsing).
func (opts *ExecOption) Exec() *gexec.Session {
	ginkgo.GinkgoHelper()
	ginkgo.By(fmt.Sprintf(">> scafctl %s", strings.Join(opts.args, " ")))

	ctx, cancel := context.WithTimeout(context.Background(), opts.timeout)
	defer cancel()

	//nolint:gosec // e2e helper executes a test-built binary with test-controlled args.
	cmd := exec.CommandContext(ctx, opts.binary, opts.args...)
	if opts.workDir != "" {
		cmd.Dir = opts.workDir
	}
	if len(opts.env) > 0 {
		cmd.Env = append(os.Environ(), opts.env...)
	}

	session, err := gexec.Start(cmd, ginkgo.GinkgoWriter, ginkgo.GinkgoWriter)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	session.Wait(opts.timeout)

	if opts.expectFailure {
		gomega.Expect(session.ExitCode()).NotTo(gomega.Equal(0), "expected scafctl to fail")
	} else {
		gomega.Expect(session.ExitCode()).To(gomega.Equal(0), "expected scafctl to succeed; stderr: %s", session.Err.Contents())
	}

	for _, kw := range opts.stdoutKeywords {
		gomega.Expect(session.Out).To(gbytes.Say(regexp.QuoteMeta(kw)))
	}
	for _, kw := range opts.stderrKeywords {
		gomega.Expect(session.Err).To(gbytes.Say(regexp.QuoteMeta(kw)))
	}

	return session
}
