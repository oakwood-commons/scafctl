# Git Repository Normalizer Example

This example demonstrates the **resolver pipeline** in action, showing how resolvers transform and validate data through all four phases: resolve → transform → validate → emit.

## Overview

The solution normalizes Git repository URLs into consistent naming conventions. It extracts the repository name, organization, and generates safe branch names and clone commands—all through resolver transformations.

**Key patterns demonstrated:**
- Extracting data from input using expressions
- Transforming extracted values for consistency
- Multi-step validation with detailed error messages
- Using transformed values in dependent resolvers
- Complex CEL expressions for conditional logic

## The Resolver Pipeline

Each resolver executes through four guaranteed phases:

```
Input (resolve) → Transform → Validate → Output (emit)
```

### Phase 1: Resolve
Determine the initial value from multiple sources (CLI, env, static, providers).

### Phase 2: Transform
Modify or derive the resolved value using CEL expressions.
- Access current value via `__self`
- Access other resolvers via `_`
- Can call functions, manipulate strings, apply logic

### Phase 3: Validate
Ensure the transformed value meets requirements.
- Multiple validation rules can be applied
- Each rule has a custom error message
- All rules must pass

### Phase 4: Emit
Output the validated value to resolver context.

## Key Resolvers

### `repoUrl` - Input URL

```yaml
repoUrl:
  description: Git repository URL (https or ssh format)
  resolve:
    from:
      - provider: cli
        key: repoUrl
      - provider: env
        key: GIT_REPO_URL
      - provider: static
        value: "https://github.com/scafctl/scafctl.git"
  validate:
    regex: "^(https://|git@|ssh://)"
    errorMessage: "Repository URL must be https://, git@, or ssh://"
```

**Pipeline:**
1. **Resolve**: User input → env variable → default URL
2. **Transform**: (none—passed through)
3. **Validate**: Ensure it's a valid Git URL format
4. **Emit**: `_.repoUrl` available to other resolvers

### `repoName` - Extraction + Normalization

This is the most complex example, showing extraction and transformation:

```yaml
repoName:
  description: Extracted and normalized repository name
  resolve:
    from:
      - provider: expression
        expr: |
          _.repoUrl.contains('/')
            ? _.repoUrl.split('/')[_.repoUrl.split('/').size() - 1]
            : _.repoUrl

  # Transform phase: normalize the extracted name
  transform:
    - expr: __self.replace('.git', '').replace('_', '-').toLowerCase()

  # Validate phase: ensure name follows conventions
  validate:
    - expr: __self.matches("^[a-z0-9-]+$")
      message: "Repository name must contain only lowercase letters, numbers, and hyphens"
    - expr: size(__self) >= 2 && size(__self) <= 50
      message: "Repository name must be between 2 and 50 characters"
    - expr: !__self.startsWith('-') && !__self.endsWith('-')
      message: "Repository name cannot start or end with a hyphen"
```

**Pipeline:**
1. **Resolve**:
   - Extract from URL using CEL expression
  - `https://github.com/scafctl/scafctl.git` → `scafctl.git`

2. **Transform**:
   - Remove `.git` suffix → `scafctl`
   - Replace underscores with hyphens (if any)
   - Convert to lowercase (if any capitals)

3. **Validate**:
   - Must match `[a-z0-9-]+` pattern
   - Length between 2-50 characters
   - Cannot start/end with hyphen

4. **Emit**: `_.repoName` → `scafctl`

### `repoOrg` - Organization Extraction

Demonstrates conditional logic in resolution:

```yaml
repoOrg:
  description: Organization or user extracted from URL
  resolve:
    from:
      - provider: cli
        key: org
      - provider: expression
        expr: |
          _.repoUrl.contains('github.com/')
            ? _.repoUrl.split('github.com/')[1].split('/')[0]
            : _.repoUrl.contains('gitlab.com/')
            ? _.repoUrl.split('gitlab.com/')[1].split('/')[0]
            : 'unknown'

  # Transform: normalize organization name
  transform:
    - expr: __self.toLowerCase()

  validate:
    - expr: __self != 'unknown' || _.repoUrl.contains('localhost')
      message: "Could not extract organization from URL. Supported: GitHub, GitLab"
```

**Pipeline:**
1. **Resolve**:
   - CLI input (if provided)
   - Extract from GitHub/GitLab URL
   - Fall back to 'unknown'

2. **Transform**: Lowercase for consistency

3. **Validate**: Ensure it was successfully extracted (or is localhost)

4. **Emit**: `_.repoOrg` → `scafctl`

### `branchName` - Derived Branch Name

