package filepath

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/kcloutie/scafctl/pkg/celexp"
	pkgfilepath "github.com/kcloutie/scafctl/pkg/filepath"
)

// DirFunc returns the directory component of a path.
func DirFunc() celexp.ExtFunction {
	funcName := "filepath.dir"
	return celexp.ExtFunction{
		Name:          funcName,
		Description:   "Returns the directory component of a path, removing the final element. Use filepath.dir(path) to get the parent directory of a file or directory path",
		FunctionNames: []string{funcName},
		Custom:        true,
		Examples: []celexp.Example{
			{
				Description: "Get directory from a file path",
				Expression:  `filepath.dir("/home/user/documents/file.txt")`,
			},
			{
				Description: "Get parent directory from a directory path",
				Expression:  `filepath.dir("/home/user/documents")`,
			},
		},
		EnvOptions: []cel.EnvOption{
			cel.Function(funcName,
				cel.Overload(strings.ReplaceAll(funcName, ".", "_"),
					[]*cel.Type{cel.StringType},
					cel.StringType,
					cel.UnaryBinding(func(pathVal ref.Val) ref.Val {
						path, ok := pathVal.Value().(string)
						if !ok {
							return types.NewErr("filepath.dir: expected string argument, got %s", pathVal.Type())
						}
						return types.String(filepath.Dir(path))
					}),
				),
			),
		},
	}
}

// NormalizeFunc normalizes a path, cleaning up separators and resolving . and .. elements.
func NormalizeFunc() celexp.ExtFunction {
	funcName := "filepath.normalize"
	return celexp.ExtFunction{
		Name:          funcName,
		Description:   "Normalizes a path by cleaning redundant separators and resolving . and .. elements. Use filepath.normalize(path) to convert a path to its shortest equivalent form with clean separators",
		FunctionNames: []string{funcName},
		Custom:        true,
		Examples: []celexp.Example{
			{
				Description: "Normalize a path with redundant separators",
				Expression:  `filepath.normalize("/home//user/../user/./documents")`,
			},
			{
				Description: "Clean a path with parent directory references",
				Expression:  `filepath.normalize("./src/../pkg/celexp")`,
			},
		},
		EnvOptions: []cel.EnvOption{
			cel.Function(funcName,
				cel.Overload(strings.ReplaceAll(funcName, ".", "_"),
					[]*cel.Type{cel.StringType},
					cel.StringType,
					cel.UnaryBinding(func(pathVal ref.Val) ref.Val {
						path, ok := pathVal.Value().(string)
						if !ok {
							return types.NewErr("filepath.normalize: expected string argument, got %s", pathVal.Type())
						}
						normalized := pkgfilepath.NormalizeFilePath(path)
						return types.String(filepath.Clean(normalized))
					}),
				),
			),
		},
	}
}

// ExistsFunc checks if a path exists on the filesystem.
func ExistsFunc() celexp.ExtFunction {
	funcName := "filepath.exists"
	return celexp.ExtFunction{
		Name:          funcName,
		Description:   "Checks if a file or directory exists at the specified path. Use filepath.exists(path) to return true if the path exists, false otherwise",
		FunctionNames: []string{funcName},
		Custom:        true,
		Examples: []celexp.Example{
			{
				Description: "Check if a file exists",
				Expression:  `filepath.exists("/etc/hosts")`,
			},
			{
				Description: "Use in conditional logic",
				Expression:  `filepath.exists("/tmp/config.json") ? "found" : "not found"`,
			},
		},
		EnvOptions: []cel.EnvOption{
			cel.Function(funcName,
				cel.Overload(strings.ReplaceAll(funcName, ".", "_"),
					[]*cel.Type{cel.StringType},
					cel.BoolType,
					cel.UnaryBinding(func(pathVal ref.Val) ref.Val {
						path, ok := pathVal.Value().(string)
						if !ok {
							return types.NewErr("filepath.exists: expected string argument, got %s", pathVal.Type())
						}
						_, err := os.Stat(path)
						return types.Bool(err == nil)
					}),
				),
			),
		},
	}
}

