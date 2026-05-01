// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package solution

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/oakwood-commons/scafctl/pkg/sourcepos"
	"github.com/oakwood-commons/scafctl/pkg/spec"
	"gopkg.in/yaml.v3"
)

const (
	// DefaultAPIVersion is the default API version for Solution resources
	DefaultAPIVersion = "scafctl.io/v1"

	// SolutionKind is the Kind identifier for Solution resources
	SolutionKind = "Solution"
)

// defaultVersion is the placeholder version applied when metadata.version is
// omitted from the solution file. The real version is assigned at build time
// via `scafctl build solution`.
var defaultVersion = semver.MustParse("0.0.0-dev")

// DefaultVersion returns a copy of the default placeholder version.
func DefaultVersion() *semver.Version {
	v := *defaultVersion
	return &v
}

// Solution represents a Kubernetes-style declarative unit of behavior in scafctl.
// It follows the apiVersion/kind pattern and separates concerns into metadata, spec, and catalog sections.
//
// A solution combines:
//   - resolvers (data resolution)
//   - templates (data to files or artifacts)
//   - actions (explicit side effects)
//
// The solution is a data model that scafctl executes deterministically, not a script or pipeline.
//
// Example:
//
//	apiVersion: scafctl.io/v1
//	kind: Solution
//	metadata:
//	  name: gcp-basic
//	  version: 1.0.1
//	  displayName: Basic GCP Solution
//	catalog:
//	  visibility: public
//	  beta: false
type Solution struct {
	// APIVersion defines the versioned schema of this representation of a Solution.
	// Default: "scafctl.io/v1"
	APIVersion string `json:"apiVersion" yaml:"apiVersion" doc:"The API version of the solution" example:"scafctl.io/v1" pattern:"^[a-z0-9]+\\.io/v[0-9]+(alpha[0-9]+|beta[0-9]+)?$"`

	// Kind is a string value representing the REST resource this object represents.
	// Must be "Solution".
	Kind string `json:"kind" yaml:"kind" doc:"The kind of resource" example:"Solution" pattern:"^Solution$"`

	// Metadata describes the solution as a product. It is immutable per version
	// and is indexed in the catalog. Metadata does not affect execution.
	Metadata Metadata `json:"metadata" yaml:"metadata" doc:"Metadata about the solution"`

	// Catalog controls distribution and visibility, not execution.
	// Published artifacts are JSON-only, and solutions are version-addressable (e.g., gcp-basic@1.0.1).
	Catalog Catalog `json:"catalog,omitempty" yaml:"catalog,omitempty" doc:"Catalog metadata for distribution" required:"false"`

	// Compose lists relative paths to partial YAML files merged into this solution at build/load time.
	// Each path must be relative to the directory containing this solution YAML file.
	Compose []string `json:"compose,omitempty" yaml:"compose,omitempty" doc:"Relative paths to partial YAML files merged into this solution" maxItems:"100"`

	// Bundle defines files and plugins to include when building a solution into a catalog artifact.
	// This section is build-time metadata only and does not affect execution.
	Bundle Bundle `json:"bundle,omitempty" yaml:"bundle,omitempty" doc:"Build-time bundling configuration"`

	// Spec defines the execution specification containing resolvers, templates, and actions.
	// This is where the actual work of the solution is defined.
	Spec Spec `json:"spec,omitempty" yaml:"spec,omitempty" doc:"Execution specification"`

	// path is an internal field for the file path where the solution was loaded from
	path string `json:"-" yaml:"-"`

	// sourceMap maps logical YAML paths to source positions (line/column).
	// It is populated during FromYAML when the solution is loaded from YAML bytes.
	sourceMap *sourcepos.SourceMap `json:"-" yaml:"-"`

	// rawContent stores the original bytes used to parse this solution.
	// It preserves the original formatting for round-trip fidelity.
	rawContent []byte `json:"-" yaml:"-"`
}

