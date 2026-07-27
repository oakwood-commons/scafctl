// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package hostname

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultTransform_MapKeyedShape(t *testing.T) {
	t.Parallel()

	// Mirrors the real clusters inventory: a top-level name-keyed map where each
	// value carries an apiServerURL.
	body := []byte(`{
		"cluster-a": {"clusterName": "cluster-a", "apiServerURL": "https://api.cluster-a.example.com:6443", "status": "in-use"},
		"cluster-d": {"clusterName": "cluster-d", "apiServerURL": "https://api.cluster-d.example.com:6443", "status": "in-use"}
	}`)

	cel := `_.map(k, {"name": k, "url": _[k].apiServerURL})`

	entries, err := defaultTransform(context.Background(), cel, body)
	require.NoError(t, err)
	require.Len(t, entries, 2)

	byName := map[string]string{}
	for _, e := range entries {
		byName[e.Name] = e.URL
	}
	assert.Equal(t, "https://api.cluster-a.example.com:6443", byName["cluster-a"])
	assert.Equal(t, "https://api.cluster-d.example.com:6443", byName["cluster-d"])
}

func TestDefaultTransform_FiltersWithCEL(t *testing.T) {
	t.Parallel()

	body := []byte(`{
		"good": {"apiServerURL": "https://api.good.example.com:6443", "status": "in-use"},
		"gone": {"apiServerURL": "https://api.gone.example.com:6443", "status": "deleted"}
	}`)

	cel := `_.map(k, _[k].status != "deleted", {"name": k, "url": _[k].apiServerURL})`

	entries, err := defaultTransform(context.Background(), cel, body)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "good", entries[0].Name)
}

func TestDefaultTransform_Errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cel     string
		body    string
		wantErr error
	}{
		{
			name:    "empty transform",
			cel:     "",
			body:    `{}`,
			wantErr: ErrTransformShape,
		},
		{
			name:    "invalid JSON body",
			cel:     "_",
			body:    `{not json`,
			wantErr: nil, // parse error, not a shape error
		},
		{
			name:    "result is an object not a list",
			cel:     `{"name": "x", "url": "https://x"}`,
			body:    `{}`,
			wantErr: ErrTransformShape,
		},
		{
			name:    "entry missing url",
			cel:     `[{"name": "x"}]`,
			body:    `{}`,
			wantErr: ErrTransformShape,
		},
		{
			name:    "entry missing name",
			cel:     `[{"url": "https://x"}]`,
			body:    `{}`,
			wantErr: ErrTransformShape,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := defaultTransform(context.Background(), tt.cel, []byte(tt.body))
			require.Error(t, err)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
			}
		})
	}
}

func TestDefaultTransform_EmptyListIsValid(t *testing.T) {
	t.Parallel()

	entries, err := defaultTransform(context.Background(), `[]`, []byte(`{}`))
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestDefaultTransform_CarriesOptionalOIDCFields(t *testing.T) {
	t.Parallel()

	body := []byte(`{
		"cluster-01": {
			"apiServerURL": "https://api.cluster-01.example.com:6443",
			"clientID": "api://cluster-01/.default",
			"authType": "oidc",
			"console": "https://console.cluster-01.example.com"
		}
	}`)

	cel := `_.map(k, {
		"name": k,
		"url": _[k].apiServerURL,
		"audience": _[k].clientID,
		"authType": _[k].authType,
		"consoleUrl": _[k].console
	})`

	entries, err := defaultTransform(context.Background(), cel, body)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "cluster-01", entries[0].Name)
	assert.Equal(t, "https://api.cluster-01.example.com:6443", entries[0].URL)
	assert.Equal(t, "api://cluster-01/.default", entries[0].Audience)
	assert.Equal(t, "oidc", entries[0].AuthType)
	assert.Equal(t, "https://console.cluster-01.example.com", entries[0].ConsoleURL)
}
