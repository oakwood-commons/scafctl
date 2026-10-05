// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package flags

import "github.com/spf13/cobra"

const (
	// CatalogFlagName is the long name of the shared --catalog flag.
	CatalogFlagName = "catalog"
	// CatalogFlagShorthand is the shorthand of the shared --catalog flag.
	CatalogFlagShorthand = "c"
	// CatalogFlagUsage is the default help text for the shared --catalog flag.
	CatalogFlagUsage = "Catalog to resolve a solution name from (registry URL or configured catalog name). If not specified, uses the default resolution order."
)

// AddCatalogFlag adds the shared -c/--catalog flag to cmd, storing the value in
// catalog.
func AddCatalogFlag(cmd *cobra.Command, catalog *string) {
	cmd.Flags().StringVarP(catalog, CatalogFlagName, CatalogFlagShorthand, "", CatalogFlagUsage)
}
