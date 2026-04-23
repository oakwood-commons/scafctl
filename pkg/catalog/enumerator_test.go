// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	orasauth "oras.land/oras-go/v2/registry/remote/auth"
)

func TestSelectEnumerator(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		authHandlerName string
		registry        string
		repository      string
		wantType        string
	}{
		{
			name:            "gcp auth handler selects GCP enumerator",
			authHandlerName: "gcp",
			registry:        "us-central1-docker.pkg.dev",
			repository:      "my-project/my-repo",
			wantType:        "*catalog.gcpEnumerator",
		},
		{
			name:            "ford-quay auth handler selects Quay enumerator",
			authHandlerName: "ford-quay",
			registry:        "fcr.ford.com",
			repository:      "ford-solutions",
			wantType:        "*catalog.quayEnumerator",
		},
		{
			name:            "GCP hostname detection without auth handler",
			authHandlerName: "",
			registry:        "us-east4-docker.pkg.dev",
			repository:      "my-project/my-repo",
			wantType:        "*catalog.gcpEnumerator",
		},
		{
			name:            "quay hostname detection without auth handler",
			authHandlerName: "",
			registry:        "quay.io",
			repository:      "myorg",
			wantType:        "*catalog.quayEnumerator",
		},
		{
			name:            "ghcr.io hostname selects GHCR enumerator",
			authHandlerName: "",
			registry:        "ghcr.io",
			repository:      "myorg",
			wantType:        "*catalog.ghcrEnumerator",
		},
		{
			name:            "github auth handler selects GHCR enumerator",
			authHandlerName: "github",
			registry:        "ghcr.io",
			repository:      "myorg",
			wantType:        "*catalog.ghcrEnumerator",
		},
		{
			name:            "unknown registry falls back to OCI",
			authHandlerName: "",
			registry:        "registry.example.com",
			repository:      "myorg/scafctl",
			wantType:        "*catalog.ociCatalogEnumerator",
		},
		{
			name:            "gcp auth handler with non-GCP host falls back to OCI",
			authHandlerName: "gcp",
			registry:        "registry.example.com",
			repository:      "myorg",
			wantType:        "*catalog.ociCatalogEnumerator",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var handler auth.Handler
			if tc.authHandlerName != "" {
				mock := auth.NewMockHandler(tc.authHandlerName)
				mock.GetTokenResult = &auth.Token{AccessToken: "test-token"}
				handler = mock
			}

			cfg := enumeratorConfig{
				authHandlerName: tc.authHandlerName,
				authHandler:     handler,
				registry:        tc.registry,
				repository:      tc.repository,
				client:          &orasauth.Client{},
				logger:          logr.Discard(),
			}

			e := selectEnumerator(cfg)
			assert.Equal(t, tc.wantType, typeString(e))
		})
	}
}

func TestOCICatalogEnumerator_Enumerate(t *testing.T) {
	t.Parallel()

	t.Run("404 returns ErrEnumerationNotSupported", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		host := server.Listener.Addr().String()
		e := &ociCatalogEnumerator{
			registry: host,
			client: &orasauth.Client{
				Client: server.Client(),
			},
			logger: logr.Discard(),
		}

		_, err := e.enumerate(t.Context())
		require.Error(t, err)
		assert.True(t, IsEnumerationNotSupported(err))
	})

	t.Run("context cancellation returns ErrEnumerationNotSupported", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(t.Context())
		cancel() // cancel immediately

		e := &ociCatalogEnumerator{
			registry: "localhost:1",
			client:   &orasauth.Client{},
			logger:   logr.Discard(),
		}

		_, err := e.enumerate(ctx)
		require.Error(t, err)
		assert.True(t, IsEnumerationNotSupported(err))
	})

	t.Run("401 with credentials returns ErrEnumerationNotSupported", func(t *testing.T) {
		t.Parallel()

		// Server always returns 401 -- both ORAS and direct will fail.
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]any{
				"errors": []map[string]string{
					{"code": "UNAUTHORIZED", "message": "access denied"},
				},
			})
		}))
		defer server.Close()

		host := server.Listener.Addr().String()
		e := &ociCatalogEnumerator{
			registry: host,
			client: &orasauth.Client{
				Credential: func(_ context.Context, _ string) (orasauth.Credential, error) {
					return orasauth.Credential{AccessToken: "tok"}, nil
				},
				Client: server.Client(),
			},
			logger: logr.Discard(),
		}

		_, err := e.enumerate(t.Context())
		require.Error(t, err)
		assert.True(t, IsEnumerationNotSupported(err))
	})

	t.Run("401 fallback to direct success", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			// Direct HTTP fallback sends Authorization header; ORAS does not on first attempt.
			if r.Header.Get("Authorization") != "" {
				json.NewEncoder(w).Encode(map[string]any{
					"repositories": []string{"repo1"},
				})
				return
			}
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]any{
				"errors": []map[string]string{
					{"code": "UNAUTHORIZED", "message": "access denied"},
				},
			})
		}))
		defer server.Close()

		host := server.Listener.Addr().String()
		e := &ociCatalogEnumerator{
			registry: host,
			client: &orasauth.Client{
				Credential: func(_ context.Context, _ string) (orasauth.Credential, error) {
					return orasauth.Credential{AccessToken: "tok"}, nil
				},
				Client: server.Client(),
			},
			logger: logr.Discard(),
		}

		repos, err := e.enumerate(t.Context())
		require.NoError(t, err)
		assert.Equal(t, []string{"repo1"}, repos)
	})
}

