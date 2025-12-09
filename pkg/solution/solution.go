package solution

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Masterminds/semver/v3"
	"gopkg.in/yaml.v3"
)

// Solution represents the metadata and configuration for a solution scaffold.
// It includes versioning, identification, categorization, maintainers, and related links.
// Fields are annotated for JSON/YAML serialization and validation constraints.
// - SchemaVersion: The schema version of the solution.
// - Name: The unique name of the solution.
// - DisplayName: The human-readable name of the solution.
// - Description: A description of the solution's purpose.
// - Category: The category under which the solution falls.
// - Version: The version of the solution.
// - ScafctlVersion: Minimum required version of 'scafctl' for compatibility.
// - Tags: A list of tags describing the solution.
// - Labels: Key-value labels for the solution.
// - Maintainers: Contacts for maintainers of the solution.
// - Links: Related content links.
// - path: Internal field for the file path where the solution was loaded from.
type Solution struct {
	SchemaVersion  *semver.Version   `json:"schemaVersion,omitempty" yaml:"schemaVersion,omitempty" doc:"The schema version of the solution" pattern:"^(0|[1-9]\\d*)\\.(0|[1-9]\\d*)\\.(0|[1-9]\\d*)(?:-((?:0|[1-9]\\d*|\\d*[a-zA-Z-][0-9a-zA-Z-]*)(?:\\.(?:0|[1-9]\\d*|\\d*[a-zA-Z-][0-9a-zA-Z-]*))*))?(?:\\+([0-9a-zA-Z-]+(?:\\.[0-9a-zA-Z-]+)*))?$"`
	Name           string            `json:"name,omitempty" yaml:"name,omitempty" doc:"The name of the solution" minLength:"3" maxLength:"60" example:"gcp-basic" pattern:"^[^\\s]+(\\w|\\-|\\_)+$"`
	DisplayName    string            `json:"displayName,omitempty" yaml:"displayName,omitempty" doc:"The name of the display name of the solution" minLength:"3" maxLength:"80" example:"Basic GCP Solution" pattern:"^(.)+$"`
	Description    string            `json:"description,omitempty" yaml:"description,omitempty" doc:"The description of the solution" minLength:"3" maxLength:"5000" example:"This solution scaffolds terraform code to create a simple GCP bucket" pattern:"^(.|\\n|\\r)*" required:"false"`
	Category       string            `json:"category,omitempty" yaml:"category,omitempty" doc:"The category of the solution" minLength:"3" maxLength:"30" example:"application" pattern:"^(\\w|\\ |\\d|-)+$"`
	Version        *semver.Version   `json:"version,omitempty" yaml:"version,omitempty" doc:"The version of the solution" minLength:"3" maxLength:"30" example:"1.0.0" pattern:"^(0|[1-9]\\d*)\\.(0|[1-9]\\d*)\\.(0|[1-9]\\d*)(?:-((?:0|[1-9]\\d*|\\d*[a-zA-Z-][0-9a-zA-Z-]*)(?:\\.(?:0|[1-9]\\d*|\\d*[a-zA-Z-][0-9a-zA-Z-]*))*))?(?:\\+([0-9a-zA-Z-]+(?:\\.[0-9a-zA-Z-]+)*))?$"`
	ScafctlVersion string            `json:"scafctlVersion,omitempty" yaml:"scafctlVersion,omitempty" doc:"Specifies the minimum required version of 'scafctl' for this solution. You can provide an exact version (e.g., '1.0.0') or use version constraints (e.g., '>= 1.0, < 1.4'). For more information on version constraints, refer to the [Masterminds semver documentation](https://github.com/Masterminds/semver?tab=readme-ov-file#basic-comparisons)" minLength:"3" maxLength:"200" example:"1.0.0" pattern:"^(\\w|\\-|\\_|\\ |\\.|\\<|\\>|=|,|\\~|\\!)+$" required:"false"`
	Tags           []string          `json:"tags,omitempty" yaml:"tags,omitempty" maxItems:"100" doc:"A list of tags for the solution" required:"false"`
	Labels         map[string]string `json:"labels,omitempty" yaml:"labels,omitempty" doc:"The labels of the solution" maxItems:"12" required:"false"`
	Maintainers    []Contact         `json:"maintainers,omitempty" yaml:"maintainers,omitempty" doc:"The maintainers of the solution" minItems:"1" maxItems:"10" required:"false"`
	Links          []Link            `json:"links,omitempty" yaml:"links,omitempty" doc:"Links to content related to the solution" maxItems:"10" required:"false"`
	path           string            `json:"-" yaml:"-" doc:"The of where the solution file was loaded from. This is internally set" required:"false"`
}

// Contact represents the maintainer's contact information, including their name and email address.
// The Name field must be between 3 and 60 characters and can include letters, spaces, and certain punctuation.
// The Email field must be a valid email address between 5 and 100 characters.
type Contact struct {
	Name  string `json:"name,omitempty" yaml:"name,omitempty" doc:"The name of the maintainer" minLength:"3" maxLength:"60" example:"John Doe" pattern:"^[\\w \\-.'(),&]+$"`
	Email string `json:"email,omitempty" yaml:"email,omitempty" doc:"The email of the maintainer" minLength:"5" maxLength:"100" example:"john.doe@example.com" pattern:"[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,3}"`
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
	return json.Unmarshal(data, s)
}

// ToYAML serializes the Solution struct into YAML format.
// It returns the resulting YAML as a byte slice, or an error if serialization fails.
func (s *Solution) ToYAML() ([]byte, error) {
	return yaml.Marshal(s)
}

// FromYAML unmarshals the provided YAML data into the Solution struct.
// It returns an error if the unmarshalling process fails.
func (s *Solution) FromYAML(data []byte) error {
	return yaml.Unmarshal(data, s)
}

// UnmarshalFromBytes attempts to unmarshal the provided byte slice into the Solution struct.
// It first tries to unmarshal using YAML format. If that fails, it attempts to unmarshal using JSON format.
// Returns an error if unmarshalling fails for both formats.
func (s *Solution) UnmarshalFromBytes(bytes []byte) error {
	if len(bytes) == 0 {
		return errors.New("no data provided to unmarshal solution")
	}
	yamlErr := s.FromYAML(bytes)
	if yamlErr != nil {
		err := s.FromJSON(bytes)
		if err != nil {
			return fmt.Errorf("failed to unmarshal solution using yaml and json bytes: YAML error: %w, JSON error: %w", yamlErr, err)
		}
	}
	return nil
}

// GetPath returns the file system path associated with the Solution.
func (s *Solution) GetPath() string {
	return s.path
}

// SetPath sets the path for the Solution.
// It updates the internal path field with the provided value.
func (s *Solution) SetPath(path string) {
	s.path = path
}
