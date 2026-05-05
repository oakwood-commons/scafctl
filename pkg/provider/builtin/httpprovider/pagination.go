// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package httpprovider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"google.golang.org/protobuf/types/known/structpb"

	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/httpc"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
)

// Pagination strategy constants.
const (
	StrategyOffset     = "offset"
	StrategyPageNumber = "pageNumber"
	StrategyCursor     = "cursor"
	StrategyLinkHeader = "linkHeader"
	StrategyCustom     = "custom"
)

// methodSupportsBody returns true for HTTP methods that accept a request body.
func methodSupportsBody(method string) bool {
	switch strings.ToUpper(method) {
	case "POST", "PUT", "PATCH":
		return true
	default:
		return false
	}
}

// paginationConfig holds parsed pagination configuration for HTTP requests.
type paginationConfig struct {
	Strategy string // "offset", "pageNumber", "cursor", "linkHeader", "custom"
	MaxPages int    // Safety limit to prevent infinite loops (required)

	// --- offset strategy ---
	OffsetParam string // Query parameter name for offset (default: "offset")
	LimitParam  string // Query parameter name for limit (default: "limit")
	Limit       int    // Page size (required for offset strategy)

	// --- pageNumber strategy ---
	PageParam     string // Query parameter name for page number (default: "page")
	PageSizeParam string // Query parameter name for page size (default: "pageSize")
	PageSize      int    // Page size (required for pageNumber strategy)
	StartPage     int    // Starting page number (default: 1)

	// --- cursor strategy ---
	NextTokenPath  string // CEL expression to extract next cursor/token from response body
	NextTokenParam string // Query parameter name to set the cursor/token on the next request
	NextURLPath    string // CEL expression to extract full next URL from response body (alternative to NextTokenParam)

	// --- custom strategy ---
	NextURL    string // CEL expression that returns the full URL for the next request (null/empty = stop)
	NextParams string // CEL expression that returns a map of query params for the next request

	// --- body template (POST body pagination) ---
	BodyTemplate string // CEL expression to generate request body per-page (evaluated for every page)

	// --- universal ---
	StopWhen    string // CEL expression evaluated against each response; true = stop paginating
	CollectPath string // CEL expression to extract items from each page's response body
}

// defaultPaginationConfig returns defaults for pagination config.
func defaultPaginationConfig() paginationConfig {
	return paginationConfig{
		MaxPages:      100,
		StartPage:     1,
		OffsetParam:   "offset",
		LimitParam:    "limit",
		PageParam:     "page",
		PageSizeParam: "pageSize",
	}
}

// pageSize returns the effective page size for the configured strategy.
// Returns Limit for offset, PageSize for pageNumber. For other strategies,
// returns PageSize if explicitly set (for bodyTemplate use), otherwise 0.
func (c *paginationConfig) pageSize() int {
	switch c.Strategy {
	case StrategyOffset:
		return c.Limit
	case StrategyPageNumber:
		return c.PageSize
	default:
		// PageSize may be explicitly set for bodyTemplate usage with cursor/linkHeader/custom
		return c.PageSize
	}
}

// initialOffset parses the initial offset value from the URL's query parameters
// for the configured offset strategy. Returns 0 if not present or not applicable.
func initialOffset(urlStr string, cfg *paginationConfig) int {
	if cfg.Strategy != StrategyOffset {
		return 0
	}
	u, err := url.Parse(urlStr)
	if err != nil {
		return 0
	}
	if v := u.Query().Get(cfg.OffsetParam); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return 0
}