func TestOCICatalogEnumerator_EnumerateDirect(t *testing.T) {
	t.Parallel()

	t.Run("success with repos", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/v2/_catalog", r.URL.Path)
			assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"repositories": []string{
					"myorg/solutions/starter-kit",
					"myorg/providers/terraform",
				},
			})
		}))
		defer server.Close()

		host := server.Listener.Addr().String()
		e := &ociCatalogEnumerator{
			registry: host,
			client: &orasauth.Client{
				Credential: func(_ context.Context, _ string) (orasauth.Credential, error) {
					return orasauth.Credential{AccessToken: "test-token"}, nil
				},
				Client: server.Client(),
			},
			insecure: true,
			logger:   logr.Discard(),
		}

		repos, err := e.enumerateDirect(t.Context())
		require.NoError(t, err)
		assert.Equal(t, []string{
			"myorg/solutions/starter-kit",
			"myorg/providers/terraform",
		}, repos)
	})

	t.Run("pagination via Link header", func(t *testing.T) {
		t.Parallel()

		callCount := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callCount++
			w.Header().Set("Content-Type", "application/json")

			if callCount == 1 {
				w.Header().Set("Link", `</v2/_catalog?last=page1>; rel="next"`)
				json.NewEncoder(w).Encode(map[string]any{
					"repositories": []string{"repo1"},
				})
			} else {
				json.NewEncoder(w).Encode(map[string]any{
					"repositories": []string{"repo2"},
				})
			}
		}))
		defer server.Close()

		host := server.Listener.Addr().String()
		e := &ociCatalogEnumerator{
			registry: host,
			client: &orasauth.Client{
				Client: server.Client(),
			},
			insecure: true,
			logger:   logr.Discard(),
		}

		repos, err := e.enumerateDirect(t.Context())
		require.NoError(t, err)
		assert.Equal(t, []string{"repo1", "repo2"}, repos)
		assert.Equal(t, 2, callCount)
	})

	t.Run("non-200 returns error", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		}))
		defer server.Close()

		host := server.Listener.Addr().String()
		e := &ociCatalogEnumerator{
			registry: host,
			client:   &orasauth.Client{Client: server.Client()},
			insecure: true,
			logger:   logr.Discard(),
		}

		_, err := e.enumerateDirect(t.Context())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "status 403")
	})

	t.Run("basic auth fallback", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, pass, ok := r.BasicAuth()
			assert.True(t, ok)
			assert.Equal(t, "myuser", user)
			assert.Equal(t, "mypass", pass)

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"repositories": []string{"repo1"},
			})
		}))
		defer server.Close()

		host := server.Listener.Addr().String()
		e := &ociCatalogEnumerator{
			registry: host,
			client: &orasauth.Client{
				Credential: func(_ context.Context, _ string) (orasauth.Credential, error) {
					return orasauth.Credential{Username: "myuser", Password: "mypass"}, nil
				},
				Client: server.Client(),
			},
			insecure: true,
			logger:   logr.Discard(),
		}

		repos, err := e.enumerateDirect(t.Context())
		require.NoError(t, err)
		assert.Equal(t, []string{"repo1"}, repos)
	})
}

