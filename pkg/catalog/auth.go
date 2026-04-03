// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/paths"
	"oras.land/oras-go/v2/registry/remote/auth"
)

// CredentialStore provides OCI registry credentials from docker config.
// It supports both static credentials and docker credential helpers,
// with a fallback to scafctl's native credential store.
type CredentialStore struct {
	configPath  string
	config      *dockerConfig
	nativeStore *NativeCredentialStore
	logger      logr.Logger
}

// dockerConfig represents the structure of ~/.docker/config.json
type dockerConfig struct {
	Auths       map[string]dockerAuthEntry `json:"auths"`
	CredHelpers map[string]string          `json:"credHelpers"`
	CredsStore  string                     `json:"credsStore"`
}

// dockerAuthEntry represents a single auth entry in docker config.
type dockerAuthEntry struct {
	Auth          string `json:"auth"`
	Username      string `json:"username"`
	Password      string `json:"password"` //nolint:gosec // G117: not a hardcoded credential, stores docker auth data
	IdentityToken string `json:"identitytoken"`
}

// credHelperResponse is the response from docker credential helpers.
type credHelperResponse struct {
	Username string `json:"Username"`
	Secret   string `json:"Secret"` //nolint:gosec // G117: not a hardcoded credential, stores docker cred helper response
}

// NewCredentialStore creates a credential store from the default docker config.
// It looks for config in the following order:
// 1. $DOCKER_CONFIG/config.json
// 2. ~/.docker/config.json
// 3. $XDG_RUNTIME_DIR/containers/auth.json (podman rootless)
// 4. ~/.config/containers/auth.json (podman)
// 5. /run/containers/$UID/auth.json (podman rootless fallback)
func NewCredentialStore(logger logr.Logger) (*CredentialStore, error) {
	configPath := findDockerConfig()

	store := &CredentialStore{
		configPath:  configPath,
		nativeStore: NewNativeCredentialStore(),
		logger:      logger.WithName("credential-store"),
	}

	// Load config if it exists
	if configPath != "" {
		store.logger.V(1).Info("found container auth config", "path", configPath)
		if err := store.loadConfig(); err != nil {
			store.logger.V(1).Info("failed to load docker config, using anonymous auth",
				"path", configPath,
				"error", err.Error())
		}
	} else {
		store.logger.V(1).Info("no container auth config found, using anonymous auth")
	}

	return store, nil
}

// findDockerConfig locates the docker/podman config file.
// It checks multiple locations in order of priority:
// 1. $DOCKER_CONFIG/config.json
// 2. ~/.docker/config.json
// 3. $XDG_RUNTIME_DIR/containers/auth.json (podman rootless)
// 4. ~/.config/containers/auth.json (podman)
// 5. /run/containers/$UID/auth.json (podman rootless fallback)
func findDockerConfig() string {
	// Check DOCKER_CONFIG env var first
	if dockerConfig := os.Getenv("DOCKER_CONFIG"); dockerConfig != "" {
		configPath := filepath.Join(dockerConfig, "config.json")
		if _, err := os.Stat(configPath); err == nil { //nolint:gosec // G703: path from trusted DOCKER_CONFIG env var
			return configPath
		}
	}

	homeDir, err := paths.HomeDir()
	if err != nil {
		return ""
	}

	// Check ~/.docker/config.json
	dockerPath := filepath.Join(homeDir, ".docker", "config.json")
	if _, err := os.Stat(dockerPath); err == nil {
		return dockerPath
	}

	// Check XDG_RUNTIME_DIR for podman rootless
	if xdgRuntime := os.Getenv("XDG_RUNTIME_DIR"); xdgRuntime != "" {
		podmanPath := filepath.Join(xdgRuntime, "containers", "auth.json")
		if _, err := os.Stat(podmanPath); err == nil { //nolint:gosec // G703: path from trusted XDG_RUNTIME_DIR env var
			return podmanPath
		}
	}

	// Check ~/.config/containers/auth.json (podman default)
	podmanPath := filepath.Join(homeDir, ".config", "containers", "auth.json")
	if _, err := os.Stat(podmanPath); err == nil {
		return podmanPath
	}

	// Check /run/containers/$UID/auth.json (podman rootless fallback)
	if uid := os.Getuid(); uid >= 0 {
		podmanPath := filepath.Join("/run", "containers", fmt.Sprintf("%d", uid), "auth.json")
		if _, err := os.Stat(podmanPath); err == nil {
			return podmanPath
		}
	}

	return ""
}

// loadConfig loads the docker config from disk.
func (c *CredentialStore) loadConfig() error {
	if c.configPath == "" {
		return nil
	}

	data, err := os.ReadFile(c.configPath)
	if err != nil {
		return fmt.Errorf("failed to read docker config: %w", err)
	}

	var config dockerConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to parse docker config: %w", err)
	}

	c.config = &config
	c.logger.V(1).Info("loaded docker config",
		"path", c.configPath,
		"authEntries", len(config.Auths),
		"credHelpers", len(config.CredHelpers))

	return nil
}

