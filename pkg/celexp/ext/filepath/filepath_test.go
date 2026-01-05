package filepath

import (
	"os"
	"strings"
	"testing"

	"github.com/google/cel-go/cel"
	pkgfilepath "github.com/oakwood-commons/scafctl/pkg/filepath"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDirFunc_Metadata tests the metadata of the DirFunc
func TestDirFunc_Metadata(t *testing.T) {
	dirFunc := DirFunc()

	assert.Equal(t, "filepath.dir", dirFunc.Name)
	assert.Equal(t, "Returns the directory component of a path, removing the final element. Use filepath.dir(path) to get the parent directory of a file or directory path", dirFunc.Description)
	assert.NotEmpty(t, dirFunc.EnvOptions)
}

func TestDirFunc_CELIntegration(t *testing.T) {
	dirFunc := DirFunc()

	env, err := cel.NewEnv(dirFunc.EnvOptions...)
	require.NoError(t, err)

	tests := []struct {
		name       string
		expression string
		expected   string
	}{
		{
			name:       "unix path with file",
			expression: `filepath.dir("/usr/local/bin/myapp")`,
			expected:   "/usr/local/bin",
		},
		{
			name:       "unix path directory",
			expression: `filepath.dir("/usr/local/bin/")`,
			expected:   "/usr/local/bin",
		},
		{
			name:       "unix nested path",
			expression: `filepath.dir("/home/user/documents/file.txt")`,
			expected:   "/home/user/documents",
		},
		{
			name:       "relative path",
			expression: `filepath.dir("dir/subdir/file.txt")`,
			expected:   "dir/subdir",
		},
		{
			name:       "single level path",
			expression: `filepath.dir("file.txt")`,
			expected:   ".",
		},
		{
			name:       "root path",
			expression: `filepath.dir("/")`,
			expected:   "/",
		},
		{
			name:       "current directory",
			expression: `filepath.dir(".")`,
			expected:   ".",
		},
		{
			name:       "parent directory",
			expression: `filepath.dir("..")`,
			expected:   ".",
		},
		{
			name:       "empty string",
			expression: `filepath.dir("")`,
			expected:   ".",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast, issues := env.Compile(tt.expression)
			require.NoError(t, issues.Err())

			prog, err := env.Program(ast)
			require.NoError(t, err)

			result, _, err := prog.Eval(map[string]interface{}{})
			require.NoError(t, err)
			// Normalize both paths for cross-platform comparison
			expectedNormalized := pkgfilepath.NormalizeFilePath(tt.expected)
			resultNormalized := pkgfilepath.NormalizeFilePath(result.Value().(string))
			assert.Equal(t, expectedNormalized, resultNormalized)
		})
	}
}

func TestDirFunc_WithVariables(t *testing.T) {
	dirFunc := DirFunc()

	env, err := cel.NewEnv(
		dirFunc.EnvOptions[0],
		cel.Variable("path", cel.StringType),
	)
	require.NoError(t, err)

	ast, issues := env.Compile(`filepath.dir(path)`)
	require.NoError(t, issues.Err())

	prog, err := env.Program(ast)
	require.NoError(t, err)

	testCases := []struct {
		path     string
		expected string
	}{
		{"/usr/local/bin/myapp", "/usr/local/bin"},
		{"relative/path/file.txt", "relative/path"},
		{"file.txt", "."},
	}

	for _, tc := range testCases {
		result, _, err := prog.Eval(map[string]interface{}{
			"path": tc.path,
		})
		require.NoError(t, err)
		// Normalize both paths for cross-platform comparison
		expectedNormalized := pkgfilepath.NormalizeFilePath(tc.expected)
		resultNormalized := pkgfilepath.NormalizeFilePath(result.Value().(string))
		assert.Equal(t, expectedNormalized, resultNormalized)
	}
}

func TestDirFunc_TypeError(t *testing.T) {
	dirFunc := DirFunc()

	env, err := cel.NewEnv(dirFunc.EnvOptions...)
	require.NoError(t, err)

	tests := []struct {
		name       string
		expression string
	}{
		{
			name:       "integer argument",
			expression: `filepath.dir(123)`,
		},
		{
			name:       "boolean argument",
			expression: `filepath.dir(true)`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, issues := env.Compile(tt.expression)
			require.Error(t, issues.Err())
			assert.Contains(t, issues.Err().Error(), "found no matching overload")
		})
	}
}

