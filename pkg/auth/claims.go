// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import "time"

// Claims represents normalized identity claims from any auth handler.
type Claims struct {
	Issuer    string    `json:"issuer,omitempty" yaml:"issuer,omitempty" doc:"Token issuer URL" example:"https://login.microsoftonline.com" maxLength:"512"`
	Subject   string    `json:"subject,omitempty" yaml:"subject,omitempty" doc:"Subject identifier" example:"user@example.com" maxLength:"512"`
	TenantID  string    `json:"tenantId,omitempty" yaml:"tenantId,omitempty" doc:"Azure AD tenant ID" example:"72f988bf-86f1-41af-91ab-2d7cd011db47" maxLength:"128"`
	ObjectID  string    `json:"objectId,omitempty" yaml:"objectId,omitempty" doc:"Object ID of the principal" example:"00000000-0000-0000-0000-000000000000" maxLength:"128"`
	ClientID  string    `json:"clientId,omitempty" yaml:"clientId,omitempty" doc:"Application/client ID" example:"04b07795-8ddb-461a-bbee-02f9e1bf7b46" maxLength:"128"`
	Email     string    `json:"email,omitempty" yaml:"email,omitempty" doc:"Email address of the identity" example:"user@example.com" maxLength:"320"`
	Name      string    `json:"name,omitempty" yaml:"name,omitempty" doc:"Display name of the identity" example:"Jane Doe" maxLength:"256"`
	Username  string    `json:"username,omitempty" yaml:"username,omitempty" doc:"Username or login name" example:"janedoe" maxLength:"256"`
	IssuedAt  time.Time `json:"issuedAt,omitempty" yaml:"issuedAt,omitempty" doc:"Time the token was issued"`
	ExpiresAt time.Time `json:"expiresAt,omitempty" yaml:"expiresAt,omitempty" doc:"Time the token expires"`
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
