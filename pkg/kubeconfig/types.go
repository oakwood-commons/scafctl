// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package kubeconfig provides the host-side manager that drives the external
// kubeconfig provider plugin. The provider carries all client-go/clientcmd work
// so core never imports the heavy Kubernetes client packages. The manager
// mirrors pkg/state: it resolves a provider with CapabilityKubeconfig from the
// registry (fetching+registering it on demand), dispatches operations via an
// "operation" input field, and unmarshals typed results from the provider
// output.
//
// The provider is stateless: it receives a token only for the whoami operation
// and never caches it.
package kubeconfig

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/oakwood-commons/scafctl/pkg/kube"
	"github.com/oakwood-commons/scafctl/pkg/provider"
)

// ProviderName is the registry name of the kubeconfig provider.
const ProviderName = "kubeconfig"

// Operation identifiers dispatched on the provider "operation" input field.
const (
	// OperationWrite merges and writes a kubeconfig exec auth entry.
	OperationWrite = "kubeconfig_write"

	// OperationRemove removes a kubeconfig cluster/context/user entry.
	OperationRemove = "kubeconfig_remove"

	// OperationCurrentServer reads the current server URL from a kubeconfig.
	OperationCurrentServer = "current_server"

	// OperationDetectAuthType probes a server to detect oauth vs oidc.
	OperationDetectAuthType = "detect_auth_type"

	// OperationReachable checks whether an API server is reachable.
	OperationReachable = "reachable"

	// OperationWhoami runs a SelfSubjectReview to identify the token's subject.
	OperationWhoami = "whoami"
)

// inputOperation is the input field that selects the operation to perform.
const inputOperation = "operation"

// InteractiveMode values control whether kubectl may run the exec credential
// plugin interactively. They mirror client-go's
// ExecConfig.InteractiveMode (client.authentication.k8s.io). Handlers that can
// refresh silently use Never; handlers that require a re-login on expiry (for
// example implicit-grant OAuth) use IfAvailable so kubectl only prompts when a
// terminal is attached.
const (
	// InteractiveModeNever never runs the plugin interactively.
	InteractiveModeNever = "Never"

	// InteractiveModeIfAvailable runs the plugin interactively only when stdin
	// is a terminal.
	InteractiveModeIfAvailable = "IfAvailable"

	// InteractiveModeAlways always runs the plugin interactively.
	InteractiveModeAlways = "Always"
)

// Sentinel errors returned by the manager.
var (
	// ErrProviderUnavailable indicates the kubeconfig provider could not be
	// resolved, fetched, registered, or spawned. The Phase 3 command falls back
	// to writing a static exec-credential kubeconfig when it sees this error.
	ErrProviderUnavailable = errors.New("kubeconfig: provider unavailable")

	// ErrInvalidOperation indicates the provider returned malformed output that
	// could not be decoded into the expected result type.
	ErrInvalidOperation = errors.New("kubeconfig: invalid operation output")
)

