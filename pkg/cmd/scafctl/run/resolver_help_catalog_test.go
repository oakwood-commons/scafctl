// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// startMinimalOCIServer starts an in-memory TLS OCI distribution server and
// stores a single versioned solution artifact for configured-alias help tests.
func startMinimalOCIServer(t *testing.T, repository, solName, ver, solYAML string) string {
	t.Helper()

	var (
		mu        sync.RWMutex
		blobs     = make(map[string][]byte)
		manifests = make(map[string][]byte)
		tags      = make(map[string]map[string]string)
	)

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		p := req.URL.Path
		switch {
		case p == "/v2/":
			w.WriteHeader(http.StatusOK)

		case strings.HasSuffix(p, "/tags/list"):
			repo := strings.TrimSuffix(strings.TrimPrefix(p, "/v2/"), "/tags/list")
			mu.RLock()
			repoTags, ok := tags[repo]
			list := make([]string, 0, len(repoTags))
			for k := range repoTags {
				list = append(list, k)
			}
			mu.RUnlock()
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			sort.Strings(list)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"name": repo, "tags": list})

		case strings.Contains(p, "/blobs/uploads/") && req.Method == http.MethodPost:
			repo := strings.TrimSuffix(strings.TrimPrefix(p, "/v2/"), "/blobs/uploads/")
			w.Header().Set("Location", "/v2/"+repo+"/blobs/uploads/u1")
			w.WriteHeader(http.StatusAccepted)

		case strings.Contains(p, "/blobs/uploads/") && req.Method == http.MethodPut:
			dgst := req.URL.Query().Get("digest")
			body, err := io.ReadAll(req.Body)
			if dgst == "" || err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			mu.Lock()
			blobs[dgst] = body
			mu.Unlock()
			w.Header().Set("Docker-Content-Digest", dgst)
			w.WriteHeader(http.StatusCreated)

		case strings.Contains(p, "/blobs/"):
			idx := strings.LastIndex(p, "/blobs/")
			dgst := p[idx+len("/blobs/"):]
			mu.RLock()
			data, ok := blobs[dgst]
			mu.RUnlock()
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
			w.Header().Set("Docker-Content-Digest", dgst)
			if req.Method == http.MethodGet {
				_, _ = w.Write(data)
			}

		case strings.Contains(p, "/manifests/"):
			idx := strings.LastIndex(p, "/manifests/")
			repo := strings.TrimPrefix(p[:idx], "/v2/")
			ref := p[idx+len("/manifests/"):]
			switch req.Method {
			case http.MethodPut:
				body, err := io.ReadAll(req.Body)
				if err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				dgst := digest.FromBytes(body).String()
				mu.Lock()
				manifests[repo+":"+dgst] = body
				if tags[repo] == nil {
					tags[repo] = make(map[string]string)
				}
				if !strings.HasPrefix(ref, "sha256:") {
					tags[repo][ref] = dgst
				}
				mu.Unlock()
				w.Header().Set("Docker-Content-Digest", dgst)
				w.WriteHeader(http.StatusCreated)
			case http.MethodGet, http.MethodHead:
				mu.RLock()
				dgst := ref
				if d, ok := tags[repo][ref]; ok {
					dgst = d
				}
				data, ok := manifests[repo+":"+dgst]
				mu.RUnlock()
				if !ok {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				mediaType := ocispec.MediaTypeImageManifest
				var probe struct {
					MediaType string `json:"mediaType"`
				}
				if json.Unmarshal(data, &probe) == nil && probe.MediaType != "" {
					mediaType = probe.MediaType
				}
				w.Header().Set("Content-Type", mediaType)
				w.Header().Set("Docker-Content-Digest", dgst)
				w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
				if req.Method == http.MethodGet {
					_, _ = w.Write(data)
				}
			default:
				w.WriteHeader(http.StatusMethodNotAllowed)
			}

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	host := strings.TrimPrefix(srv.URL, "https://")
	remoteCat, err := catalog.NewRemoteCatalog(catalog.RemoteCatalogConfig{
		Name:       "seed",
		Registry:   host,
		Repository: repository,
		Insecure:   true,
		Logger:     logr.Discard(),
	})
	require.NoError(t, err)

	_, err = remoteCat.Store(context.Background(), catalog.Reference{
		Kind:    catalog.ArtifactKindSolution,
		Name:    solName,
		Version: semver.MustParse(ver),
	}, []byte(solYAML), nil, nil, false)
	require.NoError(t, err)

	return host
}

