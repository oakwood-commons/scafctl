package catalog

import (
	"fmt"
	"slices"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/catalog"
)

// catalogKindSpec is the shared metadata for each artifact kind when registering
// subcommands under catalog operations.
type catalogKindSpec struct {
	Kind    catalog.ArtifactKind
	Aliases []string
}

func catalogKindSpecs() []catalogKindSpec {
	return []catalogKindSpec{
		{
			Kind:    catalog.ArtifactKindSolution,
			Aliases: []string{"solutions", "sol"},
		},
		{
			Kind:    catalog.ArtifactKindProvider,
			Aliases: []string{"providers"},
		},
		{
			Kind:    catalog.ArtifactKindAuthHandler,
			Aliases: []string{"auth-handlers"},
		},
	}
}

func catalogKindName(kind catalog.ArtifactKind) string {
	switch kind {
	case catalog.ArtifactKindSolution:
		return "solution"
	case catalog.ArtifactKindProvider:
		return "provider"
	case catalog.ArtifactKindAuthHandler:
		return "auth-handler"
	default:
		return strings.ToLower(string(kind))
	}
}

// catalogKindNames returns the canonical name of every registered kind, in
// registration order.
func catalogKindNames() []string {
	specs := catalogKindSpecs()
	names := make([]string, 0, len(specs))
	for _, spec := range specs {
		names = append(names, catalogKindName(spec.Kind))
	}
	return names
}

// isCatalogKind reports whether name is a registered kind subcommand name or
// one of its aliases.
func isCatalogKind(name string) bool {
	for _, spec := range catalogKindSpecs() {
		if name == catalogKindName(spec.Kind) || slices.Contains(spec.Aliases, name) {
			return true
		}
	}
	return false
}

func catalogKindUse(kind catalog.ArtifactKind, action string) string {
	name := catalogKindName(kind)
	switch action {
	case "delete":
		return fmt.Sprintf("%s <name@version>", name)
	case "pull", "push":
		return fmt.Sprintf("%s <name[@version]>", name)
	default:
		return fmt.Sprintf("%s <name>", name)
	}
}

func catalogKindShort(kind catalog.ArtifactKind, action string) string {
	switch kind {
	case catalog.ArtifactKindSolution:
		switch action {
		case "delete":
			return "Delete a solution from the catalog"
		case "pull":
			return "Pull a solution from a remote registry"
		case "push":
			return "Push a solution to a remote registry"
		}
	case catalog.ArtifactKindProvider:
		switch action {
		case "delete":
			return "Delete a provider plugin from the catalog"
		case "pull":
			return "Pull a provider plugin from a remote registry"
		case "push":
			return "Push a provider plugin to a remote registry"
		}
	case catalog.ArtifactKindAuthHandler:
		switch action {
		case "delete":
			return "Delete an auth handler plugin from the catalog"
		case "pull":
			return "Pull an auth handler plugin from a remote registry"
		case "push":
			return "Push an auth handler plugin to a remote registry"
		}
	}

	return "Manage a catalog artifact"
}