// parsePaginationConfig parses pagination configuration from inputs.
// Returns nil if no pagination configuration is present.
func parsePaginationConfig(inputs map[string]any) (*paginationConfig, error) {
	paginationInput, ok := inputs["pagination"]
	if !ok || paginationInput == nil {
		return nil, nil
	}

	pagMap, ok := paginationInput.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("pagination: expected object, got %T", paginationInput)
	}

	cfg := defaultPaginationConfig()

	// Required field: strategy
	strategy, ok := pagMap["strategy"].(string)
	if !ok || strategy == "" {
		return nil, fmt.Errorf("pagination: strategy is required")
	}
	switch strategy {
	case StrategyOffset, StrategyPageNumber, StrategyCursor, StrategyLinkHeader, StrategyCustom:
		cfg.Strategy = strategy
	default:
		return nil, fmt.Errorf("pagination: unknown strategy %q (must be one of: offset, pageNumber, cursor, linkHeader, custom)", strategy)
	}

	// Required field: maxPages
	if mp, ok := pagMap["maxPages"].(int); ok && mp > 0 {
		cfg.MaxPages = mp
	}
	if mp, ok := pagMap["maxPages"].(float64); ok && mp > 0 {
		cfg.MaxPages = int(mp)
	}

	// Universal optional fields
	if sw, ok := pagMap["stopWhen"].(string); ok {
		cfg.StopWhen = sw
	}
	if cp, ok := pagMap["collectPath"].(string); ok {
		cfg.CollectPath = cp
	}
	if bt, ok := pagMap["bodyTemplate"].(string); ok {
		cfg.BodyTemplate = bt
	}

	// Strategy-specific fields
	switch cfg.Strategy {
	case StrategyOffset:
		if op, ok := pagMap["offsetParam"].(string); ok && op != "" {
			cfg.OffsetParam = op
		}
		if lp, ok := pagMap["limitParam"].(string); ok && lp != "" {
			cfg.LimitParam = lp
		}
		if l, ok := pagMap["limit"].(int); ok && l > 0 {
			cfg.Limit = l
		}
		if l, ok := pagMap["limit"].(float64); ok && l > 0 {
			cfg.Limit = int(l)
		}
		if cfg.Limit <= 0 {
			return nil, fmt.Errorf("pagination: limit is required for offset strategy")
		}

	case StrategyPageNumber:
		if pp, ok := pagMap["pageParam"].(string); ok && pp != "" {
			cfg.PageParam = pp
		}
		if psp, ok := pagMap["pageSizeParam"].(string); ok && psp != "" {
			cfg.PageSizeParam = psp
		}
		if ps, ok := pagMap["pageSize"].(int); ok && ps > 0 {
			cfg.PageSize = ps
		}
		if ps, ok := pagMap["pageSize"].(float64); ok && ps > 0 {
			cfg.PageSize = int(ps)
		}
		if sp, ok := pagMap["startPage"].(int); ok && sp >= 0 {
			cfg.StartPage = sp
		}
		if sp, ok := pagMap["startPage"].(float64); ok && sp >= 0 {
			cfg.StartPage = int(sp)
		}
		if cfg.PageSize <= 0 {
			return nil, fmt.Errorf("pagination: pageSize is required for pageNumber strategy")
		}

	case StrategyCursor:
		if ntp, ok := pagMap["nextTokenPath"].(string); ok {
			cfg.NextTokenPath = ntp
		}
		if ntparam, ok := pagMap["nextTokenParam"].(string); ok {
			cfg.NextTokenParam = ntparam
		}
		if nup, ok := pagMap["nextURLPath"].(string); ok {
			cfg.NextURLPath = nup
		}
		// Allow pageSize for cursor strategy (used by bodyTemplate's __pageSize)
		if ps, ok := pagMap["pageSize"].(int); ok && ps > 0 {
			cfg.PageSize = ps
		}
		if ps, ok := pagMap["pageSize"].(float64); ok && ps > 0 {
			cfg.PageSize = int(ps)
		}
		// Validate: must have nextTokenPath or nextURLPath
		if cfg.NextTokenPath == "" && cfg.NextURLPath == "" {
			return nil, fmt.Errorf("pagination: cursor strategy requires nextTokenPath or nextURLPath")
		}
		// If nextTokenPath is set, nextTokenParam is required unless bodyTemplate handles pagination
		if cfg.NextTokenPath != "" && cfg.NextTokenParam == "" && cfg.BodyTemplate == "" {
			return nil, fmt.Errorf("pagination: nextTokenParam is required when nextTokenPath is set (without bodyTemplate)")
		}

	case StrategyLinkHeader:
		// No additional config required — follows rel="next" automatically
		// Allow pageSize for bodyTemplate's __pageSize
		if ps, ok := pagMap["pageSize"].(int); ok && ps > 0 {
			cfg.PageSize = ps
		}
		if ps, ok := pagMap["pageSize"].(float64); ok && ps > 0 {
			cfg.PageSize = int(ps)
		}

	case StrategyCustom:
		if nu, ok := pagMap["nextURL"].(string); ok {
			cfg.NextURL = nu
		}
		if np, ok := pagMap["nextParams"].(string); ok {
			cfg.NextParams = np
		}
		if cfg.NextURL == "" && cfg.NextParams == "" {
			return nil, fmt.Errorf("pagination: custom strategy requires nextURL or nextParams")
		}
		// Allow pageSize for bodyTemplate's __pageSize
		if ps, ok := pagMap["pageSize"].(int); ok && ps > 0 {
			cfg.PageSize = ps
		}
		if ps, ok := pagMap["pageSize"].(float64); ok && ps > 0 {
			cfg.PageSize = int(ps)
		}
	}

	return &cfg, nil
}

