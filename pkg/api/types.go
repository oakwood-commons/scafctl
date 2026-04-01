// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"net/http"
	"time"
)

// PaginationParams are the standard query parameters for paginated list endpoints.
type PaginationParams struct {
	Page    int `query:"page" minimum:"1" maximum:"10000" example:"1" doc:"Page number (1-indexed)"`
	PerPage int `query:"per_page" minimum:"1" maximum:"1000" example:"100" doc:"Items per page"`
}

// FilterParam provides an optional CEL filter expression.
type FilterParam struct {
	Filter string `query:"filter" maxLength:"2000" doc:"CEL filter expression"`
}

// PaginationInfo describes the pagination state in a list response.
type PaginationInfo struct {
	Page       int  `json:"page" minimum:"1" maximum:"10000" example:"1" doc:"Current page number (1-indexed)"`
	PerPage    int  `json:"per_page" minimum:"1" maximum:"1000" example:"100" doc:"Items per page"`
	TotalItems int  `json:"total_items" doc:"Total number of items"`
	TotalPages int  `json:"total_pages" doc:"Total number of pages"`
	HasMore    bool `json:"has_more" doc:"Whether more pages exist"`
}

// NewPaginationInfo creates a PaginationInfo from total count and params.
func NewPaginationInfo(totalItems, page, perPage int) PaginationInfo {
	if perPage <= 0 {
		perPage = 100
	}
	if page <= 0 {
		page = 1
	}
	totalPages := (totalItems + perPage - 1) / perPage
	return PaginationInfo{
		Page:       page,
		PerPage:    perPage,
		TotalItems: totalItems,
		TotalPages: totalPages,
		HasMore:    page < totalPages,
	}
}

// Paginate returns a slice of items for the given page and per-page size.
func Paginate[T any](items []T, page, perPage int) []T {
	if perPage <= 0 {
		perPage = 100
	}
	if page <= 0 {
		page = 1
	}
	start := (page - 1) * perPage
	if start >= len(items) {
		return []T{}
	}
	end := start + perPage
	if end > len(items) {
		end = len(items)
	}
	return items[start:end]
}

// Link represents a HATEOAS-style link.
type Link struct {
	Href string `json:"href" maxLength:"2048" doc:"Link URL"`
	Rel  string `json:"rel" maxLength:"100" doc:"Link relation"`
}

// HealthResponseBody is the response body for the /health endpoint.
type HealthResponseBody struct {
	Status     string            `json:"status" maxLength:"20" example:"healthy" doc:"Overall health status"`
	Version    string            `json:"version" maxLength:"50" doc:"Server version"`
	Uptime     string            `json:"uptime" maxLength:"50" doc:"Server uptime"`
	Components []ComponentStatus `json:"components,omitempty" maxItems:"50" doc:"Component health statuses"`
}

// ComponentStatus describes the health of a single component.
type ComponentStatus struct {
	Name    string `json:"name" maxLength:"100" doc:"Component name"`
	Status  string `json:"status" maxLength:"20" doc:"Component status"`
	Message string `json:"message,omitempty" maxLength:"500" doc:"Status message"`
}

// RootResponseBody is the response body for the API root endpoint.
type RootResponseBody struct {
	Name    string `json:"name" maxLength:"100" doc:"API name"`
	Version string `json:"version" maxLength:"50" doc:"API version"`
	Links   []Link `json:"links,omitempty" maxItems:"50" doc:"Available endpoints"`
}

// HealthResponse wraps the health response for Huma.
type HealthResponse struct {
	Body HealthResponseBody
}

// RootResponse wraps the root response for Huma.
type RootResponse struct {
	Body RootResponseBody
}

// StatusResponse is a minimal status response (e.g., for liveness).
type StatusResponse struct {
	Body struct {
		Status string `json:"status" maxLength:"20" example:"ok" doc:"Status"`
	}
}

// SetLastModified sets the Last-Modified header on the response.
func SetLastModified(w http.ResponseWriter, t time.Time) {
	w.Header().Set("Last-Modified", t.UTC().Format(http.TimeFormat))
}
