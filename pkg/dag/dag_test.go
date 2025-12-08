package dag_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/kcloutie/scafctl/pkg/dag"
	"k8s.io/apimachinery/pkg/util/sets"
)

type mockDagObject struct {
	key  string
	deps []string
}

func (m *mockDagObject) DagKey() string {
	return m.key
}

func (m *mockDagObject) GetDependencyKeys(dagObjectNames map[string]string, apiDepsMap map[string][]string, aliasMap map[string]string) []string {
	return m.deps
}

type mockDagObjects struct {
	items []dag.Object
}

func (m *mockDagObjects) DagItems() []dag.Object {
	return m.items
}

func TestFindCyclesInDependencies(t *testing.T) {
	tests := []struct {
		name string
		deps map[string][]string
		want error
	}{
		{
			name: "no dependencies",
			deps: map[string][]string{},
			want: nil,
		},
		{
			name: "single dependency",
			deps: map[string][]string{
				"node1": {"node2"},
			},
			want: nil,
		},
		{
			name: "multiple dependencies",
			deps: map[string][]string{
				"node1": {"node2", "node3"},
			},
			want: nil,
		},
		{
			name: "complex dependencies",
			deps: map[string][]string{
				"node1": {"node2"},
				"node2": {"node3"},
			},
			want: nil,
		},
		{
			name: "simple cycle",
			deps: map[string][]string{
				"node1": {"node2"},
				"node2": {"node1"},
			},
			want: fmt.Errorf("dagObject %q depends on %q", "node1", "node2"),
		},
		{
			name: "complex cycle",
			deps: map[string][]string{
				"node1": {"node2"},
				"node2": {"node3"},
				"node3": {"node1"},
			},
			want: fmt.Errorf("dagObject %q depends on %q", "node1", "node2"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := dag.FindCyclesInDependencies(tt.deps)
			if (err != nil && tt.want == nil) || (err == nil && tt.want != nil) {
				t.Errorf("findCyclesInDependencies() error = %v, want %v", err, tt.want)
				return
			}
			if err != nil && err.Error() != tt.want.Error() {
				t.Errorf("findCyclesInDependencies() error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestBuild(t *testing.T) {
	tests := []struct {
		name       string
		dagObjects dag.Objects
		deps       map[string][]string
		wantErr    bool
		errMsg     string
	}{
		{
			name: "valid graph",
			dagObjects: &mockDagObjects{
				items: []dag.Object{
					&mockDagObject{key: "node1"},
					&mockDagObject{key: "node2"},
				},
			},
			deps: map[string][]string{
				"node2": {"node1"},
			},
			wantErr: false,
		},
		{
			name: "duplicate dagObject",
			dagObjects: &mockDagObjects{
				items: []dag.Object{
					&mockDagObject{key: "node1"},
					&mockDagObject{key: "node1"},
				},
			},
			deps:    map[string][]string{},
			wantErr: true,
			errMsg:  "dagObject node1 is already present in Graph, can't add it again: duplicate dagObject",
		},
		{
			name: "cycle detected",
			dagObjects: &mockDagObjects{
				items: []dag.Object{
					&mockDagObject{key: "node1"},
					&mockDagObject{key: "node2"},
				},
			},
			deps: map[string][]string{
				"node1": {"node2"},
				"node2": {"node1"},
			},
			wantErr: true,
			errMsg:  "cycle detected; dagObject \"node1\" depends on \"node2\"",
		},
		{
			name: "missing previous dagObject",
			dagObjects: &mockDagObjects{
				items: []dag.Object{
					&mockDagObject{key: "node2"},
				},
			},
			deps: map[string][]string{
				"node2": {"node1"},
			},
			wantErr: true,
			errMsg:  "couldn't add link between node2 and node1: dagObject node2 depends on node1 but node1 wasn't present",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := dag.Build(tt.dagObjects, tt.deps)
			if (err != nil) != tt.wantErr {
				t.Errorf("Build() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && !cmp.Equal(err.Error(), tt.errMsg, cmpopts.EquateErrors()) && !strings.Contains(tt.errMsg, err.Error()) {
				t.Errorf("Build() error = %v, wantErr %v", err, tt.errMsg)
			}
		})
	}
}

func CreateSet(s ...string) sets.Set[string] {
	st := sets.Set[string]{}
	st.Insert(s...)
	return st
}

func TestGetSchedulable(t *testing.T) {
	g := testGraph(t)
	tcs := []struct {
		name               string
		finished           []string
		expectedDagObjects sets.Set[string]
	}{
		{
			name:               "nothing-done",
			finished:           []string{},
			expectedDagObjects: CreateSet("a", "b"),
		},
		{
			name:               "a-done",
			finished:           []string{"a"},
			expectedDagObjects: CreateSet("b", "x"),
		},
		{
			name:               "b-done",
			finished:           []string{"b"},
			expectedDagObjects: CreateSet("a"),
		},
		{
			name:               "a-and-b-done",
			finished:           []string{"a", "b"},
			expectedDagObjects: CreateSet("x"),
		},
		{
			name:               "a-x-done",
			finished:           []string{"a", "x"},
			expectedDagObjects: CreateSet("b", "y", "z"),
		},
		{
			name:               "a-x-b-done",
			finished:           []string{"a", "x", "b"},
			expectedDagObjects: CreateSet("y", "z"),
		},
		{
			name:               "a-x-y-done",
			finished:           []string{"a", "x", "y"},
			expectedDagObjects: CreateSet("b", "z"),
		},
		{
			name:               "a-x-y-b-done",
			finished:           []string{"a", "x", "y", "b"},
			expectedDagObjects: CreateSet("w", "z"),
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			tasks, err := dag.GetCandidateDagObjects(g, tc.finished...)
			if err != nil {
				t.Fatalf("Didn't expect error when getting next tasks for %v but got %v", tc.finished, err)
			}
			d := cmp.Diff(tc.expectedDagObjects, tasks, cmpopts.IgnoreFields(dag.Node{}, "Prev", "Next"))

			if d != "" {
				t.Errorf("expected that with %v done, %v would be ready to schedule but was different: %s", tc.finished, tc.expectedDagObjects, d)
			}
		})
	}
}

func TestGetSchedulable_Invalid(t *testing.T) {
	g := testGraph(t)
	tcs := []struct {
		name     string
		finished []string
	}{
		{
			name:     "only-x",
			finished: []string{"x"},
		},
		{
			name:     "only-y",
			finished: []string{"y"},
		},
		{
			name:     "only-w",
			finished: []string{"w"},
		},
		{
			name:     "only-y-and-x",
			finished: []string{"y", "x"},
		},
		{
			name:     "only-y-and-w",
			finished: []string{"y", "w"},
		},
		{
			name:     "only-x-and-w",
			finished: []string{"x", "w"},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			_, err := dag.GetCandidateDagObjects(g, tc.finished...)
			if err == nil {
				t.Fatalf("Expected error for invalid done dagObjects %v but got none", tc.finished)
			}
		})
	}
}

func testGraph(t *testing.T) *dag.Graph {
	//  b     a
	//  |    / \
	//  |   |   x
	//  |   | / |
	//  |   y   |
	//   \ /    z
	//    w
	t.Helper()
	dagObjects := []dag.Object{
		&mockDagObject{key: "a"},
		&mockDagObject{key: "b"},
		&mockDagObject{key: "w"},
		&mockDagObject{key: "x"},
		&mockDagObject{key: "y"},
		&mockDagObject{key: "z"},
	}
	deps := map[string][]string{
		"w": {"b", "y"},
		"x": {"a"},
		"y": {"a", "x"},
		"z": {"x"},
	}
	g, err := dag.Build(&mockDagObjects{items: dagObjects}, deps)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestBuildChildToParentsMap(t *testing.T) {
	tests := []struct {
		name  string
		nodes map[string]*dag.Node
		want  map[string]dag.ChildToParents
	}{
		{
			name:  "empty graph",
			nodes: map[string]*dag.Node{},
			want:  map[string]dag.ChildToParents{},
		},
		{
			name: "single node with no parents",
			nodes: map[string]*dag.Node{
				"a": {Key: "a", Prev: nil},
			},

			want: map[string]dag.ChildToParents{
				"a": {Child: "a", Parents: []string{}},
			},
		},
		{
			name: "node with one parent",
			nodes: map[string]*dag.Node{
				"b": {Key: "b", Prev: []*dag.Node{{Key: "a"}}},
			},
			want: map[string]dag.ChildToParents{
				"a": {Child: "a", Parents: []string{}},
				"b": {Child: "b", Parents: []string{"a"}},
			},
		},
		{
			name: "node with multiple parents",
			nodes: map[string]*dag.Node{
				"c": {Key: "c", Prev: []*dag.Node{{Key: "a"}, {Key: "b"}}},
			},
			want: map[string]dag.ChildToParents{
				"a": {Child: "a", Parents: []string{}},
				"b": {Child: "b", Parents: []string{}},
				"c": {Child: "c", Parents: []string{"a", "b"}},
			},
		},
		{
			name: "complex graph",
			nodes: map[string]*dag.Node{
				"d": {Key: "d", Prev: []*dag.Node{{Key: "b"}, {Key: "c"}}},
				"e": {Key: "e", Prev: []*dag.Node{{Key: "c"}}},
			},

			want: map[string]dag.ChildToParents{
				"b": {Child: "b", Parents: []string{}},
				"c": {Child: "c", Parents: []string{}},
				"d": {Child: "d", Parents: []string{"b", "c"}},
				"e": {Child: "e", Parents: []string{"c"}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := dag.BuildChildToParentsMap(tt.nodes)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GetChildToParentsMap() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNode_GetAllPrevKeys(t *testing.T) {
	// Helper to create nodes and link them
	makeNode := func(key string, prevs ...*dag.Node) *dag.Node {
		return &dag.Node{Key: key, Prev: prevs}
	}

	t.Run("single node with no prev", func(t *testing.T) {
		n := makeNode("a")
		got := n.GetAllPrevKeys()
		if len(got) != 0 {
			t.Errorf("expected no prev keys, got %v", got)
		}
	})

	t.Run("node with one prev", func(t *testing.T) {
		prev := makeNode("b")
		n := makeNode("a", prev)
		got := n.GetAllPrevKeys()
		want := []string{"b"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("expected %v, got %v", want, got)
		}
	})

	t.Run("node with multiple prevs", func(t *testing.T) {
		prev1 := makeNode("b")
		prev2 := makeNode("c")
		n := makeNode("a", prev1, prev2)
		got := n.GetAllPrevKeys()
		want := []string{"b", "c"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("expected %v, got %v", want, got)
		}
	})

	t.Run("node with transitive prevs", func(t *testing.T) {
		prev2 := makeNode("c")
		prev1 := makeNode("b", prev2)
		n := makeNode("a", prev1)
		got := n.GetAllPrevKeys()
		want := []string{"b", "c"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("expected %v, got %v", want, got)
		}
	})

	t.Run("node with shared ancestor", func(t *testing.T) {
		shared := makeNode("x")
		prev1 := makeNode("b", shared)
		prev2 := makeNode("c", shared)
		n := makeNode("a", prev1, prev2)
		got := n.GetAllPrevKeys()
		want := []string{"b", "c", "x"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("expected %v, got %v", want, got)
		}
	})

	t.Run("node with deep shared ancestor", func(t *testing.T) {
		shared := makeNode("x")
		prev1 := makeNode("b", shared)
		prev2 := makeNode("c", shared)
		deep := makeNode("d", prev1, prev2)
		n := makeNode("a", deep)
		got := n.GetAllPrevKeys()
		want := []string{"b", "c", "d", "x"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("expected %v, got %v", want, got)
		}
	})

	t.Run("node with duplicate prevs", func(t *testing.T) {
		shared := makeNode("x")
		n := makeNode("a", shared, shared)
		got := n.GetAllPrevKeys()
		want := []string{"x"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("expected %v, got %v", want, got)
		}
	})

	t.Run("sorted output", func(t *testing.T) {
		prev1 := makeNode("b")
		prev2 := makeNode("a")
		prev3 := makeNode("c")
		n := makeNode("z", prev1, prev2, prev3)
		got := n.GetAllPrevKeys()
		want := []string{"a", "b", "c"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("expected sorted %v, got %v", want, got)
		}
	})
}

func TestRunnerResults_ToAnalysis(t *testing.T) {
	type fields struct {
		ExecutionOrder []dag.ObjectExecution
		TotalTime      time.Duration
	}
	tests := []struct {
		name                     string
		fields                   fields
		sourceOfExecution        string
		timeConsumingThresholdMs int64
		wantAll                  []dag.AnalysisItem
		wantFailed               []dag.AnalysisItem
		wantTimeConsuming        []dag.AnalysisItem
		wantTotalTimeMs          int64
	}{
		{
			name: "all successful, none time-consuming",
			fields: fields{
				ExecutionOrder: []dag.ObjectExecution{
					{Name: "obj1", Position: 1, TotalTime: time.Duration(50 * time.Millisecond), Error: nil, Phase: 1},
					{Name: "obj2", Position: 2, TotalTime: time.Duration(80 * time.Millisecond), Error: nil, Phase: 2},
				},
				TotalTime: time.Duration(130 * time.Millisecond),
			},
			sourceOfExecution:        "test-source",
			timeConsumingThresholdMs: 100,
			wantAll: []dag.AnalysisItem{
				{Name: "obj1", Position: 1, TotalTimeMilliSeconds: 50, Error: nil, Phase: 1, TimeConsuming: false, SourceExecution: "test-source"},
				{Name: "obj2", Position: 2, TotalTimeMilliSeconds: 80, Error: nil, Phase: 2, TimeConsuming: false, SourceExecution: "test-source"},
			},
			wantFailed:        []dag.AnalysisItem{},
			wantTimeConsuming: []dag.AnalysisItem{},
			wantTotalTimeMs:   130,
		},
		{
			name: "one failed, one time-consuming",
			fields: fields{
				ExecutionOrder: []dag.ObjectExecution{
					{Name: "obj1", Position: 1, TotalTime: time.Duration(150 * time.Millisecond), Error: fmt.Errorf("fail"), Phase: 1},
					{Name: "obj2", Position: 2, TotalTime: time.Duration(80 * time.Millisecond), Error: nil, Phase: 2},
				},
				TotalTime: time.Duration(230 * time.Millisecond),
			},
			sourceOfExecution:        "exec",
			timeConsumingThresholdMs: 100,
			wantAll: []dag.AnalysisItem{
				{Name: "obj1", Position: 1, TotalTimeMilliSeconds: 150, Error: fmt.Errorf("fail"), Phase: 1, TimeConsuming: true, SourceExecution: "exec"},
				{Name: "obj2", Position: 2, TotalTimeMilliSeconds: 80, Error: nil, Phase: 2, TimeConsuming: false, SourceExecution: "exec"},
			},
			wantFailed: []dag.AnalysisItem{
				{Name: "obj1", Position: 1, TotalTimeMilliSeconds: 150, Error: fmt.Errorf("fail"), Phase: 1, TimeConsuming: true, SourceExecution: "exec"},
			},
			wantTimeConsuming: []dag.AnalysisItem{
				{Name: "obj1", Position: 1, TotalTimeMilliSeconds: 150, Error: fmt.Errorf("fail"), Phase: 1, TimeConsuming: true, SourceExecution: "exec"},
			},
			wantTotalTimeMs: 230,
		},
		{
			name: "all time-consuming, all failed",
			fields: fields{
				ExecutionOrder: []dag.ObjectExecution{
					{Name: "obj1", Position: 1, TotalTime: time.Duration(200 * time.Millisecond), Error: fmt.Errorf("err1"), Phase: 1},
					{Name: "obj2", Position: 2, TotalTime: time.Duration(300 * time.Millisecond), Error: fmt.Errorf("err2"), Phase: 2},
				},
				TotalTime: time.Duration(500 * time.Millisecond),
			},
			sourceOfExecution:        "src",
			timeConsumingThresholdMs: 100,
			wantAll: []dag.AnalysisItem{
				{Name: "obj1", Position: 1, TotalTimeMilliSeconds: 200, Error: fmt.Errorf("err1"), Phase: 1, TimeConsuming: true, SourceExecution: "src"},
				{Name: "obj2", Position: 2, TotalTimeMilliSeconds: 300, Error: fmt.Errorf("err2"), Phase: 2, TimeConsuming: true, SourceExecution: "src"},
			},
			wantFailed: []dag.AnalysisItem{
				{Name: "obj1", Position: 1, TotalTimeMilliSeconds: 200, Error: fmt.Errorf("err1"), Phase: 1, TimeConsuming: true, SourceExecution: "src"},
				{Name: "obj2", Position: 2, TotalTimeMilliSeconds: 300, Error: fmt.Errorf("err2"), Phase: 2, TimeConsuming: true, SourceExecution: "src"},
			},
			wantTimeConsuming: []dag.AnalysisItem{
				{Name: "obj1", Position: 1, TotalTimeMilliSeconds: 200, Error: fmt.Errorf("err1"), Phase: 1, TimeConsuming: true, SourceExecution: "src"},
				{Name: "obj2", Position: 2, TotalTimeMilliSeconds: 300, Error: fmt.Errorf("err2"), Phase: 2, TimeConsuming: true, SourceExecution: "src"},
			},
			wantTotalTimeMs: 500,
		},
		{
			name: "empty execution order",
			fields: fields{
				ExecutionOrder: []dag.ObjectExecution{},
				TotalTime:      time.Duration(0),
			},
			sourceOfExecution:        "empty",
			timeConsumingThresholdMs: 100,
			wantAll:                  []dag.AnalysisItem{},
			wantFailed:               []dag.AnalysisItem{},
			wantTimeConsuming:        []dag.AnalysisItem{},
			wantTotalTimeMs:          0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &dag.RunnerResults{
				ExecutionOrder: tt.fields.ExecutionOrder,
				TotalTime:      tt.fields.TotalTime,
			}
			got := r.ToAnalysis(tt.sourceOfExecution, tt.timeConsumingThresholdMs)
			if got.TotalTimeMilliseconds != tt.wantTotalTimeMs {
				t.Errorf("TotalTimeMilliseconds = %v, want %v", got.TotalTimeMilliseconds, tt.wantTotalTimeMs)
			}
			// Compare All
			if len(got.All) != len(tt.wantAll) {
				t.Errorf("All len = %d, want %d", len(got.All), len(tt.wantAll))
			}
			for i := range got.All {
				if got.All[i].Name != tt.wantAll[i].Name ||
					got.All[i].Position != tt.wantAll[i].Position ||
					got.All[i].TotalTimeMilliSeconds != tt.wantAll[i].TotalTimeMilliSeconds ||
					got.All[i].Phase != tt.wantAll[i].Phase ||
					got.All[i].TimeConsuming != tt.wantAll[i].TimeConsuming ||
					got.All[i].SourceExecution != tt.wantAll[i].SourceExecution ||
					((got.All[i].Error == nil) != (tt.wantAll[i].Error == nil)) ||
					(got.All[i].Error != nil && tt.wantAll[i].Error != nil && got.All[i].Error.Error() != tt.wantAll[i].Error.Error()) {
					t.Errorf("All[%d] = %+v, want %+v", i, got.All[i], tt.wantAll[i])
				}
			}
			// Compare Failed
			if len(got.Failed) != len(tt.wantFailed) {
				t.Errorf("Failed len = %d, want %d", len(got.Failed), len(tt.wantFailed))
			}
			for i := range got.Failed {
				if got.Failed[i].Name != tt.wantFailed[i].Name ||
					got.Failed[i].Position != tt.wantFailed[i].Position ||
					got.Failed[i].TotalTimeMilliSeconds != tt.wantFailed[i].TotalTimeMilliSeconds ||
					got.Failed[i].Phase != tt.wantFailed[i].Phase ||
					got.Failed[i].TimeConsuming != tt.wantFailed[i].TimeConsuming ||
					got.Failed[i].SourceExecution != tt.wantFailed[i].SourceExecution ||
					((got.Failed[i].Error == nil) != (tt.wantFailed[i].Error == nil)) ||
					(got.Failed[i].Error != nil && tt.wantFailed[i].Error != nil && got.Failed[i].Error.Error() != tt.wantFailed[i].Error.Error()) {
					t.Errorf("Failed[%d] = %+v, want %+v", i, got.Failed[i], tt.wantFailed[i])
				}
			}
			// Compare TimeConsuming
			if len(got.TimeConsuming) != len(tt.wantTimeConsuming) {
				t.Errorf("TimeConsuming len = %d, want %d", len(got.TimeConsuming), len(tt.wantTimeConsuming))
			}
			for i := range got.TimeConsuming {
				if got.TimeConsuming[i].Name != tt.wantTimeConsuming[i].Name ||
					got.TimeConsuming[i].Position != tt.wantTimeConsuming[i].Position ||
					got.TimeConsuming[i].TotalTimeMilliSeconds != tt.wantTimeConsuming[i].TotalTimeMilliSeconds ||
					got.TimeConsuming[i].Phase != tt.wantTimeConsuming[i].Phase ||
					got.TimeConsuming[i].TimeConsuming != tt.wantTimeConsuming[i].TimeConsuming ||
					got.TimeConsuming[i].SourceExecution != tt.wantTimeConsuming[i].SourceExecution ||
					((got.TimeConsuming[i].Error == nil) != (tt.wantTimeConsuming[i].Error == nil)) ||
					(got.TimeConsuming[i].Error != nil && tt.wantTimeConsuming[i].Error != nil && got.TimeConsuming[i].Error.Error() != tt.wantTimeConsuming[i].Error.Error()) {
					t.Errorf("TimeConsuming[%d] = %+v, want %+v", i, got.TimeConsuming[i], tt.wantTimeConsuming[i])
				}
			}
		})
	}
}

func TestAnalysis_ToMap(t *testing.T) {
	tests := []struct {
		name     string
		analysis *dag.Analysis
		wantNil  bool
		wantErr  bool
	}{
		{
			name: "analysis with fields returns map",
			analysis: &dag.Analysis{
				TotalTimeMilliseconds: 123,
				All: []dag.AnalysisItem{
					{Name: "obj1", Position: 1, TotalTimeMilliSeconds: 50, Phase: 1, TimeConsuming: false, SourceExecution: "src"},
				},
				Failed: []dag.AnalysisItem{
					{Name: "obj2", Position: 2, TotalTimeMilliSeconds: 80, Phase: 2, TimeConsuming: true, SourceExecution: "src", Error: fmt.Errorf("fail")},
				},
				TimeConsuming: []dag.AnalysisItem{
					{Name: "obj2", Position: 2, TotalTimeMilliSeconds: 80, Phase: 2, TimeConsuming: true, SourceExecution: "src", Error: fmt.Errorf("fail")},
				},
			},
			wantNil: false,
			wantErr: false,
		},
		{
			name: "empty analysis returns map",
			analysis: &dag.Analysis{
				TotalTimeMilliseconds: int64(0),
			},
			wantNil: false,
			wantErr: false,
		},
		{
			name:     "nil receiver returns nil map and no error",
			analysis: nil,
			wantNil:  true,
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.analysis.ToMap()
			if tt.wantErr && err == nil {
				t.Errorf("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if tt.wantNil && got != nil {
				t.Errorf("expected nil map, got %v", got)
			}
			if !tt.wantNil && got == nil {
				t.Errorf("expected non-nil map, got nil")
			}
			if got != nil {
				// Spot check some keys
				if tt.analysis != nil {
					if v, ok := got["totalTimeMilliseconds"]; !ok || v != float64(tt.analysis.TotalTimeMilliseconds) {
						t.Errorf("TotalTimeMilliseconds = %v, want %v", v, tt.analysis.TotalTimeMilliseconds)
					}
					if _, ok := got["all"]; !ok {
						t.Errorf("expected key 'All' in map")
					}
					if _, ok := got["failed"]; !ok {
						t.Errorf("expected key 'Failed' in map")
					}
					if _, ok := got["timeConsuming"]; !ok {
						t.Errorf("expected key 'TimeConsuming' in map")
					}
				}
			}
		})
	}
}

func TestRunnerResults_HasAnError(t *testing.T) {
	tests := []struct {
		name           string
		executionOrder []dag.ObjectExecution
		want           bool
	}{
		{
			name:           "no executions",
			executionOrder: []dag.ObjectExecution{},
			want:           false,
		},
		{
			name: "all successful",
			executionOrder: []dag.ObjectExecution{
				{Name: "obj1", Error: nil},
				{Name: "obj2", Error: nil},
			},
			want: false,
		},
		{
			name: "one failed",
			executionOrder: []dag.ObjectExecution{
				{Name: "obj1", Error: nil},
				{Name: "obj2", Error: fmt.Errorf("fail")},
			},
			want: true,
		},
		{
			name: "multiple failed",
			executionOrder: []dag.ObjectExecution{
				{Name: "obj1", Error: fmt.Errorf("err1")},
				{Name: "obj2", Error: fmt.Errorf("err2")},
			},
			want: true,
		},
		{
			name: "first failed",
			executionOrder: []dag.ObjectExecution{
				{Name: "obj1", Error: fmt.Errorf("err")},
				{Name: "obj2", Error: nil},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &dag.RunnerResults{
				ExecutionOrder: tt.executionOrder,
			}
			got := r.HasAnError()
			if got != tt.want {
				t.Errorf("HasAnError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRunnerResults_GetPhaseMap(t *testing.T) {
	tests := []struct {
		name           string
		executionOrder []dag.ObjectExecution
		want           map[int][]dag.ObjectExecution
	}{
		{
			name:           "empty execution order",
			executionOrder: []dag.ObjectExecution{},
			want:           map[int][]dag.ObjectExecution{},
		},
		{
			name: "single phase",
			executionOrder: []dag.ObjectExecution{
				{Name: "obj1", Phase: 1},
				{Name: "obj2", Phase: 1},
			},
			want: map[int][]dag.ObjectExecution{
				1: {
					{Name: "obj1", Phase: 1},
					{Name: "obj2", Phase: 1},
				},
			},
		},
		{
			name: "multiple phases",
			executionOrder: []dag.ObjectExecution{
				{Name: "obj1", Phase: 1},
				{Name: "obj2", Phase: 2},
				{Name: "obj3", Phase: 1},
				{Name: "obj4", Phase: 3},
			},
			want: map[int][]dag.ObjectExecution{
				1: {
					{Name: "obj1", Phase: 1},
					{Name: "obj3", Phase: 1},
				},
				2: {
					{Name: "obj2", Phase: 2},
				},
				3: {
					{Name: "obj4", Phase: 3},
				},
			},
		},
		{
			name: "phases with gaps",
			executionOrder: []dag.ObjectExecution{
				{Name: "obj1", Phase: 1},
				{Name: "obj2", Phase: 3},
			},
			want: map[int][]dag.ObjectExecution{
				1: {
					{Name: "obj1", Phase: 1},
				},
				3: {
					{Name: "obj2", Phase: 3},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &dag.RunnerResults{
				ExecutionOrder: tt.executionOrder,
			}
			got := r.GetPhaseMap()

			d := cmp.Diff(got, tt.want)
			if d != "" {
				t.Errorf("GetPhaseMap() (-want +got):\n%s", d)
				return
			}
		})
	}
}

func TestRunnerResults_BuildDependencyTree(t *testing.T) {
	// Helper to create ObjectExecution
	makeExec := func(name string, phase int, totalTime time.Duration) dag.ObjectExecution {
		return dag.ObjectExecution{
			Name:      name,
			Phase:     phase,
			TotalTime: totalTime,
		}
	}

	t.Run("single root, no children", func(t *testing.T) {
		r := &dag.RunnerResults{
			ExecutionOrder: []dag.ObjectExecution{
				makeExec("root", 1, 10*time.Millisecond),
			},
		}
		phaseMap := map[int][]dag.ObjectExecution{
			1: {makeExec("root", 1, 10*time.Millisecond)},
		}
		childToParents := map[string]dag.ChildToParents{
			"root": {Child: "root", Parents: []string{}},
		}
		got := r.BuildDependencyTree(phaseMap, childToParents)
		if len(got) != 1 {
			t.Fatalf("expected 1 root node, got %d", len(got))
		}
		if got[0].Name != "root" {
			t.Errorf("expected root node name 'root', got %q", got[0].Name)
		}
		if len(got[0].Children) != 0 {
			t.Errorf("expected no children, got %d", len(got[0].Children))
		}
	})

	t.Run("root with one child", func(t *testing.T) {
		r := &dag.RunnerResults{
			ExecutionOrder: []dag.ObjectExecution{
				makeExec("root", 1, 10*time.Millisecond),
				makeExec("child", 2, 20*time.Millisecond),
			},
		}
		phaseMap := map[int][]dag.ObjectExecution{
			1: {makeExec("root", 1, 10*time.Millisecond)},
		}
		childToParents := map[string]dag.ChildToParents{
			"root":  {Child: "root", Parents: []string{}},
			"child": {Child: "child", Parents: []string{"root"}},
		}
		got := r.BuildDependencyTree(phaseMap, childToParents)
		if len(got) != 1 {
			t.Fatalf("expected 1 root node, got %d", len(got))
		}
		root := got[0]
		if root.Name != "root" {
			t.Errorf("expected root node name 'root', got %q", root.Name)
		}
		if len(root.Children) != 1 {
			t.Errorf("expected 1 child, got %d", len(root.Children))
		}
		if _, ok := root.Children["child"]; !ok {
			t.Errorf("expected child node 'child' in children")
		}
	})

	t.Run("multiple roots and children", func(t *testing.T) {
		r := &dag.RunnerResults{
			ExecutionOrder: []dag.ObjectExecution{
				makeExec("a", 1, 10*time.Millisecond),
				makeExec("b", 1, 15*time.Millisecond),
				makeExec("c", 2, 20*time.Millisecond),
				makeExec("d", 2, 25*time.Millisecond),
			},
		}
		phaseMap := map[int][]dag.ObjectExecution{
			1: {makeExec("a", 1, 10*time.Millisecond), makeExec("b", 1, 15*time.Millisecond)},
		}
		childToParents := map[string]dag.ChildToParents{
			"a": {Child: "a", Parents: []string{}},
			"b": {Child: "b", Parents: []string{}},
			"c": {Child: "c", Parents: []string{"a"}},
			"d": {Child: "d", Parents: []string{"b"}},
		}
		got := r.BuildDependencyTree(phaseMap, childToParents)
		if len(got) != 2 {
			t.Fatalf("expected 2 root nodes, got %d", len(got))
		}
		rootNames := map[string]bool{}
		for _, root := range got {
			rootNames[root.Name] = true
		}
		if !rootNames["a"] || !rootNames["b"] {
			t.Errorf("expected roots 'a' and 'b', got %v", rootNames)
		}
		for _, root := range got {
			if root.Name == "a" {
				if _, ok := root.Children["c"]; !ok {
					t.Errorf("expected 'a' to have child 'c'")
				}
			}
			if root.Name == "b" {
				if _, ok := root.Children["d"]; !ok {
					t.Errorf("expected 'b' to have child 'd'")
				}
			}
		}
	})

	t.Run("child with multiple parents", func(t *testing.T) {
		r := &dag.RunnerResults{
			ExecutionOrder: []dag.ObjectExecution{
				makeExec("p1", 1, 10*time.Millisecond),
				makeExec("p2", 1, 15*time.Millisecond),
				makeExec("c", 2, 20*time.Millisecond),
			},
		}
		phaseMap := map[int][]dag.ObjectExecution{
			1: {makeExec("p1", 1, 10*time.Millisecond), makeExec("p2", 1, 15*time.Millisecond)},
		}
		childToParents := map[string]dag.ChildToParents{
			"p1": {Child: "p1", Parents: []string{}},
			"p2": {Child: "p2", Parents: []string{}},
			"c":  {Child: "c", Parents: []string{"p1", "p2"}},
		}
		got := r.BuildDependencyTree(phaseMap, childToParents)
		if len(got) != 2 {
			t.Fatalf("expected 2 root nodes, got %d", len(got))
		}
		for _, root := range got {
			if root.Name == "p1" {
				if _, ok := root.Children["c"]; !ok {
					t.Errorf("expected 'p1' to have child 'c'")
				}
			}
			if root.Name == "p2" {
				if _, ok := root.Children["c"]; !ok {
					t.Errorf("expected 'p2' to have child 'c'")
				}
			}
		}
	})

	t.Run("missing child in nodeMap", func(t *testing.T) {
		r := &dag.RunnerResults{
			ExecutionOrder: []dag.ObjectExecution{
				makeExec("a", 1, 10*time.Millisecond),
			},
		}
		phaseMap := map[int][]dag.ObjectExecution{
			1: {makeExec("a", 1, 10*time.Millisecond)},
		}
		childToParents := map[string]dag.ChildToParents{
			"missing": {Child: "missing", Parents: []string{"a"}},
			"a":       {Child: "a", Parents: []string{}},
		}
		got := r.BuildDependencyTree(phaseMap, childToParents)
		if len(got) != 1 {
			t.Fatalf("expected 1 root node, got %d", len(got))
		}
		if got[0].Name != "a" {
			t.Errorf("expected root node name 'a', got %q", got[0].Name)
		}
	})
}

func TestNode_Compare(t *testing.T) {
	makeNode := func(key string, prevNodes, nextNodes []*dag.Node) *dag.Node {
		return &dag.Node{Key: key, Prev: prevNodes, Next: nextNodes}
	}

	t.Run("equal nodes with no prev or next", func(t *testing.T) {
		a := makeNode("a", nil, nil)
		b := makeNode("a", nil, nil)
		if !a.Compare(b) {
			t.Errorf("expected nodes to be equal")
		}
	})

	t.Run("different keys", func(t *testing.T) {
		a := makeNode("a", nil, nil)
		b := makeNode("b", nil, nil)
		if a.Compare(b) {
			t.Errorf("expected nodes with different keys to be not equal")
		}
	})

	t.Run("different prev length", func(t *testing.T) {
		prev := makeNode("p", nil, nil)
		a := makeNode("a", []*dag.Node{prev}, nil)
		b := makeNode("a", nil, nil)
		if a.Compare(b) {
			t.Errorf("expected nodes with different prev length to be not equal")
		}
	})

	t.Run("different next length", func(t *testing.T) {
		next := makeNode("n", nil, nil)
		a := makeNode("a", nil, []*dag.Node{next})
		b := makeNode("a", nil, nil)
		if a.Compare(b) {
			t.Errorf("expected nodes with different next length to be not equal")
		}
	})

	t.Run("different prev keys", func(t *testing.T) {
		prev1 := makeNode("p1", nil, nil)
		prev2 := makeNode("p2", nil, nil)
		a := makeNode("a", []*dag.Node{prev1}, nil)
		b := makeNode("a", []*dag.Node{prev2}, nil)
		if a.Compare(b) {
			t.Errorf("expected nodes with different prev keys to be not equal")
		}
	})

	t.Run("different next keys", func(t *testing.T) {
		next1 := makeNode("n1", nil, nil)
		next2 := makeNode("n2", nil, nil)
		a := makeNode("a", nil, []*dag.Node{next1})
		b := makeNode("a", nil, []*dag.Node{next2})
		if a.Compare(b) {
			t.Errorf("expected nodes with different next keys to be not equal")
		}
	})

	t.Run("equal nodes with prev and next", func(t *testing.T) {
		prev := makeNode("p", nil, nil)
		next := makeNode("n", nil, nil)
		a := makeNode("a", []*dag.Node{prev}, []*dag.Node{next})
		b := makeNode("a", []*dag.Node{prev}, []*dag.Node{next})
		if !a.Compare(b) {
			t.Errorf("expected nodes with same prev and next to be equal")
		}
	})

	t.Run("multiple prev and next", func(t *testing.T) {
		prev1 := makeNode("p1", nil, nil)
		prev2 := makeNode("p2", nil, nil)
		next1 := makeNode("n1", nil, nil)
		next2 := makeNode("n2", nil, nil)
		a := makeNode("a", []*dag.Node{prev1, prev2}, []*dag.Node{next1, next2})
		b := makeNode("a", []*dag.Node{prev1, prev2}, []*dag.Node{next1, next2})
		if !a.Compare(b) {
			t.Errorf("expected nodes with same multiple prev and next to be equal")
		}
	})

	t.Run("order matters in prev and next", func(t *testing.T) {
		prev1 := makeNode("p1", nil, nil)
		prev2 := makeNode("p2", nil, nil)
		next1 := makeNode("n1", nil, nil)
		next2 := makeNode("n2", nil, nil)
		a := makeNode("a", []*dag.Node{prev1, prev2}, []*dag.Node{next1, next2})
		b := makeNode("a", []*dag.Node{prev2, prev1}, []*dag.Node{next2, next1})
		if a.Compare(b) {
			t.Errorf("expected nodes with different order in prev/next to be not equal")
		}
	})
}