// Credential returns the auth credential for a registry host.
// This implements the auth.CredentialFunc signature for oras-go.
func (c *CredentialStore) Credential(ctx context.Context, host string) (auth.Credential, error) {
	// Check environment variables first
	if cred := c.credentialFromEnv(); cred.Username != "" {
		c.logger.V(1).Info("using credentials from environment", "host", host)
		return cred, nil
	}

	if c.config == nil {
		// No Docker config available, try native credential store
		host = normalizeRegistryHost(host)
		if nativeCred := c.credentialFromNativeStore(host); nativeCred.Username != "" {
			c.logger.V(1).Info("using native credential store", "host", host)
			return nativeCred, nil
		}
		return auth.EmptyCredential, nil
	}

	// Normalize host for Docker Hub
	host = normalizeRegistryHost(host)

	// Check for credential helper specific to this host
	if helper, ok := c.config.CredHelpers[host]; ok {
		cred, err := c.credentialFromHelper(ctx, helper, host)
		if err != nil {
			c.logger.V(1).Info("credential helper failed, trying other methods",
				"helper", helper,
				"host", host,
				"error", err.Error())
		} else {
			c.logger.V(1).Info("using credential helper", "helper", helper, "host", host)
			return cred, nil
		}
	}

	// Check default credential store
	if c.config.CredsStore != "" {
		cred, err := c.credentialFromHelper(ctx, c.config.CredsStore, host)
		if err == nil && cred.Username != "" {
			c.logger.V(1).Info("using default credential store",
				"store", c.config.CredsStore, "host", host)
			return cred, nil
		}
	}

	// Check static auth entries
	if authEntry, ok := c.config.Auths[host]; ok {
		cred, err := c.credentialFromAuthEntry(authEntry)
		if err == nil {
			c.logger.V(1).Info("using static auth entry", "host", host)
			return cred, nil
		}
	}

	// Also try with https:// prefix for some registries
	hostWithScheme := "https://" + host
	if authEntry, ok := c.config.Auths[hostWithScheme]; ok {
		cred, err := c.credentialFromAuthEntry(authEntry)
		if err == nil {
			c.logger.V(1).Info("using static auth entry with scheme", "host", hostWithScheme)
			return cred, nil
		}
	}

	// Fall back to scafctl native credential store
	if nativeCred := c.credentialFromNativeStore(host); nativeCred.Username != "" {
		c.logger.V(1).Info("using native credential store", "host", host)
		return nativeCred, nil
	}

	c.logger.V(1).Info("no credentials found, using anonymous auth", "host", host)
	return auth.EmptyCredential, nil
}

// credentialFromEnv returns credentials from environment variables.
func (c *CredentialStore) credentialFromEnv() auth.Credential {
	username := os.Getenv("SCAFCTL_REGISTRY_USERNAME")
	password := os.Getenv("SCAFCTL_REGISTRY_PASSWORD")

	if username != "" && password != "" {
		return auth.Credential{
			Username: username,
			Password: password,
		}
	}

	return auth.EmptyCredential
}

// credentialFromAuthEntry extracts credentials from a docker auth entry.
func (c *CredentialStore) credentialFromAuthEntry(entry dockerAuthEntry) (auth.Credential, error) {
	// Check for identity token (OAuth2)
	if entry.IdentityToken != "" {
		return auth.Credential{
			RefreshToken: entry.IdentityToken,
		}, nil
	}

	// Check for explicit username/password
	if entry.Username != "" && entry.Password != "" {
		return auth.Credential{
			Username: entry.Username,
			Password: entry.Password,
		}, nil
	}

	// Decode base64 auth string
	if entry.Auth != "" {
		decoded, err := base64.StdEncoding.DecodeString(entry.Auth)
		if err != nil {
			return auth.EmptyCredential, fmt.Errorf("failed to decode auth: %w", err)
		}

		parts := strings.SplitN(string(decoded), ":", 2)
		if len(parts) == 2 {
			return auth.Credential{
				Username: parts[0],
				Password: parts[1],
			}, nil
		}
	}

	return auth.EmptyCredential, fmt.Errorf("no valid credentials in auth entry")
}

// credentialFromHelper retrieves credentials using a docker credential helper.
func (c *CredentialStore) credentialFromHelper(ctx context.Context, helper, host string) (auth.Credential, error) {
	// Docker credential helpers are named docker-credential-<helper>
	helperName := "docker-credential-" + helper

	//nolint:gosec // Command is constructed from docker config, not user input
	cmd := exec.CommandContext(ctx, helperName, "get")
	cmd.Stdin = strings.NewReader(host)

	output, err := cmd.Output()
	if err != nil {
		return auth.EmptyCredential, fmt.Errorf("credential helper %q failed: %w", helperName, err)
	}

	var resp credHelperResponse
	if err := json.Unmarshal(output, &resp); err != nil {
		return auth.EmptyCredential, fmt.Errorf("failed to parse credential helper response: %w", err)
	}

	return auth.Credential{
		Username: resp.Username,
		Password: resp.Secret,
	}, nil
}

// normalizeRegistryHost normalizes registry hostnames for lookup.
// This handles Docker Hub's special casing.
func normalizeRegistryHost(host string) string {
	// Docker Hub uses various hostnames
	switch host {
	case "docker.io", "registry-1.docker.io", "index.docker.io":
		return "https://index.docker.io/v1/"
	default:
		return host
	}
}

// credentialFromNativeStore checks the scafctl native credential store.
func (c *CredentialStore) credentialFromNativeStore(host string) auth.Credential {
	if c.nativeStore == nil {
		return auth.EmptyCredential
	}

	cred, err := c.nativeStore.GetCredential(host)
	if err != nil {
		c.logger.V(1).Info("native credential store read failed",
			"host", host,
			"error", err.Error())
		return auth.EmptyCredential
	}
	if cred == nil {
		return auth.EmptyCredential
	}

	return auth.Credential{
		Username: cred.Username,
		Password: cred.Password,
	}
}

// CredentialFunc returns an auth.CredentialFunc for use with oras-go.
func (c *CredentialStore) CredentialFunc() auth.CredentialFunc {
	return c.Credential
}