// paginatedResponse holds the response data from a single page.
type paginatedResponse struct {
	StatusCode int
	Body       string
	Headers    map[string]any
}

// executePaginated performs paginated HTTP requests using the configured strategy.
// It collects items from each page and returns a merged output.
func (p *HTTPProvider) executePaginated(
	ctx context.Context,
	client *httpc.Client,
	method, urlStr, bodyContent string,
	headers map[string]any,
	pagCfg *paginationConfig,
) (*provider.Output, error) {
	lgr := logger.FromContext(ctx)

	var allItems []any
	var lastResponse *paginatedResponse
	pageCount := 0
	currentURL := urlStr

	// Track state for bodyTemplate context variables.
	// __page starts from startPage (or 1 for non-pageNumber strategies).
	// __offset starts from the initial URL's offset param (or 0).
	var lastCursor string
	currentPage := pagCfg.StartPage
	currentOffset := initialOffset(urlStr, pagCfg)

	for pageCount < pagCfg.MaxPages {
		pageCount++
		lgr.V(1).Info("fetching page", "provider", ProviderName, "page", pageCount, "url", currentURL)

		// Evaluate bodyTemplate if configured, replacing the request body for this page.
		if pagCfg.BodyTemplate != "" {
			newBody, err := p.evaluateBodyTemplate(ctx, pagCfg, currentPage, currentOffset, lastCursor)
			if err != nil {
				return nil, fmt.Errorf("%s: page %d bodyTemplate evaluation failed: %w", ProviderName, pageCount, err)
			}
			bodyContent = newBody
		}

		// Execute the request for this page
		resp, err := p.doRequest(ctx, client, method, currentURL, bodyContent, headers)
		if err != nil {
			return nil, fmt.Errorf("%s: page %d request failed: %w", ProviderName, pageCount, err)
		}
		lastResponse = resp

		// Parse body as JSON for CEL evaluation
		var bodyData any
		if resp.Body != "" {
			if err := json.Unmarshal([]byte(resp.Body), &bodyData); err != nil {
				// If body isn't JSON, make it available as a string
				bodyData = resp.Body
			}
		}

		// Build response context for CEL expressions
		responseCtx := map[string]any{
			"statusCode": resp.StatusCode,
			"body":       bodyData,
			"rawBody":    resp.Body,
			"headers":    resp.Headers,
			"page":       pageCount,
		}

		// Collect items from this page.
		// Use evaluateCELForPagination so that "no such key" errors (e.g.,
		// the API returns a different envelope on the last page) are treated
		// as an empty result instead of a hard failure.
		if pagCfg.CollectPath != "" {
			items, err := evaluateCELForPagination(ctx, pagCfg.CollectPath, responseCtx)
			if err != nil {
				return nil, fmt.Errorf("%s: page %d collectPath evaluation failed: %w", ProviderName, pageCount, err)
			}
			if itemSlice, ok := items.([]any); ok {
				allItems = append(allItems, itemSlice...)
			} else if items != nil {
				allItems = append(allItems, items)
			}
		} else if bodyData != nil {
			// No collectPath: accumulate the raw body data
			allItems = append(allItems, bodyData)
		}

		// Check stopWhen condition.
		// Use evaluateCELForPagination so that "no such key" errors (e.g.,
		// size(body.items) when the last page omits "items") are treated
		// as a stop signal rather than a hard failure.
		if pagCfg.StopWhen != "" {
			shouldStop, err := evaluateCELForPagination(ctx, pagCfg.StopWhen, responseCtx)
			if err != nil {
				return nil, fmt.Errorf("%s: page %d stopWhen evaluation failed: %w", ProviderName, pageCount, err)
			}
			// Treat nil (from "no such key") as a stop signal:
			// if the expected field is missing, the page has no usable data.
			if shouldStop == nil {
				lgr.V(1).Info("pagination stopped: stopWhen field not found in response", "page", pageCount)
				break
			}
			if boolVal, ok := shouldStop.(bool); ok && boolVal {
				lgr.V(1).Info("pagination stopped by stopWhen condition", "page", pageCount)
				break
			}
		}

		// When bodyTemplate is set, pagination state lives in the body — do not
		// mutate the URL. We still evaluate response-driven stop signals from
		// each strategy so that linkHeader, custom, and cursor+nextURLPath
		// correctly detect end-of-pagination.
		if pagCfg.BodyTemplate != "" {
			// Check response-driven stop signals per strategy.
			bodyStop := false
			switch pagCfg.Strategy {
			case StrategyCursor:
				if pagCfg.NextURLPath != "" {
					result, err := evaluateCELForPagination(ctx, pagCfg.NextURLPath, responseCtx)
					if err != nil {
						return nil, fmt.Errorf("%s: page %d nextURLPath evaluation failed: %w", ProviderName, pageCount, err)
					}
					if isNullOrEmpty(result) {
						lgr.V(1).Info("pagination completed", "page", pageCount, "reason", "nextURLPath exhausted")
						bodyStop = true
					} else if s, ok := result.(string); !ok || s == "" {
						lgr.V(1).Info("pagination completed", "page", pageCount, "reason", "nextURLPath exhausted")
						bodyStop = true
					}
				} else if pagCfg.NextTokenPath != "" {
					cursor, err := evaluateCELForPagination(ctx, pagCfg.NextTokenPath, responseCtx)
					if err != nil {
						return nil, fmt.Errorf("%s: page %d nextTokenPath evaluation failed: %w", ProviderName, pageCount, err)
					}
					if isNullOrEmpty(cursor) {
						lgr.V(1).Info("pagination completed", "page", pageCount, "reason", "cursor exhausted")
						bodyStop = true
					} else if s, ok := cursor.(string); ok {
						lastCursor = s
					} else {
						lastCursor = fmt.Sprintf("%v", cursor)
					}
				}
			case StrategyLinkHeader:
				// Only check whether a Link rel="next" header exists — don't
				// validate the URL since we won't follow it in bodyTemplate mode.
				if !hasLinkNextHeader(resp) {
					lgr.V(1).Info("pagination completed", "page", pageCount, "reason", "no Link rel=next header")
					bodyStop = true
				}
			case StrategyCustom:
				// Only check whether the custom expressions signal stop — don't
				// validate the URL since we won't follow it in bodyTemplate mode.
				stop, err := checkCustomStop(ctx, pagCfg, responseCtx)
				if err != nil {
					return nil, fmt.Errorf("%s: page %d custom stop evaluation failed: %w", ProviderName, pageCount, err)
				}
				if stop {
					lgr.V(1).Info("pagination completed", "page", pageCount, "reason", "custom expression signaled stop")
					bodyStop = true
				}
			case StrategyOffset, StrategyPageNumber:
				// Apply short-page stop heuristic: if the response body is a
				// bare array with fewer items than pageSize, we're on the last page.
				if pageCount > 1 {
					if checkItemCountStop(responseCtx, pagCfg) {
						lgr.V(1).Info("pagination completed", "page", pageCount, "reason", "short page detected")
						bodyStop = true
					}
				}
			}
			if bodyStop {
				break
			}

			// Advance bodyTemplate state for next iteration.
			currentPage++
			currentOffset += pagCfg.pageSize()
		} else {
			// URL-based pagination: resolve the next page URL via strategy.
			nextURL, stop, err := p.resolveNextPage(ctx, pagCfg, currentURL, resp, responseCtx)
			if err != nil {
				return nil, fmt.Errorf("%s: page %d failed to resolve next page: %w", ProviderName, pageCount, err)
			}
			if stop {
				lgr.V(1).Info("pagination completed", "page", pageCount, "reason", "no more pages")
				break
			}
			currentURL = nextURL
		}
	}

	lgr.V(1).Info("pagination finished", "totalPages", pageCount, "totalItems", len(allItems))

	// Build output
	return p.buildPaginatedOutput(lastResponse, allItems, pageCount)
}

