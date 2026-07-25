# Dynamic State Configuration Example

Demonstrates referencing resolver outputs from the `state.enabled` and
`state.backend.inputs` fields. The state file path and the enable flag are
computed from resolved values instead of hard-coded literals.

## How it works

Before state is loaded, scafctl runs a minimal "Phase A" -- just the resolvers
the load-time state fields transitively require (`persist_state`, `state_path`,
and `app_name`). It evaluates `state.enabled` and `state.backend.inputs.path`
from those values, loads state, then reuses the Phase-A results in the main run
(they are not executed twice).

Only *state-independent* resolvers may be referenced here. A resolver that reads
state (via the `state` provider) or depends on one that does would be circular
and is rejected at load time with a clear error (lint rules
`state-ref-state-dependent` and `state-ref-unknown`).

## Usage

Each app gets its own state file, and persistence can be toggled per run:

~~~sh
# Writes state to ./state/billing.json
scafctl run resolver -f solution.yaml -r app_name=billing -r persist=true

# State disabled (enabled resolves to false); nothing written
scafctl run resolver -f solution.yaml -r app_name=web -r persist=false
~~~

### Inspect state

~~~sh
scafctl state list --path state/billing.json
~~~
