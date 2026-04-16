---
description: "Go testing conventions: table-driven tests, testify/assert, and benchmarks."
applyTo: "**/*_test.go"
---

# Go Testing Conventions

## Framework

- Use standard `go test` with table-driven tests
- Use `testify/assert` for assertions
- Always run with `-race` flag

## Coverage Targets

- Domain packages: 80%+
- Critical logic: 90%+
- Patch coverage: 70%+

## Benchmarks

Add benchmark tests for performance-sensitive code:

~~~go
func BenchmarkMyFeature(b *testing.B) {
    b.ReportAllocs()
    b.ResetTimer()
    for b.Loop() {
        // benchmark code
    }
}
~~~
