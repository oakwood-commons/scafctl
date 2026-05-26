// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

const (
	// ProfileSeparator is the character used to separate handler name from profile name.
	ProfileSeparator = "@"

	// MaxProfileNameLength is the maximum allowed length for a profile name.
	MaxProfileNameLength = 64

	// BuiltinProfileName is the display name for the unnamed built-in profile.
	// Internally the built-in profile is stored as "" (empty string), but this
	// constant is used for display and as an accepted alias in --profile flags.
	BuiltinProfileName = "built-in"

	// DefaultProfileName is kept for backward compatibility. Input of "default"
	// is still accepted and normalized to the unnamed built-in profile.
	DefaultProfileName = "default"
)

// profileKey is the context key for storing the active profile.
const profileContextKey contextKey = "auth.profile"

// globalProfileContextKey stores the global --auth-profile flag value.
const globalProfileContextKey contextKey = "auth.globalProfile"

// validProfileName matches ASCII alphanumeric plus - and _.
var validProfileName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)

// ParseProfileKey splits "handler@profile" into (handler, profile).
// Returns (key, "") for bare handler names without a profile separator.
// The profile name is normalized: "handler@built-in" and "handler@default"
// both return ("handler", "").
func ParseProfileKey(key string) (handler, profile string) {
	if idx := strings.Index(key, ProfileSeparator); idx >= 0 {
		return key[:idx], NormalizeProfileName(key[idx+1:])
	}
	return key, ""
}

// ProfileKey joins handler and profile into "handler@profile".
// Returns bare handler name when profile is empty.
func ProfileKey(handler, profile string) string {
	if profile == "" {
		return handler
	}
	return handler + ProfileSeparator + profile
}

// ValidateProfileName checks that a profile name is valid.
// Valid names are 1-64 characters, ASCII alphanumeric plus - and _,
// starting with an alphanumeric character.
func ValidateProfileName(name string) error {
	if name == "" {
		return fmt.Errorf("profile name must not be empty")
	}
	if isReservedProfileName(name) {
		return fmt.Errorf("profile name %q is reserved for the unnamed built-in profile; use a different name", name)
	}
	if len(name) > MaxProfileNameLength {
		return fmt.Errorf("profile name %q exceeds maximum length of %d characters", name, MaxProfileNameLength)
	}
	if strings.Contains(name, ProfileSeparator) {
		return fmt.Errorf("profile name %q must not contain %q (reserved as separator)", name, ProfileSeparator)
	}
	if !validProfileName.MatchString(name) {
		return fmt.Errorf("profile name %q must be ASCII alphanumeric, hyphens, or underscores (starting with alphanumeric)", name)
	}
	return nil
}

// WithProfile returns a new context with the given auth profile name.
func WithProfile(ctx context.Context, profile string) context.Context {
	return context.WithValue(ctx, profileContextKey, profile)
}

// ProfileFromContext retrieves the auth profile from the context.
// Returns "" if no profile is set.
func ProfileFromContext(ctx context.Context) string {
	profile, _ := ctx.Value(profileContextKey).(string)
	return profile
}

// WithGlobalProfile returns a new context with the global auth profile set.
// This is used for the --auth-profile persistent flag / SCAFCTL_AUTH_PROFILE env var.
func WithGlobalProfile(ctx context.Context, profile string) context.Context {
	return context.WithValue(ctx, globalProfileContextKey, profile)
}

// GlobalProfileFromContext retrieves the global auth profile from the context.
// Returns "" if no global profile is set.
func GlobalProfileFromContext(ctx context.Context) string {
	profile, _ := ctx.Value(globalProfileContextKey).(string)
	return profile
}

// NormalizeProfileName maps the reserved names "built-in" and "default" to the
// internal empty string representation. All other names are returned as-is.
func NormalizeProfileName(name string) string {
	if isReservedProfileName(name) {
		return ""
	}
	return name
}

// DisplayProfileName returns the display name for a profile.
// The unnamed built-in profile ("") is shown as "built-in".
func DisplayProfileName(profile string) string {
	if profile == "" {
		return BuiltinProfileName
	}
	return profile
}

// isReservedProfileName checks if a name is reserved for the unnamed built-in profile.
func isReservedProfileName(name string) bool {
	return strings.EqualFold(name, BuiltinProfileName) || strings.EqualFold(name, DefaultProfileName)
}
