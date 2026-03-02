// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package kvx

import (
	"context"

	"github.com/google/cel-go/cel"
	"github.com/oakwood-commons/kvx/pkg/tui"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/celexp/env"
	"github.com/oakwood-commons/scafctl/pkg/celexp/ext"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
)

// EvaluateWithScafctlCEL evaluates a CEL expression using scafctl's CEL environment.
// The data is made available under the "_" variable (kvx convention for root data).
// All scafctl CEL extension functions are available.
//
// The ctx parameter is used for context propagation (e.g., Writer for debug.out support).
// Use writer.WithWriter(ctx, w) to enable debug.out output.
func EvaluateWithScafctlCEL(ctx context.Context, expr string, root any) (any, error) {
	// Use EvaluateExpression which handles the full scafctl CEL environment
	// The root data is passed directly - it can be any type (map, slice, string, etc.)
	result, err := celexp.EvaluateExpression(ctx, expr, root, nil)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// SetupScafctlCELProvider configures kvx TUI to use scafctl's CEL environment.
// This enables all scafctl CEL extension functions (base64Encode, jsonEncode, etc.)
// to be available in the interactive TUI expression evaluation.
//
// The Writer parameter is used by debug.out for debug output. Pass nil if
// debug output is not needed.
func SetupScafctlCELProvider(w *writer.Writer) error {
	ctx := context.Background()

	// Create scafctl's CEL environment with all extensions and the "_" variable for kvx compatibility
	// Use NewWithWriter to include debug.out support
	celEnv, err := env.NewWithWriter(ctx, w, cel.Variable("_", cel.DynType))
	if err != nil {
		return err
	}

	// Build hints map for scafctl-specific functions to show helpful examples in TUI
	hints := buildFunctionHints()

	// Create and set the expression provider
	provider := tui.NewCELExpressionProvider(celEnv, hints)
	tui.SetExpressionProvider(provider)

	return nil
}

// buildFunctionHints returns a map of function names to example usage strings.
// These are displayed in the TUI to help users discover available functions.
func buildFunctionHints() map[string]string {
	hints := make(map[string]string)

	// Get all extension functions to build hints dynamically
	extFuncs := ext.All()
	for _, extFunc := range extFuncs {
		// Add hints for commonly used functions
		// The hint should show a practical example
		switch extFunc.Name {
		// Encoding functions
		case "base64Encode":
			hints[extFunc.Name] = "e.g. base64Encode(_.secret)"
		case "base64Decode":
			hints[extFunc.Name] = "e.g. base64Decode(_.encoded)"
		case "jsonEncode":
			hints[extFunc.Name] = "e.g. jsonEncode(_.config)"
		case "jsonDecode":
			hints[extFunc.Name] = "e.g. jsonDecode(_.jsonStr)"
		case "yamlEncode":
			hints[extFunc.Name] = "e.g. yamlEncode(_.config)"
		case "yamlDecode":
			hints[extFunc.Name] = "e.g. yamlDecode(_.yamlStr)"
		case "urlEncode":
			hints[extFunc.Name] = "e.g. urlEncode(_.queryParam)"
		case "urlDecode":
			hints[extFunc.Name] = "e.g. urlDecode(_.encoded)"

		// String functions
		case "regex.match":
			hints[extFunc.Name] = "e.g. regex.match('^test', _.name)"
		case "regex.replace":
			hints[extFunc.Name] = "e.g. regex.replace(_.text, '[0-9]+', '#')"
		case "regex.findAll":
			hints[extFunc.Name] = "e.g. regex.findAll('[0-9]+', _.text)"
		case "regex.split":
			hints[extFunc.Name] = "e.g. regex.split('[,;]+', _.csv)"
		case "split":
			hints[extFunc.Name] = "e.g. split(_.csv, ',')"
		case "join":
			hints[extFunc.Name] = "e.g. join(_.list, ', ')"
		case "trim":
			hints[extFunc.Name] = "e.g. trim(_.input)"
		case "lower":
			hints[extFunc.Name] = "e.g. lower(_.name)"
		case "upper":
			hints[extFunc.Name] = "e.g. upper(_.name)"

		// Collection functions
		case "keys":
			hints[extFunc.Name] = "e.g. keys(_.config)"
		case "values":
			hints[extFunc.Name] = "e.g. values(_.config)"
		case "flatten":
			hints[extFunc.Name] = "e.g. flatten(_.nested)"
		case "unique":
			hints[extFunc.Name] = "e.g. unique(_.items)"
		case "sort":
			hints[extFunc.Name] = "e.g. sort(_.numbers)"
		case "reverse":
			hints[extFunc.Name] = "e.g. reverse(_.list)"

		// Type conversion
		case "toString":
			hints[extFunc.Name] = "e.g. toString(_.number)"
		case "toInt":
			hints[extFunc.Name] = "e.g. toInt(_.strNum)"
		case "toFloat":
			hints[extFunc.Name] = "e.g. toFloat(_.strFloat)"
		case "toBool":
			hints[extFunc.Name] = "e.g. toBool(_.strBool)"

		// Path/URL functions
		case "pathJoin":
			hints[extFunc.Name] = "e.g. pathJoin([_.base, _.file])"
		case "pathBase":
			hints[extFunc.Name] = "e.g. pathBase(_.filepath)"
		case "pathDir":
			hints[extFunc.Name] = "e.g. pathDir(_.filepath)"

		// Hash functions
		case "sha256":
			hints[extFunc.Name] = "e.g. sha256(_.content)"
		case "md5":
			hints[extFunc.Name] = "e.g. md5(_.content)"

		// Default: use function name as placeholder
		default:
			if extFunc.Name != "" {
				hints[extFunc.Name] = "e.g. " + extFunc.Name + "(_.value)"
			}
		}
	}

	return hints
}

// EvaluateExpression is an alias for EvaluateWithScafctlCEL for convenience.
// This provides a shorter function name for common usage.
//
// The ctx parameter is used for context propagation (e.g., Writer for debug.out support).
func EvaluateExpression(ctx context.Context, expr string, data any) (any, error) {
	return EvaluateWithScafctlCEL(ctx, expr, data)
}
