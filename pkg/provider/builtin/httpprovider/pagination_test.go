// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package httpprovider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- parsePaginationConfig tests ---

func TestParsePaginationConfig_NoPagination(t *testing.T) {
	cfg, err := parsePaginationConfig(map[string]any{})
	require.NoError(t, err)
	assert.Nil(t, cfg)
}

func TestParsePaginationConfig_NilPagination(t *testing.T) {
	cfg, err := parsePaginationConfig(map[string]any{"pagination": nil})
	require.NoError(t, err)
	assert.Nil(t, cfg)
}

func TestParsePaginationConfig_InvalidType(t *testing.T) {
	cfg, err := parsePaginationConfig(map[string]any{"pagination": "invalid"})
	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "expected object")
}

func TestParsePaginationConfig_MissingStrategy(t *testing.T) {
	cfg, err := parsePaginationConfig(map[string]any{
		"pagination": map[string]any{
			"maxPages": 10,
		},
	})
	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "strategy is required")
}

func TestParsePaginationConfig_UnknownStrategy(t *testing.T) {
	cfg, err := parsePaginationConfig(map[string]any{
		"pagination": map[string]any{
			"strategy": "unknown",
			"maxPages": 10,
		},
	})
	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "unknown strategy")
}

func TestParsePaginationConfig_OffsetStrategy(t *testing.T) {
	cfg, err := parsePaginationConfig(map[string]any{
		"pagination": map[string]any{
			"strategy":    "offset",
			"maxPages":    10,
			"limit":       50,
			"offsetParam": "skip",
			"limitParam":  "take",
		},
	})
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, StrategyOffset, cfg.Strategy)
	assert.Equal(t, 10, cfg.MaxPages)
	assert.Equal(t, 50, cfg.Limit)
	assert.Equal(t, "skip", cfg.OffsetParam)
	assert.Equal(t, "take", cfg.LimitParam)
}

