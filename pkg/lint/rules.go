// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lint

import "sort"

// RuleMeta describes a single lint rule, including its severity, category,
// and human-readable guidance. This is the authoritative source of truth
// for lint rule metadata — both the linter and the MCP server derive
// their knowledge from this registry.
type RuleMeta struct {
	// Rule is the kebab-case rule identifier used in Finding.RuleName.
	Rule string `json:"rule"`

	// Severity is one of "error", "warning", or "info".
	Severity string `json:"severity"`

	// Category groups related rules (e.g. "structure", "naming", "provider").
	Category string `json:"category"`

	// Description is a short summary of what the rule checks.
	Description string `json:"description"`

	// Why explains the rationale for the rule.
	Why string `json:"why"`

	// Fix gives concrete instructions for resolving a finding.
	Fix string `json:"fix"`

	// Examples optionally provide sample YAML or commands.
	Examples []string `json:"examples,omitempty"`
}

// KnownRules is the canonical registry of all lint rules.
// Every addFinding call in lint.go MUST use a key from this map.
var KnownRules = map[string]RuleMeta{
	"empty-solution": {
		Rule:        "empty-solution",
		Severity:    string(SeverityError),
		Category:    "structure",
		Description: "The solution has no resolvers defined under spec.resolvers and no workflow defined under spec.workflow.",
		Why:         "A solution must have at least resolvers or a workflow to be useful. Without either, there's nothing to execute.",
		Fix:         "Add at least one resolver under spec.resolvers or define a workflow with actions under spec.workflow.actions.",
		Examples: []string{
			"spec:\n  resolvers:\n    greeting:\n      type: string\n      resolve:\n        with:\n          - provider: static\n            inputs:\n              value: Hello World",
		},
	},
	"reserved-name": {
		Rule:        "reserved-name",
		Severity:    string(SeverityError),
		Category:    "naming",
		Description: "A resolver uses a reserved name that conflicts with built-in variables.",
		Why:         "Names like '__actions', '__error', '__item', '__index', and '_' are reserved for internal use in CEL expressions and forEach iterations.",
		Fix:         "Rename the resolver to avoid reserved names. Use descriptive names like 'user-input' or 'api-response' instead.",
		Examples:    []string{"Reserved names: __actions, __error, __item, __index, _"},
	},
	"unused-resolver": {
		Rule:        "unused-resolver",
		Severity:    string(SeverityWarning),
		Category:    "usage",
		Description: "A resolver is defined but never referenced by any other resolver, action input, when clause, or expression.",
		Why:         "Unused resolvers add complexity without benefit. They may indicate a typo in a reference or leftover code from refactoring.",
		Fix:         "Either remove the unused resolver, reference it in an action input (rslvr: resolver-name), use it in a CEL expression (_.resolver_name), or add it as a dependency (dependsOn).",
	},
	"missing-provider": {
		Rule:        "missing-provider",
		Severity:    string(SeverityError),
		Category:    "provider",
		Description: "A resolver step or action references a provider name that is not registered in the provider registry.",
		Why:         "The solution cannot execute if it references providers that don't exist. This usually indicates a typo in the provider name.",
		Fix:         "Use list_providers to see all available provider names. Common providers: static, parameter, env, http, cel, file, exec, directory, go-template, validation.",
	},
	"invalid-expression": {
		Rule:        "invalid-expression",
		Severity:    string(SeverityError),
		Category:    "expression",
		Description: "A CEL expression (in a 'when' clause or 'expr' input) has a syntax error and cannot be parsed.",
		Why:         "Invalid CEL expressions will cause runtime failures. Common issues include missing quotes around strings, unbalanced parentheses, and using unknown functions.",
		Fix:         "Use validate_expression with type 'cel' to check the expression syntax. Use list_cel_functions to see available functions. Use evaluate_cel to test the expression with sample data.",
	},
	"invalid-template": {
		Rule:        "invalid-template",
		Severity:    string(SeverityError),
		Category:    "template",
		Description: "A Go template (in a 'tmpl' input) has a syntax error and cannot be parsed.",
		Why:         "Invalid Go templates will cause runtime failures. Common issues include unclosed actions (missing '}}'), unclosed control blocks (if/range/with without end), and unknown functions.",
		Fix:         "Use validate_expression with type 'go-template' to check the template syntax. Use evaluate_go_template to test the template with sample data.",
	},
	"invalid-dependency": {
		Rule:        "invalid-dependency",
		Severity:    string(SeverityError),
		Category:    "dependency",
		Description: "An action's dependsOn references an action name that does not exist in the workflow.",
		Why:         "Actions with invalid dependencies cannot be scheduled for execution. This usually indicates a typo in the action name.",
		Fix:         "Check the action names in spec.workflow.actions and ensure all dependsOn references match exactly. Names are case-sensitive.",
	},
	"empty-workflow": {
		Rule:        "empty-workflow",
		Severity:    string(SeverityWarning),
		Category:    "structure",
		Description: "A workflow section is defined (spec.workflow) but contains no actions.",
		Why:         "An empty workflow serves no purpose. If you only need resolvers, remove the workflow section entirely.",
		Fix:         "Either add actions under spec.workflow.actions or remove spec.workflow entirely if you only need resolvers.",
	},
	"finally-with-foreach": {
		Rule:        "finally-with-foreach",
		Severity:    string(SeverityError),
		Category:    "validation",
		Description: "An action in the spec.workflow.finally section uses forEach, which is not supported.",
		Why:         "Finally actions are cleanup/teardown steps that must run reliably. forEach adds complexity and potential failure points that could prevent cleanup from completing.",
		Fix:         "Remove the forEach from the finally action. If you need to iterate during cleanup, move the action to spec.workflow.actions and handle cleanup differently.",
	},
	"workflow-validation": {
		Rule:        "workflow-validation",
		Severity:    string(SeverityError),
		Category:    "validation",
		Description: "The workflow has structural validation errors such as circular dependencies between actions.",
		Why:         "Circular dependencies create infinite loops that prevent the workflow from completing. The dependency graph must be a DAG (directed acyclic graph).",
		Fix:         "Use render_solution with graph_type 'action-deps' to visualize the dependency graph. Break cycles by reordering dependencies or removing unnecessary dependsOn references.",
	},
	"schema-violation": {
		Rule:        "schema-violation",
		Severity:    string(SeverityError),
		Category:    "schema",
		Description: "The solution YAML violates the JSON Schema definition. This includes unknown fields, type mismatches, pattern violations, and constraint violations.",
		Why:         "Schema violations indicate the YAML structure doesn't match what scafctl expects. This will cause parsing errors or unexpected behavior.",
		Fix:         "Use get_solution_schema to see the expected schema. Common issues: unknown field names (typos), wrong types (string vs int), invalid metadata.name pattern (must be lowercase with hyphens, 3-60 chars).",
	},
	"unknown-provider-input": {
		Rule:        "unknown-provider-input",
		Severity:    string(SeverityError),
		Category:    "provider",
		Description: "An input key passed to a provider is not declared in that provider's input schema.",
		Why:         "Unknown inputs are silently ignored, which usually indicates a typo. The intended field won't receive its value.",
		Fix:         "Use get_provider_schema with the provider name to see all valid input field names. For example, the exec provider requires 'command' (not 'cmd').",
	},
	"invalid-provider-input-type": {
		Rule:        "invalid-provider-input-type",
		Severity:    string(SeverityError),
		Category:    "provider",
		Description: "A literal input value doesn't match the type expected by the provider's schema (e.g., passing a string where an integer is required).",
		Why:         "Type mismatches cause runtime errors. The provider expects a specific type and will fail to process the wrong type.",
		Fix:         "Use get_provider_schema to check the expected type for each input. Ensure literal values match (e.g., use '42' not 'forty-two' for integer fields).",
	},
	"invalid-test-name": {
		Rule:        "invalid-test-name",
		Severity:    string(SeverityError),
		Category:    "naming",
		Description: "A test case name doesn't match the required naming pattern (lowercase with hyphens).",
		Why:         "Test names must be lowercase with hyphens (e.g., 'my-test-case') for consistency and to work correctly with filtering.",
		Fix:         "Rename the test to use only lowercase letters, numbers, and hyphens. Avoid spaces, underscores, and uppercase letters.",
	},
	"unbundled-test-file": {
		Rule:        "unbundled-test-file",
		Severity:    string(SeverityError),
		Category:    "bundling",
		Description: "A test case references a file that is not covered by the solution's bundle.include patterns.",
		Why:         "When the solution is published to the catalog, unbundled files won't be included, causing test failures.",
		Fix:         "Add the file path or a glob pattern to bundle.include that covers the test file. Example: bundle:\n  include:\n    - 'tests/**'",
	},
	"unused-template": {
		Rule:        "unused-template",
		Severity:    string(SeverityWarning),
		Category:    "usage",
		Description: "A test template (name starting with '_') is defined but not referenced by any test case via 'extends'.",
		Why:         "Unused test templates add unnecessary complexity. They may indicate a typo in an extends reference.",
		Fix:         "Either remove the unused template or reference it from a test case using 'extends: _template-name'.",
	},
	"missing-description": {
		Rule:        "missing-description",
		Severity:    string(SeverityInfo),
		Category:    "documentation",
		Description: "A resolver or action does not have a description field.",
		Why:         "Descriptions help others understand what each resolver/action does. They appear in inspect_solution output and documentation.",
		Fix:         "Add a description field: description: \"Brief explanation of what this does\"",
	},
	"long-timeout": {
		Rule:        "long-timeout",
		Severity:    string(SeverityInfo),
		Category:    "performance",
		Description: "An action has a timeout exceeding 10 minutes.",
		Why:         "Very long timeouts may indicate a misconfiguration or an action that should be broken into smaller steps. They also delay failure detection.",
		Fix:         "Consider reducing the timeout or breaking the action into smaller steps. If the long timeout is intentional, this is just an informational notice.",
	},
	"unused-finally": {
		Rule:        "unused-finally",
		Severity:    string(SeverityInfo),
		Category:    "structure",
		Description: "A finally section is defined but there are no regular actions in the workflow.",
		Why:         "Finally actions are cleanup steps that run after regular actions. Without regular actions, there's nothing to clean up after.",
		Fix:         "Either add regular actions under spec.workflow.actions or remove the spec.workflow.finally section.",
	},
	"permissive-result-schema": {
		Rule:        "permissive-result-schema",
		Severity:    string(SeverityInfo),
		Category:    "schema",
		Description: "An action's resultSchema is defined but doesn't specify a type, making it accept any value.",
		Why:         "A result schema without a type constraint doesn't provide meaningful validation of action output.",
		Fix:         "Add a 'type' field to the resultSchema: resultSchema:\n  type: object\n  properties:\n    status:\n      type: string",
	},
	"invalid-result-schema": {
		Rule:        "invalid-result-schema",
		Severity:    string(SeverityError),
		Category:    "schema",
		Description: "An action's resultSchema is not a valid JSON Schema definition.",
		Why:         "Invalid result schemas cannot be used to validate action outputs, causing runtime errors.",
		Fix:         "Ensure the resultSchema follows JSON Schema syntax. Use get_solution_schema to see the expected format.",
	},
	"undefined-required-property": {
		Rule:        "undefined-required-property",
		Severity:    string(SeverityError),
		Category:    "schema",
		Description: "A resultSchema lists a property in 'required' that is not defined in 'properties'.",
		Why:         "Requiring a property that isn't defined means the validation will always fail for that property.",
		Fix:         "Either add the missing property to 'properties' or remove it from the 'required' list.",
	},
	"schema-error": {
		Rule:        "schema-error",
		Severity:    string(SeverityWarning),
		Category:    "schema",
		Description: "Schema validation could not be performed (schema generation failed).",
		Why:         "This is an internal issue that prevents full validation. The solution may still work but hasn't been fully checked.",
		Fix:         "This usually resolves itself. If persistent, check that the solution YAML is well-formed (valid YAML syntax).",
	},
	"resolver-self-reference": {
		Rule:        "resolver-self-reference",
		Severity:    string(SeverityError),
		Category:    "expression",
		Description: "A resolver's validate or transform phase references its own name via _.resolverName instead of using __self.",
		Why:         "Using _.resolverName in a transform or validate expression creates a circular dependency, because the dependency graph interprets it as the resolver depending on itself. Use __self to access the resolver's current value in these phases.",
		Fix:         "Replace _.resolverName with __self in the expression. For example, change '_.myResolver.statusCode == 200' to '__self.statusCode == 200'.",
		Examples: []string{
			"# Wrong (causes cycle):\nvalidate:\n  with:\n    - provider: validation\n      inputs:\n        expression: \"_.myResolver.statusCode == 200\"\n\n# Correct:\nvalidate:\n  with:\n    - provider: validation\n      inputs:\n        expression: \"__self.statusCode == 200\"",
		},
	},
	"unreachable-test-path": {
		Rule:        "unreachable-test-path",
		Severity:    string(SeverityWarning),
		Category:    "testing",
		Description: "A test case references a file path in its 'files' list that does not exist on disk and does not match any file via glob expansion.",
		Why:         "Tests that reference non-existent files will fail at sandbox setup time with a confusing error. This usually indicates a typo in the path, a deleted file, or a glob pattern that doesn't match anything.",
		Fix:         "Check the file path for typos. Run 'ls' or glob expansion to verify the pattern matches files. Use directories (e.g., 'templates/') or globs (e.g., 'templates/**/*.yaml') for dynamic file sets.",
		Examples: []string{
			"# Correct patterns:\nfiles:\n  - templates/main.yaml      # exact file\n  - data/                     # entire directory\n  - configs/**/*.yaml          # recursive glob",
		},
	},
	"nil-provider-input": {
		Rule:        "nil-provider-input",
		Severity:    string(SeverityError),
		Category:    "provider",
		Description: "A provider input key has no value (dangling YAML key). This will cause resolver execution to fail at runtime with an error.",
		Why:         "A YAML key with no value (e.g., 'my-input:' on its own line) results in a nil entry. When the resolver engine evaluates provider inputs, a nil value causes resolution to fail with a descriptive runtime error.",
		Fix:         "Either provide a value for the input key or remove the dangling key entirely.",
		Examples: []string{
			"# Wrong (dangling key):\ninputs:\n  my-input:\n\n# Correct:\ninputs:\n  my-input: 'some-value'",
		},
	},
	"empty-transform-with": {
		Rule:        "empty-transform-with",
		Severity:    string(SeverityWarning),
		Category:    "structure",
		Description: "A resolver has a transform phase with an empty 'with' array. No transformations will be applied.",
		Why:         "An empty transform.with array is almost certainly unintentional. The transform phase will be silently skipped, which may cause unexpected resolver output.",
		Fix:         "Either add transform steps to the with array or remove the transform section entirely.",
	},
	"empty-validate-with": {
		Rule:        "empty-validate-with",
		Severity:    string(SeverityWarning),
		Category:    "structure",
		Description: "A resolver has a validate phase with an empty 'with' array. No validations will be applied.",
		Why:         "An empty validate.with array is almost certainly unintentional. The validate phase will be silently skipped, which may cause unexpected behavior.",
		Fix:         "Either add validation rules to the with array or remove the validate section entirely.",
	},
	"null-resolver": {
		Rule:        "null-resolver",
		Severity:    string(SeverityError),
		Category:    "structure",
		Description: "A resolver key has a null value instead of a resolver definition object.",
		Why:         "A YAML key with no value (e.g., 'my-resolver:' on its own line) results in a nil entry. Resolvers require at minimum a resolve block with provider steps.",
		Fix:         "Define the resolver with at least a resolve block, or remove the dangling key entirely.",
		Examples: []string{
			"# Wrong (null resolver):\nresolvers:\n  my-resolver:\n\n# Correct:\nresolvers:\n  my-resolver:\n    resolve:\n      with:\n        - provider: static\n          inputs:\n            value: \"...\"",
		},
	},
	"exec-command-injection": {
		Rule:        "exec-command-injection",
		Severity:    string(SeverityWarning),
		Category:    "security",
		Description: "The exec provider's 'command' input uses a dynamic expression or template, which risks command injection via shell metacharacters.",
		Why:         "When command strings are built from resolved values, shell metacharacters in those values can escape the intended command and execute arbitrary code.",
		Fix:         "Pass dynamic values via the 'args' input instead. Arguments are shell-quoted before being appended to the command, reducing injection risk.",
		Examples: []string{
			"# Wrong — dynamic value in command string:\nprovider: exec\ninputs:\n  command:\n    expr: \"'echo ' + _.userInput\"\n\n# Correct — static command, dynamic args:\nprovider: exec\ninputs:\n  command: echo\n  args:\n    - expr: \"_.userInput\"",
		},
	},
	"tmpl-underscore-prefix": {
		Rule:        "tmpl-underscore-prefix",
		Severity:    string(SeverityError),
		Category:    "template",
		Description: "A Go template uses '{{ ._.resolverName }}' which is not supported. Use '{{ .resolverName }}' instead.",
		Why:         "Go templates spread resolver data at the top level, so resolvers are accessed directly with '{{ .resolverName }}'. The '._' prefix is a CEL convention ('_.resolverName') that does not apply to Go templates. Using '._' in a Go template will cause a runtime error.",
		Fix:         "Replace '{{ ._.resolverName }}' with '{{ .resolverName }}'. In Go templates, access resolvers directly. Use '._' only in CEL expressions (expr:).",
		Examples: []string{
			"# Wrong:\ntmpl: \"Deploying {{ ._.config.appName }}\"\n\n# Correct:\ntmpl: \"Deploying {{ .config.appName }}\"",
		},
	},
	"missing-state-backend": {
		Rule:        "missing-state-backend",
		Severity:    string(SeverityError),
		Category:    "state",
		Description: "The state block is configured but the backend provider is not specified.",
		Why:         "State persistence requires a backend provider (e.g., 'file' or 'github') with CapabilityState to load and save state data.",
		Fix:         "Add a backend.provider field to the state block, e.g.:\n  state:\n    enabled: true\n    backend:\n      provider: file",
	},
	"invalid-state-backend": {
		Rule:        "invalid-state-backend",
		Severity:    string(SeverityError),
		Category:    "state",
		Description: "The state backend references a provider that is not registered or lacks CapabilityState.",
		Why:         "State backends must implement CapabilityState. Using an unregistered or incompatible provider will fail at runtime.",
		Fix:         "Use a registered provider with CapabilityState such as 'file' or 'github'.",
	},
	"state-circular-dependency": {
		Rule:        "state-circular-dependency",
		Severity:    string(SeverityError),
		Category:    "state",
		Description: "A resolver referenced by state.enabled or state.backend.inputs has saveToState or uses the state-reading provider.",
		Why:         "State loading depends on these resolvers, but they would also read/write state, creating a circular dependency.",
		Fix:         "Ensure resolvers referenced by state config do not have saveToState: true and do not use state backend providers.",
	},
	"sensitive-state": {
		Rule:        "sensitive-state",
		Severity:    string(SeverityWarning),
		Category:    "security",
		Description: "A resolver marked sensitive: true also has saveToState: true. The sensitive value will be stored in plaintext in the state file.",
		Why:         "Sensitive values (API keys, tokens) saved to state are persisted unencrypted. This is intentional for validation replay but should be an explicit, informed decision.",
		Fix:         "Acknowledge the risk or remove saveToState from sensitive resolvers. Consider using the secret provider for sensitive values that do not need state persistence.",
	},
	"state-resolver-ref": {
		Rule:        "state-resolver-ref",
		Severity:    string(SeverityError),
		Category:    "state",
		Description: "A state.enabled or state.backend.inputs field uses a direct rslvr: reference. State is loaded before resolvers run, so resolver results are not available.",
		Why:         "State configuration is resolved before resolver execution using only CLI parameters (-r flags) and environment data. Direct rslvr: references will fail at runtime because resolver results do not exist yet.",
		Fix:         "Use a CEL expression referencing CLI parameters instead, e.g.:\n  path:\n    expr: \"__params.appName + '-state.json'\"\nwhere appName is passed via -r appName=myapp.",
	},
}

// ListRules returns all known lint rules sorted by severity (error > warning > info)
// then alphabetically by rule name.
func ListRules() []RuleMeta {
	rules := make([]RuleMeta, 0, len(KnownRules))
	for _, r := range KnownRules {
		rules = append(rules, r)
	}

	severityOrder := map[string]int{
		string(SeverityError):   0,
		string(SeverityWarning): 1,
		string(SeverityInfo):    2,
	}
	sort.Slice(rules, func(i, j int) bool {
		si, sj := severityOrder[rules[i].Severity], severityOrder[rules[j].Severity]
		if si != sj {
			return si < sj
		}
		return rules[i].Rule < rules[j].Rule
	})

	return rules
}

// GetRule looks up a rule by name. Returns the RuleMeta and true if found.
func GetRule(name string) (RuleMeta, bool) {
	r, ok := KnownRules[name]
	return r, ok
}
