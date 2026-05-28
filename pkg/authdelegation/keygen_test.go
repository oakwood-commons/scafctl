package authdelegation

import (
	"crypto/sha256"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestOBOKeyGenerator(t *testing.T) {
	t.Parallel()

	t.Run("produces correct key format", func(t *testing.T) {
		t.Parallel()
		params := FlowParams{CallerToken: "my-jwt", Scope: "api/.default", ClientID: "client-1"}
		key, ok := OBOKeyGenerator(params, nil)
		assert.True(t, ok)

		expectedHash := fmt.Sprintf("%x", sha256.Sum256([]byte("my-jwt")))
		assert.Equal(t, "obo|client-1|api/.default|"+expectedHash, key)
	})

	t.Run("uses custom hash func", func(t *testing.T) {
		t.Parallel()
		fakeHash := func(s string) (string, bool) { return "hashed-" + s, true }
		params := FlowParams{CallerToken: "tok", Scope: "s", ClientID: "c"}
		key, ok := OBOKeyGenerator(params, fakeHash)
		assert.True(t, ok)
		assert.Equal(t, "obo|c|s|hashed-tok", key)
	})

	t.Run("empty caller token returns false", func(t *testing.T) {
		t.Parallel()
		_, ok := OBOKeyGenerator(FlowParams{Scope: "s", ClientID: "c"}, nil)
		assert.False(t, ok)
	})

	t.Run("empty scope returns false", func(t *testing.T) {
		t.Parallel()
		_, ok := OBOKeyGenerator(FlowParams{CallerToken: "t", ClientID: "c"}, nil)
		assert.False(t, ok)
	})

	t.Run("empty client ID returns false", func(t *testing.T) {
		t.Parallel()
		_, ok := OBOKeyGenerator(FlowParams{CallerToken: "t", Scope: "s"}, nil)
		assert.False(t, ok)
	})

	t.Run("hash failure returns false", func(t *testing.T) {
		t.Parallel()
		failHash := func(_ string) (string, bool) { return "", false }
		params := FlowParams{CallerToken: "t", Scope: "s", ClientID: "c"}
		_, ok := OBOKeyGenerator(params, failHash)
		assert.False(t, ok)
	})
}

func TestClientCredKeyGenerator(t *testing.T) {
	t.Parallel()

	t.Run("produces correct key format", func(t *testing.T) {
		t.Parallel()
		params := FlowParams{ClientID: "client-1", Scope: "api/.default"}
		key, ok := ClientCredKeyGenerator(params, nil)
		assert.True(t, ok)
		assert.Equal(t, "cc|client-1|api/.default", key)
	})

	t.Run("empty client ID returns false", func(t *testing.T) {
		t.Parallel()
		_, ok := ClientCredKeyGenerator(FlowParams{Scope: "s"}, nil)
		assert.False(t, ok)
	})

	t.Run("empty scope returns false", func(t *testing.T) {
		t.Parallel()
		_, ok := ClientCredKeyGenerator(FlowParams{ClientID: "c"}, nil)
		assert.False(t, ok)
	})
}

func TestNoOpKeyGenerator(t *testing.T) {
	t.Parallel()
	_, ok := NoOpKeyGenerator(FlowParams{CallerToken: "t", Scope: "s", ClientID: "c"}, nil)
	assert.False(t, ok)
}

func TestGetKeyGenerator(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		callerType string
		params     FlowParams
		wantOK     bool
		wantPrefix string
	}{
		{"user routes to OBO", "user", FlowParams{CallerToken: "t", Scope: "s", ClientID: "c"}, true, "obo|"},
		{"app routes to CC", "app", FlowParams{ClientID: "c", Scope: "s"}, true, "cc|"},
		{"unknown routes to NoOp", "unknown", FlowParams{CallerToken: "t", Scope: "s", ClientID: "c"}, false, ""},
		{"empty routes to NoOp", "", FlowParams{CallerToken: "t", Scope: "s", ClientID: "c"}, false, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			keyGen := GetKeyGenerator(tc.callerType)
			key, ok := keyGen(tc.params, nil)
			assert.Equal(t, tc.wantOK, ok)
			if tc.wantOK {
				assert.Contains(t, key, tc.wantPrefix)
			}
		})
	}
}

func TestGenerateKey(t *testing.T) {
	t.Parallel()

	t.Run("user caller", func(t *testing.T) {
		t.Parallel()
		key, ok := GenerateKey("user", FlowParams{CallerToken: "jwt", Scope: "s", ClientID: "c"})
		assert.True(t, ok)
		assert.Contains(t, key, "obo|c|s|")
	})

	t.Run("app caller", func(t *testing.T) {
		t.Parallel()
		key, ok := GenerateKey("app", FlowParams{ClientID: "c", Scope: "s"})
		assert.True(t, ok)
		assert.Equal(t, "cc|c|s", key)
	})

	t.Run("unknown caller", func(t *testing.T) {
		t.Parallel()
		_, ok := GenerateKey("unknown", FlowParams{CallerToken: "t", Scope: "s", ClientID: "c"})
		assert.False(t, ok)
	})
}

func TestSHA256Hash(t *testing.T) {
	t.Parallel()

	t.Run("hashes non-empty input", func(t *testing.T) {
		t.Parallel()
		h, ok := SHA256Hash("hello")
		assert.True(t, ok)
		expected := fmt.Sprintf("%x", sha256.Sum256([]byte("hello")))
		assert.Equal(t, expected, h)
	})

	t.Run("empty input returns false", func(t *testing.T) {
		t.Parallel()
		_, ok := SHA256Hash("")
		assert.False(t, ok)
	})
}
