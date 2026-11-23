// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateBrowser(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		input          string
		wantNormalized string
		wantErr        string
	}{
		{name: "empty is unset", input: "", wantNormalized: ""},
		{name: "msedge", input: "msedge", wantNormalized: "msedge"},
		{name: "chrome", input: "chrome", wantNormalized: "chrome"},
		{name: "firefox", input: "firefox", wantNormalized: "firefox"},
		{name: "safari", input: "safari", wantNormalized: "safari"},
		{name: "default keyword", input: "default", wantNormalized: "default"},
		{name: "system keyword", input: "system", wantNormalized: "system"},
		{name: "app name with space", input: "Microsoft Edge", wantNormalized: "Microsoft Edge"},
		{name: "surrounding whitespace within length limit", input: "  firefox  ", wantNormalized: "firefox"},
		{name: "unix executable path", input: "/usr/bin/firefox", wantNormalized: "/usr/bin/firefox"},
		{name: "windows executable path", input: `C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`, wantNormalized: `C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`},
		{name: "apostrophe in directory path", input: `/Users/O'Connor/Applications/Firefox.app`, wantNormalized: `/Users/O'Connor/Applications/Firefox.app`},
		{name: "quoted browser token", input: `"msedge"`, wantNormalized: "msedge"},
		{name: "single-quoted browser token", input: `'msedge'`, wantNormalized: "msedge"},
		{name: "single-quoted browser token with spaces", input: ` 'firefox' `, wantNormalized: "firefox"},
		{name: "max length boundary", input: strings.Repeat("a", MaxAuthBrowserLength), wantNormalized: strings.Repeat("a", MaxAuthBrowserLength)},
		{name: "exceeds max length", input: strings.Repeat("a", MaxAuthBrowserLength+1), wantErr: "exceeds maximum length"},
		{name: "padded value exceeding max length before trim", input: "  " + strings.Repeat("a", MaxAuthBrowserLength), wantErr: "exceeds maximum length"},
		{name: "invalid utf8", input: string([]byte{0xff, 0xfe}), wantErr: "invalid UTF-8"},
		{name: "newline control char", input: "msedge\n", wantErr: "control characters"},
		{name: "null byte", input: "msedge\x00", wantErr: "control characters"},
		{name: "tab char", input: "\tmsedge", wantErr: "control characters"},
		{name: "DEL char", input: "msedge\x7f", wantErr: "control characters"},
		{name: "whitespace only", input: "   ", wantErr: "whitespace-only"},
		{name: "empty quotes", input: `""`, wantErr: "must not be empty"},
		{name: "empty single quotes", input: `''`, wantErr: "must not be empty"},
		{name: "nested empty double quotes", input: `""""`, wantErr: "must not contain double quotes"},
		{name: "nested single quotes", input: `''firefox''`, wantErr: "nested or unmatched single quotes"},
		{name: "double-quoted single-quoted token", input: `"'firefox'"`, wantErr: "nested or unmatched single quotes"},
		{name: "single-quoted double-quoted token", input: `'"firefox"'`, wantErr: "must not contain double quotes"},
		{name: "unmatched leading single quote", input: `'firefox`, wantErr: "unmatched single quotes"},
		{name: "unmatched trailing single quote", input: `firefox'`, wantErr: "unmatched single quotes"},
		{name: "unmatched leading double quote", input: `"firefox`, wantErr: "must not contain double quotes"},
		{name: "unmatched trailing double quote", input: `firefox"`, wantErr: "must not contain double quotes"},
		{name: "whitespace quotes", input: `"   "`, wantErr: "must not be empty"},
		{name: "leading single dash flag", input: "-a", wantErr: "must not start with '-'"},
		{name: "leading double dash flag", input: "--headless", wantErr: "must not start with '-'"},
		{name: "padded leading dash", input: "  --remote-debugging-port=9222", wantErr: "must not start with '-'"},
		{name: "quoted leading dash", input: `"--headless"`, wantErr: "must not start with '-'"},
		{name: "single-quoted leading dash", input: `'--headless'`, wantErr: "must not start with '-'"},
		{name: "nested double-quoted leading dash", input: `""--headless""`, wantErr: "must not contain double quotes"},
		{name: "unmatched leading quote dash", input: `"--headless`, wantErr: "must not contain double quotes"},
		{name: "embedded double quote", input: `msedge" --flag`, wantErr: "must not contain double quotes"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateBrowser(tc.input)
			if tc.wantErr == "" {
				require.NoError(t, err)
				assert.Equal(t, tc.wantNormalized, NormalizeBrowser(tc.input))
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
			}
		})
	}
}

