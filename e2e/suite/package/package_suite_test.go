// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

//go:build linux || darwin

// Package contains e2e specs for `scafctl package` workflows, including
// solution packaging and registry-backed plugin packaging. Linux/macOS-only:
// matches the supported development environment for this repo's packaging e2e
// tests while intentionally excluding Windows.
package packagetest

import (
	"testing"

	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck // dot-import is the Ginkgo/Gomega house style.
	"github.com/onsi/gomega"

	"github.com/oakwood-commons/scafctl/e2e/internal/utils"
)

func TestPackage(t *testing.T) {
	gomega.RegisterFailHandler(Fail)
	RunSpecs(t, "Solution Package Suite")
}

var _ = BeforeSuite(func() {
	utils.BuildScafctl()
})
