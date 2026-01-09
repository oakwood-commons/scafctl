package dag

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/util/sets"
)

// GetAllPrevKeys returns a sorted list of all unique keys of ancestor nodes for the current node.
// It traverses the graph recursively and ensures that each key appears only once, even if there are multiple paths to the same ancestor.
func (n *Node) GetAllPrevKeys() (keys []string) {
	// The visited map ensures that keys are not duplicated even if there are multiple paths to the same ancestor.
	keys = []string{}
	visited := make(map[string]bool)
	var visit func(n *Node)
	visit = func(n *Node) {
		for _, prev := range n.Prev {
			if !visited[prev.Key] {
				visited[prev.Key] = true
				keys = append(keys, prev.Key)
				visit(prev)
			}
		}
	}
	visit(n)
	sort.Strings(keys)
	return keys
}

// ToAnalysis generates an Analysis report from the RunnerResults.
// It takes a sourceOfExecution string and a timeConsumingThresholdMs value (in milliseconds).
// The function categorizes execution items into all, failed, and time-consuming groups based on their execution time and error status.
// Returns an Analysis struct containing the categorized results and total execution time.
func (r *RunnerResults) ToAnalysis(sourceOfExecution string, timeConsumingThresholdMs int64) Analysis {
	timeConsumingThreshold := time.Duration(timeConsumingThresholdMs) * time.Millisecond

	analysis := Analysis{
		TotalTimeMilliseconds: r.TotalTime.Milliseconds(),
		All:                   []AnalysisItem{},
		Failed:                []AnalysisItem{},
		TimeConsuming:         []AnalysisItem{},
	}

	for _, exec := range r.ExecutionOrder {
		item := AnalysisItem{
			Name:                  exec.Name,
			Position:              exec.Position,
			TotalTimeMilliSeconds: exec.TotalTime.Milliseconds(),
			Error:                 exec.Error,
			Phase:                 exec.Phase,
			TimeConsuming:         exec.TotalTime > timeConsumingThreshold,
			SourceExecution:       sourceOfExecution,
		}
		analysis.All = append(analysis.All, item)
		if exec.Error != nil {
			analysis.Failed = append(analysis.Failed, item)
		}
		if exec.TotalTime > timeConsumingThreshold {
			analysis.TimeConsuming = append(analysis.TimeConsuming, item)
		}
	}
	return analysis
}

// ToMap converts the Analysis struct to a map[string]any representation.
// It marshals the struct to JSON and then unmarshals it into a map.
// Returns nil if the receiver is nil, or an error if unmarshalling fails.
func (r *Analysis) ToMap() (map[string]any, error) {
	if r == nil {
		return nil, nil
	}
	res := map[string]any{}
	b, _ := json.Marshal(r)
	if err := json.Unmarshal(b, &res); err != nil {
		return nil, err
	}
	return res, nil
}

// HasAnError checks if any item in the ExecutionOrder has a non-nil Error.
// It returns true if at least one error is found, otherwise false.
func (r *RunnerResults) HasAnError() bool {
	for _, item := range r.ExecutionOrder {
		if item.Error != nil {
			return true
		}
	}
	return false
}

// GetPhaseMap constructs and returns a map where the keys are phase identifiers (int)
// and the values are slices of ObjectExecution corresponding to each phase.
// It iterates over the ExecutionOrder field of RunnerResults, grouping ObjectExecution
// instances by their Phase value.
func (r *RunnerResults) GetPhaseMap() map[int][]ObjectExecution {
	phaseMap := map[int][]ObjectExecution{}
	for _, ex := range r.ExecutionOrder {
		phaseMap[ex.Phase] = append(phaseMap[ex.Phase], ex)
	}
	return phaseMap
}

