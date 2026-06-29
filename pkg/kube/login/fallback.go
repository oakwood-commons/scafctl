// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package login

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/oakwood-commons/scafctl/pkg/auth/execcredential"
	"github.com/oakwood-commons/scafctl/pkg/kubeconfig"
)

// Permissions for the kubeconfig file and its parent directory.
const (
	kubeconfigFileMode = 0o600
	kubeconfigDirMode  = 0o700
)

// Top-level kubeconfig keys manipulated by the static writer.
const (
	keyAPIVersion     = "apiVersion"
	keyKind           = "kind"
	keyClusters       = "clusters"
	keyUsers          = "users"
	keyContexts       = "contexts"
	keyCurrentContext = "current-context"
)

// writeStaticKubeconfig writes a minimal exec-credential kubeconfig without the
// provider plugin. It is the fallback path taken when the kubeconfig provider is
// unavailable. It preserves all existing data in the target file by round-
// tripping through a generic map (comments and key ordering are not preserved).
func writeStaticKubeconfig(in kubeconfig.WriteInput) (kubeconfig.WriteResult, error) {
	path, err := resolveKubeconfigPath(in.KubeconfigPath)
	if err != nil {
		return kubeconfig.WriteResult{}, err
	}
	clusterName := in.ClusterName
	contextName := firstNonEmpty(in.ContextName, clusterName)
	userName := firstNonEmpty(in.UserName, clusterName)

	cfg, err := loadKubeconfig(path)
	if err != nil {
		return kubeconfig.WriteResult{}, err
	}
	cfg[keyAPIVersion] = "v1"
	cfg[keyKind] = "Config"

	cfg[keyClusters] = upsertNamed(toAnySlice(cfg[keyClusters]), clusterName, "cluster", clusterValue(in))
	cfg[keyUsers] = upsertNamed(toAnySlice(cfg[keyUsers]), userName, "user", userValue(in))
	cfg[keyContexts] = upsertNamed(toAnySlice(cfg[keyContexts]), contextName, "context", map[string]any{
		"cluster": clusterName,
		"user":    userName,
	})
	if in.SetCurrentContext {
		cfg[keyCurrentContext] = contextName
	}

	if err := writeKubeconfigFile(path, cfg); err != nil {
		return kubeconfig.WriteResult{}, err
	}
	return kubeconfig.WriteResult{
		Success:        true,
		ContextName:    contextName,
		KubeconfigPath: path,
	}, nil
}

// removeStaticKubeconfig removes the named cluster/user/context entries from the
// kubeconfig file without the provider plugin. It returns whether any entry was
// removed.
func removeStaticKubeconfig(in kubeconfig.RemoveInput) (bool, error) {
	path, err := resolveKubeconfigPath(in.KubeconfigPath)
	if err != nil {
		return false, err
	}
	data, err := os.ReadFile(path) //nolint:gosec // path is a user-controlled kubeconfig location
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read kubeconfig %q: %w", path, err)
	}
	var cfg map[string]any
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return false, fmt.Errorf("parse kubeconfig %q: %w", path, err)
	}
	if cfg == nil {
		return false, nil
	}

	clusterName := in.ClusterName
	contextName := firstNonEmpty(in.ContextName, clusterName)
	userName := firstNonEmpty(in.UserName, clusterName)

	clusters, rc := removeNamed(toAnySlice(cfg[keyClusters]), clusterName)
	users, ru := removeNamed(toAnySlice(cfg[keyUsers]), userName)
	contexts, rx := removeNamed(toAnySlice(cfg[keyContexts]), contextName)
	if !rc && !ru && !rx {
		return false, nil
	}
	cfg[keyClusters] = clusters
	cfg[keyUsers] = users
	cfg[keyContexts] = contexts
	if cc, _ := cfg[keyCurrentContext].(string); cc == contextName {
		delete(cfg, keyCurrentContext)
	}

	if err := writeKubeconfigFile(path, cfg); err != nil {
		return false, err
	}
	return true, nil
}

// clusterValue builds the kubeconfig cluster entry. CA data is preferred over
// skipping TLS verification.
func clusterValue(in kubeconfig.WriteInput) map[string]any {
	cluster := map[string]any{"server": in.Server}
	switch {
	case in.CAData != "":
		cluster["certificate-authority-data"] = base64.StdEncoding.EncodeToString([]byte(in.CAData))
	case in.InsecureSkipTLS:
		cluster["insecure-skip-tls-verify"] = true
	}
	return cluster
}

