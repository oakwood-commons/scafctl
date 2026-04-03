// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/oakwood-commons/scafctl/pkg/paths"
	"github.com/oakwood-commons/scafctl/pkg/secrets"
)

// nativeCredentialFileName is the file name for the native credential store.
const nativeCredentialFileName = "registries.json"

// nativeRegPasswordKeyPrefix is the secrets-store namespace for registry passwords.
// Passwords are stored encrypted when a secrets store is available.
const nativeRegPasswordKeyPrefix = "native-reg-" //nolint:gosec // namespace prefix, not a credential

// nativeCredentialFilePermissions is the file permission for the native credential store.
const nativeCredentialFilePermissions = 0o600

// nativeCredentialDirPermissions is the directory permission for the credential store parent.
const nativeCredentialDirPermissions = 0o700

// containerAuthFilePermissions is the file permission for container auth files.
const containerAuthFilePermissions = 0o600

// containerAuthDirPermissions is the directory permission for container auth parent dirs.
const containerAuthDirPermissions = 0o700

// NativeCredential represents a stored registry credential.
// When a secrets store is available, the Password field is empty in the JSON file
// and the actual password is retrieved from the encrypted secrets store at runtime.
// Legacy entries that contain a plaintext password in JSON are still readable.
type NativeCredential struct {
	Username          string `json:"username"`
	Password          string `json:"password,omitempty"`          //nolint:gosec // stored encrypted via secretsStore when available; JSON field retained for legacy migration
	ContainerAuthFile string `json:"containerAuthFile,omitempty"` // path to the container auth file written on login
}

// nativeCredentialFile represents the on-disk format of the credential store.
type nativeCredentialFile struct {
	Registries map[string]NativeCredential `json:"registries"`
}

// NativeCredentialStore manages scafctl-native OCI registry credentials.
// Credentials are stored at <XDG_CONFIG_HOME>/scafctl/registries.json.
// When a secrets store is available, passwords are stored encrypted there instead
// of in the JSON file. Legacy entries with plaintext JSON passwords remain readable.
type NativeCredentialStore struct {
	mu           sync.RWMutex
	path         string
	secretsStore secrets.Store // nil in test mode; passwords fall back to JSON when nil
}

// NewNativeCredentialStore creates a new native credential store without an
// encrypted secrets backend. Passwords fall back to the plaintext JSON field.
// Callers that have access to a pre-configured secrets store (e.g. the shared
// store created in root.go) should use NewNativeCredentialStoreWithSecretsStore
// so that settings such as RequireSecureKeyring are honoured.
func NewNativeCredentialStore() *NativeCredentialStore {
	return &NativeCredentialStore{
		path: filepath.Join(paths.ConfigDir(), nativeCredentialFileName),
	}
}

// NewNativeCredentialStoreWithSecretsStore creates a new native credential store
// using the provided, pre-configured secrets store for encrypted password storage.
// Use this constructor whenever a secrets store is available so that centralised
// security settings (RequireSecureKeyring, logger) are honoured.
func NewNativeCredentialStoreWithSecretsStore(secretsStore secrets.Store) *NativeCredentialStore {
	return &NativeCredentialStore{
		path:         filepath.Join(paths.ConfigDir(), nativeCredentialFileName),
		secretsStore: secretsStore,
	}
}

// NewNativeCredentialStoreWithPath creates a native credential store at a custom path.
// The secrets store is not initialised so passwords are stored in plaintext JSON.
// This is intended for testing only.
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
// The host is normalized so that Docker Hub variants resolve to a canonical key.
// When a secrets store is available, the password is retrieved from encrypted storage.
func (s *NativeCredentialStore) GetCredential(host string) (*NativeCredential, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.getCredentialLocked(host)
}

func (s *NativeCredentialStore) getCredentialLocked(host string) (*NativeCredential, error) {
	creds, err := s.load()
	if err != nil {
		return nil, fmt.Errorf("load native credentials: %w", err)
	}

	normalized := normalizeRegistryHost(host)
	cred, ok := creds.Registries[normalized]
	if !ok {
		return nil, nil
	}

	// Retrieve password from encrypted secrets store when available.
	// Fall back to the JSON field for legacy entries that pre-date encryption.
	if s.secretsStore != nil {
		passBytes, secretErr := s.secretsStore.Get(context.Background(), nativeRegPasswordKeyPrefix+normalized)
		if secretErr == nil {
			cred.Password = string(passBytes)
		} else if !errors.Is(secretErr, secrets.ErrNotFound) {
			return nil, fmt.Errorf("retrieve encrypted password for %s: %w", host, secretErr)
		}
		// ErrNotFound: fall through to the plaintext JSON value (legacy migration path)
	}

	return &cred, nil
}

