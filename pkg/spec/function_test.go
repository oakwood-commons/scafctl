// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package spec

import "testing"

func TestFunction_HasCel(t *testing.T) {
	tests := []struct {
		name string
		fn   *Function
		want bool
	}{
		{"nil", nil, false},
		{"empty", &Function{}, false},
		{"cel set", &Function{Cel: "1"}, true},
		{"template only", &Function{Template: "x"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.fn.HasCel(); got != tt.want {
				t.Errorf("HasCel() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFunction_HasTemplate(t *testing.T) {
	tests := []struct {
		name string
		fn   *Function
		want bool
	}{
		{"nil", nil, false},
		{"empty", &Function{}, false},
		{"template set", &Function{Template: "x"}, true},
		{"cel only", &Function{Cel: "1"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.fn.HasTemplate(); got != tt.want {
				t.Errorf("HasTemplate() = %v, want %v", got, tt.want)
			}
		})
	}
}
