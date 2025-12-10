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
		Name:        funcName,
		Description: "Returns the directory component of a path, removing the final element. Use filepath.dir(path) to get the parent directory of a file or directory path",
		FunctionNames: []string{
			funcName,
		},
		Custom: true,
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
		Name:        funcName,
		Description: "Normalizes a path by cleaning redundant separators and resolving . and .. elements. Use filepath.normalize(path) to convert a path to its shortest equivalent form with clean separators",
		FunctionNames: []string{
			funcName,
		},
		Custom: true,
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
		Name:        funcName,
		Description: "Checks if a file or directory exists at the specified path. Use filepath.exists(path) to return true if the path exists, false otherwise",
		FunctionNames: []string{
			funcName,
		},
		Custom: true,
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