// SetCredential stores a credential for the given registry host.
// The host is normalized so that Docker Hub variants are stored under a canonical key.
// containerAuthFile is the path to the container auth file written during login, or
// empty if no container auth file was written.
// When a secrets store is available the password is stored encrypted; only
// the username and metadata are written to the JSON file.
func (s *NativeCredentialStore) SetCredential(host, username, password, containerAuthFile string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	creds, err := s.load()
	if err != nil {
		return fmt.Errorf("load native credentials: %w", err)
	}

	normalized := normalizeRegistryHost(host)

	if s.secretsStore != nil {
		if err := s.secretsStore.Set(context.Background(), nativeRegPasswordKeyPrefix+normalized, []byte(password)); err != nil {
			return fmt.Errorf("store encrypted password for %s: %w", host, err)
		}
		// Do not write plaintext password to JSON when encryption is available.
		creds.Registries[normalized] = NativeCredential{
			Username:          username,
			ContainerAuthFile: containerAuthFile,
		}
	} else {
		// Fallback: plaintext JSON (secrets store unavailable in this environment).
		creds.Registries[normalized] = NativeCredential{
			Username:          username,
			Password:          password,
			ContainerAuthFile: containerAuthFile,
		}
	}

	return s.save(creds)
}

// DeleteCredential removes the credential for the given registry host.
// The host is normalized so that Docker Hub variants resolve to the canonical key.
func (s *NativeCredentialStore) DeleteCredential(host string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	creds, err := s.load()
	if err != nil {
		return fmt.Errorf("load native credentials: %w", err)
	}

	normalized := normalizeRegistryHost(host)
	if _, ok := creds.Registries[normalized]; !ok {
		return nil
	}

	if s.secretsStore != nil {
		_ = s.secretsStore.Delete(context.Background(), nativeRegPasswordKeyPrefix+normalized)
	}
	delete(creds.Registries, normalized)

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

// ListCredentialEntries returns all stored registry hosts and their full credential entries.
// When a secrets store is available, passwords are retrieved from encrypted storage.
func (s *NativeCredentialStore) ListCredentialEntries() (map[string]NativeCredential, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	creds, err := s.load()
	if err != nil {
		return nil, fmt.Errorf("load native credentials: %w", err)
	}

	result := make(map[string]NativeCredential, len(creds.Registries))
	for host, cred := range creds.Registries {
		if s.secretsStore != nil {
			passBytes, secretErr := s.secretsStore.Get(context.Background(), nativeRegPasswordKeyPrefix+host)
			if secretErr == nil {
				cred.Password = string(passBytes)
			}
			// Ignore ErrNotFound — plaintext JSON password (if any) is used as-is.
		}
		result[host] = cred
	}

	return result, nil
}

// DeleteAll removes all stored credentials.
func (s *NativeCredentialStore) DeleteAll() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Remove encrypted passwords first before clearing the index.
	if s.secretsStore != nil {
		existing, err := s.load()
		if err == nil {
			for host := range existing.Registries {
				_ = s.secretsStore.Delete(context.Background(), nativeRegPasswordKeyPrefix+host)
			}
		}
	}

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
// Returns the resolved file path on success so callers can persist it.
// This is a best-effort operation; callers should treat errors as warnings.
func (s *NativeCredentialStore) WriteContainerAuth(host, username, password string) (string, error) {
	authFile, err := detectContainerAuthFile()
	if err != nil {
		return "", fmt.Errorf("detect container auth file: %w", err)
	}

	if err := writeContainerAuthEntry(authFile, host, username, password); err != nil {
		return "", err
	}
	return authFile, nil
}

// DeleteContainerAuth removes a credential from the container auth file.
// It uses the path stored in the credential entry so deletion always targets
// the same file that was written during login.
// This is a best-effort operation.
func (s *NativeCredentialStore) DeleteContainerAuth(host string) error {
	cred, err := s.GetCredential(host)
	if err != nil || cred == nil {
		return err
	}

	filePath := cred.ContainerAuthFile
	if filePath == "" {
		// Legacy entry: file path not persisted; fall back to detection.
		filePath, err = detectContainerAuthFile()
		if err != nil {
			return fmt.Errorf("detect container auth file: %w", err)
		}
	}

	return deleteContainerAuthEntry(filePath, host)
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
// Writes are atomic via a temp file + rename to avoid partial-write corruption.
// Permissions are explicitly set after rename to handle pre-existing files
// that may have been created with broader permissions.
func (s *NativeCredentialStore) save(creds *nativeCredentialFile) error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, nativeCredentialDirPermissions); err != nil {
		return fmt.Errorf("create credential directory %s: %w", dir, err)
	}

	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal credentials: %w", err)
	}

	tmpFile, err := os.CreateTemp(dir, nativeCredentialFileName+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary credential file in %s: %w", dir, err)
	}
	tmpPath := tmpFile.Name()
	removeTmp := true
	defer func() {
		if removeTmp {
			_ = os.Remove(tmpPath)
		}
	}()

	if err := tmpFile.Chmod(nativeCredentialFilePermissions); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("set permissions on temporary credential file %s: %w", tmpPath, err)
	}

	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("write temporary credential file %s: %w", tmpPath, err)
	}

	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("sync temporary credential file %s: %w", tmpPath, err)
	}

	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close temporary credential file %s: %w", tmpPath, err)
	}

	if err := os.Rename(tmpPath, s.path); err != nil { //nolint:gosec // tmpPath is created by os.CreateTemp in the same directory as s.path (XDG config), not user-controlled
		return fmt.Errorf("rename temporary credential file %s to %s: %w", tmpPath, s.path, err)
	}
	removeTmp = false

	// Enforce correct permissions even if the file pre-existed with looser perms.
	if err := os.Chmod(s.path, nativeCredentialFilePermissions); err != nil {
		return fmt.Errorf("set permissions on credential file %s: %w", s.path, err)
	}

	return nil
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
// It uses raw JSON maps to preserve all existing top-level fields (e.g., credsStore).
// Returns an error if the existing file exists but cannot be parsed.
func writeContainerAuthEntry(filePath, host, username, password string) error {
	// Use raw maps to preserve unrelated top-level fields (e.g. credsStore, credHelpers).
	raw := make(map[string]json.RawMessage)
	auths := make(map[string]json.RawMessage)

	data, err := os.ReadFile(filePath)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("read container auth file %s: %w", filePath, err)
		}
	} else {
		if err := json.Unmarshal(data, &raw); err != nil {
			return fmt.Errorf("parse container auth file %s: %w", filePath, err)
		}
		if authsRaw, ok := raw["auths"]; ok {
			if err := json.Unmarshal(authsRaw, &auths); err != nil {
				return fmt.Errorf("parse auths in container auth file %s: %w", filePath, err)
			}
		}
	}

	entryData, err := json.Marshal(containerAuthEntry{Auth: encodeBasicAuth(username, password)})
	if err != nil {
		return fmt.Errorf("marshal container auth entry for host %s: %w", host, err)
	}
	auths[host] = entryData

	updatedAuths, err := json.Marshal(auths)
	if err != nil {
		return fmt.Errorf("marshal updated auths: %w", err)
	}
	raw["auths"] = updatedAuths

	updated, err := json.MarshalIndent(raw, "", "\t")
	if err != nil {
		return fmt.Errorf("marshal updated container auth file: %w", err)
	}

	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, containerAuthDirPermissions); err != nil {
		return fmt.Errorf("create container auth directory %s: %w", dir, err)
	}

	// Atomic write: temp file + fsync + rename + post-rename chmod.
	tmpFile, err := os.CreateTemp(dir, filepath.Base(filePath)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp container auth file in %s: %w", dir, err)
	}
	tmpPath := tmpFile.Name()
	removeTmp := true
	defer func() {
		if removeTmp {
			_ = os.Remove(tmpPath)
		}
	}()

	if err := tmpFile.Chmod(containerAuthFilePermissions); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("chmod temp container auth file %s: %w", tmpPath, err)
	}

	if _, err := tmpFile.Write(updated); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("write temp container auth file %s: %w", tmpPath, err)
	}

	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("sync temp container auth file %s: %w", tmpPath, err)
	}

	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close temp container auth file %s: %w", tmpPath, err)
	}

	if err := os.Rename(tmpPath, filePath); err != nil { //nolint:gosec // tmpPath from os.CreateTemp in same dir as filePath
		return fmt.Errorf("rename container auth file %s to %s: %w", tmpPath, filePath, err)
	}
	removeTmp = false

	// Enforce correct permissions on the final file (handles pre-existing loose-permission files).
	if err := os.Chmod(filePath, containerAuthFilePermissions); err != nil {
		return fmt.Errorf("chmod container auth file %s: %w", filePath, err)
	}

	return nil
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

	// Atomic write: temp file + fsync + rename + post-rename chmod.
	dir := filepath.Dir(filePath)
	tmpFile, err := os.CreateTemp(dir, filepath.Base(filePath)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp container auth file in %s: %w", dir, err)
	}
	tmpPath := tmpFile.Name()
	removeTmp := true
	defer func() {
		if removeTmp {
			_ = os.Remove(tmpPath)
		}
	}()

	if err := tmpFile.Chmod(containerAuthFilePermissions); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("chmod temp container auth file %s: %w", tmpPath, err)
	}

	if _, err := tmpFile.Write(updatedData); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("write temp container auth file %s: %w", tmpPath, err)
	}

	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("sync temp container auth file %s: %w", tmpPath, err)
	}

	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close temp container auth file %s: %w", tmpPath, err)
	}

	if err := os.Rename(tmpPath, filePath); err != nil { //nolint:gosec // tmpPath from os.CreateTemp in same dir as filePath
		return fmt.Errorf("rename container auth file %s to %s: %w", tmpPath, filePath, err)
	}
	removeTmp = false

	// Enforce correct permissions on the final file.
	if err := os.Chmod(filePath, containerAuthFilePermissions); err != nil {
		return fmt.Errorf("chmod container auth file %s: %w", filePath, err)
	}

	return nil
}

// encodeBasicAuth creates a base64-encoded auth string for Docker config.
func encodeBasicAuth(username, password string) string {
	auth := username + ":" + password
	return base64.StdEncoding.EncodeToString([]byte(auth))
}
