// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package cmdutil_test

import (
	"bytes"
	"os"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/cmd/cmdutil"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
)

// newRenderWriter returns a Writer whose stdout and stderr are directed to the
// same buffer so tests can assert on the combined output regardless of stream.
func newRenderWriter() (*bytes.Buffer, *writer.Writer) {
	buf := &bytes.Buffer{}
	ioStreams := &terminal.IOStreams{
		In:     os.Stdin,
		Out:    buf,
		ErrOut: buf,
	}
	cliParams := settings.NewCliParams()
	cliParams.NoColor = true
	return buf, writer.New(ioStreams, cliParams)
}

func TestRenderVerifyResult(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		result     *bundler.VerifyResult
		wantSubstr []string
		notSubstr  []string
	}{
		{
			name: "static pass",
			result: &bundler.VerifyResult{
				Successes: []string{"templates/main.tmpl"},
			},
			wantSubstr: []string{"Static paths:", "templates/main.tmpl"},
		},
		{
			name: "static fail",
			result: &bundler.VerifyResult{
				Errors: []bundler.VerifyError{{Path: "templates/missing.tmpl", Reason: "not found in bundle"}},
			},
			wantSubstr: []string{"Static paths:", "templates/missing.tmpl", "not found in bundle", " -- "},
			notSubstr:  []string{"\u2014"},
		},
		{
			name: "glob hit",
			result: &bundler.VerifyResult{
				Successes: []string{"glob:files/**"},
			},
			wantSubstr: []string{"Bundle includes (glob coverage):", "files/**"},
			notSubstr:  []string{"glob:files"},
		},
		{
			name: "glob miss",
			result: &bundler.VerifyResult{
				Warnings: []string{`pattern "missing/**" matches no bundled files`},
			},
			wantSubstr: []string{"Bundle includes (glob coverage):", "missing/**"},
		},
		{
			name: "plugin entries",
			result: &bundler.VerifyResult{
				Successes: []string{"plugin:acme (provider) 1.2.3"},
			},
			wantSubstr: []string{"Plugins:", "acme (provider) 1.2.3"},
			notSubstr:  []string{"plugin:acme"},
		},
		{
			name: "bare warnings",
			result: &bundler.VerifyResult{
				Warnings: []string{"solution references 2 local files but has no bundle"},
			},
			wantSubstr: []string{"solution references 2 local files but has no bundle"},
			notSubstr:  []string{"Bundle includes (glob coverage):"},
		},
		{
			name:      "empty result",
			result:    &bundler.VerifyResult{},
			notSubstr: []string{"Static paths:", "Plugins:", "Bundle includes"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			buf, w := newRenderWriter()
			cmdutil.RenderVerifyResult(w, tt.result)
			out := buf.String()
			for _, want := range tt.wantSubstr {
				assert.Contains(t, out, want)
			}
			for _, notWant := range tt.notSubstr {
				assert.NotContains(t, out, notWant)
			}
		})
	}
}

func TestRenderVerifyResult_NilSafe(t *testing.T) {
	t.Parallel()
	buf, w := newRenderWriter()
	cmdutil.RenderVerifyResult(w, nil)
	assert.Empty(t, buf.String())
	// nil writer must not panic
	cmdutil.RenderVerifyResult(nil, &bundler.VerifyResult{})
}
