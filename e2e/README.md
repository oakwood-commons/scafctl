# E2E Testing Guide

This module (`e2e/`) contains scafctl's end-to-end tests: specs that build the
real `scafctl` binary and drive it as a subprocess against fixture data,
isolated local state, and (when needed) an in-memory OCI registry.

If you are adding a new e2e spec, read "Walkthrough: adding a new spec" below
and copy the pattern. The rest of this document is reference material.

## Why a separate module

`e2e/` is its own Go module with its own `go.mod`, plus two Go workspace
files that serve different purposes:

- `go.work` (repo root) lists only `.` -- the main module. This is what your
  editor/tooling uses day to day, and it deliberately does **not** include
  `e2e/`, so editing the main module doesn't pull in the e2e module's test-only
  dependencies (Ginkgo, Gomega, go-containerregistry, etc).
- `e2e/go.work` lists `.` and `..` -- it unions the e2e module with the root
  module. Running `go test` with `GOWORK=e2e/go.work` guarantees the e2e
  tests compile the code in your working tree (via a `replace` in
  `e2e/go.mod` pointing at `../`), not a pinned version from `e2e/go.sum`.

In practice you rarely set `GOWORK` by hand -- the `test:e2e:go` task does it
for you (see "Running the suite").

**Naming gotcha:** `task test:e2e` (no `:go` suffix) does **not** run this
module. It runs unit test coverage, solution linting, and
`tests/integration/`. The Ginkgo suites in this directory are run by
`task test:e2e:go`. If you're looking for "did my new e2e spec run," use
`test:e2e:go`.

## Ginkgo and Gomega primer

