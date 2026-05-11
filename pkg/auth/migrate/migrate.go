// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package migrate provides domain logic for migrating built-in auth handlers
// to plugin-based handlers.
package migrate

import (
	"context"
	"fmt"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	authofficial "github.com/oakwood-commons/scafctl/pkg/auth/official"
	"github.com/oakwood-commons/scafctl/pkg/plugin"
	"github.com/oakwood-commons/scafctl/pkg/solution"
)

// Status represents the migration status of a single handler.
type Status string

const (
	// StatusReady indicates the handler is fully migrated and ready.
	StatusReady Status = "READY"

	// StatusFailed indicates the handler migration failed.
	StatusFailed Status = "FAILED"
)

// Result holds the migration result for a single handler.
type Result struct {
	Name         string `json:"name"         yaml:"name"`
	PluginSource string `json:"pluginSource" yaml:"pluginSource"`
	TokenMessage string `json:"tokenMessage" yaml:"tokenMessage"`
	Status       Status `json:"status"       yaml:"status"`
	ErrorMessage string `json:"errorMessage,omitempty" yaml:"errorMessage,omitempty"`
}

// FetchFunc is the signature for the function that fetches plugins.
type FetchFunc func(ctx context.Context, deps []solution.PluginDependency) ([]plugin.FetchResult, error)

// Handlers performs the migration for all official auth handlers.
// It fetches plugin binaries via fetchFn and validates cached token
// accessibility for each handler.
func Handlers(ctx context.Context, officialReg *authofficial.Registry, authReg *auth.Registry, fetchFn FetchFunc) []Result {
	names := officialReg.Names()
	results := make([]Result, 0, len(names))

	// Build plugin dependencies for all official handlers.
	deps := make([]solution.PluginDependency, 0, len(names))
	for _, name := range names {
		handler := officialReg.MustGet(name)
		deps = append(deps, handler.ToPluginDependency())
	}

	// Fetch all plugins.
	fetchResults, fetchErr := fetchFn(ctx, deps)

	// Build a map of fetch results by name for lookup.
	fetchMap := make(map[string]*plugin.FetchResult, len(fetchResults))
	for i := range fetchResults {
		fetchMap[fetchResults[i].Name] = &fetchResults[i]
	}

	for _, name := range names {
		handler := officialReg.MustGet(name)
		result := Result{Name: name}

		fr, fetched := fetchMap[handler.CatalogRef]
		if fetchErr != nil && !fetched {
			result.PluginSource = fmt.Sprintf("FAILED (%v)", fetchErr)
			result.Status = StatusFailed
			result.ErrorMessage = fetchErr.Error()
			result.TokenMessage = "skipped (plugin not available)"
			results = append(results, result)
			continue
		}

		if fetched {
			if fr.FromCache {
				result.PluginSource = fmt.Sprintf("cached (%s)", fr.Version)
			} else {
				result.PluginSource = fmt.Sprintf("installed (%s)", fr.Version)
			}
		} else {
			result.PluginSource = "FAILED (not found in catalog)"
			result.Status = StatusFailed
			result.ErrorMessage = fmt.Sprintf("handler %q not found in catalog", name)
			result.TokenMessage = "skipped (plugin not available)"
			results = append(results, result)
			continue
		}

		// Validate token migration by checking cached tokens.
		tokenMsg, tokenOK := validateTokenMigration(ctx, authReg, name)
		result.TokenMessage = tokenMsg
		if tokenOK {
			result.Status = StatusReady
		} else {
			result.Status = StatusFailed
			result.ErrorMessage = tokenMsg
		}
		results = append(results, result)
	}

	return results
}

// validateTokenMigration checks if cached tokens are accessible for a handler.
// It returns a human-readable message describing the token state and a boolean
// indicating whether the check passed (true) or encountered an error (false).
func validateTokenMigration(ctx context.Context, authReg *auth.Registry, name string) (string, bool) {
	if authReg == nil || !authReg.Has(name) {
		return "no cached tokens (login required after migration)", true
	}

	handler, err := authReg.Get(name)
	if err != nil {
		return "no cached tokens (login required after migration)", true
	}

	lister, ok := handler.(auth.TokenLister)
	if !ok {
		return "no cached tokens (login required after migration)", true
	}

	tokens, err := lister.ListCachedTokens(ctx)
	if err != nil {
		return fmt.Sprintf("could not read cached tokens: %v", err), false
	}

	if len(tokens) == 0 {
		return "no cached tokens (login required after migration)", true
	}

	valid := 0
	for _, t := range tokens {
		if !t.IsExpired {
			valid++
		}
	}

	if valid == len(tokens) {
		return fmt.Sprintf("%d cached token(s) validated successfully", len(tokens)), true
	}

	return fmt.Sprintf("%d cached token(s), %d expired", len(tokens), len(tokens)-valid), true
}

// IsReady returns true if the result indicates a successful migration.
func (r *Result) IsReady() bool {
	return r.Status == StatusReady
}

// StatusString returns the status as a string for display.
func (r *Result) StatusString() string {
	return string(r.Status)
}
