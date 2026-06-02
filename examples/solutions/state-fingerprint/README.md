# State + Fingerprinting Showcase

Comprehensive demonstration of **state persistence** and **file fingerprinting**
in a code generation pipeline. On first run, templates are rendered and
configuration is persisted to state. On subsequent runs, actions are skipped
when source files haven't changed.

## Features Demonstrated

### State

| Feature | Where |
| --------- | ------- |
| `state.enabled` + `state.backend` config | Top-level `state:` block |
| Automatic parameter replay | CLI `-r` values saved and restored |
| Computed resolvers using replayed values | `output_dir`, `file_header` |
| Immutable resolver values (optional) | Shown in state-immutable example |

### Fingerprinting

| Feature | Where |
| --------- | ------- |
| `sources` glob patterns | `generate-code` action tracks `templates/**/*.tpl` |
| `generates` glob patterns | `generate-code` action tracks `output/**/*.go` |
| Skip on unchanged sources | Second run skips with "up-to-date" |
| Re-execute on modified generates | Editing output triggers re-generation |
| `--skip-fingerprint` bypass | Force flag ignores stored hashes |

## Setup

Templates are provided in the `templates/` directory -- no setup needed.

## Usage

### First Run

All actions execute, state and fingerprints are stored:

~~~sh
scafctl run solution -f solution.yaml -r project_name=myservice -r author=alice
~~~

### Second Run (Skipped)

Actions are skipped because template sources haven't changed.
Parameters are replayed from state automatically (no -r needed):

~~~sh
scafctl run solution -f solution.yaml
~~~

### Subsequent Runs (No Parameters Needed)

Values are loaded from state automatically:

~~~sh
scafctl run solution -f solution.yaml
~~~

### Modify a Template (Fingerprint Detects Change)

~~~sh
echo '// updated' >> templates/model.go.tpl
scafctl run solution -f solution.yaml
~~~

### Tamper with Output (Generates Hash Mismatch)

~~~sh
echo '// manual edit' >> output/myservice/model.go
scafctl run solution -f solution.yaml
~~~

### Force Re-execution

~~~sh
scafctl run solution -f solution.yaml --skip-fingerprint
~~~

### Inspect State

~~~sh
scafctl state list --path state-fingerprint-demo.json
~~~

### Clear State

~~~sh
scafctl state clear --path state-fingerprint-demo.json
~~~

## How It Works

~~~text
+----------------------------------------------------------------+
|                     Solution Execution                          |
+----------------------------------------------------------------+
|                                                                 |
|  1. State Load                                                  |
|     +-- Read state-fingerprint-demo.json from disk              |
|     +-- Merge saved parameters into parameter provider          |
|                                                                 |
|  2. Resolver Execution                                          |
|     +-- project_name: parameter (replayed or -r flag)           |
|     +-- author: parameter -> "anonymous" fallback               |
|     +-- output_dir: CEL expression using project_name           |
|     +-- file_header: CEL expression using multiple values       |
|     +-- template_vars: composite of resolved values             |
|     +-- template_files: read templates/ directory               |
|     +-- rendered_files: render templates with variables         |
|                                                                 |
|  3. State Save (parameters persisted atomically)                |
|     +-- CLI -r parameters merged into state                     |
|                                                                 |
|  4. Action Execution                                            |
|     +-- generate-code:                                          |
|     |   +-- Check fingerprint (sources SHA-256)                 |
|     |   +-- If up-to-date -> SKIP                               |
|     |   +-- If stale -> write rendered files to output/         |
|     |   +-- Record new fingerprint (sources + generates)        |
|     +-- show-summary:                                           |
|         +-- Display results using resolved values               |
|                                                                 |
|  5. State Final Save                                            |
|     +-- Fingerprint hashes persisted to disk                    |
|                                                                 |
+----------------------------------------------------------------+
~~~