// TestNormalizeFunc_Metadata tests the metadata of the NormalizeFunc
func TestNormalizeFunc_Metadata(t *testing.T) {
	normalizeFunc := NormalizeFunc()

	assert.Equal(t, "filepath.normalize", normalizeFunc.Name)
	assert.Equal(t, "Normalizes a path by cleaning redundant separators and resolving . and .. elements. Use filepath.normalize(path) to convert a path to its shortest equivalent form with clean separators", normalizeFunc.Description)
	assert.NotEmpty(t, normalizeFunc.EnvOptions)
}

func TestNormalizeFunc_CELIntegration(t *testing.T) {
	normalizeFunc := NormalizeFunc()

	env, err := cel.NewEnv(normalizeFunc.EnvOptions...)
	require.NoError(t, err)

	tests := []struct {
		name       string
		expression string
		expected   string
	}{
		{
			name:       "double slashes",
			expression: `filepath.normalize("/usr//local//bin")`,
			expected:   "/usr/local/bin",
		},
		{
			name:       "trailing slash",
			expression: `filepath.normalize("/usr/local/bin/")`,
			expected:   "/usr/local/bin",
		},
		{
			name:       "current directory references",
			expression: `filepath.normalize("/usr/./local/./bin")`,
			expected:   "/usr/local/bin",
		},
		{
			name:       "parent directory references",
			expression: `filepath.normalize("/usr/local/../bin")`,
			expected:   "/usr/bin",
		},
		{
			name:       "complex mixed path",
			expression: `filepath.normalize("/usr/./local/../lib//bin/./")`,
			expected:   "/usr/lib/bin",
		},
		{
			name:       "relative path with dots",
			expression: `filepath.normalize("./dir/../file.txt")`,
			expected:   "file.txt",
		},
		{
			name:       "already clean path",
			expression: `filepath.normalize("/usr/local/bin")`,
			expected:   "/usr/local/bin",
		},
		{
			name:       "empty string",
			expression: `filepath.normalize("")`,
			expected:   ".",
		},
		{
			name:       "just dot",
			expression: `filepath.normalize(".")`,
			expected:   ".",
		},
		{
			name:       "just double dot",
			expression: `filepath.normalize("..")`,
			expected:   "..",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast, issues := env.Compile(tt.expression)
			require.NoError(t, issues.Err())

			prog, err := env.Program(ast)
			require.NoError(t, err)

			result, _, err := prog.Eval(map[string]interface{}{})
			require.NoError(t, err)
			// Normalize both paths for cross-platform comparison
			expectedNormalized := pkgfilepath.NormalizeFilePath(tt.expected)
			resultNormalized := pkgfilepath.NormalizeFilePath(result.Value().(string))
			assert.Equal(t, expectedNormalized, resultNormalized)
		})
	}
}

func TestNormalizeFunc_WithVariables(t *testing.T) {
	normalizeFunc := NormalizeFunc()

	env, err := cel.NewEnv(
		normalizeFunc.EnvOptions[0],
		cel.Variable("path", cel.StringType),
	)
	require.NoError(t, err)

	ast, issues := env.Compile(`filepath.normalize(path)`)
	require.NoError(t, issues.Err())

	prog, err := env.Program(ast)
	require.NoError(t, err)

	testCases := []struct {
		path     string
		expected string
	}{
		{"/usr//local//bin", "/usr/local/bin"},
		{"./dir/../file.txt", "file.txt"},
		{"/usr/local/bin/", "/usr/local/bin"},
	}

	for _, tc := range testCases {
		result, _, err := prog.Eval(map[string]interface{}{
			"path": tc.path,
		})
		require.NoError(t, err)
		// Normalize both paths for cross-platform comparison
		expectedNormalized := pkgfilepath.NormalizeFilePath(tc.expected)
		resultNormalized := pkgfilepath.NormalizeFilePath(result.Value().(string))
		assert.Equal(t, expectedNormalized, resultNormalized)
	}
}

func TestNormalizeFunc_TypeError(t *testing.T) {
	normalizeFunc := NormalizeFunc()

	env, err := cel.NewEnv(normalizeFunc.EnvOptions...)
	require.NoError(t, err)

	tests := []struct {
		name       string
		expression string
	}{
		{
			name:       "integer argument",
			expression: `filepath.normalize(123)`,
		},
		{
			name:       "list argument",
			expression: `filepath.normalize(["path"])`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, issues := env.Compile(tt.expression)
			require.Error(t, issues.Err())
			assert.Contains(t, issues.Err().Error(), "found no matching overload")
		})
	}
}

