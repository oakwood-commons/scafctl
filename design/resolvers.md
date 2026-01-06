# Resolvers

## Purpose

Resolvers produce named data values. They exist to gather, normalize, validate, and emit data in a deterministic way so that actions and other resolvers can consume it without re-computation or implicit behavior.

Resolvers are the only mechanism for introducing data into a solution. Actions never fetch or derive data on their own.

Resolvers do not cause side effects. They only compute values.

---

## Responsibilities

A resolver is responsible for:

- Declaring how a value is obtained
- Normalizing or deriving the value using pure computation
- Validating the final value
- Emitting a named result into the resolver context

A resolver is not responsible for:

- Performing side effects
- Orchestrating execution
- Rendering output
- Mutating shared state

---

## Resolver Model

### Conceptual Flow

- resolve
  - fetch raw value using providers
- transform
  - apply provider-backed transformations
- validate
  - enforce constraints
- emit
  - publish value into context

Each resolver follows this sequence exactly and in order.

---

## Resolver Phases

Resolvers execute through four fixed phases.

---

## 1. Resolve

The resolve phase selects an initial value from one or more sources.

~~~yaml
resolve:
  from:
    - provider: parameter
      inputs:
        key: name
    - provider: env
      inputs:
        key: PROJECT_NAME
    - provider: static
      inputs:
        value: my-app
~~~

Key properties:

- Uses providers explicitly
- Sources are evaluated in order
- First non-null value wins by default
- Providers return raw, unprocessed data

Optional controls:

- `when:` to conditionally skip a source
- `until:` to stop evaluation early

The resolve phase answers: where does the value come from?

---

## 2. Transform

The transform phase derives a new value from the resolved value.

Transform steps are provider-backed, but differ from resolve in intent:

- Resolve selects a value
- Transform modifies an existing value

---

### Transform as Provider Execution

Each transform step is executed by a provider. Transform is not limited to expression evaluation. Any provider that is pure and side-effect-free may participate in transform.

The distinction is semantic:

- Resolve selects an initial value from sources
- Transform derives new values from an existing value

Although transform uses providers, it does not choose between competing inputs. It applies a sequence of operations to a single value. This is why the keyword is `into` rather than `from`.

#### Example Using Multiple Providers

~~~yaml
transform:
  into:
    - provider: filesystem
      inputs:
        operation: read
        path: ./templates/base-name.txt

    - provider: expression
      expr: __self.trim()

    - provider: expression
      expr: __self.toLowerCase()

    - provider: expression
      expr: __self.replace("_", "-")
~~~

In this example:

- The filesystem provider supplies a value derived from local state
- Subsequent expression providers normalize and shape that value
- Each step receives the previous value as `__self`

Transform providers must be deterministic and free of externally visible side effects.

#### Controls

- `when:` at transform level to skip all steps
- `when:` at item level to conditionally apply a step
- `until:` to stop processing once a condition is met

The transform phase answers: how should this value be shaped?

---

## 3. Validate

The validate phase enforces constraints on the transformed value.

~~~yaml
validate:
  - expr: __self.matches("^[a-z0-9-]+$")
    message: "Must be lowercase alphanumeric with hyphens"
~~~

Validation rules are provider-backed and always use the expression provider.

Rules:

- All validations must pass
- Validation failures stop execution
- Validation does not mutate data

---

## 4. Emit

If resolve, transform, and validate succeed, the resolver emits its value.

- The value becomes available as `_.<resolverName>`
- Values are immutable after emission
- Emission is implicit and cannot be customized

---

## Feeding Resolver Values Into Providers

Resolvers emit typed values that can be consumed by providers in later resolvers or actions. This is enabled through custom unmarshalling, which allows provider inputs to accept multiple shapes while preserving strong typing.

### Supported Input Forms

Each provider input field may accept one of the following forms.

#### 1. Literal Value

Passed as-is with no evaluation.

~~~yaml
inputs:
  image: nginx:1.27
~~~

#### 2. Direct Resolver Binding

Copies the resolver value directly, preserving its type.

~~~yaml
inputs:
  image: _.image
~~~

#### 3. Expression-Based Value

Evaluated using CEL before provider execution.

~~~yaml
inputs:
  image:
    expr: _.org + "/" + _.repo + ":" + _.version
~~~

#### 4. Template-Based Value

Rendered using Go templating. Always produces a string.

~~~yaml
inputs:
  image: "{{ _.org }}/{{ _.repo }}:{{ _.version }}"
~~~

---

### Go Representation (Custom Unmarshalling)

~~~go
type StringValue struct {
  Literal *string
  Expr    *string
}