// BuildDependencyTree constructs a dependency tree of execution objects based on the provided phase map and child-to-parents relationships.
// It initializes nodes for each execution in the runner results, links children to their respective parent nodes, and returns the root nodes
// corresponding to the top-level executions (phase 1). The resulting tree allows traversal of dependencies between executions.
//
// Parameters:
//   - phaseMap: a map where the key is the phase number and the value is a slice of ObjectExecution representing executions in that phase.
//   - childToParents: a map where the key is the child's name and the value is a ChildToParents struct containing its parent names.
//
// Returns:
//   - A slice of DependencyNode representing the root nodes of the constructed dependency tree.
func (r *RunnerResults) BuildDependencyTree(phaseMap map[int][]ObjectExecution, childToParents map[string]ChildToParents) []DependencyNode {
	// Create a map to store nodes by their name for quick lookup
	nodeMap := make(map[string]*DependencyNode)

	// Initialize nodes for all providers in the runner results
	for _, execution := range r.ExecutionOrder {
		nodeMap[execution.Name] = &DependencyNode{
			Name:      execution.Name,
			TotalTime: execution.TotalTime,
			Phase:     execution.Phase,
			Children:  make(map[string]*DependencyNode),
		}
	}

	// Build the tree structure using the child-to-parents map
	for child, parents := range childToParents {
		// Track nodes that have been added as children to avoid duplication
		// addedAsChild := make(map[string]bool)
		childNode, exists := nodeMap[child]
		if !exists {
			continue
		}

		for _, parent := range parents.Parents {
			parentNode, exists := nodeMap[parent]
			if !exists {
				continue
			}
			parentNode.Children[child] = childNode
		}
	}

	// Collect root nodes based on the phaseMap
	var rootNodes []DependencyNode
	if topLevelExecutions, exists := phaseMap[1]; exists {
		for _, execution := range topLevelExecutions {
			if rootNode, exists := nodeMap[execution.Name]; exists {
				rootNodes = append(rootNodes, *rootNode)
			}
		}
	}

	return rootNodes
}

// newGraph creates and returns a new Graph instance with an initialized empty set of nodes.
func newGraph() *Graph {
	return &Graph{Nodes: map[string]*Node{}}
}

// addDagObject adds a new DAG object to the graph as a node.
// It returns an error if a node with the same key already exists in the graph.
// The function creates a new Node with the key obtained from the provided Object,
// adds it to the graph's Nodes map, and returns the newly created Node.
func (g *Graph) addDagObject(t Object) (*Node, error) {
	if _, ok := g.Nodes[t.DagKey()]; ok {
		return nil, errors.New("duplicate dagObject")
	}
	newNode := &Node{
		Key: t.DagKey(),
	}
	g.Nodes[t.DagKey()] = newNode
	return newNode, nil
}

// Build constructs a directed acyclic graph (DAG) from the provided dagObjects and their dependencies.
// It first adds all DAG items to the graph, ensuring no duplicates. Then, it checks for cycles in the
// dependency map and returns an error if any are found. Finally, it processes all dependency constraints
// to establish links between nodes in the graph. Returns the constructed Graph or an error if any issues
// are encountered during the process.
//
// Parameters:
//   - dagObjects: Objects containing the DAG items to be added to the graph.
//   - deps: A map where keys are DAG object identifiers and values are slices of identifiers representing dependencies.
//
// Returns:
//   - *Graph: The constructed DAG.
//   - error: An error if a duplicate DAG object is found, a cycle is detected, or a link cannot be established.
func Build(dagObjects Objects, deps map[string][]string) (*Graph, error) {
	d := newGraph()

	for _, pt := range dagObjects.DagItems() {
		if _, err := d.addDagObject(pt); err != nil {
			return nil, fmt.Errorf("dagObject %s is already present in Graph, can't add it again: %w", pt.DagKey(), err)
		}
	}

	// Ensure no cycles in the graph
	if err := findCyclesInDependencies(deps); err != nil {
		return nil, fmt.Errorf("cycle detected; %w", err)
	}

	// Process all from and runAfter constraints to add dagObject dependency
	for r, dagObjectDeps := range deps {
		for _, previousDagObject := range dagObjectDeps {
			if err := addLink(r, previousDagObject, d.Nodes); err != nil {
				return nil, fmt.Errorf("couldn't add link between %s and %s: %w", r, previousDagObject, err)
			}
		}
	}
	return d, nil
}

// linkDagObjects links two DAG nodes by updating their Prev and Next slices.
// It appends 'prev' to the 'Prev' slice of 'next', and 'next' to the 'Next' slice of 'prev'.
// This establishes a directed edge from 'prev' to 'next' in the DAG.
func linkDagObjects(prev, next *Node) {
	next.Prev = append(next.Prev, prev)
	prev.Next = append(prev.Next, next)
}

