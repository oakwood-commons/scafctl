package spec

// ForEachClause defines iteration over an array.
// When present, the associated operation is executed once per array element
// and results are collected into an output array preserving order.
type ForEachClause struct {
	// Item is the variable name alias for the current element.
	// Creates both __item (always) and this custom name.
	// Optional - if not specified, only __item is available.
	Item string `json:"item,omitempty" yaml:"item,omitempty" doc:"Variable name alias for current array element" maxLength:"50" pattern:"^[a-zA-Z_][a-zA-Z0-9_]*$" patternDescription:"Must be a valid identifier" example:"region"`

	// Index is the variable name alias for the current 0-based index.
	// Creates both __index (always) and this custom name.
	// Optional - if not specified, only __index is available.
	Index string `json:"index,omitempty" yaml:"index,omitempty" doc:"Variable name alias for current index" maxLength:"50" pattern:"^[a-zA-Z_][a-zA-Z0-9_]*$" patternDescription:"Must be a valid identifier" example:"i"`

	// In specifies the array to iterate over.
	// Optional - defaults to __self (current transform value) for resolvers.
	In *ValueRef `json:"in,omitempty" yaml:"in,omitempty" doc:"Array to iterate over (default: __self for resolvers)"`

	// Concurrency limits parallel execution.
	// 0 (default) means unlimited parallelism.
	Concurrency int `json:"concurrency,omitempty" yaml:"concurrency,omitempty" doc:"Maximum parallel iterations (0=unlimited)" minimum:"0" example:"5"`

	// OnError defines behavior when an iteration fails.
	// This field is only used by actions; resolvers ignore it.
	OnError OnErrorBehavior `json:"onError,omitempty" yaml:"onError,omitempty" doc:"Error handling behavior (actions only)" example:"fail" default:"fail"`
}
