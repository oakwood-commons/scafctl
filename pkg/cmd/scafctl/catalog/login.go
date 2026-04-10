// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/secrets"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// LoginOptions holds options for the catalog login command.
type LoginOptions struct {
	BinaryName        string
	Registry          string
	AuthProvider      string
	Scope             string
	Username          string
	PasswordStdin     bool
	PasswordEnv       string
	WriteRegistryAuth bool
	CliParams         *settings.Run
	IOStreams         *terminal.IOStreams
}

// CommandLogin creates the 'catalog login' command.
func CommandLogin(cliParams *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
	options := &LoginOptions{
		CliParams: cliParams,
		IOStreams: ioStreams,
	}

	cmd := &cobra.Command{
		Use:   "login <registry>",
		Short: "Authenticate to an OCI registry",
		Long: strings.ReplaceAll(heredoc.Doc(`
			Authenticate to an OCI registry for catalog operations.

			This command stores credentials for registry access without requiring
			Docker or Podman to be installed. It supports two authentication modes:

			Mode 1 — Auth handler bridge (cloud registries):
			  Bridges an existing scafctl auth handler token to registry credentials.
			  The auth handler is auto-detected from the registry host or can be
			  specified with --auth-provider.

			  OAuth scope is auto-detected from the matching catalog remote's
			  authScope field when --scope is not provided. Use --scope to override.

			  Requires prior authentication: scafctl auth login <handler>

			Mode 2 — Direct credentials:
			  Authenticates with a username and password/token. The password is read
			  from stdin (--password-stdin) or an environment variable (--password-env).

			Auto-detected registries:
			  - ghcr.io           → github handler
			  - *.pkg.dev, gcr.io → gcp handler
			  - *.azurecr.io      → entra handler

			Examples:
			  # Login to GHCR using GitHub auth handler (auto-detected)
			  scafctl auth login github
			  scafctl catalog login ghcr.io

			  # Login to GCR using GCP auth handler
			  scafctl auth login gcp
			  scafctl catalog login us-docker.pkg.dev

			  # Login to ACR using Entra auth handler
			  scafctl auth login entra
			  scafctl catalog login myacr.azurecr.io

			  # Login with explicit auth provider
			  scafctl catalog login quay.io --auth-provider quay

			  # Login with direct credentials (e.g. Docker Hub, robot accounts)
			  echo TOKEN | scafctl catalog login quay.io --username myorg+deployer --password-stdin

			  # Login with password from environment variable (CI/automation)
			  scafctl catalog login quay.io --username admin --password-env REGISTRY_PASSWORD

			  # Login and also write to container auth file (Docker/Podman interop)
			  scafctl catalog login ghcr.io --write-registry-auth
		`), settings.CliBinaryName, cliParams.BinaryName),
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			options.BinaryName = cliParams.BinaryName
			options.Registry = args[0]
			return runCatalogLogin(cmd.Context(), options)
		},
	}

	cmd.Flags().StringVar(&options.AuthProvider, "auth-provider", "", "Auth handler name (e.g. github, gcp, entra). Auto-detected for known registries.")
	cmd.Flags().StringVar(&options.Scope, "scope", "", "OAuth scope for auth provider token requests (auto-detected from catalog config's authScope if not set)")
	cmd.Flags().StringVar(&options.Username, "username", "", "Username for direct credential login (triggers direct mode)")
	cmd.Flags().BoolVar(&options.PasswordStdin, "password-stdin", false, "Read password from stdin (required with --username)")
	cmd.Flags().StringVar(&options.PasswordEnv, "password-env", "", "Read password from named environment variable (alternative to --password-stdin)")
	cmd.Flags().BoolVar(&options.WriteRegistryAuth, "write-registry-auth", false, "Also write credentials to container auth file for Docker/Podman interop")

	return cmd
}

func runCatalogLogin(ctx context.Context, opts *LoginOptions) error {
	if opts.BinaryName == "" {
		opts.BinaryName = settings.CliBinaryName
	}

	w := writer.FromContext(ctx)
	if w == nil {
		return fmt.Errorf("writer not initialized in context")
	}

	// Mode 2: Direct credentials
	if opts.Username != "" {
		return runDirectCredentialLogin(ctx, w, opts)
	}

	// Mode 1: Auth handler bridge
	return runAuthHandlerLogin(ctx, w, opts)
}