func TestCommandResolver_DynamicHelp_ConfiguredRemoteCatalogAlias(t *testing.T) {
	const remoteSolYAML = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: remote-alias-app
  version: 1.0.0
spec:
  resolvers:
    remote_alias_param:
      description: parameter from configured remote alias
      type: string
      resolve:
        with:
          - provider: parameter
            inputs:
              key: remote_alias_param
`

	host := startMinimalOCIServer(t, "team/solutions", "remote-alias-app", "1.0.0", remoteSolYAML)
	emptyWorkDir := t.TempDir()

	catalogConfigYAML := func(alias string) string {
		return fmt.Sprintf("catalogs:\n  - name: %s\n    type: oci\n    url: oci://%s/team/solutions\n    insecure: true\n", alias, host)
	}

	buildRootWithSubcommands := func(cfgDefaultPath, configDefaultsYAML string, cliParams *settings.Run) *cobra.Command {
		root := &cobra.Command{
			Use: "mycli",
		}
		if configDefaultsYAML != "" {
			root.Annotations = map[string]string{"configDefaults": configDefaultsYAML}
		}
		root.PersistentFlags().String("config", cfgDefaultPath, "")

		streams, _, _ := terminal.NewTestIOStreams()
		runCmd := &cobra.Command{Use: "run"}
		runCmd.AddCommand(CommandResolver(cliParams, streams, "mycli/run"))
		valCmd := &cobra.Command{Use: "validate"}
		valCmd.AddCommand(CommandValidateResolver(cliParams, streams, "mycli/validate"))
		root.AddCommand(runCmd, valCmd)
		return root
	}

	t.Run("config file alias with pre-set ActionDiscoveryFileNames works for run and validate resolver", func(t *testing.T) {
		cfgFile := filepath.Join(t.TempDir(), "config.yaml")
		require.NoError(t, os.WriteFile(cfgFile, []byte(catalogConfigYAML("shared")), 0o600))

		for _, subcmd := range []string{"run", "validate"} {
			t.Setenv("XDG_DATA_HOME", t.TempDir())
			t.Setenv("XDG_CACHE_HOME", t.TempDir())

			cliParams := settings.NewCliParams()
			cliParams.BinaryName = "mycli"
			// Pre-populate ActionDiscoveryFileNames to verify config loading and
			// attachment happen independently of the custom-action-filename check.
			cliParams.ActionDiscoveryFileNames = []string{"custom-actions.yaml"}

			root := buildRootWithSubcommands("", "", cliParams)
			var out bytes.Buffer
			root.SetOut(&out)
			root.SetArgs([]string{"--config", cfgFile, subcmd, "resolver", "-s", "remote-alias-app", "-c", "shared", "--help"})

			require.NoError(t, root.ExecuteContext(provider.WithWorkingDirectory(t.Context(), emptyWorkDir)))
			assert.Contains(t, out.String(), "remote_alias_param", "%s resolver should render parameter table from configured alias", subcmd)
			assert.Contains(t, out.String(), "parameter from configured remote alias")
		}
	})

	t.Run("embedder configDefaults alias works for run and validate resolver", func(t *testing.T) {
		missingCfg := filepath.Join(t.TempDir(), "nonexistent-config.yaml")
		defaultsYAML := catalogConfigYAML("embedder-shared")

		for _, subcmd := range []string{"run", "validate"} {
			t.Setenv("XDG_DATA_HOME", t.TempDir())
			t.Setenv("XDG_CACHE_HOME", t.TempDir())

			cliParams := settings.NewCliParams()
			cliParams.BinaryName = "mycli"
			cliParams.ActionDiscoveryFileNames = []string{"custom-actions.yaml"}

			root := buildRootWithSubcommands(missingCfg, defaultsYAML, cliParams)
			var out bytes.Buffer
			root.SetOut(&out)
			root.SetArgs([]string{subcmd, "resolver", "-s", "remote-alias-app", "-c", "embedder-shared", "--help"})

			require.NoError(t, root.ExecuteContext(provider.WithWorkingDirectory(t.Context(), emptyWorkDir)))
			assert.Contains(t, out.String(), "remote_alias_param", "%s resolver should render parameter table from embedder configDefaults alias", subcmd)
			assert.Contains(t, out.String(), "parameter from configured remote alias")
		}
	})

	t.Run("unconfigured alias silently omits parameter table without failing help", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", t.TempDir())
		t.Setenv("XDG_CACHE_HOME", t.TempDir())

		missingCfg := filepath.Join(t.TempDir(), "nonexistent-config.yaml")
		cliParams := settings.NewCliParams()
		cliParams.BinaryName = "mycli"

		root := buildRootWithSubcommands(missingCfg, "", cliParams)
		var out bytes.Buffer
		root.SetOut(&out)
		root.SetArgs([]string{"run", "resolver", "-s", "remote-alias-app", "-c", "unknown-alias", "--help"})

		require.NoError(t, root.ExecuteContext(provider.WithWorkingDirectory(t.Context(), emptyWorkDir)))
		assert.NotContains(t, out.String(), "remote_alias_param")
	})
}