// Metadata contains the descriptive information about a solution.
// Metadata is immutable per version and does not affect execution.
type Metadata struct {
	// Name is the unique identifier for the solution (e.g., "gcp-basic")
	Name string `json:"name" yaml:"name" doc:"The unique name of the solution" minLength:"3" maxLength:"60" example:"gcp-basic" pattern:"^[a-z0-9]([a-z0-9-]+[a-z0-9])?$"`

	// Version is the semantic version of the solution.
	// Optional for local development; required for catalog publishing (build/push).
	Version *semver.Version `json:"version,omitempty" yaml:"version,omitempty" doc:"The version of the solution" example:"1.0.0" pattern:"^(0|[1-9]\\d*)\\.(0|[1-9]\\d*)\\.(0|[1-9]\\d*)(?:-((?:0|[1-9]\\d*|\\d*[a-zA-Z-][0-9a-zA-Z-]*)(?:\\.(?:0|[1-9]\\d*|\\d*[a-zA-Z-][0-9a-zA-Z-]*))*))?(?:\\+([0-9a-zA-Z-]+(?:\\.[0-9a-zA-Z-]+)*))?$" required:"false"`

	// DisplayName is the human-readable name of the solution
	DisplayName string `json:"displayName,omitempty" yaml:"displayName,omitempty" doc:"The display name of the solution" minLength:"3" maxLength:"80" example:"Basic GCP Solution" pattern:"^(.)+$" required:"false"`

	// Description provides details about the solution's purpose
	Description string `json:"description,omitempty" yaml:"description,omitempty" doc:"The description of the solution" minLength:"3" maxLength:"5000" example:"This solution scaffolds terraform code to create a simple GCP bucket" pattern:"^(.|\\n|\\r)*" required:"false"`

	// Category classifies the solution (e.g., "application", "infrastructure")
	Category string `json:"category,omitempty" yaml:"category,omitempty" doc:"The category of the solution" minLength:"1" maxLength:"30" example:"application" pattern:"^[a-z0-9]([a-z0-9-]*[a-z0-9])?$" required:"false"`

	// Tags are searchable keywords associated with the solution
	Tags []string `json:"tags,omitempty" yaml:"tags,omitempty" maxItems:"100" doc:"A list of tags for the solution" required:"false"`

	// Maintainers lists the people or teams responsible for the solution
	Maintainers []Contact `json:"maintainers,omitempty" yaml:"maintainers,omitempty" doc:"The maintainers of the solution" minItems:"1" maxItems:"10" required:"false"`

	// Links provides references to related documentation or resources
	Links []Link `json:"links,omitempty" yaml:"links,omitempty" doc:"Links to content related to the solution" maxItems:"10" required:"false"`

	// Icon is a URL or path to an icon image for the solution
	Icon string `json:"icon,omitempty" yaml:"icon,omitempty" doc:"URL or path to the solution icon" maxLength:"500" pattern:"^((http|https|file):\\/\\/.+|[a-zA-Z0-9_-]+\\.(png|jpg|jpeg|svg|gif))$" required:"false"`

	// Banner is a URL or path to a banner image for the solution
	Banner string `json:"banner,omitempty" yaml:"banner,omitempty" doc:"URL or path to the solution banner" maxLength:"500" pattern:"^((http|https|file):\\/\\/.+|[a-zA-Z0-9_-]+\\.(png|jpg|jpeg|svg|gif))$" required:"false"`
}

// Catalog controls distribution and visibility of the solution.
// Catalog metadata does not affect execution behavior.
type Catalog struct {
	// Visibility controls who can discover and use the solution.
	// Valid values: "public", "private", "internal"
	Visibility string `json:"visibility,omitempty" yaml:"visibility,omitempty" doc:"The visibility of the solution in the catalog" example:"public" pattern:"^(public|private|internal)$" required:"false"`

	// Beta indicates if the solution is in beta/preview status
	Beta bool `json:"beta,omitempty" yaml:"beta,omitempty" doc:"Indicates if the solution is in beta" required:"false"`

	// Disabled marks the solution as unavailable (but keeps it in the catalog)
	Disabled bool `json:"disabled,omitempty" yaml:"disabled,omitempty" doc:"Indicates if the solution is disabled" required:"false"`
}

// Contact represents the maintainer's contact information, including their name and email address.
// The Name field must be between 3 and 60 characters and can include letters, spaces, and certain punctuation.
// The Email field must be a valid email address between 5 and 100 characters.
type Contact struct {
	Name  string `json:"name,omitempty" yaml:"name,omitempty" doc:"The name of the maintainer" minLength:"3" maxLength:"60" example:"John Doe" pattern:"^[\\w \\-.'(),&]+$"`
	Email string `json:"email,omitempty" yaml:"email,omitempty" doc:"The email of the maintainer" minLength:"5" maxLength:"100" example:"john.doe@example.com" pattern:"[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,3}"`
}

