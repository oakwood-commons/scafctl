// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package authdelegation

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

// KeyGenerator produces a cache key from flow parameters.
// Returns the key and true on success, or the zero value and false if inputs are insufficient.
type KeyGenerator[K comparable] func(params FlowParams, hashFunc func(string) (string, bool)) (K, bool)

// OBOKeyGenerator produces a cache key for OBO flows: "obo|clientID|scope|hash(callerToken)".
func OBOKeyGenerator(params FlowParams, hashFunc func(string) (string, bool)) (string, bool) {
	if hashFunc == nil {
		hashFunc = SHA256Hash
	}
	if params.CallerToken == "" || params.Scope == "" || params.ClientID == "" {
		return "", false
	}
	hashedToken, ok := hashFunc(params.CallerToken)
	if !ok {
		return "", false
	}
	var b strings.Builder
	b.WriteString("obo|")
	b.WriteString(params.ClientID)
	b.WriteString("|")
	b.WriteString(params.Scope)
	b.WriteString("|")
	b.WriteString(hashedToken)
	return b.String(), true
}

// ClientCredKeyGenerator produces a cache key for client_credentials flows: "cc|clientID|scope".
func ClientCredKeyGenerator(params FlowParams, _ func(string) (string, bool)) (string, bool) {
	if params.ClientID == "" || params.Scope == "" {
		return "", false
	}
	var b strings.Builder
	b.WriteString("cc|")
	b.WriteString(params.ClientID)
	b.WriteString("|")
	b.WriteString(params.Scope)
	return b.String(), true
}

// NoOpKeyGenerator always returns ("", false), disabling caching.
func NoOpKeyGenerator(_ FlowParams, _ func(string) (string, bool)) (string, bool) {
	return "", false
}

// GetKeyGenerator selects the appropriate key generator based on the caller type.
func GetKeyGenerator(callerType string) KeyGenerator[string] {
	switch callerType {
	case "user":
		return OBOKeyGenerator
	case "app":
		return ClientCredKeyGenerator
	default:
		return NoOpKeyGenerator
	}
}

// GenerateKey is a convenience wrapper that selects and invokes the key generator.
func GenerateKey(callerType string, params FlowParams) (string, bool) {
	keyGen := GetKeyGenerator(callerType)
	return keyGen(params, nil)
}

// SHA256Hash returns the hex-encoded SHA-256 hash of the input.
func SHA256Hash(input string) (string, bool) {
	if input == "" {
		return "", false
	}
	h := sha256.Sum256([]byte(input))
	return fmt.Sprintf("%x", h[:]), true
}
