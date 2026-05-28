package authdelegation

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/httpc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOBOFlow(t *testing.T) {
	t.Parallel()

	t.Run("sends correct form params", func(t *testing.T) {
		t.Parallel()
		var captured url.Values
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.NoError(t, r.ParseForm())
			captured = r.PostForm
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "tok", "token_type": "Bearer", "expires_in": 60, "scope": "s",
			})
		}))
		t.Cleanup(srv.Close)

		flow := oboFlow(srv.URL, &SecretCredential{Secret: "my-secret"}, httpc.NewClient(nil))
		_, err := flow(context.Background(), FlowParams{CallerToken: "jwt123", Scope: "api/.default", ClientID: "my-client"})
		require.NoError(t, err)

		assert.Equal(t, "urn:ietf:params:oauth:grant-type:jwt-bearer", captured.Get("grant_type"))
		assert.Equal(t, "my-client", captured.Get("client_id"))
		assert.Equal(t, "jwt123", captured.Get("assertion"))
		assert.Equal(t, "api/.default", captured.Get("scope"))
		assert.Equal(t, "on_behalf_of", captured.Get("requested_token_use"))
		assert.Equal(t, "my-secret", captured.Get("client_secret"))
	})

	t.Run("credential apply error", func(t *testing.T) {
		t.Parallel()
		flow := oboFlow("http://unused", &failingCredential{}, httpc.NewClient(nil))
		_, err := flow(context.Background(), FlowParams{CallerToken: "t", Scope: "s"})
		assert.ErrorContains(t, err, "applying server credential")
	})
}

func TestClientCredentialFlow(t *testing.T) {
	t.Parallel()

	t.Run("sends correct form params", func(t *testing.T) {
		t.Parallel()
		var captured url.Values
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.NoError(t, r.ParseForm())
			captured = r.PostForm
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "cc-tok", "token_type": "Bearer", "expires_in": 3600, "scope": "api/.default",
			})
		}))
		t.Cleanup(srv.Close)

		flow := clientCredentialFlow(srv.URL, &SecretCredential{Secret: "my-secret"}, httpc.NewClient(nil))
		_, err := flow(context.Background(), FlowParams{CallerToken: "ignored", Scope: "api/.default", ClientID: "my-client"})
		require.NoError(t, err)

		assert.Equal(t, "client_credentials", captured.Get("grant_type"))
		assert.Equal(t, "my-client", captured.Get("client_id"))
		assert.Equal(t, "api/.default", captured.Get("scope"))
		assert.Equal(t, "my-secret", captured.Get("client_secret"))
		assert.Empty(t, captured.Get("assertion"))
	})

	t.Run("credential apply error", func(t *testing.T) {
		t.Parallel()
		flow := clientCredentialFlow("http://unused", &failingCredential{}, httpc.NewClient(nil))
		_, err := flow(context.Background(), FlowParams{CallerToken: "t", Scope: "s", ClientID: "c"})
		assert.ErrorContains(t, err, "applying server credential")
	})
}
