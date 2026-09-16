// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package httpc

import (
	"context"
	"errors"
	"fmt"

	upstream "github.com/oakwood-commons/httpc"

	"github.com/oakwood-commons/scafctl/pkg/config"
)

// AllowedPrivateCIDRsKey is the configuration path users set to exempt an
// address range from private-address blocking. Error messages cite it so a
// denial points at something the user can actually change, rather than at the
// Go field the upstream library names.
const AllowedPrivateCIDRsKey = "httpClient.allowedPrivateCIDRs"

// ErrBlockedByPolicy reports that a request was refused because its destination
// address is not permitted. Errors wrap it, so callers match with errors.Is.
var ErrBlockedByPolicy = upstream.ErrBlockedByPolicy

// NormalizeCIDR converts a single allowlist entry into CIDR form.
//
// Re-exported from the config package so that configuration validation and the
// runtime policy always agree on what a valid entry is.
func NormalizeCIDR(entry string) (string, error) {
	return config.NormalizeCIDR(entry)
}

// PolicyFromAppConfig derives the destination-address policy for a client from
// application configuration.
//
// The zero value denies private, loopback, and link-local addresses. Cloud
// metadata addresses are denied unconditionally and cannot be re-enabled by any
// configuration this function produces.
//
// Precedence follows the upstream library exactly, because these settings carry
// the same names there and a reader should not have to learn two rule sets:
//
//   - allowPrivateIPs: true opens every private range at once.
//   - allowedPrivateCIDRs, when present, WINS over allowPrivateIPs and narrows
//     the client to just the listed ranges. Tightening a legacy configuration by
//     adding an allowlist must actually tighten it.
//   - A present but EMPTY allowedPrivateCIDRs means "no exceptions" and still
//     overrides allowPrivateIPs. Only an absent (nil) list leaves it in force.
//
// A nil cfg yields the default deny policy.
func PolicyFromAppConfig(cfg *config.HTTPClientConfig) (*upstream.IPPolicy, error) {
	if cfg == nil {
		return &upstream.IPPolicy{}, nil
	}

	policy := &upstream.IPPolicy{}
	if cfg.AllowPrivateIPs != nil && *cfg.AllowPrivateIPs {
		policy = upstream.AllowAllPrivateIPs()
	}

	// "Set" is the test, not "non-empty": an explicitly empty list is a
	// deliberate "no exceptions" and must override allowPrivateIPs.
	if entries, set := cfg.PrivateCIDRs(); set {
		normalized := make([]string, 0, len(entries))
		for i, entry := range entries {
			cidr, err := NormalizeCIDR(entry)
			if err != nil {
				return nil, fmt.Errorf("%s[%d] %q: %w", AllowedPrivateCIDRsKey, i, entry, err)
			}
			normalized = append(normalized, cidr)
		}

		allowlisted, err := upstream.NewIPPolicy(normalized...)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", AllowedPrivateCIDRsKey, err)
		}
		policy = allowlisted
	}

	// Applies to whichever policy was resolved above, including the implicit
	// deny-all one, so it does not require an allowlist to be set.
	if cfg.TrustProxyResolution != nil {
		withTrust := *policy
		withTrust.TrustProxyResolution = *cfg.TrustProxyResolution
		policy = &withTrust
	}

	return policy, nil
}

// PolicyFromContext derives the destination-address policy from the application
// configuration carried on ctx.
//
// Missing configuration and an unusable allowlist both yield the default deny
// policy, so a configuration problem narrows what a client may reach rather
// than widening it. Callers that need to report why an allowlist was rejected
// should use PolicyFromAppConfig directly; configuration validation already
// rejects a malformed allowlist at startup.
func PolicyFromContext(ctx context.Context) *upstream.IPPolicy {
	appCfg := config.FromContext(ctx)
	if appCfg == nil {
		return &upstream.IPPolicy{}
	}
	policy, err := PolicyFromAppConfig(&appCfg.HTTPClient)
	if err != nil {
		return &upstream.IPPolicy{}
	}
	return policy
}

// ExplainBlocked annotates a destination-address denial with the configuration
// key that would permit it, and returns every other error unchanged.
//
// The upstream message names a Go struct field, which is accurate for a library
// consumer and useless to someone editing a configuration file. Call this where
// an error reaches the user.
func ExplainBlocked(err error) error {
	if err == nil || !errors.Is(err, ErrBlockedByPolicy) {
		return err
	}
	return fmt.Errorf("%w (to permit this destination, add its address range to %s; "+
		"cloud metadata addresses can never be permitted)", err, AllowedPrivateCIDRsKey)
}
