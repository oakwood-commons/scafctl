// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"sync"

	"github.com/oakwood-commons/scafctl/pkg/auth"
)

// AuthPromptBroker gates HostService.PromptAuthResponse callbacks to the
// window in which a single auth handler's Login is in progress, and carries
// the interactive prompt function installed by the login UI for that login.
//
// The login wrapper (AuthHandlerWrapper.Login) opens the window via Begin
// right before invoking the plugin's Login RPC and closes it when the RPC
// returns; the HostServiceServer consults Prompt on every PromptAuthResponse
// call. A broker instance is shared by value-copies of HostServiceDeps
// (the pointer survives copies made by WithSecretScope), so the wrapper and
// the HostServiceServer for the same plugin client always see the same one.
//
// A nil prompt function on an active window means "login in progress, but
// the session is not interactive" -- the RPC returns Unavailable.
//
// ponytail: single active window per broker; concurrent logins of different
// handlers in one process (server mode) do not each get a window -- last
// Begin wins and the other login's prompt calls are refused. Per-handler
// windows if that ever matters.
type AuthPromptBroker struct {
	mu      sync.Mutex
	gen     uint64 // window generation; an end() only clears its own window
	handler string
	prompt  auth.PasteBackFunc
}

// Begin registers the paste-back prompt for an in-flight Login of handler and
// returns the function that ends the registration. Call the returned function
// exactly once when the Login RPC returns. A nil prompt registers an active
// but non-interactive window (PromptAuthResponse yields Unavailable). A
// window never closes a newer one: concurrent Begin calls each own their own
// generation, and the newest registration wins while all are active.
func (b *AuthPromptBroker) Begin(handler string, prompt auth.PasteBackFunc) func() {
	b.mu.Lock()
	b.gen++
	myGen := b.gen
	b.handler = handler
	b.prompt = prompt
	b.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			b.mu.Lock()
			if b.gen == myGen {
				b.handler = ""
				b.prompt = nil
			}
			b.mu.Unlock()
		})
	}
}

// Prompt returns the prompt function registered for an active Login of the
// named handler. The second return is false when no Login is in progress for
// that handler (a nil prompt with true means: active login, non-interactive).
func (b *AuthPromptBroker) Prompt(handler string) (auth.PasteBackFunc, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.handler != handler {
		return nil, false
	}
	return b.prompt, true
}
