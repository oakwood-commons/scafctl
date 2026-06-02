{{ .header }}
package {{ .projectName }}

import (
	"fmt"
	"time"
)

// Model represents the core domain entity.
type Model struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// NewModel creates a new Model with the given name.
func NewModel(name string) *Model {
	now := time.Now()
	return &Model{
		Name:      name,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// Validate checks that the model is in a valid state.
func (m *Model) Validate() error {
	if m.Name == "" {
		return fmt.Errorf("model name must not be empty")
	}
	return nil
}
