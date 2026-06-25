// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package execcredential builds Kubernetes client-go ExecCredential payloads
// from scafctl auth tokens. It deliberately hand-rolls the tiny JSON schema so
// that scafctl core never imports client-go.
//
// The ExecCredential is the response format of a kubectl/oc exec credential
// plugin: kubectl invokes the plugin, the plugin prints this JSON to stdout,
// and kubectl reads status.token as the bearer token for the API server.
// See https://kubernetes.io/docs/reference/access-authn-authz/authentication/#client-go-credential-plugins
package execcredential

import (
	"encoding/json"
	"strings"
	"time"
)

const (
	// Kind is the ExecCredential resource kind.
	Kind = "ExecCredential"

	// DefaultAPIVersion is the client.authentication.k8s.io API version used
	// when kubectl does not advertise one via KUBERNETES_EXEC_INFO.
	DefaultAPIVersion = "client.authentication.k8s.io/v1"

	// ExecInfoEnv is the environment variable kubectl sets when it invokes an
	// exec credential plugin. Its presence signals that exec-credential output
	// is expected even without an explicit flag.
	ExecInfoEnv = "KUBERNETES_EXEC_INFO"

	// apiVersionPrefix guards against echoing an unrelated apiVersion parsed
	// from the exec info payload.
	apiVersionPrefix = "client.authentication.k8s.io/"
)

// ExecCredential is the client-go credential plugin response envelope.
type ExecCredential struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Status     Status `json:"status"`
}

// Status carries the bearer token and its optional expiry.
type Status struct {
	Token               string `json:"token"`
	ExpirationTimestamp string `json:"expirationTimestamp,omitempty"`
}

// NewWithAPIVersion builds an ExecCredential with an explicit API version.
// An empty apiVersion falls back to DefaultAPIVersion.
func NewWithAPIVersion(apiVersion, token string, expiresAt time.Time) ExecCredential {
	if apiVersion == "" {
		apiVersion = DefaultAPIVersion
	}
	ec := ExecCredential{
		APIVersion: apiVersion,
		Kind:       Kind,
		Status:     Status{Token: token},
	}
	if !expiresAt.IsZero() {
		ec.Status.ExpirationTimestamp = expiresAt.UTC().Format(time.RFC3339)
	}
	return ec
}

// JSON marshals the ExecCredential to its on-the-wire JSON form.
func (e ExecCredential) JSON() ([]byte, error) {
	return json.Marshal(e)
}

// APIVersionFromExecInfo extracts the client.authentication.k8s.io API version
// that kubectl requested from a KUBERNETES_EXEC_INFO payload. It returns
// DefaultAPIVersion when the payload is empty, unparseable, or advertises an
// apiVersion outside the client.authentication.k8s.io group. Echoing the
// requested version keeps the plugin compatible with both v1 and v1beta1
// clusters without importing client-go.
func APIVersionFromExecInfo(execInfo string) string {
	if execInfo == "" {
		return DefaultAPIVersion
	}
	var parsed struct {
		APIVersion string `json:"apiVersion"`
	}
	if err := json.Unmarshal([]byte(execInfo), &parsed); err != nil {
		return DefaultAPIVersion
	}
	if !strings.HasPrefix(parsed.APIVersion, apiVersionPrefix) {
		return DefaultAPIVersion
	}
	return parsed.APIVersion
}
