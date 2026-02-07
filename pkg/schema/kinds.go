package schema

import (
	"fmt"
	"strings"
	"sync"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/solution"
)

// KindDefinition holds metadata and type information for an explainable kind.
type KindDefinition struct {
	// Name is the kind name used in CLI (e.g., "provider", "solution")
	Name string

	// Aliases are alternative names (e.g., "providers", "prov")
	Aliases []string

	// Description is a brief explanation of the kind
	Description string

	// TypeInstance is a pointer to the type for reflection
	// Use: (*MyType)(nil)
	TypeInstance any

	// TypeInfo is the cached introspected type information
	TypeInfo *TypeInfo
}

// KindRegistry manages the collection of explainable types.
type KindRegistry struct {
	mu    sync.RWMutex
	kinds map[string]*KindDefinition
}

// globalKindRegistry is the default registry instance.
var globalKindRegistry = NewKindRegistry()

// NewKindRegistry creates a new kind registry.
func NewKindRegistry() *KindRegistry {
	return &KindRegistry{
		kinds: make(map[string]*KindDefinition),
	}
}

// Register adds a kind definition to the registry.
func (r *KindRegistry) Register(def *KindDefinition) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Introspect the type
	typeInfo, err := IntrospectType(def.TypeInstance)
	if err != nil {
		return fmt.Errorf("failed to introspect type for kind %q: %w", def.Name, err)
	}
	def.TypeInfo = typeInfo
	def.TypeInfo.Description = def.Description

	// Register by primary name
	r.kinds[strings.ToLower(def.Name)] = def

	// Register by aliases
	for _, alias := range def.Aliases {
		r.kinds[strings.ToLower(alias)] = def
	}

	return nil
}

// Get retrieves a kind definition by name or alias.
func (r *KindRegistry) Get(name string) (*KindDefinition, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	def, ok := r.kinds[strings.ToLower(name)]
	return def, ok
}

// List returns all unique kind definitions (excluding aliases).
func (r *KindRegistry) List() []*KindDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()

	seen := make(map[string]bool)
	var result []*KindDefinition

	for _, def := range r.kinds {
		if !seen[def.Name] {
			seen[def.Name] = true
			result = append(result, def)
		}
	}

	return result
}

// Names returns all registered kind names (primary names only).
func (r *KindRegistry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	seen := make(map[string]bool)
	var names []string

	for _, def := range r.kinds {
		if !seen[def.Name] {
			seen[def.Name] = true
			names = append(names, def.Name)
		}
	}

	return names
}

// GetGlobalRegistry returns the global kind registry.
func GetGlobalRegistry() *KindRegistry {
	return globalKindRegistry
}

// RegisterKind registers a kind in the global registry.
func RegisterKind(def *KindDefinition) error {
	return globalKindRegistry.Register(def)
}

// GetKind retrieves a kind from the global registry.
func GetKind(name string) (*KindDefinition, bool) {
	return globalKindRegistry.Get(name)
}

// ListKinds returns all kinds from the global registry.
func ListKinds() []*KindDefinition {
	return globalKindRegistry.List()
}

// registerBuiltinKinds registers all built-in explainable kinds.
// This is called automatically when the package is imported via globalKindRegistry initialization.
//
//nolint:gochecknoinits // This init is necessary to register built-in kinds
func init() {
	// Provider Descriptor
	_ = RegisterKind(&KindDefinition{
		Name:    "provider",
		Aliases: []string{"providers", "prov", "p"},
		Description: `Provider Descriptor defines a provider's identity, versioning, schemas, 
capabilities, and catalog metadata. Providers are stateless execution 
primitives that perform single, well-defined operations.`,
		TypeInstance: (*provider.Descriptor)(nil),
	})

	// Solution
	_ = RegisterKind(&KindDefinition{
		Name:    "solution",
		Aliases: []string{"solutions", "sol", "s"},
		Description: `Solution is a Kubernetes-style declarative unit of behavior in scafctl.
It follows the apiVersion/kind pattern and separates concerns into 
metadata, spec, and catalog sections. A solution combines resolvers 
(data resolution), templates (data to files), and actions (side effects).`,
		TypeInstance: (*solution.Solution)(nil),
	})

	// Action
	_ = RegisterKind(&KindDefinition{
		Name:    "action",
		Aliases: []string{"actions", "act", "a"},
		Description: `Action represents a single action definition within a workflow.
Actions perform side-effect operations using providers and can depend 
on other actions for sequencing and data flow.`,
		TypeInstance: (*action.Action)(nil),
	})

	// Workflow
	_ = RegisterKind(&KindDefinition{
		Name:    "workflow",
		Aliases: []string{"workflows", "wf", "w"},
		Description: `Workflow contains the action execution specification. It defines 
two sections: regular actions that execute based on dependencies, 
and finally actions that execute after all regular actions complete.`,
		TypeInstance: (*action.Workflow)(nil),
	})

	// Resolver
	_ = RegisterKind(&KindDefinition{
		Name:    "resolver",
		Aliases: []string{"resolvers", "res", "r"},
		Description: `Resolver represents a single resolver definition that performs 
data resolution through resolve, transform, and validate phases.
Resolvers are the primary mechanism for obtaining and processing 
configuration data.`,
		TypeInstance: (*resolver.Resolver)(nil),
	})

	// Spec (Solution Spec)
	_ = RegisterKind(&KindDefinition{
		Name:    "spec",
		Aliases: []string{"specs"},
		Description: `Spec defines the execution specification for a solution. It contains 
resolvers that perform data resolution, transformation, and validation, 
and optionally a workflow that defines actions to execute.`,
		TypeInstance: (*solution.Spec)(nil),
	})

	// Schema (JSON Schema for provider properties)
	_ = RegisterKind(&KindDefinition{
		Name:    "schema",
		Aliases: []string{"properties", "property", "prop"},
		Description: `Schema defines the JSON Schema for provider input and output properties.
It uses the standard JSON Schema specification to define types, validation rules, and documentation.`,
		TypeInstance: (*jsonschema.Schema)(nil),
	})

	// RetryConfig
	_ = RegisterKind(&KindDefinition{
		Name:    "retry",
		Aliases: []string{"retryconfig"},
		Description: `RetryConfig defines automatic retry behavior for failed actions.
It configures the number of attempts, backoff strategy, and delays.`,
		TypeInstance: (*action.RetryConfig)(nil),
	})
}
