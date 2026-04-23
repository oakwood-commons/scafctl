// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"testing"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildRemoteCatalog_ParsesURL(t *testing.T) {
	t.Parallel()

	logger := logr.Discard()
	catCfg := config.CatalogConfig{
		Name: "test",
		Type: config.CatalogTypeOCI,
		URL:  "oci://ghcr.io/myorg",
	}

	remoteCat, err := buildRemoteCatalog(catCfg, nil, nil, logger)
	require.NoError(t, err)
	require.NotNil(t, remoteCat)
	assert.Equal(t, "test", remoteCat.Name())
}
