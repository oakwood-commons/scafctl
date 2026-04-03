---
name: golang-testing
description: "Go testing patterns including table-driven tests, subtests, benchmarks, fuzzing, and test coverage. Use when writing tests, improving coverage, or debugging test failures."
---
# Go Testing Patterns

Comprehensive Go testing patterns for writing reliable, maintainable tests.

## When to Activate

- Writing new Go functions or methods
- Adding test coverage to existing code
- Creating benchmarks for performance-critical code
- Implementing fuzz tests for input validation
- Debugging test failures

## Table-Driven Tests

The standard pattern for Go tests. Enables comprehensive coverage with minimal code.

```go
func TestParseConfig(t *testing.T) {
    tests := []struct {
        name    string
        input   string
        want    *Config
        wantErr bool
    }{
        {
            name:  "valid config",
            input: `{"host": "localhost", "port": 8080}`,
            want:  &Config{Host: "localhost", Port: 8080},
        },
        {
            name:    "invalid JSON",
            input:   `{invalid}`,
            wantErr: true,
        },
        {
            name:    "empty input",
            input:   "",
            wantErr: true,
        },
        {
            name:  "minimal config",
            input: `{}`,
            want:  &Config{},
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := ParseConfig(tt.input)
            if tt.wantErr {
                assert.Error(t, err)
                return
            }
            assert.NoError(t, err)
            assert.Equal(t, tt.want, got)
        })
    }
}
```

## Subtests and Sub-benchmarks

### Organizing Related Tests

```go
func TestUser(t *testing.T) {
    db := setupTestDB(t)

    t.Run("Create", func(t *testing.T) {
        user := &User{Name: "Alice"}
        err := db.CreateUser(user)
        assert.NoError(t, err)
        assert.NotEmpty(t, user.ID)
    })

    t.Run("Get", func(t *testing.T) {
        user, err := db.GetUser("alice-id")
        assert.NoError(t, err)
        assert.Equal(t, "Alice", user.Name)
    })
}
```

### Parallel Subtests

```go
func TestParallel(t *testing.T) {
    tests := []struct {
        name  string
        input string
    }{
        {"case1", "input1"},
        {"case2", "input2"},
        {"case3", "input3"},
    }
    for _, tt := range tests {
        tt := tt // Capture range variable
        t.Run(tt.name, func(t *testing.T) {
            t.Parallel()
            result := Process(tt.input)
            _ = result
        })
    }
}
```

## Test Helpers

### Helper Functions

```go
func setupTestDB(t *testing.T) *sql.DB {
    t.Helper()
    db, err := sql.Open("sqlite3", ":memory:")
    if err != nil {
        t.Fatalf("failed to open database: %v", err)
    }
    t.Cleanup(func() { db.Close() })
    if _, err := db.Exec(schema); err != nil {
        t.Fatalf("failed to create schema: %v", err)
    }
    return db
}
```

### Temporary Files and Directories

```go
func TestFileProcessing(t *testing.T) {
    tmpDir := t.TempDir() // Automatically cleaned up
    testFile := filepath.Join(tmpDir, "test.txt")
    err := os.WriteFile(testFile, []byte("test content"), 0644)
    require.NoError(t, err)

    result, err := ProcessFile(testFile)
    require.NoError(t, err)
    assert.NotNil(t, result)
}
```

## Mocking with Interfaces

### Interface-Based Mocking

```go
// Define interface for dependencies
type UserRepository interface {
    GetUser(ctx context.Context, id string) (*User, error)
    SaveUser(ctx context.Context, user *User) error
}

// Mock implementation for tests
// Convention: place mocks in a dedicated mock.go file in the same package
type MockUserRepository struct {
    GetUserFunc  func(ctx context.Context, id string) (*User, error)
    SaveUserFunc func(ctx context.Context, user *User) error
}

func (m *MockUserRepository) GetUser(ctx context.Context, id string) (*User, error) {
    return m.GetUserFunc(ctx, id)
}

func (m *MockUserRepository) SaveUser(ctx context.Context, user *User) error {
    return m.SaveUserFunc(ctx, user)
}

// Test using mock
func TestUserService(t *testing.T) {
    mock := &MockUserRepository{
        GetUserFunc: func(ctx context.Context, id string) (*User, error) {
            if id == "123" {
                return &User{ID: "123", Name: "Alice"}, nil
            }
            return nil, ErrNotFound
        },
    }
    service := NewUserService(mock)
    user, err := service.GetUserProfile(context.Background(), "123")
    assert.NoError(t, err)
    assert.Equal(t, "Alice", user.Name)
}
```