// doRequest performs a single HTTP request and returns the parsed response.
func (p *HTTPProvider) doRequest(
	ctx context.Context,
	client *httpc.Client,
	method, urlStr, bodyContent string,
	headers map[string]any,
) (*paginatedResponse, error) {
	if !privateIPsAllowed(ctx) {
		if err := validateURLNotPrivate(urlStr); err != nil {
			return nil, fmt.Errorf("%s: %w", ProviderName, err)
		}
	}

	var bodyReader io.Reader
	if bodyContent != "" {
		bodyReader = strings.NewReader(bodyContent)
	}

	req, err := http.NewRequestWithContext(ctx, method, urlStr, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Provide GetBody so the httpc OnUnauthorized hook can replay the body on an auth retry.
	if bodyContent != "" {
		capturedBody := bodyContent
		req.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader(capturedBody)), nil
		}
	}

	for key, value := range headers {
		if strValue, ok := value.(string); ok {
			req.Header.Set(key, strValue)
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Limit the response body size to prevent denial-of-service via unbounded
	// responses. The limit is configurable via httpClient.maxResponseBodySize.
	limit := maxResponseBodySize(ctx)
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}
	if int64(len(respBody)) > limit {
		return nil, fmt.Errorf("response body exceeds maximum size of %d bytes", limit)
	}

	respHeaders := make(map[string]any)
	for key, values := range resp.Header {
		if len(values) == 1 {
			respHeaders[key] = values[0]
		} else {
			respHeaders[key] = values
		}
	}

	return &paginatedResponse{
		StatusCode: resp.StatusCode,
		Body:       string(respBody),
		Headers:    respHeaders,
	}, nil
}

