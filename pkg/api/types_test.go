// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewPaginationInfo(t *testing.T) {
	tests := []struct {
		name        string
		total       int
		page        int
		perPage     int
		wantPages   int
		wantHasMore bool
	}{
		{"first of three pages", 25, 1, 10, 3, true},
		{"last page", 25, 3, 10, 3, false},
		{"exactly one page", 10, 1, 10, 1, false},
		{"empty", 0, 1, 10, 0, false},
		{"zero perPage defaults to 100", 200, 1, 0, 2, true},
		{"zero page defaults to 1", 10, 0, 10, 1, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := NewPaginationInfo(tt.total, tt.page, tt.perPage)
			assert.Equal(t, tt.wantPages, info.TotalPages)
			assert.Equal(t, tt.wantHasMore, info.HasMore)
			assert.Equal(t, tt.total, info.TotalItems)
		})
	}
}

func TestPaginate(t *testing.T) {
	items := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}

	tests := []struct {
		name    string
		page    int
		perPage int
		want    []int
	}{
		{"first page", 1, 3, []int{1, 2, 3}},
		{"second page", 2, 3, []int{4, 5, 6}},
		{"last partial page", 4, 3, []int{10}},
		{"beyond last page", 5, 3, []int{}},
		{"full page", 1, 10, items},
		{"defaults on zero", 0, 0, items},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Paginate(items, tt.page, tt.perPage)
			assert.Equal(t, tt.want, result)
		})
	}
}

func BenchmarkPaginate(b *testing.B) {
	items := make([]int, 1000)
	for i := range items {
		items[i] = i
	}
	for b.Loop() {
		Paginate(items, 5, 100)
	}
}

func BenchmarkNewPaginationInfo(b *testing.B) {
	for b.Loop() {
		NewPaginationInfo(1000, 5, 100)
	}
}

func TestSetLastModified(t *testing.T) {
	rec := httptest.NewRecorder()
	now := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	SetLastModified(rec, now)
	assert.Equal(t, "Wed, 01 Apr 2026 12:00:00 GMT", rec.Header().Get("Last-Modified"))
}

func TestSetLastModified_ZeroTime(t *testing.T) {
	rec := httptest.NewRecorder()
	SetLastModified(rec, time.Time{})
	assert.NotEmpty(t, rec.Header().Get("Last-Modified"))
}