// findCyclesInDependencies analyzes a dependency graph represented by the input map,
// where each key is a DAG object and its value is a slice of dependencies.
// It detects cycles in the dependency graph using a variant of Kahn's algorithm.
// If cycles are found, it returns an error describing the interdependencies;
// otherwise, it returns nil.
// https://stackoverflow.com/questions/67644378/detecting-cycles-in-topological-sort-using-kahns-algorithm-in-degree-out-deg
func findCyclesInDependencies(deps map[string][]string) error {
	independentDagObjects := sets.NewString()
	dag := make(map[string]sets.Set[string], len(deps))
	childMap := make(map[string]sets.Set[string], len(deps))
	for dagObject, dagObjectDeps := range deps {
		if len(dagObjectDeps) == 0 {
			continue
		}
		dag[dagObject] = sets.Set[string]{}
		dag[dagObject].Insert(dagObjectDeps...)

		for _, dep := range dagObjectDeps {
			if len(deps[dep]) == 0 {
				independentDagObjects.Insert(dep)
			}
			if children, ok := childMap[dep]; ok {
				children.Insert(dagObject)
			} else {
				childMap[dep] = sets.Set[string]{}
				childMap[dep].Insert(dagObject)
			}
		}
	}

	for {
		parent, ok := independentDagObjects.PopAny()
		if !ok {
			break
		}
		children := childMap[parent]
		for {
			child, ok := children.PopAny()
			if !ok {
				break
			}
			dag[child].Delete(parent)
			if dag[child].Len() == 0 {
				independentDagObjects.Insert(child)
				delete(dag, child)
			}
		}
	}

	return getInterdependencyError(dag)
}

// getInterdependencyError returns an error describing the interdependencies of the first
// dagObject in the provided DAG map. If the DAG is empty, it returns nil.
// The error message specifies which dagObject depends on which other objects.
// Dependencies are listed in sorted order and quoted.
// The function is intended to help diagnose cyclic or unexpected dependencies in a DAG structure.
func getInterdependencyError(dag map[string]sets.Set[string]) error {
	if len(dag) == 0 {
		return nil
	}
	firstChild := ""
	for dagObject := range dag {
		if firstChild == "" || firstChild > dagObject {
			firstChild = dagObject
		}
	}
	deps := sets.List(dag[firstChild])
	depNames := make([]string, 0, len(deps))
	sort.Strings(deps)
	for _, dep := range deps {
		depNames = append(depNames, fmt.Sprintf("%q", dep))
	}
	return fmt.Errorf("dagObject %q depends on %s", firstChild, strings.Join(depNames, ", "))
}

// addLink creates a link between two DAG nodes by connecting the node identified by
// previousDagObject to the node identified by r. It returns an error if the previousDagObject
// node does not exist in the provided nodes map.
func addLink(r, previousDagObject string, nodes map[string]*Node) error {
	prev, ok := nodes[previousDagObject]
	if !ok {
		return fmt.Errorf("dagObject %s depends on %s but %s wasn't present", r, previousDagObject, previousDagObject)
	}
	next := nodes[r]
	linkDagObjects(prev, next)
	return nil
}

func getRoots(g *Graph) []*Node {
	n := []*Node{}
	for _, node := range g.Nodes {
		if len(node.Prev) == 0 {
			n = append(n, node)
		}
	}
	return n
}

// GetCandidateDagObjects returns a set of DAG object names that are candidates for scheduling,
// given a graph and a list of completed DAG object names. It traverses the graph from its roots,
// determines which objects are schedulable based on their dependencies, and ensures that the
// provided list of completed objects is valid (i.e., no object is marked as done before its
// ancestors). If the list of completed objects is invalid, an error is returned.
func GetCandidateDagObjects(g *Graph, doneDagObjects ...string) (sets.Set[string], error) {
	roots := getRoots(g)
	tm := sets.Set[string]{}
	tm.Insert(doneDagObjects...)

	d := sets.Set[string]{}

	visited := sets.Set[string]{}
	for _, root := range roots {
		schedulable := findSchedulable(root, visited, tm)
		for _, dagObjectName := range schedulable {
			d.Insert(dagObjectName)
		}
	}

	visitedNames := make([]string, 0, len(visited))
	for v := range visited {
		visitedNames = append(visitedNames, v)
	}

	notVisited := DiffLeft(doneDagObjects, visitedNames)
	if len(notVisited) > 0 {
		return nil, fmt.Errorf("invalid list of done dagObjects; some dagObjects were indicated completed without ancestors being done: %v", notVisited)
	}

	return d, nil
}

