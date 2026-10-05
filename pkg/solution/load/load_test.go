// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package load

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
	"github.com/oakwood-commons/scafctl/pkg/solution/source"
)

var allKinds = []source.Kind{
	source.KindUnknown,
	source.KindAutoDiscover,
	source.KindStdin,
	source.KindURL,
	source.KindRemoteRef,
	source.KindCatalogQualified,
	source.KindBareName,
	source.KindLocalFile,
}

func TestNewLoaders_StoreDependencies(t *testing.T) {
	t.Parallel()

	in := strings.NewReader("x")
	assert.Same(t, in, NewStdinLoader(in).in)
}

func TestWithLenient_SharedAcrossLoaders(t *testing.T) {
	t.Parallel()

	opt := WithLenient()
	assert.True(t, NewFileLoader(opt).lenient)
	assert.True(t, NewURLLoader(nil, opt).lenient)
}

func TestMaterializedSolution_LockFile(t *testing.T) {
	t.Parallel()

	valid, err := json.Marshal(bundler.LockFile{
		Version: bundler.LockFileVersion,
		Plugins: []bundler.LockPlugin{{Name: "aws-provider", Version: "1.2.3"}},
	})
	require.NoError(t, err)

	tests := []struct {
		name       string
		lock       []byte
		wantPlugin string
		wantErr    string
	}{
		{name: "no lock", lock: nil},
		{name: "empty lock", lock: []byte{}},
		{name: "valid lock", lock: valid, wantPlugin: "aws-provider"},
		{name: "malformed lock", lock: []byte("{not json"), wantErr: "solution lock: parsing lock file"},
		{name: "unsupported version", lock: []byte(`{"version":99}`), wantErr: "unsupported lock file version 99"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			lf, err := (&MaterializedSolution{Lock: tt.lock}).LockFile()

			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				assert.Nil(t, lf)
				return
			}
			require.NoError(t, err)
			if tt.wantPlugin == "" {
				assert.Nil(t, lf)
				return
			}
			require.NotNil(t, lf)
			require.Len(t, lf.Plugins, 1)
			assert.Equal(t, tt.wantPlugin, lf.Plugins[0].Name)
		})
	}
}
