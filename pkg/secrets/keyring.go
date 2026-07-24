// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package secrets

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/paths"
	"github.com/zalando/go-keyring"
)

const (
	// masterKeyFileName is the name of the file-based master key file.
	masterKeyFileName = "master.key"

	// KeyringBackendOS indicates the OS keyring was used.
	KeyringBackendOS = "os"

	// KeyringBackendEnv indicates the environment variable keyring was used.
	KeyringBackendEnv = "env"

	// KeyringBackendFile indicates the file-based keyring was used.
	KeyringBackendFile = "file"
)

const (
	// KeyringService is the service name used in the OS keyring.
	KeyringService = "scafctl"

	// KeyringMasterKeyAccount is the account name for the master encryption key.
	KeyringMasterKeyAccount = "master-key"

	// EnvSecretKey is the environment variable name for the fallback secret key.
	EnvSecretKey = "SCAFCTL_SECRET_KEY" //nolint:gosec // This is the env var name, not a credential
)

// ErrKeyNotFound is returned when a key is not found in the keyring.
var ErrKeyNotFound = errors.New("key not found in keyring")

// ErrKeyringUnavailable is returned when a keyring backend's underlying
// provider is entirely unavailable (as opposed to the key merely being
// absent). On Linux this is the common headless/WSL/CI case where the
// freedesktop Secret Service (org.freedesktop.secrets) is not running, so
// go-keyring returns a raw D-Bus "not provided by any .service files" error.
// Callers treat this as "this backend cannot serve requests" and fall through
// to the next backend in the chain, rather than failing hard.
var ErrKeyringUnavailable = errors.New("keyring provider unavailable")

// ErrKeyringReadOnly is returned by a backend that structurally cannot persist
// values (e.g. the environment-variable keyring, which reads SCAFCTL_SECRET_KEY
// but cannot write it back). chainKeyring.Set treats this as "skip this backend
// and try the next writable one" rather than a genuine hard failure.
var ErrKeyringReadOnly = errors.New("keyring backend is read-only")

// osKeyringUnavailableMarkers are substrings that identify errors from
// go-keyring/godbus indicating the OS Secret Service is not reachable at all.
// go-keyring does not export a typed sentinel for these D-Bus conditions, so we
// match on the stable D-Bus name/text it surfaces.
//
// We deliberately match only *service-unreachable* phrasings -- D-Bus activation
// failure and session-bus connection failure -- and NOT permission/authorization
// errors. A permission-denied error (service present but access blocked, e.g.
// D-Bus "AccessDenied"/"not authorized") is a genuine hard failure and must NOT
// be misclassified as "unavailable" and silently downgraded to the file backend.
// The bare service name "org.freedesktop.secrets" is likewise excluded because it
// appears in both activation-failure and permission-denied messages.
var osKeyringUnavailableMarkers = []string{
	"was not provided by any .service",         // D-Bus activation failure (no Secret Service)
	"dial unix",                                // session bus socket unreachable (no D-Bus session)
	"connection refused",                       // session bus present but refusing connections
	"The name org.freedesktop.secrets was not", // explicit activation-failure variant
}

// isOSKeyringUnavailable reports whether err indicates the OS keyring provider
// is unavailable (missing Secret Service, unsupported platform) rather than the
// requested key simply being absent.
func isOSKeyringUnavailable(err error) bool {
	if err == nil {
		return false
	}
	// go-keyring exports this sentinel when the platform has no keyring at all.
	if errors.Is(err, keyring.ErrUnsupportedPlatform) {
		return true
	}
	msg := err.Error()
	for _, marker := range osKeyringUnavailableMarkers {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

// OSKeyring implements Keyring using the OS keychain via go-keyring.
type OSKeyring struct{}

// NewOSKeyring creates a new OS keyring wrapper.
func NewOSKeyring() *OSKeyring {
	return &OSKeyring{}
}

// Get retrieves a value from the OS keyring.
//
// An unavailable Secret Service (headless/WSL/CI Linux) is reported as
// ErrKeyringUnavailable so that chainKeyring.Get can distinguish "this backend
// cannot serve, skip it" from a genuine hard access error (e.g. permission
// denied). A genuine hard error must NOT be masked as key-not-found, since a
// spurious not-found on read can trigger destructive fallback key generation
// and orphan cleanup when encrypted secrets already exist.
func (k *OSKeyring) Get(service, account string) (string, error) {
	value, err := keyring.Get(service, account)
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return "", ErrKeyNotFound
		}
		if isOSKeyringUnavailable(err) {
			return "", fmt.Errorf("%w: %w", ErrKeyringUnavailable, err)
		}
		return "", NewKeyringError("get", err)
	}
	return value, nil
}