// resolveNextPage determines the URL for the next page based on the pagination strategy.
// Returns (nextURL, stop, error). If stop is true, pagination should end.
func (p *HTTPProvider) resolveNextPage(
	ctx context.Context,
	pagCfg *paginationConfig,
	currentURL string,
	resp *paginatedResponse,
	responseCtx map[string]any,
) (string, bool, error) {
	switch pagCfg.Strategy {
	case StrategyOffset:
		return resolveOffsetNext(currentURL, pagCfg, responseCtx)
	case StrategyPageNumber:
		return resolvePageNumberNext(currentURL, pagCfg, responseCtx)
	case StrategyCursor:
		return resolveCursorNext(ctx, currentURL, pagCfg, responseCtx)
	case StrategyLinkHeader:
		return resolveLinkHeaderNext(currentURL, resp)
	case StrategyCustom:
		return resolveCustomNext(ctx, currentURL, pagCfg, responseCtx)
	default:
		return "", true, fmt.Errorf("unknown pagination strategy: %q", pagCfg.Strategy)
	}
}

// resolveOffsetNext increments the offset parameter for the next page.
func resolveOffsetNext(currentURL string, pagCfg *paginationConfig, responseCtx map[string]any) (string, bool, error) {
	u, err := url.Parse(currentURL)
	if err != nil {
		return "", true, fmt.Errorf("failed to parse URL: %w", err)
	}

	q := u.Query()

	// Determine current offset
	currentOffset := 0
	if offsetStr := q.Get(pagCfg.OffsetParam); offsetStr != "" {
		if parsed, err := strconv.Atoi(offsetStr); err == nil {
			currentOffset = parsed
		}
	}

	// Check if we got fewer items than the limit (last page)
	page, _ := responseCtx["page"].(int)
	if page > 1 {
		if shouldStop := checkItemCountStop(responseCtx, pagCfg); shouldStop {
			return "", true, nil
		}
	}

	// Set next offset
	nextOffset := currentOffset + pagCfg.Limit
	q.Set(pagCfg.OffsetParam, strconv.Itoa(nextOffset))
	q.Set(pagCfg.LimitParam, strconv.Itoa(pagCfg.Limit))
	u.RawQuery = q.Encode()

	return u.String(), false, nil
}

