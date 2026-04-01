// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package endpoints

import (
	"embed"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/danielgtaylor/huma/v2"
)

//go:embed examples/*.json
var exampleFS embed.FS

// exampleJSON loads and parses a JSON example file from the embedded filesystem.
// Returns nil if the file does not exist or cannot be parsed.
func exampleJSON(name string) any {
	data, err := exampleFS.ReadFile("examples/" + name + ".json")
	if err != nil {
		return nil
	}
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return nil
	}
	return v
}

// addExamples attaches embedded request/response JSON examples to a Huma operation.
// It looks for files named "{operationID}-request.json" and "{operationID}-response.json"
// in the embedded examples/ directory. Missing files are silently skipped.
func addExamples(op huma.Operation, responseStatus int) huma.Operation {
	if op.Method == http.MethodPost || op.Method == http.MethodPut || op.Method == http.MethodPatch {
		if reqData := exampleJSON(op.OperationID + "-request"); reqData != nil {
			op.RequestBody = &huma.RequestBody{
				Content: map[string]*huma.MediaType{
					"application/json": {
						Examples: map[string]*huma.Example{
							"default": {
								Summary: "Example request",
								Value:   reqData,
							},
						},
					},
				},
			}
		}
	}

	if respData := exampleJSON(op.OperationID + "-response"); respData != nil {
		if op.Responses == nil {
			op.Responses = map[string]*huma.Response{}
		}
		statusStr := strconv.Itoa(responseStatus)
		op.Responses[statusStr] = &huma.Response{
			Content: map[string]*huma.MediaType{
				"application/json": {
					Examples: map[string]*huma.Example{
						"default": {
							Summary: "Example response",
							Value:   respData,
						},
					},
				},
			},
		}
	}

	return op
}
