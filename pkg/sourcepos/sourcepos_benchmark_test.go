// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package sourcepos

import (
	"fmt"
	"strings"
	"testing"
)

func buildTestYAML(depth, keysPerLevel int) []byte {
	var sb strings.Builder
	buildYAMLLevel(&sb, "", depth, keysPerLevel, 0)
	return []byte(sb.String())
}

func buildYAMLLevel(sb *strings.Builder, indent string, depth, keysPerLevel, currentDepth int) {
	for i := 0; i < keysPerLevel; i++ {
		key := fmt.Sprintf("key%d_%d", currentDepth, i)
		if currentDepth >= depth {
			fmt.Fprintf(sb, "%s%s: value%d\n", indent, key, i)
		} else {
			fmt.Fprintf(sb, "%s%s:\n", indent, key)
			buildYAMLLevel(sb, indent+"  ", depth, keysPerLevel, currentDepth+1)
		}
	}
}

func BenchmarkBuildSourceMap(b *testing.B) {
	b.Run("small", func(b *testing.B) {
		data := buildTestYAML(2, 3)
		if _, err := BuildSourceMap(data, "test.yaml"); err != nil {
			b.Fatal(err)
		}
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_, _ = BuildSourceMap(data, "test.yaml")
		}
	})

	b.Run("medium", func(b *testing.B) {
		data := buildTestYAML(3, 5)
		if _, err := BuildSourceMap(data, "test.yaml"); err != nil {
			b.Fatal(err)
		}
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_, _ = BuildSourceMap(data, "test.yaml")
		}
	})

	b.Run("large", func(b *testing.B) {
		data := buildTestYAML(4, 5)
		if _, err := BuildSourceMap(data, "test.yaml"); err != nil {
			b.Fatal(err)
		}
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_, _ = BuildSourceMap(data, "test.yaml")
		}
	})
}

func BenchmarkSourceMap_Get(b *testing.B) {
	data := buildTestYAML(3, 5)
	sm, err := BuildSourceMap(data, "test.yaml")
	if err != nil {
		b.Fatal(err)
	}

	paths := sm.Paths()
	if len(paths) == 0 {
		b.Fatal("no paths in source map")
	}
	targetPath := paths[len(paths)/2]

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		sm.Get(targetPath)
	}
}

func BenchmarkSourceMap_Merge(b *testing.B) {
	data1 := buildTestYAML(2, 4)
	data2 := buildTestYAML(2, 4)
	sm1, err := BuildSourceMap(data1, "file1.yaml")
	if err != nil {
		b.Fatal(err)
	}
	sm2, err := BuildSourceMap(data2, "file2.yaml")
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		target := NewSourceMap()
		target.Merge(sm1)
		target.Merge(sm2)
	}
}
