# State Examples

Demonstrates state persistence across solution executions.

## solution.yaml

A solution that persists resolver values to a local state file.
On first run, values are collected via parameters. On subsequent runs,
they are loaded from state automatically.

### First Run

~~~sh
scafctl run resolver -f solution.yaml -r username=alice -r env=prod
~~~

### Subsequent Runs

~~~sh
# No parameters needed -- values come from state
scafctl run resolver -f solution.yaml
~~~

### Inspect State

~~~sh
scafctl state list --path state-example.json
scafctl state get --path state-example.json --key username
~~~

### Clear State

~~~sh
scafctl state clear --path state-example.json
~~~
