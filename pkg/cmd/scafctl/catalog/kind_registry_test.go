package catalog

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/stretchr/testify/require"
)

func TestCatalogKindSpecs(t *testing.T) {
	specs := catalogKindSpecs()
	require.Len(t, specs, 3)

	seen := map[catalog.ArtifactKind]bool{}
	for _, spec := range specs {
		seen[spec.Kind] = true
	}

	require.True(t, seen[catalog.ArtifactKindSolution])
	require.True(t, seen[catalog.ArtifactKindProvider])
	require.True(t, seen[catalog.ArtifactKindAuthHandler])
}

func TestCatalogKindUse(t *testing.T) {
	require.Equal(t, "solution <name@version>", catalogKindUse(catalog.ArtifactKindSolution, "delete"))
	require.Equal(t, "provider <name[@version]>", catalogKindUse(catalog.ArtifactKindProvider, "pull"))
	require.Equal(t, "auth-handler <name[@version]>", catalogKindUse(catalog.ArtifactKindAuthHandler, "push"))
}