// runDirectCredentialLogin handles login with explicit username/password.
func runDirectCredentialLogin(ctx context.Context, w *writer.Writer, opts *LoginOptions) error {
	_ = ctx // ctx reserved for future use

	password, err := readPassword(opts)
	if err != nil {
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	// Explicitly initialise secrets store so encryption is available.
	var nativeStore *catalog.NativeCredentialStore
	if ss, ssErr := secrets.New(); ssErr == nil {
		nativeStore = catalog.NewNativeCredentialStoreWithSecretsStore(ss)
	} else {
		nativeStore = catalog.NewNativeCredentialStore()
	}

	containerAuthFile := ""
	if opts.WriteRegistryAuth {
		if writtenPath, containerErr := nativeStore.WriteContainerAuth(opts.Registry, opts.Username, password); containerErr != nil {
			w.Warningf("Failed to write container auth file: %v", containerErr)
			w.Warning("Docker/Podman interop may not work.")
		} else {
			containerAuthFile = writtenPath
		}
	}

	if err := nativeStore.SetCredential(opts.Registry, opts.Username, password, containerAuthFile); err != nil {
		err = fmt.Errorf("failed to store credentials: %w", err)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.GeneralError)
	}

	w.Infof("Login succeeded for %s", opts.Registry)
	return nil
}

// runAuthHandlerLogin handles login using an auth handler bridge.
func runAuthHandlerLogin(ctx context.Context, w *writer.Writer, opts *LoginOptions) error {
	handlerName := opts.AuthProvider

	// Auto-detect handler from registry host
	if handlerName == "" {
		var customHandlers []config.CustomOAuth2Config
		if cfg := config.FromContext(ctx); cfg != nil {
			customHandlers = cfg.Auth.CustomOAuth2
		}
		handlerName = catalog.InferAuthHandler(opts.Registry, customHandlers)
	}

	if handlerName == "" {
		err := fmt.Errorf("no auth handler found for %q — use --username/--password-stdin for direct credentials, or --auth-provider to specify a handler", opts.Registry)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	// Resolve scope from catalog config if not provided via --scope
	scope := opts.Scope
	if scope == "" {
		if cfg := config.FromContext(ctx); cfg != nil {
			for _, cat := range cfg.Catalogs {
				catRegistry, _ := catalog.ParseCatalogURL(cat.URL)
				if catRegistry == opts.Registry && cat.AuthScope != "" {
					scope = cat.AuthScope
					break
				}
			}
		}
	}

	// Get handler from registry
	handler, err := auth.GetHandler(ctx, handlerName)
	if err != nil {
		err = fmt.Errorf("auth handler %q not available (did you run '%s auth login %s'?): %w", handlerName, opts.BinaryName, handlerName, err)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.GeneralError)
	}

	// Bridge auth handler token to registry credentials
	username, password, err := catalog.BridgeAuthToRegistry(ctx, handler, opts.Registry, scope)
	if err != nil {
		err = fmt.Errorf("failed to bridge auth to registry: %w", err)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.GeneralError)
	}

	// Explicitly initialise secrets store so encryption is available.
	var nativeStore *catalog.NativeCredentialStore
	if ss, ssErr := secrets.New(); ssErr == nil {
		nativeStore = catalog.NewNativeCredentialStoreWithSecretsStore(ss)
	} else {
		nativeStore = catalog.NewNativeCredentialStore()
	}

	containerAuthFile := ""
	if opts.WriteRegistryAuth {
		if writtenPath, containerErr := nativeStore.WriteContainerAuth(opts.Registry, username, password); containerErr != nil {
			w.Warningf("Failed to write container auth file: %v", containerErr)
			w.Warning("Docker/Podman interop may not work.")
		} else {
			containerAuthFile = writtenPath
		}
	}

	if err := nativeStore.SetCredential(opts.Registry, username, password, containerAuthFile); err != nil {
		err = fmt.Errorf("failed to store credentials: %w", err)
		w.Errorf("%v", err)
		return exitcode.WithCode(err, exitcode.GeneralError)
	}

	// Display success with identity info
	status, statusErr := handler.Status(ctx)
	if statusErr == nil && status.Claims != nil {
		identity := status.Claims.DisplayIdentity()
		if identity != "" {
			w.Infof("Login succeeded for %s (authenticated as %s via %s)", opts.Registry, identity, handlerName)
			return nil
		}
	}

	w.Infof("Login succeeded for %s (via %s handler)", opts.Registry, handlerName)
	return nil
}

// readPassword reads the password from stdin or an environment variable.
func readPassword(opts *LoginOptions) (string, error) {
	if opts.PasswordStdin && opts.PasswordEnv != "" {
		return "", fmt.Errorf("cannot use both --password-stdin and --password-env")
	}

	if !opts.PasswordStdin && opts.PasswordEnv == "" {
		return "", fmt.Errorf("--password-stdin or --password-env is required with --username")
	}

	if opts.PasswordEnv != "" {
		password := os.Getenv(opts.PasswordEnv)
		if password == "" {
			return "", fmt.Errorf("environment variable %q is empty or not set", opts.PasswordEnv)
		}
		return password, nil
	}

	// Read from stdin
	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return "", fmt.Errorf("read password from stdin: %w", err)
		}
		return "", fmt.Errorf("no password provided on stdin")
	}

	password := strings.TrimSpace(scanner.Text())
	if password == "" {
		return "", fmt.Errorf("password from stdin is empty")
	}

	return password, nil
}