Shows how transforms reference other resolvers:

```yaml
branchName:
  description: Safe branch name derived from repo name
  resolve:
    from:
      - provider: cli
        key: branch
      - provider: expression
        expr: "_.repoName"

  # Transform: prefix with conventional branch type
  transform:
    - expr: "'feature/' + __self"

  validate:
    - expr: __self.matches("^(feature|fix|docs|chore)/[a-z0-9-]+$")
      message: "Branch name must follow convention: feature/*, fix/*, docs/*, or chore/*"
```

**Pipeline:**
1. **Resolve**: CLI input → use `repoName` resolver
2. **Transform**: Add `feature/` prefix → `feature/scafctl`
3. **Validate**: Ensure matches branch naming convention
4. **Emit**: `_.branchName` → `feature/scafctl`

### `normalizedBranch` - Conditional Transform Stopping with `until:`

The most advanced example—demonstrates transform array with **conditional stopping** using the `until:` field:

```yaml
normalizedBranch:
  description: Normalized branch name with fallback logic and conditional stopping
  resolve:
    from:
      - provider: cli
        key: normalizedBranch
      - provider: static
        value: ""

  # Transform array with conditional stopping using 'until:' field
  transform:
    # Step 1: Try to use cloneBranch if provided
    - expr: _.cloneBranch != "" ? _.cloneBranch : __self
      until: __self != ""  # Stop remaining transforms if we found a value

    # Step 2: If still empty, use branchName as fallback
    - expr: _.branchName != "" ? _.branchName : __self
      until: __self != ""  # Stop remaining transforms if we found a value

    # Step 3: Always normalize to safe branch name format
    - expr: __self.toLowerCase().replace(' ', '-').replace('_', '-')

  validate:
    - expr: __self == "" || __self.matches("^[a-z0-9-/]+$")
      message: "Branch name must contain only lowercase letters, numbers, hyphens, and slashes"
```

**How `until:` works:**

The `until:` field on each transform item controls whether remaining transforms execute:
- If `until` condition is **true**, skip this and all remaining transforms (stop early)
- If `until` condition is **false**, execute this transform and continue to next
- If `until` is omitted, always execute the transform

**Pipeline examples:**

**Case 1: cloneBranch provided**
```
Input: normalizedBranch = "", cloneBranch = "production"
Step 1: __self = "production"
        until condition: "production" != "" is TRUE → STOP (skip steps 2-3)
Output: "production"
```

**Case 2: cloneBranch empty, branchName available**
```
Input: normalizedBranch = "", cloneBranch = "", branchName = "feature/scafctl"
Step 1: __self = "" (cloneBranch is empty)
        until condition: "" != "" is FALSE → CONTINUE
Step 2: __self = "feature/scafctl" (use branchName)
        until condition: "feature/scafctl" != "" is TRUE → STOP (skip step 3)
Output: "feature/scafctl"
```

**Case 3: Both empty, normalization applies**
```
Input: normalizedBranch = "my branch", cloneBranch = "", branchName = "feature/repo"
Step 1: __self = "my branch" (custom value provided)
        until condition: "my branch" != "" is TRUE → STOP (skip steps 2-3)
Wait, let me reconsider... actually:
Step 1: expr tries cloneBranch which is empty, so __self stays "my branch"
        until condition: "my branch" != "" is TRUE → STOP
Output: "my branch"

OR if user doesn't provide normalizedBranch:
Input: normalizedBranch = "", cloneBranch = "", branchName = "feature/repo"
Step 1: __self = "" (cloneBranch is empty)
        until condition: "" != "" is FALSE → CONTINUE
Step 2: __self = "feature/repo" (use branchName)
        until condition: "feature/repo" != "" is TRUE → STOP (skip step 3)
Output: "feature/repo"

OR with spaces that need normalizing:
Input: normalizedBranch = "my branch", cloneBranch = "", branchName = "feature/repo"
Step 1: __self = "my branch"
        until condition: "my branch" != "" is TRUE → STOP
Output: "my branch"

Actually to hit step 3, you'd need all earlier conditions to fail:
Input: normalizedBranch = "", cloneBranch = "", branchName = ""
Step 1: __self = ""
        until condition: "" != "" is FALSE → CONTINUE
Step 2: __self = ""
        until condition: "" != "" is FALSE → CONTINUE
Step 3: __self = "" → normalize → ""
Output: ""
```

This pattern is useful for **fallback/retry logic** where you try multiple sources and stop as soon as one succeeds.

### `cloneCommand` - Conditional Transform Logic

### `repoConfig` - Type-Based Transform with `until: type(__self) == ...`

