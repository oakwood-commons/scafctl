// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Command fakebrowser is a portable Go browser helper used via $BROWSER in unit
// and integration tests to follow OAuth authorization redirects to localhost
// and atomically capture the resulting HTTP status, headers, and HTML body.
package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

func main() {
	bodyPath := os.Getenv("SCAFCTL_TEST_CAPTURE_BODY")
	hdrPath := os.Getenv("SCAFCTL_TEST_CAPTURE_HEADERS")
	code := os.Getenv("SCAFCTL_TEST_OAUTH_CODE")
	if code == "" {
		code = "SUBPROC-AUTH-CODE"
	}

	statusLine := "STATUS:SCRIPT_ERROR\n"
	bodyOut := ""

	if len(os.Args) >= 2 && strings.HasPrefix(os.Args[1], "-") {
		// Simulate macOS "open -a <missing-app> <url>" failing when invoked via
		// a PATH shim so browser fallback exercises the default launcher.
		os.Exit(1)
	}

	if len(os.Args) >= 2 {
		if cbURL, ok := buildLoopbackCallbackURL(os.Args[1], code); ok {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, cbURL, nil) //nolint:gosec // restricted to validated loopback host and port
			if reqErr == nil {
				if resp, doErr := http.DefaultClient.Do(req); doErr == nil { //nolint:gosec // restricted to validated loopback host and port
					b, _ := io.ReadAll(resp.Body)
					_ = resp.Body.Close()
					bodyOut = string(b)
					statusLine = formatHeaders(resp.StatusCode, resp.Header)
				} else {
					bodyOut = "REQUEST_ERROR: " + doErr.Error()
				}
			}
			cancel()
		}
	}

	if hdrPath != "" {
		_ = writeAtomic(hdrPath, []byte(statusLine))
	}
	if bodyPath != "" {
		_ = writeAtomic(bodyPath, []byte(bodyOut))
	}
}

func buildLoopbackCallbackURL(rawAuthURL, code string) (string, bool) {
	parsed, err := url.Parse(rawAuthURL)
	if err != nil {
		return "", false
	}
	qs := parsed.Query()
	redir, err := url.Parse(qs.Get("redirect_uri"))
	if err != nil || redir.Scheme != "http" {
		return "", false
	}

	var safeHost string
	switch redir.Hostname() {
	case "localhost":
		safeHost = "localhost"
	case "127.0.0.1":
		safeHost = "127.0.0.1"
	default:
		return "", false
	}

	portNum, err := strconv.Atoi(redir.Port())
	if err != nil || portNum <= 0 || portNum > 65535 {
		return "", false
	}

	cbPath := redir.Path
	if cbPath == "" {
		cbPath = "/"
	}
	outQuery := url.Values{}
	outQuery.Set("code", code)
	outQuery.Set("state", qs.Get("state"))

	target := url.URL{
		Scheme:   "http",
		Host:     fmt.Sprintf("%s:%d", safeHost, portNum),
		Path:     cbPath,
		RawQuery: outQuery.Encode(),
	}
	return target.String(), true
}

func formatHeaders(status int, hdr http.Header) string {
	var b strings.Builder
	fmt.Fprintf(&b, "STATUS:%d\n", status)
	keys := make([]string, 0, len(hdr))
	for k := range hdr {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&b, "%s: %s\n", k, strings.Join(hdr.Values(k), ", "))
	}
	return b.String()
}

func writeAtomic(dest string, data []byte) error {
	cleanDest := filepath.Clean(dest)
	dir := filepath.Dir(cleanDest)
	base := filepath.Base(cleanDest)
	if base == "." || base == ".." || base == string(filepath.Separator) {
		return fmt.Errorf("invalid destination filename: %q", dest)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()

	tmpName := base + ".tmp"
	if err := root.WriteFile(tmpName, data, 0o600); err != nil {
		return err
	}
	return os.Rename(filepath.Join(dir, tmpName), filepath.Join(dir, base)) //nolint:gosec // confined to validated directory and basename
}