func TestOCICatalogEnumerator_HasCredentials(t *testing.T) {
	t.Parallel()

	t.Run("no credential func", func(t *testing.T) {
		t.Parallel()
		e := &ociCatalogEnumerator{
			client: &orasauth.Client{},
			logger: logr.Discard(),
		}
		assert.False(t, e.hasCredentials(t.Context()))
	})

	t.Run("has access token", func(t *testing.T) {
		t.Parallel()
		e := &ociCatalogEnumerator{
			client: &orasauth.Client{
				Credential: func(_ context.Context, _ string) (orasauth.Credential, error) {
					return orasauth.Credential{AccessToken: "tok"}, nil
				},
			},
			logger: logr.Discard(),
		}
		assert.True(t, e.hasCredentials(t.Context()))
	})

	t.Run("has username+password", func(t *testing.T) {
		t.Parallel()
		e := &ociCatalogEnumerator{
			client: &orasauth.Client{
				Credential: func(_ context.Context, _ string) (orasauth.Credential, error) {
					return orasauth.Credential{Username: "u", Password: "p"}, nil
				},
			},
			logger: logr.Discard(),
		}
		assert.True(t, e.hasCredentials(t.Context()))
	})

	t.Run("empty credentials", func(t *testing.T) {
		t.Parallel()
		e := &ociCatalogEnumerator{
			client: &orasauth.Client{
				Credential: func(_ context.Context, _ string) (orasauth.Credential, error) {
					return orasauth.EmptyCredential, nil
				},
			},
			logger: logr.Discard(),
		}
		assert.False(t, e.hasCredentials(t.Context()))
	})
}

func TestQuayEnumerator(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/api/v1/repository", r.URL.Path)
			assert.Equal(t, "ford-solutions", r.URL.Query().Get("namespace"))
			assert.Equal(t, "Bearer my-quay-token", r.Header.Get("Authorization"))

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"repositories": []any{
					map[string]string{"namespace": "ford-solutions", "name": "solutions/starter-kit"},
					map[string]string{"namespace": "ford-solutions", "name": "providers/terraform"},
				},
			})
		}))
		defer server.Close()

		host := server.Listener.Addr().String()
		e := &quayEnumerator{
			registry:  host,
			namespace: "ford-solutions",
			client: &orasauth.Client{
				Credential: func(_ context.Context, _ string) (orasauth.Credential, error) {
					return orasauth.Credential{Password: "my-quay-token"}, nil
				},
				Client: server.Client(),
			},
			insecure: true,
			logger:   logr.Discard(),
		}

		repos, err := e.enumerate(t.Context())
		require.NoError(t, err)
		assert.Equal(t, []string{
			"ford-solutions/solutions/starter-kit",
			"ford-solutions/providers/terraform",
		}, repos)
	})

	t.Run("no namespace returns error", func(t *testing.T) {
		t.Parallel()

		e := &quayEnumerator{
			registry: "fcr.ford.com",
			client:   &orasauth.Client{},
			logger:   logr.Discard(),
		}

		_, err := e.enumerate(t.Context())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no repository namespace")
		assert.True(t, IsEnumerationNotSupported(err))
	})

	t.Run("non-200 returns error", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		}))
		defer server.Close()

		host := server.Listener.Addr().String()
		e := &quayEnumerator{
			registry:  host,
			namespace: "ford-solutions",
			client:    &orasauth.Client{Client: server.Client()},
			insecure:  true,
			logger:    logr.Discard(),
		}

		_, err := e.enumerate(t.Context())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "status 403")
	})
}

