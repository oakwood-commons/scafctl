// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package celprovider

import (
	"os"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/celexp/env"
)

func TestMain(m *testing.M) {
	celexp.SetEnvFactory(env.New)
	celexp.SetCacheFactory(env.GlobalCache)
	os.Exit(m.Run())
}