// resolvePageNumberNext increments the page number parameter for the next page.
func resolvePageNumberNext(currentURL string, pagCfg *paginationConfig, responseCtx map[string]any) (string, bool, error) {
	u, err := url.Parse(currentURL)
	if err != nil {
		return "", true, fmt.Errorf("failed to parse URL: %w", err)
	}

	q := u.Query()

	// Determine current page
	currentPage := pagCfg.StartPage
	if pageStr := q.Get(pagCfg.PageParam); pageStr != "" {
		if parsed, err := strconv.Atoi(pageStr); err == nil {
			currentPage = parsed
		}
	}

	// Check if we got fewer items than pageSize (last page)
	page, _ := responseCtx["page"].(int)
	if page > 1 {
		if shouldStop := checkItemCountStop(responseCtx, pagCfg); shouldStop {
			return "", true, nil
		}
	}

	// Set next page
	nextPage := currentPage + 1
	q.Set(pagCfg.PageParam, strconv.Itoa(nextPage))
	q.Set(pagCfg.PageSizeParam, strconv.Itoa(pagCfg.PageSize))
	u.RawQuery = q.Encode()

	return u.String(), false, nil
}

// isNullOrEmpty checks if a CEL evaluation result represents an absent/null/empty value.
// CEL null values are returned as structpb.NullValue (int32) after type conversion.
func isNullOrEmpty(result any) bool {
	if result == nil {
		return true
	}
	// CEL null values come through as structpb.NullValue after conversion
	if _, ok := result.(structpb.NullValue); ok {
		return true
	}
	if s, ok := result.(string); ok {
		return s == ""
	}
	return false
}

// resolveCursorNext extracts the cursor/token from the response and builds the next URL.
func resolveCursorNext(ctx context.Context, currentURL string, pagCfg *paginationConfig, responseCtx map[string]any) (string, bool, error) {
	// If nextURLPath is set, extract the full URL from the response body
	if pagCfg.NextURLPath != "" {
		result, err := evaluateCELForPagination(ctx, pagCfg.NextURLPath, responseCtx)
		if err != nil {
			return "", true, fmt.Errorf("nextURLPath evaluation failed: %w", err)
		}
		if isNullOrEmpty(result) {
			return "", true, nil // No more pages
		}
		nextURL, ok := result.(string)
		if !ok || nextURL == "" {
			return "", true, nil // No more pages
		}
		if err := validateNextURLHost(currentURL, nextURL); err != nil {
			return "", true, err
		}
		return nextURL, false, nil
	}

	// Extract cursor token from response body
	result, err := evaluateCELForPagination(ctx, pagCfg.NextTokenPath, responseCtx)
	if err != nil {
		return "", true, fmt.Errorf("nextTokenPath evaluation failed: %w", err)
	}
	if isNullOrEmpty(result) {
		return "", true, nil // No more pages
	}
	nextToken, ok := result.(string)
	if !ok {
		// Try numeric token
		nextToken = fmt.Sprintf("%v", result)
	}
	if nextToken == "" {
		return "", true, nil // No more pages
	}

	// Set the cursor token as a query parameter
	u, err := url.Parse(currentURL)
	if err != nil {
		return "", true, fmt.Errorf("failed to parse URL: %w", err)
	}
	q := u.Query()
	q.Set(pagCfg.NextTokenParam, nextToken)
	u.RawQuery = q.Encode()

	return u.String(), false, nil
}

// linkHeaderRegex matches Link header entries like: <url>; rel="next"
var linkHeaderRegex = regexp.MustCompile(`<([^>]+)>;\s*rel="next"`)