// TestExistsFunc_Metadata tests the metadata of the ExistsFunc
func TestExistsFunc_Metadata(t *testing.T) {
	existsFunc := ExistsFunc()

	assert.Equal(t, "filepath.exists", existsFunc.Name)
	assert.Equal(t, "Checks if a file or directory exists at the specified path. Use filepath.exists(path) to return true if the path exists, false otherwise", existsFunc.Description)
	assert.NotEmpty(t, existsFunc.EnvOptions)
}

func TestExistsFunc_CELIntegration(t *testing.T) {
	existsFunc := ExistsFunc()

	env, err := cel.NewEnv(existsFunc.EnvOptions...)
	require.NoError(t, err)

	// Create a temporary file for testing
	tmpFile, err := os.CreateTemp("", "test-file-*.txt")
	require.NoError(t, err)
	tmpFilePath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpFilePath)

	// Create a temporary directory
	tmpDir, err := os.MkdirTemp("", "test-dir-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	tests := []struct {
		name        string
		path        string
		expected    bool
		setupFunc   func() string
		cleanupFunc func(string)
	}{
		{
			name:     "existing file",
			path:     tmpFilePath,
			expected: true,
		},
		{
			name:     "existing directory",
			path:     tmpDir,
			expected: true,
		},
		{
			name:     "non-existent path",
			path:     "/this/path/should/not/exist/12345",
			expected: false,
		},
		{
			name:     "current directory",
			path:     ".",
			expected: true,
		},
		{
			name: "file that was just created",
			setupFunc: func() string {
				f, _ := os.CreateTemp("", "dynamic-test-*.txt")
				path := f.Name()
				f.Close()
				return path
			},
			cleanupFunc: func(path string) {
				os.Remove(path)
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := tt.path
			if tt.setupFunc != nil {
				path = tt.setupFunc()
			}
			if tt.cleanupFunc != nil {
				defer tt.cleanupFunc(path)
			}

			// Escape backslashes for CEL expression (backslashes are escape chars in CEL)
			// Replace \ with / which works on all operating systems including Windows
			celPath := strings.ReplaceAll(path, "\\", "/")
			expression := `filepath.exists("` + celPath + `")`
			ast, issues := env.Compile(expression)
			require.NoError(t, issues.Err())

			prog, err := env.Program(ast)
			require.NoError(t, err)

			result, _, err := prog.Eval(map[string]interface{}{})
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result.Value())
		})
	}
}

func TestExistsFunc_WithVariables(t *testing.T) {
	existsFunc := ExistsFunc()

	env, err := cel.NewEnv(
		existsFunc.EnvOptions[0],
		cel.Variable("path", cel.StringType),
	)
	require.NoError(t, err)

	ast, issues := env.Compile(`filepath.exists(path)`)
	require.NoError(t, issues.Err())

	prog, err := env.Program(ast)
	require.NoError(t, err)

	// Create a temp file
	tmpFile, err := os.CreateTemp("", "test-var-*.txt")
	require.NoError(t, err)
	tmpFilePath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpFilePath)

	testCases := []struct {
		path     string
		expected bool
	}{
		{tmpFilePath, true},
		{"/non/existent/path", false},
		{".", true},
	}

	for _, tc := range testCases {
		result, _, err := prog.Eval(map[string]interface{}{
			"path": tc.path,
		})
		require.NoError(t, err)
		assert.Equal(t, tc.expected, result.Value())
	}
}

func TestExistsFunc_TypeError(t *testing.T) {
	existsFunc := ExistsFunc()

	env, err := cel.NewEnv(existsFunc.EnvOptions...)
	require.NoError(t, err)

	tests := []struct {
		name       string
		expression string
	}{
		{
			name:       "integer argument",
			expression: `filepath.exists(123)`,
		},
		{
			name:       "boolean argument",
			expression: `filepath.exists(false)`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, issues := env.Compile(tt.expression)
			require.Error(t, issues.Err())
			assert.Contains(t, issues.Err().Error(), "found no matching overload")
		})
	}
}

