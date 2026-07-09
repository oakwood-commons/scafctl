// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

// ErrHandlerDisabled is returned by disabled auth handlers when any operation
// is attempted. Use errors.Is to detect this condition.
var ErrHandlerDisabled = errors.New("auth handler disabled")

// disabledHandler wraps an existing Handler and rejects all operations with
// ErrHandlerDisabled. It preserves the handler's identity (Name, DisplayName)
// and metadata (SupportedFlows, Capabilities) for discoverability, but all
// state-changing or token-acquiring methods fail immediately.
type disabledHandler struct {
	name    string
	display string
	reason  string
	flows   []Flow
	caps    []Capability
}

func newDisabledHandler(original Handler, reason string) *disabledHandler {
	return &disabledHandler{
		name:    original.Name(),
		display: original.DisplayName(),
		reason:  reason,
		flows:   original.SupportedFlows(),
		caps:    original.Capabilities(),
	}
}

func (d *disabledHandler) Name() string               { return d.name }
func (d *disabledHandler) DisplayName() string        { return d.display + " (disabled)" }
func (d *disabledHandler) SupportedFlows() []Flow     { return d.flows }
func (d *disabledHandler) Capabilities() []Capability { return d.caps }

func (d *disabledHandler) err() error {
	return fmt.Errorf("%w: %s (%s)", ErrHandlerDisabled, d.name, d.reason)
}

func (d *disabledHandler) Login(_ context.Context, _ LoginOptions) (*Result, error) {
	return nil, d.err()
}

func (d *disabledHandler) Logout(_ context.Context) error {
	return d.err()
}

func (d *disabledHandler) Status(_ context.Context) (*Status, error) {
	return &Status{Authenticated: false}, nil
}

func (d *disabledHandler) GetToken(_ context.Context, _ TokenOptions) (*Token, error) {
	return nil, d.err()
}

func (d *disabledHandler) InjectAuth(_ context.Context, _ *http.Request, _ TokenOptions) error {
	return d.err()
}

// IsDisabled reports whether the given handler is a disabled wrapper.
func IsDisabled(h Handler) bool {
	_, ok := h.(*disabledHandler)
	return ok
}

var _ Handler = (*disabledHandler)(nil)
