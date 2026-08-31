package identity

type PluginIdentity struct {
	name    string
	catalog string
}

func (p PluginIdentity) Name() string {
	return p.name
}

func (p PluginIdentity) Catalog() string {
	return p.catalog
}

type VersionedPluginIdentity struct {
	PluginIdentity
	Version string
}

type catalog interface {
	CatalogName() string
}
type artifactName interface {
	ArtifactName() string
}

type CatalogArtifact interface {
	catalog
	artifactName
}

func DependencyToPluginIdentity(dep CatalogArtifact) PluginIdentity {
	return PluginIdentity{
		name:    dep.ArtifactName(),
		catalog: dep.CatalogName(),
	}
}

func IdentityWithVersion(id PluginIdentity, version string) VersionedPluginIdentity {
	return VersionedPluginIdentity{
		PluginIdentity: id,
		Version:        version,
	}
}

func NewPluginIdentity(name, catalog string) PluginIdentity {
	return PluginIdentity{
		name:    name,
		catalog: catalog,
	}
}