// JoinFunc joins path elements into a single path.
func JoinFunc() celexp.ExtFunction {
	funcName := "filepath.join"

	// Helper function to join paths from ref.Val arguments
	joinPaths := func(args ...ref.Val) ref.Val {
		paths := make([]string, len(args))
		for i, arg := range args {
			path, ok := arg.Value().(string)
			if !ok {
				return types.NewErr("filepath.join: expected string argument at position %d, got %s", i, arg.Type())
			}
			paths[i] = path
		}
		return types.String(filepath.Clean(filepath.Join(paths...)))
	}

	return celexp.ExtFunction{
		Name:          funcName,
		Description:   "Joins any number of path elements into a single path, separating them with the OS-specific path separator. Use filepath.join(path1, path2, ...) to combine path segments",
		FunctionNames: []string{funcName},
		Custom:        true,
		Examples: []celexp.Example{
			{
				Description: "Join two path segments",
				Expression:  `filepath.join("/home/user", "documents")`,
			},
			{
				Description: "Join multiple path segments",
				Expression:  `filepath.join("/var", "log", "app", "errors.log")`,
			},
			{
				Description: "Join with relative paths",
				Expression:  `filepath.join("./config", "settings", "app.json")`,
			},
		},
		EnvOptions: []cel.EnvOption{
			cel.Function(funcName,
				// 2 parameters
				cel.Overload(strings.ReplaceAll(funcName, ".", "_")+"_2",
					[]*cel.Type{cel.StringType, cel.StringType},
					cel.StringType,
					cel.FunctionBinding(joinPaths),
				),
				// 3 parameters
				cel.Overload(strings.ReplaceAll(funcName, ".", "_")+"_3",
					[]*cel.Type{cel.StringType, cel.StringType, cel.StringType},
					cel.StringType,
					cel.FunctionBinding(joinPaths),
				),
				// 4 parameters
				cel.Overload(strings.ReplaceAll(funcName, ".", "_")+"_4",
					[]*cel.Type{cel.StringType, cel.StringType, cel.StringType, cel.StringType},
					cel.StringType,
					cel.FunctionBinding(joinPaths),
				),
				// 5 parameters
				cel.Overload(strings.ReplaceAll(funcName, ".", "_")+"_5",
					[]*cel.Type{cel.StringType, cel.StringType, cel.StringType, cel.StringType, cel.StringType},
					cel.StringType,
					cel.FunctionBinding(joinPaths),
				),
				// 6 parameters
				cel.Overload(strings.ReplaceAll(funcName, ".", "_")+"_6",
					[]*cel.Type{cel.StringType, cel.StringType, cel.StringType, cel.StringType, cel.StringType, cel.StringType},
					cel.StringType,
					cel.FunctionBinding(joinPaths),
				),
				// 7 parameters
				cel.Overload(strings.ReplaceAll(funcName, ".", "_")+"_7",
					[]*cel.Type{cel.StringType, cel.StringType, cel.StringType, cel.StringType, cel.StringType, cel.StringType, cel.StringType},
					cel.StringType,
					cel.FunctionBinding(joinPaths),
				),
				// 8 parameters
				cel.Overload(strings.ReplaceAll(funcName, ".", "_")+"_8",
					[]*cel.Type{cel.StringType, cel.StringType, cel.StringType, cel.StringType, cel.StringType, cel.StringType, cel.StringType, cel.StringType},
					cel.StringType,
					cel.FunctionBinding(joinPaths),
				),
				// 9 parameters
				cel.Overload(strings.ReplaceAll(funcName, ".", "_")+"_9",
					[]*cel.Type{cel.StringType, cel.StringType, cel.StringType, cel.StringType, cel.StringType, cel.StringType, cel.StringType, cel.StringType, cel.StringType},
					cel.StringType,
					cel.FunctionBinding(joinPaths),
				),
				// 10 parameters
				cel.Overload(strings.ReplaceAll(funcName, ".", "_")+"_10",
					[]*cel.Type{cel.StringType, cel.StringType, cel.StringType, cel.StringType, cel.StringType, cel.StringType, cel.StringType, cel.StringType, cel.StringType, cel.StringType},
					cel.StringType,
					cel.FunctionBinding(joinPaths),
				),
			),
		},
	}
}
