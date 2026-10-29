// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseSourceSelector(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		origin  string
		want    SourceSelector
		wantErr bool
	}{
		{name: "empty selects any", origin: "", want: SourceSelector{Kind: SourceAny}},
		{name: "whitespace selects any", origin: "   ", want: SourceSelector{Kind: SourceAny}},
		{name: "built keyword", origin: "built", want: SourceSelector{Kind: SourceBuilt}},
		{name: "built keyword trimmed", origin: "  built  ", want: SourceSelector{Kind: SourceBuilt}},
		{name: "registry with namespace", origin: "ghcr.io/org/plugins", want: SourceSelector{Kind: SourceRemote, Remote: "ghcr.io/org/plugins"}},
		{name: "oci scheme and trailing slash", origin: "oci://ghcr.io/org/plugins/", want: SourceSelector{Kind: SourceRemote, Remote: "ghcr.io/org/plugins"}},
		{name: "https scheme", origin: "https://ghcr.io/org", want: SourceSelector{Kind: SourceRemote, Remote: "ghcr.io/org"}},
		{name: "bare host", origin: "ghcr.io", want: SourceSelector{Kind: SourceRemote, Remote: "ghcr.io"}},
		{name: "localhost with port", origin: "localhost:5000", want: SourceSelector{Kind: SourceRemote, Remote: "localhost:5000"}},
		{name: "case preserved and non-keyword passes through", origin: "Built", want: SourceSelector{Kind: SourceRemote, Remote: "Built"}},
		{name: "scheme only is rejected", origin: "oci://", wantErr: true},
		{name: "slash only is rejected", origin: "/", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := ParseSourceSelector(tt.origin)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
