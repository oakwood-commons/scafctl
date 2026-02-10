# Catalog CLI Design Decision: Push/Pull Reference Patterns

**Status:** Decided — Option B  
**Created:** 2026-02-06  
**Decided:** 2026-02-07  
**Decision:** Keep current `--catalog` flag approach with config-based default catalog resolution.

---

## Context

Currently, `scafctl catalog push` requires a `--catalog` flag to specify the target registry:

```bash
scafctl catalog push my-solution@1.0.0 --catalog ghcr.io/myorg
# Pushes to: ghcr.io/myorg/solutions/my-solution:1.0.0
```

This differs from docker/podman, which uses full image names:

```bash
docker tag my-image ghcr.io/myorg/my-image:1.0.0
docker push ghcr.io/myorg/my-image:1.0.0
```

We need to decide if we should align with the docker pattern, keep the current approach, or support both.

---

## Key Difference: Multiple Artifact Kinds

Unlike Docker (which only has container images), scafctl has three artifact kinds:

| Kind | Repository Path Suffix |
|------|------------------------|
| `solution` | `/solutions/` |
| `provider` | `/providers/` |
| `auth-handler` | `/auth-handlers/` |

This is why we inject the kind into the path. The question is: **how should users specify this?**

---

## How We Identify Artifact Kind

We have **two mechanisms** to determine artifact kind:

### 1. Path-Based Detection

Parse the URL path to extract kind:
- `ghcr.io/myorg/solutions/my-solution:1.0.0` → kind = `solution`
- `ghcr.io/myorg/providers/aws:1.0.0` → kind = `provider`

### 2. Metadata-Based Detection

Read kind from OCI manifest annotations:

```json
{
  "annotations": {
    "dev.scafctl.artifact.type": "solution"
  }
}
```

This is stored when we push artifacts and can be fetched by reading the manifest (without pulling all layers).

---

## Options

### Option A: Docker-Style Tagging

Add a `scafctl catalog tag` command to create full references:

```bash
# Build locally
scafctl build solution deploy.yaml --version 1.0.0

# Tag with full remote reference
scafctl catalog tag deploy@1.0.0 ghcr.io/myorg/solutions/deploy:1.0.0

# Push using full name (no --catalog needed)
scafctl catalog push ghcr.io/myorg/solutions/deploy:1.0.0

# Pull using full name
scafctl catalog pull ghcr.io/myorg/solutions/deploy:1.0.0
```

| Pros | Cons |
|------|------|
| Familiar to Docker users | Extra step (tag before push) |
| Full name is explicit - no surprises | User must know to include `/solutions/` in path |
| Works for any registry path structure | More typing |
| Consistent push/pull syntax | Requires new `tag` command |

---

### Option B: Keep Current Approach + Config Default

Keep `--catalog` flag but allow setting a default in config:

```bash
# Set default catalog in config
scafctl config set catalog ghcr.io/myorg

# Push (uses configured catalog, infers kind from local artifact metadata)
scafctl catalog push deploy@1.0.0

# Or override with --catalog
scafctl catalog push deploy@1.0.0 --catalog ghcr.io/other-org

# Pull still uses full reference
scafctl catalog pull ghcr.io/myorg/solutions/deploy@1.0.0
```

| Pros | Cons |
|------|------|
| Fewer flags after initial setup | Config-dependent behavior can be confusing |
| Kind is always correct (from metadata) | Still different from docker pattern |
| Simple for users who push to same registry | Asymmetric push/pull syntax |
| No new commands needed | |

---

### Option C: Hybrid - Support Both Patterns

Support both short names (with `--catalog`) and full remote references:

```bash
# Pattern 1: Short name + catalog flag (current)
scafctl catalog push deploy@1.0.0 --catalog ghcr.io/myorg
# → ghcr.io/myorg/solutions/deploy:1.0.0

# Pattern 2: Full remote reference (docker-style)
scafctl catalog push ghcr.io/myorg/solutions/deploy:1.0.0
# → Direct push, no --catalog needed

# Pull works with full references (already implemented)
scafctl catalog pull ghcr.io/myorg/solutions/deploy:1.0.0
```

**Detection logic:**
- If reference contains `/` and looks like a URL → treat as full remote reference
- If reference is a bare name → require `--catalog` flag

| Pros | Cons |
|------|------|
| Supports both mental models | Two ways to do the same thing |
| Docker users can use familiar pattern | More complex documentation |
| Existing users don't need to change | Implementation complexity |
| Graceful transition path | |

---

### Option D: Metadata-First (No Path Convention Required)

Allow pushing to any path, detect kind from manifest metadata:

