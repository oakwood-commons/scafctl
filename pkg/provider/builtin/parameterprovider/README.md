# Parameter Provider

The parameter provider enables access to CLI parameters passed via `-r` or `--resolver` flags.

## Usage

### Basic String Parameter

```yaml
resolvers:
  environment:
    resolve:
      with:
        - provider: parameter
          inputs:
            key: env
```

**CLI Usage:**
```bash
scafctl run solution example -r env=prod
```

If the parameter is not provided, the resolver will fail. Use resolver fallback chains to provide defaults:

If the parameter is not provided, the resolver will fail. Use resolver fallback chains to provide defaults:

```yaml
resolvers:
  environment:
    resolve:
      with:
        - provider: parameter
          inputs:
            key: env
        - provider: static
          inputs:
            value: "dev"
```

### Multiple Parameters

```yaml
resolvers:
  environment:
    resolve:
      with:
        - provider: parameter
          inputs:
            key: env
  regions:
    resolve:
      with:
        - provider: parameter
          inputs:
            key: regions
```

**CLI Usage:**
```bash
scafctl run solution example -r env=prod -r regions=us-east1,us-west1
```

## Parsing Rules

Parameters are parsed according to a precedence order:

1. **Stdin** (`-r config=-`) - Read from stdin
2. **File Protocol** (`-r config=file:///path/to/config.yaml`) - Read file content
3. **HTTP Protocol** (`-r config=https://example.com/config`) - Fetch URL content
4. **JSON** (`-r config='{"key":"value"}'`) - Parse as JSON object/array
5. **Boolean** (`-r flag=true`) - Parse as boolean (`true`/`false`, case-insensitive)
6. **Number** (`-r count=42`, `-r rate=3.14`) - Parse as integer or float
7. **CSV** (`-r items=a,b,c`) - Split by comma into array
8. **Literal String** (fallback) - Treat as string

### Examples

#### Boolean
```bash
scafctl run solution -r dryRun=true
# → true (boolean)
```

#### Integer
```bash
scafctl run solution -r count=42
# → 42 (int64)
```

#### Float
```bash
scafctl run solution -r rate=3.14
# → 3.14 (float64)
```

#### CSV Array
```bash
scafctl run solution -r regions=us-east1,us-west1,eu-west1
# → ["us-east1", "us-west1", "eu-west1"] ([]string)
```

#### JSON Object
```bash
scafctl run solution -r 'config={"database":"postgres","port":5432}'
# → map[string]any{"database": "postgres", "port": 5432}
```

#### JSON Array
```bash
scafctl run solution -r 'items=["a","b","c"]'
# → []any{"a", "b", "c"}
```

#### File Content
```bash
scafctl run solution -r config=file:///path/to/config.yaml
# → Contents of /path/to/config.yaml as string
```

#### URL Content
```bash
scafctl run solution -r manifest=https://example.com/manifest.json
# → Response body from URL as string
```

#### Quoted Strings (Override Parsing)
```bash
scafctl run solution -r url="https://example.com"
# → "https://example.com" (literal string, not fetched)

scafctl run solution -r items="a,b,c"
# → "a,b,c" (literal string, not array)
```

## Multiple Values for Same Key

When the same parameter key is specified multiple times, values are merged into an array:

```bash
scafctl run solution -r items=a -r items=b -r items=c
# → ["a", "b", "c"]
```

## Stdin Handling

When a parameter value is `-`, the content is read from stdin:

```bash
cat config.yaml | scafctl run solution -r config=-
```

**Note:** Stdin is read once at CLI initialization. If multiple parameters reference stdin (e.g., `-r config=- -r data=-`), they all receive the same content with a warning.

## Provider Inputs

| Input | Type | Required | Description |
|-------|------|----------|-------------|
| `key` | string | Yes | Name of the parameter to retrieve (exact match) |

## Provider Output

| Output | Type | Description |
|--------|------|-------------|
| `value` | any | The parameter value (typed based on parsing) |
| `exists` | boolean | Whether the parameter was provided via CLI |
| `type` | string | Detected type: "string", "integer", "float", "boolean", "array", "object", "null" |

## Example Solution

```yaml
apiVersion: scafctl.io/v1
kind: Solution

metadata:
  name: deployment-example
  version: 1.0.0

spec:
  resolvers:
    environment:
      resolve:
        with:
          - provider: parameter
            inputs:
              key: env
          - provider: static
            inputs:
              value: "dev"
    
    regions:
      resolve:
        with:
          - provider: parameter
            inputs:
              key: regions
          - provider: static
            inputs:
              value: ["us-east1"]
    
    replicas:
      resolve:
        with:
          - provider: parameter
            inputs:
              key: replicas
          - provider: static
            inputs:
              value: 3
    
    enableMonitoring:
      resolve:
        with:
          - provider: parameter
            inputs:
              key: monitoring
          - provider: static
            inputs:
              value: true

  actions:
    deploy:
      provider: api
      inputs:
        endpoint: "https://api.example.com/deploy"
        config:
          environment: ${ resolvers.environment }
          regions: ${ resolvers.regions }
          replicas: ${ resolvers.replicas }
          monitoring: ${ resolvers.enableMonitoring }
```

**Run with parameters:**
```bash
scafctl run solution deployment-example \
  -r env=prod \
  -r regions=us-east1,us-west1,eu-west1 \
  -r replicas=10 \
  -r monitoring=false
```

**Run with defaults:**
```bash
scafctl run solution deployment-example
# Uses: env=dev, regions=[us-east1], replicas=3, monitoring=true
```