func (s *StringValue) UnmarshalYAML(value *yaml.Node) error {
  switch value.Kind {

  case yaml.ScalarNode:
    var v string
    if err := value.Decode(&v); err != nil {
      return err
    }
    s.Literal = &v
    return nil

  case yaml.MappingNode:
    var raw struct {
      Expr string `yaml:"expr"`
    }
    if err := value.Decode(&raw); err != nil {
      return err
    }
    s.Expr = &raw.Expr
    return nil

  default:
    return fmt.Errorf("invalid input type")
  }
}
~~~

All inputs are resolved into concrete values before provider execution. Providers never see expressions, templates, or resolver references.

---

## Resolver Parameters (CLI Overrides)

### Purpose

Resolvers may receive values directly from the CLI using resolver parameters. This allows users, pipelines, and external executors to override resolver inputs without modifying solution files.

Resolver parameters:

- Override the resolve phase
- Preserve transform and validate phases
- Support rich data types
- Work identically in run and render modes

---

## CLI Syntax

Resolver parameters are supplied using the `-r` or `--resolver` flag.

~~~bash
scafctl run solution:example -r key=value
~~~

Multiple resolver parameters may be supplied:

~~~bash
scafctl run solution:example \
  -r env=prod \
  -r regions=us-east1,us-west1
~~~

Each `-r` maps directly to a resolver name.

---

## Resolver Parameter Semantics

When a resolver parameter is supplied:

- The resolver `resolve.from` block is skipped
- The supplied value becomes the resolver input
- `transform` still executes
- `validate` still executes
- The value is emitted normally

---

## Supported Resolver Input Forms

Resolver parameters support multiple input forms.

### Literal Strings

~~~bash
-r name=my-app
~~~

### Numbers

~~~bash
-r replicas=3
-r timeout=1.5
~~~

### Booleans

~~~bash
-r dryRun=true
~~~

### CSV Lists

~~~bash
-r environments=dev,qa,prod
~~~

### JSON Values

~~~bash
-r config={"foo":"bar","count":3}
~~~

### Stdin Input

~~~bash
cat config.json | scafctl run solution:example -r config=-
~~~

### File References

~~~bash
-r config=file://./config.json
~~~

### URL References

~~~bash
-r data=https://example.com/data.json
~~~

### Explicit Type Prefixes (Optional)

~~~bash
-r environments=json:["dev","qa","prod"]
-r count=int:3
-r enabled=bool:true
-r raw=string:00123
~~~

---

## Interaction with Cobra

scafctl uses Cobra for CLI parsing, but Cobra is intentionally limited to collecting raw resolver input strings.

Cobra responsibilities:

- Register `-r` / `--resolver` flags
- Accept repeated string values
- Preserve input ordering

Cobra does not:

- Parse types
- Decode JSON
- Read files or stdin
- Apply resolver semantics

All parsing, decoding, validation, and error reporting occurs in scafctl core, not in Cobra.

---

## Resolver Dependencies

Resolvers form a directed acyclic graph inferred from references.

~~~yaml
resolvers:
  name:
    resolve:
      from:
        - provider: parameter
          inputs:
            key: name

  image:
    resolve:
      from:
        - provider: expression
          expr: _.name + ":latest"
~~~

Rules:

- Dependencies are inferred from `_` references
- Execution order is computed automatically
- Independent resolvers execute concurrently
- Cycles are rejected

---

## Minimal Resolver Execution

Resolvers run only when required.

~~~bash
scafctl run solution:myapp --action deploy
~~~

Force all resolvers with:

~~~bash
scafctl run solution:myapp --resolve-all
~~~

---

## Resolver Example

~~~yaml
spec:
  resolvers:
    environment:
      description: Deployment environment

      resolve:
        from:
          - provider: parameter
            inputs:
              key: env
          - provider: static
            inputs:
              value: dev

      transform:
        into:
          - expr: __self.toLowerCase()

      validate:
        - expr: __self in ["dev", "staging", "prod"]
          message: "Invalid environment"
~~~

---

## Design Constraints

- Resolve uses providers to select values
- Transform uses providers to derive values
- Validate uses providers to enforce constraints
- Resolver values are fed into providers via typed input binding
- All resolver phases are provider-backed
- Resolvers must remain pure and deterministic

---

## Summary

Resolvers define how data is sourced, derived, checked, and shared in scafctl. Resolve chooses data, transform shapes data, validate enforces correctness, and emit publishes results. Resolver parameters allow explicit runtime input while preserving the resolver lifecycle. Custom unmarshalling ensures resolver values are safely consumed by providers without leaking evaluation logic into provider implementations.
