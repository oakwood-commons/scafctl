// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package kubeconfig

import (
	"context"

	"github.com/Masterminds/semver/v3"
	"github.com/google/jsonschema-go/jsonschema"

	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/schemahelper"
)

// MockCall records a single ExecuteProvider invocation for assertions.
type MockCall struct {
	// Operation is the value of the "operation" input field.
	Operation string

	// Inputs is the full input map passed to the provider.
	Inputs map[string]any
}

// MockProvider is an in-process provider implementing CapabilityKubeconfig with
// operation dispatch. Tests register it in a provider.Registry (via
// WithRegistry) so the manager resolves it without fetching a plugin. It records
// every call and returns configurable canned outputs.
//
// Outputs default to a JSON-serializable map (mirroring the real plugin gRPC
// round-trip) so the manager's map-fallback decode path is exercised. Set
// ExecuteFunc to fully override behavior, or Err to fail every operation.
type MockProvider struct {
	// ExecuteFunc, when set, fully overrides dispatch. op is the operation field.
	ExecuteFunc func(ctx context.Context, op string, inputs map[string]any) (*provider.Output, error)

	// Err, when set (and ExecuteFunc is nil), is returned for every operation.
	Err error

	// Canned outputs returned by the default dispatch. Nil fields fall back to
	// echoing relevant input values with success=true.
	Write     *WriteResult
	Remove    *RemoveResult
	Server    string
	Detect    *DetectResult
	Reachable *ReachableResult
	Whoami    *WhoamiResult

	// Calls records every invocation in order.
	Calls []MockCall
}

// NewMockProvider returns a MockProvider with default success behavior.
func NewMockProvider() *MockProvider {
	return &MockProvider{}
}

// Descriptor implements provider.Provider.
func (m *MockProvider) Descriptor() *provider.Descriptor {
	return &provider.Descriptor{
		Name:        ProviderName,
		DisplayName: "Mock Kubeconfig",
		Description: "Mock kubeconfig provider for testing",
		APIVersion:  "v1",
		Version:     semver.MustParse("1.0.0"),
		Capabilities: []provider.Capability{
			provider.CapabilityKubeconfig,
		},
		// WriteOperations mirrors the real kubeconfig provider so tests exercise
		// the host-side write-operation guard (regression coverage for #579:
		// kubeconfig writes must be permitted under CapabilityKubeconfig).
		WriteOperations: []string{OperationWrite, OperationRemove},
		OutputSchemas: map[provider.Capability]*jsonschema.Schema{
			provider.CapabilityKubeconfig: schemahelper.ObjectSchema([]string{"success"}, map[string]*jsonschema.Schema{
				"success": schemahelper.BoolProp("Whether the operation succeeded"),
			}),
		},
	}
}

// Execute implements provider.Provider. It records the call and dispatches on
// the "operation" input field.
func (m *MockProvider) Execute(ctx context.Context, input any) (*provider.Output, error) {
	inputs, _ := input.(map[string]any)
	op, _ := inputs[inputOperation].(string)
	m.Calls = append(m.Calls, MockCall{Operation: op, Inputs: inputs})

	if m.ExecuteFunc != nil {
		return m.ExecuteFunc(ctx, op, inputs)
	}
	if m.Err != nil {
		return nil, m.Err
	}

	switch op {
	case OperationWrite:
		ctxName, _ := inputs["context_name"].(string)
		path, _ := inputs["kubeconfig_path"].(string)
		if ctxName == "" {
			// Mirror the real provider: an empty context name defaults to the
			// cluster name.
			ctxName, _ = inputs["cluster_name"].(string)
		}
		if m.Write != nil {
			ctxName = m.Write.ContextName
			path = m.Write.KubeconfigPath
		}
		return mapOutput(map[string]any{
			"success":         true,
			"context_name":    ctxName,
			"kubeconfig_path": path,
		}), nil
	case OperationRemove:
		removed := true
		if m.Remove != nil {
			removed = m.Remove.Removed
		}
		return mapOutput(map[string]any{
			"success": true,
			"removed": removed,
		}), nil
	case OperationCurrentServer:
		return mapOutput(map[string]any{
			"success": true,
			"server":  m.Server,
		}), nil
	case OperationDetectAuthType:
		out := map[string]any{"success": true}
		if m.Detect != nil {
			out["auth_type"] = string(m.Detect.AuthType)
			out["oidc_issuer"] = m.Detect.OIDCIssuer
			out["oauth_endpoint"] = m.Detect.OAuthEndpoint
		}
		return mapOutput(out), nil
	case OperationReachable:
		out := map[string]any{"success": true, "reachable": true, "status": 200}
		if m.Reachable != nil {
			out["reachable"] = m.Reachable.Reachable
			out["status"] = m.Reachable.Status
		}
		return mapOutput(out), nil
	case OperationWhoami:
		out := map[string]any{"success": true}
		if m.Whoami != nil {
			out["username"] = m.Whoami.Username
			out["groups"] = m.Whoami.Groups
			out["uid"] = m.Whoami.UID
		}
		return mapOutput(out), nil
	default:
		return mapOutput(map[string]any{"success": false}), nil
	}
}

// LastCall returns the most recent recorded call, or false when none exist.
func (m *MockProvider) LastCall() (MockCall, bool) {
	if len(m.Calls) == 0 {
		return MockCall{}, false
	}
	return m.Calls[len(m.Calls)-1], true
}

func mapOutput(data map[string]any) *provider.Output {
	return &provider.Output{Data: data}
}
