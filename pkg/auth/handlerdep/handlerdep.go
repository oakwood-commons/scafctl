// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package handlerdep decides how an auth handler name maps to a plugin
// dependency for catalog resolution. It centralizes the precedence rules —
// config pin, official allowlist, then bare catalog name — used by the auth
// registry fallback resolver and the CLI auth commands.
//
// Resolve performs NO network or filesystem I/O; it only decides the
// dependency shape and reports which source produced it. The caller is
// responsible for actually fetching and registering the plugin.
package handlerdep

import (
	"context"
	"errors"

	authofficial "github.com/oakwood-commons/scafctl/pkg/auth/official"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/solution"
)

// defaultVersion is applied when neither a config pin nor an official entry
// specifies a version constraint. "latest" lets the catalog resolver pick the
// newest available version.
const defaultVersion = "latest"

// Source records where a handler dependency was resolved from.
type Source int

const (
	// SourceUnknown is the zero value, used when resolution fails.
	SourceUnknown Source = iota
	// SourceConfigPin means the dependency came from auth.handlers.<name>.plugin.
	SourceConfigPin
	// SourceOfficial means the name matched the official handler allowlist.
	SourceOfficial
	// SourceCatalog means the name resolves as a bare catalog artifact name.
	SourceCatalog
)

// String returns a human-readable name for the source.
func (s Source) String() string {
	switch s {
	case SourceUnknown:
		return "unknown"
	case SourceConfigPin:
		return "config-pin"
	case SourceOfficial:
		return "official"
	case SourceCatalog:
		return "catalog"
	default:
		return "unknown"
	}
}

// ErrThirdPartyDisabled is returned by Resolve when the name resolves to a
// non-official (catalog or config-pinned) handler but third-party auth handler
// resolution is disabled via settings.disableThirdPartyAuthHandlers.
var ErrThirdPartyDisabled = errors.New("third-party auth handlers are disabled")

// ErrOfficialDisabled is returned by Resolve when the name matches an official
// handler but official auth handler resolution is disabled via
// settings.disableOfficialAuthHandlers.
var ErrOfficialDisabled = errors.New("official auth handlers are disabled")

// Resolve maps an auth handler name to the plugin dependency that should be
// fetched from a configured catalog, and reports the source that produced it.
//
// Precedence:
//  1. Config pin (auth.handlers.<name>.plugin) — explicit user mapping.
//  2. Official allowlist — first-party trust anchor.
//  3. Bare catalog name — resolve the name directly as a catalog artifact.
//
// Resolve honors the disable switches: an official name is rejected with
// ErrOfficialDisabled when settings.disableOfficialAuthHandlers is set, and a
// non-official (config-pinned or catalog) name is rejected with
// ErrThirdPartyDisabled when settings.disableThirdPartyAuthHandlers is set.
func Resolve(ctx context.Context, name string) (solution.PluginDependency, Source, error) {
	cfg := config.FromContext(ctx)

	source, dep := decide(ctx, name, cfg)

	if err := gate(source, cfg); err != nil {
		return solution.PluginDependency{}, SourceUnknown, err
	}
	return dep, source, nil
}

// IsKnown reports whether name is a resolvable official handler or config pin
// under the current policy. It performs no catalog probe, so a bare catalog
// name that exists only in a remote catalog returns false. It honors the
// disable switches: a config pin is not "known" when
// settings.disableThirdPartyAuthHandlers is set, and an official name is not
// "known" when settings.disableOfficialAuthHandlers is set -- matching what
// Resolve would allow. Use this for cheap pre-validation and shell completion,
// not as an authoritative existence check.
func IsKnown(ctx context.Context, name string) bool {
	cfg := config.FromContext(ctx)
	if cfg != nil {
		if hc, ok := cfg.Auth.Handlers[name]; ok && hc.Plugin != nil {
			// Config pins are third-party; policy may forbid resolving them.
			return !cfg.Settings.DisableThirdPartyAuthHandlers
		}
	}
	if reg := authofficial.RegistryFromContext(ctx); reg != nil && reg.Has(name) {
		return cfg == nil || !cfg.Settings.DisableOfficialAuthHandlers
	}
	return false
}

// decide selects the source and dependency shape by precedence, ignoring the
// disable switches (applied separately by gate).
func decide(ctx context.Context, name string, cfg *config.Config) (Source, solution.PluginDependency) {
	if cfg != nil {
		if hc, ok := cfg.Auth.Handlers[name]; ok && hc.Plugin != nil {
			ref := hc.Plugin.Ref
			if ref == "" {
				ref = name
			}
			version := hc.Plugin.Version
			if version == "" {
				version = defaultVersion
			}
			return SourceConfigPin, solution.PluginDependency{
				Name:    ref,
				Kind:    solution.PluginKindAuthHandler,
				Version: version,
			}
		}
	}

	if h, ok := officialEntry(ctx, name); ok {
		return SourceOfficial, h.ToPluginDependency()
	}

	return SourceCatalog, solution.PluginDependency{
		Name:    name,
		Kind:    solution.PluginKindAuthHandler,
		Version: defaultVersion,
	}
}

// officialEntry reports whether name is an official auth handler and returns
// its catalog entry. It consults the official registry from context when
// present (authoritative, including embedder overrides via
// RootOptions.OfficialAuthHandlers). When no registry is attached -- which
// happens when settings.disableOfficialAuthHandlers is set -- it falls back to
// the canonical default official names so the disable gate can still classify
// and block official handlers instead of letting them resolve as bare catalog
// artifacts.
func officialEntry(ctx context.Context, name string) (authofficial.AuthHandler, bool) {
	if reg := authofficial.RegistryFromContext(ctx); reg != nil {
		return reg.Get(name)
	}
	for _, h := range authofficial.DefaultAuthHandlers() {
		if h.Name == name {
			return h, true
		}
	}
	return authofficial.AuthHandler{}, false
}

// gate applies the disable switches to a resolved source.
func gate(source Source, cfg *config.Config) error {
	if cfg == nil {
		return nil
	}
	switch source {
	case SourceOfficial:
		if cfg.Settings.DisableOfficialAuthHandlers {
			return ErrOfficialDisabled
		}
	case SourceConfigPin, SourceCatalog:
		if cfg.Settings.DisableThirdPartyAuthHandlers {
			return ErrThirdPartyDisabled
		}
	case SourceUnknown:
	}
	return nil
}
