// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package solution

import (
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateResolverName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid simple name",
			input:   "environment",
			wantErr: false,
		},
		{
			name:    "valid name with hyphen",
			input:   "my-resolver",
			wantErr: false,
		},
		{
			name:    "valid name with underscore",
			input:   "my_resolver",
			wantErr: false,
		},
		{
			name:    "valid name with numbers",
			input:   "resolver123",
			wantErr: false,
		},
		{
			name:    "valid single underscore prefix",
			input:   "_internal",
			wantErr: false,
		},
		{
			name:    "empty name",
			input:   "",
			wantErr: true,
			errMsg:  "cannot be empty",
		},
		{
			name:    "double underscore prefix",
			input:   "__reserved",
			wantErr: true,
			errMsg:  "cannot start with '__'",
		},
		{
			name:    "triple underscore prefix",
			input:   "___also_reserved",
			wantErr: true,
			errMsg:  "cannot start with '__'",
		},
		{
			name:    "contains space",
			input:   "my resolver",
			wantErr: true,
			errMsg:  "cannot contain whitespace",
		},
		{
			name:    "contains tab",
			input:   "my\tresolver",
			wantErr: true,
			errMsg:  "cannot contain whitespace",
		},
		{
			name:    "contains newline",
			input:   "my\nresolver",
			wantErr: true,
			errMsg:  "cannot contain whitespace",
		},
		{
			name:    "only double underscore",
			input:   "__",
			wantErr: true,
			errMsg:  "cannot start with '__'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateResolverName(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestSolution_ValidateSpec(t *testing.T) {
	tests := []struct {
		name    string
		sol     *Solution
		wantErr bool
		errMsg  string
	}{
		{
			name: "nil solution",
			sol:  nil,
		},
		{
			name: "solution without spec",
			sol: &Solution{
				Spec: Spec{},
			},
		},
		{
			name: "solution with valid resolvers",
			sol: &Solution{
				Spec: Spec{
					Resolvers: map[string]*resolver.Resolver{
						"environment": {Type: resolver.TypeString},
						"region":      {Type: resolver.TypeString},
					},
				},
			},
		},
		{
			name: "solution with invalid resolver name (double underscore)",
			sol: &Solution{
				Spec: Spec{
					Resolvers: map[string]*resolver.Resolver{
						"__internal": {Type: resolver.TypeString},
					},
				},
			},
			wantErr: true,
			errMsg:  "cannot start with '__'",
		},
		{
			name: "solution with invalid resolver name (whitespace)",
			sol: &Solution{
				Spec: Spec{
					Resolvers: map[string]*resolver.Resolver{
						"my resolver": {Type: resolver.TypeString},
					},
				},
			},
			wantErr: true,
			errMsg:  "cannot contain whitespace",
		},
		{
			name: "solution with multiple invalid resolver names",
			sol: &Solution{
				Spec: Spec{
					Resolvers: map[string]*resolver.Resolver{
						"__reserved": {Type: resolver.TypeString},
						"with space": {Type: resolver.TypeString},
						"valid_name": {Type: resolver.TypeString},
						"__also_bad": {Type: resolver.TypeString},
					},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.sol.ValidateSpec()
			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestSolution_ValidateSpec_CircularDependency(t *testing.T) {
	// Create a CEL expression that references another resolver
	exprA := celexp.Expression(`_.b`)
	exprB := celexp.Expression(`_.a`)

	sol := &Solution{
		Spec: Spec{
			Resolvers: map[string]*resolver.Resolver{
				"a": {
					Type: resolver.TypeString,
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{
							{
								Provider: "cel",
								Inputs: map[string]*resolver.ValueRef{
									"expression": {Expr: &exprA},
								},
							},
						},
					},
				},
				"b": {
					Type: resolver.TypeString,
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{
							{
								Provider: "cel",
								Inputs: map[string]*resolver.ValueRef{
									"expression": {Expr: &exprB},
								},
							},
						},
					},
				},
			},
		},
	}

	err := sol.ValidateSpec()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resolver dependency error")
}

func TestSolution_ValidateSpec_DependsOn(t *testing.T) {
	tests := []struct {
		name    string
		sol     *Solution
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid dependsOn reference",
			sol: &Solution{
				Spec: Spec{
					Resolvers: map[string]*resolver.Resolver{
						"config": {Type: resolver.TypeString},
						"app": {
							Type:      resolver.TypeString,
							DependsOn: []string{"config"},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "multiple valid dependsOn references",
			sol: &Solution{
				Spec: Spec{
					Resolvers: map[string]*resolver.Resolver{
						"config":      {Type: resolver.TypeString},
						"credentials": {Type: resolver.TypeString},
						"app": {
							Type:      resolver.TypeString,
							DependsOn: []string{"config", "credentials"},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "dependsOn references non-existent resolver",
			sol: &Solution{
				Spec: Spec{
					Resolvers: map[string]*resolver.Resolver{
						"app": {
							Type:      resolver.TypeString,
							DependsOn: []string{"nonexistent"},
						},
					},
				},
			},
			wantErr: true,
			errMsg:  "non-existent resolver \"nonexistent\"",
		},
		{
			name: "dependsOn with empty entry",
			sol: &Solution{
				Spec: Spec{
					Resolvers: map[string]*resolver.Resolver{
						"config": {Type: resolver.TypeString},
						"app": {
							Type:      resolver.TypeString,
							DependsOn: []string{"config", ""},
						},
					},
				},
			},
			wantErr: true,
			errMsg:  "empty dependsOn entry",
		},
		{
			name: "dependsOn self-reference",
			sol: &Solution{
				Spec: Spec{
					Resolvers: map[string]*resolver.Resolver{
						"app": {
							Type:      resolver.TypeString,
							DependsOn: []string{"app"},
						},
					},
				},
			},
			wantErr: true,
			errMsg:  "cannot depend on itself",
		},
		{
			name: "circular dependency via dependsOn",
			sol: &Solution{
				Spec: Spec{
					Resolvers: map[string]*resolver.Resolver{
						"a": {
							Type:      resolver.TypeString,
							DependsOn: []string{"b"},
						},
						"b": {
							Type:      resolver.TypeString,
							DependsOn: []string{"a"},
						},
					},
				},
			},
			wantErr: true,
			errMsg:  "resolver dependency error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.sol.ValidateSpec()
			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestSolution_Validate_Integration(t *testing.T) {
	tests := []struct {
		name    string
		sol     *Solution
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid solution with spec",
			sol: &Solution{
				APIVersion: DefaultAPIVersion,
				Kind:       SolutionKind,
				Metadata: Metadata{
					Name:    "test-solution",
					Version: testSemver("1.0.0"),
				},
				Spec: Spec{
					Resolvers: map[string]*resolver.Resolver{
						"environment": {Type: resolver.TypeString},
					},
				},
			},
		},
		{
			name: "valid solution without spec",
			sol: &Solution{
				APIVersion: DefaultAPIVersion,
				Kind:       SolutionKind,
				Metadata: Metadata{
					Name:    "minimal-solution",
					Version: testSemver("2.0.0"),
				},
			},
		},
		{
			name: "invalid envelope fails before spec validation",
			sol: &Solution{
				APIVersion: "wrong/version",
				Kind:       SolutionKind,
				Metadata: Metadata{
					Name:    "test-solution",
					Version: testSemver("0.1.0"),
				},
			},
			wantErr: true,
			errMsg:  "apiVersion must be",
		},
		{
			name: "valid envelope but invalid spec",
			sol: &Solution{
				APIVersion: DefaultAPIVersion,
				Kind:       SolutionKind,
				Metadata: Metadata{
					Name:    "test-solution",
					Version: testSemver("1.0.0"),
				},
				Spec: Spec{
					Resolvers: map[string]*resolver.Resolver{
						"__invalid": {Type: resolver.TypeString},
					},
				},
			},
			wantErr: true,
			errMsg:  "cannot start with '__'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.sol.Validate()
			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestSolution_LoadFromBytes_WithSpec(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid solution with resolvers",
			yaml: `
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: test-solution
  version: 1.0.0
spec:
  resolvers:
    environment:
      type: string
      resolve:
        with:
          - provider: parameter
            inputs:
              key: "env"
`,
			wantErr: false,
		},
		{
			name: "invalid resolver name in YAML",
			yaml: `
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: test-solution
  version: 1.0.0
spec:
  resolvers:
    __internal:
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value: "test"
`,
			wantErr: true,
			errMsg:  "cannot start with '__'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var sol Solution
			err := sol.LoadFromBytes([]byte(tt.yaml))
			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func testSemver(version string) *semver.Version {
	ver, err := semver.NewVersion(version)
	if err != nil {
		panic(err)
	}
	return ver
}
