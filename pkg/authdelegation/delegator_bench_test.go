package authdelegation

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	manager "github.com/oakwood-commons/go-flight/cache"
	"github.com/oakwood-commons/scafctl/pkg/api/middleware"
	"github.com/oakwood-commons/scafctl/pkg/httpc"
)

func BenchmarkDelegateToken_CacheHit(b *testing.B) {
	b.ReportAllocs()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "bench-token", "expires_in": 3600,
		})
	}))
	defer srv.Close()

	tokenCache := NewTokenCache[string, TokenResult](context.Background(), 1024, 0, time.Hour)
	mgr := manager.NewManager(
		manager.WithStore("bench", tokenCache),
	)

	client := httpc.NewClient(nil)
	registry := NewFlowRegistry()
	registry.Register("obo", oboFlow(srv.URL, &SecretCredential{Secret: "s"}, client))
	registry.Register("client_credentials", clientCredentialFlow(srv.URL, &SecretCredential{Secret: "s"}, client))

	cfg := EntraDelegatorConfig{TenantID: "t", ClientID: "c", CredentialType: CredentialTypeSecret, ClientSecret: "s"}
	d, err := NewEntraDelegator(cfg, WithHTTPClient(client), WithFlowRegistry(registry), WithManager(mgr))
	if err != nil {
		b.Fatal(err)
	}

	ctx := middleware.WithAccessToken(context.Background(), "caller-jwt-token")
	ctx = middleware.WithAuthClaims(ctx, &middleware.AuthClaims{IDType: "user"})

	// Warm the cache
	if _, err := d.DelegateToken(ctx, "api/.default"); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for range b.N {
		_, _ = d.DelegateToken(ctx, "api/.default")
	}
}

func BenchmarkDelegateToken_FlowSelection(b *testing.B) {
	b.ReportAllocs()

	// Measure overhead of flow selection + key generation without network calls.
	// Uses a no-op flow that returns immediately.
	noopFlow := func(_ context.Context, _ FlowParams) (TokenResult, error) {
		return TokenResult{AccessToken: "x", ExpiresIn: 60}, nil
	}

	registry := NewFlowRegistry()
	registry.Register("obo", noopFlow)
	registry.Register("client_credentials", noopFlow)

	cfg := EntraDelegatorConfig{TenantID: "t", ClientID: "c", CredentialType: CredentialTypeSecret, ClientSecret: "s"}
	d, err := NewEntraDelegator(cfg, WithFlowRegistry(registry))
	if err != nil {
		b.Fatal(err)
	}

	// Realistic ~1KB JWT token
	token := strings.Repeat("eyJhbGciOiJSUzI1NiJ9.", 40)
	ctx := middleware.WithAccessToken(context.Background(), token)
	ctx = middleware.WithAuthClaims(ctx, &middleware.AuthClaims{IDType: "user"})

	b.ResetTimer()
	for range b.N {
		_, _ = d.DelegateToken(ctx, "api/.default")
	}
}