// Integration tests combining multiple functions
func TestFilepath_CombinedFunctions(t *testing.T) {
	dirFunc := DirFunc()
	normalizeFunc := NormalizeFunc()
	existsFunc := ExistsFunc()

	env, err := cel.NewEnv(
		dirFunc.EnvOptions[0],
		normalizeFunc.EnvOptions[0],
		existsFunc.EnvOptions[0],
	)
	require.NoError(t, err)

	tests := []struct {
		name       string
		expression string
		checkFunc  func(t *testing.T, result interface{})
	}{
		{
			name:       "normalize then get directory",
			expression: `filepath.dir(filepath.normalize("/usr//local//bin/file.txt"))`,
			checkFunc: func(t *testing.T, result interface{}) {
				// Normalize both paths for cross-platform comparison
				expected := pkgfilepath.NormalizeFilePath("/usr/local/bin")
				actual := pkgfilepath.NormalizeFilePath(result.(string))
				assert.Equal(t, expected, actual)
			},
		},
		{
			name:       "check if current directory exists",
			expression: `filepath.exists(".")`,
			checkFunc: func(t *testing.T, result interface{}) {
				assert.Equal(t, true, result)
			},
		},
		{
			name:       "get dir and check existence",
			expression: `filepath.exists(filepath.dir("."))`,
			checkFunc: func(t *testing.T, result interface{}) {
				assert.Equal(t, true, result)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast, issues := env.Compile(tt.expression)
			require.NoError(t, issues.Err())

			prog, err := env.Program(ast)
			require.NoError(t, err)

			result, _, err := prog.Eval(map[string]interface{}{})
			require.NoError(t, err)
			tt.checkFunc(t, result.Value())
		})
	}
}

// Benchmark tests
func BenchmarkDirFunc_CEL(b *testing.B) {
	dirFunc := DirFunc()
	env, _ := cel.NewEnv(dirFunc.EnvOptions...)
	ast, _ := env.Compile(`filepath.dir("/usr/local/bin/myapp")`)
	prog, _ := env.Program(ast)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		prog.Eval(map[string]interface{}{})
	}
}

func BenchmarkNormalizeFunc_CEL(b *testing.B) {
	normalizeFunc := NormalizeFunc()
	env, _ := cel.NewEnv(normalizeFunc.EnvOptions...)
	ast, _ := env.Compile(`filepath.normalize("/usr/./local/../lib//bin/./")`)
	prog, _ := env.Program(ast)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		prog.Eval(map[string]interface{}{})
	}
}

func BenchmarkExistsFunc_CEL(b *testing.B) {
	existsFunc := ExistsFunc()
	env, _ := cel.NewEnv(existsFunc.EnvOptions...)
	ast, _ := env.Compile(`filepath.exists(".")`)
	prog, _ := env.Program(ast)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		prog.Eval(map[string]interface{}{})
	}
}

// TestJoinFunc_Metadata tests the metadata of the JoinFunc
func TestJoinFunc_Metadata(t *testing.T) {
	joinFunc := JoinFunc()

	assert.Equal(t, "filepath.join", joinFunc.Name)
	assert.Equal(t, "Joins any number of path elements into a single path, separating them with the OS-specific path separator. Use filepath.join(path1, path2, ...) to combine path segments", joinFunc.Description)
	assert.NotEmpty(t, joinFunc.EnvOptions)
	assert.NotEmpty(t, joinFunc.Examples)
	assert.Len(t, joinFunc.Examples, 3)
}

func TestJoinFunc_CELIntegration(t *testing.T) {
	joinFunc := JoinFunc()

	env, err := cel.NewEnv(joinFunc.EnvOptions...)
	require.NoError(t, err)

	tests := []struct {
		name       string
		expression string
		expected   string
	}{
		{
			name:       "join two paths",
			expression: `filepath.join("/home/user", "documents")`,
			expected:   "/home/user/documents",
		},
		{
			name:       "join three paths",
			expression: `filepath.join("/var", "log", "app.log")`,
			expected:   "/var/log/app.log",
		},
		{
			name:       "join four paths",
			expression: `filepath.join("/home", "user", "documents", "file.txt")`,
			expected:   "/home/user/documents/file.txt",
		},
		{
			name:       "join five paths",
			expression: `filepath.join("/a", "b", "c", "d", "e")`,
			expected:   "/a/b/c/d/e",
		},
		{
			name:       "join six paths",
			expression: `filepath.join("/a", "b", "c", "d", "e", "f")`,
			expected:   "/a/b/c/d/e/f",
		},
		{
			name:       "join seven paths",
			expression: `filepath.join("/a", "b", "c", "d", "e", "f", "g")`,
			expected:   "/a/b/c/d/e/f/g",
		},
		{
			name:       "join eight paths",
			expression: `filepath.join("/a", "b", "c", "d", "e", "f", "g", "h")`,
			expected:   "/a/b/c/d/e/f/g/h",
		},
		{
			name:       "join nine paths",
			expression: `filepath.join("/a", "b", "c", "d", "e", "f", "g", "h", "i")`,
			expected:   "/a/b/c/d/e/f/g/h/i",
		},
		{
			name:       "join ten paths",
			expression: `filepath.join("/a", "b", "c", "d", "e", "f", "g", "h", "i", "j")`,
			expected:   "/a/b/c/d/e/f/g/h/i/j",
		},
		{
			name:       "join with relative paths",
			expression: `filepath.join(".", "config", "app.json")`,
			expected:   "config/app.json",
		},
		{
			name:       "join with empty strings",
			expression: `filepath.join("/home", "", "user")`,
			expected:   "/home/user",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast, issues := env.Compile(tt.expression)
			require.NoError(t, issues.Err())

			prog, err := env.Program(ast)
			require.NoError(t, err)

			result, _, err := prog.Eval(map[string]interface{}{})
			require.NoError(t, err)
			// Normalize both paths for cross-platform comparison
			expectedNormalized := pkgfilepath.NormalizeFilePath(tt.expected)
			resultNormalized := pkgfilepath.NormalizeFilePath(result.Value().(string))
			assert.Equal(t, expectedNormalized, resultNormalized)
		})
	}
}

