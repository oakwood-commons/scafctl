// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTagForLocalRef(t *testing.T) {
	v := semver.MustParse("1.2.3")

	tests := []struct {
		name    string
		ref     Reference
		want    string
		wantErr string
	}{
		{
			name: "no remote means built",
			ref:  Reference{Kind: ArtifactKindSolution, Name: "demo", Version: v},
			want: "solution/demo:1.2.3",
		},
		{
			name: "built formats with singular kind",
			ref:  Reference{Kind: ArtifactKindProvider, Name: "demo", Version: v, Built: true},
			want: "provider/demo:1.2.3",
		},
		{
			name: "remote embeds host and pluralizes kind",
			ref:  Reference{Kind: ArtifactKindProvider, Name: "demo", Version: v, Remote: "example.com"},
			want: "example.com/providers/demo:1.2.3",
		},
		{
			name: "remote auth-handler pluralizes correctly",
			ref:  Reference{Kind: ArtifactKindAuthHandler, Name: "demo", Version: v, Remote: "example.com"},
			want: "example.com/auth-handlers/demo:1.2.3",
		},
		{
			name: "remote wins over the built flag",
			ref:  Reference{Kind: ArtifactKindSolution, Name: "demo", Version: v, Built: true, Remote: "example.com"},
			want: "example.com/solutions/demo:1.2.3",
		},
		{
			name: "version wins over digest when both are present",
			ref:  Reference{Kind: ArtifactKindSolution, Name: "demo", Version: v, Digest: "sha256:abc123"},
			want: "solution/demo:1.2.3",
		},
		{
			name: "digest is joined with @",
			ref:  Reference{Kind: ArtifactKindSolution, Name: "demo", Digest: "sha256:abc123"},
			want: "solution/demo@sha256:abc123",
		},
		{
			name: "remote digest is joined with @",
			ref:  Reference{Kind: ArtifactKindProvider, Name: "demo", Digest: "sha256:abc123", Remote: "example.com"},
			want: "example.com/providers/demo@sha256:abc123",
		},
		{
			name:    "no version or digest is an error",
			ref:     Reference{Kind: ArtifactKindSolution, Name: "demo"},
			wantErr: "no version or digest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tagForLocalRef(tt.ref)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
