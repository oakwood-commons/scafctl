// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package official

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListItems_NilRegistry(t *testing.T) {
	ctx := context.Background()
	items := ListItems(ctx)
	assert.Nil(t, items)
}

func TestListItems_WithRegistry(t *testing.T) {
	reg := NewRegistryFrom([]Provider{
		{Name: "alpha", CatalogRef: "alpha-ref", DefaultVersion: "v1.0.0"},
		{Name: "beta", CatalogRef: "beta-ref", DefaultVersion: "latest"},
	})
	ctx := WithRegistry(context.Background(), reg)

	items := ListItems(ctx)
	require.Len(t, items, 2)

	// Items are returned in sorted name order (Names() sorts)
	assert.Equal(t, "alpha", items[0].Name)
	assert.Equal(t, "alpha", items[0].DisplayName)
	assert.Equal(t, "official", items[0].Source)
	assert.Equal(t, "v1.0.0", items[0].Version)
	assert.Contains(t, items[0].Description, "alpha-ref")
	assert.Empty(t, items[0].Capabilities)

	assert.Equal(t, "beta", items[1].Name)
	assert.Equal(t, "latest", items[1].Version)
}

func TestListItems_DefaultRegistry(t *testing.T) {
	reg := NewRegistry()
	ctx := WithRegistry(context.Background(), reg)

	items := ListItems(ctx)
	assert.Len(t, items, 10)

	for _, item := range items {
		assert.Equal(t, "official", item.Source)
		assert.NotEmpty(t, item.Name)
		assert.NotEmpty(t, item.Description)
	}
}

func TestDetail(t *testing.T) {
	p := Provider{
		Name:           "exec",
		CatalogRef:     "exec",
		DefaultVersion: "latest",
	}

	detail := Detail(p)
	assert.Equal(t, "exec", detail["name"])
	assert.Equal(t, "official", detail["source"])
	assert.Equal(t, "exec", detail["catalogRef"])
	assert.Equal(t, "latest", detail["version"])
	assert.NotEmpty(t, detail["description"])
}

func TestDetail_CustomProvider(t *testing.T) {
	p := Provider{
		Name:           "aws",
		CatalogRef:     "aws-provider",
		DefaultVersion: ">=1.0.0",
	}

	detail := Detail(p)
	assert.Equal(t, "aws", detail["name"])
	assert.Equal(t, "official", detail["source"])
	assert.Equal(t, "aws-provider", detail["catalogRef"])
	assert.Equal(t, ">=1.0.0", detail["version"])
}

func BenchmarkListItems(b *testing.B) {
	reg := NewRegistry()
	ctx := WithRegistry(context.Background(), reg)
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		ListItems(ctx)
	}
}

func BenchmarkDetail(b *testing.B) {
	p := Provider{Name: "exec", CatalogRef: "exec", DefaultVersion: "latest"}
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		Detail(p)
	}
}
