// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/settings"
)

// StartupWarnings reports configuration that materially widens what a caller of
// this server can reach, as messages meant for a human operator.
//
// These are returned rather than logged. A logger is configurable -- this
// project's default logging level is "none", which discards Info entirely -- so
// a security warning written through it reaches nobody in the default
// configuration. Returning the text lets the caller put it somewhere a human
// actually sees, which for the CLI means stderr, on the same reasoning kubectl,
// helm, docker, and gh all apply: stdout carries machine-readable output and
// must stay clean, while a warning about exposure must not be suppressible by a
// log-level setting that exists to quiet operational noise.
//
// An embedder that runs the server without this project's CLI should surface
// these the same way.
func StartupWarnings(cfg *config.Config) []string {
	if cfg == nil {
		return nil
	}

	var warnings []string
	apiCfg := cfg.APIServer

	host := apiCfg.Host
	if host == "" {
		host = settings.DefaultAPIHost
	}

	// Warn when the server is reachable beyond this machine without
	// authentication. The API executes caller-submitted solutions by design, so
	// binding a non-loopback address with auth disabled exposes that capability
	// to anyone who can reach the port. The default (127.0.0.1) is safe; this
	// fires only when an operator has explicitly widened the bind address.
	if !isLoopbackHost(host) && !apiCfg.Auth.AzureOIDC.Enabled {
		// The admin prefix follows the configured API version, so build the
		// remediation path from it -- a hardcoded "/v1/admin/" would tell an
		// operator running apiVersion: v2 to block the wrong route.
		apiVersion := apiCfg.APIVersion
		if apiVersion == "" {
			apiVersion = settings.DefaultAPIVersion
		}
		adminPrefix := "/" + apiVersion + "/admin/"

		warnings = append(warnings, "API server is binding a non-loopback address ("+host+
			") with authentication DISABLED. This exposes solution execution to any caller that can reach this port. "+
			"Enable apiServer.auth.azureOIDC, or bind 127.0.0.1 and front the server with an authenticating proxy. "+
			"If you use a same-host proxy, also block "+adminPrefix+" at the proxy: with auth disabled the admin gate "+
			"falls back to a loopback peer-address check, which a same-host proxy makes indistinguishable from a "+
			"local caller.")
	}

	// The destination-address policy is global config, shared with the CLI, and
	// can be set from the environment as well as the config file. That is a
	// reasonable local-development convenience, but the two cases are not
	// equivalent: on a workstation it grants the operator access they already
	// have, while on a server that fetches caller-supplied URLs it lends the
	// server's network position to whoever supplies them. Announce it, so a
	// setting inherited from a local config or a stray environment variable
	// cannot widen a deployment silently.
	//
	// Only the blanket flag warrants this. A narrow allowedPrivateCIDRs list is
	// the recommended way to reach an internal host and is deliberate by
	// construction, so warning on it would train operators to ignore the
	// warning that matters.
	if cfg.HTTPClient.AllowPrivateIPs != nil && *cfg.HTTPClient.AllowPrivateIPs {
		warnings = append(warnings, "httpClient.allowPrivateIPs is enabled, so this server may fetch URLs that "+
			"resolve into private, loopback, and link-local address space. On a server handling caller-supplied "+
			"URLs this exposes internal services. Prefer httpClient.allowedPrivateCIDRs, which permits only the "+
			"ranges you name. (Cloud metadata addresses remain blocked regardless.)")
	}

	return warnings
}

// StartupWarnings reports this server's configuration warnings. See the
// package-level StartupWarnings for why they are returned rather than logged.
func (s *Server) StartupWarnings() []string {
	return StartupWarnings(s.cfg)
}