## Benchmarks

Use `b.Loop()` (not `for i := 0; i < b.N; i++`) and always call `b.ReportAllocs()` before `b.ResetTimer()`. Benchmark-only files should use `*_benchmark_test.go` naming.

### Basic Benchmarks

```go
func BenchmarkProcess(b *testing.B) {
    data := generateTestData(1000)
    b.ReportAllocs()
    b.ResetTimer()
    for b.Loop() {
        Process(data)
    }
}

// Run: go test -bench=BenchmarkProcess -benchmem
```

### Benchmark with Different Sizes

```go
func BenchmarkSort(b *testing.B) {
    sizes := []int{100, 1000, 10000, 100000}
    for _, size := range sizes {
        b.Run(fmt.Sprintf("size=%d", size), func(b *testing.B) {
            data := generateRandomSlice(size)
            b.ReportAllocs()
            b.ResetTimer()
            for b.Loop() {
                tmp := make([]int, len(data))
                copy(tmp, data)
                sort.Ints(tmp)
            }
        })
    }
}
```

## Fuzzing (Go 1.18+)

### Basic Fuzz Test

```go
func FuzzParseJSON(f *testing.F) {
    f.Add(`{"name": "test"}`)
    f.Add(`{"count": 123}`)
    f.Add(`[]`)
    f.Add(`""`)

    f.Fuzz(func(t *testing.T, input string) {
        var result map[string]interface{}
        err := json.Unmarshal([]byte(input), &result)
        if err != nil {
            return // Invalid JSON is expected
        }
        _, err = json.Marshal(result)
        if err != nil {
            t.Errorf("Marshal failed after successful Unmarshal: %v", err)
        }
    })
}

// Run: go test -fuzz=FuzzParseJSON -fuzztime=30s
```

## HTTP Handler Testing

```go
func TestHealthHandler(t *testing.T) {
    req := httptest.NewRequest(http.MethodGet, "/health", nil)
    w := httptest.NewRecorder()
    HealthHandler(w, req)

    resp := w.Result()
    defer resp.Body.Close()

    assert.Equal(t, http.StatusOK, resp.StatusCode)
    body, _ := io.ReadAll(resp.Body)
    assert.Equal(t, "OK", string(body))
}
```

## Test Coverage

### Running Coverage

```bash
go test -cover ./...                              # Quick summary
go test -coverprofile=coverage.out ./...           # Generate profile
go tool cover -html=coverage.out                   # View in browser
go tool cover -func=coverage.out                   # Function summary
go test -race -coverprofile=coverage.out ./...     # With race detection
```

### Coverage Targets

| Code Type | Target |
|-----------|--------|
| Critical business logic | 100% |
| Public APIs | 90%+ |
| General code | 80%+ |
| Generated code | Exclude |

## Testing Commands

```bash
go test ./...                                # All tests
go test -v ./...                             # Verbose
go test -run TestAdd ./...                   # Specific test
go test -run "TestUser/Create" ./...         # Specific subtest
go test -race ./...                          # Race detector
go test -short ./...                         # Short tests only
go test -timeout 30s ./...                   # With timeout
go test -bench=. -benchmem ./...             # Benchmarks
go test -fuzz=FuzzParse -fuzztime=30s ./...  # Fuzzing
go test -count=10 ./...                      # Flaky test detection
```

## Best Practices

**DO:**
- Use table-driven tests for comprehensive coverage
- Test behavior, not implementation
- Use `t.Helper()` in helper functions
- Use `t.Parallel()` for independent tests
- Clean up resources with `t.Cleanup()`
- Use meaningful test names that describe the scenario
- Use `testify/assert` and `testify/require` for assertions
- Place mocks in `mock.go` files

**DON'T:**
- Use `time.Sleep()` in tests (use channels or conditions)
- Ignore flaky tests (fix or remove them)
- Mock everything (prefer integration tests when possible)
- Skip error path testing
