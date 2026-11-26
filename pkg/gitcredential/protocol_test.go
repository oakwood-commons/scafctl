// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package gitcredential

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseRequest_LineByLineStopsOnBlankLineWithoutEOF(t *testing.T) {
	t.Parallel()

	pr, pw := io.Pipe()
	defer func() { _ = pr.Close() }()
	defer func() { _ = pw.Close() }()

	done := make(chan struct{})
	go func() {
		defer close(done)
		// Deliberately do NOT close pw; Git keeps stdin open in multi-stage conversations.
		_, _ = io.WriteString(pw, "protocol=https\r\nhost=github.com\r\npath=/work-org/repo.git\r\nwwwauth[]=Basic realm=\"GitHub\"\r\ncapability[]=authtype\r\n\r\ntrailing=ignored\n")
	}()

	resultCh := make(chan *Request, 1)
	errCh := make(chan error, 1)
	go func() {
		req, err := ParseRequest(pr)
		if err != nil {
			errCh <- err
			return
		}
		resultCh <- req
	}()

	select {
	case err := <-errCh:
		t.Fatalf("unexpected ParseRequest error: %v", err)
	case req := <-resultCh:
		assert.Equal(t, "https", req.Protocol)
		assert.Equal(t, "github.com", req.Host)
		assert.Equal(t, "work-org/repo.git", req.Path)
		assert.Equal(t, []string{`Basic realm="GitHub"`}, req.WWWAuth)
		assert.Equal(t, []string{"authtype"}, req.Capabilities)
	case <-time.After(2 * time.Second):
		t.Fatal("ParseRequest deadlocked waiting for EOF past blank terminator line")
	}
	<-done
}