// resolveLinkHeaderNext extracts the next URL from the Link response header (RFC 8288).
func resolveLinkHeaderNext(currentURL string, resp *paginatedResponse) (string, bool, error) {
	linkHeader, ok := resp.Headers["Link"]
	if !ok {
		return "", true, nil // No Link header = no more pages
	}

	var linkStr string
	switch v := linkHeader.(type) {
	case string:
		linkStr = v
	case []string:
		linkStr = strings.Join(v, ", ")
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				parts = append(parts, s)
			}
		}
		linkStr = strings.Join(parts, ", ")
	default:
		return "", true, nil
	}

	matches := linkHeaderRegex.FindStringSubmatch(linkStr)
	if len(matches) < 2 {
		return "", true, nil // No rel="next" link
	}

	nextURL := matches[1]
	if err := validateNextURLHost(currentURL, nextURL); err != nil {
		return "", true, err
	}
	return nextURL, false, nil
}

// resolveCustomNext uses CEL expressions to determine the next page URL.
func resolveCustomNext(ctx context.Context, currentURL string, pagCfg *paginationConfig, responseCtx map[string]any) (string, bool, error) {
	// If nextURL expression is set, evaluate it for the full URL
	if pagCfg.NextURL != "" {
		result, err := evaluateCELForPagination(ctx, pagCfg.NextURL, responseCtx)
		if err != nil {
			return "", true, fmt.Errorf("nextURL evaluation failed: %w", err)
		}
		if isNullOrEmpty(result) {
			return "", true, nil
		}
		nextURL, ok := result.(string)
		if !ok || nextURL == "" {
			return "", true, nil
		}
		if err := validateNextURLHost(currentURL, nextURL); err != nil {
			return "", true, err
		}
		return nextURL, false, nil
	}

	// If nextParams expression is set, evaluate it for query parameter updates
	if pagCfg.NextParams != "" {
		result, err := evaluateCELForPagination(ctx, pagCfg.NextParams, responseCtx)
		if err != nil {
			return "", true, fmt.Errorf("nextParams evaluation failed: %w", err)
		}
		if isNullOrEmpty(result) {
			return "", true, nil
		}

		paramsMap, ok := result.(map[string]any)
		if !ok {
			return "", true, fmt.Errorf("nextParams must evaluate to a map, got %T", result)
		}
		if len(paramsMap) == 0 {
			return "", true, nil // Empty map = no more pages
		}

		u, err := url.Parse(currentURL)
		if err != nil {
			return "", true, fmt.Errorf("failed to parse URL: %w", err)
		}
		q := u.Query()
		for k, v := range paramsMap {
			q.Set(k, fmt.Sprintf("%v", v))
		}
		u.RawQuery = q.Encode()
		return u.String(), false, nil
	}

	return "", true, nil
}

// hasLinkNextHeader checks whether the response contains a Link header with rel="next".
// Used in bodyTemplate mode to detect end-of-pagination without host validation.
func hasLinkNextHeader(resp *paginatedResponse) bool {
	linkHeader, ok := resp.Headers["Link"]
	if !ok {
		return false
	}

	var linkStr string
	switch v := linkHeader.(type) {
	case string:
		linkStr = v
	case []string:
		linkStr = strings.Join(v, ", ")
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				parts = append(parts, s)
			}
		}
		linkStr = strings.Join(parts, ", ")
	default:
		return false
	}

	return linkHeaderRegex.MatchString(linkStr)
}

