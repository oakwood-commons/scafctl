package executionregistry

import (
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/solution"
)

type PluginArtifact interface {
	ArtifactName() string
	VersionConstraint() string
	CatalogName() string
}

var _ PluginArtifact = solution.PluginDependency{}

// ExecutionRegistry is a per-execution provider registry that overlays resolved
// external plugin references on top of a shared [provider.CompositeRegistry].
//
// The resolution map maps a raw provider name (as written in the solution YAML)
// to a [PluginArtifact] — the resolved external dependency carrying the
// artifact name, catalog, and version constraint needed to look up the concrete
// provider in the composite registry's external tier.
//
// On a Get or Has call:
//   - If the name is in the resolution map, it is an external provider. The
//     artifact's Name, Catalog, and Version are used as lookup keys into the
//     composite registry's external tier.
//   - If the name is not in the resolution map, it falls through to the
//     composite registry's builtin tier.
type ExecutionRegistry[T PluginArtifact] struct {
	composite  *provider.CompositeRegistry // shared registry with built-in and external providers
	resolution map[string]T                // raw ref -> external resolved dep
}

func NewExecutionRegistry[T PluginArtifact](shared *provider.CompositeRegistry, resolution map[string]T) *ExecutionRegistry[T] {
	return &ExecutionRegistry[T]{
		composite:  shared,
		resolution: resolution,
	}
}

func (sr *ExecutionRegistry[T]) Get(name string) (provider.Provider, bool) {
	if dep, ok := sr.resolution[name]; ok {
		return sr.getExternal(dep)
	}
	return sr.composite.GetBase(name)
}

func (sr *ExecutionRegistry[T]) Has(name string) bool {
	if dep, ok := sr.resolution[name]; ok {
		return sr.hasExternal(dep)
	}
	return sr.composite.HasBase(name)
}

// DescriptorLookup returns a function that looks up provider descriptors by
// name across both tiers. Built-ins take precedence; if no built-in matches,
// the latest external provider registered under the name is consulted. The
// returned function returns nil when neither tier has the provider. This is
// used for dependency extraction during resolver phase building.
func (sr *ExecutionRegistry[T]) DescriptorLookup() provider.DescriptorLookup {
	return func(name string) *provider.Descriptor {
		if dep, ok := sr.resolution[name]; ok {
			if p, ok := sr.getExternal(dep); ok {
				return p.Descriptor()
			}
		}
		if p, ok := sr.composite.GetBase(name); ok {
			return p.Descriptor()
		}
		return nil
	}
}

func (sr *ExecutionRegistry[T]) List() []provider.Provider {
	builtins := sr.composite.ListBase()
	providers := make([]provider.Provider, 0, len(builtins)+len(sr.resolution))
	for _, name := range builtins {
		if p, ok := sr.composite.GetBase(name); ok {
			providers = append(providers, p)
		}
	}
	for _, dep := range sr.resolution {
		if p, ok := sr.getExternal(dep); ok {
			providers = append(providers, p)
		}
	}
	return providers
}

func (sr *ExecutionRegistry[T]) getExternal(dep T) (provider.Provider, bool) {
	return sr.composite.GetExternal(dep.ArtifactName(),
		provider.WithCatalogName(dep.CatalogName()),
		provider.WithVersionOrConstraint(dep.VersionConstraint()))
}

func (sr *ExecutionRegistry[T]) hasExternal(dep T) bool {
	return sr.composite.HasExternal(dep.ArtifactName(),
		provider.WithCatalogName(dep.CatalogName()),
		provider.WithVersionOrConstraint(dep.VersionConstraint()))
}
