package solution

import "testing"

func TestSolution_UnmarshalFromBytes(t *testing.T) {
	tests := []struct {
		name    string
		bytes   []byte
		wantErr bool
	}{
		{
			name: "valid YAML",
			bytes: []byte(`
name: test-solution
displayName: Test Solution
description: A test solution
category: test
version: 1.2.3
schemaVersion: 1.0.0
scafctlVersion: ">= 1.0.0"
tags:
  - tag1
  - tag2
labels:
  env: test
maintainers:
  - name: John Doe
    email: john.doe@example.com
links:
  - name: Docs
    url: https://example.com/docs
`),
			wantErr: false,
		},
		{
			name: "valid JSON",
			bytes: []byte(`{
				"name": "test-solution",
				"displayName": "Test Solution",
				"description": "A test solution",
				"category": "test",
				"version": "1.2.3",
				"schemaVersion": "1.0.0",
				"scafctlVersion": ">= 1.0.0",
				"tags": ["tag1", "tag2"],
				"labels": {"env": "test"},
				"maintainers": [{"name": "John Doe", "email": "john.doe@example.com"}],
				"links": [{"name": "Docs", "url": "https://example.com/docs"}]
			}`),
			wantErr: false,
		},
		{
			name:    "invalid data",
			bytes:   []byte(`not a valid yaml or json`),
			wantErr: true,
		},
		{
			name:    "empty input",
			bytes:   []byte(``),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var s Solution
			gotErr := s.UnmarshalFromBytes(tt.bytes)
			if gotErr != nil {
				if !tt.wantErr {
					t.Errorf("UnmarshalFromBytes() failed: %v", gotErr)
				}
				return
			}
			if tt.wantErr {
				t.Fatal("UnmarshalFromBytes() succeeded unexpectedly")
			}
		})
	}
}