// WriteInput describes a kubeconfig_write operation. The host supplies
// ExecCommand/ExecArgs so the embedder binary name is baked into the kubeconfig
// exec block; the provider never hardcodes a binary name.
type WriteInput struct {
	// Server is the cluster API server URL.
	Server string `json:"server" yaml:"server" doc:"Cluster API server URL" maxLength:"2048" example:"https://api.example.com:6443"`

	// Audience is the OIDC audience/client ID the minted token must target.
	Audience string `json:"audience,omitempty" yaml:"audience,omitempty" doc:"OIDC audience/client ID for the minted token" maxLength:"512"`

	// ClusterName is the kubeconfig cluster entry name.
	ClusterName string `json:"cluster_name" yaml:"cluster_name" doc:"Kubeconfig cluster entry name" maxLength:"253" example:"prod"`

	// ContextName is the kubeconfig context entry name. Empty defaults to the
	// cluster name inside the provider.
	ContextName string `json:"context_name,omitempty" yaml:"context_name,omitempty" doc:"Kubeconfig context entry name" maxLength:"253"`

	// Namespace is the default namespace set on the written context. Empty omits
	// the namespace so the context has no default.
	Namespace string `json:"namespace,omitempty" yaml:"namespace,omitempty" doc:"Default namespace set on the written context" maxLength:"253" example:"prod"`

	// UserName is the kubeconfig user entry name. Empty defaults to the cluster
	// name inside the provider.
	UserName string `json:"user_name,omitempty" yaml:"user_name,omitempty" doc:"Kubeconfig user entry name" maxLength:"253"`

	// KubeconfigPath is the kubeconfig file path. Empty resolves the KUBECONFIG
	// env var or ~/.kube/config inside the provider.
	KubeconfigPath string `json:"kubeconfig_path,omitempty" yaml:"kubeconfig_path,omitempty" doc:"Kubeconfig file path; empty resolves KUBECONFIG or ~/.kube/config" maxLength:"4096"`

	// ExecCommand is the command placed in the kubeconfig exec block (the host
	// binary name).
	ExecCommand string `json:"exec_command" yaml:"exec_command" doc:"Command for the kubeconfig exec block (host binary name)" maxLength:"4096" example:"scafctl"`

	// ExecArgs are the static arguments for the kubeconfig exec block.
	ExecArgs []string `json:"exec_args,omitempty" yaml:"exec_args,omitempty" doc:"Static arguments for the kubeconfig exec block" maxItems:"100"`

	// CAData is the PEM-encoded cluster certificate authority bundle. When set,
	// it is preferred over InsecureSkipTLS so the API server's certificate is
	// verified.
	CAData string `json:"ca_data,omitempty" yaml:"ca_data,omitempty" doc:"PEM-encoded cluster CA bundle; preferred over insecure_skip_tls" maxLength:"1048576"`

	// InteractiveMode controls whether kubectl may run the exec credential
	// plugin interactively (Never, IfAvailable, or Always). Empty defaults to
	// IfAvailable inside the provider.
	InteractiveMode string `json:"interactive_mode,omitempty" yaml:"interactive_mode,omitempty" doc:"Exec plugin interactive mode (Never, IfAvailable, Always)" enum:"Never,IfAvailable,Always" example:"IfAvailable"`

	// InstallHint is the message kubectl shows when the exec command is missing
	// from PATH. Empty omits the hint.
	InstallHint string `json:"install_hint,omitempty" yaml:"install_hint,omitempty" doc:"Message kubectl shows when the exec command is missing from PATH" maxLength:"4096"`

	// ProvideClusterInfo asks kubectl to pass cluster details to the plugin via
	// KUBERNETES_EXEC_INFO. It is false when the server is baked into the exec
	// args.
	ProvideClusterInfo bool `json:"provide_cluster_info,omitempty" yaml:"provide_cluster_info,omitempty" doc:"Pass cluster details to the plugin via KUBERNETES_EXEC_INFO"`

	// InsecureSkipTLS disables API server TLS verification (development only).
	InsecureSkipTLS bool `json:"insecure_skip_tls,omitempty" yaml:"insecure_skip_tls,omitempty" doc:"Disable API server TLS verification (development only)"`

	// SetCurrentContext sets the written context as the current-context.
	SetCurrentContext bool `json:"set_current_context,omitempty" yaml:"set_current_context,omitempty" doc:"Set the written context as current-context"`
}

func (in WriteInput) toInputs() map[string]any {
	return map[string]any{
		"server":               in.Server,
		"audience":             in.Audience,
		"cluster_name":         in.ClusterName,
		"context_name":         in.ContextName,
		"namespace":            in.Namespace,
		"user_name":            in.UserName,
		"kubeconfig_path":      in.KubeconfigPath,
		"exec_command":         in.ExecCommand,
		"exec_args":            in.ExecArgs,
		"ca_data":              in.CAData,
		"interactive_mode":     in.InteractiveMode,
		"install_hint":         in.InstallHint,
		"provide_cluster_info": in.ProvideClusterInfo,
		"insecure_skip_tls":    in.InsecureSkipTLS,
		"set_current_context":  in.SetCurrentContext,
	}
}

// WriteResult is the kubeconfig_write output.
type WriteResult struct {
	// Success reports whether the operation succeeded.
	Success bool `json:"success" yaml:"success" doc:"Whether the operation succeeded"`

	// ContextName is the name of the written (or merged) context.
	ContextName string `json:"context_name" yaml:"context_name" doc:"Name of the written context"`

	// KubeconfigPath is the resolved kubeconfig file path that was written.
	KubeconfigPath string `json:"kubeconfig_path" yaml:"kubeconfig_path" doc:"Resolved kubeconfig file path that was written"`
}

