// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// PrivateCIDRs is the single place the AllowedPrivateCIDRs pointer is
// dereferenced, so it carries the whole absent-versus-empty distinction that
// the policy layer depends on.
func TestPrivateCIDRs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		cfg         *HTTPClientConfig
		wantSet     bool
		wantEntries []string
	}{
		{
			name:    "nil config",
			cfg:     nil,
			wantSet: false,
		},
		{
			name:    "field absent",
			cfg:     &HTTPClientConfig{},
			wantSet: false,
		},
		{
			name:        "present but empty means no exceptions",
			cfg:         &HTTPClientConfig{AllowedPrivateCIDRs: PrivateCIDRList()},
			wantSet:     true,
			wantEntries: []string{},
		},
		{
			name:        "populated",
			cfg:         &HTTPClientConfig{AllowedPrivateCIDRs: PrivateCIDRList("10.0.0.0/8", "fd00::/8")},
			wantSet:     true,
			wantEntries: []string{"10.0.0.0/8", "fd00::/8"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			entries, set := tt.cfg.PrivateCIDRs()
			assert.Equal(t, tt.wantSet, set)
			if !tt.wantSet {
				assert.Nil(t, entries, "an absent list must not masquerade as an empty one")
				return
			}
			// Never nil when set, so a caller can encode it without
			// reintroducing the null-versus-[] ambiguity.
			assert.NotNil(t, entries)
			assert.Equal(t, tt.wantEntries, entries)
		})
	}
}

// A pointer to a nil slice is reachable through a struct literal, and must read
// as "set" with an encodable empty list rather than as absent.
func TestPrivateCIDRs_PointerToNilSlice(t *testing.T) {
	t.Parallel()

	var inner []string
	cfg := &HTTPClientConfig{AllowedPrivateCIDRs: &inner}

	entries, set := cfg.PrivateCIDRs()
	assert.True(t, set)
	assert.NotNil(t, entries)
	assert.Empty(t, entries)
}

func TestPrivateCIDRList(t *testing.T) {
	t.Parallel()

	t.Run("no arguments yields a present empty list", func(t *testing.T) {
		t.Parallel()
		got := PrivateCIDRList()
		require.NotNil(t, got)
		assert.NotNil(t, *got, "must be encodable as [] rather than null")
		assert.Empty(t, *got)
	})

	t.Run("entries are preserved in order", func(t *testing.T) {
		t.Parallel()
		got := PrivateCIDRList("10.0.0.0/8", "192.168.0.0/16")
		require.NotNil(t, got)
		assert.Equal(t, []string{"10.0.0.0/8", "192.168.0.0/16"}, *got)
	})
}
