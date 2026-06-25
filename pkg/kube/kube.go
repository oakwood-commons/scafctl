// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package kube provides dependency-free primitives for resolving Kubernetes /
// OpenShift cluster connection details by name. It carries no client-go
// dependency: it only defines the ClusterInfo shape, the ClusterResolver
// extension point, and context plumbing so embedders can supply a cluster
// registry. The heavier kubeconfig machinery (client-go/clientcmd) lives in a
// separate provider plugin.
package kube

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Sentinel errors returned by ClusterInfo.Validate so callers can match with
// errors.Is rather than string comparison.
var (
	// ErrEmptyClusterName indicates a ClusterInfo with no Name.
	ErrEmptyClusterName = errors.New("cluster name must not be empty")

	// ErrInvalidAuthType indicates a ClusterInfo with an unrecognized AuthType.
	ErrInvalidAuthType = errors.New("invalid auth type")
)

// AuthType identifies how a client authenticates to a cluster's API server.
// The empty value means "auto-detect" so embedders can leave it unset.
type AuthType string

const (
	// AuthTypeAuto leaves the authentication method to runtime auto-detection.
	AuthTypeAuto AuthType = ""

	// AuthTypeOAuth selects an OAuth login flow (e.g. the OpenShift bundled
	// OAuth server's implicit grant).
	AuthTypeOAuth AuthType = "oauth"

	// AuthTypeOIDC selects an external OIDC identity provider flow.
	AuthTypeOIDC AuthType = "oidc"
)

// Valid reports whether the AuthType is one of the recognized values.
func (a AuthType) Valid() bool {
	switch a {
	case AuthTypeAuto, AuthTypeOAuth, AuthTypeOIDC:
		return true
	default:
		return false
	}
}

// ClusterInfo describes the connection and authentication details for a single
// Kubernetes / OpenShift cluster. It is pure data with no client-go types so it
// can live in core. Tags are provided so embedders can unmarshal a cluster
// registry from YAML/JSON config.
type ClusterInfo struct {
	// Name is the cluster's logical name used for lookup and completion.
	Name string `json:"name" yaml:"name" doc:"Cluster logical name used for lookup and completion" maxLength:"253" example:"prod"`

	// APIServerURL is the cluster's API server endpoint (https://...). It may
	// be empty when the caller supplies an explicit --server or relies on
	// auto-detection.
	APIServerURL string `json:"apiServerURL,omitempty" yaml:"apiServerURL,omitempty" doc:"Cluster API server endpoint; may be empty for --server or auto-detection" maxLength:"2048" example:"https://api.example.com:6443"`

	// ConsoleURL is the optional web console URL (informational).
	ConsoleURL string `json:"consoleURL,omitempty" yaml:"consoleURL,omitempty" doc:"Optional web console URL (informational)" maxLength:"2048" example:"https://console.example.com"`

	// AuthType selects the authentication method. The zero value
	// (AuthTypeAuto) means auto-detect.
	AuthType AuthType `json:"authType,omitempty" yaml:"authType,omitempty" doc:"Authentication method; empty means auto-detect" example:"oidc"`

	// OIDCAudience is the client ID / audience the minted token must target
	// for OIDC clusters. Ignored for non-OIDC clusters.
	OIDCAudience string `json:"oidcAudience,omitempty" yaml:"oidcAudience,omitempty" doc:"Client ID/audience the minted token must target for OIDC clusters" maxLength:"512" example:"my-cluster-client-id"`

	// InsecureSkipTLS disables API server TLS verification. Use only for
	// development clusters with self-signed certificates.
	InsecureSkipTLS bool `json:"insecureSkipTLS,omitempty" yaml:"insecureSkipTLS,omitempty" doc:"Disable API server TLS verification (development only)"`
}

// Validate checks the ClusterInfo for required fields and a recognized
// AuthType. APIServerURL is intentionally optional because login can resolve it
// via an explicit --server flag or auto-detection.
func (c ClusterInfo) Validate() error {
	if strings.TrimSpace(c.Name) == "" {
		return ErrEmptyClusterName
	}
	if !c.AuthType.Valid() {
		return fmt.Errorf("%w: %q", ErrInvalidAuthType, c.AuthType)
	}
	return nil
}

// ClusterResolver resolves cluster names to connection details. Embedders with a
// cluster registry provide the implementation; scafctl ships no cluster data.
// When no resolver is configured, commands fall back to explicit --server flags
// and auto-detection.
type ClusterResolver interface {
	// Resolve returns the ClusterInfo for the named cluster.
	Resolve(ctx context.Context, name string) (*ClusterInfo, error)

	// List returns all known clusters. It powers shell completion of cluster
	// names.
	List(ctx context.Context) ([]ClusterInfo, error)
}
