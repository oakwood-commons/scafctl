// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package flags_test

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/flags"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseKeyValue(t *testing.T) {
	tests := []struct {
		name    string
		input   []string
		want    map[string][]string
		wantErr bool
	}{
		{
			name:  "single key-value",
			input: []string{"key=value"},
			want:  map[string][]string{"key": {"value"}},
		},
		{
			name:  "multiple different keys",
			input: []string{"key1=value1", "key2=value2"},
			want: map[string][]string{
				"key1": {"value1"},
				"key2": {"value2"},
			},
		},
		{
			name:  "same key multiple times",
			input: []string{"region=us-east1", "region=us-west1", "region=eu-west1"},
			want: map[string][]string{
				"region": {"us-east1", "us-west1", "eu-west1"},
			},
		},
		{
			name:  "value with equals sign",
			input: []string{"config=key=value"},
			want:  map[string][]string{"config": {"key=value"}},
		},
		{
			name:  "value with multiple equals",
			input: []string{"json={\"key\":\"value\"}"},
			want:  map[string][]string{"json": {"{\"key\":\"value\"}"}},
		},
		{
			name:  "value with newlines",
			input: []string{"script=line1\nline2\nline3"},
			want:  map[string][]string{"script": {"line1\nline2\nline3"}},
		},
		{
			name:  "empty value",
			input: []string{"key="},
			want:  map[string][]string{"key": {""}},
		},
		{
			name:  "value with special characters",
			input: []string{"key=!@#$%^&*()"},
			want:  map[string][]string{"key": {"!@#$%^&*()"}},
		},
		{
			name:  "value with commas",
			input: []string{"list=item1,item2,item3"},
			want:  map[string][]string{"list": {"item1,item2,item3"}},
		},
		{
			name:  "value with quotes",
			input: []string{"msg=\"hello world\""},
			want:  map[string][]string{"msg": {"\"hello world\""}},
		},
		{
			name:    "missing equals sign",
			input:   []string{"keyvalue"},
			wantErr: true,
		},
		{
			name:    "empty key",
			input:   []string{"=value"},
			wantErr: true,
		},
		{
			name:    "key with spaces",
			input:   []string{"my key=value"},
			wantErr: true,
		},
		{
			name:    "key with newline",
			input:   []string{"key\nname=value"},
			wantErr: true,
		},
		{
			name:    "key with tab",
			input:   []string{"key\tname=value"},
			wantErr: true,
		},
		{
			name:  "key with leading/trailing whitespace trimmed",
			input: []string{" key =value"},
			want:  map[string][]string{"key": {"value"}},
		},
		{
			name:  "empty input",
			input: []string{},
			want:  map[string][]string{},
		},
		{
			name: "complex real-world example",
			input: []string{
				"env=prod",
				"region=us-east1",
				"region=us-west1",
				"apiKey=sk_live_abc123",
				"config={\"nested\":{\"key\":\"value\"}}",
				"script=#!/bin/bash\necho \"hello\"\nexit 0",
			},
			want: map[string][]string{
				"env":    {"prod"},
				"region": {"us-east1", "us-west1"},
				"apiKey": {"sk_live_abc123"},
				"config": {"{\"nested\":{\"key\":\"value\"}}"},
				"script": {"#!/bin/bash\necho \"hello\"\nexit 0"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := flags.ParseKeyValue(tt.input)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGetFirst(t *testing.T) {
	m := map[string][]string{
		"single":   {"value1"},
		"multiple": {"value1", "value2", "value3"},
		"empty":    {},
	}

	assert.Equal(t, "value1", flags.GetFirst(m, "single"))
	assert.Equal(t, "value1", flags.GetFirst(m, "multiple"))
	assert.Equal(t, "", flags.GetFirst(m, "nonexistent"))
	assert.Equal(t, "", flags.GetFirst(m, "empty"))
}

func TestGetAll(t *testing.T) {
	m := map[string][]string{
		"single":   {"value1"},
		"multiple": {"value1", "value2", "value3"},
		"empty":    {},
	}

	assert.Equal(t, []string{"value1"}, flags.GetAll(m, "single"))
	assert.Equal(t, []string{"value1", "value2", "value3"}, flags.GetAll(m, "multiple"))
	assert.Nil(t, flags.GetAll(m, "nonexistent"))
	assert.Equal(t, []string{}, flags.GetAll(m, "empty"))
}

func TestHas(t *testing.T) {
	m := map[string][]string{
		"key":   {"value"},
		"empty": {},
	}

	assert.True(t, flags.Has(m, "key"))
	assert.True(t, flags.Has(m, "empty"))
	assert.False(t, flags.Has(m, "nonexistent"))
}

func TestSplitKeyValue(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantKey   string
		wantValue string
		wantErr   bool
	}{
		{
			name:      "simple key-value",
			input:     "key=value",
			wantKey:   "key",
			wantValue: "value",
		},
		{
			name:      "value with equals",
			input:     "key=val=ue",
			wantKey:   "key",
			wantValue: "val=ue",
		},
		{
			name:      "empty value",
			input:     "key=",
			wantKey:   "key",
			wantValue: "",
		},
		{
			name:      "value with special chars",
			input:     "key=!@#$%",
			wantKey:   "key",
			wantValue: "!@#$%",
		},
		{
			name:    "no equals sign",
			input:   "keyvalue",
			wantErr: true,
		},
		{
			name:    "empty key",
			input:   "=value",
			wantErr: true,
		},
		{
			name:    "whitespace only key",
			input:   "  =value",
			wantErr: true,
		},
		{
			name:    "key with space",
			input:   "my key=value",
			wantErr: true,
		},
		{
			name:    "key with tab",
			input:   "my\tkey=value",
			wantErr: true,
		},
		{
			name:    "key with newline",
			input:   "my\nkey=value",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Note: splitKeyValue is not exported, so we test it through ParseKeyValue
			result, err := flags.ParseKeyValue([]string{tt.input})

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.Contains(t, result, tt.wantKey)
			assert.Equal(t, []string{tt.wantValue}, result[tt.wantKey])
		})
	}
}

func TestParseKeyValueCSV(t *testing.T) {
	tests := []struct {
		name    string
		input   []string
		want    map[string][]string
		wantErr bool
	}{
		{
			name:  "single entry no CSV",
			input: []string{"key=value"},
			want:  map[string][]string{"key": {"value"}},
		},
		{
			name:  "multiple entries in one flag",
			input: []string{"region=us-east1,region=us-west1,region=eu-west1"},
			want:  map[string][]string{"region": {"us-east1", "us-west1", "eu-west1"}},
		},
		{
			name:  "mixed keys in one flag",
			input: []string{"region=us-east1,env=prod,region=us-west1"},
			want: map[string][]string{
				"region": {"us-east1", "us-west1"},
				"env":    {"prod"},
			},
		},
		{
			name:  "quoted value with comma",
			input: []string{"region=\"us-east1,region=us-west1,region=eu-west1\""},
			want:  map[string][]string{"region": {"us-east1,region=us-west1,region=eu-west1"}},
		},
		{
			name:  "quoted value with escaped quotes",
			input: []string{"msg=\"Hello \\\"world\\\"\""},
			want:  map[string][]string{"msg": {"Hello \"world\""}},
		},
		{
			name:  "single quoted value",
			input: []string{"msg='Hello, world'"},
			want:  map[string][]string{"msg": {"Hello, world"}},
		},
		{
			name:  "whitespace around commas",
			input: []string{"region=us-east1, region=us-west1 , region=eu-west1"},
			want:  map[string][]string{"region": {"us-east1", "us-west1", "eu-west1"}},
		},
		{
			name: "multiple flags combined",
			input: []string{
				"region=us-east1,region=us-west1",
				"region=eu-west1",
			},
			want: map[string][]string{"region": {"us-east1", "us-west1", "eu-west1"}},
		},
		{
			name: "complex real-world example",
			input: []string{
				"env=prod,region=us-east1,region=us-west1",
				"apiKey=sk_live_abc123",
				"config=\"{\\\"nested\\\":{\\\"key\\\":\\\"value\\\"}}\"",
			},
			want: map[string][]string{
				"env":    {"prod"},
				"region": {"us-east1", "us-west1"},
				"apiKey": {"sk_live_abc123"},
				"config": {"{\"nested\":{\"key\":\"value\"}}"},
			},
		},
		{
			name:  "value with equals sign",
			input: []string{"config=key=value"},
			want:  map[string][]string{"config": {"key=value"}},
		},
		{
			name:  "empty quoted value",
			input: []string{"key=\"\""},
			want:  map[string][]string{"key": {""}},
		},
		{
			name:  "trailing comma ignored",
			input: []string{"region=us-east1,region=us-west1,"},
			want:  map[string][]string{"region": {"us-east1", "us-west1"}},
		},
		{
			name:    "unterminated quote",
			input:   []string{"key=\"value"},
			wantErr: true,
		},
		{
			name:    "invalid key in CSV",
			input:   []string{"valid=value,bad key=value"},
			wantErr: true,
		},
		// Shorthand syntax tests
		{
			name:  "shorthand - two values same key",
			input: []string{"env=prod,qa"},
			want:  map[string][]string{"env": {"prod", "qa"}},
		},
		{
			name:  "shorthand - three values same key",
			input: []string{"env=prod,qa,staging"},
			want:  map[string][]string{"env": {"prod", "qa", "staging"}},
		},
		{
			name:  "shorthand - mixed with different keys",
			input: []string{"region=us-east,env=prod,staging"},
			want: map[string][]string{
				"region": {"us-east"},
				"env":    {"prod", "staging"},
			},
		},
		{
			name:  "shorthand - multiple keys with shorthand",
			input: []string{"region=us-east,us-west,env=prod,qa,debug=true"},
			want: map[string][]string{
				"region": {"us-east", "us-west"},
				"env":    {"prod", "qa"},
				"debug":  {"true"},
			},
		},
		{
			name: "shorthand - across multiple flags",
			input: []string{
				"env=prod,qa",
				"region=us-east1",
			},
			want: map[string][]string{
				"env":    {"prod", "qa"},
				"region": {"us-east1"},
			},
		},
		{
			name: "shorthand - does not carry between flags",
			input: []string{
				"env=prod",
				"qa",
			},
			wantErr: true, // "qa" has no key in second flag
		},
		{
			name:    "shorthand - bare value first",
			input:   []string{"prod,env=qa"},
			wantErr: true, // No previous key for "prod"
		},
		{
			name:  "shorthand - with whitespace",
			input: []string{"env=prod, qa, staging"},
			want:  map[string][]string{"env": {"prod", "qa", "staging"}},
		},
		{
			name:  "shorthand - mixed explicit and shorthand",
			input: []string{"env=prod,env=qa,staging"},
			want:  map[string][]string{"env": {"prod", "qa", "staging"}},
		},
		{
			name:  "shorthand - with quoted values",
			input: []string{"msg=\"Hello, world\",\"Goodbye, world\""},
			want:  map[string][]string{"msg": {"Hello, world", "Goodbye, world"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := flags.ParseKeyValueCSV(tt.input)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseKeyValueCSV_Schemes(t *testing.T) {
	tests := []struct {
		name    string
		input   []string
		want    map[string][]string
		wantErr bool
	}{
		{
			name:  "json scheme without commas",
			input: []string{"data=json://{\"key\":\"value\"}"},
			want:  map[string][]string{"data": {"json://{\"key\":\"value\"}"}},
		},
		{
			name:  "json scheme with commas in value",
			input: []string{"data=json://[1,2,3]"},
			want:  map[string][]string{"data": {"json://[1,2,3]"}},
		},
		{
			name:  "json scheme with nested object and commas",
			input: []string{"data=json://{\"a\":1,\"b\":2,\"c\":3}"},
			want:  map[string][]string{"data": {"json://{\"a\":1,\"b\":2,\"c\":3}"}},
		},
		{
			name:  "json scheme in CSV context",
			input: []string{"env=prod,data=json://[1,2,3],region=us-east1"},
			want: map[string][]string{
				"env":    {"prod"},
				"data":   {"json://[1,2,3]"},
				"region": {"us-east1"},
			},
		},
		{
			name:  "json scheme with nested URL",
			input: []string{"config=json://{\"url\":\"https://example.com\"}"},
			want:  map[string][]string{"config": {"json://{\"url\":\"https://example.com\"}"}},
		},
		{
			name:  "yaml scheme",
			input: []string{"data=yaml://key: value"},
			want:  map[string][]string{"data": {"yaml://key: value"}},
		},
		{
			name:  "yaml scheme with commas",
			input: []string{"data=yaml://items: [a, b, c]"},
			want:  map[string][]string{"data": {"yaml://items: [a, b, c]"}},
		},
		{
			name:  "base64 scheme",
			input: []string{"data=base64://SGVsbG8sIFdvcmxkIQ=="},
			want:  map[string][]string{"data": {"base64://SGVsbG8sIFdvcmxkIQ=="}},
		},
		{
			name:  "http scheme",
			input: []string{"url=\"http://example.com/path?a=1,b=2\""},
			want:  map[string][]string{"url": {"http://example.com/path?a=1,b=2"}},
		},
		{
			name:  "https scheme",
			input: []string{"url=\"https://example.com/path?a=1,b=2\""},
			want:  map[string][]string{"url": {"https://example.com/path?a=1,b=2"}},
		},
		{
			name:  "file scheme",
			input: []string{"path=file:///etc/config.json"},
			want:  map[string][]string{"path": {"file:///etc/config.json"}},
		},
		{
			name:  "quoted json scheme in CSV",
			input: []string{"env=prod,data=\"json://[1,2,3]\",region=us-east1"},
			want: map[string][]string{
				"env":    {"prod"},
				"data":   {"json://[1,2,3]"},
				"region": {"us-east1"},
			},
		},
		{
			name: "multiple json schemes",
			input: []string{
				"config=json://{\"a\":1,\"b\":2}",
				"data=json://[1,2,3]",
			},
			want: map[string][]string{
				"config": {"json://{\"a\":1,\"b\":2}"},
				"data":   {"json://[1,2,3]"},
			},
		},
		{
			name:  "json scheme with complex nested structure",
			input: []string{"data=json://{\"users\":[{\"name\":\"Alice\",\"age\":30},{\"name\":\"Bob\",\"age\":25}]}"},
			want:  map[string][]string{"data": {"json://{\"users\":[{\"name\":\"Alice\",\"age\":30},{\"name\":\"Bob\",\"age\":25}]}"}},
		},
		{
			name:  "mixed schemes and regular values",
			input: []string{"env=prod,json=json://[1,2,3],yaml=yaml://key: value,url=https://example.com,name=test"},
			want: map[string][]string{
				"env":  {"prod"},
				"json": {"json://[1,2,3]"},
				"yaml": {"yaml://key: value"},
				"url":  {"https://example.com"},
				"name": {"test"},
			},
		},
		{
			name:  "scheme at end of CSV",
			input: []string{"env=prod,region=us-east1,data=json://[1,2,3]"},
			want: map[string][]string{
				"env":    {"prod"},
				"region": {"us-east1"},
				"data":   {"json://[1,2,3]"},
			},
		},
		{
			name:  "scheme at start of CSV",
			input: []string{"data=json://[1,2,3],env=prod,region=us-east1"},
			want: map[string][]string{
				"data":   {"json://[1,2,3]"},
				"env":    {"prod"},
				"region": {"us-east1"},
			},
		},
		{
			name:  "json with special characters and commas",
			input: []string{"data=json://{\"msg\":\"Hello, World!\",\"count\":42}"},
			want:  map[string][]string{"data": {"json://{\"msg\":\"Hello, World!\",\"count\":42}"}},
		},
		{
			name:  "json scheme preserves prefix",
			input: []string{"data=json://[1,2,3]"},
			want:  map[string][]string{"data": {"json://[1,2,3]"}},
		},
		{
			name:  "multiple values same key with schemes",
			input: []string{"data=json://[1,2,3],data=json://[4,5,6]"},
			want:  map[string][]string{"data": {"json://[1,2,3]", "json://[4,5,6]"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := flags.ParseKeyValueCSV(tt.input)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
