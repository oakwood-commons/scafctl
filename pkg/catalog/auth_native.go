// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/oakwood-commons/scafctl/pkg/paths"
)

// nativeCredentialFileName is the file name for the native credential store.
const nativeCredentialFileName = "registries.json"

// nativeCredentialFilePermissions is the file permission for the native credential store.
const nativeCredentialFilePermissions = 0o600

// nativeCredentialDirPermissions is the directory permission for the credential store parent.
const nativeCredentialDirPermissions = 0o700

// containerAuthFilePermissions is the file permission for container auth files.
const containerAuthFilePermissions = 0o600

// containerAuthDirPermissions is the directory permission for container auth parent dirs.
const containerAuthDirPermissions = 0o700

// NativeCredential represents a stored registry credential.
type NativeCredential struct {
	Username      string `json:"username"`
	Password      string `json:"password"` //nolint:gosec // G117: not a hardcoded credential, stores OCI registry auth data
	ContainerAuth bool   `json:"containerAuth,omitempty"`
}

// nativeCredentialFile represents the on-disk format of the credential store.
type nativeCredentialFile struct {
	Registries map[string]NativeCredential `json:"registries"`
}

// NativeCredentialStore manages scafctl-native OCI registry credentials.
// Credentials are stored at <XDG_CONFIG_HOME>/scafctl/registries.json.
type NativeCredentialStore struct {
	mu   sync.RWMutex
	path string
}

// NewNativeCredentialStore creates a new native credential store.
func NewNativeCredentialStore() *NativeCredentialStore {
	return &NativeCredentialStore{
		path: filepath.Join(paths.ConfigDir(), nativeCredentialFileName),
	}
}

// NewNativeCredentialStoreWithPath creates a native credential store at a custom path.
// This is primarily used for testing.
func NewNativeCredentialStoreWithPath(path string) *NativeCredentialStore {
	return &NativeCredentialStore{
		path: path,
	}
}

// Path returns the file path of the credential store.
func (s *NativeCredentialStore) Path() string {
	return s.path
}

// GetCredential returns the credential for the given registry host.
func (s *NativeCredentialStore) GetCredential(host string) (*NativeCredential, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	creds, err := s.load()
	if err != nil {
		return nil, fmt.Errorf("load native credentials: %w", err)
	}

	cred, ok := creds.Registries[host]
	if !ok {
		return nil, nil
	}

	return &cred, nil
}

// SetCredential stores a credential for the given registry host.
func (s *NativeCredentialStore) SetCredential(host, username, password string, containerAuth bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	creds, err := s.load()
	if err != nil {
		return fmt.Errorf("load native credentials: %w", err)
	}

	creds.Registries[host] = NativeCredential{
		Username:      username,
		Password:      password,
		ContainerAuth: containerAuth,
	}

	return s.save(creds)
}

// DeleteCredential removes the credential for the given registry host.
func (s *NativeCredentialStore) DeleteCredential(host string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	creds, err := s.load()
	if err != nil {
		return fmt.Errorf("load native credentials: %w", err)
	}

	if _, ok := creds.Registries[host]; !ok {
		return nil
	}

	delete(creds.Registries, host)

	return s.save(creds)
}

// ListCredentials returns all stored registry hosts and their usernames.
func (s *NativeCredentialStore) ListCredentials() (map[string]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	creds, err := s.load()
	if err != nil {
		return nil, fmt.Errorf("load native credentials: %w", err)
	}

	result := make(map[string]string, len(creds.Registries))
	for host, cred := range creds.Registries {
		result[host] = cred.Username
	}

	return result, nil
}

// DeleteAll removes all stored credentials.
func (s *NativeCredentialStore) DeleteAll() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	creds := &nativeCredentialFile{
		Registries: make(map[string]NativeCredential),
	}

	return s.save(creds)
}

// WriteContainerAuth writes a credential to the container auth file for
// Docker/Podman interop. The file is detected in order:
// 1. $REGISTRY_AUTH_FILE environment variable
// 2. ~/.config/containers/auth.json
// 3. ~/.docker/config.json
// This is a best-effort operation; errors are returned but callers should
// treat them as warnings since the native store already succeeded.
func (s *NativeCredentialStore) WriteContainerAuth(host, username, password string) error {
	authFile, err := detectContainerAuthFile()
	if err != nil {
		return fmt.Errorf("detect container auth file: %w", err)
	}

	return writeContainerAuthEntry(authFile, host, username, password)
}

// DeleteContainerAuth removes a credential from the container auth file.
// This is a best-effort operation.
func (s *NativeCredentialStore) DeleteContainerAuth(host string) error {
	authFile, err := detectContainerAuthFile()
	if err != nil {
		return fmt.Errorf("detect container auth file: %w", err)
	}

	return deleteContainerAuthEntry(authFile, host)
}

// load reads the credential file from disk.
// Returns an empty store if the file does not exist.
func (s *NativeCredentialStore) load() (*nativeCredentialFile, error) {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return &nativeCredentialFile{
			Registries: make(map[string]NativeCredential),
		}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read credential file %s: %w", s.path, err)
	}

	var creds nativeCredentialFile
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, fmt.Errorf("parse credential file %s: %w", s.path, err)
	}

	if creds.Registries == nil {
		creds.Registries = make(map[string]NativeCredential)
	}

	return &creds, nil
}

