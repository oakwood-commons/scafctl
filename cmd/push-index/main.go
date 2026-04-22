// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// push-index pushes the official catalog index artifact to GHCR.
// This enables unauthenticated users to discover available packages.
//
// Usage: go run ./cmd/push-index
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/go-logr/logr"
	githubauth "github.com/oakwood-commons/scafctl/pkg/auth/github"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	logger := logr.Discard()

	// Use scafctl's GitHub auth handler (device flow + token cache).
	ghHandler, err := githubauth.New(
		githubauth.WithLogger(logger),
	)
	if err != nil {
		return fmt.Errorf("creating GitHub auth handler: %w", err)
	}

	// Create credential store from docker/gh credentials.
	credStore, err := catalog.NewCredentialStore(logger)
	if err != nil {
		return fmt.Errorf("creating credential store: %w", err)
	}

	// Create the remote catalog with the GitHub auth handler wired in.
	// This uses BridgeAuthToRegistry internally to convert the GitHub
	// token into OCI registry credentials for GHCR.
	cat, err := catalog.NewRemoteCatalog(catalog.RemoteCatalogConfig{
		Name:            "official",
		Registry:        "ghcr.io",
		Repository:      "oakwood-commons",
		CredentialStore: credStore,
		AuthHandler:     ghHandler,
		Logger:          logger,
	})
	if err != nil {
		return fmt.Errorf("creating remote catalog: %w", err)
	}

	// The artifacts to publish in the index.
	// Update this list when adding packages to the official catalog.
	artifacts := []catalog.DiscoveredArtifact{
		{Kind: catalog.ArtifactKindSolution, Name: "hello-world"},
	}

	fmt.Fprintln(os.Stdout, "Pushing catalog index to ghcr.io/oakwood-commons/catalog-index:latest")
	for _, a := range artifacts {
		fmt.Fprintf(os.Stdout, "  - %s/%s\n", a.Kind, a.Name)
	}

	if err := cat.PushIndex(ctx, artifacts); err != nil {
		return fmt.Errorf("pushing catalog index: %w", err)
	}

	fmt.Fprintln(os.Stdout, "Done!")
	return nil
}
