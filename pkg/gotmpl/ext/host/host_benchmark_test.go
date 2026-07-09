// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package host

import (
	"strings"
	"testing"
	"text/template"
)

func BenchmarkHostConfigDir_Template(b *testing.B) {
	funcs := template.FuncMap{}
	for k, v := range ConfigDirFunc().Func {
		funcs[k] = v
	}
	tmpl, err := template.New("bench").Funcs(funcs).Parse(`{{ hostConfigDir }}/config.d/x.yaml`)
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	for b.Loop() {
		var sb strings.Builder
		if err := tmpl.Execute(&sb, nil); err != nil {
			b.Fatal(err)
		}
	}
}
