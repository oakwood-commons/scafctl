// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package hostname

import "errors"

// Sentinel errors returned by the resolver. Callers map these to exit codes:
// selector/loop/shape errors are invalid-input; credential/fetch errors are
// general failures.
var (
	// ErrSelectorNotFound indicates the hostname selector was not found in the
	// static aliases or the resolved inventory.
	ErrSelectorNotFound = errors.New("hostname: selector not found")

	// ErrResolverLoop indicates the resolver's source auth provider is the same
	// handler being resolved, which would recurse.
	ErrResolverLoop = errors.New("hostname: resolver loop detected")

	// ErrTransformShape indicates the CEL transform did not produce a list of
	// {name, url} entries.
	ErrTransformShape = errors.New("hostname: transform did not produce a list of {name, url} entries")

	// ErrNoCredentials indicates the resolver's auth provider is set but no
	// cached credentials are available. The host never triggers an interactive
	// login to satisfy a resolver.
	ErrNoCredentials = errors.New("hostname: no cached credentials for resolver auth provider")
)
