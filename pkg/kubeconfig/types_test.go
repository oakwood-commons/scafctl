// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package kubeconfig

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/kube"
	"github.com/oakwood-commons/scafctl/pkg/provider"
)

func TestWriteInput_ToInputs(t *testing.T) {
	t.Parallel()
	in := WriteInput{
		Server:            "https://api.example.com:6443",
		Audience:          "aud",
		ClusterName:       "prod",
		ContextName:       "ctx",
		UserName:          "user",
		KubeconfigPath:    "/tmp/kubeconfig",
		ExecCommand:       "scafctl",
		ExecArgs:          []string{"auth", "token"},
		InsecureSkipTLS:   true,
		SetCurrentContext: true,
	}
	got := in.toInputs()
	assert.Equal(t, "https://api.example.com:6443", got["server"])
	assert.Equal(t, "prod", got["cluster_name"])
	assert.Equal(t, "scafctl", got["exec_command"])
	assert.Equal(t, []string{"auth", "token"}, got["exec_args"])
	assert.Equal(t, true, got["insecure_skip_tls"])
	assert.Equal(t, true, got["set_current_context"])
}

func TestDecodeOutput_MapPath(t *testing.T) {
	t.Parallel()
	result := &provider.ExecutionResult{
		Output: provider.Output{Data: map[string]any{
			"success":         true,
			"context_name":    "prod-ctx",
			"kubeconfig_path": "/tmp/kubeconfig",
		}},
	}
	got, err := decodeOutput[WriteResult](result)
	require.NoError(t, err)
	assert.True(t, got.Success)
	assert.Equal(t, "prod-ctx", got.ContextName)
	assert.Equal(t, "/tmp/kubeconfig", got.KubeconfigPath)
}

func TestDecodeOutput_PointerPath(t *testing.T) {
	t.Parallel()
	want := &WriteResult{Success: true, ContextName: "prod-ctx"}
	result := &provider.ExecutionResult{Output: provider.Output{Data: want}}
	got, err := decodeOutput[WriteResult](result)
	require.NoError(t, err)
	assert.Equal(t, *want, got)
}

func TestDecodeOutput_ValuePath(t *testing.T) {
	t.Parallel()
	want := WriteResult{Success: true, ContextName: "prod-ctx"}
	result := &provider.ExecutionResult{Output: provider.Output{Data: want}}
	got, err := decodeOutput[WriteResult](result)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestDecodeOutput_NilTypedPointer(t *testing.T) {
	t.Parallel()
	var nilPtr *WriteResult
	result := &provider.ExecutionResult{Output: provider.Output{Data: nilPtr}}
	_, err := decodeOutput[WriteResult](result)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidOperation)
}

func TestDecodeOutput_NilResult(t *testing.T) {
	t.Parallel()
	_, err := decodeOutput[WriteResult](nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidOperation)
}

func TestDecodeOutput_NilData(t *testing.T) {
	t.Parallel()
	result := &provider.ExecutionResult{Output: provider.Output{Data: nil}}
	_, err := decodeOutput[WriteResult](result)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidOperation)
}

func TestDecodeOutput_Unmarshalable(t *testing.T) {
	t.Parallel()
	// A channel cannot be marshaled to JSON, forcing the marshal error path.
	result := &provider.ExecutionResult{Output: provider.Output{Data: make(chan int)}}
	_, err := decodeOutput[WriteResult](result)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidOperation)
}

func TestDecodeOutput_DetectResultAuthType(t *testing.T) {
	t.Parallel()
	result := &provider.ExecutionResult{
		Output: provider.Output{Data: map[string]any{
			"success":   true,
			"auth_type": "oidc",
		}},
	}
	got, err := decodeOutput[DetectResult](result)
	require.NoError(t, err)
	assert.Equal(t, kube.AuthTypeOIDC, got.AuthType)
}

func BenchmarkWriteInput_ToInputs(b *testing.B) {
	in := WriteInput{
		Server:      "https://api.example.com:6443",
		ClusterName: "prod",
		ExecCommand: "scafctl",
		ExecArgs:    []string{"auth", "token"},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = in.toInputs()
	}
}

func BenchmarkDecodeOutput_MapPath(b *testing.B) {
	result := &provider.ExecutionResult{
		Output: provider.Output{Data: map[string]any{
			"success":         true,
			"context_name":    "prod-ctx",
			"kubeconfig_path": "/tmp/kubeconfig",
		}},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := decodeOutput[WriteResult](result); err != nil {
			b.Fatal(err)
		}
	}
}