// RemoveInput describes a kubeconfig_remove operation.
type RemoveInput struct {
	// ClusterName is the kubeconfig cluster entry name to remove.
	ClusterName string `json:"cluster_name" yaml:"cluster_name" doc:"Kubeconfig cluster entry name to remove" maxLength:"253"`

	// ContextName is the kubeconfig context entry name to remove.
	ContextName string `json:"context_name,omitempty" yaml:"context_name,omitempty" doc:"Kubeconfig context entry name to remove" maxLength:"253"`

	// UserName is the kubeconfig user entry name to remove.
	UserName string `json:"user_name,omitempty" yaml:"user_name,omitempty" doc:"Kubeconfig user entry name to remove" maxLength:"253"`

	// KubeconfigPath is the kubeconfig file path. Empty resolves the KUBECONFIG
	// env var or ~/.kube/config inside the provider.
	KubeconfigPath string `json:"kubeconfig_path,omitempty" yaml:"kubeconfig_path,omitempty" doc:"Kubeconfig file path; empty resolves KUBECONFIG or ~/.kube/config" maxLength:"4096"`
}

func (in RemoveInput) toInputs() map[string]any {
	return map[string]any{
		"cluster_name":    in.ClusterName,
		"context_name":    in.ContextName,
		"user_name":       in.UserName,
		"kubeconfig_path": in.KubeconfigPath,
	}
}

// RemoveResult is the kubeconfig_remove output.
type RemoveResult struct {
	// Success reports whether the operation succeeded.
	Success bool `json:"success" yaml:"success" doc:"Whether the operation succeeded"`

	// Removed reports whether a matching entry existed and was removed.
	Removed bool `json:"removed" yaml:"removed" doc:"Whether a matching entry was removed"`
}

// CurrentServerInput describes a current_server operation.
type CurrentServerInput struct {
	// KubeconfigPath is the kubeconfig file path. Empty resolves the KUBECONFIG
	// env var or ~/.kube/config inside the provider.
	KubeconfigPath string `json:"kubeconfig_path,omitempty" yaml:"kubeconfig_path,omitempty" doc:"Kubeconfig file path; empty resolves KUBECONFIG or ~/.kube/config" maxLength:"4096"`

	// ContextName is the context to read. Empty uses the current-context.
	ContextName string `json:"context_name,omitempty" yaml:"context_name,omitempty" doc:"Context to read; empty uses the current-context" maxLength:"253"`
}

func (in CurrentServerInput) toInputs() map[string]any {
	return map[string]any{
		"kubeconfig_path": in.KubeconfigPath,
		"context_name":    in.ContextName,
	}
}

// currentServerResult is the current_server output.
type currentServerResult struct {
	Success bool   `json:"success" yaml:"success"`
	Server  string `json:"server" yaml:"server"`
}

// DetectInput describes a detect_auth_type operation.
type DetectInput struct {
	// Server is the cluster API server URL to probe.
	Server string `json:"server" yaml:"server" doc:"Cluster API server URL to probe" maxLength:"2048" example:"https://api.example.com:6443"`

	// InsecureSkipTLS disables TLS verification while probing (development only).
	InsecureSkipTLS bool `json:"insecure_skip_tls,omitempty" yaml:"insecure_skip_tls,omitempty" doc:"Disable TLS verification while probing (development only)"`
}

func (in DetectInput) toInputs() map[string]any {
	return map[string]any{
		"server":            in.Server,
		"insecure_skip_tls": in.InsecureSkipTLS,
	}
}

// DetectResult is the detect_auth_type output.
type DetectResult struct {
	// Success reports whether the probe completed.
	Success bool `json:"success" yaml:"success" doc:"Whether the probe completed"`

	// AuthType is the detected authentication method (auto/oauth/oidc).
	AuthType kube.AuthType `json:"auth_type" yaml:"auth_type" doc:"Detected authentication method (auto/oauth/oidc)" example:"oidc"`

	// OIDCIssuer is the discovered OIDC issuer URL, when AuthType is oidc.
	OIDCIssuer string `json:"oidc_issuer,omitempty" yaml:"oidc_issuer,omitempty" doc:"Discovered OIDC issuer URL"`

	// OAuthEndpoint is the discovered OAuth authorization endpoint, when
	// AuthType is oauth.
	OAuthEndpoint string `json:"oauth_endpoint,omitempty" yaml:"oauth_endpoint,omitempty" doc:"Discovered OAuth authorization endpoint"`
}