// save writes the credential file to disk with secure permissions.
func (s *NativeCredentialStore) save(creds *nativeCredentialFile) error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, nativeCredentialDirPermissions); err != nil {
		return fmt.Errorf("create credential directory %s: %w", dir, err)
	}

	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal credentials: %w", err)
	}

	if err := os.WriteFile(s.path, data, nativeCredentialFilePermissions); err != nil {
		return fmt.Errorf("write credential file %s: %w", s.path, err)
	}

	return nil
}

// containerAuthConfig represents a minimal Docker/Podman auth config.
type containerAuthConfig struct {
	Auths map[string]containerAuthEntry `json:"auths"`
}

// containerAuthEntry represents an auth entry in container config.
type containerAuthEntry struct {
	Auth string `json:"auth"`
}

// detectContainerAuthFile finds the container auth file path.
func detectContainerAuthFile() (string, error) {
	// 1. $REGISTRY_AUTH_FILE
	if envPath := os.Getenv("REGISTRY_AUTH_FILE"); envPath != "" {
		return envPath, nil
	}

	homeDir, err := paths.HomeDir()
	if err != nil {
		return "", fmt.Errorf("determine home directory: %w", err)
	}

	// 2. ~/.config/containers/auth.json
	podmanPath := filepath.Join(homeDir, ".config", "containers", "auth.json")
	if _, err := os.Stat(podmanPath); err == nil {
		return podmanPath, nil
	}

	// 3. ~/.docker/config.json
	dockerPath := filepath.Join(homeDir, ".docker", "config.json")
	if _, err := os.Stat(dockerPath); err == nil {
		return dockerPath, nil
	}

	// Default to podman location (will be created)
	return podmanPath, nil
}

// writeContainerAuthEntry writes a single auth entry to the container auth file.
func writeContainerAuthEntry(filePath, host, username, password string) error {
	cfg := &containerAuthConfig{
		Auths: make(map[string]containerAuthEntry),
	}

	// Read existing config if it exists
	data, err := os.ReadFile(filePath)
	if err == nil {
		if jsonErr := json.Unmarshal(data, cfg); jsonErr != nil {
			// Can't parse existing file — start fresh to avoid corruption
			cfg = &containerAuthConfig{
				Auths: make(map[string]containerAuthEntry),
			}
		}
	}

	// Encode credentials as base64
	encoded := encodeBasicAuth(username, password)
	cfg.Auths[host] = containerAuthEntry{Auth: encoded}

	return writeContainerAuthFile(filePath, cfg)
}

// deleteContainerAuthEntry removes an auth entry from the container auth file.
func deleteContainerAuthEntry(filePath, host string) error {
	data, err := os.ReadFile(filePath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read container auth file %s: %w", filePath, err)
	}

	// Use map to preserve unknown fields
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("parse container auth file %s: %w", filePath, err)
	}

	authsRaw, ok := raw["auths"]
	if !ok {
		return nil
	}

	var auths map[string]json.RawMessage
	if err := json.Unmarshal(authsRaw, &auths); err != nil {
		return fmt.Errorf("parse auths in container auth file: %w", err)
	}

	if _, ok := auths[host]; !ok {
		return nil
	}

	delete(auths, host)

	updatedAuths, err := json.Marshal(auths)
	if err != nil {
		return fmt.Errorf("marshal updated auths: %w", err)
	}
	raw["auths"] = updatedAuths

	updatedData, err := json.MarshalIndent(raw, "", "\t")
	if err != nil {
		return fmt.Errorf("marshal updated container auth file: %w", err)
	}

	return os.WriteFile(filePath, updatedData, containerAuthFilePermissions)
}

// writeContainerAuthFile writes the container auth config to disk.
func writeContainerAuthFile(filePath string, cfg *containerAuthConfig) error {
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, containerAuthDirPermissions); err != nil {
		return fmt.Errorf("create container auth directory %s: %w", dir, err)
	}

	// Read existing file to preserve fields we don't manage
	existing := make(map[string]json.RawMessage)
	data, err := os.ReadFile(filePath)
	if err == nil {
		_ = json.Unmarshal(data, &existing) // ignore parse errors, we'll merge what we can
	}

	// Update the auths field
	authsData, err := json.Marshal(cfg.Auths)
	if err != nil {
		return fmt.Errorf("marshal auths: %w", err)
	}
	existing["auths"] = authsData

	result, err := json.MarshalIndent(existing, "", "\t")
	if err != nil {
		return fmt.Errorf("marshal container auth file: %w", err)
	}

	return os.WriteFile(filePath, result, containerAuthFilePermissions)
}

// encodeBasicAuth creates a base64-encoded auth string for Docker config.
func encodeBasicAuth(username, password string) string {
	auth := username + ":" + password
	return base64.StdEncoding.EncodeToString([]byte(auth))
}