func TestJoinFunc_WithVariables(t *testing.T) {
	joinFunc := JoinFunc()

	env, err := cel.NewEnv(
		joinFunc.EnvOptions[0],
		cel.Variable("base", cel.StringType),
		cel.Variable("dir", cel.StringType),
		cel.Variable("file", cel.StringType),
	)
	require.NoError(t, err)

	ast, issues := env.Compile(`filepath.join(base, dir, file)`)
	require.NoError(t, issues.Err())

	prog, err := env.Program(ast)
	require.NoError(t, err)

	result, _, err := prog.Eval(map[string]interface{}{
		"base": "/home/user",
		"dir":  "documents",
		"file": "readme.txt",
	})
	require.NoError(t, err)
	// Normalize both paths for cross-platform comparison
	expected := pkgfilepath.NormalizeFilePath("/home/user/documents/readme.txt")
	actual := pkgfilepath.NormalizeFilePath(result.Value().(string))
	assert.Equal(t, expected, actual)
}

func TestJoinFunc_TypeError(t *testing.T) {
	joinFunc := JoinFunc()

	env, err := cel.NewEnv(
		joinFunc.EnvOptions[0],
		cel.Variable("num", cel.IntType),
	)
	require.NoError(t, err)

	// CEL will catch type errors at compile time
	_, issues := env.Compile(`filepath.join("/home", num)`)
	require.Error(t, issues.Err())
	assert.Contains(t, issues.Err().Error(), "found no matching overload")
}

func TestJoinFunc_ChainedOperations(t *testing.T) {
	joinFunc := JoinFunc()
	dirFunc := DirFunc()

	env, err := cel.NewEnv(
		joinFunc.EnvOptions[0],
		dirFunc.EnvOptions[0],
	)
	require.NoError(t, err)

	ast, issues := env.Compile(`filepath.dir(filepath.join("/home", "user", "documents", "file.txt"))`)
	require.NoError(t, issues.Err())

	prog, err := env.Program(ast)
	require.NoError(t, err)

	result, _, err := prog.Eval(map[string]interface{}{})
	require.NoError(t, err)
	// Normalize both paths for cross-platform comparison
	expected := pkgfilepath.NormalizeFilePath("/home/user/documents")
	actual := pkgfilepath.NormalizeFilePath(result.Value().(string))
	assert.Equal(t, expected, actual)
}

func BenchmarkJoinFunc_CEL_TwoParams(b *testing.B) {
	joinFunc := JoinFunc()
	env, _ := cel.NewEnv(joinFunc.EnvOptions...)
	ast, _ := env.Compile(`filepath.join("/home/user", "documents")`)
	prog, _ := env.Program(ast)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		prog.Eval(map[string]interface{}{})
	}
}

func BenchmarkJoinFunc_CEL_FiveParams(b *testing.B) {
	joinFunc := JoinFunc()
	env, _ := cel.NewEnv(joinFunc.EnvOptions...)
	ast, _ := env.Compile(`filepath.join("/a", "b", "c", "d", "e")`)
	prog, _ := env.Program(ast)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		prog.Eval(map[string]interface{}{})
	}
}

func BenchmarkJoinFunc_CEL_TenParams(b *testing.B) {
	joinFunc := JoinFunc()
	env, _ := cel.NewEnv(joinFunc.EnvOptions...)
	ast, _ := env.Compile(`filepath.join("/a", "b", "c", "d", "e", "f", "g", "h", "i", "j")`)
	prog, _ := env.Program(ast)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		prog.Eval(map[string]interface{}{})
	}
}
