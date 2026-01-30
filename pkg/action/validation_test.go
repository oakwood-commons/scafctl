package action

import (
	"context"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/gotmpl"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockProvider implements provider.Provider for testing.
type mockProvider struct {
	desc *provider.Descriptor
}

func (m *mockProvider) Descriptor() *provider.Descriptor {
	return m.desc
}

func (m *mockProvider) Execute(_ context.Context, _ any) (*provider.Output, error) {
	return &provider.Output{Data: map[string]any{}}, nil
}

// mockRegistry implements RegistryInterface for testing.
type mockRegistry struct {
	providers map[string]provider.Provider
}

func newMockRegistry() *mockRegistry {
	return &mockRegistry{
		providers: make(map[string]provider.Provider),
	}
}

func (m *mockRegistry) Get(name string) (provider.Provider, bool) {
	p, ok := m.providers[name]
	return p, ok
}

func (m *mockRegistry) Has(name string) bool {
	_, ok := m.providers[name]
	return ok
}

func (m *mockRegistry) Register(name string, p provider.Provider) {
	m.providers[name] = p
}

// Helper to create a mock provider with action capability.
func mockActionProvider(name string) *mockProvider {
	return &mockProvider{
		desc: &provider.Descriptor{
			Name:         name,
			APIVersion:   "v1",
			Version:      semver.MustParse("1.0.0"),
			Description:  "Mock provider for testing",
			MockBehavior: "Returns mock data",
			Capabilities: []provider.Capability{provider.CapabilityAction},
			Schema:       provider.SchemaDefinition{},
			OutputSchemas: map[provider.Capability]provider.SchemaDefinition{
				provider.CapabilityAction: {},
			},
		},
	}
}

// Helper to create a mock provider without action capability.
func mockNonActionProvider(name string) *mockProvider {
	return &mockProvider{
		desc: &provider.Descriptor{
			Name:         name,
			APIVersion:   "v1",
			Version:      semver.MustParse("1.0.0"),
			Description:  "Mock provider for testing",
			MockBehavior: "Returns mock data",
			Capabilities: []provider.Capability{provider.CapabilityFrom},
			Schema:       provider.SchemaDefinition{},
			OutputSchemas: map[provider.Capability]provider.SchemaDefinition{
				provider.CapabilityFrom: {},
			},
		},
	}
}

// Helper to create a CEL expression pointer.
func celExpr(s string) *celexp.Expression {
	e := celexp.Expression(s)
	return &e
}

// Helper to create a Go template pointer.
func goTmpl(s string) *gotmpl.GoTemplatingContent {
	t := gotmpl.GoTemplatingContent(s)
	return &t
}

func TestValidationError_Error(t *testing.T) {
	tests := []struct {
		name     string
		err      *ValidationError
		expected string
	}{
		{
			name: "full context",
			err: &ValidationError{
				Section:    "actions",
				ActionName: "deploy",
				Field:      "provider",
				Message:    "provider not found",
			},
			expected: "actions.deploy.provider: provider not found",
		},
		{
			name: "section and action only",
			err: &ValidationError{
				Section:    "finally",
				ActionName: "cleanup",
				Message:    "action definition cannot be nil",
			},
			expected: "finally.cleanup: action definition cannot be nil",
		},
		{
			name: "section only",
			err: &ValidationError{
				Section: "actions",
				Field:   "dependsOn",
				Message: "circular dependency detected",
			},
			expected: "actions.dependsOn: circular dependency detected",
		},
		{
			name: "message only",
			err: &ValidationError{
				Message: "workflow cannot be nil",
			},
			expected: "workflow cannot be nil",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.err.Error())
		})
	}
}

