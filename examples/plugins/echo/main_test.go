// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"testing"

	sdkplugin "github.com/oakwood-commons/scafctl-plugin-sdk/plugin"
	sdkprovider "github.com/oakwood-commons/scafctl-plugin-sdk/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newPlugin() *EchoPlugin { return &EchoPlugin{} }

func TestGetProviders(t *testing.T) {
	providers, err := newPlugin().GetProviders(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"echo"}, providers)
}

func TestGetProviderDescriptor(t *testing.T) {
	tests := []struct {
		name        string
		provider    string
		wantErr     string
		wantName    string
		wantCaps    []sdkprovider.Capability
		wantVersion string
	}{
		{
			name:        "valid echo provider",
			provider:    "echo",
			wantName:    "echo",
			wantCaps:    []sdkprovider.Capability{sdkprovider.CapabilityTransform},
			wantVersion: "1.0.0",
		},
		{
			name:     "unknown provider",
			provider: "nonexistent",
			wantErr:  "unknown provider: nonexistent",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			desc, err := newPlugin().GetProviderDescriptor(context.Background(), tt.provider)
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
				assert.Nil(t, desc)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, desc)
			assert.Equal(t, tt.wantName, desc.Name)
			assert.Equal(t, "Echo Provider", desc.DisplayName)
			assert.Equal(t, "v1", desc.APIVersion)
			assert.Equal(t, tt.wantVersion, desc.Version.String())
			assert.Equal(t, tt.wantCaps, desc.Capabilities)
			assert.NotNil(t, desc.Schema)
			assert.Contains(t, desc.OutputSchemas, sdkprovider.CapabilityTransform)
		})
	}
}

func TestExecuteProvider(t *testing.T) {
	tests := []struct {
		name    string
		input   map[string]any
		want    string
		wantErr string
	}{
		{
			name:  "basic echo",
			input: map[string]any{"message": "hello"},
			want:  "hello",
		},
		{
			name:  "uppercase echo",
			input: map[string]any{"message": "hello", "uppercase": true},
			want:  "HELLO",
		},
		{
			name:  "uppercase false",
			input: map[string]any{"message": "Hello", "uppercase": false},
			want:  "Hello",
		},
		{
			name:  "missing uppercase defaults to false",
			input: map[string]any{"message": "test"},
			want:  "test",
		},
		{
			name:    "missing message",
			input:   map[string]any{},
			wantErr: "message must be a string",
		},
		{
			name:    "wrong message type",
			input:   map[string]any{"message": 123},
			wantErr: "message must be a string",
		},
		{
			name:  "empty message",
			input: map[string]any{"message": ""},
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := newPlugin().ExecuteProvider(context.Background(), "echo", tt.input)
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
				assert.Nil(t, out)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, out)
			data, ok := out.Data.(map[string]any)
			require.True(t, ok)
			assert.Equal(t, tt.want, data["echoed"])
		})
	}
}

func TestExecuteProvider_UnknownProvider(t *testing.T) {
	_, err := newPlugin().ExecuteProvider(context.Background(), "unknown", nil)
	require.EqualError(t, err, "unknown provider: unknown")
}

func TestDescribeWhatIf(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		input    map[string]any
		want     string
		wantErr  string
	}{
		{
			name:     "with message",
			provider: "echo",
			input:    map[string]any{"message": "hi"},
			want:     `Would echo "hi"`,
		},
		{
			name:     "empty message",
			provider: "echo",
			input:    map[string]any{"message": ""},
			want:     "Would echo message",
		},
		{
			name:     "missing message",
			provider: "echo",
			input:    map[string]any{},
			want:     "Would echo message",
		},
		{
			name:     "unknown provider",
			provider: "other",
			input:    map[string]any{},
			wantErr:  "unknown provider: other",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := newPlugin().DescribeWhatIf(context.Background(), tt.provider, tt.input)
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestConfigureProvider(t *testing.T) {
	err := newPlugin().ConfigureProvider(context.Background(), "echo", sdkplugin.ProviderConfig{})
	require.NoError(t, err)
}

func TestExecuteProviderStream(t *testing.T) {
	err := newPlugin().ExecuteProviderStream(context.Background(), "echo", nil, nil)
	require.ErrorIs(t, err, sdkplugin.ErrStreamingNotSupported)
}

func TestExtractDependencies(t *testing.T) {
	deps, err := newPlugin().ExtractDependencies(context.Background(), "echo", nil)
	require.NoError(t, err)
	assert.Nil(t, deps)
}

func TestStopProvider(t *testing.T) {
	err := newPlugin().StopProvider(context.Background(), "echo")
	require.NoError(t, err)
}

func BenchmarkExecuteProvider(b *testing.B) {
	p := newPlugin()
	ctx := context.Background()
	input := map[string]any{"message": "hello", "uppercase": true}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		_, _ = p.ExecuteProvider(ctx, "echo", input)
	}
}
