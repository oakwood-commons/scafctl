// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package credentialhelper implements the scafctl credential-helper CLI
// commands, exposing scafctl's encrypted credential store via the Docker
// credential helper protocol.
package credentialhelper

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/credentialhelper"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/secrets"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
)

// CommandCredentialHelper returns the credential-helper command group.
func CommandCredentialHelper(_ *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "credential-helper",
		Short: "Docker/Podman credential helper protocol",
		Long: `Implements the Docker credential helper protocol, exposing scafctl's
encrypted credential store (AES-256-GCM) to Docker, Podman, Buildah,
and any OCI client.

Configure Docker to use scafctl as a credential helper:
  scafctl credential-helper install --docker

Or add manually to ~/.docker/config.json:
  { "credsStore": "scafctl" }`,
	}

	cmd.AddCommand(commandGet())
	cmd.AddCommand(commandStore())
	cmd.AddCommand(commandErase())
	cmd.AddCommand(commandList())
	cmd.AddCommand(commandInstall(ioStreams))
	cmd.AddCommand(commandUninstall(ioStreams))

	return cmd
}

func newHelper() (*credentialhelper.Helper, error) {
	store, err := secrets.New()
	if err != nil {
		return nil, fmt.Errorf("initialize secrets store: %w", err)
	}
	// Inject the already-initialised secrets store so the native credential store
	// uses the same backend as the credential helper, avoiding a second keyring init.
	nativeStore := catalog.NewNativeCredentialStoreWithSecretsStore(store)
	return credentialhelper.New(store, credentialhelper.WithNativeStore(nativeStore)), nil
}

func commandGet() *cobra.Command {
	return &cobra.Command{
		Use:   "get",
		Short: "Get credentials for a registry",
		Long:  "Reads a server URL from stdin and writes credentials as JSON to stdout.",
		Args:  cobra.NoArgs,
		// Silence usage on errors - credential helpers should only output JSON
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			input, err := io.ReadAll(io.LimitReader(os.Stdin, credentialhelper.MaxInputSize))
			if err != nil {
				return writeError(os.Stdout, "failed to read input")
			}
			helper, err := newHelper()
			if err != nil {
				return writeError(os.Stdout, err.Error())
			}
			cred, err := helper.Get(cmd.Context(), string(input))
			if err != nil {
				return writeError(os.Stdout, err.Error())
			}
			return json.NewEncoder(os.Stdout).Encode(cred)
		},
	}
}

func commandStore() *cobra.Command {
	return &cobra.Command{
		Use:           "store",
		Short:         "Store credentials for a registry",
		Long:          "Reads a JSON credential object from stdin and stores it.",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			input, err := io.ReadAll(io.LimitReader(os.Stdin, credentialhelper.MaxInputSize))
			if err != nil {
				return writeError(os.Stdout, "failed to read input")
			}
			var cred credentialhelper.Credential
			if err := json.Unmarshal(input, &cred); err != nil {
				return writeError(os.Stdout, "invalid JSON input")
			}
			helper, err := newHelper()
			if err != nil {
				return writeError(os.Stdout, err.Error())
			}
			if err := helper.Store(cmd.Context(), &cred); err != nil {
				return writeError(os.Stdout, err.Error())
			}
			return nil
		},
	}
}

func commandErase() *cobra.Command {
	return &cobra.Command{
		Use:           "erase",
		Short:         "Erase credentials for a registry",
		Long:          "Reads a server URL from stdin and removes credentials for that registry.",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			input, err := io.ReadAll(io.LimitReader(os.Stdin, credentialhelper.MaxInputSize))
			if err != nil {
				return writeError(os.Stdout, "failed to read input")
			}
			helper, err := newHelper()
			if err != nil {
				return writeError(os.Stdout, err.Error())
			}
			if err := helper.Erase(cmd.Context(), string(input)); err != nil {
				return writeError(os.Stdout, err.Error())
			}
			return nil
		},
	}
}

func commandList() *cobra.Command {
	return &cobra.Command{
		Use:           "list",
		Short:         "List all stored credentials",
		Long:          "Writes a JSON map of server URLs to usernames to stdout.",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			helper, err := newHelper()
			if err != nil {
				return writeError(os.Stdout, err.Error())
			}
			result, err := helper.List(cmd.Context())
			if err != nil {
				return writeError(os.Stdout, err.Error())
			}
			return json.NewEncoder(os.Stdout).Encode(result)
		},
	}
}

// writeError writes a Docker credential helper error response to w and returns
// an error to signal non-zero exit. The error message is intentionally NOT
// printed by cobra because SilenceErrors is set.
func writeError(w io.Writer, message string) error {
	resp := credentialhelper.ErrorResponse{Message: message}
	_ = json.NewEncoder(w).Encode(resp)
	return exitcode.WithCode(fmt.Errorf("%s", message), exitcode.GeneralError)
}