Specs use [Ginkgo](https://onsi.github.io/ginkgo/) (BDD-style structure) and
[Gomega](https://onsi.github.io/gomega/) (assertions). If you haven't used
them before, the vocabulary you'll see repeatedly:

- `Describe("...", func() { ... })` -- groups related specs (like a test
  suite section). Nest `When`/`Context` inside for sub-scenarios.
- `It("...", func() { ... })` -- one test case ("it does X").
- `BeforeEach(func() { ... })` -- runs before every `It` in the enclosing
  `Describe`/`Context`. Used here to build fresh isolation per test.
- `BeforeSuite(func() { ... })` -- runs once before any spec in the whole
  Ginkgo test binary. Used here to build the `scafctl` binary once.
- `gomega.Expect(actual).To(gomega.Equal(expected))` -- assertion style.
  Failures point at the matcher, not a raw `if err != nil { t.Fatal(...) }`.
- A Go `TestXxx(t *testing.T)` function is still required per package -- it's
  the bridge that registers Ginkgo with `go test`, but it contains almost no
  logic itself (see below).

You do not need deep Ginkgo expertise to add a spec: follow the walkthrough
and mirror an existing file.

## Directory layout

- `e2e/suite/<area>/` -- Ginkgo specs, one Go package per CLI area (e.g.
  `suite/package/` for `scafctl package`/`catalog` workflows). Each package
  has exactly one `TestXxx` + `BeforeSuite` file (the "suite entrypoint") and
  one or more `*_test.go` files with the actual `Describe` blocks.
- `e2e/internal/utils/` -- shared helpers: building the binary, isolating the
  environment, running commands, standing up a local registry, fixture paths.
- `e2e/testdata/` -- fixtures: solution YAML files, plugin source, expected
  file trees. Specs should read fixtures from here, not construct large
  inline strings, except for small solution snippets that only exist to prove
  one specific behavior (see the external-provider spec for an example of
  that exception).
- `e2e/go.mod`, `e2e/go.work` -- module and workspace files described above.

## Core helpers (`e2e/internal/utils`)

| Helper | Purpose |
| --- | --- |
| `BuildScafctl()` | Builds the `scafctl` binary once via `gexec.Build`, or reuses `$SCAFCTL_PATH` if set. Sets the package-level `ScafctlPath`. Call once, from `BeforeSuite`. |
| `NewIsolatedEnv()` | Returns an `Isolation{Root, Env}`: a fresh temp dir wired as `XDG_DATA_HOME`/`XDG_CACHE_HOME`/`XDG_CONFIG_HOME`, plus a config that disables the official remote catalog. Call from `BeforeEach` so every test gets its own sandbox. |
| `Scafctl(args...)` | Builds a command invocation (`*ExecOption`). Chain `.WithEnv(iso.Env)`, `.WithWorkDir(...)`, `.ExpectFailure()`, `.MatchStdout(...)`, `.MatchStderr(...)`, then call `.Exec()`. |
| `(*ExecOption).Exec()` | Runs the command, asserts the exit code matches expectations (0 unless `ExpectFailure()` was called), asserts any keyword matchers, and returns the `*gexec.Session` for further inspection (`session.Out.Contents()`, `session.Err.Contents()`). |
| `NewLocalOCIRegistry()` | Starts an `httptest`-backed OCI registry and returns its host:port. Use when a spec needs to push/pull/list against a "remote" catalog. Cleans itself up via `GinkgoT().Cleanup`. |
| `BuildPluginBinaryForPlatform(out, pluginDir, platform)` | Cross-compiles a plugin fixture (e.g. `linux/amd64`) for multi-platform plugin packaging tests. |
| `SolutionFixture(name)` | Resolves `e2e/testdata/solutions/<name>` to an absolute path, independent of the test's working directory. |

## Walkthrough: adding a new spec

This walks through adding one new `It` to an existing area (`suite/package/`).
Adding a whole new area follows the same shape, just with an extra suite
entrypoint file.

1. **Confirm it belongs in e2e.** Ask: does this behavior only manifest when
   the real CLI binary runs end to end (arg parsing, process exit code,
   files actually written to disk, a real registry round-trip)? If a plain
   Go unit test against the underlying package would prove the same thing,
   write that instead -- e2e tests are slower to write, run, and debug.

2. **Add or reuse a fixture.** For a solution-based test, add a folder under
   `e2e/testdata/solutions/<name>/` with a `solution.yaml` (and any templates
   or configs it references). Reuse `utils.SolutionFixture("<name>")` to get
   its path rather than hardcoding a relative path.

3. **Pick (or create) the suite file.** If you're extending an existing area,
   add your `Describe` block to an existing `*_test.go` file, or create a new
   `package_<scenario>_test.go` file in the same package -- Ginkgo suites
   support many files per package.

4. **Write the spec using the standard shape:**

   ```go
   var _ = Describe("my new workflow", func() {
       var iso utils.Isolation

       BeforeEach(func() {
           iso = utils.NewIsolatedEnv()
       })

       It("does the thing", func() {
           session := utils.Scafctl(
               "package", "solution",
               "-f", utils.SolutionFixture("my-fixture")+"/solution.yaml",
               "--version", "1.0.0",
               "-o", "json",
           ).WithEnv(iso.Env).Exec()

           out := string(session.Out.Contents())
           gomega.Expect(out).To(gomega.ContainSubstring("my-fixture"))
       })
   })
   ```

   Every scafctl invocation in the test should go through `utils.Scafctl(...)`
   with `.WithEnv(iso.Env)` -- never call `os/exec` directly, and never rely
   on the host's real `$HOME`/`$XDG_*` state.

5. **If the scenario needs a registry**, call `utils.NewLocalOCIRegistry()` in
   `BeforeEach` alongside `NewIsolatedEnv()`, and pass `--catalog
   <registryAddr>/<repo> --insecure` to the relevant `catalog`/`package`
   commands. See `package_plugin_registry_test.go` for the push+list pattern,
   and `package_solution_external_provider_test.go` for a solution that
   resolves a provider from that registry.

6. **Prefer structured output over string scraping** when the command
   supports `-o json`/`-o yaml`: unmarshal into a small local struct scoped to
   the fields the spec cares about (see `buildResult` in
   `package_solution_builtin_bundle_test.go`), rather than importing the CLI's
   internal result type.

7. **Assert on outcomes a user could observe**: stdout/stderr content, exit
   code, files written under `iso.Root` (e.g. `iso.CatalogBlobsDir()`), or
   catalog contents fetched back through `pkg/catalog`. Avoid asserting on
   internal implementation details that a user can't see.

8. **New area only:** if this is the first spec in a new CLI area, add a
   suite entrypoint (`<area>_suite_test.go`) modeled on
   `package_suite_test.go`:

   ```go
   //go:build linux || darwin

   package myareatest

   import (
       "testing"

       . "github.com/onsi/ginkgo/v2"
       "github.com/onsi/gomega"

       "github.com/oakwood-commons/scafctl/e2e/internal/utils"
   )

   func TestMyArea(t *testing.T) {
       gomega.RegisterFailHandler(Fail)
       RunSpecs(t, "My Area Suite")
   }

   var _ = BeforeSuite(func() {
       utils.BuildScafctl()
   })
   ```

   Only add the `//go:build linux || darwin` tag if the suite relies on
   Unix-only behavior (as the packaging suites do); omit it if the scenario
   is cross-platform.

9. **Run it** (see below), then run the full e2e module once more to confirm
   you haven't broken isolation for other specs (shared registries/ports are
   the most common way one spec's cleanup leaks into another).

## Running the suite

From the repo root, run everything the e2e module owns:

```bash
task test:e2e:go
```

Iterating on one package while writing a spec:

```bash
cd e2e
GOWORK=./go.work go test ./suite/package/... -run TestPackage
```

Skip rebuilding the binary on every iteration by pointing at a binary you
already built:

```bash
task build
SCAFCTL_PATH="$(pwd)/dist/scafctl" GOWORK=./e2e/go.work go test ./e2e/suite/package/...
```

## Conventions and pitfalls

- **Isolation per test, not per suite.** Call `NewIsolatedEnv()` in
  `BeforeEach`, not `BeforeSuite` -- sharing one temp dir/catalog across `It`
  blocks makes failures order-dependent and breaks `-p`/shuffle-safe runs
  (the task runs with `-shuffle=on`).
- **Don't reach into the host environment.** No real `$HOME`, no real
  `~/.scafctl`, no live network calls to the official catalog -- that's what
  `NewIsolatedEnv`'s config (`disableOfficialCatalog: true`) is for.
- **One registry per test that needs one.** `NewLocalOCIRegistry()` binds an
  ephemeral port and cleans itself up; don't try to share a registry across
  `It` blocks.
- **Build tags matter.** Packaging specs are tagged `//go:build linux ||
  darwin` because they assert on Unix-flavored behavior. Only add this tag if
  your spec actually needs it.
- **Keep fixtures minimal and named for what they test**, not for the PR that
  added them (e.g. `builtin-bundle`, not `test-case-3`).
- **Exit code assertions are automatic.** `.Exec()` already asserts exit code
  0 (or nonzero if you called `.ExpectFailure()`) -- you don't need to check
  `session.ExitCode()` yourself unless you need a specific non-zero value.