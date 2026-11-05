// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	ginkgo "github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/onsi/gomega/gexec"
)

// ExecAnyExit runs the command like Exec but asserts nothing about the exit
// status, so specs can check an exact code with ExpectExitCode. It still fails
// the spec if the process cannot be started or times out.
func (opts *ExecOption) ExecAnyExit() *gexec.Session {
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
	gomega.Expect(ctx.Err()).NotTo(gomega.HaveOccurred(), "scafctl timed out after %s", opts.timeout)
	return session
}

// ExpectExitCode asserts the session exited with exactly the given code,
// including stderr in the failure message.
func ExpectExitCode(session *gexec.Session, code int) {
	ginkgo.GinkgoHelper()
	gomega.Expect(session.ExitCode()).To(gomega.Equal(code),
		"unexpected exit code; stderr: %s", session.Err.Contents())
}