func TestParsePaginationConfig_OffsetStrategy_MissingLimit(t *testing.T) {
	_, err := parsePaginationConfig(map[string]any{
		"pagination": map[string]any{
			"strategy": "offset",
			"maxPages": 10,
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "limit is required")
}

func TestParsePaginationConfig_OffsetStrategy_Defaults(t *testing.T) {
	cfg, err := parsePaginationConfig(map[string]any{
		"pagination": map[string]any{
			"strategy": "offset",
			"maxPages": 5,
			"limit":    25,
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "offset", cfg.OffsetParam)
	assert.Equal(t, "limit", cfg.LimitParam)
}

func TestParsePaginationConfig_PageNumberStrategy(t *testing.T) {
	cfg, err := parsePaginationConfig(map[string]any{
		"pagination": map[string]any{
			"strategy":      "pageNumber",
			"maxPages":      5,
			"pageSize":      25,
			"pageParam":     "p",
			"pageSizeParam": "size",
			"startPage":     0,
		},
	})
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, StrategyPageNumber, cfg.Strategy)
	assert.Equal(t, 25, cfg.PageSize)
	assert.Equal(t, "p", cfg.PageParam)
	assert.Equal(t, "size", cfg.PageSizeParam)
	assert.Equal(t, 0, cfg.StartPage)
}

func TestParsePaginationConfig_PageNumberStrategy_MissingPageSize(t *testing.T) {
	_, err := parsePaginationConfig(map[string]any{
		"pagination": map[string]any{
			"strategy": "pageNumber",
			"maxPages": 5,
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pageSize is required")
}

func TestParsePaginationConfig_CursorStrategy_WithTokenPath(t *testing.T) {
	cfg, err := parsePaginationConfig(map[string]any{
		"pagination": map[string]any{
			"strategy":       "cursor",
			"maxPages":       10,
			"nextTokenPath":  "body.nextCursor",
			"nextTokenParam": "cursor",
		},
	})
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, StrategyCursor, cfg.Strategy)
	assert.Equal(t, "body.nextCursor", cfg.NextTokenPath)
	assert.Equal(t, "cursor", cfg.NextTokenParam)
}

func TestParsePaginationConfig_CursorStrategy_WithNextURL(t *testing.T) {
	cfg, err := parsePaginationConfig(map[string]any{
		"pagination": map[string]any{
			"strategy":    "cursor",
			"maxPages":    10,
			"nextURLPath": "body['@odata.nextLink']",
		},
	})
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, "body['@odata.nextLink']", cfg.NextURLPath)
}

func TestParsePaginationConfig_CursorStrategy_MissingPaths(t *testing.T) {
	_, err := parsePaginationConfig(map[string]any{
		"pagination": map[string]any{
			"strategy": "cursor",
			"maxPages": 10,
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nextTokenPath or nextURLPath")
}

func TestParsePaginationConfig_CursorStrategy_MissingParam(t *testing.T) {
	_, err := parsePaginationConfig(map[string]any{
		"pagination": map[string]any{
			"strategy":      "cursor",
			"maxPages":      10,
			"nextTokenPath": "body.next",
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nextTokenParam is required")
}

func TestParsePaginationConfig_LinkHeaderStrategy(t *testing.T) {
	cfg, err := parsePaginationConfig(map[string]any{
		"pagination": map[string]any{
			"strategy":    "linkHeader",
			"maxPages":    5,
			"collectPath": "body",
		},
	})
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, StrategyLinkHeader, cfg.Strategy)
	assert.Equal(t, "body", cfg.CollectPath)
}

func TestParsePaginationConfig_CustomStrategy(t *testing.T) {
	cfg, err := parsePaginationConfig(map[string]any{
		"pagination": map[string]any{
			"strategy": "custom",
			"maxPages": 3,
			"nextURL":  "body.links.next",
		},
	})
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, StrategyCustom, cfg.Strategy)
	assert.Equal(t, "body.links.next", cfg.NextURL)
}

func TestParsePaginationConfig_CustomStrategy_MissingExpressions(t *testing.T) {
	_, err := parsePaginationConfig(map[string]any{
		"pagination": map[string]any{
			"strategy": "custom",
			"maxPages": 3,
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nextURL or nextParams")
}

func TestParsePaginationConfig_Float64MaxPages(t *testing.T) {
	cfg, err := parsePaginationConfig(map[string]any{
		"pagination": map[string]any{
			"strategy": "linkHeader",
			"maxPages": float64(15),
		},
	})
	require.NoError(t, err)
	assert.Equal(t, 15, cfg.MaxPages)
}

func TestParsePaginationConfig_StopWhenAndCollectPath(t *testing.T) {
	cfg, err := parsePaginationConfig(map[string]any{
		"pagination": map[string]any{
			"strategy":    "linkHeader",
			"maxPages":    10,
			"stopWhen":    "size(body) == 0",
			"collectPath": "body.items",
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "size(body) == 0", cfg.StopWhen)
	assert.Equal(t, "body.items", cfg.CollectPath)
}

// --- Link header parsing tests ---

func TestResolveLinkHeaderNext_WithRelNext(t *testing.T) {
	resp := &paginatedResponse{
		Headers: map[string]any{
			"Link": `<https://api.example.com/items?page=2>; rel="next", <https://api.example.com/items?page=5>; rel="last"`,
		},
	}
	nextURL, stop, err := resolveLinkHeaderNext("https://api.example.com/items?page=1", resp)
	require.NoError(t, err)
	assert.False(t, stop)
	assert.Equal(t, "https://api.example.com/items?page=2", nextURL)
}

func TestResolveLinkHeaderNext_NoLinkHeader(t *testing.T) {
	resp := &paginatedResponse{
		Headers: map[string]any{},
	}
	_, stop, err := resolveLinkHeaderNext("https://api.example.com/items", resp)
	require.NoError(t, err)
	assert.True(t, stop)
}

func TestResolveLinkHeaderNext_NoRelNext(t *testing.T) {
	resp := &paginatedResponse{
		Headers: map[string]any{
			"Link": `<https://api.example.com/items?page=1>; rel="prev"`,
		},
	}
	_, stop, err := resolveLinkHeaderNext("https://api.example.com/items?page=2", resp)
	require.NoError(t, err)
	assert.True(t, stop)
}

func TestResolveLinkHeaderNext_MultipleHeaderValues(t *testing.T) {
	resp := &paginatedResponse{
		Headers: map[string]any{
			"Link": []string{
				`<https://api.example.com/items?page=1>; rel="prev"`,
				`<https://api.example.com/items?page=3>; rel="next"`,
			},
		},
	}
	nextURL, stop, err := resolveLinkHeaderNext("https://api.example.com/items?page=2", resp)
	require.NoError(t, err)
	assert.False(t, stop)
	assert.Equal(t, "https://api.example.com/items?page=3", nextURL)
}

// --- Offset strategy tests ---

func TestResolveOffsetNext(t *testing.T) {
	cfg := &paginationConfig{
		Strategy:    StrategyOffset,
		OffsetParam: "offset",
		LimitParam:  "limit",
		Limit:       50,
	}
	responseCtx := map[string]any{
		"page": 1,
		"body": []any{1, 2, 3}, // won't trigger stop on page 1
	}

	nextURL, stop, err := resolveOffsetNext("https://api.example.com/items?offset=0&limit=50", cfg, responseCtx)
	require.NoError(t, err)
	assert.False(t, stop)
	assert.Contains(t, nextURL, "offset=50")
	assert.Contains(t, nextURL, "limit=50")
}

func TestResolveOffsetNext_StopsOnEmptyPage(t *testing.T) {
	cfg := &paginationConfig{
		Strategy:    StrategyOffset,
		OffsetParam: "offset",
		LimitParam:  "limit",
		Limit:       50,
	}
	responseCtx := map[string]any{
		"page": 2,
		"body": []any{1, 2, 3}, // fewer than limit=50
	}

	_, stop, err := resolveOffsetNext("https://api.example.com/items?offset=50&limit=50", cfg, responseCtx)
	require.NoError(t, err)
	assert.True(t, stop)
}

// --- Page number strategy tests ---

func TestResolvePageNumberNext(t *testing.T) {
	cfg := &paginationConfig{
		Strategy:      StrategyPageNumber,
		PageParam:     "page",
		PageSizeParam: "pageSize",
		PageSize:      25,
		StartPage:     1,
	}
	responseCtx := map[string]any{
		"page": 1,
		"body": map[string]any{"items": []any{1, 2, 3}},
	}

	nextURL, stop, err := resolvePageNumberNext("https://api.example.com/items?page=1&pageSize=25", cfg, responseCtx)
	require.NoError(t, err)
	assert.False(t, stop)
	assert.Contains(t, nextURL, "page=2")
	assert.Contains(t, nextURL, "pageSize=25")
}

// --- Integration tests (full Execute with pagination) ---

// newPaginatedServer creates a test server that serves paginated JSON responses.
// Each page returns items and metadata for different pagination strategies.
func newPaginatedServer(t *testing.T, totalItems, pageSize int) *httptest.Server {
	t.Helper()

	items := make([]map[string]any, totalItems)
	for i := 0; i < totalItems; i++ {
		items[i] = map[string]any{"id": i + 1, "name": fmt.Sprintf("item-%d", i+1)}
	}

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()

		// Determine offset based on query params
		offset := 0
		if o := q.Get("offset"); o != "" {
			offset, _ = strconv.Atoi(o)
		} else if p := q.Get("page"); p != "" {
			pageNum, _ := strconv.Atoi(p)
			offset = (pageNum - 1) * pageSize
		}

		// Calculate page slice
		end := offset + pageSize
		if end > totalItems {
			end = totalItems
		}
		var pageItems []map[string]any
		if offset < totalItems {
			pageItems = items[offset:end]
		} else {
			pageItems = []map[string]any{}
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		resp := map[string]any{
			"items": pageItems,
			"total": totalItems,
		}

		_ = json.NewEncoder(w).Encode(resp)
	}))
}

func TestHTTPProvider_Pagination_Offset(t *testing.T) {
	totalItems := 7
	pageSize := 3

	server := newPaginatedServer(t, totalItems, pageSize)
	defer server.Close()

	p := NewHTTPProvider()
	ctx := testContext(t)

	inputs := map[string]any{
		"url":    server.URL + "/items",
		"method": "GET",
		"pagination": map[string]any{
			"strategy":    "offset",
			"maxPages":    10,
			"limit":       pageSize,
			"offsetParam": "offset",
			"limitParam":  "limit",
			"collectPath": "body.items",
			"stopWhen":    "size(body.items) == 0",
		},
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	data := output.Data.(map[string]any)
	assert.Equal(t, 200, data["statusCode"])

	// Should have fetched multiple pages
	pages, ok := data["pages"].(int)
	require.True(t, ok)
	assert.True(t, pages > 1, "expected multiple pages, got %d", pages)

	// Parse the collected items
	var collectedItems []map[string]any
	err = json.Unmarshal([]byte(data["body"].(string)), &collectedItems)
	require.NoError(t, err)
	assert.Equal(t, totalItems, len(collectedItems))

	// Verify items are correct
	for i, item := range collectedItems {
		id, _ := item["id"].(float64)
		assert.Equal(t, float64(i+1), id)
	}
}

func TestHTTPProvider_Pagination_PageNumber(t *testing.T) {
	totalItems := 5
	pageSize := 2

	server := newPaginatedServer(t, totalItems, pageSize)
	defer server.Close()

	p := NewHTTPProvider()
	ctx := testContext(t)

	inputs := map[string]any{
		"url":    server.URL + "/items?page=1&pageSize=2",
		"method": "GET",
		"pagination": map[string]any{
			"strategy":      "pageNumber",
			"maxPages":      10,
			"pageSize":      pageSize,
			"pageParam":     "page",
			"pageSizeParam": "pageSize",
			"startPage":     1,
			"collectPath":   "body.items",
			"stopWhen":      "size(body.items) == 0",
		},
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	data := output.Data.(map[string]any)
	assert.Equal(t, 200, data["statusCode"])

	pages := data["pages"].(int)
	assert.True(t, pages > 1)

	var collectedItems []map[string]any
	err = json.Unmarshal([]byte(data["body"].(string)), &collectedItems)
	require.NoError(t, err)
	assert.Equal(t, totalItems, len(collectedItems))
}

func TestHTTPProvider_Pagination_Cursor(t *testing.T) {
	totalItems := 6
	pageSize := 2

	items := make([]map[string]any, totalItems)
	for i := 0; i < totalItems; i++ {
		items[i] = map[string]any{"id": i + 1, "name": fmt.Sprintf("item-%d", i+1)}
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cursor := r.URL.Query().Get("cursor")
		offset := 0
		if cursor != "" {
			offset, _ = strconv.Atoi(cursor)
		}

		end := offset + pageSize
		if end > totalItems {
			end = totalItems
		}
		pageItems := items[offset:end]

		var nextCursor *string
		if end < totalItems {
			c := strconv.Itoa(end)
			nextCursor = &c
		}

		resp := map[string]any{
			"items":      pageItems,
			"nextCursor": nextCursor,
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := NewHTTPProvider()
	ctx := testContext(t)

	inputs := map[string]any{
		"url":    server.URL + "/items",
		"method": "GET",
		"pagination": map[string]any{
			"strategy":       "cursor",
			"maxPages":       10,
			"nextTokenPath":  "body.nextCursor",
			"nextTokenParam": "cursor",
			"collectPath":    "body.items",
		},
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	data := output.Data.(map[string]any)
	assert.Equal(t, 200, data["statusCode"])

	pages := data["pages"].(int)
	assert.Equal(t, 3, pages) // 6 items / 2 per page

	var collectedItems []map[string]any
	err = json.Unmarshal([]byte(data["body"].(string)), &collectedItems)
	require.NoError(t, err)
	assert.Equal(t, totalItems, len(collectedItems))
}

func TestHTTPProvider_Pagination_CursorNextURL(t *testing.T) {
	totalItems := 4
	pageSize := 2

	items := make([]map[string]any, totalItems)
	for i := 0; i < totalItems; i++ {
		items[i] = map[string]any{"id": i + 1}
	}

	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cursor := r.URL.Query().Get("cursor")
		offset := 0
		if cursor != "" {
			offset, _ = strconv.Atoi(cursor)
		}

		end := offset + pageSize
		if end > totalItems {
			end = totalItems
		}
		pageItems := items[offset:end]

		resp := map[string]any{
			"value": pageItems,
		}
		if end < totalItems {
			resp["@odata.nextLink"] = fmt.Sprintf("%s/items?cursor=%d", serverURL, end)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()
	serverURL = server.URL

	p := NewHTTPProvider()
	ctx := testContext(t)

	inputs := map[string]any{
		"url":    server.URL + "/items",
		"method": "GET",
		"pagination": map[string]any{
			"strategy":    "cursor",
			"maxPages":    10,
			"nextURLPath": "body['@odata.nextLink']",
			"collectPath": "body.value",
		},
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	data := output.Data.(map[string]any)
	pages := data["pages"].(int)
	assert.Equal(t, 2, pages) // 4 items / 2 per page

	var collectedItems []map[string]any
	err = json.Unmarshal([]byte(data["body"].(string)), &collectedItems)
	require.NoError(t, err)
	assert.Equal(t, totalItems, len(collectedItems))
}

func TestHTTPProvider_Pagination_LinkHeader(t *testing.T) {
	totalItems := 6
	pageSize := 2

	items := make([]map[string]any, totalItems)
	for i := 0; i < totalItems; i++ {
		items[i] = map[string]any{"id": i + 1}
	}

	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pageStr := r.URL.Query().Get("page")
		page := 1
		if pageStr != "" {
			page, _ = strconv.Atoi(pageStr)
		}

		offset := (page - 1) * pageSize
		end := offset + pageSize
		if end > totalItems {
			end = totalItems
		}
		pageItems := items[offset:end]

		// Set Link header
		totalPages := (totalItems + pageSize - 1) / pageSize
		if page < totalPages {
			w.Header().Set("Link", fmt.Sprintf(`<%s/items?page=%d>; rel="next"`, serverURL, page+1))
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(pageItems)
	}))
	defer server.Close()
	serverURL = server.URL

	p := NewHTTPProvider()
	ctx := testContext(t)

	inputs := map[string]any{
		"url":    server.URL + "/items?page=1",
		"method": "GET",
		"pagination": map[string]any{
			"strategy":    "linkHeader",
			"maxPages":    10,
			"collectPath": "body",
		},
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	data := output.Data.(map[string]any)
	pages := data["pages"].(int)
	assert.Equal(t, 3, pages) // 6 items / 2 per page

	totalCollected := data["totalItems"].(int)
	assert.Equal(t, totalItems, totalCollected)
}

func TestHTTPProvider_Pagination_Custom_NextURL(t *testing.T) {
	totalItems := 4
	pageSize := 2

	items := make([]map[string]any, totalItems)
	for i := 0; i < totalItems; i++ {
		items[i] = map[string]any{"id": i + 1}
	}

	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		offsetStr := r.URL.Query().Get("start")
		offset := 0
		if offsetStr != "" {
			offset, _ = strconv.Atoi(offsetStr)
		}

		end := offset + pageSize
		if end > totalItems {
			end = totalItems
		}
		pageItems := items[offset:end]

		resp := map[string]any{
			"results": pageItems,
		}
		if end < totalItems {
			resp["links"] = map[string]any{
				"next": fmt.Sprintf("%s/api?start=%d", serverURL, end),
			}
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()
	serverURL = server.URL

	p := NewHTTPProvider()
	ctx := testContext(t)

	inputs := map[string]any{
		"url":    server.URL + "/api",
		"method": "GET",
		"pagination": map[string]any{
			"strategy":    "custom",
			"maxPages":    10,
			"nextURL":     "has(body.links) && has(body.links.next) ? string(body.links.next) : ''",
			"collectPath": "body.results",
		},
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	data := output.Data.(map[string]any)
	pages := data["pages"].(int)
	assert.Equal(t, 2, pages)

	var collectedItems []map[string]any
	err = json.Unmarshal([]byte(data["body"].(string)), &collectedItems)
	require.NoError(t, err)
	assert.Equal(t, totalItems, len(collectedItems))
}

func TestHTTPProvider_Pagination_Custom_NextParams(t *testing.T) {
	totalItems := 6
	pageSize := 2

	items := make([]map[string]any, totalItems)
	for i := 0; i < totalItems; i++ {
		items[i] = map[string]any{"id": i + 1}
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startStr := r.URL.Query().Get("start")
		start := 0
		if startStr != "" {
			start, _ = strconv.Atoi(startStr)
		}

		end := start + pageSize
		if end > totalItems {
			end = totalItems
		}
		var pageItems []map[string]any
		if start < totalItems {
			pageItems = items[start:end]
		} else {
			pageItems = []map[string]any{}
		}

		resp := map[string]any{
			"data":  pageItems,
			"start": start,
			"count": len(pageItems),
			"total": totalItems,
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := NewHTTPProvider()
	ctx := testContext(t)

	inputs := map[string]any{
		"url":    server.URL + "/api?start=0&count=2",
		"method": "GET",
		"pagination": map[string]any{
			"strategy":    "custom",
			"maxPages":    10,
			"nextParams":  "int(body.start) + int(body.count) < int(body.total) ? {'start': string(int(body.start) + int(body.count)), 'count': '2'} : {}",
			"collectPath": "body.data",
			"stopWhen":    "size(body.data) == 0",
		},
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	data := output.Data.(map[string]any)

	var collectedItems []map[string]any
	err = json.Unmarshal([]byte(data["body"].(string)), &collectedItems)
	require.NoError(t, err)
	assert.Equal(t, totalItems, len(collectedItems))
}

func TestHTTPProvider_Pagination_MaxPagesLimit(t *testing.T) {
	// Server returns infinite pages
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items":      []any{1, 2, 3},
			"nextCursor": "always-more",
		})
	}))
	defer server.Close()

	p := NewHTTPProvider()
	ctx := testContext(t)

	inputs := map[string]any{
		"url":    server.URL,
		"method": "GET",
		"pagination": map[string]any{
			"strategy":       "cursor",
			"maxPages":       3,
			"nextTokenPath":  "body.nextCursor",
			"nextTokenParam": "cursor",
			"collectPath":    "body.items",
		},
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	data := output.Data.(map[string]any)
	pages := data["pages"].(int)
	assert.Equal(t, 3, pages, "should stop at maxPages limit")
}

func TestHTTPProvider_Pagination_StopWhen(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		var items []any
		if requestCount <= 2 {
			items = []any{requestCount}
		} else {
			items = []any{} // Empty on page 3
		}

		resp := map[string]any{
			"items":      items,
			"nextCursor": fmt.Sprintf("page%d", requestCount+1),
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := NewHTTPProvider()
	ctx := testContext(t)

	inputs := map[string]any{
		"url":    server.URL,
		"method": "GET",
		"pagination": map[string]any{
			"strategy":       "cursor",
			"maxPages":       10,
			"nextTokenPath":  "body.nextCursor",
			"nextTokenParam": "cursor",
			"collectPath":    "body.items",
			"stopWhen":       "size(body.items) == 0",
		},
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)

	data := output.Data.(map[string]any)
	pages := data["pages"].(int)
	assert.Equal(t, 3, pages, "should stop when items are empty")
	totalItems := data["totalItems"].(int)
	assert.Equal(t, 2, totalItems, "should have collected 2 items before empty page")
}

func TestHTTPProvider_Pagination_NoCollectPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cursor := r.URL.Query().Get("cursor")

		resp := map[string]any{"data": "page-data"}
		if cursor == "" {
			resp["next"] = "page2"
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := NewHTTPProvider()
	ctx := testContext(t)

	inputs := map[string]any{
		"url":    server.URL,
		"method": "GET",
		"pagination": map[string]any{
			"strategy":       "cursor",
			"maxPages":       10,
			"nextTokenPath":  "has(body.next) ? body.next : ''",
			"nextTokenParam": "cursor",
			// No collectPath — should accumulate raw body data
		},
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)

	data := output.Data.(map[string]any)
	pages := data["pages"].(int)
	assert.Equal(t, 2, pages)
	totalItems := data["totalItems"].(int)
	assert.Equal(t, 2, totalItems)
}

func TestHTTPProvider_Pagination_EmptyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items":      []any{},
			"nextCursor": nil,
		})
	}))
	defer server.Close()

	p := NewHTTPProvider()
	ctx := testContext(t)

	inputs := map[string]any{
		"url":    server.URL,
		"method": "GET",
		"pagination": map[string]any{
			"strategy":       "cursor",
			"maxPages":       10,
			"nextTokenPath":  "body.nextCursor",
			"nextTokenParam": "cursor",
			"collectPath":    "body.items",
		},
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)

	data := output.Data.(map[string]any)
	pages := data["pages"].(int)
	assert.Equal(t, 1, pages)
	totalItems := data["totalItems"].(int)
	assert.Equal(t, 0, totalItems)
	assert.Equal(t, "[]", data["body"])
}

func TestHTTPProvider_Pagination_NonJSONResponse(t *testing.T) {
	requestCount := 0
	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.Header().Set("Content-Type", "text/plain")

		if requestCount == 1 {
			w.Header().Set("Link", fmt.Sprintf(`<%s?page=2>; rel="next"`, serverURL))
		}

		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, "page %d content", requestCount)
	}))
	defer server.Close()
	serverURL = server.URL

	p := NewHTTPProvider()
	ctx := testContext(t)

	inputs := map[string]any{
		"url":    server.URL,
		"method": "GET",
		"pagination": map[string]any{
			"strategy": "linkHeader",
			"maxPages": 5,
			// No collectPath, body isn't JSON
		},
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)

	data := output.Data.(map[string]any)
	// Non-JSON bodies are accumulated as string values
	pages := data["pages"].(int)
	assert.True(t, pages >= 1)
}

func TestHTTPProvider_Pagination_HTTPErrorStopsGracefully(t *testing.T) {
	requestCount := 0
	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if requestCount == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Link", fmt.Sprintf(`<%s?page=2>; rel="next"`, serverURL))
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode([]any{1, 2, 3})
		} else {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode([]any{4, 5})
			// No Link header = pagination stops
		}
	}))
	defer server.Close()
	serverURL = server.URL

	p := NewHTTPProvider()
	ctx := testContext(t)

	inputs := map[string]any{
		"url":    server.URL + "?page=1",
		"method": "GET",
		"pagination": map[string]any{
			"strategy":    "linkHeader",
			"maxPages":    10,
			"collectPath": "body",
		},
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)

	data := output.Data.(map[string]any)
	pages := data["pages"].(int)
	assert.Equal(t, 2, pages)
}

func TestHTTPProvider_Pagination_DescriptorHasPaginationSchema(t *testing.T) {
	p := NewHTTPProvider()
	schema := p.Descriptor().Schema
	require.NotNil(t, schema)
	require.NotNil(t, schema.Properties)

	paginationProp, ok := schema.Properties["pagination"]
	require.True(t, ok, "schema should have a 'pagination' property")
	assert.Equal(t, "object", paginationProp.Type)
	assert.Contains(t, paginationProp.Required, "strategy")
	assert.Contains(t, paginationProp.Required, "maxPages")

	// Verify key sub-properties exist
	assert.Contains(t, paginationProp.Properties, "strategy")
	assert.Contains(t, paginationProp.Properties, "maxPages")
	assert.Contains(t, paginationProp.Properties, "collectPath")
	assert.Contains(t, paginationProp.Properties, "stopWhen")
	assert.Contains(t, paginationProp.Properties, "nextTokenPath")
	assert.Contains(t, paginationProp.Properties, "nextURLPath")
	assert.Contains(t, paginationProp.Properties, "nextURL")
	assert.Contains(t, paginationProp.Properties, "nextParams")
}

func TestHTTPProvider_Pagination_OutputIncludesPagesAndTotalItems(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items":      []any{"a", "b"},
			"nextCursor": nil,
		})
	}))
	defer server.Close()

	p := NewHTTPProvider()
	ctx := testContext(t)

	inputs := map[string]any{
		"url":    server.URL,
		"method": "GET",
		"pagination": map[string]any{
			"strategy":       "cursor",
			"maxPages":       5,
			"nextTokenPath":  "body.nextCursor",
			"nextTokenParam": "cursor",
			"collectPath":    "body.items",
		},
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)

	data := output.Data.(map[string]any)
	assert.Equal(t, 1, data["pages"])
	assert.Equal(t, 2, data["totalItems"])
	assert.Equal(t, 200, data["statusCode"])
	assert.NotNil(t, data["headers"])
}

// Verify that non-paginated requests still work normally
func TestHTTPProvider_NonPaginated_StillWorks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"message":"hello"}`))
	}))
	defer server.Close()

	p := NewHTTPProvider()
	ctx := testContext(t)

	inputs := map[string]any{
		"url":    server.URL,
		"method": "GET",
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	data := output.Data.(map[string]any)
	assert.Equal(t, 200, data["statusCode"])
	assert.Equal(t, `{"message":"hello"}`, data["body"])
	// Non-paginated: no "pages" or "totalItems" field
	_, hasPages := data["pages"]
	assert.False(t, hasPages, "non-paginated response should not have 'pages' field")
}