// checkCustomStop evaluates custom nextURL/nextParams expressions to determine if
// pagination should stop. Used in bodyTemplate mode to check stop signals without
// host validation (since the URL is not followed).
func checkCustomStop(ctx context.Context, pagCfg *paginationConfig, responseCtx map[string]any) (bool, error) {
	if pagCfg.NextURL != "" {
		result, err := evaluateCELForPagination(ctx, pagCfg.NextURL, responseCtx)
		if err != nil {
			return true, fmt.Errorf("nextURL evaluation failed: %w", err)
		}
		if isNullOrEmpty(result) {
			return true, nil
		}
		nextURL, ok := result.(string)
		if !ok || nextURL == "" {
			return true, nil
		}
		return false, nil
	}

	if pagCfg.NextParams != "" {
		result, err := evaluateCELForPagination(ctx, pagCfg.NextParams, responseCtx)
		if err != nil {
			return true, fmt.Errorf("nextParams evaluation failed: %w", err)
		}
		if isNullOrEmpty(result) {
			return true, nil
		}
		paramsMap, ok := result.(map[string]any)
		if !ok {
			return true, fmt.Errorf("nextParams must evaluate to a map, got %T", result)
		}
		if len(paramsMap) == 0 {
			return true, nil
		}
		return false, nil
	}

	return true, nil
}

// checkItemCountStop checks if pagination should stop based on collected item count.
// For offset and pageNumber strategies, if the response body is a JSON array and
// has fewer items than the page size, we've reached the last page.
func checkItemCountStop(responseCtx map[string]any, pagCfg *paginationConfig) bool {
	bodyData := responseCtx["body"]

	pageSize := pagCfg.Limit
	if pagCfg.Strategy == StrategyPageNumber {
		pageSize = pagCfg.PageSize
	}

	// If body is directly an array
	if arr, ok := bodyData.([]any); ok {
		return len(arr) < pageSize
	}

	return false
}

// evaluateCELForPagination evaluates a CEL expression for pagination control.
// Unlike evaluateCEL, key-access errors (e.g., "no such key") are treated as nil
// results rather than errors, since a missing key typically means "no more pages".
func evaluateCELForPagination(ctx context.Context, expression string, responseCtx map[string]any) (any, error) {
	result, err := celexp.EvaluateExpression(ctx, expression, nil, responseCtx)
	if err != nil {
		// Treat "no such key" errors as nil (no more pages)
		if strings.Contains(err.Error(), "no such key") {
			return nil, nil
		}
		return nil, err
	}
	return result, nil
}

// evaluateBodyTemplate evaluates a bodyTemplate CEL expression to produce the request body
// for the current page. The expression receives pagination context variables:
// __page (1-indexed page number), __pageSize, __offset, __cursor.
func (p *HTTPProvider) evaluateBodyTemplate(
	ctx context.Context,
	pagCfg *paginationConfig,
	page, offset int,
	cursor string,
) (string, error) {
	bodyCtx := map[string]any{
		"__page":     page,
		"__pageSize": pagCfg.pageSize(),
		"__offset":   offset,
		"__cursor":   cursor,
	}

	result, err := celexp.EvaluateExpression(ctx, pagCfg.BodyTemplate, nil, bodyCtx)
	if err != nil {
		return "", err
	}

	if isNullOrEmpty(result) {
		return "", nil
	}

	switch v := result.(type) {
	case string:
		return v, nil
	default:
		// Non-string results (maps, slices) are marshaled to JSON
		jsonBytes, err := json.Marshal(v)
		if err != nil {
			return "", fmt.Errorf("failed to marshal bodyTemplate result to JSON: %w", err)
		}
		return string(jsonBytes), nil
	}
}

// buildPaginatedOutput constructs the provider output for paginated requests.
func (p *HTTPProvider) buildPaginatedOutput(lastResponse *paginatedResponse, items []any, pageCount int) (*provider.Output, error) {
	// Marshal collected items to JSON for the body field
	var bodyStr string
	if len(items) > 0 {
		bodyBytes, err := json.Marshal(items)
		if err != nil {
			return nil, fmt.Errorf("%s: failed to marshal collected items: %w", ProviderName, err)
		}
		bodyStr = string(bodyBytes)
	} else {
		bodyStr = "[]"
	}

	var lastStatusCode int
	var lastHeaders map[string]any
	if lastResponse != nil {
		lastStatusCode = lastResponse.StatusCode
		lastHeaders = lastResponse.Headers
	}

	return &provider.Output{
		Data: map[string]any{
			"statusCode": lastStatusCode,
			"body":       bodyStr,
			"headers":    lastHeaders,
			"pages":      pageCount,
			"totalItems": len(items),
		},
	}, nil
}
