---
name: go-template-patterns
description: "Go template patterns, built-in functions, custom scafctl functions, and conventions for scafctl. Use when working on Go template evaluation, the gotmpl package, or template-based providers."
---

# Go Template Patterns in scafctl

## Execution API

```go
result, err := gotmpl.Execute(ctx, gotmpl.TemplateOptions{
    Content:    "Hello {{.name}}",
    Name:       "greeting",
    Data:       map[string]any{"name": "world"},
    LeftDelim:  "{{",           // Default
    RightDelim: "}}",           // Default
    MissingKey: gotmpl.MissingKeyDefault,
    Funcs:      template.FuncMap{},  // Additional funcs
})
```

### MissingKey Options

| Option | Behavior |
|--------|----------|
| `MissingKeyDefault` | Prints `<no value>` for missing keys |
| `MissingKeyZero` | Returns zero value for the type |
| `MissingKeyError` | Stops execution with error |

## Template Data Context

In solution templates, the root data (`.`) contains all resolved values:

```gotemplate
{{.resolver_name}}              <!-- Direct resolver access -->
{{.config.database.host}}       <!-- Nested field access -->
```

### Built-in Template Variables (for file templates)

When used with the directory provider for file generation:

| Variable | Contains |
|----------|----------|
| `.__filePath` | Full file path being generated |
| `.__fileName` | File name with extension |
| `.__fileStem` | File name without extension |
| `.__fileDir` | Directory containing the file |
| `.__fileExt` | File extension |

## Functions

### Sprig v3 (100+ functions)

All [Sprig v3](http://masterminds.github.io/sprig/) functions are available:

**String:**
```gotemplate
{{trim .name}}                  <!-- Strip whitespace -->
{{upper .name}}                 <!-- Uppercase -->
{{lower .name}}                 <!-- Lowercase -->
{{title .name}}                 <!-- Title case -->
{{snakecase .name}}             <!-- snake_case -->
{{camelcase .name}}             <!-- CamelCase -->
{{kebabcase .name}}             <!-- kebab-case -->
{{repeat 3 .char}}              <!-- Repeat string -->
{{substr 0 5 .name}}            <!-- Substring -->
{{replace "old" "new" .input}}  <!-- Replace -->
{{contains "sub" .name}}        <!-- Contains check -->
{{hasPrefix "pre" .name}}       <!-- Prefix check -->
{{hasSuffix "suf" .name}}       <!-- Suffix check -->
{{quote .name}}                 <!-- Add quotes -->
{{squote .name}}                <!-- Add single quotes -->
{{nospace .name}}               <!-- Remove spaces -->
{{indent 4 .block}}             <!-- Indent text -->
{{nindent 4 .block}}            <!-- Newline + indent -->
```

**Collections:**
```gotemplate
{{join "," .items}}             <!-- Join list -->
{{first .items}}                <!-- First element -->
{{last .items}}                 <!-- Last element -->
{{slice .items 1 3}}            <!-- Sub-slice -->
{{has .items "value"}}          <!-- List contains -->
{{uniq .items}}                 <!-- Remove duplicates -->
{{sortAlpha .items}}            <!-- Sort strings -->
```

**Dictionary:**
```gotemplate
{{keys .config}}                <!-- Map keys -->
{{values .config}}              <!-- Map values -->
{{hasKey .config "key"}}        <!-- Key exists -->
{{pluck "name" .items}}         <!-- Extract field from list of maps -->
{{pick .config "key1" "key2"}}  <!-- Select keys -->
{{omit .config "secret"}}       <!-- Exclude keys -->
```

**Math:**
```gotemplate
{{add 1 2}}                     <!-- Addition -->
{{sub 10 3}}                    <!-- Subtraction -->
{{mul 2 5}}                     <!-- Multiplication -->
{{div 10 3}}                    <!-- Division -->
{{mod 10 3}}                    <!-- Modulo -->
{{max 3 5 1}}                   <!-- Maximum -->
{{min 3 5 1}}                   <!-- Minimum -->
```

**Type/Logic:**
```gotemplate
{{empty .value}}                <!-- Is zero/nil/empty -->
{{default "fallback" .value}}   <!-- Default if empty -->
{{ternary "yes" "no" .flag}}    <!-- Ternary -->
{{typeOf .value}}               <!-- Go type name -->
{{kindOf .value}}               <!-- Go kind name -->
```

**Encoding:**
```gotemplate
{{b64enc .data}}                <!-- Base64 encode -->
{{b64dec .encoded}}             <!-- Base64 decode -->
{{sha256sum .data}}             <!-- SHA256 hash -->
```

**UUID:**
```gotemplate
{{uuidv4}}                      <!-- Generate UUID v4 -->
```

### Custom scafctl Functions

```gotemplate
{{cel "_.name.upperAscii()"}}   <!-- Inline CEL evaluation -->
{{toHcl .config}}               <!-- Convert to HCL format -->
{{fromYaml .yamlString}}        <!-- Parse YAML string to object -->
{{toYaml .data}}                <!-- Convert to YAML string -->
{{slugify .title}}              <!-- URL-safe slug -->
{{toDNSString .name}}           <!-- DNS-safe string -->
{{where .items "condition"}}    <!-- Filter array -->
{{select .items "mapping"}}     <!-- Transform array elements -->
```

## CEL vs Go Template Decision Guide

| Use Case | CEL (`expr`) | Go Template (`tmpl`) |
|----------|-------------|---------------------|
| Data manipulation | Preferred | Avoid |
| Conditionals | Preferred | Acceptable |
| Text rendering | Avoid | Preferred |
| Multi-line output | Avoid | Preferred |
| File content | Avoid | Preferred |
| Type coercion | Preferred | Limited |
| List/map operations | Preferred | Basic only |
| String concatenation | Either | Either |

**Rule of thumb**: CEL for **data**, Go templates for **text**.

## Template File Conventions

- Use `.tpl` extension for template files
- Templates are for **text rendering only** -- no data logic
- Access data via resolver names: `{{.my_resolver}}`
- Use `{{- ... -}}` for whitespace control

## Anti-Patterns

| Anti-Pattern | Why | Instead |
|-------------|-----|---------|
| Complex logic in templates | Hard to test, debug | Use CEL transform phase, then reference result |
| Data manipulation in templates | Templates are for rendering | Use CEL expressions |
| Deeply nested `{{if}}` blocks | Unreadable | Split into multiple resolvers with `when` |
| Magic values in templates | Maintenance burden | Use resolver references |

## Key Packages

- `pkg/gotmpl/`: Template execution, options, missing key modes
- `pkg/provider/builtin/gotmplprovider/`: Go template provider for transform phase
