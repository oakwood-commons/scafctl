package solution

import (
	"encoding/json"

	"github.com/Masterminds/semver/v3"
	"gopkg.in/yaml.v3"
)

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
}

type Contact struct {
	Name  string `json:"name,omitempty" yaml:"name,omitempty" doc:"The name of the maintainer" minLength:"3" maxLength:"60" example:"John Doe" pattern:"^[\\w \\-.'(),&]+$"`
	Email string `json:"email,omitempty" yaml:"email,omitempty" doc:"The email of the maintainer" minLength:"5" maxLength:"100" example:"john.doe@example.com" pattern:"[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,3}"`
}

type Link struct {
	Name string `json:"name,omitempty" yaml:"name,omitempty" doc:"The name of the link" minLength:"3" maxLength:"30" example:"Documentation" pattern:"^(\\w|\\-|\\_|\\ )+$"`
	URL  string `json:"url,omitempty" yaml:"url,omitempty" doc:"The URL of the link" minLength:"12" maxLength:"500" example:"https://google.com" format:"uri" pattern:"^(http|https):\\/\\/.+"`
}

func (s *Solution) ToJSON() ([]byte, error) {
	return json.MarshalIndent(s, "", "  ")
}

func (s *Solution) FromJSON(data []byte) error {
	return json.Unmarshal(data, s)
}

func (s *Solution) ToYAML() ([]byte, error) {
	return yaml.Marshal(s)
}

func (s *Solution) FromYAML(data []byte) error {
	return yaml.Unmarshal(data, s)
}
