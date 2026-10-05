// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package load

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/solution/source"
)

type errReader struct{ err error }

func (r errReader) Read([]byte) (int, error) { return 0, r.err }

func TestStdinLoader_Load(t *testing.T) {
	t.Parallel()

	errRead := errors.New("boom")

	tests := []struct {
		name        string
		in          io.Reader
		opts        []StdinOption
		wantName    string
		wantCompose []string
		wantErr     error
		errMsg      string
	}{
		{
			name: "valid solution",
			in:   strings.NewReader(validSolutionYAML),
		},
		{
			name:        "compose is parsed but not applied",
			in:          strings.NewReader(composeRootYAML),
			wantCompose: []string{"part.yaml"},
		},
		{
			name:   "nil reader",
			in:     nil,
			errMsg: "no reader provided",
		},
		{
			name:    "read error",
			in:      errReader{err: errRead},
			wantErr: errRead,
			errMsg:  "reading solution from stdin",
		},
		{
			name:   "parse error",
			in:     strings.NewReader("not valid yaml: ["),
			errMsg: "parsing solution from stdin",
		},
		{
			name:   "strict validation failure",
			in:     strings.NewReader(undefinedDepJSON),
			errMsg: "parsing solution from stdin",
		},
		{
			name:     "lenient skips spec validation",
			in:       strings.NewReader(undefinedDepJSON),
			opts:     []StdinOption{WithLenient()},
			wantName: "lenient",
		},
		{
			name:   "parse error lenient",
			in:     strings.NewReader("not valid yaml: ["),
			opts:   []StdinOption{WithLenient()},
			errMsg: "parsing solution from stdin",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := source.Classification{Kind: source.KindStdin, Ref: "-"}
			got, err := NewStdinLoader(tt.in, tt.opts...).Load(t.Context(), c)

			if tt.errMsg != "" {
				require.Error(t, err)
				assert.Nil(t, got)
				assert.Contains(t, err.Error(), tt.errMsg)
				if tt.wantErr != nil {
					assert.ErrorIs(t, err, tt.wantErr)
				}
				return
			}

			require.NoError(t, err)
			require.NotNil(t, got)
			require.NotNil(t, got.Solution)
			assert.Empty(t, got.Solution.GetPath())
			if tt.wantName != "" {
				assert.Equal(t, tt.wantName, got.Solution.Metadata.Name)
				return
			}
			assert.Equal(t, tt.wantCompose, got.Solution.Compose)
			assert.Empty(t, got.Solution.Spec.Resolvers)
			assert.Equal(t, c, got.Source)
			assert.Nil(t, got.Bundle)
			assert.Nil(t, got.Lock)
		})
	}
}

func TestStdinLoader_Load_UnsupportedKinds(t *testing.T) {
	t.Parallel()

	for _, kind := range allKinds {
		if kind == source.KindStdin {
			continue
		}
		t.Run(kind.String(), func(t *testing.T) {
			t.Parallel()
			got, err := NewStdinLoader(strings.NewReader("")).Load(t.Context(), source.Classification{Kind: kind, Ref: "ref"})
			require.ErrorIs(t, err, ErrUnsupportedKind)
			assert.Nil(t, got)
			assert.Contains(t, err.Error(), kind.String())
		})
	}
}

func BenchmarkStdinLoader_Load(b *testing.B) {
	c := source.Classification{Kind: source.KindStdin, Ref: "-"}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := NewStdinLoader(strings.NewReader(validSolutionYAML)).Load(b.Context(), c); err != nil {
			b.Fatal(err)
		}
	}
}
