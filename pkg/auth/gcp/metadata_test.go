// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package gcp

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetMetadataHost_Default(t *testing.T) {
	t.Setenv(EnvGCEMetadataHost, "")
	host := getMetadataHost()
	assert.Equal(t, defaultMetadataHost, host)
}

func TestGetMetadataHost_EnvOverride(t *testing.T) {
	t.Setenv(EnvGCEMetadataHost, "my-metadata-server")
	host := getMetadataHost()
	assert.Equal(t, "my-metadata-server", host)
}

func TestGetMetadataTokenURL(t *testing.T) {
	t.Setenv(EnvGCEMetadataHost, "test-server")
	url := getMetadataTokenURL()
	assert.True(t, strings.Contains(url, "test-server"))
	assert.True(t, strings.Contains(url, "token"))
}

func TestGetMetadataEmailURL(t *testing.T) {
	t.Setenv(EnvGCEMetadataHost, "test-server")
	url := getMetadataEmailURL()
	assert.True(t, strings.Contains(url, "test-server"))
	assert.True(t, strings.Contains(url, "email"))
}
