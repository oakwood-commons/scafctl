// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package dag_test

import (
	"fmt"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/dag"
)

// benchDagObject implements dag.Object for benchmarking.
type benchDagObject struct {
	key  string
	deps []string
}

func (b *benchDagObject) DagKey() string { return b.key }

func (b *benchDagObject) GetDependencyKeys(objectNamesMap map[string]string, apiDepsMap map[string][]string, aliasMap map[string]string) []string {
	return b.deps
}

// benchDagObjects implements dag.Objects for benchmarking.
type benchDagObjects struct {
	items []dag.Object
}

func (b *benchDagObjects) DagItems() []dag.Object { return b.items }

// buildLinearGraph creates a linear graph: node0 -> node1 -> ... -> nodeN-1
func buildLinearGraph(n int) (dag.Objects, map[string][]string) {
	items := make([]dag.Object, n)
	deps := make(map[string][]string, n)
	for i := 0; i < n; i++ {
		key := fmt.Sprintf("node%d", i)
		items[i] = &benchDagObject{key: key}
		if i > 0 {
			deps[key] = []string{fmt.Sprintf("node%d", i-1)}
		}
	}
	return &benchDagObjects{items: items}, deps
}

// buildWideGraph creates a wide graph: one root, N-1 nodes depending on root.
func buildWideGraph(n int) (dag.Objects, map[string][]string) {
	items := make([]dag.Object, n)
	deps := make(map[string][]string, n)
	items[0] = &benchDagObject{key: "root"}
	for i := 1; i < n; i++ {
		key := fmt.Sprintf("node%d", i)
		items[i] = &benchDagObject{key: key}
		deps[key] = []string{"root"}
	}
	return &benchDagObjects{items: items}, deps
}

// buildDiamondGraph creates a layered diamond with n layers.
func buildDiamondGraph(n int) (dag.Objects, map[string][]string) {
	items := []dag.Object{&benchDagObject{key: "start"}}
	deps := make(map[string][]string)

	for layer := 0; layer < n; layer++ {
		left := fmt.Sprintf("l%d-a", layer)
		right := fmt.Sprintf("l%d-b", layer)
		merge := fmt.Sprintf("l%d-merge", layer)

		items = append(items,
			&benchDagObject{key: left},
			&benchDagObject{key: right},
			&benchDagObject{key: merge},
		)

		var prevKey string
		if layer == 0 {
			prevKey = "start"
		} else {
			prevKey = fmt.Sprintf("l%d-merge", layer-1)
		}

		deps[left] = []string{prevKey}
		deps[right] = []string{prevKey}
		deps[merge] = []string{left, right}
	}

	return &benchDagObjects{items: items}, deps
}

func BenchmarkDAG_Build_SmallGraph(b *testing.B) {
	objects, deps := buildLinearGraph(10)

	b.ResetTimer()
	for b.Loop() {
		_, _ = dag.Build(objects, deps)
	}
}

func BenchmarkDAG_Build_MediumGraph(b *testing.B) {
	objects, deps := buildLinearGraph(100)

	b.ResetTimer()
	for b.Loop() {
		_, _ = dag.Build(objects, deps)
	}
}

func BenchmarkDAG_Build_LargeGraph(b *testing.B) {
	objects, deps := buildLinearGraph(1000)

	b.ResetTimer()
	for b.Loop() {
		_, _ = dag.Build(objects, deps)
	}
}

func BenchmarkDAG_Build_WideGraph(b *testing.B) {
	objects, deps := buildWideGraph(100)

	b.ResetTimer()
	for b.Loop() {
		_, _ = dag.Build(objects, deps)
	}
}

func BenchmarkDAG_Build_DiamondGraph(b *testing.B) {
	objects, deps := buildDiamondGraph(50)

	b.ResetTimer()
	for b.Loop() {
		_, _ = dag.Build(objects, deps)
	}
}

func BenchmarkDAG_CycleDetection_NoCycles(b *testing.B) {
	// Large acyclic dependency map
	deps := make(map[string][]string, 100)
	for i := 1; i < 100; i++ {
		deps[fmt.Sprintf("node%d", i)] = []string{fmt.Sprintf("node%d", i-1)}
	}
	objects := &benchDagObjects{}
	items := make([]dag.Object, 100)
	for i := 0; i < 100; i++ {
		items[i] = &benchDagObject{key: fmt.Sprintf("node%d", i)}
	}
	objects.items = items

	b.ResetTimer()
	for b.Loop() {
		_, _ = dag.Build(objects, deps)
	}
}

func BenchmarkDAG_GetCandidateDagObjects(b *testing.B) {
	objects, deps := buildLinearGraph(20)
	g, err := dag.Build(objects, deps)
	if err != nil {
		b.Fatal(err)
	}

	// Mark first 10 nodes as done
	done := make([]string, 10)
	for i := 0; i < 10; i++ {
		done[i] = fmt.Sprintf("node%d", i)
	}

	b.ResetTimer()
	for b.Loop() {
		_, _ = dag.GetCandidateDagObjects(g, done...)
	}
}

func BenchmarkDAG_GetCandidateDagObjects_WideGraph(b *testing.B) {
	objects, deps := buildWideGraph(50)
	g, err := dag.Build(objects, deps)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for b.Loop() {
		// Root done, all children become candidates
		_, _ = dag.GetCandidateDagObjects(g, "root")
	}
}

func BenchmarkDAG_BuildChildToParentsMap(b *testing.B) {
	objects, deps := buildLinearGraph(100)
	g, err := dag.Build(objects, deps)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for b.Loop() {
		_ = dag.BuildChildToParentsMap(g.Nodes)
	}
}
