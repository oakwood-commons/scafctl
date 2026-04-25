// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"fmt"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/oakwood-commons/scafctl/pkg/provider/schemahelper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestProvider creates a mock provider for run tests.
func newTestProvider(name string, caps []Capability, schema *jsonschema.Schema, execFn func(context.Context, any) (*Output, error)) *mockExecutableProvider {
	version, _ := semver.NewVersion("1.0.0")
	return &mockExecutableProvider{
		descriptor: &Descriptor{
			Name:         name,
			APIVersion:   "v1",
			Version:      version,
			Description:  "Test provider",
			Capabilities: caps,
			Schema:       schema,
		},
		executeFunc: execFn,
	}
}

// nilDescriptorProvider implements Provider but returns nil from Descriptor().
type nilDescriptorProvider struct{}

func (p *nilDescriptorProvider) Descriptor() *Descriptor                           { return nil }
func (p *nilDescriptorProvider) Execute(_ context.Context, _ any) (*Output, error) { return nil, nil }

// ---------------------------------------------------------------------------
// ResolveCapability
// ---------------------------------------------------------------------------

func TestResolveCapability(t *testing.T) {
	tests := []struct {
		name       string
		caps       []Capability
		requested  string
		wantCap    Capability
		wantErrMsg string
	}{
		{
			name:    "defaults to first capability",
			caps:    []Capability{CapabilityFrom, CapabilityAction},
			wantCap: CapabilityFrom,
		},
		{
			name:      "selects requested capability",
			caps:      []Capability{CapabilityFrom, CapabilityAction},
			requested: "action",
			wantCap:   CapabilityAction,
		},
		{
			name:       "rejects unsupported capability",
			caps:       []Capability{CapabilityFrom},
			requested:  "action",
			wantErrMsg: "does not support capability",
		},
		{
			name:       "rejects invalid capability name",
			caps:       []Capability{CapabilityFrom},
			requested:  "bogus",
			wantErrMsg: "invalid capability",
		},
		{
			name:       "no capabilities declared",
			caps:       nil,
			wantErrMsg: "declares no capabilities",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			desc := &Descriptor{Name: "test", Capabilities: tt.caps}
			got, err := ResolveCapability(desc, tt.requested)
			if tt.wantErrMsg != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErrMsg)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantCap, got)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ValidateInputKeys
// ---------------------------------------------------------------------------

func TestValidateInputKeys(t *testing.T) {
	schema := schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
		"url":    schemahelper.StringProp(""),
		"method": schemahelper.StringProp(""),
	})

	tests := []struct {
		name       string
		inputs     map[string]any
		schema     *jsonschema.Schema
		wantErrMsg string
	}{
		{
			name:   "all valid keys",
			inputs: map[string]any{"url": "https://example.com", "method": "GET"},
			schema: schema,
		},
		{
			name:   "empty inputs",
			inputs: map[string]any{},
			schema: schema,
		},
		{
			name:       "unknown key",
			inputs:     map[string]any{"url": "x", "typo": "y"},
			schema:     schema,
			wantErrMsg: "unknown input keys",
		},
		{
			name:   "nil schema",
			inputs: map[string]any{"anything": "goes"},
			schema: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			desc := &Descriptor{Name: "test", Schema: tt.schema}
			err := ValidateInputKeys(tt.inputs, desc)
			if tt.wantErrMsg != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErrMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// RunProvider
// ---------------------------------------------------------------------------

func TestRunProvider_Success(t *testing.T) {
	prov := newTestProvider("static", []Capability{CapabilityFrom}, nil,
		func(_ context.Context, _ any) (*Output, error) {
			return &Output{Data: map[string]any{"value": "hello"}}, nil
		})

	ctx := WithExecutionMode(context.Background(), CapabilityFrom)
	result, err := RunProvider(ctx, RunOptions{
		Provider: prov,
		Inputs:   map[string]any{"value": "hello"},
	})
	require.NoError(t, err)
	assert.Equal(t, "static", result.Provider)
	assert.Equal(t, "from", result.Capability)
	assert.Equal(t, map[string]any{"value": "hello"}, result.Data)
	assert.False(t, result.DryRun)
	assert.NotEmpty(t, result.Duration)
}

func TestRunProvider_DryRun(t *testing.T) {
	prov := newTestProvider("static", []Capability{CapabilityFrom}, nil, nil)

	result, err := RunProvider(context.Background(), RunOptions{
		Provider: prov,
		Inputs:   map[string]any{"value": "test"},
		DryRun:   true,
	})
	require.NoError(t, err)
	assert.True(t, result.DryRun)
}

func TestRunProvider_WithCapability(t *testing.T) {
	prov := newTestProvider("multi", []Capability{CapabilityFrom, CapabilityAction}, nil, nil)

	result, err := RunProvider(context.Background(), RunOptions{
		Provider:   prov,
		Inputs:     map[string]any{},
		Capability: "action",
	})
	require.NoError(t, err)
	assert.Equal(t, "action", result.Capability)
}

func TestRunProvider_InvalidCapability(t *testing.T) {
	prov := newTestProvider("test", []Capability{CapabilityFrom}, nil, nil)

	_, err := RunProvider(context.Background(), RunOptions{
		Provider:   prov,
		Inputs:     map[string]any{},
		Capability: "action",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not support capability")
}

func TestRunProvider_InvalidInputKeys(t *testing.T) {
	schema := schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
		"url": schemahelper.StringProp(""),
	})
	prov := newTestProvider("http", []Capability{CapabilityFrom}, schema, nil)

	_, err := RunProvider(context.Background(), RunOptions{
		Provider: prov,
		Inputs:   map[string]any{"typo": "x"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown input keys")
}

func TestRunProvider_NilProvider(t *testing.T) {
	_, err := RunProvider(context.Background(), RunOptions{
		Provider: nil,
		Inputs:   map[string]any{},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "provider is required")
}

func TestRunProvider_NilDescriptor(t *testing.T) {
	prov := &nilDescriptorProvider{}
	_, err := RunProvider(context.Background(), RunOptions{
		Provider: prov,
		Inputs:   map[string]any{},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil descriptor")
}

func TestRunProvider_ExecutionFailure(t *testing.T) {
	prov := newTestProvider("failing", []Capability{CapabilityFrom}, nil,
		func(_ context.Context, _ any) (*Output, error) {
			return nil, fmt.Errorf("network timeout")
		})

	_, err := RunProvider(context.Background(), RunOptions{
		Provider: prov,
		Inputs:   map[string]any{},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "provider execution failed")
}

func TestRunProvider_WarningsAndMetadata(t *testing.T) {
	prov := newTestProvider("warn", []Capability{CapabilityFrom}, nil,
		func(_ context.Context, _ any) (*Output, error) {
			return &Output{
				Data:     map[string]any{"ok": true},
				Warnings: []string{"rate limit approaching"},
				Metadata: map[string]any{"rateRemaining": 10},
			}, nil
		})

	result, err := RunProvider(context.Background(), RunOptions{
		Provider: prov,
		Inputs:   map[string]any{},
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"rate limit approaching"}, result.Warnings)
	assert.Equal(t, map[string]any{"rateRemaining": 10}, result.Metadata)
}

// ---------------------------------------------------------------------------
// RunProvider write-operation auto-promote
// ---------------------------------------------------------------------------

func newWriteClassifierTestProvider(caps []Capability, writeOps []string, execFn func(context.Context, any) (*Output, error)) *mockExecutableProvider {
	version, _ := semver.NewVersion("1.0.0")
	return &mockExecutableProvider{
		descriptor: &Descriptor{
			Name:            "github",
			APIVersion:      "v1",
			Version:         version,
			Description:     "Test provider with write operations",
			Capabilities:    caps,
			WriteOperations: writeOps,
		},
		executeFunc: execFn,
	}
}

func TestRunProvider_AutoPromotesWriteOpToAction(t *testing.T) {
	prov := newWriteClassifierTestProvider(
		[]Capability{CapabilityFrom, CapabilityAction},
		[]string{"create_issue"},
		func(_ context.Context, _ any) (*Output, error) {
			return &Output{Data: map[string]any{"id": 42}}, nil
		},
	)

	result, err := RunProvider(context.Background(), RunOptions{
		Provider: prov,
		Inputs:   map[string]any{"operation": "create_issue", "title": "test"},
	})
	require.NoError(t, err)
	assert.Equal(t, "action", result.Capability)
}

func TestRunProvider_ReadOpStaysInFrom(t *testing.T) {
	prov := newWriteClassifierTestProvider(
		[]Capability{CapabilityFrom, CapabilityAction},
		[]string{"create_issue"},
		func(_ context.Context, _ any) (*Output, error) {
			return &Output{Data: map[string]any{"issues": []string{}}}, nil
		},
	)

	result, err := RunProvider(context.Background(), RunOptions{
		Provider: prov,
		Inputs:   map[string]any{"operation": "list_issues"},
	})
	require.NoError(t, err)
	assert.Equal(t, "from", result.Capability)
}

func TestRunProvider_ExplicitCapabilitySkipsAutoPromote(t *testing.T) {
	prov := newWriteClassifierTestProvider(
		[]Capability{CapabilityFrom, CapabilityAction},
		[]string{"create_issue"},
		nil,
	)

	// Explicitly requesting "from" for a write op should NOT auto-promote.
	// The executor's ValidateWriteOperation will block it.
	_, err := RunProvider(context.Background(), RunOptions{
		Provider:   prov,
		Inputs:     map[string]any{"operation": "create_issue"},
		Capability: "from",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "write operation")
}

func TestRunProvider_NoActionCapabilityNoPromotion(t *testing.T) {
	// Provider supports only "from" -- cannot promote.
	prov := newWriteClassifierTestProvider(
		[]Capability{CapabilityFrom},
		[]string{"create_issue"},
		nil,
	)

	_, err := RunProvider(context.Background(), RunOptions{
		Provider: prov,
		Inputs:   map[string]any{"operation": "create_issue"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "write operation")
}

func TestRunProvider_NilWriteOpsNoPromotion(t *testing.T) {
	// Provider with nil WriteOperations (no classification) -- should not
	// auto-promote; the operation runs in the default "from" capability.
	prov := newWriteClassifierTestProvider(
		[]Capability{CapabilityFrom, CapabilityAction},
		nil,
		func(_ context.Context, _ any) (*Output, error) {
			return &Output{Data: map[string]any{"ok": true}}, nil
		},
	)

	result, err := RunProvider(context.Background(), RunOptions{
		Provider: prov,
		Inputs:   map[string]any{"operation": "create_issue"},
	})
	require.NoError(t, err)
	assert.Equal(t, "from", result.Capability)
}

// ---------------------------------------------------------------------------
// descriptorHasCapability
// ---------------------------------------------------------------------------

func TestDescriptorHasCapability(t *testing.T) {
	desc := &Descriptor{
		Name:         "test",
		Capabilities: []Capability{CapabilityFrom, CapabilityAction},
	}
	assert.True(t, descriptorHasCapability(desc, CapabilityFrom))
	assert.True(t, descriptorHasCapability(desc, CapabilityAction))
	assert.False(t, descriptorHasCapability(desc, CapabilityTransform))
}

// ---------------------------------------------------------------------------
// Benchmarks
// ---------------------------------------------------------------------------

func BenchmarkResolveCapability(b *testing.B) {
	desc := &Descriptor{
		Name:         "test",
		Capabilities: []Capability{CapabilityFrom, CapabilityTransform, CapabilityAction},
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		ResolveCapability(desc, "action")
	}
}

func BenchmarkValidateInputKeys(b *testing.B) {
	schema := schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
		"url":     schemahelper.StringProp(""),
		"method":  schemahelper.StringProp(""),
		"timeout": schemahelper.IntProp(""),
		"headers": schemahelper.StringProp(""),
	})
	desc := &Descriptor{Name: "http", Schema: schema}
	inputs := map[string]any{"url": "x", "method": "GET"}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		ValidateInputKeys(inputs, desc)
	}
}
