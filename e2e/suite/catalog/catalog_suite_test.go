// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

//go:build linux || darwin

// Package catalogtest contains e2e specs for `scafctl catalog` workflows,
// including multi-source pulls into the shared local catalog. Linux/macOS-only:
// matches the supported development environment for this repo's catalog e2e
// tests while intentionally excluding Windows.
package catalogtest

import (
	"testing"

	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck // dot-import is the Ginkgo/Gomega house style.
	"github.com/onsi/gomega"

	"github.com/oakwood-commons/scafctl/e2e/internal/utils"
)

func TestCatalog(t *testing.T) {
	gomega.RegisterFailHandler(Fail)
	RunSpecs(t, "Catalog Suite")
}

var _ = BeforeSuite(func() {
	utils.BuildScafctl()
})