// Bundle defines files and plugins to include when building a solution into a catalog artifact.
// This section is build-time metadata only and does not affect execution.
type Bundle struct {
	// Include is a list of glob patterns or explicit file paths to bundle.
	// Paths are relative to the directory containing the solution YAML file.
	Include []string `json:"include,omitempty" yaml:"include,omitempty" doc:"Glob patterns or file paths to include in the bundle" maxItems:"1000"`

	// Plugins declares external plugins required by this solution.
	Plugins []PluginDependency `json:"plugins,omitempty" yaml:"plugins,omitempty" doc:"External plugins required by this solution" maxItems:"50"`
}

// IsEmpty returns true if the bundle has no includes and no plugins.
func (b Bundle) IsEmpty() bool {
	return len(b.Include) == 0 && len(b.Plugins) == 0
}

// PluginDependency declares an external plugin required by a solution.
type PluginDependency struct {
	// Name is the plugin's catalog reference (e.g., "aws-provider").
	Name string `json:"name" yaml:"name" doc:"Plugin catalog reference" example:"aws-provider" maxLength:"100" pattern:"^[a-z0-9]([a-z0-9-]+[a-z0-9])?$" patternDescription:"lowercase alphanumeric with hyphens"`

	// Kind is the plugin type.
	Kind PluginKind `json:"kind" yaml:"kind" doc:"Plugin type" example:"provider"`

	// Version is a semver constraint (e.g., "^1.5.0", ">=2.0.0", "3.1.2") or "latest".
	Version string `json:"version" yaml:"version" doc:"Semver version constraint or 'latest'" example:"^1.5.0" maxLength:"50" pattern:"^([~^>=<]*[0-9]|latest$)" patternDescription:"semver constraint or 'latest'"`

	// Defaults provides default values for plugin inputs.
	// These are shallow-merged beneath inline provider inputs (inline always wins).
	Defaults map[string]*spec.ValueRef `json:"defaults,omitempty" yaml:"defaults,omitempty" doc:"Default input values for this plugin (supports ValueRef)"`
}

// PluginKind is the type of plugin (provider, auth-handler).
type PluginKind string

const (
	// PluginKindProvider represents a provider plugin.
	PluginKindProvider PluginKind = "provider"

	// PluginKindAuthHandler represents an auth handler plugin.
	PluginKindAuthHandler PluginKind = "auth-handler"
)

// IsValid returns true if the plugin kind is a recognized value.
func (k PluginKind) IsValid() bool {
	switch k {
	case PluginKindProvider, PluginKindAuthHandler:
		return true
	default:
		return false
	}
}

// Link represents a named hyperlink with validation constraints on its fields.
// Name must be 3-30 characters and match the allowed pattern.
// URL must be a valid URI, 12-500 characters, and start with http or https.
type Link struct {
	Name string `json:"name,omitempty" yaml:"name,omitempty" doc:"The name of the link" minLength:"3" maxLength:"30" example:"Documentation" pattern:"^(\\w|\\-|\\_|\\ )+$"`
	URL  string `json:"url,omitempty" yaml:"url,omitempty" doc:"The URL of the link" minLength:"12" maxLength:"500" example:"https://google.com" format:"uri" pattern:"^(http|https):\\/\\/.+"`
}

// ToJSON serializes the Solution struct into JSON format.
// It returns the resulting JSON as a byte slice and any error encountered during marshaling.
func (s *Solution) ToJSON() ([]byte, error) {
	return json.Marshal(s)
}

// ToJSONPretty serializes the Solution struct into a pretty-printed JSON format.
// It returns the resulting JSON as a byte slice and any error encountered during marshaling.
func (s *Solution) ToJSONPretty() ([]byte, error) {
	return json.MarshalIndent(s, "", "  ")
}

// FromJSON unmarshals the provided JSON data into the Solution struct.
// It returns an error if the unmarshalling fails.
func (s *Solution) FromJSON(data []byte) error {
	if err := json.Unmarshal(data, s); err != nil {
		return err
	}
	s.rawContent = append([]byte(nil), data...)
	return nil
}

// ToYAML serializes the Solution struct into YAML format.
// It returns the resulting YAML as a byte slice, or an error if serialization fails.
func (s *Solution) ToYAML() ([]byte, error) {
	return yaml.Marshal(s)
}

// FromYAML unmarshals the provided YAML data into the Solution struct.
// It also builds a SourceMap that maps logical YAML paths to source positions.
// It returns an error if the unmarshalling process fails.
func (s *Solution) FromYAML(data []byte) error {
	if err := yaml.Unmarshal(data, s); err != nil {
		return err
	}

	s.rawContent = append([]byte(nil), data...)

	// Build source map for line/column tracking.
	// This is a best-effort operation; parsing errors are non-fatal since we
	// already successfully unmarshalled the data above.
	sm, err := sourcepos.BuildSourceMap(data, s.path)
	if err == nil {
		s.sourceMap = sm
	}

	return nil
}

