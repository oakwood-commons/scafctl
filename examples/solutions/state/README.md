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

### Clear State

~~~sh
scafctl state clear --path state-example.json
~~~
