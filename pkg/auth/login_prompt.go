// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
)

// PasteBackFunc renders the host-authored paste-back prompt during an
// interactive login and returns the value the user pasted. It is installed
// into the login context (WithPasteBack) by the interactive login UI when --
// and only when -- the session can actually render the prompt and read the
// answer (an interactive terminal). Plugin-backed handlers bridge it to the
// HostService.PromptAuthResponse RPC.
//
// The returned value is the OAuth redirect URL the user's browser landed on
// (authorization code included). Implementations must never log or echo it
// beyond the user's own terminal echo, and must honor ctx cancellation so a
// localhost callback that arrives first stops the prompt without leaving a
// reader behind.
type PasteBackFunc func(ctx context.Context, authorizationURL, redirectURI string) (string, error)

// pasteBackCtxKey is the context key for the active login's paste-back prompt.
type pasteBackCtxKey struct{}

// WithPasteBack installs a paste-back prompt for the interactive login running
// under ctx. Passing a nil fn removes the prompt (the login is served
// non-interactively; PromptAuthResponse then returns Unavailable).
func WithPasteBack(ctx context.Context, fn PasteBackFunc) context.Context {
	return context.WithValue(ctx, pasteBackCtxKey{}, fn)
}

// PasteBackFromContext returns the paste-back prompt installed for the active
// login, or a nil func when the session is non-interactive.
func PasteBackFromContext(ctx context.Context) PasteBackFunc {
	if fn, ok := ctx.Value(pasteBackCtxKey{}).(PasteBackFunc); ok {
		return fn
	}
	return nil
}