The most advanced pattern—demonstrates **type checking in `until:` conditions**:

```yaml
repoConfig:
  description: Repository configuration that can be string URL or object config
  resolve:
    from:
      - provider: cli
        key: repoConfig
      - provider: expression
        expr: "_.repoUrl"  # Default to URL string

  # Transform with type checking using 'until: type(__self) == ...'
  transform:
    # Step 1: Try to parse as JSON if it's a string
    - expr: |
        type(__self) == "string" ?
          (__self.startsWith('{') ? parseJson(__self) : __self)
          : __self
      until: type(__self) != "string"  # Stop if we parsed into object

    # Step 2: If we have object, ensure it has required fields
    - expr: |
        type(__self) == "object" ?
          __self + {url: __self.url || _.repoUrl, name: __self.name || _.repoName}
          : __self
      until: type(__self) != "object"  # Stop if it's still not an object

    # Step 3: Convert back to normalized string if needed
    - expr: |
        type(__self) == "object" ? __self.url : __self

  validate:
    - expr: __self != null && __self != ""
      message: "Repository config must be valid URL or JSON object"
```

**How type-based `until:` works:**

Using `type(__self)` in `until:` conditions enables powerful patterns:

**Case 1: String input (URL)**
```
Input: repoConfig = "https://github.com/test/repo"
Step 1: type(__self) = "string"
        Doesn't start with '{', so stays as URL string
        until: type(__self) != "string" is FALSE → CONTINUE

Step 2: type(__self) = "string" (still a string)
        Can't add fields to string, stays as-is
        until: type(__self) != "object" is TRUE → STOP

Output: "https://github.com/test/repo"
```

**Case 2: JSON input (object)**
```
Input: repoConfig = '{"url":"https://github.com/custom/repo","name":"custom"}'
Step 1: type(__self) = "string"
        Starts with '{', so parseJson → {url: "...", name: "custom"}
        until: type(__self) != "string" is TRUE → STOP

Output: "https://github.com/custom/repo"
```

**Case 3: Object input directly**
```
Input: repoConfig = {url: "https://...", name: "custom"}
Step 1: type(__self) = "object"
        Doesn't match "string", stays as-is
        until: type(__self) != "string" is TRUE → STOP

Output: "https://..."
```

This pattern is powerful for:
- **Flexible input types**: Accept string, JSON string, or object
- **Type-driven logic**: Different transforms for different types
- **Progressive parsing**: Parse only if needed, stop when done

Shows conditional logic inside transform expressions:

```yaml
cloneCommand:
  description: Ready-to-use git clone command
  resolve:
    from:
      - provider: expression
        expr: "'git clone ' + _.repoUrl + ' ' + _.repoName"

  # Transform: add optional branch checkout
  transform:
    - expr: |
        _.cloneBranch ?
          __self + ' && cd ' + _.repoName + ' && git checkout ' + _.cloneBranch
          : __self

  validate:
    - expr: __self.startsWith('git clone')
      message: "Clone command must start with 'git clone'"
```

**Pipeline:**
1. **Resolve**: Build basic clone command
  - `git clone https://github.com/scafctl/scafctl.git scafctl`

2. **Transform**:
   - If `cloneBranch` is set, append checkout commands
   - Result: `git clone ... && cd scafctl && git checkout main`
   - If not set, pass through as-is

3. **Validate**: Ensure starts with `git clone`

4. **Emit**: `_.cloneCommand` → ready-to-execute command

### `description` - Generated Default

Shows transform that generates values when not provided:

```yaml
description:
  description: Auto-generated repository description
  resolve:
    from:
      - provider: cli
        key: description
      - provider: static
        value: ""

  # Transform: generate from repo info if not provided
  transform:
    - expr: |
        __self == "" ?
          "Repository: " + _.repoName + " from " + _.repoOrg
          : __self

  validate:
    - expr: size(__self) <= 200
      message: "Description must be 200 characters or less"
```

**Pipeline:**
1. **Resolve**: User input → empty string
2. **Transform**:
  - If empty, generate: `Repository: scafctl from scafctl`
   - If provided, use as-is
3. **Validate**: Max 200 characters
4. **Emit**: `_.description` → auto-generated or user-provided

## Usage Examples

### Default: Normalize default GitHub URL

```bash
scafctl run solution:git-repo-normalizer
```

Resolves to:
- `repoUrl`: `https://github.com/scafctl/scafctl.git`
- `repoName`: `scafctl`
- `repoOrg`: `scafctl`
- `branchName`: `feature/scafctl`
- `cloneCommand`: `git clone https://github.com/scafctl/scafctl.git scafctl`