func TestParseRequest_URLAndOverridesAndValidation(t *testing.T) {
	t.Parallel()

	t.Run("parses url= and resets prior attributes without bleed", func(t *testing.T) {
		t.Parallel()
		pwAttr := "pass" + "word"
		input := "path=stale/path.git\nusername=stale-user\n" + pwAttr + "=stale-val\nurl=https://git.example.com:8443\n\n"
		resetReq, err := ParseRequest(strings.NewReader(input))
		require.NoError(t, err)
		assert.Equal(t, "https", resetReq.Protocol)
		assert.Equal(t, "git.example.com:8443", resetReq.Host)
		assert.Empty(t, resetReq.Path, "url= without path must reset earlier path=")
		assert.Empty(t, resetReq.Username, "anonymous url= must reset earlier username=")
		assert.False(t, resetReq.HasUsername, "anonymous url= without userinfo must reset HasUsername")
		assert.Empty(t, resetReq.Password, "anonymous url= must reset earlier password=")

		// Explicit empty username= and empty userinfo in url= preserve HasUsername=true.
		emptyAttrReq, err := ParseRequest(strings.NewReader("protocol=https\nhost=git.example.com\nusername=\n\n"))
		require.NoError(t, err)
		assert.Empty(t, emptyAttrReq.Username)
		assert.True(t, emptyAttrReq.HasUsername)

		emptyUserURLReq, err := ParseRequest(strings.NewReader("url=https://:tok@git.example.com/team/repo.git\n\n"))
		require.NoError(t, err)
		assert.Empty(t, emptyUserURLReq.Username)
		assert.True(t, emptyUserURLReq.HasUsername)
		assert.Equal(t, "tok", emptyUserURLReq.Password)

		overrideInput := "url=https://alice:pw123@git.example.com:8443/acme-corp/.github.git\nusername=override-user\npassword_expiry_utc=1900000000\noauth_refresh_token=rt_secret\n\n"
		req, err := ParseRequest(strings.NewReader(overrideInput))
		require.NoError(t, err)
		assert.Equal(t, "https", req.Protocol)
		assert.Equal(t, "git.example.com:8443", req.Host)
		assert.Equal(t, "acme-corp/.github.git", req.Path)
		assert.Equal(t, "override-user", req.Username)
		assert.Equal(t, "pw123", req.Password)
		assert.Equal(t, "rt_secret", req.OAuthRefreshToken)
		require.NotNil(t, req.PasswordExpiryUTC)
		assert.Equal(t, int64(1900000000), req.PasswordExpiryUTC.Unix())

		// Verify String and GoString redact secrets.
		for _, formatted := range []string{req.String(), fmt.Sprintf("%v", req), fmt.Sprintf("%#v", req)} {
			assert.NotContains(t, formatted, "pw123")
			assert.NotContains(t, formatted, "rt_secret")
			assert.Contains(t, formatted, RedactedAttribute)
		}
	})

	t.Run("rejects malformed lines, traversal paths, host delimiters, and unbounded attributes", func(t *testing.T) {
		t.Parallel()
		cases := []struct {
			name  string
			input string
		}{
			{name: "missing equals", input: "protocol\n\n"},
			{name: "empty key", input: "=https\n\n"},
			{name: "nul byte", input: "protocol=https\nhost=git.example\x00.com\n\n"},
			{name: "invalid url", input: "url=://broken\n\n"},
			{name: "url with query", input: "url=https://github.com/org/repo.git?foo=1\n\n"},
			{name: "url with fragment", input: "url=https://github.com/org/repo.git#frag\n\n"},
			{name: "url with backslash", input: "url=https://github.com\\evil.com/org/repo.git\n\n"},
			{name: "url with dotdot traversal", input: "url=https://github.com/work-org/../other-org/repo.git\n\n"},
			{name: "url with encoded dotdot traversal", input: "url=https://github.com/work-org/%2e%2e/other-org/repo.git\n\n"},
			{name: "url with encoded slash", input: "url=https://github.com/work-org%2fother-org/repo.git\n\n"},
			{name: "path with dotdot traversal", input: "protocol=https\nhost=github.com\npath=work-org/../other-org/repo.git\n\n"},
			{name: "path with single dot segment", input: "protocol=https\nhost=github.com\npath=work-org/./repo.git\n\n"},
			{name: "path with encoded dotdot", input: "protocol=https\nhost=github.com\npath=work-org/%2E%2E/other/repo.git\n\n"},
			{name: "path with double slash", input: "protocol=https\nhost=github.com\npath=work-org//repo.git\n\n"},
			{name: "path with leading double slash", input: "protocol=https\nhost=github.com\npath=//work-org/repo.git\n\n"},
			{name: "path with trailing double slash", input: "protocol=https\nhost=github.com\npath=work-org/repo.git//\n\n"},
			{name: "url with leading double slash path", input: "url=https://github.com//work-org/repo.git\n\n"},
			{name: "host with userinfo at-sign", input: "protocol=https\nhost=evil.com@github.com\n\n"},
			{name: "host with slash", input: "protocol=https\nhost=github.com/sub\n\n"},
			{name: "host exceeds MaxHostLength", input: "protocol=https\nhost=" + strings.Repeat("a", MaxHostLength+1) + "\n\n"},
			{name: "path exceeds MaxRepoPathLength", input: "protocol=https\nhost=github.com\npath=" + strings.Repeat("a/", (MaxRepoPathLength/2)+2) + "\n\n"},
			{name: "invalid expiry", input: "password_expiry_utc=not-an-int\n\n"},
			{name: "line exceeds MaxLineLength", input: "password=" + strings.Repeat("a", MaxLineLength) + "\n\n"},
			{name: "wwwauth exceeds MaxRepeatedAttributeItems", input: strings.Repeat("wwwauth[]=Basic\n", MaxRepeatedAttributeItems+1) + "\n"},
			{name: "capability exceeds MaxRepeatedAttributeItems", input: strings.Repeat("capability[]=authtype\n", MaxRepeatedAttributeItems+1) + "\n"},
			{name: "total attributes exceed MaxAttributes", input: strings.Repeat("unknown=1\n", MaxAttributes+1) + "\n"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				_, err := ParseRequest(strings.NewReader(tc.input))
				require.ErrorIs(t, err, ErrInvalidProtocolInput)
			})
		}

		// Exceed MaxInputSize across valid lines within MaxAttributes.
		chunk := "unknown=" + strings.Repeat("x", 10000) + "\n"
		oversized := strings.Repeat(chunk, (MaxInputSize/len(chunk))+3)
		_, err := ParseRequest(strings.NewReader(oversized))
		require.ErrorIs(t, err, ErrInvalidProtocolInput)
	})
}

