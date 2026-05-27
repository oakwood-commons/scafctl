package authdelegation

import (
	"strings"
	"testing"
)

func BenchmarkOBOKeyGenerator(b *testing.B) {
	b.ReportAllocs()

	// Simulate a realistic ~1KB JWT token
	token := strings.Repeat("eyJhbGciOiJSUzI1NiJ9.", 40)
	params := FlowParams{
		CallerToken: token,
		Scope:       "api://my-app/.default",
		ClientID:    "550e8400-e29b-41d4-a716-446655440000",
	}

	b.ResetTimer()
	for range b.N {
		_, _ = OBOKeyGenerator(params, nil)
	}
}

func BenchmarkClientCredKeyGenerator(b *testing.B) {
	b.ReportAllocs()

	params := FlowParams{
		Scope:    "api://my-app/.default",
		ClientID: "550e8400-e29b-41d4-a716-446655440000",
	}

	b.ResetTimer()
	for range b.N {
		_, _ = ClientCredKeyGenerator(params, nil)
	}
}

func BenchmarkSHA256Hash(b *testing.B) {
	b.ReportAllocs()

	// Realistic JWT size (~1KB)
	token := strings.Repeat("eyJhbGciOiJSUzI1NiJ9.", 40)

	b.ResetTimer()
	for range b.N {
		_, _ = SHA256Hash(token)
	}
}
