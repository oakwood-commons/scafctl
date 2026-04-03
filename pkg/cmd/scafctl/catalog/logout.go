// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"fmt"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// LogoutOptions holds options for the catalog logout command.
type LogoutOptions struct {
	Registry  string
	All       bool
	CliParams *settings.Run
	IOStreams *terminal.IOStreams
}

// CommandLogout creates the 'catalog logout' command.
func CommandLogout(cliParams *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
	options := &LogoutOptions{
		CliParams: cliParams,
		IOStreams: ioStreams,
	}

	cmd := &cobra.Command{
		Use:   "logout [registry]",
		Short: "Remove stored credentials for an OCI registry",
		Long: heredoc.Doc(`
			Remove stored credentials for an OCI registry.

			This command removes credentials from the scafctl native credential
			store. If the credentials were also written to a container auth file
			(Docker/Podman config), those entries are cleaned up as well.

			Use --all to remove all stored registry credentials at once.

			Examples:
			  # Remove credentials for a specific registry
			  scafctl catalog logout ghcr.io

			  # Remove all stored registry credentials
			  scafctl catalog logout --all
		`),
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				options.Registry = args[0]
			}
			if options.Registry == "" && !options.All {
				w := writer.FromContext(cmd.Context())
				err := fmt.Errorf("specify a registry or use --all to remove all credentials")
				if w != nil {
					w.Errorf("%v", err)
				}
				return exitcode.WithCode(err, exitcode.InvalidInput)
			}
			return runCatalogLogout(cmd.Context(), options)
		},
	}

	cmd.Flags().BoolVar(&options.All, "all", false, "Remove all stored registry credentials")

	return cmd
}

func runCatalogLogout(ctx context.Context, opts *LogoutOptions) error {
	w := writer.FromContext(ctx)
	if w == nil {
		return fmt.Errorf("writer not initialized in context")
	}

	nativeStore := catalog.NewNativeCredentialStore()

	if opts.All {
		return logoutAll(w, nativeStore)
	}

	return logoutRegistry(w, nativeStore, opts.Registry)
}

func logoutAll(w *writer.Writer, nativeStore *catalog.NativeCredentialStore) error {
	creds, err := nativeStore.ListCredentialEntries()
	if err != nil {
		err = fmt.Errorf("failed to list credentials: %w", err)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.GeneralError)
	}

	if len(creds) == 0 {
		w.Info("No stored registry credentials found")
		return nil
	}

	// Clean up container auth entries first
	for host, entry := range creds {
		if entry.ContainerAuth {
			if containerErr := nativeStore.DeleteContainerAuth(host); containerErr != nil {
				w.Warningf("Failed to clean container auth for %s: %v", host, containerErr)
			}
		}
	}

	if err := nativeStore.DeleteAll(); err != nil {
		err = fmt.Errorf("failed to delete all credentials: %w", err)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.GeneralError)
	}

	w.Infof("Removed credentials for %d registries", len(creds))
	return nil
}

func logoutRegistry(w *writer.Writer, nativeStore *catalog.NativeCredentialStore, registry string) error {
	// Check if credential exists
	creds, err := nativeStore.ListCredentialEntries()
	if err != nil {
		err = fmt.Errorf("failed to list credentials: %w", err)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.GeneralError)
	}

	entry, exists := creds[registry]
	if !exists {
		err := fmt.Errorf("no credentials stored for %s", registry)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	// Clean up container auth if it was written there
	if entry.ContainerAuth {
		if containerErr := nativeStore.DeleteContainerAuth(registry); containerErr != nil {
			w.Warningf("Failed to clean container auth for %s: %v", registry, containerErr)
		}
	}

	if err := nativeStore.DeleteCredential(registry); err != nil {
		err = fmt.Errorf("failed to delete credentials for %s: %w", registry, err)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.GeneralError)
	}

	w.Infof("Logout succeeded for %s", registry)
	return nil
}
