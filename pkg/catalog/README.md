# pkg/catalog (proposal)

Lightweight API for resolving and publishing catalog resources across local and remote backends.

## Key Types (Go sketch)

```go
package catalog

import (
    "context"
)

type Kind string

const (
    KindSolution  Kind = "solution"
    KindDatasource     = "datasource"
    KindRelease        = "release"
    KindProvider       = "provider"
)

type Ref struct {
    Kind       Kind
    Name       string
    Constraint string // e.g., ^1.2.0, ~1.3.2, ">=1.3.0-0"
}

type Platform struct {
    OS      string // e.g., linux, windows, darwin
    Arch    string // e.g., amd64, arm64, arm
    Variant string // optional, e.g., armv7
    Libc    string // optional, gnu or musl (Linux only)
}

type Options struct {
    IncludePrerelease bool
    Source            string   // optional source name to pin
    Platform          *Platform // for releases; nil = auto-detect from runtime
}

type Artifact struct {
    OS      string // platform fields (for releases)
    Arch    string
    Variant string
    Libc    string
    Ext     string
    Path    string
    Digest  string
    Size    int64
}

type Descriptor struct {
    Kind     Kind
    Name     string
    Version  string // resolved SemVer
    Source   string // which catalog source resolved this
    // For non-release kinds:
    Path     string // relative path to artifact
    Digest   string // sha256:...
    Size     int64
    // For releases:
    Artifact *Artifact // selected platform artifact
    Metadata map[string]any
}

// Resolver reads per-package index.json and resolves a version by constraint.
type Resolver interface {
    Resolve(ctx context.Context, ref Ref, opts Options) (Descriptor, error)
}

// Publisher writes artifacts and updates per-package index.json atomically.
type Publisher interface {
    Publish(
        ctx context.Context,
        kind Kind,
        name string,
        version string,
        files map[string]string, // srcPath -> destRelativePath (e.g., "./dist/pkg.tgz" -> "v1.2.3/package.tgz")
        meta map[string]any,
        opts Options,
    ) error
}

// Source abstracts a catalog root (local folder, bucket, HTTP read-only).
type Source interface {
    Name() string
    URI() string
    // Minimal operations the resolver/publisher need (list/get/put if writable).
}
```

## Notes

- Use Masterminds/semver v3 for constraint parsing and prerelease handling.
- Keep `Source` minimal; add capabilities via feature flags (e.g., write support).
- Implement filesystem source first; buckets next (S3/GCS/Azure) using existing httpc/fs utilities.

## Platform Detection (for releases)

When `Options.Platform` is nil, auto-detect from environment:

```go
import "runtime"

func DetectPlatform() Platform {
    p := Platform{
        OS:   runtime.GOOS,
        Arch: runtime.GOARCH,
    }
    
    // Detect libc on Linux
    if p.OS == "linux" {
        p.Libc = detectLibc() // check for musl vs gnu
    }
    
    return p
}

func detectLibc() string {
    // Read /proc/self/exe or check for musl signatures
    // Default to "gnu" if unsure
    return "gnu"
}
```

Resolution selection logic for releases:

1. Filter artifacts by exact match: `(os, arch, variant, libc)` if all specified
2. If not found, try `(os, arch)` match ignoring variant/libc
3. If not found, check for a universal artifact (`os: "any"`, `arch: "any"`)
4. Return error with available platforms listed

This allows fallback chains like:
- Request `linux/amd64/musl` → prefer musl → fallback to generic `linux/amd64` → fallback to universal
- Windows/Mac usually omit libc/variant, simplifying to `(os, arch)` match
