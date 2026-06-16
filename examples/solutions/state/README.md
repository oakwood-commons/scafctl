# State Examples

Demonstrates state persistence across solution executions using the
parameter-replay pattern.

## solution.yaml

A solution that persists CLI parameters to a local state file.
On first run, values are provided via `-r` flags. On subsequent runs,
saved parameters are automatically replayed via the parameter provider.

### First Run

~~~sh
scafctl run resolver -f solution.yaml -r username=alice -r env=prod
~~~

### Subsequent Runs

~~~sh
# No parameters needed -- saved parameters are replayed from state
scafctl run resolver -f solution.yaml
~~~

### Inspect State

~~~sh
scafctl state list --path state-example.json
~~~

## github-state.yaml

GitHub-based state persistence with PR workflows using `saveOverrides`.
State is loaded from `main` and saved to a resolver-derived feature branch,
enabling PR-based review of state changes.

Requires the `github` provider plugin (>= 0.6.0).

### First Run

~~~sh
scafctl run resolver -f ./github-state.yaml \
  -r app_name=my-app -r branch_name=feat/my-feature -r environment=prod
~~~

### How it works

- **Load**: reads state from `refs/heads/main` at `state/<app_name>.json`
- **Save**: writes state to the `featureBranch` resolver value (e.g., `feat/my-feature`)
- **saveOverrides**: the `branch` and `message` inputs are only resolved at
  save time when resolver data (`_`) is available

### Prerequisites

~~~sh
scafctl auth login github
scafctl plugins install github
~~~

### Clear State

~~~sh
scafctl state clear --path state-example.json
~~~
