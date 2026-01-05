package debug

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/output"
)

//nolint:revive // DebugOutFunc is descriptive and matches the pattern of other Func names in the codebase
func DebugOutFunc(ioStreams *terminal.IOStreams) celexp.ExtFunction {
	funcName := "debug.out"

	// Default to os streams if nil
	if ioStreams == nil {
		ioStreams = terminal.NewIOStreams(os.Stdin, os.Stdout, os.Stderr, true)
	}

	return celexp.ExtFunction{
		Name:          funcName,
		Description:   "Outputs a debug message to the console and returns the value for inline debugging. Use debug.out(message) to print and return a message, or debug.out(message, value) to print a message and return a different value",
		FunctionNames: []string{funcName},
		Custom:        true,
		Examples: []celexp.Example{
			{
				Description: "Debug a single value",
				Expression:  `debug.out("Current value: " + myVar)`,
			},
			{
				Description: "Debug while returning a different value",
				Expression:  `debug.out("Processing item", item.name)`,
			},
			{
				Description: "Debug in a list operation",
				Expression:  `items.map(x, debug.out(x))`,
			},
		},
		EnvOptions: []cel.EnvOption{
			cel.Function(funcName,
				cel.Overload(strings.ReplaceAll(funcName, ".", "_"),
					[]*cel.Type{cel.AnyType},
					cel.AnyType,
					cel.FunctionBinding(func(args ...ref.Val) ref.Val {
						output.WriteDebug(ioStreams, fmt.Sprintf("CEL DEBUG OUTPUT: %v", args[0].Value()), !ioStreams.ColorEnabled)
						// Return the message for single argument version
						return args[0]
					},
					),
				),
				cel.Overload(strings.ReplaceAll(funcName, ".", "_")+"_with_value",
					[]*cel.Type{cel.AnyType, cel.AnyType},
					cel.AnyType,
					cel.FunctionBinding(func(args ...ref.Val) ref.Val {
						output.WriteDebug(ioStreams, fmt.Sprintf("CEL DEBUG OUTPUT: %v", args[0].Value()), !ioStreams.ColorEnabled)
						// Return the value (second argument) for two argument version
						return args[1]
					},
					),
				),
			),
		},
	}
}

//nolint:revive // DebugThrowFunc is descriptive and matches the pattern of other Func names in the codebase
func DebugThrowFunc() celexp.ExtFunction {
	funcName := "debug.throw"

	return celexp.ExtFunction{
		Name:          funcName,
		Description:   "Throws an error with the provided message, immediately halting CEL expression evaluation. Use debug.throw(message) to stop execution and return an error with the specified message",
		FunctionNames: []string{funcName},
		Custom:        true,
		Examples: []celexp.Example{
			{
				Description: "Throw an error unconditionally",
				Expression:  `debug.throw("Configuration is invalid")`,
			},
			{
				Description: "Throw an error conditionally",
				Expression:  `value < 0 ? debug.throw("Value must be positive") : value * 2`,
			},
		},
		EnvOptions: []cel.EnvOption{
			cel.Function(funcName,
				cel.Overload(strings.ReplaceAll(funcName, ".", "_"),
					[]*cel.Type{cel.AnyType},
					cel.AnyType,
					cel.FunctionBinding(func(args ...ref.Val) ref.Val {
						return types.NewErr("%v", args[0].Value())
					},
					),
				),
			),
		},
	}
}

//nolint:revive // DebugSleepFunc is descriptive and matches the pattern of other Func names in the codebase
func DebugSleepFunc() celexp.ExtFunction {
	funcName := "debug.sleep"

	return celexp.ExtFunction{
		Name:          funcName,
		Description:   "Pauses execution for the specified duration in milliseconds and returns the value for inline debugging. Use debug.sleep(duration) to sleep and return the duration value, or debug.sleep(duration, value) to sleep and return a different value",
		FunctionNames: []string{funcName},
		Custom:        true,
		Examples: []celexp.Example{
			{
				Description: "Sleep for 1 second (1000ms)",
				Expression:  `debug.sleep(1000)`,
			},
			{
				Description: "Sleep and return a specific value",
				Expression:  `debug.sleep(500, "Ready")`,
			},
			{
				Description: "Use in expression for timing",
				Expression:  `debug.sleep(100) + 5`,
			},
		},
		EnvOptions: []cel.EnvOption{
			cel.Function(funcName,
				cel.Overload(strings.ReplaceAll(funcName, ".", "_"),
					[]*cel.Type{cel.IntType},
					cel.IntType,
					cel.FunctionBinding(func(args ...ref.Val) ref.Val {
						s, ok := args[0].Value().(int64)
						if !ok {
							return types.NewErr("failed to convert object to int64. The type is %T", args[0].Value())
						}
						if s < 0 {
							s = 0
						}
						time.Sleep(time.Duration(s) * time.Millisecond)
						return args[0]
					},
					),
				),

				cel.Overload(strings.ReplaceAll(funcName, ".", "_")+"_with_value",
					[]*cel.Type{cel.IntType, cel.AnyType},
					cel.AnyType,
					cel.FunctionBinding(func(args ...ref.Val) ref.Val {
						s, ok := args[0].Value().(int64)
						if !ok {
							return types.NewErr("failed to convert object to int64. The type is %T", args[0].Value())
						}
						if s < 0 {
							s = 0
						}
						time.Sleep(time.Duration(s) * time.Millisecond)
						return args[1]
					},
					),
				),
			),
		},
	}
}
