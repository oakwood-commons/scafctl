// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

//go:build linux || darwin

// Package authtest contains e2e specs for scafctl's authentication surface:
// the CLI auth commands, token caching, secret storage, and error handling
// around the pluggable auth handler interface. Specs drive a real scafctl
// binary against an in-process fake OAuth2 identity provider (see
// utils.FakeIdP), so they exercise scafctl's own boundary without any network
// access, real IdP credentials, or browser interaction. Provider-specific
// protocol correctness (Entra/GCP/GitHub wire formats, JWT/claims parsing) is
// intentionally left to each handler implementation's own tests.
//
// Linux/macOS-only: matches the supported development environment for this
// repo's e2e tests and the secrets store's Unix file-permission model.
package authtest

import (
	"testing"

	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck // dot-import is the Ginkgo/Gomega house style.
	"github.com/onsi/gomega"

	"github.com/oakwood-commons/scafctl/e2e/internal/utils"
)

func TestAuth(t *testing.T) {
	gomega.RegisterFailHandler(Fail)
	RunSpecs(t, "Auth Suite")
}

var _ = BeforeSuite(func() {
	utils.BuildScafctl()
})
