// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync/atomic"

	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
)

// credentialVerdict records whether a registry accepts a catalog's credentials,
// independently of any one repository.
type credentialVerdict int

const (
	// credentialsUnverified means the check could not tell: it never presented
	// the credentials, or it failed for a reason other than a 401/403.
	credentialsUnverified credentialVerdict = iota
	// credentialsAccepted means the registry authenticated the credentials.
	credentialsAccepted
	// credentialsRejected means the registry refused the credentials.
	credentialsRejected
)

// credentialProbe checks the catalog's credentials against its registry,
// reporting whether they were presented and the outcome.
type credentialProbe func(ctx context.Context) (presented bool, err error)

// classifyCredentialProbe turns a probe outcome into a verdict. Credentials are
// accepted only when they were presented and the registry answered; a 401/403
// to presented credentials rejects them. Anything else is unverified, so a
// caller relying on the verdict fails closed.
func classifyCredentialProbe(presented bool, err error) credentialVerdict {
	switch {
	case !presented:
		return credentialsUnverified
	case err == nil:
		return credentialsAccepted
	case isRegistryDenied(err):
		return credentialsRejected
	default:
		return credentialsUnverified
	}
}

// credentialsAccepted reports whether the registry accepts this catalog's
// credentials. It tells a denial of the credentials (expired or revoked) apart
// from a denial of one repository: GHCR, for one, answers a logged-in request
// for a package that does not exist with 403 DENIED.
//
// The registry is asked on every call and nothing is cached, so the verdict
// always reflects the credentials in use now: a refreshed token is honored on
// the next lookup and an expired one stops being trusted. Callers only ask
// after a credentialed denial, so successful lookups never pay for the check.
func (c *RemoteCatalog) credentialsAccepted(ctx context.Context) bool {
	probe := c.probeCredentials
	if probe == nil {
		probe = c.pingWithCredentials
	}
	presented, err := probe(ctx)
	verdict := classifyCredentialProbe(presented, err)
	c.logger.V(1).Info("checked catalog credentials",
		"registry", c.registry, "presented", presented, "accepted", verdict == credentialsAccepted, "error", err)
	return verdict == credentialsAccepted
}

// pingWithCredentials pings the registry base endpoint (/v2/), the same check
// "docker login" makes, with the catalog's credentials. It uses a fresh token
// cache so a cached token cannot stand in for the credentials, and reports
// whether the registry actually asked for them: a registry whose base endpoint
// is open answers without authenticating anyone.
//
// Like "docker login", it asks for an unscoped token. Some registries hint a
// repository scope in the /v2/ challenge (GHCR sends
// scope="repository:user/image:pull") and deny a token for that scope to
// valid credentials, which the auth client would otherwise request; see
// unscopedChallenge.
func (c *RemoteCatalog) pingWithCredentials(ctx context.Context) (bool, error) {
	if c.client == nil || c.client.Credential == nil {
		return false, nil
	}
	reg, err := remote.NewRegistry(c.registry)
	if err != nil {
		return false, fmt.Errorf("pinging registry %q: %w", c.registry, err)
	}

	var presented atomic.Bool
	credential := c.client.Credential
	probe := *c.client
	probe.Cache = auth.NewCache()
	probe.Credential = func(ctx context.Context, host string) (auth.Credential, error) {
		cred, err := credential(ctx, host)
		if err == nil && cred != auth.EmptyCredential {
			presented.Store(true)
		}
		return cred, err
	}
	probe.Client = unscopedHTTPClient(c.client.Client)
	reg.Client = &probe

	err = reg.Ping(ctx)
	return presented.Load(), err
}

// unscopedHTTPClient returns a copy of base (http.DefaultClient when nil, as
// the auth client defaults) whose transport strips the scope from bearer
// challenges. The copy keeps base's TLS and retry behavior.
func unscopedHTTPClient(base *http.Client) *http.Client {
	if base == nil {
		base = http.DefaultClient
	}
	client := *base
	next := client.Transport
	if next == nil {
		next = http.DefaultTransport
	}
	client.Transport = unscopedChallenge{next: next}
	return &client
}

// wwwAuthenticateHeader is the response header carrying an auth challenge.
const wwwAuthenticateHeader = "Www-Authenticate"

// challengeScopeParam matches a scope parameter of an auth challenge together
// with the separator that follows it.
var challengeScopeParam = regexp.MustCompile(`(?i)\bscope\s*=\s*"[^"]*"\s*,?\s*`)

// unscopedChallenge is a transport that removes the scope parameter from the
// challenges of 401 responses. The auth client merges a challenge's scope into
// its token request; without it, the request carries no scope and the token
// endpoint authenticates only the credentials, not access to a repository.
type unscopedChallenge struct {
	next http.RoundTripper
}

// RoundTrip implements http.RoundTripper.
func (t unscopedChallenge) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.next.RoundTrip(req)
	if err != nil || resp.StatusCode != http.StatusUnauthorized {
		return resp, err
	}
	challenges := resp.Header.Values(wwwAuthenticateHeader)
	if len(challenges) == 0 {
		return resp, nil
	}
	stripped := make([]string, len(challenges))
	for i, challenge := range challenges {
		stripped[i] = stripChallengeScope(challenge)
	}
	resp.Header[wwwAuthenticateHeader] = stripped
	return resp, nil
}

// stripChallengeScope removes the scope parameter from an auth challenge.
func stripChallengeScope(challenge string) string {
	return strings.TrimRight(challengeScopeParam.ReplaceAllString(challenge, ""), ", \t")
}
