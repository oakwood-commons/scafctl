// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import "time"

// Claims represents normalized identity claims from any auth handler.
type Claims struct {
	Issuer    string
	Subject   string
	TenantID  string
	ObjectID  string
	ClientID  string
	Email     string
	Name      string
	Username  string
	IssuedAt  time.Time
	ExpiresAt time.Time
}

// IsEmpty returns true if the claims have no meaningful data.
func (c *Claims) IsEmpty() bool {
	if c == nil {
		return true
	}
	return c.Subject == "" && c.Email == "" && c.Name == "" && c.Username == ""
}

// DisplayIdentity returns the best available identity string for display.
func (c *Claims) DisplayIdentity() string {
	if c == nil {
		return ""
	}
	if c.Email != "" {
		return c.Email
	}
	if c.Username != "" {
		return c.Username
	}
	if c.Name != "" {
		return c.Name
	}
	return c.Subject
}