// DiffLeft returns a slice containing elements that are present in the 'left' slice but not in the 'right' slice.
// It compares elements using equality and preserves the order from the 'left' slice.
// Duplicate elements in 'left' that are not in 'right' will be included in the result.
func DiffLeft(left, right []string) []string {
	extra := []string{}
	for _, s := range left {
		found := false
		for _, s2 := range right {
			if s == s2 {
				found = true
			}
		}
		if !found {
			extra = append(extra, s)
		}
	}
	return extra
}

// findSchedulable traverses the DAG starting from node n and returns a slice of keys
// representing nodes that are ready to be scheduled. It uses the visited set to avoid
// revisiting nodes and the doneDagObjects set to track completed nodes. If a node is
// already completed, the function recursively checks its successors. If a node is not
// completed but all its predecessors are done, it is considered schedulable and its key
// is returned. Otherwise, an empty slice is returned.
func findSchedulable(n *Node, visited, doneDagObjects sets.Set[string]) []string {
	if visited.Has(n.Key) {
		return []string{}
	}
	visited.Insert(n.Key)
	if doneDagObjects.Has(n.Key) {
		schedulable := []string{}
		// This one is done! Take note of it and look at the next candidate
		for _, next := range n.Next {
			if _, ok := visited[next.Key]; !ok {
				schedulable = append(schedulable, findSchedulable(next, visited, doneDagObjects)...)
			}
		}
		return schedulable
	}
	// This one isn't done! Return it if it's schedulable
	if isSchedulable(doneDagObjects, n.Prev) {
		return []string{n.Key}
	}
	// This one isn't done, but it also isn't ready to schedule
	return []string{}
}

// isSchedulable determines if all predecessor nodes in the DAG have been completed.
// It returns true if either there are no predecessors, or all predecessor nodes' keys
// are present in the doneDagObjects set.
func isSchedulable(doneDagObjects sets.Set[string], prevs []*Node) bool {
	if len(prevs) == 0 {
		return true
	}
	collected := []string{}
	for _, n := range prevs {
		if doneDagObjects.Has(n.Key) {
			collected = append(collected, n.Key)
		}
	}
	return len(collected) == len(prevs)
}

// Compare checks whether the current Node is structurally equal to another Node.
// It compares the Key, the length of Prev and Next slices, and the Keys of all
// Nodes in Prev and Next. Returns true if all these properties match, false otherwise.
func (n *Node) Compare(other *Node) bool {
	if n.Key != other.Key {
		return false
	}
	if len(n.Prev) != len(other.Prev) || len(n.Next) != len(other.Next) {
		return false
	}
	for i := range n.Prev {
		if n.Prev[i].Key != other.Prev[i].Key {
			return false
		}
	}
	for i := range n.Next {
		if n.Next[i].Key != other.Next[i].Key {
			return false
		}
	}
	return true
}

// BuildChildToParentsMap constructs a mapping from each child node to its parent nodes.
// Given a map of node keys to Node pointers, it returns a map where each key is a child node's key,
// and the value is a ChildToParents struct containing the child key and a slice of its parent keys.
// This function ensures that all nodes and their parents are represented in the resulting map.
func BuildChildToParentsMap(nodes map[string]*Node) (childToParents map[string]ChildToParents) {
	childToParents = map[string]ChildToParents{}
	for _, node := range nodes {
		if _, ok := childToParents[node.Key]; !ok {
			childToParents[node.Key] = ChildToParents{
				Child:   node.Key,
				Parents: []string{},
			}
		}
		parents := make([]string, 0, len(node.Prev))
		for _, prev := range node.Prev {
			if _, ok := childToParents[prev.Key]; !ok {
				childToParents[prev.Key] = ChildToParents{
					Child:   prev.Key,
					Parents: []string{},
				}
			}
			parents = append(parents, prev.Key)
		}
		old := childToParents[node.Key]
		old.Parents = parents
		childToParents[node.Key] = old
	}
	return childToParents
}
