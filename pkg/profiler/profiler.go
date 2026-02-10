// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package profiler

type FunctionCallDetails struct {
	Package              string `json:"package" yaml:"package" docs:"The package where the function exists."`
	FileName             string `json:"fileName" yaml:"fileName" docs:"The file where the function exists."`
	Name                 string `json:"name" yaml:"name" docs:"The name of the function."`
	DurationMilliseconds int64  `json:"durationMilliseconds" yaml:"durationMilliseconds" docs:"The duration of the function execution in milliseconds."`
}

type FunctionCallDetailsList []FunctionCallDetails

func (fcd *FunctionCallDetailsList) ToMap() []map[string]interface{} {
	if fcd == nil {
		return nil
	}

	result := make([]map[string]interface{}, len(*fcd))
	for i, details := range *fcd {
		result[i] = map[string]interface{}{
			"package":              details.Package,
			"fileName":             details.FileName,
			"name":                 details.Name,
			"durationMilliseconds": details.DurationMilliseconds,
		}
	}
	return result
}