// ReachableInput describes a reachable operation.
type ReachableInput struct {
	// Server is the cluster API server URL to check.
	Server string `json:"server" yaml:"server" doc:"Cluster API server URL to check" maxLength:"2048" example:"https://api.example.com:6443"`

	// InsecureSkipTLS disables TLS verification while checking (development only).
	InsecureSkipTLS bool `json:"insecure_skip_tls,omitempty" yaml:"insecure_skip_tls,omitempty" doc:"Disable TLS verification while checking (development only)"`
}

func (in ReachableInput) toInputs() map[string]any {
	return map[string]any{
		"server":            in.Server,
		"insecure_skip_tls": in.InsecureSkipTLS,
	}
}

// ReachableResult is the reachable output.
type ReachableResult struct {
	// Success reports whether the check completed.
	Success bool `json:"success" yaml:"success" doc:"Whether the check completed"`

	// Reachable reports whether the API server responded.
	Reachable bool `json:"reachable" yaml:"reachable" doc:"Whether the API server responded"`

	// Status is the HTTP status code returned by the health probe.
	Status int `json:"status" yaml:"status" doc:"HTTP status code returned by the health probe" maximum:"599" example:"200"`
}

// WhoamiInput describes a whoami operation. The token is supplied only for this
// operation and is never cached by the provider.
type WhoamiInput struct {
	// Server is the cluster API server URL.
	Server string `json:"server" yaml:"server" doc:"Cluster API server URL" maxLength:"2048" example:"https://api.example.com:6443"`

	// Token is the bearer token used for the SelfSubjectReview call.
	Token string `json:"token" yaml:"token" doc:"Bearer token for the SelfSubjectReview call" maxLength:"8192"`

	// Audience is the OIDC audience the token targets.
	Audience string `json:"audience,omitempty" yaml:"audience,omitempty" doc:"OIDC audience the token targets" maxLength:"512"`

	// CAData is the PEM-encoded cluster certificate authority bundle. When set,
	// it is preferred over InsecureSkipTLS so the SelfSubjectReview call verifies
	// the API server's certificate against a private CA.
	CAData string `json:"ca_data,omitempty" yaml:"ca_data,omitempty" doc:"PEM-encoded cluster CA bundle; preferred over insecure_skip_tls" maxLength:"1048576"`

	// InsecureSkipTLS disables TLS verification (development only).
	InsecureSkipTLS bool `json:"insecure_skip_tls,omitempty" yaml:"insecure_skip_tls,omitempty" doc:"Disable TLS verification (development only)"`
}

func (in WhoamiInput) toInputs() map[string]any {
	return map[string]any{
		"server":            in.Server,
		"token":             in.Token,
		"audience":          in.Audience,
		"ca_data":           in.CAData,
		"insecure_skip_tls": in.InsecureSkipTLS,
	}
}

// WhoamiResult is the whoami output.
type WhoamiResult struct {
	// Success reports whether the review completed.
	Success bool `json:"success" yaml:"success" doc:"Whether the review completed"`

	// Username is the authenticated subject's username.
	Username string `json:"username" yaml:"username" doc:"Authenticated subject's username"`

	// Groups are the subject's group memberships.
	Groups []string `json:"groups,omitempty" yaml:"groups,omitempty" doc:"Subject's group memberships" maxItems:"1000"`

	// UID is the subject's unique identifier.
	UID string `json:"uid,omitempty" yaml:"uid,omitempty" doc:"Subject's unique identifier"`
}

// decodeOutput decodes a provider execution result into the typed result T.
// It accepts a direct typed value (returned by in-process providers and mocks)
// and falls back to a JSON round-trip of the output data (returned after a
// plugin gRPC round-trip serializes the data to a map). Malformed output is
// reported as ErrInvalidOperation.
func decodeOutput[T any](result *provider.ExecutionResult) (T, error) {
	var out T
	if result == nil {
		return out, fmt.Errorf("%w: nil execution result", ErrInvalidOperation)
	}
	if result.Output.Data == nil {
		return out, fmt.Errorf("%w: nil output data", ErrInvalidOperation)
	}

	switch typed := result.Output.Data.(type) {
	case *T:
		if typed == nil {
			return out, fmt.Errorf("%w: nil typed output", ErrInvalidOperation)
		}
		return *typed, nil
	case T:
		return typed, nil
	}

	b, err := json.Marshal(result.Output.Data)
	if err != nil {
		return out, fmt.Errorf("%w: marshal output: %w", ErrInvalidOperation, err)
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return out, fmt.Errorf("%w: unmarshal output: %w", ErrInvalidOperation, err)
	}
	return out, nil
}