// userValue builds the kubeconfig user entry with a client-go exec credential
// plugin block.
func userValue(in kubeconfig.WriteInput) map[string]any {
	exec := map[string]any{
		"apiVersion":         execcredential.DefaultAPIVersion,
		"command":            in.ExecCommand,
		"args":               in.ExecArgs,
		"provideClusterInfo": in.ProvideClusterInfo,
	}
	if in.InteractiveMode != "" {
		exec["interactiveMode"] = in.InteractiveMode
	}
	if in.InstallHint != "" {
		exec["installHint"] = in.InstallHint
	}
	return map[string]any{"exec": exec}
}

// loadKubeconfig reads and parses an existing kubeconfig, returning an empty map
// when the file does not exist.
func loadKubeconfig(path string) (map[string]any, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path is a user-controlled kubeconfig location
	switch {
	case err == nil:
		var cfg map[string]any
		if uerr := yaml.Unmarshal(data, &cfg); uerr != nil {
			return nil, fmt.Errorf("parse existing kubeconfig %q: %w", path, uerr)
		}
		if cfg == nil {
			cfg = map[string]any{}
		}
		return cfg, nil
	case errors.Is(err, fs.ErrNotExist):
		return map[string]any{}, nil
	default:
		return nil, fmt.Errorf("read kubeconfig %q: %w", path, err)
	}
}

// writeKubeconfigFile marshals and writes the kubeconfig with restrictive
// permissions, creating the parent directory when needed. The write is atomic
// (temp file + fsync + rename) so an interrupt or a concurrent login cannot
// leave a truncated or corrupted kubeconfig.
func writeKubeconfigFile(path string, cfg map[string]any) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal kubeconfig: %w", err)
	}
	dir := filepath.Dir(path)
	if dir == "" {
		dir = "."
	}
	if err := os.MkdirAll(dir, kubeconfigDirMode); err != nil {
		return fmt.Errorf("create kubeconfig directory %q: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp kubeconfig in %q: %w", dir, err)
	}
	tmpName := tmp.Name()
	removeTmp := true
	defer func() {
		if removeTmp {
			_ = os.Remove(tmpName)
		}
	}()

	if err := tmp.Chmod(kubeconfigFileMode); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temp kubeconfig %q: %w", tmpName, err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp kubeconfig %q: %w", tmpName, err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync temp kubeconfig %q: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp kubeconfig %q: %w", tmpName, err)
	}

	renameErr := os.Rename(tmpName, path) //nolint:gosec // path is a user-controlled kubeconfig location
	if renameErr != nil {
		// Windows does not reliably support renaming over an existing file.
		// Retry after removing the destination when it already exists.
		if _, statErr := os.Stat(path); statErr == nil {
			if removeErr := os.Remove(path); removeErr == nil {
				renameErr = os.Rename(tmpName, path) //nolint:gosec // same rationale as above
			}
		}
		if renameErr != nil {
			return fmt.Errorf("write kubeconfig %q: %w", path, renameErr)
		}
	}
	removeTmp = false
	return nil
}

// resolveKubeconfigPath resolves the kubeconfig path: the explicit path, else
// the first entry of KUBECONFIG, else ~/.kube/config.
func resolveKubeconfigPath(explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	if env := os.Getenv("KUBECONFIG"); env != "" {
		for _, p := range filepath.SplitList(env) {
			if p != "" {
				return p, nil
			}
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory for kubeconfig: %w", err)
	}
	return filepath.Join(home, ".kube", "config"), nil
}

// upsertNamed replaces the entry named name in a kubeconfig list (clusters,
// users, or contexts) or appends it. key is the inner field name ("cluster",
// "user", or "context").
func upsertNamed(list []any, name, key string, value map[string]any) []any {
	entry := map[string]any{"name": name, key: value}
	for i, item := range list {
		if m, ok := item.(map[string]any); ok {
			if n, _ := m["name"].(string); n == name {
				list[i] = entry
				return list
			}
		}
	}
	return append(list, entry)
}

// removeNamed drops the entry named name from a kubeconfig list, reporting
// whether a match was removed.
func removeNamed(list []any, name string) ([]any, bool) {
	out := make([]any, 0, len(list))
	removed := false
	for _, item := range list {
		if m, ok := item.(map[string]any); ok {
			if n, _ := m["name"].(string); n == name {
				removed = true
				continue
			}
		}
		out = append(out, item)
	}
	return out, removed
}

// toAnySlice coerces a kubeconfig list field to []any, returning nil for absent
// or malformed values.
func toAnySlice(v any) []any {
	if s, ok := v.([]any); ok {
		return s
	}
	return nil
}
