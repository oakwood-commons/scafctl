package dag

import "time"

// Object represents an object within a directed acyclic graph (DAG).
// It provides methods to retrieve a unique key for the object and to obtain
// the keys of its dependencies based on provided mappings.
//
// DagKey returns a unique string key identifying the DAG object.
//
// GetDependencyKeys returns a slice of dependency keys for the DAG object.
// It uses the provided objectNamesMap, apiDepsMap, and aliasMap to resolve
// dependencies:
//   - objectNamesMap: maps object names to their keys i.e. resolver name would be the key and the value
//   - apiDepsMap: maps object keys to their API dependencies.
//   - aliasMap: maps aliases to object keys.
type Object interface {
	DagKey() string
	GetDependencyKeys(objectNamesMap map[string]string, apiDepsMap map[string][]string, aliasMap map[string]string) []string
}

// Objects represents a collection of Object instances that can be retrieved as a slice.
// Implementations of this interface should provide the DagItems method to return all contained Object elements.
type Objects interface {
	DagItems() []Object
}

// Node represents a node in a directed acyclic graph (DAG).
// Each node has a unique key and maintains references to its
// previous and next nodes in the graph.
//
// Key:  A unique identifier for the node.
// Prev: A slice of pointers to the previous nodes in the graph.
// Next: A slice of pointers to the next nodes in the graph.
type Node struct {
	// Key represent a unique name of the node in a graph
	Key string
	// Prev represent all the Previous object Nodes for the current object
	Prev []*Node
	// Next represent all the Next object Nodes for the current object
	Next []*Node
}

// Graph represents a directed acyclic graph (DAG) structure,
// where Nodes is a map of node identifiers to their corresponding Node objects.
type Graph struct {
	// Nodes maps node identifiers to their corresponding Node instances, representing the set of nodes in the DAG.
	Nodes map[string]*Node
}

// DependencyNode represents a node in a dependency tree, containing information about the object,
// its child dependencies, execution time, and execution phase.
// Name is the identifier for the object.
// Children is a map of child nodes in the dependency tree.
// TotalTime records the total time taken to execute this object.
// Phase indicates the execution phase of the object.
type DependencyNode struct {
	Name      string                     `json:"name" yaml:"name" doc:"The name of the object" example:"platformAdministratorsGroup" pattern:"^(.|\\n|\\r)*"`
	Children  map[string]*DependencyNode `json:"children,omitempty" yaml:"children,omitempty" doc:"The child nodes of the dependency tree" required:"false" `
	TotalTime time.Duration              `json:"totalTime" yaml:"totalTime" doc:"The total time taken to execute this object"`
	Phase     int                        `json:"phase,omitempty" yaml:"phase,omitempty" doc:"The phase in which the object was executed" minimum:"0" maximum:"99999999" required:"false" `
}

// RunnerResults holds the results of executing a set of objects, including the total execution time
// and the order in which each object was executed.
type RunnerResults struct {
	TotalTime      time.Duration     `json:"totalTime" yaml:"totalTime" doc:"Total time taken to execute all objects"`
	ExecutionOrder []ObjectExecution `json:"executionOrder" yaml:"executionOrder" doc:"The order in which objects were executed"`
}

// ObjectExecution represents the execution details of an object within a DAG.
// It contains metadata such as the object's name, type, execution position, total execution time,
// any error encountered, the phase of execution, whether it was executed, and the result value.
type ObjectExecution struct {
	Name        string         `json:"name" yaml:"name" doc:"The name of the object"`
	ObjectType  string         `json:"objectType" yaml:"objectType" doc:"The type of the object"`
	Position    int            `json:"position" yaml:"position" doc:"The position of the object in the execution order"`
	TotalTime   time.Duration  `json:"totalTime" yaml:"totalTime" doc:"Total time taken to execute the object"`
	Error       error          `json:"error,omitempty" yaml:"error,omitempty" doc:"Error encountered during object execution"`
	Phase       int            `json:"phase,omitempty" yaml:"phase,omitempty" doc:"The phase in which the object was executed"`
	WasExecuted bool           `json:"wasExecuted,omitempty" yaml:"wasExecuted,omitempty" doc:"Indicates if the object was executed"`
	Value       map[string]any `json:"value,omitempty" yaml:"value,omitempty" doc:"The value or result of the object execution" required:"false"`
}

// AnalysisItem represents an object analyzed during DAG execution.
// It contains metadata about the object's execution, including its name,
// position in the execution order, total execution time, any error encountered,
// the phase of execution, and flags indicating if it was time-consuming or
// executed in a particular state.
type AnalysisItem struct {
	Name                  string `json:"name" yaml:"name" doc:"The name of the object"`
	Position              int    `json:"position" yaml:"position" doc:"The position of the object in the execution order"`
	TotalTimeMilliSeconds int64  `json:"totalTimeMilliSeconds" yaml:"totalTimeMilliSeconds" doc:"Total time taken to execute the object in milliseconds"`
	Error                 error  `json:"error,omitempty" yaml:"error,omitempty" doc:"Error encountered during object execution"`
	Phase                 int    `json:"phase,omitempty" yaml:"phase,omitempty" doc:"The phase in which the object was executed"`
	TimeConsuming         bool   `json:"timeConsuming,omitempty" yaml:"timeConsuming,omitempty" doc:"Indicates if the object took a long time to execute (over 30ms)"`
	SourceExecution       string `json:"sourceExecution,omitempty" yaml:"sourceExecution,omitempty" doc:"Where the object was executed from (e.g., from state or fresh run)" required:"false"`
}

// Analysis represents the execution analysis of a set of objects,
// including total execution time, details for all objects, failed objects,
// and those that took a significant amount of time to execute.
type Analysis struct {
	TotalTimeMilliseconds int64          `json:"totalTimeMilliseconds" yaml:"totalTimeMilliseconds" doc:"Total time taken to execute all objects in milliseconds"`
	All                   []AnalysisItem `json:"all" yaml:"all" doc:"All objects with their execution details"`
	Failed                []AnalysisItem `json:"failed" yaml:"failed" doc:"objects that failed during execution"`
	TimeConsuming         []AnalysisItem `json:"timeConsuming" yaml:"timeConsuming" doc:"objects that took a long time to execute (over 10ms)"`
}

// ChildToParents represents a relationship between a child object and its parent objects.
// It contains the name of the child and a list of its parent names.
type ChildToParents struct {
	Child   string   `json:"child" yaml:"child" doc:"The child object"`
	Parents []string `json:"parents" yaml:"parents" doc:"The parent objects"`
}