func TestGCPEnumerator(t *testing.T) {
	t.Parallel()

	t.Run("success with pagination", func(t *testing.T) {
		t.Parallel()

		callCount := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "Bearer gcp-access-token", r.Header.Get("Authorization"))
			callCount++

			w.Header().Set("Content-Type", "application/json")

			if callCount == 1 {
				assert.Empty(t, r.URL.Query().Get("pageToken"))
				json.NewEncoder(w).Encode(map[string]any{
					"packages": []map[string]string{
						{"name": "projects/my-project/locations/us-central1/repositories/my-repo/packages/solutions%2Fstarter-kit"},
					},
					"nextPageToken": "page2",
				})
			} else {
				assert.Equal(t, "page2", r.URL.Query().Get("pageToken"))
				json.NewEncoder(w).Encode(map[string]any{
					"packages": []map[string]string{
						{"name": "projects/my-project/locations/us-central1/repositories/my-repo/packages/providers%2Fterraform"},
					},
				})
			}
		}))
		defer server.Close()

		mockHandler := auth.NewMockHandler("gcp")
		mockHandler.GetTokenResult = &auth.Token{AccessToken: "gcp-access-token"}

		e := &gcpEnumerator{
			project:     "my-project",
			location:    "us-central1",
			gcpRepo:     "my-repo",
			repository:  "my-project/my-repo",
			authHandler: mockHandler,
			authScope:   "https://www.googleapis.com/auth/cloud-platform",
			httpClient:  server.Client(),
			apiBaseURL:  server.URL,
			logger:      logr.Discard(),
		}

		repos, err := e.enumerate(t.Context())
		require.NoError(t, err)
		assert.Equal(t, []string{
			"my-project/my-repo/solutions/starter-kit",
			"my-project/my-repo/providers/terraform",
		}, repos)
		assert.Equal(t, 2, callCount)
	})

	t.Run("non-200 returns error", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte("access denied"))
		}))
		defer server.Close()

		mockHandler := auth.NewMockHandler("gcp")
		mockHandler.GetTokenResult = &auth.Token{AccessToken: "tok"}

		e := &gcpEnumerator{
			project:     "my-project",
			location:    "us-central1",
			gcpRepo:     "my-repo",
			repository:  "my-project/my-repo",
			authHandler: mockHandler,
			httpClient:  server.Client(),
			apiBaseURL:  server.URL,
			logger:      logr.Discard(),
		}

		_, err := e.enumerate(t.Context())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "status 403")
	})

	t.Run("no auth handler returns error", func(t *testing.T) {
		t.Parallel()

		e := &gcpEnumerator{
			project:    "my-project",
			location:   "us-central1",
			gcpRepo:    "my-repo",
			httpClient: http.DefaultClient,
			apiBaseURL: gcpDefaultAPIBase,
			logger:     logr.Discard(),
		}

		_, err := e.enumerate(t.Context())
		require.Error(t, err)
		assert.True(t, IsEnumerationNotSupported(err))
	})

	t.Run("auth handler error propagates", func(t *testing.T) {
		t.Parallel()

		mockHandler := auth.NewMockHandler("gcp")
		mockHandler.GetTokenErr = assert.AnError

		e := &gcpEnumerator{
			project:     "my-project",
			location:    "us-central1",
			gcpRepo:     "my-repo",
			authHandler: mockHandler,
			httpClient:  http.DefaultClient,
			apiBaseURL:  gcpDefaultAPIBase,
			logger:      logr.Discard(),
		}

		_, err := e.enumerate(t.Context())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "getting GCP token")
	})
}

func TestParseGCPRegistryURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		registry   string
		repository string
		wantLoc    string
		wantProj   string
		wantRepo   string
		wantErr    bool
	}{
		{
			name:       "standard GCP AR",
			registry:   "us-central1-docker.pkg.dev",
			repository: "ford-6c2cb87c6f2cc16da4f2260c/cldctl-oci",
			wantLoc:    "us-central1",
			wantProj:   "ford-6c2cb87c6f2cc16da4f2260c",
			wantRepo:   "cldctl-oci",
		},
		{
			name:       "multi-region",
			registry:   "europe-west1-docker.pkg.dev",
			repository: "my-project/my-repo/extra/path",
			wantLoc:    "europe-west1",
			wantProj:   "my-project",
			wantRepo:   "my-repo",
		},
		{
			name:       "not a GCP host",
			registry:   "ghcr.io",
			repository: "myorg/scafctl",
			wantErr:    true,
		},
		{
			name:       "too few path segments",
			registry:   "us-central1-docker.pkg.dev",
			repository: "only-project",
			wantErr:    true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			loc, proj, repo, err := parseGCPRegistryURL(tc.registry, tc.repository)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantLoc, loc)
			assert.Equal(t, tc.wantProj, proj)
			assert.Equal(t, tc.wantRepo, repo)
		})
	}
}