```bash
# Push to custom path (kind detected from local catalog metadata)
scafctl catalog push deploy@1.0.0 --catalog ghcr.io/myorg/custom-path
# → ghcr.io/myorg/custom-path/deploy:1.0.0 (no /solutions/ injected)

# Pull from custom path (kind detected by fetching manifest)
scafctl catalog pull ghcr.io/myorg/custom-path/deploy:1.0.0
# → Fetches manifest, reads dev.scafctl.artifact.type annotation
```

| Pros | Cons |
|------|------|
| Maximum flexibility | Requires extra network call on pull |
| No path conventions required | Can't tell artifact type from URL alone |
| Works with existing registries | Slightly slower pulls |
| Future-proof for new artifact types | |

---

## Comparison Matrix

| Criteria | Option A (Docker-style) | Option B (Config default) | Option C (Hybrid) | Option D (Metadata-first) |
|----------|-------------------------|---------------------------|-------------------|---------------------------|
| Docker familiarity | ✅ High | ❌ Low | ✅ High | ⚠️ Medium |
| Learning curve | ⚠️ Medium | ✅ Low | ⚠️ Medium | ✅ Low |
| Typing required | ❌ Most | ✅ Least | ⚠️ Flexible | ⚠️ Medium |
| Path flexibility | ✅ Any path | ❌ Requires convention | ⚠️ Partial | ✅ Any path |
| Implementation effort | ⚠️ Medium | ✅ Low | ⚠️ Medium | ⚠️ Medium |
| Backward compatible | ❌ Breaking | ✅ Yes | ✅ Yes | ⚠️ Partial |

---

## Questions to Discuss

1. **How important is Docker/Podman CLI familiarity?**
   - Are our users primarily Docker users who expect that pattern?
   - Or are they new to OCI artifacts?

2. **Should we enforce the `/solutions/`, `/providers/`, `/auth-handlers/` path structure?**
   - Pro: Predictable, self-documenting URLs
   - Con: Limits flexibility, may conflict with existing registry structures

3. **Is an extra network call on pull acceptable for flexibility?**
   - Fetching manifest to detect kind adds ~100-200ms
   - Could be cached

4. **Do we want a `scafctl catalog tag` command regardless?**
   - Useful for aliasing (e.g., `my-solution@1.0.0` → `my-solution:latest`)
   - Docker has this, but it's a separate decision

5. **Should `scafctl run solution` support direct remote references?**
   - e.g., `scafctl run solution ghcr.io/myorg/solutions/deploy@1.0.0`
   - Would pull on-demand if not in local catalog

---

## Recommendation

**Option B was chosen.**

---

## Decision

**Option B: Keep Current Approach + Config Default**

### Rationale

Option B offers the lowest learning curve, least typing after initial setup, and requires no new commands. It integrates naturally with the existing `scafctl config add-catalog` / `scafctl config use-catalog` CLI. Kind inference from local catalog metadata ensures correctness without user-facing complexity.

### Implementation Summary

The `--catalog` flag on `push` and `delete` (remote mode) now:

1. **Accepts a URL** (e.g., `ghcr.io/myorg`) — used directly
2. **Accepts a catalog name** (e.g., `myregistry`) — looked up in config
3. **Falls back to the default catalog** from config when omitted

Resolution logic (in `resolveCatalogURL`):
- If the value contains `.` or `:` → it's a URL, use directly
- If it's a plain string → look up from `catalogs[]` in config
- If empty → use `settings.defaultCatalog` from config

### User Workflows

```bash
# One-time setup: configure a default catalog
scafctl config add-catalog ghcr --type oci --url ghcr.io/myorg --default

# Push (uses default catalog, infers kind from local metadata)
scafctl catalog push deploy@1.0.0

# Push with explicit catalog name
scafctl catalog push deploy@1.0.0 --catalog ghcr

# Push with ad-hoc URL (no config needed)
scafctl catalog push deploy@1.0.0 --catalog ghcr.io/other-org

# Pull uses full reference (unchanged)
scafctl catalog pull ghcr.io/myorg/solutions/deploy@1.0.0

# Delete with --catalog flag
scafctl catalog delete deploy@1.0.0 --catalog ghcr
```

### Files Changed

- `pkg/cmd/scafctl/catalog/resolve.go` — new `resolveCatalogURL()` helper
- `pkg/cmd/scafctl/catalog/resolve_test.go` — tests for URL resolution
- `pkg/cmd/scafctl/catalog/push.go` — `--catalog` is now optional
- `pkg/cmd/scafctl/catalog/delete.go` — added `--catalog` flag for remote delete