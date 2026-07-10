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
	Rule string `json:"rule" yaml:"rule"`

	// Severity is one of "error", "warning", or "info".
	Severity string `json:"severity" yaml:"severity"`

	// Category groups related rules (e.g. "structure", "naming", "provider").
	Category string `json:"category" yaml:"category"`

	// Description is a short summary of what the rule checks.
	Description string `json:"description" yaml:"description"`

	// Why explains the rationale for the rule.
	Why string `json:"why" yaml:"why"`

	// Fix gives concrete instructions for resolving a finding.
	Fix string `json:"fix" yaml:"fix"`

	// Examples optionally provide sample YAML or commands.
	Examples []string `json:"examples,omitempty" yaml:"examples,omitempty"`
}

// KnownRules is the canonical registry of all lint rules.
// Every addFinding call in lint.go MUST use a key from this map.
var KnownRules = map[string]RuleMeta{
	"deprecated-field": {
		Rule:        "deprecated-field",
		Severity:    string(SeverityWarning),
		Category:    "deprecation",
		Description: "A field that has been marked deprecated is used. The solution still works, but the field will be removed in a future major version.",
		Why:         "Deprecated fields are retained for backward compatibility but should be migrated to their replacement. Keeping them increases the risk of breakage when the field is eventually removed.",
		Fix:         "Replace the deprecated field with the suggested replacement field. Run get_solution_schema to see the replacement, or check the field's [DEPRECATED] marker.",
		Examples: []string{
			"# Deprecated:\n- provider: http\n  onError: continue\n\n# Replacement:\n- provider: http\n  continueOnError: true",
		},
	},
	"deprecated-field-conflict": {
		Rule:        "deprecated-field-conflict",
		Severity:    string(SeverityError),
		Category:    "deprecation",
		Description: "Both a deprecated field and its replacement are set on the same object.",
		Why:         "When both are present the replacement takes precedence at runtime and the deprecated field is silently ignored, which is confusing and error-prone.",
		Fix:         "Remove the deprecated field and keep only its replacement.",
		Examples: []string{
			"# Conflict (remove onError):\n- provider: http\n  onError: continue\n  continueOnError: true",
		},
	},
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
	"hyphenated-name": {
		Rule:        "hyphenated-name",
		Severity:    string(SeverityWarning),
		Category:    "naming",
		Description: "A resolver name contains hyphens, which require bracket notation in CEL expressions.",
		Why:         "Hyphens in resolver names require quoting in CEL: _[\"my-resolver\"] instead of _.myResolver. This is more verbose and error-prone for both humans and AI agents.",
		Fix:         "Use camelCase for CEL-friendly access: myResolver instead of my-resolver.",
		Examples: []string{
			"# Hyphenated (requires quoting in CEL):\nresolvers:\n  my-service:\n    ...\n# Access: _[\"my-service\"]\n\n# camelCase (direct CEL access):\nresolvers:\n  myService:\n    ...\n# Access: _.myService",
		},
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
	"call-not-found": {
		Rule:        "call-not-found",
		Severity:    string(SeverityError),
		Category:    "call",
		Description: "A resolve/transform/validate step or action sets 'call' to a name that is not defined under spec.calls.",
		Why:         "The call cannot be expanded at runtime if the referenced definition does not exist. This is usually a typo or a missing definition.",
		Fix:         "Add the definition under spec.calls, or correct the 'call' name to match an existing definition. Run get_solution_schema to see the calls section.",
		Examples: []string{
			"spec:\n  calls:\n    getUser:\n      provider: http\n      inputs:\n        uri: {tmpl: 'https://api/users/{{ .args.id }}'}\n      args:\n        id: {type: string, required: true}\n  resolvers:\n    user:\n      resolve:\n        with:\n          - call: getUser\n            args: {id: {literal: '1'}}",
		},
	},
	"call-provider-exclusive": {
		Rule:        "call-provider-exclusive",
		Severity:    string(SeverityError),
		Category:    "call",
		Description: "A step or action sets both 'call' and 'provider'. Exactly one may be set.",
		Why:         "A host either invokes a reusable call definition (which supplies the provider) or names a provider directly. Setting both is ambiguous.",
		Fix:         "Remove 'provider' when using 'call', or remove 'call' when naming a provider directly.",
	},
	"call-args-without-call": {
		Rule:        "call-args-without-call",
		Severity:    string(SeverityError),
		Category:    "call",
		Description: "A step or action sets 'args' but has no 'call'. Arguments have nowhere to bind without a call.",
		Why:         "The 'args' block only applies to a call invocation. Without 'call', the arguments are silently ignored, which is almost always a mistake.",
		Fix:         "Add a 'call' referencing a definition, or remove the 'args' block.",
	},
	"call-missing-arg": {
		Rule:        "call-missing-arg",
		Severity:    string(SeverityError),
		Category:    "call",
		Description: "A call site does not supply a required argument declared by the call definition.",
		Why:         "Required arguments must be supplied at every call site; a missing one fails at bind time.",
		Fix:         "Supply the required argument under 'args', or make the argument optional (add a default) in the definition.",
	},
	"call-unknown-arg": {
		Rule:        "call-unknown-arg",
		Severity:    string(SeverityError),
		Category:    "call",
		Description: "A call site supplies an argument name that the call definition does not declare.",
		Why:         "Unknown arguments are rejected at bind time and usually indicate a typo in the argument name.",
		Fix:         "Correct the argument name to match a declared arg, or declare the argument under the definition's 'args'.",
	},
	"call-provider-not-found": {
		Rule:        "call-provider-not-found",
		Severity:    string(SeverityWarning),
		Category:    "call",
		Description: "A call definition names a provider that is not registered in the provider registry.",
		Why:         "The definition cannot execute if its provider does not exist. This is usually a typo in the provider name.",
		Fix:         "Use list_providers to see available providers, and correct the definition's 'provider' field.",
	},
	"call-definition-resolver-ref": {
		Rule:        "call-definition-resolver-ref",
		Severity:    string(SeverityError),
		Category:    "call",
		Description: "A call definition input references a solution resolver via _.resolverName.",
		Why:         "Call definitions are strictly isolated: their inputs may reference only their declared args plus always-available globals. Reaching into a solution resolver breaks isolation, makes the definition impossible to reason about on its own, and injects hidden dependency edges that change scheduling for every call site.",
		Fix:         "Pass the value in as a declared argument: add it under the definition's 'args' and supply it at each call site via 'args'.",
	},
	"invalid-expression": {
		Rule:        "invalid-expression",
		Severity:    string(SeverityError),
		Category:    "expression",
		Description: "A CEL expression (in a 'when' clause or 'expr' input) has a syntax error and cannot be parsed.",
		Why:         "Invalid CEL expressions will cause runtime failures. Common issues include missing quotes around strings, unbalanced parentheses, and using unknown functions.",
		Fix:         "Use validate_expression with type 'cel' to check the expression syntax. Use list_cel_functions to see available functions. Use evaluate_cel to test the expression with sample data.",
	},
	"orvalue-on-non-optional": {
		Rule:        "orvalue-on-non-optional",
		Severity:    string(SeverityError),
		Category:    "expression",
		Description: "A CEL expression calls '.orValue(...)' on a provably non-optional (concrete) value, such as '__self.orValue([])' or '_.field.orValue(\"\")'.",
		Why:         "orValue() only has an overload on CEL optional types. Calling it on a concrete value (a plain identifier or field-access chain) has no matching overload and fails at runtime, even though the expression parses and lints for syntax. This commonly happens when an optional marker ('.?') is dropped during editing or conversion.",
		Fix:         "Make the receiver optional so orValue() applies (e.g. '_.?field.orValue(\"\")' or '_[?\"field\"].orValue(\"\")'), or remove '.orValue(...)' and supply the fallback another way (e.g. a ternary or a separate resolver source).",
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
	"non-validation-provider": {
		Rule:        "non-validation-provider",
		Severity:    string(SeverityWarning),
		Category:    "provider",
		Description: "A validate.with step uses a provider that does not declare the validation capability.",
		Why:         "Providers without the validation capability will not produce validation results. The validate phase expects providers that return valid/invalid status. Using other providers here is almost certainly a mistake.",
		Fix:         "Use the 'validation' provider (for CEL expressions) or another provider that declares the validation capability.",
		Examples: []string{
			"validate:\n  with:\n    - provider: validation\n      inputs:\n        expression: size(__self) > 0",
		},
	},
	"resolve-foreach": {
		Rule:        "resolve-foreach",
		Severity:    string(SeverityWarning),
		Category:    "structure",
		Description: "A resolve.with step uses forEach, which is not supported in the resolve phase.",
		Why:         "During the resolve phase, __self is not yet available because the resolver is still obtaining its initial value. forEach requires an iterable input, and the resolve phase cannot provide one from the current resolver. Use forEach in the transform phase instead.",
		Fix:         "Move the forEach clause to a transform step, or use a separate resolver to iterate over an array.",
		Examples: []string{
			"# Wrong (forEach in resolve):\nresolve:\n  with:\n    - provider: http\n      forEach:\n        in:\n          expr: items\n\n# Correct (forEach in transform):\ntransform:\n  with:\n    - provider: cel\n      forEach:\n        in:\n          expr: __self\n        item: item",
		},
	},
	"empty-validate-with": {
		Rule:        "empty-validate-with",
		Severity:    string(SeverityWarning),
		Category:    "structure",
		Description: "A resolver has a validate phase with an empty 'with' array. No validations will be applied.",
		Why:         "An empty validate.with array is almost certainly unintentional. The validate phase will be silently skipped, which may cause unexpected behavior.",
		Fix:         "Either add validation rules to the with array or remove the validate section entirely.",
	},
	"missing-fallback-source": {
		Rule:        "missing-fallback-source",
		Severity:    string(SeverityWarning),
		Category:    "structure",
		Description: "All sources in a resolver's resolve.with list have 'when' conditions with no unconditional fallback.",
		Why:         "If every source has a 'when' condition and none of the conditions are met at runtime, the resolver will fail with 'no sources produced a value'. Adding an unconditional fallback (e.g. a static provider) ensures the resolver always produces a value.",
		Fix:         "Add a source without a 'when' condition as the last entry in resolve.with. A static provider with a sensible default is a common pattern.",
		Examples: []string{
			"# Problem: all sources are conditional\nresolve:\n  with:\n    - provider: http\n      when:\n        expr: '_.authenticated == true'\n    - provider: http\n      when:\n        expr: '_.fallbackEnabled == true'\n\n# Fix: add unconditional fallback\nresolve:\n  with:\n    - provider: http\n      when:\n        expr: '_.authenticated == true'\n    - provider: static\n      inputs:\n        value: default-value",
		},
	},
	"parameter-missing-default": {
		Rule:        "parameter-missing-default",
		Severity:    string(SeverityWarning),
		Category:    "structure",
		Description: "A resolver depends on an unconditional 'parameter' source that has no 'default', and there is no other unconditional source guaranteed to produce a value.",
		Why:         "The 'parameter' provider fails at runtime with 'parameter not provided' when the CLI parameter is absent and no 'default' is set. Such a resolver passes lint but fails the moment it runs without the parameter. Adding a 'default' (or an unconditional fallback source) guarantees the resolver always produces a value.",
		Fix:         "Add a 'default' input to the parameter source, or add an unconditional fallback source (e.g. a static provider) after it in resolve.with.",
		Examples: []string{
			"# Problem: parameter source with no default and no fallback\nresolve:\n  with:\n    - provider: parameter\n      inputs:\n        key: environment\n\n# Fix: add a default\nresolve:\n  with:\n    - provider: parameter\n      inputs:\n        key: environment\n        default: development",
		},
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
		Severity:    string(SeverityInfo),
		Category:    "template",
		Description: "A Go template uses '{{ ._.resolverName }}' — the underscore alias works but direct access '{{ .resolverName }}' is shorter.",
		Why:         "Go templates spread resolver data at the top level, so resolvers can be accessed directly with '{{ .resolverName }}'. The '._' alias is injected at runtime for CEL/template parity ('_.resolverName' in CEL, '._.resolverName' in templates). Both forms work, but direct access is preferred for brevity.",
		Fix:         "Replace '{{ ._.resolverName }}' with '{{ .resolverName }}'. Both work, but direct access is shorter.",
		Examples: []string{
			"# Preferred:\ntmpl: \"Deploying {{ .config.appName }}\"\n\n# Also works (via underscore alias):\ntmpl: \"Deploying {{ ._.config.appName }}\"",
		},
	},
	"template-unknown-accessor": {
		Rule:        "template-unknown-accessor",
		Severity:    string(SeverityWarning),
		Category:    "template",
		Description: "A resolve/transform Go template references a root-level accessor ('{{ .field }}' or '{{ ._.name }}') that is not a known resolver, data-input key, or forEach alias.",
		Why:         "Go templates render an unknown root accessor as empty (missingkey default) instead of failing, so a typo silently produces blank output. The template root namespace is the union of resolver values, the step's data-input keys, and any forEach item/index aliases.",
		Fix:         "Fix the typo, add a resolver (or dependsOn) with that name, or provide the value via a data input. Use '{{ ._.name }}' to force a resolver reference.",
		Examples: []string{
			"# Wrong — 'projcts' is a typo (no such resolver/data key):\nprovider: go-template\ninputs:\n  template:\n    tmpl: \"{{ range .projcts }}{{ .name }}{{ end }}\"\n\n# Correct — reference the real resolver 'projects':\nprovider: go-template\ninputs:\n  template:\n    tmpl: \"{{ range .projects }}{{ .name }}{{ end }}\"",
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
		Fix:         "Use a registered provider with CapabilityState such as 'file' or 'http'. External providers like 'github' require an installed plugin.",
	},
	"immutable-requires-state": {
		Rule:        "immutable-requires-state",
		Severity:    string(SeverityError),
		Category:    "state",
		Description: "A resolver has immutable: true but the solution has no state block configured.",
		Why:         "Without a state block and backend provider, there is nowhere to persist the locked resolver value. The immutable flag has no effect.",
		Fix:         "Add a state block with a backend provider to the solution so that the resolver value can be persisted.",
		Examples: []string{
			"state:\n  enabled: true\n  backend:\n    provider: file\n    inputs:\n      path: \"my-state.json\"\n\nspec:\n  resolvers:\n    cluster_id:\n      type: string\n      immutable: true\n      resolve:\n        with:\n          - provider: parameter\n            inputs:\n              key: \"cluster_id\"\n          - provider: exec\n            inputs:\n              command: \"uuidgen\"",
		},
	},
	"state-resolver-ref": {
		Rule:        "state-resolver-ref",
		Severity:    string(SeverityError),
		Category:    "state",
		Description: "A state.enabled or state.backend.inputs field uses a direct rslvr: reference. State is loaded before resolvers run, so resolver results are not available.",
		Why:         "State configuration is resolved before resolver execution using only CLI parameters (-r flags) and environment data. Direct rslvr: references will fail at runtime because resolver results do not exist yet.",
		Fix:         "Use a CEL expression referencing CLI parameters instead, e.g.:\n  path:\n    expr: \"__params.appName + '-state.json'\"\nwhere appName is passed via -r appName=myapp.",
	},
	"state-save-override-state-ref": {
		Rule:        "state-save-override-state-ref",
		Severity:    string(SeverityError),
		Category:    "state",
		Description: "A state.backend.saveOverrides field references the 'state' provider, creating a circular dependency.",
		Why:         "The state provider reads from the state being saved, so referencing it in saveOverrides would create a circular dependency. Use resolver references or CEL expressions instead.",
		Fix:         "Use a resolver reference (rslvr:) or CEL expression (expr:) that does not depend on the state provider.",
	},
	"state-github-no-save-branch": {
		Rule:        "state-github-no-save-branch",
		Severity:    string(SeverityInfo),
		Category:    "state",
		Description: "GitHub state backend has no save branch configured. For PR workflows, use saveOverrides to set the branch at save time.",
		Why:         "Without a save-specific branch, state is saved to the same branch used for loading (typically main). For PR-based workflows, you likely want to save state to a feature branch derived from a resolver.",
		Fix:         "Add a saveOverrides.branch field referencing a resolver:\n  saveOverrides:\n    branch: { rslvr: featureBranch }\nThis ensures state is saved to the same branch as your scaffolded files.",
	},
	"unused-suppression": {
		Rule:        "unused-suppression",
		Severity:    string(SeverityInfo),
		Category:    "suppression",
		Description: "A suppression directive (scafctl-lint-ignore/disable) did not match any lint finding.",
		Why:         "Unused suppressions indicate stale comments left after fixing an issue, or misspelled rule names. Removing them keeps the codebase clean and prevents masking future issues.",
		Fix:         "Remove the suppression comment or fix the rule name. Check for typos in the rule name after the colon.",
		Examples: []string{
			"# Line suppression:\n# scafctl-lint-ignore: exec-command-injection\n\n# Block suppression:\n# scafctl-lint-disable: exec-command-injection\n...\n# scafctl-lint-enable: exec-command-injection\n\n# File-level suppression:\n# scafctl-lint-disable-file: exec-command-injection",
		},
	},
	"explicit-on-finally": {
		Rule:        "explicit-on-finally",
		Severity:    string(SeverityWarning),
		Category:    "workflow",
		Description: "A finally action has explicit: true, which has no effect because finally actions always run.",
		Why:         "Finally actions execute unconditionally after all regular actions complete. Setting explicit: true is misleading because the action cannot be excluded from execution.",
		Fix:         "Remove explicit: true from the finally action, or move the action to workflow.actions if it should be opt-in.",
	},
	"redundant-depends-on": {
		Rule:        "redundant-depends-on",
		Severity:    string(SeverityInfo),
		Category:    "dependency",
		Description: "A resolver's dependsOn entries are already inferred from value references (expr:, rslvr:, tmpl:).",
		Why:         "dependsOn is automatically inferred from value references in resolver inputs, when clauses, and expressions. Explicit entries that duplicate inferred dependencies add noise without changing behavior.",
		Fix:         "Remove redundant dependsOn entries. Only use explicit dependsOn when a resolver must run after another without referencing its value.",
		Examples: []string{
			"# Redundant (inferred from expr):\nimageRef:\n  dependsOn: [registry, namespace]\n  resolve:\n    with:\n      - provider: cel\n        inputs:\n          expression:\n            expr: _.registry + \"/\" + _.namespace\n\n# Correct (no explicit dependsOn needed):\nimageRef:\n  resolve:\n    with:\n      - provider: cel\n        inputs:\n          expression:\n            expr: _.registry + \"/\" + _.namespace",
		},
	},
	"transform-shape-mismatch": {
		Rule:        "transform-shape-mismatch",
		Severity:    string(SeverityWarning),
		Category:    "provider",
		Description: "A transform step accesses provider-specific fields (e.g., __self.body) but the resolve chain includes a fallback that produces a different shape.",
		Why:         "When a resolve chain has both a provider returning a structured object (e.g., http returns {statusCode, body, headers}) and a static fallback returning a scalar or array, transform steps accessing provider-specific fields will fail at runtime when the fallback path is taken.",
		Fix:         "Add a 'when' condition to the transform step to guard against the shape mismatch, e.g.:\n  when:\n    expr: 'type(__self) == map_type && has(__self.body)'",
		Examples: []string{
			"# Problem: transform assumes http shape but static fallback is []\nresolve:\n  with:\n    - provider: http\n      when:\n        expr: 'has(_.credentials)'\n      inputs:\n        url: https://api.example.com/data\n    - provider: static\n      inputs:\n        value: []\ntransform:\n  with:\n    - provider: cel\n      inputs:\n        expression: '__self.body.items'\n\n# Fix: add a when guard on the transform step\ntransform:\n  with:\n    - provider: cel\n      when:\n        expr: 'type(__self) != list_type'\n      inputs:\n        expression: '__self.body.items'",
		},
	},
	"fingerprint-without-sources": {
		Rule:        "fingerprint-without-sources",
		Severity:    string(SeverityWarning),
		Category:    "workflow",
		Description: "An action has a fingerprint block but no sources defined. Fingerprinting requires sources to function.",
		Why:         "The fingerprint system hashes source files to determine whether an action is up-to-date. Without sources, there are no files to hash and the fingerprint block has no effect.",
		Fix:         "Either add sources patterns to the action or remove the fingerprint block.",
		Examples: []string{
			"# Correct:\nactions:\n  build:\n    sources:\n      - 'src/**/*.go'\n    fingerprint:\n      scope: files\n    provider: exec\n    inputs:\n      command: go build",
		},
	},
	"missing-template-dependency": {
		Rule:        "missing-template-dependency",
		Severity:    string(SeverityWarning),
		Category:    "dependency",
		Description: "A go-template render-tree resolver reads external template files that reference resolvers not listed in its dependsOn and not otherwise reachable in its dependency graph.",
		Why:         "When a go-template render-tree step renders external template files, the DAG cannot auto-detect which resolver names are used inside those files. Discovery is role-based, not extension-based: any file reached via bundle.include or a directory provider is treated as a go-template source (e.g. .tpl, .tmpl, .gotmpl, but also .tf, .yaml, or any other extension), so references inside non-standard extensions are still detected. Missing dependencies cause 'map has no entry for key' runtime errors because the template executes before its data dependencies are resolved.",
		Fix:         "Add the missing resolver names to the dependsOn list of the render-tree resolver, or of the resolver that consumes the rendered output.",
		Examples: []string{
			"# Template file (templates/main.tf) uses {{ .appName }} and {{ .region }}\n# Ensure both are in dependsOn:\nresolvers:\n  rendered:\n    dependsOn: [appName, region, templateSource]\n    resolve:\n      with:\n        - provider: go-template\n          inputs:\n            operation: render-tree\n            entries:\n              rslvr: templateSource\n            data:\n              rslvr: _",
		},
	},
	"resolver-cycle": {
		Rule:        "resolver-cycle",
		Severity:    string(SeverityError),
		Category:    "dependency",
		Description: "The resolver dependency graph contains a circular dependency.",
		Why:         "Circular dependencies between resolvers create infinite loops that prevent the DAG from being scheduled. The dependency graph must be acyclic.",
		Fix:         "Break the cycle by reordering dependencies, removing unnecessary references, or extracting cycle-causing logic into a separate resolver. When a validate block causes the cycle, extract the validation into a dedicated resolver that runs after all its dependencies.",
		Examples: []string{
			"# Problem: validate block creates a cycle\n# resolverA validate needs resolverB, but resolverB depends on resolverA\n\n# Fix: extract the validation into a separate resolver\nresolvers:\n  resolverA:\n    resolve:\n      with:\n        - provider: parameter\n          inputs:\n            key: inputA\n  validateA:\n    dependsOn: [resolverA, resolverB]\n    validate:\n      with:\n        - provider: validation\n          inputs:\n            expression:\n              expr: '_.resolverB.valid == true'\n            message: 'Validation failed'",
		},
	},
	"invalid-fingerprint-scope": {
		Rule:        "invalid-fingerprint-scope",
		Severity:    string(SeverityError),
		Category:    "workflow",
		Description: "The fingerprint.scope value is not a recognized option.",
		Why:         "An unrecognized scope silently defaults to 'all', which may not be the intended behavior. Valid values are 'all' and 'files'.",
		Fix:         "Set fingerprint.scope to 'all' (check both files and inputs) or 'files' (check files only).",
		Examples: []string{
			"# Correct:\nactions:\n  build:\n    sources:\n      - 'src/**/*.go'\n    fingerprint:\n      scope: files\n    provider: exec\n    inputs:\n      command: go build",
		},
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
