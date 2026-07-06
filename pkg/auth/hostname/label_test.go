// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package hostname

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAliasForURL(t *testing.T) {
	t.Parallel()

	aliases := map[string]string{
		"pd1020": "https://api.pd1020.caas.ford.com:6443",
		"np0510": "https://api.np0510.caas.ford.com:6443",
		"legacy": "api.pd1020.caas.ford.com", // bare host, same as pd1020
	}

	tests := []struct {
		name      string
		aliases   map[string]string
		url       string
		wantAlias string
		wantOK    bool
	}{
		{
			name:      "exact url match",
			aliases:   aliases,
			url:       "https://api.np0510.caas.ford.com:6443",
			wantAlias: "np0510",
			wantOK:    true,
		},
		{
			name:      "match ignores scheme and port",
			aliases:   aliases,
			url:       "https://api.np0510.caas.ford.com",
			wantAlias: "np0510",
			wantOK:    true,
		},
		{
			name:      "ambiguous match returns lexicographically first",
			aliases:   aliases,
			url:       "https://api.pd1020.caas.ford.com:6443",
			wantAlias: "legacy",
			wantOK:    true,
		},
		{
			name:      "no match",
			aliases:   aliases,
			url:       "https://api.unknown.example.com",
			wantAlias: "",
			wantOK:    false,
		},
		{
			name:      "empty aliases",
			aliases:   nil,
			url:       "https://api.pd1020.caas.ford.com",
			wantAlias: "",
			wantOK:    false,
		},
		{
			name:      "empty url",
			aliases:   aliases,
			url:       "",
			wantAlias: "",
			wantOK:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := AliasForURL(tt.aliases, tt.url)
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.wantAlias, got)
		})
	}
}

func TestDisplayHostFromURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		url  string
		want string
	}{
		{"https url with port", "https://api.pd1020.caas.ford.com:6443", "api.pd1020.caas.ford.com"},
		{"http url with path", "http://host.example.com/some/path", "host.example.com"},
		{"bare host", "host.example.com", "host.example.com"},
		{"bare host with port", "host.example.com:8443", "host.example.com"},
		{"uppercase normalized", "HTTPS://API.EXAMPLE.COM", "api.example.com"},
		{"unparseable falls back to trimmed original", "://bad", "://bad"},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, DisplayHostFromURL(tt.url))
		})
	}
}
