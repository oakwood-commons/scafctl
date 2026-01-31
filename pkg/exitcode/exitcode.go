// Package exitcode provides centralized exit codes for CLI commands.
// All commands should import this package to ensure consistent exit codes.
package exitcode

// Standard exit codes for CLI commands.
// These follow common Unix conventions where possible.
const (
	// Success indicates successful execution.
	Success = 0

	// GeneralError indicates an unspecified error occurred.
	GeneralError = 1

	// ValidationFailed indicates input validation failed.
	ValidationFailed = 2

	// InvalidInput indicates invalid solution structure (e.g., circular dependency).
	InvalidInput = 3

	// FileNotFound indicates a file was not found or could not be parsed.
	FileNotFound = 4

	// RenderFailed indicates rendering/transformation failed.
	RenderFailed = 5

	// ActionFailed indicates action/workflow execution failed.
	ActionFailed = 6

	// ConfigError indicates a configuration error.
	ConfigError = 7

	// CatalogError indicates a catalog operation failed.
	CatalogError = 8

	// TimeoutError indicates an operation timed out.
	TimeoutError = 9

	// PermissionDenied indicates insufficient permissions.
	PermissionDenied = 10
)

// Description returns a human-readable description of an exit code.
func Description(code int) string {
	switch code {
	case Success:
		return "success"
	case GeneralError:
		return "general error"
	case ValidationFailed:
		return "validation failed"
	case InvalidInput:
		return "invalid input"
	case FileNotFound:
		return "file not found"
	case RenderFailed:
		return "render failed"
	case ActionFailed:
		return "action failed"
	case ConfigError:
		return "configuration error"
	case CatalogError:
		return "catalog error"
	case TimeoutError:
		return "timeout"
	case PermissionDenied:
		return "permission denied"
	default:
		return "unknown error"
	}
}
