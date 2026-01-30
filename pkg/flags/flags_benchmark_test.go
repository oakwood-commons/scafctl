package flags_test

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/flags"
)

// BenchmarkParseKeyValue benchmarks basic key-value parsing without CSV
func BenchmarkParseKeyValue(b *testing.B) {
	b.Run("single simple", func(b *testing.B) {
		input := []string{"key=value"}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = flags.ParseKeyValue(input)
		}
	})

	b.Run("multiple simple", func(b *testing.B) {
		input := []string{
			"env=production",
			"region=us-east1",
			"tier=premium",
		}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = flags.ParseKeyValue(input)
		}
	})

	b.Run("with special chars", func(b *testing.B) {
		input := []string{
			"message=Hello, World!",
			"data=value with spaces",
			"url=https://example.com?a=1&b=2",
		}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = flags.ParseKeyValue(input)
		}
	})
}

// BenchmarkParseKeyValueCSV benchmarks CSV-aware parsing
func BenchmarkParseKeyValueCSV(b *testing.B) {
	b.Run("simple no CSV", func(b *testing.B) {
		input := []string{"key=value"}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = flags.ParseKeyValueCSV(input)
		}
	})

	b.Run("simple CSV", func(b *testing.B) {
		input := []string{"env=prod,region=us-east1,tier=premium"}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = flags.ParseKeyValueCSV(input)
		}
	})

	b.Run("quoted values", func(b *testing.B) {
		input := []string{`message="Hello, World",data="value with commas, and more"`}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = flags.ParseKeyValueCSV(input)
		}
	})

	b.Run("json scheme", func(b *testing.B) {
		input := []string{`config=json://{"db":"postgres","port":5432},env=prod`}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = flags.ParseKeyValueCSV(input)
		}
	})

	b.Run("yaml scheme", func(b *testing.B) {
		input := []string{`config=yaml://items: [a, b, c],env=staging`}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = flags.ParseKeyValueCSV(input)
		}
	})

	b.Run("mixed schemes", func(b *testing.B) {
		input := []string{
			`config=json://{"key":"value"},data=yaml://items: [a,b],token=base64://SGVsbG8=,env=prod`,
		}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = flags.ParseKeyValueCSV(input)
		}
	})

	b.Run("realistic workload", func(b *testing.B) {
		input := []string{
			`env=production,region=us-east1,region=us-west1`,
			`config=json://{"timeout":30,"retries":3}`,
			`data=yaml://name: MyApp` + "\nversion: 1.0",
			`apiKey=secret-key-12345`,
		}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = flags.ParseKeyValueCSV(input)
		}
	})
}

// BenchmarkSplitCSV benchmarks the core CSV splitting logic
func BenchmarkSplitCSV(b *testing.B) {
	b.Run("no commas", func(b *testing.B) {
		input := []string{"key=value"}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = flags.ParseKeyValueCSV(input)
		}
	})

	b.Run("three entries", func(b *testing.B) {
		input := []string{"a=1,b=2,c=3"}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = flags.ParseKeyValueCSV(input)
		}
	})

	b.Run("ten entries", func(b *testing.B) {
		input := []string{"a=1,b=2,c=3,d=4,e=5,f=6,g=7,h=8,i=9,j=10"}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = flags.ParseKeyValueCSV(input)
		}
	})
}

// BenchmarkHelperFunctions benchmarks the helper functions
func BenchmarkHelperFunctions(b *testing.B) {
	parsed := map[string][]string{
		"single": {"value1"},
		"multi":  {"value1", "value2", "value3", "value4", "value5"},
		"env":    {"production"},
	}

	b.Run("GetFirst", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = flags.GetFirst(parsed, "single")
			_ = flags.GetFirst(parsed, "multi")
			_ = flags.GetFirst(parsed, "nonexistent")
		}
	})

	b.Run("GetAll", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = flags.GetAll(parsed, "single")
			_ = flags.GetAll(parsed, "multi")
			_ = flags.GetAll(parsed, "nonexistent")
		}
	})

	b.Run("Has", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = flags.Has(parsed, "single")
			_ = flags.Has(parsed, "multi")
			_ = flags.Has(parsed, "nonexistent")
		}
	})
}

// BenchmarkComplexParsing benchmarks worst-case parsing scenarios
func BenchmarkComplexParsing(b *testing.B) {
	b.Run("deeply nested JSON", func(b *testing.B) {
		input := []string{`config=json://{"level1":{"level2":{"level3":{"level4":"value"}}}}`}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = flags.ParseKeyValueCSV(input)
		}
	})

	b.Run("escaped quotes", func(b *testing.B) {
		input := []string{`msg="He said \"Hello\" and \"Goodbye\"",data="value"`}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = flags.ParseKeyValueCSV(input)
		}
	})

	b.Run("multiple flags with CSV", func(b *testing.B) {
		input := []string{
			"a=1,b=2,c=3",
			"d=4,e=5,f=6",
			"g=7,h=8,i=9",
		}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = flags.ParseKeyValueCSV(input)
		}
	})
}
