// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package spec

// Usage holds optional, author-curated documentation describing how to consume a
// solution. It enriches the auto-generated usage view surfaced by
// `inspect solution --usage`; when absent, the view is generated entirely from
// the solution's parameters and actions.
type Usage struct {
	// Synopsis is a one-line description of how to consume the solution.
	// When set it takes precedence over the metadata description in the usage view.
	Synopsis string `json:"synopsis,omitempty" yaml:"synopsis,omitempty" doc:"One-line description of how to consume the solution" maxLength:"500" example:"Registry index for Terraform modules and providers"`

	// Details is free-form, multi-paragraph prose describing how the solution
	// works and when to use it -- the long-form counterpart to the one-line
	// description/synopsis (analogous to a command's long help or a README body).
	Details string `json:"details,omitempty" yaml:"details,omitempty" doc:"Long-form description of how the solution works and when to use it" maxLength:"20000"`

	// Examples are curated command examples shown in the usage view.
	Examples []UsageExample `json:"examples,omitempty" yaml:"examples,omitempty" doc:"Curated usage examples" maxItems:"50"`
}

// UsageExample is a single curated example: a human description paired with the
// CLI command that performs it.
type UsageExample struct {
	// Description explains what the example does.
	Description string `json:"description" yaml:"description" doc:"What the example does" minLength:"1" maxLength:"500" example:"Refresh the registry from live APIs"`

	// Command is the CLI invocation to run.
	Command string `json:"command" yaml:"command" doc:"CLI command to run" minLength:"1" maxLength:"2048" example:"scafctl run solution -r action=refresh"`
}
