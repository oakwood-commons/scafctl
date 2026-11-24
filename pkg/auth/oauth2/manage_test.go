// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package oauth2

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/secrets"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubNameChecker struct {
	names map[string]struct{}
}

type onReadHookReader struct {
	r      io.Reader
	onRead func()
	fired  bool
}

func (o *onReadHookReader) Read(p []byte) (int, error) {
	if !o.fired && o.onRead != nil {
		o.fired = true
		o.onRead()
	}
	return o.r.Read(p)
}

func newStubNameChecker(names ...string) *stubNameChecker {
	m := make(map[string]struct{}, len(names))
	for _, n := range names {
		m[n] = struct{}{}
	}
	return &stubNameChecker{names: m}
}

func (s *stubNameChecker) Has(name string) bool {
	_, ok := s.names[name]
	return ok
}

func TestValidateHandlerName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		input       string
		binaryNames []string
		wantErr     string
	}{
		{name: "simple name", input: "quay"},
		{name: "hyphens underscores dots", input: "my-corp_idp.v2"},
		{name: "max length 64", input: strings.Repeat("a", config.MaxCustomOAuth2NameLength)},
		{name: "empty", input: "", wantErr: "name is required"},
		{name: "exceeds max length", input: strings.Repeat("a", config.MaxCustomOAuth2NameLength+1), wantErr: "exceeds maximum length"},
		{name: "leading hyphen", input: "-quay", wantErr: "must start with an ASCII letter or digit"},
		{name: "leading dot", input: ".quay", wantErr: "must start with an ASCII letter or digit"},
		{name: "double dot", input: "foo..bar", wantErr: "must not contain path separators or '..'"},
		{name: "path slash", input: "foo/bar", wantErr: "must not contain path separators"},
		{name: "whitespace", input: "my idp", wantErr: "invalid characters"},
		{name: "reserved scafctl prefix", input: "scafctl.internal", wantErr: "reserved prefix"},
		{name: "reserved exact scafctl", input: "scafctl", wantErr: "reserved prefix"},
		{name: "reserved embedder prefix", input: "mycli.token", binaryNames: []string{"mycli"}, wantErr: "reserved prefix"},
		{name: "reserved exact embedder", input: "mycli", binaryNames: []string{"mycli"}, wantErr: "reserved prefix"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateHandlerName(tc.input, tc.binaryNames...)
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestValidateFlagSecretRef(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		flagName       string
		raw            string
		allowStdinHint bool
		binaryNames    []string
		wantErr        string
		wantPlaintext  bool
	}{
		{name: "empty allowed", flagName: "client-secret", raw: ""},
		{name: "secret ref", flagName: "client-secret", raw: "secret://quay-secret"},
		{name: "env ref", flagName: "federated-token", raw: "env://OIDC_TOKEN"},
		{name: "file ref", flagName: "token", raw: "file:///var/run/secrets/token"},
		{
			name:           "bare client-secret rejected with stdin hint",
			flagName:       "client-secret",
			raw:            "super-secret-plaintext",
			allowStdinHint: true,
			wantErr:        "--client-secret-stdin",
			wantPlaintext:  true,
		},
		{
			name:           "bare token rejected without stdin hint",
			flagName:       "token",
			raw:            "ghp_plaintext",
			allowStdinHint: false,
			wantErr:        "must be a SecretRef URI",
			wantPlaintext:  true,
		},
		{
			name:          "unsupported uri scheme",
			flagName:      "client-secret",
			raw:           "vault://kv/secret",
			wantErr:       "must be a SecretRef URI",
			wantPlaintext: true,
		},
		{
			name:     "reserved scafctl secret prefix",
			flagName: "client-secret",
			raw:      "secret://scafctl.auth.oauth2.quay.refresh_token",
			wantErr:  "reserved internal secret prefix",
		},
		{
			name:        "reserved embedder secret prefix",
			flagName:    "token",
			raw:         "secret://mycli.internal",
			binaryNames: []string{"mycli"},
			wantErr:     "reserved internal secret prefix",
		},
		{
			name:     "empty env var name",
			flagName: "token",
			raw:      "env://",
			wantErr:  "requires a variable name",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateFlagSecretRef(tc.flagName, tc.raw, tc.allowStdinHint, tc.binaryNames...)
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				if tc.wantPlaintext {
					assert.ErrorIs(t, err, ErrPlaintextSecretFlag)
				}
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestParseConfigYAMLAndLoadConfigFile(t *testing.T) {
	t.Parallel()

	t.Run("parses rich YAML with tokenExchange and DCR", func(t *testing.T) {
		t.Parallel()
		raw := `
name: custom-ar
displayName: Custom Artifact Registry
tokenURL: https://idp.example.com/oauth2/token
defaultFlow: client_credentials
scopes:
  - registry:read
registry: us-docker.pkg.dev
registryUsername: oauth2accesstoken
tokenExchange:
  url: https://us-docker.pkg.dev/v2/token
  method: GET
  tokenJSONPath: token
dynamicClientRegistration:
  registrationEndpoint: https://idp.example.com/oauth2/register
  initialAccessToken: env://DCR_INIT_TOKEN
  clientMetadata:
    client_name: scafctl-cli
`
		tmpFile := filepath.Join(t.TempDir(), "handler.yaml")
		require.NoError(t, os.WriteFile(tmpFile, []byte(raw), 0o600))

		cfg, err := LoadConfigFile(tmpFile)
		require.NoError(t, err)
		assert.Equal(t, "custom-ar", cfg.Name)
		assert.Equal(t, "Custom Artifact Registry", cfg.DisplayName)
		require.NotNil(t, cfg.TokenExchange)
		assert.Equal(t, "token", cfg.TokenExchange.TokenJSONPath)
		require.NotNil(t, cfg.DynamicClientRegistration)
		assert.Equal(t, "https://idp.example.com/oauth2/register", cfg.DynamicClientRegistration.RegistrationEndpoint)
		assert.NoError(t, ValidateConfig(cfg))
	})

	t.Run("rejects unknown field typo", func(t *testing.T) {
		t.Parallel()
		_, err := ParseConfigYAML([]byte("name: quay\nclientId: wrong-casing\n"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "clientId")
	})

	t.Run("rejects empty file", func(t *testing.T) {
		t.Parallel()
		_, err := ParseConfigYAML([]byte("   \n  "))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "empty")
	})

	t.Run("rejects multiple YAML documents", func(t *testing.T) {
		t.Parallel()
		_, err := ParseConfigYAML([]byte("name: one\n---\nname: two\n"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "single YAML document")
	})

	t.Run("missing file preserves os.ErrNotExist", func(t *testing.T) {
		t.Parallel()
		_, err := LoadConfigFile(filepath.Join(t.TempDir(), "missing.yaml"))
		require.Error(t, err)
		assert.ErrorIs(t, err, os.ErrNotExist)
	})

	t.Run("custom ReadFileFunc seam cleans path and parses content", func(t *testing.T) {
		t.Parallel()
		var gotPath string
		cfg, err := LoadConfigFile("dir/../handler.yaml", func(filename string) ([]byte, error) {
			gotPath = filename
			return []byte("name: seam-idp\ndefaultFlow: pat\ntoken: env://SEAM_TOKEN\n"), nil
		})
		require.NoError(t, err)
		assert.Equal(t, "handler.yaml", gotPath)
		assert.Equal(t, "seam-idp", cfg.Name)
	})
}

func TestCheckHandlerNameCollision(t *testing.T) {
	t.Parallel()

	t.Run("rejects official handler from NameChecker without hardcoding", func(t *testing.T) {
		t.Parallel()
		officialReg := newStubNameChecker("custom-official")

		err := CheckHandlerNameCollision(context.Background(), &config.Config{}, officialReg, "custom-official")
		require.ErrorIs(t, err, ErrReservedHandlerName)
	})

	t.Run("handles typed-nil NameChecker safely", func(t *testing.T) {
		t.Parallel()
		var typedNil *stubNameChecker
		assert.NoError(t, CheckHandlerNameCollision(context.Background(), &config.Config{}, typedNil, "github"))
	})

	t.Run("allows official handler name when DisableOfficialAuthHandlers is true", func(t *testing.T) {
		t.Parallel()
		officialReg := newStubNameChecker("github", "entra", "gcp", "openshift")
		cfg := &config.Config{
			Settings: config.Settings{DisableOfficialAuthHandlers: true},
		}

		assert.NoError(t, CheckHandlerNameCollision(context.Background(), cfg, officialReg, "github"))
	})

	t.Run("rejects registered non-custom handler without invoking fallback", func(t *testing.T) {
		t.Parallel()
		fallbackCalled := false
		reg := auth.NewRegistry(auth.WithFallbackResolver(func(_ context.Context, _ string) (auth.Handler, error) {
			fallbackCalled = true
			return nil, fmt.Errorf("unexpected fallback call")
		}))
		require.NoError(t, reg.Register(auth.NewMockHandler("embedder-sso")))
		ctx := auth.WithRegistry(context.Background(), reg)

		err := CheckHandlerNameCollision(ctx, &config.Config{}, nil, "embedder-sso")
		require.ErrorIs(t, err, ErrReservedHandlerName)
		assert.False(t, fallbackCalled, "collision check must never trigger network fallback")

		// Unregistered name also must not trigger fallback.
		assert.NoError(t, CheckHandlerNameCollision(ctx, &config.Config{}, nil, "my-quay"))
		assert.False(t, fallbackCalled, "collision check must never trigger network fallback")
	})

	t.Run("rejects embedder built-in oauth2.Handler even when cfg.Auth.CustomOAuth2 contains same name", func(t *testing.T) {
		t.Parallel()
		builtinOAuth, err := New(config.CustomOAuth2Config{
			Name:         "corp-sso",
			AuthorizeURL: "https://sso.example.com/authorize",
			TokenURL:     "https://sso.example.com/token",
			ClientID:     "corp-cli",
		}, WithSecretStore(secrets.NewMockStore()))
		require.NoError(t, err)

		reg := auth.NewRegistry()
		require.NoError(t, reg.Register(builtinOAuth))
		require.NoError(t, reg.Disable("corp-sso", "server mode"))
		ctx := auth.WithRegistry(context.Background(), reg)

		err = CheckHandlerNameCollision(ctx, &config.Config{}, nil, "corp-sso")
		require.ErrorIs(t, err, ErrReservedHandlerName)

		// Even when config.yaml also has a stale customOAuth2 entry with the same
		// name, the registered built-in handler (IsCustomConfig() == false) still wins
		// at startup and must be rejected as a collision (including under --force).
		cfgWithSameName := &config.Config{
			Auth: config.GlobalAuthConfig{
				CustomOAuth2: []config.CustomOAuth2Config{
					{
						Name:        "corp-sso",
						DefaultFlow: "pat",
						Token:       "env://STALE_CORP_TOKEN",
					},
				},
			},
		}
		err = CheckHandlerNameCollision(ctx, cfgWithSameName, nil, "corp-sso")
		require.ErrorIs(t, err, ErrReservedHandlerName)

		cfgPath := filepath.Join(t.TempDir(), "config.yaml")
		require.NoError(t, os.WriteFile(cfgPath, []byte("auth:\n  customOAuth2:\n    - name: corp-sso\n      defaultFlow: pat\n      token: env://STALE_CORP_TOKEN\n"), 0o600))
		_, err = AddCustomOAuth2(ctx, AddOptions{
			ConfigPath: cfgPath,
			Force:      true,
			Entry: config.CustomOAuth2Config{
				Name:        "corp-sso",
				DefaultFlow: "pat",
				Token:       "env://NEW_CORP_TOKEN",
			},
		})
		require.ErrorIs(t, err, ErrReservedHandlerName)
	})

	t.Run("allows existing customOAuth2 handler registered in auth.Registry", func(t *testing.T) {
		t.Parallel()
		oc := config.CustomOAuth2Config{
			Name:         "quay",
			AuthorizeURL: "https://quay.io/oauth/authorize",
			ClientID:     "cid",
			ResponseType: "token",
		}
		h, err := New(oc, WithSecretStore(secrets.NewMockStore()), WithCustomConfigSource())
		require.NoError(t, err)

		reg := auth.NewRegistry()
		require.NoError(t, reg.Register(h))
		ctx := auth.WithRegistry(context.Background(), reg)
		cfg := &config.Config{
			Auth: config.GlobalAuthConfig{
				CustomOAuth2: []config.CustomOAuth2Config{oc},
			},
		}

		assert.NoError(t, CheckHandlerNameCollision(ctx, cfg, nil, "quay"))
	})

	t.Run("rejects config-pinned plugin handler", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{
			Auth: config.GlobalAuthConfig{
				Handlers: map[string]config.HandlerConfig{
					"acme": {Plugin: &config.HandlerPluginConfig{Ref: "acme-auth"}},
				},
			},
		}
		err := CheckHandlerNameCollision(context.Background(), cfg, nil, "acme")
		require.ErrorIs(t, err, ErrReservedHandlerName)
	})
}

func TestAddRemoveListCustomOAuth2(t *testing.T) {
	t.Parallel()

	t.Run("lifecycle add, update with force, list sanitized, and remove", func(t *testing.T) {
		t.Parallel()
		configPath := filepath.Join(t.TempDir(), "config.yaml")
		store := secrets.NewMockStore()
		officialReg := newStubNameChecker("entra", "gcp", "github", "openshift")
		ctx := secrets.WithStore(context.Background(), store)

		// Empty list returns non-nil empty slice.
		initial, err := ListCustomOAuth2(ctx, configPath)
		require.NoError(t, err)
		require.NotNil(t, initial)
		assert.Empty(t, initial)

		// Add via stdin client secret.
		res, err := AddCustomOAuth2(ctx, AddOptions{
			ConfigPath: configPath,
			Entry: config.CustomOAuth2Config{
				Name:        "quay",
				DisplayName: "Quay.io",
				TokenURL:    "https://quay.io/oauth/token",
				ClientID:    "quay-client",
				DefaultFlow: "client_credentials",
				Registry:    "quay.io",
			},
			ReadClientSecretStdin: true,
			ClientSecretReader:    strings.NewReader("my-super-secret\r\n"),
		})
		require.NoError(t, err)
		assert.Equal(t, "quay", res.Name)
		assert.False(t, res.Updated)
		assert.Equal(t, "quay-client-secret", res.StoredSecretName)

		storedBytes, err := store.Get(ctx, "quay-client-secret")
		require.NoError(t, err)
		assert.Equal(t, "my-super-secret", string(storedBytes))

		// Duplicate without Force fails and does not overwrite secret.
		_, err = AddCustomOAuth2(ctx, AddOptions{
			ConfigPath: configPath,
			Entry: config.CustomOAuth2Config{
				Name:        "quay",
				TokenURL:    "https://quay.io/oauth/token",
				ClientID:    "other",
				DefaultFlow: "client_credentials",
			},
			ReadClientSecretStdin: true,
			ClientSecretReader:    strings.NewReader("should-not-overwrite"),
		})
		require.ErrorIs(t, err, ErrHandlerAlreadyExists)
		storedBytes, err = store.Get(ctx, "quay-client-secret")
		require.NoError(t, err)
		assert.Equal(t, "my-super-secret", string(storedBytes))

		// Collision with official handler fails even when Force is true.
		_, err = AddCustomOAuth2(ctx, AddOptions{
			ConfigPath:       configPath,
			Force:            true,
			OfficialHandlers: officialReg,
			Entry: config.CustomOAuth2Config{
				Name:         "github",
				AuthorizeURL: "https://github.com/login/oauth/authorize",
				TokenURL:     "https://github.com/login/oauth/access_token",
				ClientID:     "cid",
			},
		})
		require.ErrorIs(t, err, ErrReservedHandlerName)

		// Overwrite existing entry with Force.
		res2, err := AddCustomOAuth2(ctx, AddOptions{
			ConfigPath: configPath,
			Force:      true,
			Entry: config.CustomOAuth2Config{
				Name:         "quay",
				DisplayName:  "Quay Updated",
				AuthorizeURL: "https://quay.io/oauth/authorize",
				ClientID:     "quay-client-v2",
				DefaultFlow:  "interactive",
				ResponseType: "token",
				Token:        "env://QUAY_ROBOT_TOKEN",
				Username:     "secret://quay-robot-user",
			},
		})
		require.NoError(t, err)
		assert.True(t, res2.Updated)

		// List returns sanitized fields -- never raw secret refs or plaintext.
		listed, err := ListCustomOAuth2(ctx, configPath)
		require.NoError(t, err)
		require.Len(t, listed, 1)
		assert.Equal(t, "quay", listed[0].Name)
		assert.Equal(t, "Quay Updated", listed[0].DisplayName)
		assert.Equal(t, config.RedactedValue, listed[0].Token)
		assert.Equal(t, config.RedactedValue, listed[0].Username)

		// Remove handler and verify config round-trips back to empty.
		require.NoError(t, RemoveCustomOAuth2(ctx, configPath, "quay"))
		afterRemove, err := ListCustomOAuth2(ctx, configPath)
		require.NoError(t, err)
		assert.Empty(t, afterRemove)

		// Removing absent handler returns ErrHandlerNotFound.
		err = RemoveCustomOAuth2(ctx, configPath, "quay")
		require.ErrorIs(t, err, ErrHandlerNotFound)
	})

	t.Run("enforces MaxCustomOAuth2Handlers capacity limit", func(t *testing.T) {
		t.Parallel()
		configPath := filepath.Join(t.TempDir(), "config.yaml")
		ctx := context.Background()

		for i := range config.MaxCustomOAuth2Handlers {
			_, err := AddCustomOAuth2(ctx, AddOptions{
				ConfigPath: configPath,
				Entry: config.CustomOAuth2Config{
					Name:        fmt.Sprintf("idp-%02d", i),
					DefaultFlow: "pat",
					Token:       "env://API_TOKEN",
				},
			})
			require.NoError(t, err)
		}

		// Adding a 21st distinct handler fails.
		_, err := AddCustomOAuth2(ctx, AddOptions{
			ConfigPath: configPath,
			Entry: config.CustomOAuth2Config{
				Name:        "idp-overflow",
				DefaultFlow: "pat",
				Token:       "env://API_TOKEN",
			},
		})
		require.ErrorIs(t, err, ErrMaxHandlersExceeded)

		// Updating an existing handler at capacity with Force succeeds.
		res, err := AddCustomOAuth2(ctx, AddOptions{
			ConfigPath: configPath,
			Force:      true,
			Entry: config.CustomOAuth2Config{
				Name:        "idp-00",
				DisplayName: "Updated At Capacity",
				DefaultFlow: "pat",
				Token:       "env://API_TOKEN",
			},
		})
		require.NoError(t, err)
		assert.True(t, res.Updated)
	})

	t.Run("rolls back newly stored or pre-existing stdin secret if config Save fails", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		blockerParent := filepath.Join(tmpDir, "blocked-parent")
		configPath := filepath.Join(blockerParent, "config.yaml")
		store := secrets.NewMockStore()
		ctx := secrets.WithStore(context.Background(), store)

		// Create a regular 0600 file at blockerParent during stdin secret reading
		// (after mgr.Load() has completed) so mgr.Save() fails deterministically
		// with ENOTDIR/EEXIST when creating the parent directory for configPath.
		readerWithSaveBlocker := func(secret string) io.Reader {
			return &onReadHookReader{
				r: strings.NewReader(secret),
				onRead: func() {
					_ = os.WriteFile(blockerParent, []byte("not-a-directory"), 0o600)
				},
			}
		}

		_, err := AddCustomOAuth2(ctx, AddOptions{
			ConfigPath: configPath,
			Entry: config.CustomOAuth2Config{
				Name:        "quay",
				TokenURL:    "https://quay.io/oauth/token",
				ClientID:    "cid",
				DefaultFlow: "client_credentials",
			},
			ReadClientSecretStdin: true,
			ClientSecretReader:    readerWithSaveBlocker("ephemeral-secret"),
		})
		require.Error(t, err)

		exists, existsErr := store.Exists(ctx, "quay-client-secret")
		require.NoError(t, existsErr)
		assert.False(t, exists, "failed config save must roll back newly written secret")

		require.NoError(t, os.Remove(blockerParent))

		// Seed a pre-existing secret and verify failed Save restores the original value.
		require.NoError(t, store.Set(ctx, "quay-client-secret", []byte("original-secret")))
		_, err = AddCustomOAuth2(ctx, AddOptions{
			ConfigPath: configPath,
			Entry: config.CustomOAuth2Config{
				Name:        "quay",
				TokenURL:    "https://quay.io/oauth/token",
				ClientID:    "cid",
				DefaultFlow: "client_credentials",
			},
			ReadClientSecretStdin: true,
			ClientSecretReader:    readerWithSaveBlocker("replacement-secret"),
		})
		require.Error(t, err)

		restored, getErr := store.Get(ctx, "quay-client-secret")
		require.NoError(t, getErr)
		assert.Equal(t, "original-secret", string(restored), "failed config save must restore previous secret")
	})

	t.Run("enforces 0600 file permissions and atomic replacement on config.yaml", func(t *testing.T) {
		t.Parallel()
		if runtime.GOOS == "windows" {
			t.Skip("POSIX file permissions and inode replacement only")
		}

		tmpDir := t.TempDir()
		newCfgPath := filepath.Join(tmpDir, "created.yaml")
		_, err := AddCustomOAuth2(context.Background(), AddOptions{
			ConfigPath: newCfgPath,
			Entry: config.CustomOAuth2Config{
				Name:        "quay",
				DefaultFlow: "pat",
				Token:       "env://QUAY_TOKEN",
			},
		})
		require.NoError(t, err)
		infoBefore, statErr := os.Stat(newCfgPath)
		require.NoError(t, statErr)
		assert.Equal(t, os.FileMode(0o600), infoBefore.Mode().Perm())

		// Hold an open file descriptor to the initial file and verify that
		// overwriting replaces the file atomically via a new 0600 inode without
		// exposing the new contents to existing open file descriptors.
		openReader, openErr := os.Open(newCfgPath)
		require.NoError(t, openErr)
		defer func() { _ = openReader.Close() }()

		_, err = AddCustomOAuth2(context.Background(), AddOptions{
			ConfigPath: newCfgPath,
			Force:      true,
			Entry: config.CustomOAuth2Config{
				Name:        "quay",
				DisplayName: "Quay Tightened",
				DefaultFlow: "pat",
				Token:       "env://QUAY_NEW_SECRET_TOKEN",
			},
		})
		require.NoError(t, err)
		infoAfter, statErr := os.Stat(newCfgPath)
		require.NoError(t, statErr)
		assert.Equal(t, os.FileMode(0o600), infoAfter.Mode().Perm())
		assert.False(t, os.SameFile(infoBefore, infoAfter), "save must atomically replace config.yaml with a new inode")

		oldBytes, readErr := io.ReadAll(openReader)
		require.NoError(t, readErr)
		assert.NotContains(t, string(oldBytes), "Quay Tightened", "pre-existing open file descriptor must not observe newly written config contents")
		assert.NotContains(t, string(oldBytes), "QUAY_NEW_SECRET_TOKEN")
	})

	t.Run("config.d and WithBaseConfig preserve layering on Add and guard Remove", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.yaml")
		dropInDir := filepath.Join(tmpDir, "config.d")
		require.NoError(t, os.MkdirAll(dropInDir, 0o700))

		dropInYAML := `auth:
  customOAuth2:
    - name: corp-idp
      displayName: Corp DropIn
      defaultFlow: pat
      token: env://CORP_TOKEN
`
		require.NoError(t, os.WriteFile(filepath.Join(dropInDir, "10-auth.yaml"), []byte(dropInYAML), 0o600))
		ctx := context.Background()

		// Removing a config.d-defined handler directly is rejected with ErrImmutableHandler.
		err := RemoveCustomOAuth2(ctx, configPath, "corp-idp")
		require.ErrorIs(t, err, ErrImmutableHandler)

		// Adding a user handler keeps corp-idp active WITHOUT baking corp-idp into config.yaml.
		_, err = AddCustomOAuth2(ctx, AddOptions{
			ConfigPath: configPath,
			Entry: config.CustomOAuth2Config{
				Name:        "user-quay",
				DisplayName: "User Quay",
				DefaultFlow: "pat",
				Token:       "env://USER_QUAY_TOKEN",
			},
		})
		require.NoError(t, err)

		userRaw, readErr := os.ReadFile(configPath)
		require.NoError(t, readErr)
		assert.Contains(t, string(userRaw), "user-quay")
		assert.NotContains(t, string(userRaw), "corp-idp", "config.d handler must not be baked into config.yaml")
		assert.NotContains(t, string(userRaw), "CORP_TOKEN")

		listed, listErr := ListCustomOAuth2(ctx, configPath)
		require.NoError(t, listErr)
		require.Len(t, listed, 2)
		assert.Equal(t, "corp-idp", listed[0].Name)
		assert.Equal(t, "user-quay", listed[1].Name)

		// Overriding corp-idp with --force writes user override; removing it restores config.d.
		_, err = AddCustomOAuth2(ctx, AddOptions{
			ConfigPath: configPath,
			Force:      true,
			Entry: config.CustomOAuth2Config{
				Name:        "corp-idp",
				DisplayName: "Corp User Override",
				DefaultFlow: "pat",
				Token:       "env://CORP_OVERRIDE_TOKEN",
			},
		})
		require.NoError(t, err)

		rmRes, rmErr := RemoveCustomOAuth2WithResult(ctx, configPath, "corp-idp")
		require.NoError(t, rmErr)
		assert.True(t, rmRes.RestoredLowerLayer)

		listedAfterRestore, listErr := ListCustomOAuth2(ctx, configPath)
		require.NoError(t, listErr)
		require.Len(t, listedAfterRestore, 2)
		assert.Equal(t, "Corp DropIn", listedAfterRestore[0].DisplayName)

		// Remove user-quay so corp-idp is once again the sole handler (from config.d).
		require.NoError(t, RemoveCustomOAuth2(ctx, configPath, "user-quay"))

		// Force-adding a handler identical to the sole config.d handler must
		// persist the user override in config.yaml (not discard it during drop-in
		// pruning), shielding it from subsequent config.d edits and allowing
		// RemoveCustomOAuth2WithResult to remove the override later.
		_, err = AddCustomOAuth2(ctx, AddOptions{
			ConfigPath: configPath,
			Force:      true,
			Entry: config.CustomOAuth2Config{
				Name:        "corp-idp",
				DisplayName: "Corp DropIn",
				DefaultFlow: "pat",
				Token:       "env://CORP_TOKEN",
			},
		})
		require.NoError(t, err)

		// Modify config.d fragment; user override in config.yaml must take precedence.
		updatedDropInYAML := `auth:
  customOAuth2:
    - name: corp-idp
      displayName: Corp DropIn Changed
      defaultFlow: pat
      token: env://CORP_TOKEN
`
		require.NoError(t, os.WriteFile(filepath.Join(dropInDir, "10-auth.yaml"), []byte(updatedDropInYAML), 0o600))

		listedPinned, listErr := ListCustomOAuth2(ctx, configPath)
		require.NoError(t, listErr)
		require.Len(t, listedPinned, 1)
		assert.Equal(t, "Corp DropIn", listedPinned[0].DisplayName, "force-added identical user override must survive pruning and shield against config.d changes")

		rmIdentical, rmErr := RemoveCustomOAuth2WithResult(ctx, configPath, "corp-idp")
		require.NoError(t, rmErr)
		assert.True(t, rmIdentical.RestoredLowerLayer)
	})

	t.Run("validates DeviceCodePollInterval bounds and endpoint URLs", func(t *testing.T) {
		t.Parallel()
		configPath := filepath.Join(t.TempDir(), "config.yaml")

		_, err := AddCustomOAuth2(context.Background(), AddOptions{
			ConfigPath: configPath,
			Entry: config.CustomOAuth2Config{
				Name:                   "bad-poll",
				DeviceAuthURL:          "https://idp.example.com/device",
				TokenURL:               "https://idp.example.com/token",
				ClientID:               "cid",
				DefaultFlow:            "device_code",
				DeviceCodePollInterval: 99,
			},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "deviceCodePollInterval")

		_, err = AddCustomOAuth2(context.Background(), AddOptions{
			ConfigPath: configPath,
			Entry: config.CustomOAuth2Config{
				Name:         "bad-url",
				TokenURL:     "not-a-valid-url",
				ClientID:     "cid",
				DefaultFlow:  "client_credentials",
				ClientSecret: "env://SECRET",
			},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "tokenURL must be a valid http:// or https:// URL")
	})

	t.Run("rejects empty or oversized stdin secret", func(t *testing.T) {
		t.Parallel()
		configPath := filepath.Join(t.TempDir(), "config.yaml")
		ctx := secrets.WithStore(context.Background(), secrets.NewMockStore())

		_, err := AddCustomOAuth2(ctx, AddOptions{
			ConfigPath: configPath,
			Entry: config.CustomOAuth2Config{
				Name:        "quay",
				TokenURL:    "https://quay.io/oauth/token",
				ClientID:    "cid",
				DefaultFlow: "client_credentials",
			},
			ReadClientSecretStdin: true,
			ClientSecretReader:    strings.NewReader("  \r\n"),
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "empty")

		oversized := strings.Repeat("x", MaxStdinSecretBytes+1)
		_, err = AddCustomOAuth2(ctx, AddOptions{
			ConfigPath: configPath,
			Entry: config.CustomOAuth2Config{
				Name:        "quay",
				TokenURL:    "https://quay.io/oauth/token",
				ClientID:    "cid",
				DefaultFlow: "client_credentials",
			},
			ReadClientSecretStdin: true,
			ClientSecretReader:    strings.NewReader(oversized),
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "exceeds maximum size")
	})

	t.Run("embedder WithBaseConfig and custom binary name", func(t *testing.T) {
		t.Parallel()
		configPath := filepath.Join(t.TempDir(), "config.yaml")
		require.NoError(t, os.WriteFile(configPath, []byte(""), 0o600))

		baseYAML := []byte(`
auth:
  customOAuth2:
    - name: embedder-idp
      displayName: Embedder IDP
      defaultFlow: pat
      token: env://EMBEDDER_TOKEN
`)
		ctx := settings.IntoContext(context.Background(), &settings.Run{BinaryName: "mycli"})
		ctx = config.WithManagerOptions(ctx, []config.ManagerOption{
			config.WithBaseConfig(baseYAML),
		})

		items, err := ListCustomOAuth2(ctx, configPath)
		require.NoError(t, err)
		require.Len(t, items, 1)
		assert.Equal(t, "embedder-idp", items[0].Name)
		assert.Equal(t, config.RedactedValue, items[0].Token)

		// Reserved secret prefix for custom binary name "mycli." is rejected.
		_, err = AddCustomOAuth2(ctx, AddOptions{
			ConfigPath: configPath,
			BinaryName: "mycli",
			Entry: config.CustomOAuth2Config{
				Name:        "bad-ref",
				DefaultFlow: "pat",
				Token:       "secret://mycli.internal-token",
			},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "mycli.")
	})
}

func BenchmarkListCustomOAuth2(b *testing.B) {
	configPath := filepath.Join(b.TempDir(), "config.yaml")
	ctx := context.Background()
	_, err := AddCustomOAuth2(ctx, AddOptions{
		ConfigPath: configPath,
		Entry: config.CustomOAuth2Config{
			Name:         "quay",
			DisplayName:  "Quay.io",
			AuthorizeURL: "https://quay.io/oauth/authorize",
			ClientID:     "quay-client",
			DefaultFlow:  "interactive",
			ResponseType: "token",
			Registry:     "quay.io",
		},
	})
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		items, listErr := ListCustomOAuth2(ctx, configPath)
		if listErr != nil || len(items) != 1 {
			b.Fatalf("unexpected result: len=%d err=%v", len(items), listErr)
		}
	}
}

func TestConfigPersistenceError_Unwrap(t *testing.T) {
	t.Parallel()
	inner := errors.New("disk full")
	wrapped := &ConfigPersistenceError{Err: inner}
	assert.Equal(t, "disk full", wrapped.Error())
	assert.ErrorIs(t, wrapped, inner)
}

func TestCommittedExampleCustomOAuth2HandlerYAML(t *testing.T) {
	t.Parallel()
	examplePath := filepath.Join("..", "..", "..", "examples", "auth", "custom-oauth2-handler.yaml")
	cfg, err := LoadConfigFile(examplePath)
	require.NoError(t, err)
	assert.Equal(t, "custom-ar", cfg.Name)
	require.NoError(t, ValidateConfig(cfg))
}
