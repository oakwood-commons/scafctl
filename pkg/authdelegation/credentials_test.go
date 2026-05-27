package authdelegation

import (
	"net/url"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSecretCredential_Apply(t *testing.T) {
	t.Parallel()
	v := url.Values{}
	c := &SecretCredential{Secret: "s3cr3t"}
	require.NoError(t, c.Apply(v))
	assert.Equal(t, "s3cr3t", v.Get("client_secret"))
}

func TestWIFCredential_Apply(t *testing.T) {
	t.Parallel()

	t.Run("reads file and sets assertion params", func(t *testing.T) {
		t.Parallel()
		f := writeTempToken(t, "my-wif-token")
		v := url.Values{}
		c := &WIFCredential{
			TokenFile:           f,
			ClientAssertionType: "urn:ietf:params:oauth:client-assertion-type:jwt-bearer",
		}
		require.NoError(t, c.Apply(v))
		assert.Equal(t, "my-wif-token", v.Get("client_assertion"))
		assert.Equal(t, "urn:ietf:params:oauth:client-assertion-type:jwt-bearer", v.Get("client_assertion_type"))
	})

	t.Run("trims whitespace from token", func(t *testing.T) {
		t.Parallel()
		f := writeTempToken(t, "  trimmed-token\n")
		v := url.Values{}
		c := &WIFCredential{TokenFile: f, ClientAssertionType: "urn:ietf:params:oauth:client-assertion-type:jwt-bearer"}
		require.NoError(t, c.Apply(v))
		assert.Equal(t, "trimmed-token", v.Get("client_assertion"))
	})

	t.Run("missing file returns error", func(t *testing.T) {
		t.Parallel()
		c := &WIFCredential{TokenFile: "/nonexistent/token", ClientAssertionType: "urn:ietf:params:oauth:client-assertion-type:jwt-bearer"}
		err := c.Apply(url.Values{})
		assert.ErrorContains(t, err, "reading federated token file")
	})

	t.Run("empty file returns error", func(t *testing.T) {
		t.Parallel()
		f := writeTempToken(t, "   ")
		c := &WIFCredential{TokenFile: f, ClientAssertionType: "urn:ietf:params:oauth:client-assertion-type:jwt-bearer"}
		err := c.Apply(url.Values{})
		assert.ErrorContains(t, err, "is empty")
	})
}

func writeTempToken(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "wif-token-*")
	require.NoError(t, err)
	_, err = f.WriteString(content)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	return f.Name()
}