// Set stores a value in the OS keyring.
func (k *OSKeyring) Set(service, account, value string) error {
	if err := keyring.Set(service, account, value); err != nil {
		// The Secret Service is unavailable: report a distinct sentinel so the
		// chain can fall through to a writable backend (env/file) rather than
		// aborting the write.
		if isOSKeyringUnavailable(err) {
			return fmt.Errorf("%w: %w", ErrKeyringUnavailable, err)
		}
		return NewKeyringError("set", err)
	}
	return nil
}

// Delete removes a value from the OS keyring.
func (k *OSKeyring) Delete(service, account string) error {
	if err := keyring.Delete(service, account); err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return nil // Idempotent delete
		}
		return NewKeyringError("delete", err)
	}
	return nil
}

// envKeyring implements Keyring using environment variables.
// This is a fallback for environments where the OS keyring is not available.
// It only supports reading the master key from SCAFCTL_SECRET_KEY.
type envKeyring struct {
	envKey string
}

// newEnvKeyring creates a new environment variable keyring.
// envKey is the name of the environment variable to read from.
func newEnvKeyring(envKey string) *envKeyring {
	return &envKeyring{envKey: envKey}
}

// Get retrieves the secret key from the environment variable.
// Only supports the master key account; other accounts return ErrKeyNotFound.
func (k *envKeyring) Get(service, account string) (string, error) {
	// Only support the master key
	if service != KeyringService || account != KeyringMasterKeyAccount {
		return "", ErrKeyNotFound
	}

	value := os.Getenv(k.envKey)
	if value == "" {
		return "", ErrKeyNotFound
	}

	return value, nil
}

// Set is a no-op for envKeyring since we can't modify environment variables persistently.
// Returns ErrKeyringReadOnly so the chain skips to the next writable backend.
func (k *envKeyring) Set(_, _, _ string) error {
	return fmt.Errorf("%w: cannot set values in environment keyring", ErrKeyringReadOnly)
}

// Delete is a no-op for envKeyring.
// Returns ErrKeyringReadOnly so the chain skips to the next writable backend.
func (k *envKeyring) Delete(_, _ string) error {
	return fmt.Errorf("%w: cannot delete values in environment keyring", ErrKeyringReadOnly)
}

// fileKeyring implements Keyring using a file-based key store.
// This is a last-resort fallback for environments where neither the OS keyring
// nor the environment variable is available. The master key is stored in a file
// at the XDG data directory with 0600 permissions.
type fileKeyring struct {
	dir string // directory where the key file is stored
}

// newFileKeyring creates a new file-based keyring.
// dir is the directory under which the master key file will be stored.
func newFileKeyring(dir string) *fileKeyring {
	return &fileKeyring{dir: dir}
}

// Get retrieves the secret key from a file.
// Only supports the master key account; other accounts return ErrKeyNotFound.
func (k *fileKeyring) Get(service, account string) (string, error) {
	if service != KeyringService || account != KeyringMasterKeyAccount {
		return "", ErrKeyNotFound
	}

	data, err := os.ReadFile(k.keyFilePath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", ErrKeyNotFound
		}
		return "", NewKeyringError("get", fmt.Errorf("reading key file: %w", err))
	}

	value := string(data)
	if value == "" {
		return "", ErrKeyNotFound
	}

	return value, nil
}