// UnmarshalFromBytes attempts to unmarshal the provided byte slice into the Solution struct.
// It first tries to unmarshal using YAML format. If that fails and the content looks like JSON,
// it attempts to unmarshal using JSON format. Returns only the relevant error for clarity.
func (s *Solution) UnmarshalFromBytes(bytes []byte) error {
	if len(bytes) == 0 {
		return errors.New("no data provided to unmarshal solution")
	}
	yamlErr := s.FromYAML(bytes)
	if yamlErr == nil {
		return nil
	}
	// Only try JSON if the content looks like JSON (starts with { or [)
	trimmed := strings.TrimSpace(string(bytes))
	if len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[') {
		if jsonErr := s.FromJSON(bytes); jsonErr == nil {
			return nil
		}
	}
	return yamlErr
}

// LoadFromBytes unmarshals the provided bytes, applies defaults, and validates the solution.
// It is a convenience helper to ensure envelope normalization happens consistently.
func (s *Solution) LoadFromBytes(bytes []byte) error {
	if s == nil {
		return errors.New("solution is nil")
	}

	if err := s.UnmarshalFromBytes(bytes); err != nil {
		return err
	}

	s.ApplyDefaults()
	return s.Validate()
}

// GetPath returns the file system path associated with the Solution.
func (s *Solution) GetPath() string {
	return s.path
}

// RawContent returns a copy of the original bytes used to parse this solution.
// It preserves the original formatting for round-trip fidelity.
// Returns nil if the solution was not loaded from bytes.
func (s *Solution) RawContent() []byte {
	if s.rawContent == nil {
		return nil
	}
	out := make([]byte, len(s.rawContent))
	copy(out, s.rawContent)
	return out
}

// SetPath sets the path for the Solution.
// It updates the internal path field with the provided value.
func (s *Solution) SetPath(path string) {
	s.path = path
}

// SourceMap returns the source map for this solution, if available.
// The source map is populated during FromYAML and maps logical YAML paths
// (e.g., "spec.resolvers.appName") to source positions (line, column, file).
// Returns nil if the solution was not loaded from YAML or if source map building failed.
func (s *Solution) SourceMap() *sourcepos.SourceMap {
	if s == nil {
		return nil
	}
	return s.sourceMap
}

// SetSourceMap sets the source map for this solution.
// This is useful when composing solutions from multiple files.
func (s *Solution) SetSourceMap(sm *sourcepos.SourceMap) {
	if s == nil {
		return
	}
	s.sourceMap = sm
}

// ApplyDefaults populates default values for optional top-level fields.
// It is safe to call on a nil receiver.
func (s *Solution) ApplyDefaults() {
	if s == nil {
		return
	}
	if s.APIVersion == "" {
		s.APIVersion = DefaultAPIVersion
	}
	if s.Kind == "" {
		s.Kind = SolutionKind
	}
	if s.Metadata.Version == nil {
		v := *defaultVersion
		s.Metadata.Version = &v
	}
	if s.Catalog.Visibility == "" {
		s.Catalog.Visibility = "private"
	}
}

// Validate performs lightweight runtime validation on the Solution envelope.
// It enforces apiVersion/kind, required metadata, and catalog enums.
func (s *Solution) Validate() error {
	if s == nil {
		return errors.New("solution is nil")
	}

	var problems []string

	if s.APIVersion != DefaultAPIVersion {
		problems = append(problems, fmt.Sprintf("apiVersion must be %s", DefaultAPIVersion))
	}
	if s.Kind != SolutionKind {
		problems = append(problems, fmt.Sprintf("kind must be %s", SolutionKind))
	}
	if s.Metadata.Name == "" {
		problems = append(problems, "metadata.name is required")
	}

	if vis := s.Catalog.Visibility; vis != "" {
		switch vis {
		case "public", "private", "internal":
		default:
			problems = append(problems, "catalog.visibility must be public, private, or internal when set")
		}
	}

	if len(problems) > 0 {
		return fmt.Errorf("solution validation failed: %s", strings.Join(problems, "; "))
	}

	// Validate spec section (resolvers, templates, actions)
	if err := s.ValidateSpec(); err != nil {
		return err
	}

	return nil
}
