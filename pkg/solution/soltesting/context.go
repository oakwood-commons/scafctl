// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package soltesting

// BuildAssertionContext creates the CEL variable map from command output.
// The returned map contains top-level variables that can be referenced
// directly in CEL expressions: __stdout, __stderr, __exitCode, __output, __files.
// The __ prefix follows the convention used elsewhere in scafctl CEL contexts
// (e.g. __self, __item, __execution) and avoids collisions with user-defined names.
//
// When output is nil (stdout was not valid JSON or -o json was not specified),
// the "__output" variable is set to nil. CEL expressions referencing it will be
// caught during evaluation and produce a StatusError result with an appropriate diagnostic.
func BuildAssertionContext(cmdOutput *CommandOutput) map[string]any {
	if cmdOutput == nil {
		return map[string]any{
			"__stdout":   "",
			"__stderr":   "",
			"__exitCode": 0,
			"__output":   nil,
			"__files":    map[string]any{},
		}
	}

	// Convert files map for CEL compatibility
	filesMap := make(map[string]any, len(cmdOutput.Files))
	for k, v := range cmdOutput.Files {
		filesMap[k] = map[string]any{
			"exists":  v.Exists,
			"content": v.Content,
		}
	}

	var output any
	if cmdOutput.Output != nil {
		output = cmdOutput.Output
	}

	return map[string]any{
		"__stdout":   cmdOutput.Stdout,
		"__stderr":   cmdOutput.Stderr,
		"__exitCode": cmdOutput.ExitCode,
		"__output":   output,
		"__files":    filesMap,
	}
}