// Set stores a value in a key file with restricted permissions.
// Uses atomic write (temp file + rename) to prevent corruption.
func (k *fileKeyring) Set(service, account, value string) error {
	if service != KeyringService || account != KeyringMasterKeyAccount {
		return NewKeyringError("set", errors.New("file keyring only supports master key"))
	}

	// Ensure directory exists with 0700 permissions
	if err := os.MkdirAll(k.dir, 0o700); err != nil {
		return NewKeyringError("set", fmt.Errorf("creating key directory: %w", err))
	}

	// Write to a temp file first, then rename for atomicity
	targetPath := k.keyFilePath()
	tmpFile, err := os.CreateTemp(k.dir, ".master-key-*")
	if err != nil {
		return NewKeyringError("set", fmt.Errorf("creating temp file: %w", err))
	}
	tmpPath := tmpFile.Name()

	// Ensure cleanup on error
	success := false
	defer func() {
		if !success {
			os.Remove(tmpPath)
		}
	}()

	// Set restrictive permissions before writing
	if err := tmpFile.Chmod(0o600); err != nil {
		tmpFile.Close()
		return NewKeyringError("set", fmt.Errorf("setting file permissions: %w", err))
	}

	if _, err := tmpFile.WriteString(value); err != nil {
		tmpFile.Close()
		return NewKeyringError("set", fmt.Errorf("writing key file: %w", err))
	}

	if err := tmpFile.Close(); err != nil {
		return NewKeyringError("set", fmt.Errorf("closing temp file: %w", err))
	}

	// Atomic rename
	if err := os.Rename(tmpPath, targetPath); err != nil { //nolint:gosec // G703: paths are within managed keyring directory
		return NewKeyringError("set", fmt.Errorf("renaming key file: %w", err))
	}

	success = true
	return nil
}

// Delete removes the key file.
func (k *fileKeyring) Delete(service, account string) error {
	if service != KeyringService || account != KeyringMasterKeyAccount {
		return nil // Idempotent for unsupported accounts
	}

	err := os.Remove(k.keyFilePath())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return NewKeyringError("delete", fmt.Errorf("removing key file: %w", err))
	}
	return nil
}

// keyFilePath returns the full path to the master key file.
func (k *fileKeyring) keyFilePath() string {
	return filepath.Join(k.dir, masterKeyFileName)
}

// keyringEntry pairs a keyring with a backend identifier.
type keyringEntry struct {
	keyring Keyring
	backend string
}

// chainKeyring implements Keyring by trying multiple keyrings in order.
// The first keyring that successfully returns a value wins.
// Set/Delete operations are attempted on the first keyring (primary).
type chainKeyring struct {
	entries []keyringEntry
	// resolvedBackend is set after a successful Get, indicating which backend was used.
	resolvedBackend string
}

// newChainKeyring creates a keyring that tries entries in order.
func newChainKeyring(entries ...keyringEntry) *chainKeyring {
	return &chainKeyring{entries: entries}
}

// Get tries each keyring in order and returns the first successful result.
//
// Error precedence when no backend has a value:
//  1. A genuine hard access error (e.g. permission denied) from any backend
//     wins -- it must surface, never be masked as key-not-found, because a
//     spurious not-found triggers destructive fallback key generation and
//     orphan cleanup when encrypted secrets already exist.
//  2. Otherwise, a clean key-not-found (including from a backend that is merely
//     unavailable, such as a missing OS Secret Service) is returned, which is
//     what lets first-run key generation proceed on headless/WSL/CI systems.
func (k *chainKeyring) Get(service, account string) (string, error) {
	var firstHardErr error
	sawNotFound := false
	for _, entry := range k.entries {
		value, err := entry.keyring.Get(service, account)
		if err == nil {
			k.resolvedBackend = entry.backend
			return value, nil
		}
		if errors.Is(err, ErrKeyNotFound) {
			sawNotFound = true
			continue
		}
		// An unavailable backend (e.g. missing OS Secret Service) is skipped so
		// the chain falls through to env/file. Any other error is a genuine
		// hard failure that must not be masked by a downstream not-found.
		if errors.Is(err, ErrKeyringUnavailable) {
			continue
		}
		if firstHardErr == nil {
			firstHardErr = err
		}
	}
	// A genuine hard error takes precedence over a downstream not-found so a
	// real access failure is never silently downgraded to first-run key-gen.
	if firstHardErr != nil {
		return "", firstHardErr
	}
	if sawNotFound {
		return "", ErrKeyNotFound
	}
	return "", ErrKeyNotFound
}

