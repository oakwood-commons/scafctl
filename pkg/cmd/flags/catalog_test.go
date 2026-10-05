// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package flags

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAddCatalogFlag(t *testing.T) {
	var catalog string
	cmd := &cobra.Command{Use: "test"}
	AddCatalogFlag(cmd, &catalog)

	f := cmd.Flags().Lookup(CatalogFlagName)
	require.NotNil(t, f)
	assert.Equal(t, CatalogFlagShorthand, f.Shorthand)
	assert.Equal(t, CatalogFlagUsage, f.Usage)
	assert.Empty(t, f.DefValue)

	require.NoError(t, cmd.ParseFlags([]string{"-c", "official"}))
	assert.Equal(t, "official", catalog)
}
