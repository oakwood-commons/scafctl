// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package gitcredential

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/secrets"
)

func BenchmarkParseRequest(b *testing.B) {
	payload := "protocol=https\nhost=github.com\npath=work-org/service.git\nwwwauth[]=Basic realm=\"GitHub\"\n\n"
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, err := ParseRequest(strings.NewReader(payload))
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkWriteResponse(b *testing.B) {
	exp := time.Now().Add(time.Hour).UTC()
	resp := &Response{
		Protocol:          "https",
		Host:              "github.com",
		Path:              "work-org/service.git",
		Username:          "x-access-token",
		Password:          "mock-benchmark-placeholder-val",
		PasswordExpiryUTC: &exp,
	}
	var buf bytes.Buffer
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		buf.Reset()
		if err := WriteResponse(&buf, resp); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMatchRule(b *testing.B) {
	rules := []config.GitCredentialRule{
		{Host: "github.com", Handler: "github", Profile: "default"},
		{Host: "github.com", PathPrefix: "work-org", Handler: "github", Profile: "work"},
		{Host: "github.com", PathPrefix: "work-org/platform", Handler: "github", Profile: "platform"},
		{Host: "git.example.com", PathPrefix: "acme-corp", Handler: "acme-git", Profile: "ci"},
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = MatchRule("github.com", "work-org/platform/service.git", rules)
	}
}

func BenchmarkHelperGet_DynamicHandler(b *testing.B) {
	reg := auth.NewRegistry()
	gh := auth.NewMockHandler("github")
	gh.SetToken(&auth.Token{
		AccessToken: "mock-bench-val",
		ExpiresAt:   time.Now().Add(time.Hour),
	})
	_ = reg.Register(gh)

	helper := New(
		secrets.NewMockStore(),
		WithTokenResolver(NewAuthTokenResolver(reg)),
	)
	req := &Request{
		Protocol: "https",
		Host:     "github.com",
		Path:     "work-org/repo.git",
	}
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := helper.Get(ctx, req); err != nil {
			b.Fatal(err)
		}
	}
}
