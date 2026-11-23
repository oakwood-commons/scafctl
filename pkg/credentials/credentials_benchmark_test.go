// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package credentials

import (
	"context"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/secrets"
	"github.com/oakwood-commons/scafctl/pkg/spec"
)

func BenchmarkResolver_ResolveSecret(b *testing.B) {
	store := secrets.NewMockStore()
	store.Data["npm-token"] = []byte("npm_secret_value_1234567890")
	ctx := secrets.WithStore(context.Background(), store)
	r := NewResolver()

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = r.ResolveSecret(ctx, "npm-token")
	}
}

func BenchmarkResolver_ResolveToken(b *testing.B) {
	reg := auth.NewRegistry()
	gh := auth.NewMockHandler("github")
	gh.GetTokenResult = &auth.Token{
		AccessToken: "gho_live_token_1234567890",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour),
	}
	_ = reg.Register(gh)
	ctx := auth.WithRegistry(context.Background(), reg)
	r := NewResolver()
	req := spec.TokenRef{Handler: "github"}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = r.ResolveToken(ctx, req)
	}
}