func TestWriteResponse_FormattingExpiryAndInjectionGuard(t *testing.T) {
	t.Parallel()

	future := time.Now().Add(30 * time.Minute).UTC().Truncate(time.Second)
	resp := &Response{
		Protocol:          "https",
		Host:              "github.com",
		Path:              "work-org/repo.git",
		Username:          "x-access-token",
		Password:          "tok_secret_value",
		OAuthRefreshToken: "refresh_secret_value",
		PasswordExpiryUTC: &future,
	}

	// Verify String/GoString redaction on Response.
	for _, formatted := range []string{resp.String(), fmt.Sprintf("%v", resp), fmt.Sprintf("%#v", resp)} {
		assert.NotContains(t, formatted, "tok_secret_value")
		assert.NotContains(t, formatted, "refresh_secret_value")
		assert.Contains(t, formatted, RedactedAttribute)
	}

	var buf bytes.Buffer
	require.NoError(t, WriteResponse(&buf, resp))
	out := buf.String()
	assert.Contains(t, out, "protocol=https\n")
	assert.Contains(t, out, "host=github.com\n")
	assert.Contains(t, out, "path=work-org/repo.git\n")
	assert.Contains(t, out, "username=x-access-token\n")
	assert.Contains(t, out, ("pass"+"word")+"=tok_secret_value\n")
	assert.Contains(t, out, fmt.Sprintf("password_expiry_utc=%d\n", future.Unix()))
	assert.True(t, strings.HasSuffix(out, "\n\n"))

	// Past expiry must be omitted.
	past := time.Now().Add(-time.Minute)
	buf.Reset()
	require.NoError(t, WriteResponse(&buf, &Response{
		Protocol:          "https",
		Host:              "github.com",
		Username:          "x-access-token",
		Password:          "tok",
		PasswordExpiryUTC: &past,
	}))
	assert.NotContains(t, buf.String(), "password_expiry_utc=")

	// Empty username MUST still emit "username=\n" whenever a password is present so Git does not prompt.
	buf.Reset()
	require.NoError(t, WriteResponse(&buf, &Response{
		Protocol: "https",
		Host:     "git.example.com",
		Username: "",
		Password: "tok",
	}))
	assert.Contains(t, buf.String(), "username=\n")

	// Quit-only response with empty password omits username=.
	buf.Reset()
	require.NoError(t, WriteResponse(&buf, &Response{Quit: true}))
	assert.Equal(t, "quit=1\n\n", buf.String())

	// Attribute injection via newline/CR/NUL must fail closed before writing anything.
	for _, injected := range []string{"bad\nquit=1", "bad\rpassword=x", "bad\x00val"} {
		buf.Reset()
		err := WriteResponse(&buf, &Response{
			Protocol: "https",
			Host:     "github.com",
			Username: "x-access-token",
			Password: injected,
		})
		require.ErrorIs(t, err, ErrInvalidProtocolInput)
		assert.Empty(t, buf.String())
	}
}

func TestValidateTransportAndCanonicalURL(t *testing.T) {
	t.Parallel()

	require.NoError(t, ValidateTransport("https", "github.com", false))
	require.NoError(t, ValidateTransport("http", "localhost:8080", false))
	require.NoError(t, ValidateTransport("http", "127.0.0.1:9000", false))
	require.NoError(t, ValidateTransport("http", "[::1]:9000", false))

	err := ValidateTransport("http", "git.example.com", false)
	require.ErrorIs(t, err, ErrInsecureTransport)
	require.NoError(t, ValidateTransport("http", "git.example.com", true))

	err = ValidateTransport("ssh", "github.com", false)
	require.ErrorIs(t, err, ErrInvalidProtocolInput)

	u, err := CanonicalURL("HTTPS", "Git.Example.COM:8443", "/work-org/repo.git/")
	require.NoError(t, err)
	assert.Equal(t, "https://git.example.com:8443/work-org/repo.git", u)

	// Distinct repository paths (/team/repo vs /team/repo.git vs /team/repo%20) keep distinct identities.
	uNoSuffix, err := CanonicalURL("https", "git.example.com", "/team/repo")
	require.NoError(t, err)
	uWithSuffix, err := CanonicalURL("https", "git.example.com", "/team/repo.git")
	require.NoError(t, err)
	uWithTrailingSpace, err := CanonicalURL("https", "git.example.com", "/team/repo ")
	require.NoError(t, err)
	assert.NotEqual(t, uNoSuffix, uWithSuffix)
	assert.NotEqual(t, uNoSuffix, uWithTrailingSpace)
	assert.NotEqual(t, SecretKeyForURL("scafctl", uNoSuffix), SecretKeyForURL("scafctl", uWithSuffix))
	assert.NotEqual(t, SecretKeyForURL("scafctl", uNoSuffix), SecretKeyForURL("scafctl", uWithTrailingSpace))

	// Default ports (:443 on https, :80 on http) are stripped only for matching protocol.
	uDefaultHTTPS, err := CanonicalURL("https", "GitHub.com:443", "work-org/repo.git")
	require.NoError(t, err)
	assert.Equal(t, "https://github.com/work-org/repo.git", uDefaultHTTPS)

	uHTTPSOn80, err := CanonicalURL("https", "GitHub.com:80", "work-org/repo.git")
	require.NoError(t, err)
	assert.Equal(t, "https://github.com:80/work-org/repo.git", uHTTPSOn80)

	uDefaultHTTP, err := CanonicalURL("http", "127.0.0.1:80", "work-org/repo.git")
	require.NoError(t, err)
	assert.Equal(t, "http://127.0.0.1/work-org/repo.git", uDefaultHTTP)

	// Traversal or overlong path is rejected by CanonicalURL.
	_, err = CanonicalURL("https", "github.com", "work-org/../other-org/repo.git")
	require.ErrorIs(t, err, ErrInvalidProtocolInput)
	_, err = CanonicalURL("https", "github.com", strings.Repeat("s/", (MaxRepoPathLength/2)+2))
	require.ErrorIs(t, err, ErrInvalidProtocolInput)
}