func TestLoadErrorContext(t *testing.T) {
	t.Parallel()

	assert.Nil(t, LoadErrorFromContext(nil)) //nolint:staticcheck // intentionally testing nil context guard
	assert.Nil(t, FromContext(nil))          //nolint:staticcheck // intentionally testing nil context guard
	base := context.Background()
	assert.Nil(t, LoadErrorFromContext(base))
	assert.Equal(t, base, WithLoadError(base, nil))

	sentinel := errors.New("broken config")
	withErr := WithLoadError(base, sentinel)
	assert.ErrorIs(t, LoadErrorFromContext(withErr), sentinel)
}

func TestConfigValidate_AuthBrowser(t *testing.T) {
	t.Parallel()

	var validCfg Config
	validCfg.Auth.Browser = "msedge"
	require.NoError(t, validCfg.Validate())

	var invalidCfg Config
	invalidCfg.Auth.Browser = "--inject-flag"
	err := invalidCfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "auth: browser:")
}

func TestAuthBrowserStructTagsAndSanitize(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		typ  reflect.Type
	}{
		{name: "GlobalAuthConfig", typ: reflect.TypeFor[GlobalAuthConfig]()},
		{name: "SanitizedAuth", typ: reflect.TypeFor[SanitizedAuth]()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			field, ok := tc.typ.FieldByName("Browser")
			require.True(t, ok, "%s must define Browser", tc.name)
			assert.NotEmpty(t, field.Tag.Get("doc"))
			assert.Equal(t, "msedge", field.Tag.Get("example"))
			assert.Equal(t, "256", field.Tag.Get("maxLength"))
		})
	}

	sanitized := SanitizeConfig(&Config{
		Auth: GlobalAuthConfig{Browser: "firefox"},
	})
	assert.Equal(t, "firefox", sanitized.Auth.Browser)

	known := getKnownConfigKeys()
	assert.True(t, isKnownKey("auth.browser", known), "auth.browser must be recognized as a known config key")
}

func TestManager_AuthBrowser_SetSaveReloadAndDropIn(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	dropInDir := filepath.Join(tmpDir, ConfigDirName)
	require.NoError(t, os.MkdirAll(dropInDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dropInDir, "10-logging.yaml"), []byte("logging:\n  level: info\n"), 0o600))

	mgr := NewManager(configPath)
	_, err := mgr.Load()
	require.NoError(t, err)

	mgr.Set("auth.browser", "msedge")
	require.NoError(t, mgr.Save())

	mgrReload := NewManager(configPath)
	reloaded, err := mgrReload.Load()
	require.NoError(t, err)
	assert.Equal(t, "msedge", reloaded.Auth.Browser)

	// Invalid value via Set is rejected by Save before corrupting disk.
	mgrReload.Set("auth.browser", "--bad-flag")
	err = mgrReload.Save()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "refusing to save invalid config")

	// Also exercise bool, int, and nil branches in Manager.Set("auth.browser", ...).
	mgrReload.Set("auth.browser", true)
	assert.Equal(t, "true", mgrReload.Config().Auth.Browser)
	mgrReload.Set("auth.browser", 42)
	assert.Equal(t, "42", mgrReload.Config().Auth.Browser)
	mgrReload.Set("auth.browser", nil)
	assert.Empty(t, mgrReload.Config().Auth.Browser)
}

func TestManager_AuthBrowser_EnvOverrideAndEmbedderPrefix(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	t.Setenv("MYCLI_AUTH_BROWSER", "brave")
	t.Setenv("SCAFCTL_AUTH_BROWSER", "firefox")

	mgr := NewManager(configPath, WithEnvPrefix("mycli"))
	cfg, err := mgr.Load()
	require.NoError(t, err)
	assert.Equal(t, "brave", cfg.Auth.Browser)
}