func TestAggregatedValidationError(t *testing.T) {
	t.Run("no errors", func(t *testing.T) {
		errs := &AggregatedValidationError{}
		assert.False(t, errs.HasErrors())
		assert.Equal(t, "validation failed (no details)", errs.Error())
		assert.Nil(t, errs.ToError())
	})

	t.Run("single error", func(t *testing.T) {
		errs := &AggregatedValidationError{}
		errs.AddError(&ValidationError{
			Section:    "actions",
			ActionName: "deploy",
			Message:    "provider is required",
		})
		assert.True(t, errs.HasErrors())
		assert.Equal(t, "actions.deploy: provider is required", errs.Error())
		assert.NotNil(t, errs.ToError())
	})

	t.Run("multiple errors", func(t *testing.T) {
		errs := &AggregatedValidationError{}
		errs.AddError(&ValidationError{Section: "actions", ActionName: "a", Message: "error 1"})
		errs.AddError(&ValidationError{Section: "actions", ActionName: "b", Message: "error 2"})
		assert.True(t, errs.HasErrors())
		assert.Contains(t, errs.Error(), "2 errors")
		assert.Contains(t, errs.Error(), "error 1")
		assert.Contains(t, errs.Error(), "error 2")
	})
}

func TestValidateWorkflow_NilWorkflow(t *testing.T) {
	err := ValidateWorkflow(nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "workflow cannot be nil")
}

func TestValidateWorkflow_EmptyWorkflow(t *testing.T) {
	w := &Workflow{}
	err := ValidateWorkflow(w, nil)
	assert.NoError(t, err)
}