// Set stores a value in the first keyring that accepts it.
// A backend that structurally cannot store the value -- an unavailable provider
// (ErrKeyringUnavailable, e.g. a missing OS Secret Service) or a read-only
// backend (ErrKeyringReadOnly, e.g. the environment) -- is skipped in favor of
// the next writable one. This is what lets the file backend persist a freshly
// generated master key when the OS keyring is absent.
//
// Any OTHER error (e.g. permission denied, disk full, corruption) is a genuine
// hard failure: it is returned immediately rather than silently downgrading to
// a less-secure backend, so a real write failure on a healthy OS keyring is
// never masked.
func (k *chainKeyring) Set(service, account, value string) error {
	var skipErrs []error
	for _, entry := range k.entries {
		err := entry.keyring.Set(service, account, value)
		if err == nil {
			k.resolvedBackend = entry.backend
			return nil
		}
		if errors.Is(err, ErrKeyringUnavailable) || errors.Is(err, ErrKeyringReadOnly) {
			skipErrs = append(skipErrs, err)
			continue
		}
		// Genuine hard failure on a backend that is present and writable: do
		// not silently fall through to a less-secure backend.
		return err
	}
	if len(skipErrs) > 0 {
		// Every backend was unavailable or read-only. Join their errors so the
		// upstream (OS) cause is preserved alongside the env/file errors.
		return errors.Join(skipErrs...)
	}
	return NewKeyringError("set", errors.New("no keyrings configured"))
}

// Delete removes a value from keyrings. Tries all keyrings, ignoring errors.
func (k *chainKeyring) Delete(service, account string) error {
	for _, entry := range k.entries {
		_ = entry.keyring.Delete(service, account)
	}
	return nil
}

// Backend returns the backend identifier for the keyring that last satisfied a Get.
func (k *chainKeyring) Backend() string {
	return k.resolvedBackend
}

// NewDefaultKeyring creates the default keyring with the following fallback order:
//  1. OS keyring (most secure - uses OS keychain)
//  2. Environment variable SCAFCTL_SECRET_KEY (explicit user intent, e.g. CI)
//  3. File-based key storage (auto-fallback, less secure but "just works")
func NewDefaultKeyring() Keyring {
	return newChainKeyring(
		keyringEntry{keyring: NewOSKeyring(), backend: KeyringBackendOS},
		keyringEntry{keyring: newEnvKeyring(EnvSecretKey), backend: KeyringBackendEnv},
		keyringEntry{keyring: newFileKeyring(paths.DataDir()), backend: KeyringBackendFile},
	)
}

// GetMasterKeyFromKeyring retrieves the master encryption key from the keyring.
// The key is stored as base64-encoded string in the keyring.
func GetMasterKeyFromKeyring(kr Keyring) ([]byte, error) {
	encoded, err := kr.Get(KeyringService, KeyringMasterKeyAccount)
	if err != nil {
		return nil, err
	}

	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("failed to decode master key: %w", err)
	}

	if len(key) != masterKeySize {
		return nil, fmt.Errorf("invalid master key size: expected %d bytes, got %d", masterKeySize, len(key))
	}

	return key, nil
}

// SetMasterKeyInKeyring stores the master encryption key in the keyring.
// The key is stored as base64-encoded string.
func SetMasterKeyInKeyring(kr Keyring, key []byte) error {
	if len(key) != masterKeySize {
		return fmt.Errorf("invalid master key size: expected %d bytes, got %d", masterKeySize, len(key))
	}

	encoded := base64.StdEncoding.EncodeToString(key)
	return kr.Set(KeyringService, KeyringMasterKeyAccount, encoded)
}

// DeleteMasterKeyFromKeyring removes the master encryption key from the keyring.
func DeleteMasterKeyFromKeyring(kr Keyring) error {
	return kr.Delete(KeyringService, KeyringMasterKeyAccount)
}
