//go:build linux || darwin

package catalogtest

// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package catalogtest contains e2e specs for `scafctl catalog push`,
// `catalog pull` and `catalog delete` against real (in-process) OCI
// registries. Linux/macOS-only, matching the other registry-backed e2e suites.

import (
	"testing"

	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck // dot-import is the Ginkgo/Gomega house style.
	"github.com/onsi/gomega"

	"github.com/oakwood-commons/scafctl/e2e/internal/utils"
)

func TestCatalog(t *testing.T) {
	gomega.RegisterFailHandler(Fail)
	RunSpecs(t, "Catalog Push/Pull/Delete Suite")
}

var _ = BeforeSuite(func() {
	utils.BuildScafctl()
})