func TestParseLinkHeader_Enumerator(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		header  string
		baseURL string
		want    string
	}{
		{
			name: "empty header",
			want: "",
		},
		{
			name:    "absolute URL",
			header:  `<https://registry.example.com/v2/_catalog?last=repo3>; rel="next"`,
			baseURL: "https://registry.example.com/v2/_catalog",
			want:    "https://registry.example.com/v2/_catalog?last=repo3",
		},
		{
			name:    "relative URL",
			header:  `</v2/_catalog?last=repo3>; rel="next"`,
			baseURL: "https://registry.example.com/v2/_catalog",
			want:    "https://registry.example.com/v2/_catalog?last=repo3",
		},
		{
			name:    "no next rel",
			header:  `<https://example.com>; rel="prev"`,
			baseURL: "https://example.com",
			want:    "",
		},
		{
			name:    "multiple links picks next",
			header:  `<https://example.com/prev>; rel="prev", <https://example.com/next>; rel="next"`,
			baseURL: "https://example.com",
			want:    "https://example.com/next",
		},
		{
			name:    "malformed brackets",
			header:  `no brackets; rel="next"`,
			baseURL: "https://example.com",
			want:    "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := parseLinkHeader(tc.header, tc.baseURL)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestGCPEnumerator_FetchPage(t *testing.T) {
	t.Parallel()

	t.Run("parses packages correctly", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
			assert.Contains(t, r.URL.Path, "/v1/projects/p/locations/l/repositories/r/packages")

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"packages": []map[string]string{
					{"name": "projects/p/locations/l/repositories/r/packages/solutions%2Fmy-app"},
					{"name": "projects/p/locations/l/repositories/r/packages/providers%2Fterraform"},
					{"name": "projects/p/locations/l/repositories/r/packages/auth-handlers%2Fgcp"},
				},
			})
		}))
		defer server.Close()

		e := &gcpEnumerator{
			project:    "p",
			location:   "l",
			gcpRepo:    "r",
			repository: "p/r",
			httpClient: server.Client(),
			apiBaseURL: server.URL,
			logger:     logr.Discard(),
		}

		repos, nextToken, err := e.fetchPage(t.Context(), "test-token", "")
		require.NoError(t, err)
		assert.Empty(t, nextToken)
		assert.Equal(t, []string{
			"p/r/solutions/my-app",
			"p/r/providers/terraform",
			"p/r/auth-handlers/gcp",
		}, repos)
	})

	t.Run("skips unexpected format", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"packages": []map[string]string{
					{"name": "projects/p/locations/l/repositories/r/packages/solutions%2Fmy-app"},
					{"name": "unexpected/format/package"},
				},
			})
		}))
		defer server.Close()

		e := &gcpEnumerator{
			project:    "p",
			location:   "l",
			gcpRepo:    "r",
			repository: "p/r",
			httpClient: server.Client(),
			apiBaseURL: server.URL,
			logger:     logr.Discard(),
		}

		repos, _, err := e.fetchPage(t.Context(), "test-token", "")
		require.NoError(t, err)
		assert.Equal(t, []string{"p/r/solutions/my-app"}, repos)
	})
}

// typeString returns a string representation of the type for test assertions.
func typeString(v any) string {
	if v == nil {
		return "<nil>"
	}
	return "*catalog." + typeName(v)
}

func typeName(v any) string {
	switch v.(type) {
	case *ociCatalogEnumerator:
		return "ociCatalogEnumerator"
	case *quayEnumerator:
		return "quayEnumerator"
	case *gcpEnumerator:
		return "gcpEnumerator"
	case *ghcrEnumerator:
		return "ghcrEnumerator"
	default:
		return "unknown"
	}
}

func BenchmarkSelectEnumerator(b *testing.B) {
	cfg := enumeratorConfig{
		authHandlerName: "gcp",
		authHandler:     auth.NewMockHandler("gcp"),
		registry:        "us-central1-docker.pkg.dev",
		repository:      "my-project/my-repo",
		client:          &orasauth.Client{},
		logger:          logr.Discard(),
	}

	b.ResetTimer()
	for b.Loop() {
		selectEnumerator(cfg)
	}
}

func BenchmarkParseGCPRegistryURL(b *testing.B) {
	b.ResetTimer()
	for b.Loop() {
		parseGCPRegistryURL("us-central1-docker.pkg.dev", "my-project/my-repo") //nolint:errcheck // benchmark
	}
}

