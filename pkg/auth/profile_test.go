// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseProfileKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		key         string
		wantHandler string
		wantProfile string
	}{
		{name: "bare handler", key: "github", wantHandler: "github", wantProfile: ""},
		{name: "handler with profile", key: "github@work", wantHandler: "github", wantProfile: "work"},
		{name: "handler with complex profile", key: "entra@prod-tenant", wantHandler: "entra", wantProfile: "prod-tenant"},
		{name: "multiple @ symbols", key: "handler@profile@extra", wantHandler: "handler", wantProfile: "profile@extra"},
		{name: "empty handler", key: "@profile", wantHandler: "", wantProfile: "profile"},
		{name: "empty string", key: "", wantHandler: "", wantProfile: ""},
		{name: "trailing @", key: "github@", wantHandler: "github", wantProfile: ""},
		{name: "handler@default normalizes to empty", key: "github@default", wantHandler: "github", wantProfile: ""},
		{name: "handler@Default case insensitive", key: "github@Default", wantHandler: "github", wantProfile: ""},
		{name: "handler@built-in normalizes to empty", key: "github@built-in", wantHandler: "github", wantProfile: ""},
		{name: "handler@Built-In case insensitive", key: "github@Built-In", wantHandler: "github", wantProfile: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			handler, profile := ParseProfileKey(tt.key)
			assert.Equal(t, tt.wantHandler, handler)
			assert.Equal(t, tt.wantProfile, profile)
		})
	}
}

func TestProfileKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		handler string
		profile string
		want    string
	}{
		{name: "bare handler", handler: "github", profile: "", want: "github"},
		{name: "with profile", handler: "github", profile: "work", want: "github@work"},
		{name: "entra with profile", handler: "entra", profile: "prod", want: "entra@prod"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, ProfileKey(tt.handler, tt.profile))
		})
	}
}

func TestValidateProfileName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		profile string
		wantErr bool
	}{
		{name: "valid simple", profile: "work", wantErr: false},
		{name: "valid with hyphens", profile: "prod-tenant", wantErr: false},
		{name: "valid with underscores", profile: "my_profile", wantErr: false},
		{name: "valid alphanumeric", profile: "profile1", wantErr: false},
		{name: "valid single char", profile: "a", wantErr: false},
		{name: "empty", profile: "", wantErr: true},
		{name: "reserved default", profile: "default", wantErr: true},
		{name: "reserved Default case insensitive", profile: "Default", wantErr: true},
		{name: "reserved DEFAULT", profile: "DEFAULT", wantErr: true},
		{name: "reserved built-in", profile: "built-in", wantErr: true},
		{name: "reserved Built-In case insensitive", profile: "Built-In", wantErr: true},
		{name: "contains @", profile: "bad@name", wantErr: true},
		{name: "starts with hyphen", profile: "-invalid", wantErr: true},
		{name: "starts with underscore", profile: "_invalid", wantErr: true},
		{name: "contains space", profile: "bad name", wantErr: true},
		{name: "contains special chars", profile: "bad!name", wantErr: true},
		{name: "too long", profile: strings.Repeat("a", MaxProfileNameLength+1), wantErr: true},
		{name: "max length", profile: strings.Repeat("a", MaxProfileNameLength), wantErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateProfileName(tt.profile)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestProfileKey_Roundtrip(t *testing.T) {
	t.Parallel()

	handler, profile := "github", "work"
	key := ProfileKey(handler, profile)
	gotHandler, gotProfile := ParseProfileKey(key)
	assert.Equal(t, handler, gotHandler)
	assert.Equal(t, profile, gotProfile)
}

func TestWithProfile_ProfileFromContext(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	assert.Equal(t, "", ProfileFromContext(ctx))

	ctx = WithProfile(ctx, "work")
	assert.Equal(t, "work", ProfileFromContext(ctx))
}

func TestValidateProfileName_MaxLength(t *testing.T) {
	t.Parallel()

	// Exactly at max length should pass
	require.NoError(t, ValidateProfileName(strings.Repeat("a", MaxProfileNameLength)))

	// One over should fail
	require.Error(t, ValidateProfileName(strings.Repeat("a", MaxProfileNameLength+1)))
}

func TestNormalizeProfileName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "default lowercase", in: "default", want: ""},
		{name: "default uppercase", in: "Default", want: ""},
		{name: "default all caps", in: "DEFAULT", want: ""},
		{name: "built-in lowercase", in: "built-in", want: ""},
		{name: "built-in mixed case", in: "Built-In", want: ""},
		{name: "named profile", in: "work", want: "work"},
		{name: "empty stays empty", in: "", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, NormalizeProfileName(tt.in))
		})
	}
}

func TestDisplayProfileName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty shows built-in", in: "", want: "built-in"},
		{name: "named profile unchanged", in: "work", want: "work"},
		{name: "another named profile", in: "staging", want: "staging"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, DisplayProfileName(tt.in))
		})
	}
}
