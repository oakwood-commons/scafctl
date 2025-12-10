package arrays

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUniqueStrings(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "empty slice",
			input:    []string{},
			expected: []string{},
		},
		{
			name:     "nil slice",
			input:    nil,
			expected: []string{},
		},
		{
			name:     "single element",
			input:    []string{"hello"},
			expected: []string{"hello"},
		},
		{
			name:     "no duplicates",
			input:    []string{"a", "b", "c"},
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "with duplicates",
			input:    []string{"a", "b", "a", "c", "b"},
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "all duplicates",
			input:    []string{"test", "test", "test"},
			expected: []string{"test"},
		},
		{
			name:     "consecutive duplicates",
			input:    []string{"a", "a", "b", "b", "c", "c"},
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "with empty strings",
			input:    []string{"", "a", "", "b", ""},
			expected: []string{"", "a", "b"},
		},
		{
			name:     "preserves order",
			input:    []string{"z", "a", "z", "b", "a"},
			expected: []string{"z", "a", "b"},
		},
		{
			name:     "special characters",
			input:    []string{"hello-world", "foo_bar", "hello-world", "test@example.com"},
			expected: []string{"hello-world", "foo_bar", "test@example.com"},
		},
		{
			name:     "unicode characters",
			input:    []string{"こんにちは", "hello", "こんにちは", "世界"},
			expected: []string{"こんにちは", "hello", "世界"},
		},
		{
			name:     "case sensitive",
			input:    []string{"Hello", "hello", "HELLO", "Hello"},
			expected: []string{"Hello", "hello", "HELLO"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := UniqueStrings(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func BenchmarkUniqueStrings_Small(b *testing.B) {
	input := []string{"a", "b", "c", "a", "b"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		UniqueStrings(input)
	}
}

func BenchmarkUniqueStrings_Medium(b *testing.B) {
	input := make([]string, 100)
	for i := 0; i < 100; i++ {
		input[i] = string(rune('a' + (i % 26)))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		UniqueStrings(input)
	}
}

func BenchmarkUniqueStrings_Large(b *testing.B) {
	input := make([]string, 1000)
	for i := 0; i < 1000; i++ {
		input[i] = string(rune('a' + (i % 26)))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		UniqueStrings(input)
	}
}