func TestGHCREnumerator(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/orgs/oakwood-commons/packages", r.URL.Path)
			assert.Equal(t, "container", r.URL.Query().Get("package_type"))
			assert.Equal(t, "application/vnd.github+json", r.Header.Get("Accept"))

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]map[string]string{
				{"name": "solutions/starter-kit"},
				{"name": "providers/terraform"},
			})
		}))
		defer server.Close()

		e := &ghcrEnumerator{
			org:        "oakwood-commons",
			repository: "oakwood-commons",
			client:     &orasauth.Client{Client: server.Client()},
			httpClient: server.Client(),
			apiBaseURL: server.URL,
			logger:     logr.Discard(),
		}

		repos, err := e.enumerate(t.Context())
		require.NoError(t, err)
		assert.Equal(t, []string{
			"oakwood-commons/solutions/starter-kit",
			"oakwood-commons/providers/terraform",
		}, repos)
	})

	t.Run("with pagination", func(t *testing.T) {
		t.Parallel()

		callCount := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callCount++
			w.Header().Set("Content-Type", "application/json")

			if callCount == 1 {
				// First page: include Link header for next page
				nextURL := "http://" + r.Host + "/orgs/myorg/packages?package_type=container&per_page=100&page=2"
				w.Header().Set("Link", `<`+nextURL+`>; rel="next"`)
				json.NewEncoder(w).Encode([]map[string]string{
					{"name": "solutions/app-a"},
				})
				return
			}

			// Second page: no Link header (last page)
			json.NewEncoder(w).Encode([]map[string]string{
				{"name": "solutions/app-b"},
			})
		}))
		defer server.Close()

		e := &ghcrEnumerator{
			org:        "myorg",
			repository: "myorg",
			client:     &orasauth.Client{Client: server.Client()},
			httpClient: server.Client(),
			apiBaseURL: server.URL,
			logger:     logr.Discard(),
		}

		repos, err := e.enumerate(t.Context())
		require.NoError(t, err)
		assert.Equal(t, []string{
			"myorg/solutions/app-a",
			"myorg/solutions/app-b",
		}, repos)
		assert.Equal(t, 2, callCount)
	})

	t.Run("with credentials after 401", func(t *testing.T) {
		t.Parallel()

		callCount := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callCount++
			if callCount == 1 {
				// First attempt (anonymous) returns 401
				assert.Empty(t, r.Header.Get("Authorization"))
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			// Retry with credentials
			assert.Equal(t, "Bearer ghp_test_token", r.Header.Get("Authorization"))

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]map[string]string{
				{"name": "solutions/private-app"},
			})
		}))
		defer server.Close()

		e := &ghcrEnumerator{
			org:        "myorg",
			repository: "myorg",
			client: &orasauth.Client{
				Credential: func(_ context.Context, _ string) (orasauth.Credential, error) {
					return orasauth.Credential{Password: "ghp_test_token"}, nil
				},
				Client: server.Client(),
			},
			httpClient: server.Client(),
			apiBaseURL: server.URL,
			logger:     logr.Discard(),
		}

		repos, err := e.enumerate(t.Context())
		require.NoError(t, err)
		assert.Equal(t, []string{"myorg/solutions/private-app"}, repos)
	})

	t.Run("non-200 returns enumeration not supported", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		}))
		defer server.Close()

		e := &ghcrEnumerator{
			org:        "myorg",
			repository: "myorg",
			client:     &orasauth.Client{Client: server.Client()},
			httpClient: server.Client(),
			apiBaseURL: server.URL,
			logger:     logr.Discard(),
		}

		_, err := e.enumerate(t.Context())
		require.Error(t, err)
		assert.True(t, IsEnumerationNotSupported(err))
	})

	t.Run("empty org returns error", func(t *testing.T) {
		t.Parallel()

		_, err := newGHCREnumerator(enumeratorConfig{
			registry:   "ghcr.io",
			repository: "",
			client:     &orasauth.Client{},
			logger:     logr.Discard(),
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "requires a repository")
	})

	t.Run("nested org path extracts first segment", func(t *testing.T) {
		t.Parallel()

		e, err := newGHCREnumerator(enumeratorConfig{
			registry:   "ghcr.io",
			repository: "oakwood-commons/scafctl",
			client:     &orasauth.Client{},
			logger:     logr.Discard(),
		})
		require.NoError(t, err)
		assert.Equal(t, "oakwood-commons", e.org)
		assert.Equal(t, "oakwood-commons/scafctl", e.repository)
	})

	t.Run("auth_handler_fallback_after_stored_creds_rejected", func(t *testing.T) {
		t.Parallel()

		callCount := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callCount++
			authHeader := r.Header.Get("Authorization")
			switch callCount {
			case 1:
				// Anonymous → 401
				assert.Empty(t, authHeader)
				w.WriteHeader(http.StatusUnauthorized)
			case 2:
				// Stored cred → 401 (rejected)
				assert.Equal(t, "Bearer stored_token", authHeader)
				w.WriteHeader(http.StatusUnauthorized)
			case 3:
				// Auth handler bridge → success
				assert.Equal(t, "Bearer handler_token", authHeader)
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode([]map[string]string{
					{"name": "solutions/private-app"},
				})
			default:
				t.Fatalf("unexpected call %d", callCount)
			}
		}))
		defer server.Close()

		mockHandler := auth.NewMockHandler("github")
		mockHandler.GetTokenResult = &auth.Token{AccessToken: "handler_token"}
		mockHandler.StatusResult = &auth.Status{
			Authenticated: true,
			Claims:        &auth.Claims{Username: "testuser"},
		}

		e := &ghcrEnumerator{
			org:        "myorg",
			repository: "myorg",
			client: &orasauth.Client{
				Credential: func(_ context.Context, _ string) (orasauth.Credential, error) {
					return orasauth.Credential{Password: "stored_token"}, nil
				},
				Client: server.Client(),
			},
			httpClient:  server.Client(),
			apiBaseURL:  server.URL,
			authHandler: mockHandler,
			logger:      logr.Discard(),
		}

		repos, err := e.enumerate(t.Context())
		require.NoError(t, err)
		assert.Equal(t, []string{"myorg/solutions/private-app"}, repos)
		assert.Equal(t, 3, callCount, "expected 3 calls: anonymous, stored cred, handler bridge")
	})

	t.Run("auth_error_mentions_read_packages_scope", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		}))
		defer server.Close()

		e := &ghcrEnumerator{
			org:        "myorg",
			repository: "myorg",
			client:     &orasauth.Client{Client: server.Client()},
			httpClient: server.Client(),
			apiBaseURL: server.URL,
			logger:     logr.Discard(),
		}

		_, err := e.enumerate(t.Context())
		require.Error(t, err)
		assert.True(t, IsEnumerationNotSupported(err))
		assert.Contains(t, err.Error(), "read:packages")
	})

	t.Run("user_namespace_fallback_on_404", func(t *testing.T) {
		t.Parallel()

		callCount := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callCount++

			if strings.Contains(r.URL.Path, "/orgs/") {
				// Org endpoint returns 404 for user namespaces.
				w.WriteHeader(http.StatusNotFound)
				return
			}

			// Users endpoint succeeds.
			assert.Contains(t, r.URL.Path, "/users/")
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]map[string]string{
				{"name": "solutions/my-app"},
			})
		}))
		defer server.Close()

		e := &ghcrEnumerator{
			org:        "someuser",
			repository: "someuser",
			client:     &orasauth.Client{Client: server.Client()},
			httpClient: server.Client(),
			apiBaseURL: server.URL,
			logger:     logr.Discard(),
		}

		repos, err := e.enumerate(t.Context())
		require.NoError(t, err)
		assert.Equal(t, []string{"someuser/solutions/my-app"}, repos)
		assert.GreaterOrEqual(t, callCount, 2, "expected at least 2 calls: org attempt + user attempt")
	})
}

func TestNewGHCREnumerator(t *testing.T) {
	t.Parallel()

	t.Run("valid config", func(t *testing.T) {
		t.Parallel()

		e, err := newGHCREnumerator(enumeratorConfig{
			registry:   "ghcr.io",
			repository: "myorg",
			client:     &orasauth.Client{},
			logger:     logr.Discard(),
		})
		require.NoError(t, err)
		assert.Equal(t, "myorg", e.org)
		assert.Equal(t, "myorg", e.repository)
		assert.Equal(t, ghcrDefaultAPIBase, e.apiBaseURL)
	})
}

func BenchmarkGHCREnumerator(b *testing.B) {
	cfg := enumeratorConfig{
		registry:   "ghcr.io",
		repository: "oakwood-commons",
		client:     &orasauth.Client{},
		logger:     logr.Discard(),
	}

	b.ResetTimer()
	for b.Loop() {
		newGHCREnumerator(cfg) //nolint:errcheck // benchmark
	}
}