func TestValidateWorkflow_ActionNameRules(t *testing.T) {
	tests := []struct {
		name        string
		actionName  string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "valid simple name",
			actionName:  "deploy",
			expectError: false,
		},
		{
			name:        "valid name with underscore",
			actionName:  "deploy_app",
			expectError: false,
		},
		{
			name:        "valid name with hyphen",
			actionName:  "deploy-app",
			expectError: false,
		},
		{
			name:        "valid name starting with underscore",
			actionName:  "_private",
			expectError: false,
		},
		{
			name:        "valid name with numbers",
			actionName:  "deploy2",
			expectError: false,
		},
		{
			name:        "invalid - starts with number",
			actionName:  "2deploy",
			expectError: true,
			errorMsg:    "must match pattern",
		},
		{
			name:        "invalid - reserved prefix __",
			actionName:  "__reserved",
			expectError: true,
			errorMsg:    "reserved",
		},
		{
			name:        "invalid - contains [",
			actionName:  "deploy[0]",
			expectError: true,
			errorMsg:    "reserved for forEach",
		},
		{
			name:        "invalid - contains ]",
			actionName:  "deploy]",
			expectError: true,
			errorMsg:    "reserved for forEach",
		},
		{
			name:        "invalid - starts with hyphen",
			actionName:  "-deploy",
			expectError: true,
			errorMsg:    "must match pattern",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := &Workflow{
				Actions: map[string]*Action{
					tt.actionName: {Provider: "shell"},
				},
			}
			err := ValidateWorkflow(w, nil)
			if tt.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateWorkflow_ActionNameUniqueness(t *testing.T) {
	t.Run("duplicate in same section", func(t *testing.T) {
		// This case can't happen with maps (keys are unique)
		// But we test cross-section uniqueness
	})

	t.Run("duplicate across sections", func(t *testing.T) {
		w := &Workflow{
			Actions: map[string]*Action{
				"shared": {Provider: "shell"},
			},
			Finally: map[string]*Action{
				"shared": {Provider: "shell"},
			},
		}
		err := ValidateWorkflow(w, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "already defined")
	})
}

func TestValidateWorkflow_DependsOnRules(t *testing.T) {
	t.Run("valid dependency in actions", func(t *testing.T) {
		w := &Workflow{
			Actions: map[string]*Action{
				"build":  {Provider: "shell"},
				"deploy": {Provider: "shell", DependsOn: []string{"build"}},
			},
		}
		err := ValidateWorkflow(w, nil)
		assert.NoError(t, err)
	})

	t.Run("invalid dependency - not found", func(t *testing.T) {
		w := &Workflow{
			Actions: map[string]*Action{
				"deploy": {Provider: "shell", DependsOn: []string{"nonexistent"}},
			},
		}
		err := ValidateWorkflow(w, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("self dependency", func(t *testing.T) {
		w := &Workflow{
			Actions: map[string]*Action{
				"deploy": {Provider: "shell", DependsOn: []string{"deploy"}},
			},
		}
		err := ValidateWorkflow(w, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot depend on itself")
	})

	t.Run("finally cannot depend on actions", func(t *testing.T) {
		w := &Workflow{
			Actions: map[string]*Action{
				"build": {Provider: "shell"},
			},
			Finally: map[string]*Action{
				"cleanup": {Provider: "shell", DependsOn: []string{"build"}},
			},
		}
		err := ValidateWorkflow(w, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found in finally section")
	})

	t.Run("valid dependency in finally", func(t *testing.T) {
		w := &Workflow{
			Finally: map[string]*Action{
				"cleanup1": {Provider: "shell"},
				"cleanup2": {Provider: "shell", DependsOn: []string{"cleanup1"}},
			},
		}
		err := ValidateWorkflow(w, nil)
		assert.NoError(t, err)
	})
}

func TestValidateWorkflow_CycleDetection(t *testing.T) {
	t.Run("simple cycle", func(t *testing.T) {
		w := &Workflow{
			Actions: map[string]*Action{
				"a": {Provider: "shell", DependsOn: []string{"b"}},
				"b": {Provider: "shell", DependsOn: []string{"a"}},
			},
		}
		err := ValidateWorkflow(w, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "circular dependency")
	})

	t.Run("longer cycle", func(t *testing.T) {
		w := &Workflow{
			Actions: map[string]*Action{
				"a": {Provider: "shell", DependsOn: []string{"c"}},
				"b": {Provider: "shell", DependsOn: []string{"a"}},
				"c": {Provider: "shell", DependsOn: []string{"b"}},
			},
		}
		err := ValidateWorkflow(w, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "circular dependency")
	})

	t.Run("no cycle - linear", func(t *testing.T) {
		w := &Workflow{
			Actions: map[string]*Action{
				"a": {Provider: "shell"},
				"b": {Provider: "shell", DependsOn: []string{"a"}},
				"c": {Provider: "shell", DependsOn: []string{"b"}},
			},
		}
		err := ValidateWorkflow(w, nil)
		assert.NoError(t, err)
	})

	t.Run("no cycle - diamond", func(t *testing.T) {
		w := &Workflow{
			Actions: map[string]*Action{
				"a": {Provider: "shell"},
				"b": {Provider: "shell", DependsOn: []string{"a"}},
				"c": {Provider: "shell", DependsOn: []string{"a"}},
				"d": {Provider: "shell", DependsOn: []string{"b", "c"}},
			},
		}
		err := ValidateWorkflow(w, nil)
		assert.NoError(t, err)
	})
}

func TestValidateWorkflow_ProviderValidation(t *testing.T) {
	t.Run("missing provider", func(t *testing.T) {
		w := &Workflow{
			Actions: map[string]*Action{
				"deploy": {},
			},
		}
		err := ValidateWorkflow(w, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "provider is required")
	})

	t.Run("provider not found", func(t *testing.T) {
		reg := newMockRegistry()
		w := &Workflow{
			Actions: map[string]*Action{
				"deploy": {Provider: "nonexistent"},
			},
		}
		err := ValidateWorkflow(w, reg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "provider \"nonexistent\" not found")
	})

	t.Run("provider without action capability", func(t *testing.T) {
		reg := newMockRegistry()
		reg.Register("http", mockNonActionProvider("http"))

		w := &Workflow{
			Actions: map[string]*Action{
				"deploy": {Provider: "http"},
			},
		}
		err := ValidateWorkflow(w, reg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "does not have action capability")
	})

	t.Run("provider with action capability", func(t *testing.T) {
		reg := newMockRegistry()
		reg.Register("shell", mockActionProvider("shell"))

		w := &Workflow{
			Actions: map[string]*Action{
				"deploy": {Provider: "shell"},
			},
		}
		err := ValidateWorkflow(w, reg)
		assert.NoError(t, err)
	})

	t.Run("skip validation with nil registry", func(t *testing.T) {
		w := &Workflow{
			Actions: map[string]*Action{
				"deploy": {Provider: "shell"},
			},
		}
		err := ValidateWorkflow(w, nil)
		assert.NoError(t, err)
	})
}

func TestValidateWorkflow_ActionsReferences(t *testing.T) {
	t.Run("valid __actions reference with dependsOn", func(t *testing.T) {
		w := &Workflow{
			Actions: map[string]*Action{
				"build": {Provider: "shell"},
				"deploy": {
					Provider:  "shell",
					DependsOn: []string{"build"},
					Inputs: map[string]*spec.ValueRef{
						"artifact": {Expr: celExpr("__actions.build.results.path")},
					},
				},
			},
		}
		err := ValidateWorkflow(w, nil)
		assert.NoError(t, err)
	})

	t.Run("__actions reference without dependsOn", func(t *testing.T) {
		w := &Workflow{
			Actions: map[string]*Action{
				"build": {Provider: "shell"},
				"deploy": {
					Provider: "shell",
					Inputs: map[string]*spec.ValueRef{
						"artifact": {Expr: celExpr("__actions.build.results.path")},
					},
				},
			},
		}
		err := ValidateWorkflow(w, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "requires it to be listed in dependsOn")
	})

	t.Run("__actions reference to nonexistent action", func(t *testing.T) {
		w := &Workflow{
			Actions: map[string]*Action{
				"deploy": {
					Provider: "shell",
					Inputs: map[string]*spec.ValueRef{
						"artifact": {Expr: celExpr("__actions.nonexistent.results")},
					},
				},
			},
		}
		err := ValidateWorkflow(w, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "action not found")
	})

	t.Run("finally can reference regular actions without dependsOn", func(t *testing.T) {
		w := &Workflow{
			Actions: map[string]*Action{
				"build": {Provider: "shell"},
			},
			Finally: map[string]*Action{
				"cleanup": {
					Provider: "shell",
					Inputs: map[string]*spec.ValueRef{
						"status": {Expr: celExpr("__actions.build.status")},
					},
				},
			},
		}
		err := ValidateWorkflow(w, nil)
		assert.NoError(t, err)
	})

	t.Run("finally __actions reference to finally requires dependsOn", func(t *testing.T) {
		w := &Workflow{
			Finally: map[string]*Action{
				"cleanup1": {Provider: "shell"},
				"cleanup2": {
					Provider: "shell",
					Inputs: map[string]*spec.ValueRef{
						"status": {Expr: celExpr("__actions.cleanup1.status")},
					},
				},
			},
		}
		err := ValidateWorkflow(w, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "requires it to be listed in dependsOn")
	})

	t.Run("template __actions reference", func(t *testing.T) {
		w := &Workflow{
			Actions: map[string]*Action{
				"build": {Provider: "shell"},
				"deploy": {
					Provider: "shell",
					Inputs: map[string]*spec.ValueRef{
						"artifact": {Tmpl: goTmpl("{{ .__actions.build.results.path }}")},
					},
				},
			},
		}
		err := ValidateWorkflow(w, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "requires it to be listed in dependsOn")
	})
}

func TestValidateWorkflow_ForEachValidation(t *testing.T) {
	t.Run("forEach not allowed in finally", func(t *testing.T) {
		w := &Workflow{
			Finally: map[string]*Action{
				"cleanup": {
					Provider: "shell",
					ForEach:  &spec.ForEachClause{},
				},
			},
		}
		err := ValidateWorkflow(w, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "forEach is not allowed in finally")
	})

	t.Run("forEach allowed in actions", func(t *testing.T) {
		w := &Workflow{
			Actions: map[string]*Action{
				"deploy": {
					Provider: "shell",
					ForEach:  &spec.ForEachClause{},
				},
			},
		}
		err := ValidateWorkflow(w, nil)
		assert.NoError(t, err)
	})

	t.Run("forEach invalid onError", func(t *testing.T) {
		w := &Workflow{
			Actions: map[string]*Action{
				"deploy": {
					Provider: "shell",
					ForEach: &spec.ForEachClause{
						OnError: spec.OnErrorBehavior("invalid"),
					},
				},
			},
		}
		err := ValidateWorkflow(w, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "forEach.onError")
	})

	t.Run("forEach valid onError continue", func(t *testing.T) {
		w := &Workflow{
			Actions: map[string]*Action{
				"deploy": {
					Provider: "shell",
					ForEach: &spec.ForEachClause{
						OnError: spec.OnErrorContinue,
					},
				},
			},
		}
		err := ValidateWorkflow(w, nil)
		assert.NoError(t, err)
	})

	t.Run("forEach negative concurrency", func(t *testing.T) {
		w := &Workflow{
			Actions: map[string]*Action{
				"deploy": {
					Provider: "shell",
					ForEach: &spec.ForEachClause{
						Concurrency: -1,
					},
				},
			},
		}
		err := ValidateWorkflow(w, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "forEach.concurrency must be >= 0")
	})

	t.Run("forEach valid concurrency", func(t *testing.T) {
		w := &Workflow{
			Actions: map[string]*Action{
				"deploy": {
					Provider: "shell",
					ForEach: &spec.ForEachClause{
						Concurrency: 5,
					},
				},
			},
		}
		err := ValidateWorkflow(w, nil)
		assert.NoError(t, err)
	})
}

func TestValidateWorkflow_RetryValidation(t *testing.T) {
	t.Run("retry maxAttempts less than 1", func(t *testing.T) {
		w := &Workflow{
			Actions: map[string]*Action{
				"deploy": {
					Provider: "shell",
					Retry: &RetryConfig{
						MaxAttempts: 0,
					},
				},
			},
		}
		err := ValidateWorkflow(w, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "retry.maxAttempts must be >= 1")
	})

	t.Run("retry valid maxAttempts", func(t *testing.T) {
		w := &Workflow{
			Actions: map[string]*Action{
				"deploy": {
					Provider: "shell",
					Retry: &RetryConfig{
						MaxAttempts: 3,
					},
				},
			},
		}
		err := ValidateWorkflow(w, nil)
		assert.NoError(t, err)
	})

	t.Run("retry invalid backoff", func(t *testing.T) {
		w := &Workflow{
			Actions: map[string]*Action{
				"deploy": {
					Provider: "shell",
					Retry: &RetryConfig{
						MaxAttempts: 3,
						Backoff:     BackoffType("invalid"),
					},
				},
			},
		}
		err := ValidateWorkflow(w, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "retry.backoff")
	})

	t.Run("retry valid backoff types", func(t *testing.T) {
		backoffTypes := []BackoffType{BackoffFixed, BackoffLinear, BackoffExponential}
		for _, bt := range backoffTypes {
			w := &Workflow{
				Actions: map[string]*Action{
					"deploy": {
						Provider: "shell",
						Retry: &RetryConfig{
							MaxAttempts: 3,
							Backoff:     bt,
						},
					},
				},
			}
			err := ValidateWorkflow(w, nil)
			assert.NoError(t, err, "backoff type %s should be valid", bt)
		}
	})
}

func TestValidateWorkflow_OnErrorValidation(t *testing.T) {
	t.Run("invalid onError", func(t *testing.T) {
		w := &Workflow{
			Actions: map[string]*Action{
				"deploy": {
					Provider: "shell",
					OnError:  spec.OnErrorBehavior("invalid"),
				},
			},
		}
		err := ValidateWorkflow(w, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "onError must be 'fail' or 'continue'")
	})

	t.Run("valid onError fail", func(t *testing.T) {
		w := &Workflow{
			Actions: map[string]*Action{
				"deploy": {
					Provider: "shell",
					OnError:  spec.OnErrorFail,
				},
			},
		}
		err := ValidateWorkflow(w, nil)
		assert.NoError(t, err)
	})

	t.Run("valid onError continue", func(t *testing.T) {
		w := &Workflow{
			Actions: map[string]*Action{
				"deploy": {
					Provider: "shell",
					OnError:  spec.OnErrorContinue,
				},
			},
		}
		err := ValidateWorkflow(w, nil)
		assert.NoError(t, err)
	})

	t.Run("empty onError is valid (defaults to fail)", func(t *testing.T) {
		w := &Workflow{
			Actions: map[string]*Action{
				"deploy": {
					Provider: "shell",
				},
			},
		}
		err := ValidateWorkflow(w, nil)
		assert.NoError(t, err)
	})
}

func TestValidateWorkflow_NilAction(t *testing.T) {
	w := &Workflow{
		Actions: map[string]*Action{
			"deploy": nil,
		},
	}
	err := ValidateWorkflow(w, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "action definition cannot be nil")
}

func TestParseActionsRefsFromString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "simple dot notation",
			input:    "__actions.build.results",
			expected: []string{"build"},
		},
		{
			name:     "bracket notation double quotes",
			input:    `__actions["build"].results`,
			expected: []string{"build"},
		},
		{
			name:     "bracket notation single quotes",
			input:    `__actions['build'].results`,
			expected: []string{"build"},
		},
		{
			name:     "multiple references",
			input:    "__actions.build.results + __actions.test.status",
			expected: []string{"build", "test"},
		},
		{
			name:     "hyphenated action name",
			input:    "__actions.my-action.results",
			expected: []string{"my-action"},
		},
		{
			name:     "underscore action name",
			input:    "__actions.my_action.results",
			expected: []string{"my_action"},
		},
		{
			name:     "no __actions reference",
			input:    "some.other.path",
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			refs := make(map[string]struct{})
			parseActionsRefsFromString(tt.input, refs)

			result := make([]string, 0, len(refs))
			for ref := range refs {
				result = append(result, ref)
			}

			assert.ElementsMatch(t, tt.expected, result)
		})
	}
}

func TestFindCycle(t *testing.T) {
	tests := []struct {
		name      string
		deps      map[string][]string
		hasCycle  bool
		cycleSize int // minimum expected cycle size
	}{
		{
			name: "no deps",
			deps: map[string][]string{
				"a": {},
				"b": {},
			},
			hasCycle: false,
		},
		{
			name: "linear chain",
			deps: map[string][]string{
				"a": {},
				"b": {"a"},
				"c": {"b"},
			},
			hasCycle: false,
		},
		{
			name: "simple cycle",
			deps: map[string][]string{
				"a": {"b"},
				"b": {"a"},
			},
			hasCycle:  true,
			cycleSize: 2,
		},
		{
			name: "triangle cycle",
			deps: map[string][]string{
				"a": {"c"},
				"b": {"a"},
				"c": {"b"},
			},
			hasCycle:  true,
			cycleSize: 3,
		},
		{
			name: "diamond - no cycle",
			deps: map[string][]string{
				"a": {},
				"b": {"a"},
				"c": {"a"},
				"d": {"b", "c"},
			},
			hasCycle: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cycle := findCycle(tt.deps)
			if tt.hasCycle {
				assert.NotNil(t, cycle)
				assert.GreaterOrEqual(t, len(cycle), tt.cycleSize)
			} else {
				assert.Nil(t, cycle)
			}
		})
	}
}

func TestValidateWorkflow_ComplexScenario(t *testing.T) {
	reg := newMockRegistry()
	reg.Register("shell", mockActionProvider("shell"))
	reg.Register("http", mockActionProvider("http"))

	w := &Workflow{
		Actions: map[string]*Action{
			"build": {
				Provider: "shell",
			},
			"test": {
				Provider:  "shell",
				DependsOn: []string{"build"},
				Retry: &RetryConfig{
					MaxAttempts: 3,
					Backoff:     BackoffExponential,
				},
			},
			"deploy": {
				Provider:  "http",
				DependsOn: []string{"build", "test"},
				Inputs: map[string]*spec.ValueRef{
					"artifact": {Expr: celExpr("__actions.build.results.path")},
					"version":  {Expr: celExpr("__actions.test.results.version")},
				},
				ForEach: &spec.ForEachClause{
					Item:        "region",
					Concurrency: 3,
					OnError:     spec.OnErrorContinue,
				},
			},
		},
		Finally: map[string]*Action{
			"notify": {
				Provider: "http",
				Inputs: map[string]*spec.ValueRef{
					"status": {Expr: celExpr("__actions.deploy.status")},
				},
			},
			"cleanup": {
				Provider:  "shell",
				DependsOn: []string{"notify"},
			},
		},
	}

	err := ValidateWorkflow(w, reg)
	assert.NoError(t, err)
}
