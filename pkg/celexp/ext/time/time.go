// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package celtime

import (
	"strings"
	"time"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
)

// NowFunc returns the current time as a CEL timestamp.
func NowFunc() celexp.ExtFunction {
	funcName := "time.now"
	return celexp.ExtFunction{
		Name:          funcName,
		Description:   "Returns the current time as a CEL timestamp",
		FunctionNames: []string{funcName},
		Custom:        true,
		Examples: []celexp.Example{
			{
				Description: "Get current time",
				Expression:  `time.now()`,
			},
			{
				Description: "Compare current time with a specific time",
				Expression:  `time.now() > timestamp("2025-01-01T00:00:00Z")`,
			},
		},
		EnvOptions: []cel.EnvOption{
			cel.Function(funcName,
				cel.Overload(strings.ReplaceAll(funcName, ".", "_"),
					[]*cel.Type{},
					cel.TimestampType,
					cel.FunctionBinding(func(_ ...ref.Val) ref.Val {
						return types.Timestamp{Time: time.Now()}
					}),
				),
			),
		},
	}
}

// NowFmtFunc returns the current time formatted according to the provided layout string.
// The layout string follows Go's time.Format conventions.
func NowFmtFunc() celexp.ExtFunction {
	funcName := "time.nowFmt"
	return celexp.ExtFunction{
		Name:          funcName,
		Description:   "Returns the current time formatted according to the provided layout string. Uses Go's time.Format layout conventions (e.g., '2006-01-02 15:04:05' for 'YYYY-MM-DD HH:MM:SS')",
		FunctionNames: []string{funcName},
		Custom:        true,
		Examples: []celexp.Example{
			{
				Description: "Get current time in RFC3339 format",
				Expression:  `time.nowFmt("2006-01-02T15:04:05Z07:00")`,
			},
			{
				Description: "Get current date only",
				Expression:  `time.nowFmt("2006-01-02")`,
			},
			{
				Description: "Get current time with custom format",
				Expression:  `time.nowFmt("January 2, 2006 at 3:04 PM")`,
			},
			{
				Description: "Get current Unix timestamp as string",
				Expression:  `time.nowFmt("20060102150405")`,
			},
		},
		EnvOptions: []cel.EnvOption{
			cel.Function(funcName,
				cel.Overload(strings.ReplaceAll(funcName, ".", "_"),
					[]*cel.Type{cel.StringType},
					cel.StringType,
					cel.UnaryBinding(func(layoutVal ref.Val) ref.Val {
						layout, ok := layoutVal.Value().(string)
						if !ok {
							return types.NewErr("time.nowFmt: layout must be a string")
						}
						formatted := time.Now().Format(layout)
						return types.String(formatted)
					}),
				),
			),
		},
	}
}