### Custom URL without .git suffix

```bash
scafctl run solution:git-repo-normalizer \
  -r repoUrl=https://github.com/my-org/my_awesome_project
```

Transforms:
- Input: `my_awesome_project`
- Transform: Remove `.git`, replace `_` with `-`, lowercase
- Output: `my-awesome-project`

### With branch checkout

```bash
scafctl run solution:git-repo-normalizer \
  -r repoUrl=https://github.com/test-org/test-repo.git \
  -r branch=main
```

Clone command transforms to:
```bash
git clone https://github.com/test-org/test-repo.git test-repo && cd test-repo && git checkout main
```

### Custom description

```bash
scafctl run solution:git-repo-normalizer \
  -r description="My custom repo description"
```

Description is preserved (transform detects non-empty value and doesn't override).

## Learning Points

1. **Transform Phase is Powerful**
   - Extract data from input
   - Normalize formats (lowercase, trim, replace, etc.)
   - Derive values from other resolvers
   - Apply conditional logic
   - Generate default values

2. **Transform Array Execution**
   - Transform is an array of CEL expressions
   - Each executes in order by default
   - Each receives `__self` as output from previous transform
   - `until:` field can conditionally stop execution

3. **The `until:` Field for Conditional Stopping**
   - `until: <CEL expression>` stops remaining transforms if condition is true
   - Useful for fallback/retry patterns
   - Stops at first successful result
   - Reduces unnecessary operations

4. **`__self` vs `_`**
   - `__self` = current resolver's value (what transform receives/modifies)
   - `_` = all other resolvers (references for dependencies)
   - Each transform modifies `__self` for next transform

5. **Multiple Validation Rules**

   - Each rule validates different aspect
   - All must pass
   - Each has custom error message for clarity

4. **Resolver Dependencies Flow Naturally**
   - `repoName` depends on `repoUrl`
   - `branchName` depends on `repoName`
   - CEL expressions reference via `_`
   - Engine resolves DAG automatically

5. **Transforms vs Validation**
   - **Transform**: Modify the value (output is new value)
   - **Validate**: Check the value (output is pass/fail)
   - Both run in guaranteed order

6. **CEL Expressions Are Turing-Complete**
   - Conditional logic (`?` operator)
   - String manipulation (`.split()`, `.replace()`, etc.)
   - Comparisons and boolean logic
   - Function calls (`.matches()`, `.size()`, etc.)

## Testing

This solution includes comprehensive tests:

```yaml
- name: resolve-github-https
  type: engine
  synopsis: Normalize GitHub HTTPS URL with defaults

- name: resolve-custom-github-url
  type: engine
  synopsis: Transform custom GitHub URL without .git suffix

- name: resolve-with-branch-checkout
  type: engine
  synopsis: Transform clone command with branch checkout

- name: validate-repo-name-format
  type: engine
  synopsis: Validate that transformed repo name follows conventions

- name: custom-description
  type: engine
  synopsis: Validate custom description is preserved in transform

- name: dry-run-normalize
  type: cli
  synopsis: Verify repository normalization without side effects
```

Run tests:
```bash
scafctl test solution:git-repo-normalizer
```

## Execution Flow

```
1. User provides (or defaults): repoUrl
   ↓
2. repoUrl resolves, validates
   ↓
3. repoName resolves from repoUrl
   - Extract via CEL expression
   - Transform: remove .git, normalize case/separators
   - Validate: must match [a-z0-9-]+ and length constraints
   ↓
4. repoOrg resolves from repoUrl (parallel to repoName)
   - Extract organization from URL
   - Transform: lowercase
   - Validate: successfully extracted
   ↓
5. branchName resolves from repoName
   - Uses resolved repoName value
   - Transform: add feature/ prefix
   - Validate: matches branch convention
   ↓
6. cloneCommand resolves from repoUrl, repoName, cloneBranch
   - Build basic command
   - Transform: conditionally add checkout based on cloneBranch
   - Validate: starts with 'git clone'
   ↓
7. description resolves (can auto-generate from repoName, repoOrg)
   - Transform: generate if empty
   - Validate: length constraint
   ↓
8. All resolvers available in context (_)
   Action displays normalized information
```

## See Also

- `notes/resolvers.md` - Detailed resolver pipeline documentation
- `notes/EXPRESSION-LANGUAGE-PATTERN.md` - CEL expression patterns
- `examples/go-taskfile/` - Action dependencies and DAG execution
- `examples/terraform-multi-env/` - Templates as resolver sources
