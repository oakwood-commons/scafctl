// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package credentialhelper implements the Docker credential helper protocol,
// exposing scafctl's encrypted credential store to Docker, Podman, Buildah,
// and any OCI client. See https://github.com/docker/docker-credential-helpers.
package credentialhelper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/secrets"
)

const (
	// keyPrefix namespaces credential helper entries in the secrets store,
	// separate from auth handler tokens and other scafctl secrets.
	keyPrefix = "credhelper:" //nolint:gosec // namespace prefix, not a credential

	// MaxInputSize caps stdin input to prevent abuse.
	MaxInputSize = 1 << 20 // 1 MiB
)

// Credential represents a Docker credential helper credential.
type Credential struct {
	ServerURL string `json:"ServerURL" yaml:"ServerURL" doc:"Registry server URL"`
	Username  string `json:"Username" yaml:"Username" doc:"Username for the registry"`
	Secret    string `json:"Secret" yaml:"Secret" doc:"Password or token for the registry"` //nolint:gosec // Required by Docker credential helper protocol
}

// ErrorResponse is the Docker credential helper error format.
type ErrorResponse struct {
	Message string `json:"message" yaml:"message" doc:"Error description"`
}

// Helper implements the Docker credential helper protocol operations.
type Helper struct {
	store       secrets.Store
	nativeStore *catalog.NativeCredentialStore
}

// Option configures the Helper.
type Option func(*Helper)

// WithNativeStore sets the native credential store for fallback lookups on Get.
func WithNativeStore(ns *catalog.NativeCredentialStore) Option {
	return func(h *Helper) { h.nativeStore = ns }
}

// New creates a new credential helper with the given secrets store.
func New(store secrets.Store, opts ...Option) *Helper {
	h := &Helper{store: store}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// Get retrieves credentials for a registry server URL.
// It first checks the credhelper: namespace, then falls back to the native
// credential store if configured.
func (h *Helper) Get(ctx context.Context, serverURL string) (*Credential, error) {
	serverURL = strings.TrimSpace(serverURL)
	if serverURL == "" {
		return nil, fmt.Errorf("credentials not found")
	}

	// Check credhelper: namespace first
	data, err := h.store.Get(ctx, keyPrefix+serverURL)
	if err == nil && len(data) > 0 {
		var cred Credential
		if jsonErr := json.Unmarshal(data, &cred); jsonErr == nil {
			cred.ServerURL = serverURL
			return &cred, nil
		}
	}

	// Fallback to native credential store.
	// Pass serverURL directly; normalizeRegistryHost inside GetCredential handles
	// Docker Hub variants (including https://index.docker.io/v1/) correctly.
	if h.nativeStore != nil {
		native, nativeErr := h.nativeStore.GetCredential(serverURL)
		if nativeErr == nil && native != nil {
			return &Credential{
				ServerURL: serverURL,
				Username:  native.Username,
				Secret:    native.Password,
			}, nil
		}
	}

	return nil, fmt.Errorf("credentials not found")
}

// Store saves credentials for a registry server URL.
func (h *Helper) Store(ctx context.Context, cred *Credential) error {
	if cred.ServerURL == "" {
		return fmt.Errorf("ServerURL is required")
	}
	data, err := json.Marshal(cred) //nolint:gosec // G117: required by Docker credential helper protocol
	if err != nil {
		return fmt.Errorf("marshal credential: %w", err)
	}
	return h.store.Set(ctx, keyPrefix+cred.ServerURL, data)
}

// Erase removes credentials for a registry server URL.
// Returns nil even if the credential doesn't exist.
func (h *Helper) Erase(ctx context.Context, serverURL string) error {
	serverURL = strings.TrimSpace(serverURL)
	if serverURL == "" {
		return nil
	}
	err := h.store.Delete(ctx, keyPrefix+serverURL)
	if err != nil && errors.Is(err, secrets.ErrNotFound) {
		return nil // no-op per Docker credential helper spec
	}
	return err
}

// List returns all credentials stored in the credhelper: namespace.
// The returned map keys are server URLs and values are usernames.
func (h *Helper) List(ctx context.Context) (map[string]string, error) {
	names, err := h.store.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list secrets: %w", err)
	}
	result := make(map[string]string)
	for _, name := range names {
		if !strings.HasPrefix(name, keyPrefix) {
			continue
		}
		serverURL := strings.TrimPrefix(name, keyPrefix)
		data, getErr := h.store.Get(ctx, name)
		if getErr != nil {
			continue
		}
		var cred Credential
		if jsonErr := json.Unmarshal(data, &cred); jsonErr != nil {
			continue
		}
		result[serverURL] = cred.Username
	}
	return result, nil
}
