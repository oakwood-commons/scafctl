// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package login

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/kube"
	"github.com/oakwood-commons/scafctl/pkg/kubeconfig"
)

func TestBuildExecArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		caps    []auth.Capability
		info    kube.ClusterInfo
		profile string
		want    []string
	}{
		{
			name: "minimal",
			info: kube.ClusterInfo{},
			want: []string{"auth", "token", "h", "--exec-credential"},
		},
		{
			name: "scope when capable with audience",
			caps: []auth.Capability{auth.CapScopesOnTokenRequest},
			info: kube.ClusterInfo{OIDCAudience: "aud"},
			want: []string{"auth", "token", "h", "--exec-credential", "--scope", "aud"},
		},
		{
			name: "no scope when not capable",
			info: kube.ClusterInfo{OIDCAudience: "aud"},
			want: []string{"auth", "token", "h", "--exec-credential"},
		},
		{
			name:    "profile appended",
			info:    kube.ClusterInfo{},
			profile: "work",
			want:    []string{"auth", "token", "h", "--exec-credential", "--profile", "work"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h := &stubAuth{name: "h", caps: tt.caps}
			got := buildExecArgs(h, tt.info, tt.profile)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestInteractiveModeFor(t *testing.T) {
	t.Parallel()

	assert.Equal(t, kubeconfig.InteractiveModeIfAvailable, interactiveModeFor(kube.AuthTypeOAuth))
	assert.Equal(t, kubeconfig.InteractiveModeNever, interactiveModeFor(kube.AuthTypeOIDC))
	assert.Equal(t, kubeconfig.InteractiveModeNever, interactiveModeFor(kube.AuthTypeAuto))
}

func TestBuildInstallHint(t *testing.T) {
	t.Parallel()

	hint := buildInstallHint("mycli")
	assert.Contains(t, hint, "mycli")
	assert.True(t, strings.Contains(hint, "mycli kube login"), "hint should reference the kube login command")
}
