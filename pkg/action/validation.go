package action

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/gotmpl"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/spec"
)

// actionNameRegex validates action names.
// Names must start with a letter or underscore, followed by alphanumerics, underscores, or hyphens.
var actionNameRegex = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_-]*$`)

// ValidationError provides detailed validation failure information.
// It contains context about where the validation error occurred.
type ValidationError struct {
	// Section is the workflow section where the error occurred ("actions" or "finally").
	Section string `json:"section,omitempty" yaml:"section,omitempty" doc:"Workflow section (actions or finally)" example:"actions"`

	// ActionName is the name of the action with the validation error.
	ActionName string `json:"actionName,omitempty" yaml:"actionName,omitempty" doc:"Action that failed validation" example:"deploy"`

	// Field is the specific field that failed validation.
	Field string `json:"field,omitempty" yaml:"field,omitempty" doc:"Field that failed validation" example:"dependsOn"`

	// Message is the human-readable validation error message.
	Message string `json:"message" yaml:"message" doc:"Validation failure message" maxLength:"500"`
}

// Error implements the error interface.
func (e *ValidationError) Error() string {
	var parts []string

	if e.Section != "" {
		parts = append(parts, e.Section)
	}
	if e.ActionName != "" {
		parts = append(parts, e.ActionName)
	}
	if e.Field != "" {
		parts = append(parts, e.Field)
	}

	if len(parts) > 0 {
		return fmt.Sprintf("%s: %s", strings.Join(parts, "."), e.Message)
	}
	return e.Message
}

// AggregatedValidationError represents multiple validation errors.
// This is returned when ValidateWorkflow finds multiple issues.
type AggregatedValidationError struct {
	// Errors contains all validation errors found.
	Errors []*ValidationError `json:"errors" yaml:"errors" doc:"All validation errors" minItems:"1"`
}

// Error implements the error interface.
func (e *AggregatedValidationError) Error() string {
	if len(e.Errors) == 0 {
		return "validation failed (no details)"
	}

	if len(e.Errors) == 1 {
		return e.Errors[0].Error()
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("workflow validation failed with %d errors:\n", len(e.Errors)))
	for i, err := range e.Errors {
		sb.WriteString(fmt.Sprintf("  %d. %s\n", i+1, err.Error()))
	}
	return strings.TrimSuffix(sb.String(), "\n")
}

// HasErrors returns true if there are any validation errors.
func (e *AggregatedValidationError) HasErrors() bool {
	return len(e.Errors) > 0
}

// AddError adds a validation error.
func (e *AggregatedValidationError) AddError(err *ValidationError) {
	e.Errors = append(e.Errors, err)
}

// ToError returns nil if no errors, or the AggregatedValidationError itself.
func (e *AggregatedValidationError) ToError() error {
	if !e.HasErrors() {
		return nil
	}
	return e
}

// RegistryInterface defines the provider registry operations needed for validation.
// This interface allows for mocking in tests.
type RegistryInterface interface {
	// Get retrieves a provider by name.
	Get(name string) (provider.Provider, bool)

	// Has checks if a provider exists.
	Has(name string) bool
}

// ValidateWorkflow validates the entire workflow definition.
// It checks all validation rules and returns an aggregated error if any fail.
// Pass nil for registry to skip provider capability checks.
func ValidateWorkflow(w *Workflow, registry RegistryInterface) error {
	if w == nil {
		return &ValidationError{
			Message: "workflow cannot be nil",
		}
	}

	errs := &AggregatedValidationError{}

	// Collect all action names across sections for uniqueness check
	allNames := make(map[string]string) // name -> section

	// Validate actions section
	validateSection(w.Actions, "actions", allNames, w, registry, errs)

	// Validate finally section
	validateSection(w.Finally, "finally", allNames, w, registry, errs)

	return errs.ToError()
}

// validateSection validates all actions in a workflow section.
func validateSection(
	actions map[string]*Action,
	section string,
	allNames map[string]string,
	workflow *Workflow,
	registry RegistryInterface,
	errs *AggregatedValidationError,
) {
	for name, action := range actions {
		// Set action name from map key
		if action != nil {
			action.Name = name
		}

		// Validate action name
		validateActionName(name, section, allNames, errs)

		// Register name for uniqueness tracking
		if _, exists := allNames[name]; !exists {
			allNames[name] = section
		}

		if action == nil {
			errs.AddError(&ValidationError{
				Section:    section,
				ActionName: name,
				Message:    "action definition cannot be nil",
			})
			continue
		}

		// Validate individual action
		validateAction(action, section, workflow, registry, errs)
	}

	// Validate no dependency cycles within section
	validateNoCycles(actions, section, errs)
}

// validateActionName validates an action name.
func validateActionName(name, section string, allNames map[string]string, errs *AggregatedValidationError) {
	// Rule 1: Action names must match regex
	if !actionNameRegex.MatchString(name) {
		errs.AddError(&ValidationError{
			Section:    section,
			ActionName: name,
			Field:      "name",
			Message:    fmt.Sprintf("action name must match pattern ^[a-zA-Z_][a-zA-Z0-9_-]*$, got %q", name),
		})
	}

	// Rule 2: Action names starting with __ are reserved
	if strings.HasPrefix(name, "__") {
		errs.AddError(&ValidationError{
			Section:    section,
			ActionName: name,
			Field:      "name",
			Message:    "action names starting with '__' are reserved",
		})
	}

	// Rule 3: Action names containing [ or ] are reserved
	if strings.ContainsAny(name, "[]") {
		errs.AddError(&ValidationError{
			Section:    section,
			ActionName: name,
			Field:      "name",
			Message:    "action names containing '[' or ']' are reserved for forEach expansion",
		})
	}

	// Rule 4: Action names must be unique across all sections
	if existingSection, exists := allNames[name]; exists {
		errs.AddError(&ValidationError{
			Section:    section,
			ActionName: name,
			Field:      "name",
			Message:    fmt.Sprintf("action name %q already defined in %s section", name, existingSection),
		})
	}
}

// validateAction validates an individual action definition.
func validateAction(
	action *Action,
	section string,
	workflow *Workflow,
	registry RegistryInterface,
	errs *AggregatedValidationError,
) {
	// Validate dependsOn references
	validateDependsOn(action, section, workflow, errs)

	// Validate provider
	validateProvider(action, section, registry, errs)

	// Validate __actions references in inputs
	validateActionsReferences(action, section, workflow, errs)

	// Rule 10: ForEach only allowed in actions section
	if action.ForEach != nil && section == "finally" {
		errs.AddError(&ValidationError{
			Section:    section,
			ActionName: action.Name,
			Field:      "forEach",
			Message:    "forEach is not allowed in finally section",
		})
	}

	// Validate forEach configuration
	if action.ForEach != nil {
		validateForEach(action, section, errs)
	}

	// Validate retry configuration
	if action.Retry != nil {
		validateRetry(action, section, errs)
	}

	// Validate onError
	if action.OnError != "" && !action.OnError.IsValid() {
		errs.AddError(&ValidationError{
			Section:    section,
			ActionName: action.Name,
			Field:      "onError",
			Message:    fmt.Sprintf("onError must be 'fail' or 'continue', got %q", action.OnError),
		})
	}
}

// validateDependsOn validates action dependencies.
func validateDependsOn(action *Action, section string, workflow *Workflow, errs *AggregatedValidationError) {
	for _, dep := range action.DependsOn {
		var validActions map[string]*Action

		// Rule 5 & 6: dependsOn must reference actions in the same section
		switch section {
		case "actions":
			validActions = workflow.Actions
		case "finally":
			validActions = workflow.Finally
		}

		if _, exists := validActions[dep]; !exists {
			errs.AddError(&ValidationError{
				Section:    section,
				ActionName: action.Name,
				Field:      "dependsOn",
				Message:    fmt.Sprintf("dependency %q not found in %s section", dep, section),
			})
		}

		// Check for self-reference
		if dep == action.Name {
			errs.AddError(&ValidationError{
				Section:    section,
				ActionName: action.Name,
				Field:      "dependsOn",
				Message:    "action cannot depend on itself",
			})
		}
	}
}

// validateProvider validates the provider configuration.
func validateProvider(action *Action, section string, registry RegistryInterface, errs *AggregatedValidationError) {
	if action.Provider == "" {
		errs.AddError(&ValidationError{
			Section:    section,
			ActionName: action.Name,
			Field:      "provider",
			Message:    "provider is required",
		})
		return
	}

	// Rule 8: Provider must exist and have CapabilityAction
	if registry == nil {
		// Skip provider validation if no registry provided
		return
	}

	p, exists := registry.Get(action.Provider)
	if !exists {
		errs.AddError(&ValidationError{
			Section:    section,
			ActionName: action.Name,
			Field:      "provider",
			Message:    fmt.Sprintf("provider %q not found", action.Provider),
		})
		return
	}

	// Check for CapabilityAction
	desc := p.Descriptor()
	if desc == nil {
		errs.AddError(&ValidationError{
			Section:    section,
			ActionName: action.Name,
			Field:      "provider",
			Message:    fmt.Sprintf("provider %q has no descriptor", action.Provider),
		})
		return
	}

	hasActionCapability := false
	for _, cap := range desc.Capabilities {
		if cap == provider.CapabilityAction {
			hasActionCapability = true
			break
		}
	}

	if !hasActionCapability {
		errs.AddError(&ValidationError{
			Section:    section,
			ActionName: action.Name,
			Field:      "provider",
			Message:    fmt.Sprintf("provider %q does not have action capability", action.Provider),
		})
	}
}

// validateActionsReferences validates __actions references in action inputs.
func validateActionsReferences(action *Action, section string, workflow *Workflow, errs *AggregatedValidationError) {
	// Rule 9: __actions.<name> references must be valid
	refs := extractActionsReferences(action)

	for _, ref := range refs {
		// For regular actions: referenced action must be in dependsOn
		if section == "actions" {
			if !slices.Contains(action.DependsOn, ref) {
				// Also check if the action exists at all
				if _, exists := workflow.Actions[ref]; !exists {
					errs.AddError(&ValidationError{
						Section:    section,
						ActionName: action.Name,
						Field:      "inputs",
						Message:    fmt.Sprintf("__actions reference to %q: action not found", ref),
					})
				} else {
					errs.AddError(&ValidationError{
						Section:    section,
						ActionName: action.Name,
						Field:      "inputs",
						Message:    fmt.Sprintf("__actions reference to %q requires it to be listed in dependsOn", ref),
					})
				}
			}
		}

		// For finally actions: referenced action must exist in regular actions or be in dependsOn
		if section == "finally" {
			_, inActions := workflow.Actions[ref]
			_, inFinally := workflow.Finally[ref]

			if !inActions && !inFinally {
				errs.AddError(&ValidationError{
					Section:    section,
					ActionName: action.Name,
					Field:      "inputs",
					Message:    fmt.Sprintf("__actions reference to %q: action not found", ref),
				})
			}

			// If referencing a finally action, it must be in dependsOn
			if inFinally && !slices.Contains(action.DependsOn, ref) && ref != action.Name {
				errs.AddError(&ValidationError{
					Section:    section,
					ActionName: action.Name,
					Field:      "inputs",
					Message:    fmt.Sprintf("__actions reference to finally action %q requires it to be listed in dependsOn", ref),
				})
			}
		}
	}
}

// extractActionsReferences extracts action names referenced via __actions from inputs and when condition.
func extractActionsReferences(action *Action) []string {
	refs := make(map[string]struct{})

	// Extract from inputs
	for _, valueRef := range action.Inputs {
		if valueRef == nil {
			continue
		}
		extractRefsFromValueRef(valueRef, refs)
	}

	// Extract from when condition
	if action.When != nil && action.When.Expr != nil {
		extractRefsFromExpression(action.When.Expr, refs)
	}

	// Convert to slice
	result := make([]string, 0, len(refs))
	for ref := range refs {
		result = append(result, ref)
	}
	return result
}

// extractRefsFromValueRef extracts __actions references from a ValueRef.
func extractRefsFromValueRef(v *spec.ValueRef, refs map[string]struct{}) {
	if v == nil {
		return
	}

	if v.Expr != nil {
		extractRefsFromExpression(v.Expr, refs)
	}

	if v.Tmpl != nil {
		extractRefsFromTemplate(v.Tmpl, refs)
	}
}

// extractRefsFromExpression extracts __actions references from a CEL expression.
func extractRefsFromExpression(expr *celexp.Expression, refs map[string]struct{}) {
	if expr == nil {
		return
	}

	// Use the RequiredVariables to check for __actions, then parse action names
	requiredVars, err := expr.RequiredVariables()
	if err != nil {
		// Can't parse expression, skip reference extraction
		return
	}

	if !slices.Contains(requiredVars, celexp.VarActions) {
		return
	}

	// Parse the expression to find __actions.<name> patterns
	exprStr := string(*expr)
	parseActionsRefsFromString(exprStr, refs)
}

// extractRefsFromTemplate extracts __actions references from a Go template.
func extractRefsFromTemplate(tmpl *gotmpl.GoTemplatingContent, refs map[string]struct{}) {
	if tmpl == nil {
		return
	}

	// Use GetGoTemplateReferences to parse the template
	tmplRefs, err := gotmpl.GetGoTemplateReferences(string(*tmpl), "", "")
	if err != nil {
		// Can't parse template, skip reference extraction
		return
	}

	for _, ref := range tmplRefs {
		// Template references come as .__actions.name.field or __actions.name.field
		path := ref.Path
		if strings.HasPrefix(path, ".__actions.") || strings.HasPrefix(path, "__actions.") {
			parseActionsRefsFromString(path, refs)
		}
	}
}

// parseActionsRefsFromString parses __actions.<name> patterns from a string.
func parseActionsRefsFromString(s string, refs map[string]struct{}) {
	// Pattern: __actions.actionName or __actions["actionName"]
	// actionName can be followed by .field or ["field"]

	// Simple pattern matching for __actions.name
	idx := 0
	for idx < len(s) {
		// Find __actions
		actionsIdx := strings.Index(s[idx:], "__actions")
		if actionsIdx == -1 {
			break
		}
		idx += actionsIdx + len("__actions")

		if idx >= len(s) {
			break
		}

		// Expect . or [
		switch s[idx] {
		case '.':
			// __actions.name pattern
			idx++
			name := parseIdentifier(s[idx:])
			if name != "" {
				refs[name] = struct{}{}
				idx += len(name)
			}

		case '[':
			// __actions["name"] or __actions['name'] pattern
			idx++
			name := parseQuotedString(s[idx:])
			if name != "" {
				refs[name] = struct{}{}
				// Skip past the quoted string and closing bracket
				idx += len(name) + 2 // quotes
				if idx < len(s) && s[idx] == ']' {
					idx++
				}
			}
		}
	}
}

// parseIdentifier parses an identifier (action name) from a string.
func parseIdentifier(s string) string {
	var sb strings.Builder
	for _, c := range s {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-' {
			sb.WriteRune(c)
		} else {
			break
		}
	}
	return sb.String()
}

// parseQuotedString parses a quoted string.
func parseQuotedString(s string) string {
	if len(s) < 2 {
		return ""
	}

	quote := s[0]
	if quote != '"' && quote != '\'' {
		return ""
	}

	end := strings.IndexByte(s[1:], quote)
	if end == -1 {
		return ""
	}

	return s[1 : end+1]
}

// validateForEach validates the forEach configuration.
func validateForEach(action *Action, section string, errs *AggregatedValidationError) {
	forEach := action.ForEach

	// Rule 13: forEach.onError must be valid
	if forEach.OnError != "" && !forEach.OnError.IsValid() {
		errs.AddError(&ValidationError{
			Section:    section,
			ActionName: action.Name,
			Field:      "forEach.onError",
			Message:    fmt.Sprintf("forEach.onError must be 'fail' or 'continue', got %q", forEach.OnError),
		})
	}

	// Rule 14: forEach.concurrency must be >= 0
	if forEach.Concurrency < 0 {
		errs.AddError(&ValidationError{
			Section:    section,
			ActionName: action.Name,
			Field:      "forEach.concurrency",
			Message:    fmt.Sprintf("forEach.concurrency must be >= 0, got %d", forEach.Concurrency),
		})
	}
}

// validateRetry validates the retry configuration.
func validateRetry(action *Action, section string, errs *AggregatedValidationError) {
	retry := action.Retry

	// Rule 11: retry.maxAttempts must be >= 1
	if retry.MaxAttempts < 1 {
		errs.AddError(&ValidationError{
			Section:    section,
			ActionName: action.Name,
			Field:      "retry.maxAttempts",
			Message:    fmt.Sprintf("retry.maxAttempts must be >= 1, got %d", retry.MaxAttempts),
		})
	}

	// Validate backoff type
	if retry.Backoff != "" && !retry.Backoff.IsValid() {
		errs.AddError(&ValidationError{
			Section:    section,
			ActionName: action.Name,
			Field:      "retry.backoff",
			Message:    fmt.Sprintf("retry.backoff must be 'fixed', 'linear', or 'exponential', got %q", retry.Backoff),
		})
	}
}

// validateNoCycles checks for dependency cycles within a section.
func validateNoCycles(actions map[string]*Action, section string, errs *AggregatedValidationError) {
	// Build adjacency list
	deps := make(map[string][]string)
	for name, action := range actions {
		if action != nil {
			deps[name] = action.DependsOn
		}
	}

	// Rule 7: No dependency cycles
	cycle := findCycle(deps)
	if cycle != nil {
		errs.AddError(&ValidationError{
			Section: section,
			Field:   "dependsOn",
			Message: fmt.Sprintf("circular dependency detected: %s", strings.Join(cycle, " → ")),
		})
	}
}

// findCycle detects cycles in a dependency graph using DFS.
// Returns the cycle path if found, nil otherwise.
func findCycle(deps map[string][]string) []string {
	// State: 0 = unvisited, 1 = visiting, 2 = visited
	state := make(map[string]int)
	parent := make(map[string]string)

	var cycle []string

	var dfs func(node string) bool
	dfs = func(node string) bool {
		state[node] = 1 // visiting

		for _, dep := range deps[node] {
			if state[dep] == 1 {
				// Found a cycle - reconstruct the path
				cycle = []string{dep}
				for curr := node; curr != dep; curr = parent[curr] {
					cycle = append([]string{curr}, cycle...)
				}
				cycle = append(cycle, dep) // close the cycle
				return true
			}

			if state[dep] == 0 {
				parent[dep] = node
				if dfs(dep) {
					return true
				}
			}
		}

		state[node] = 2 // visited
		return false
	}

	// Start DFS from all unvisited nodes
	for node := range deps {
		if state[node] == 0 {
			if dfs(node) {
				return cycle
			}
		}
	}

	return nil
}

// Ensure context package is used (for future enhancements)
var _ = context.Background
