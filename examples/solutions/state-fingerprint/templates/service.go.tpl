{{ .header }}
package {{ .projectName }}

import "fmt"

// Service handles the application lifecycle.
type Service struct {
	name   string
	author string
}

// NewService creates a new Service instance.
func NewService() *Service {
	return &Service{
		name:   "{{ .projectName }}",
		author: "{{ .author }}",
	}
}

// Start begins the service.
func (s *Service) Start() error {
	fmt.Printf("Service %s (by %s) is running\n", s.name, s.author)
	return nil
}

// Stop gracefully shuts down the service.
func (s *Service) Stop() error {
	fmt.Printf("Service %s shutting down\n", s.name)
	return nil
}
