// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ErrOpaqueToken is returned when a token is not a decodable JWT (e.g., encrypted or opaque).
var ErrOpaqueToken = fmt.Errorf("token is not a decodable JWT")

// jwtClaims is the union of claim names found in both OIDC ID tokens and
// OAuth 2.0 access tokens issued by Entra ID (and other common providers).
type jwtClaims struct {
	Issuer            string `json:"iss"`
	Subject           string `json:"sub"`
	Audience          string `json:"aud"`
	TenantID          string `json:"tid"`
	ObjectID          string `json:"oid"`
	Email             string `json:"email"`
	PreferredUsername string `json:"preferred_username"`
	Name              string `json:"name"`
	IssuedAt          int64  `json:"iat"`
	ExpiresAt         int64  `json:"exp"`

	// Access-token-specific claim names (Entra v1 tokens use appid, v2 use azp).
	AppID            string `json:"appid"`
	AuthorizedParty  string `json:"azp"`
	UniqueName       string `json:"unique_name"`
	UPN              string `json:"upn"`
	ServicePrincipal string `json:"app_displayname"`
}

// ParseJWTClaims decodes a JWT token string (without signature verification)
// and extracts normalized identity claims. This works for both ID tokens and
// access tokens that are standard three-part JWTs.
//
// Returns ErrOpaqueToken if the token is not a decodable JWT (e.g., encrypted
// or opaque tokens issued by some providers for first-party resources).
func ParseJWTClaims(rawJWT string) (*Claims, error) {
	parts := strings.SplitN(rawJWT, ".", 3)
	if len(parts) != 3 {
		return nil, fmt.Errorf("%w: expected 3 parts, got %d", ErrOpaqueToken, len(parts))
	}

	// Decode payload (base64url, part index 1)
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("%w: failed to decode payload: %v", ErrOpaqueToken, err)
	}

	var raw jwtClaims
	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, fmt.Errorf("%w: failed to parse claims: %v", ErrOpaqueToken, err)
	}

	// Resolve email: prefer email > preferred_username > upn > unique_name
	email := raw.Email
	if email == "" {
		email = raw.PreferredUsername
	}
	if email == "" {
		email = raw.UPN
	}
	if email == "" {
		email = raw.UniqueName
	}

	// Resolve username: prefer preferred_username > upn > unique_name
	username := raw.PreferredUsername
	if username == "" {
		username = raw.UPN
	}
	if username == "" {
		username = raw.UniqueName
	}

	// Resolve client ID: prefer aud > azp > appid
	clientID := raw.Audience
	if clientID == "" {
		clientID = raw.AuthorizedParty
	}
	if clientID == "" {
		clientID = raw.AppID
	}

	claims := &Claims{
		Issuer:   raw.Issuer,
		Subject:  raw.Subject,
		TenantID: raw.TenantID,
		ObjectID: raw.ObjectID,
		ClientID: clientID,
		Email:    email,
		Name:     raw.Name,
		Username: username,
	}

	if raw.IssuedAt != 0 {
		claims.IssuedAt = time.Unix(raw.IssuedAt, 0)
	}
	if raw.ExpiresAt != 0 {
		claims.ExpiresAt = time.Unix(raw.ExpiresAt, 0)
	}

	return claims, nil
}
