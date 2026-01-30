# Solution Implementation Outline

## Data Model
- Struct: Solution at [pkg/solution/solution.go#L20-L182](pkg/solution/solution.go#L20-L182)
- Defaults/constants: `DefaultAPIVersion = "scafctl.io/v1"`, `SolutionKind = "Solution"` [pkg/solution/solution.go#L12-L18](pkg/solution/solution.go#L12-L18)
- Fields and purpose:
  - `APIVersion` (string): versioned schema identifier; default noted in design [pkg/solution/solution.go#L42-L48](pkg/solution/solution.go#L42-L48)
  - `Kind` (string): must be `Solution` [pkg/solution/solution.go#L46-L48](pkg/solution/solution.go#L46-L48)
  - `Metadata` (Metadata): immutable, catalog-indexed descriptive info (name/version/displayName/description/category/tags/maintainers/links/icon/banner) [pkg/solution/solution.go#L50-L94](pkg/solution/solution.go#L50-L94)
  - `Catalog` (Catalog, optional): publishing/distribution only; no execution impact (visibility/beta/disabled) [pkg/solution/solution.go#L54-L108](pkg/solution/solution.go#L54-L108)
  - `path` (string, internal): file path of loaded solution; not serialized [pkg/solution/solution.go#L58-L59](pkg/solution/solution.go#L58-L59)

## Metadata & Catalog Details
- Metadata validation is via struct tags (length/pattern/examples) for JSON/YAML [pkg/solution/solution.go#L64-L94](pkg/solution/solution.go#L64-L94)
- Catalog fields and constraints: visibility enum, beta flag, disabled flag; tagged for JSON/YAML with docs [pkg/solution/solution.go#L96-L108](pkg/solution/solution.go#L96-L108)

## Serialization / Deserialization
- JSON: `ToJSON`, `ToJSONPretty`, `FromJSON` [pkg/solution/solution.go#L126-L142](pkg/solution/solution.go#L126-L142)
- YAML: `ToYAML`, `FromYAML` [pkg/solution/solution.go#L144-L154](pkg/solution/solution.go#L144-L154)
- Dual-format load: `UnmarshalFromBytes` tries YAML, then JSON; rejects empty input; wraps both errors [pkg/solution/solution.go#L156-L171](pkg/solution/solution.go#L156-L171)

## Path Tracking
- Accessors: `GetPath`, `SetPath` for the internal path; excluded from JSON/YAML [pkg/solution/solution.go#L173-L182](pkg/solution/solution.go#L173-L182)

## Design Alignment Notes
- Matches design doc sections: top-level `apiVersion/kind/metadata/catalog/spec` with catalog non-execution semantics and semver default for apiVersion.
- Resolver/action specs themselves are modeled elsewhere; this struct focuses on the top-level envelope plus metadata/catalog.
- CEL and Go template typing are documented in design; the struct uses plain strings with doc tags (CEL typed handling occurs in downstream processing).

## Gaps / Considerations
- Runtime validation now exists at the envelope level via `ApplyDefaults` and `Validate`, which enforce apiVersion/kind, required metadata name/version, and catalog visibility enum; callers should still validate spec/resolvers/actions elsewhere.
- `path` is settable but not guarded; callers must ensure correctness.
- Catalog defaulting is applied for visibility (defaults to private); beta/disabled rely on zero values. Further spec-level defaults may still be needed.
