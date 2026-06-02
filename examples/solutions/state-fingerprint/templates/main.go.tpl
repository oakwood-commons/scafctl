{{ .header }}
// Package {{ .projectName }} provides the main application entry point.
package {{ .projectName }}

import (
	"fmt"
	"os"
)

func main() {
	fmt.Println("{{ .projectName }} starting...")
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	svc := NewService()
	return svc.Start()
}
