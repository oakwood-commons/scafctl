// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import "time"

// descriptorOnlyAnnotations are local provenance keys. They describe how this
// catalog obtained an artifact, not what the artifact is, so they live on the
// tagged descriptor (index.json) and never enter a content blob (manifest,
// index, or config), where they would change the digest.
var descriptorOnlyAnnotations = []string{AnnotationSourceCanonical, AnnotationOrigin}

// DropAnnotations returns a transformer that removes the listed keys
// (a denylist). Every other key is kept.
func DropAnnotations(keys ...string) AnnotationTransformer {
	drop := keySet(keys)
	return func(annotations map[string]string) map[string]string {
		out := make(map[string]string, len(annotations))
		for k, v := range annotations {
			if _, ok := drop[k]; !ok {
				out[k] = v
			}
		}
		return out
	}
}

func keySet(keys []string) map[string]struct{} {
	set := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		set[k] = struct{}{}
	}
	return set
}

// contentIdentity stamps the identity a content blob carries: artifact type,
// name, version, and creation time, all derived from ref. The ref always wins
// over the same keys supplied as caller metadata, so the blob can never claim
// an identity other than the one it is built and tagged as.
func contentIdentity(ref Reference, createdAt time.Time, annotations map[string]string) map[string]string {
	out := copyAnnotations(annotations)
	out[AnnotationArtifactType] = ref.Kind.String()
	out[AnnotationArtifactName] = ref.Name
	if ref.HasVersion() {
		out[AnnotationVersion] = ref.Version.String()
	} else {
		delete(out, AnnotationVersion)
	}
	out[AnnotationCreated] = formatCreated(createdAt)
	return out
}

// stripDescriptorOnly removes local provenance keys from annotations bound for
// a content blob. It is the last step of every builder's pipeline, so no
// caller metadata or transformer can put provenance into a digest.
func stripDescriptorOnly(annotations map[string]string) map[string]string {
	return DropAnnotations(descriptorOnlyAnnotations...)(annotations)
}

// localProvenance stamps the annotations a local tag descriptor carries: the
// artifact identity from ref, plus AnnotationSourceCanonical set to ref.Remote
// for a remote-cached artifact and removed for a built one. Provenance is
// binary, so an inherited canonical from a previous origin is never kept.
func localProvenance(ref Reference, annotations map[string]string) map[string]string {
	out := copyAnnotations(annotations)
	out[AnnotationArtifactType] = ref.Kind.String()
	out[AnnotationArtifactName] = ref.Name
	if ref.HasVersion() {
		out[AnnotationVersion] = ref.Version.String()
	}
	if ref.Remote != "" {
		out[AnnotationSourceCanonical] = ref.Remote
	} else {
		delete(out, AnnotationSourceCanonical)
	}
	return out
}

func formatCreated(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

func mergeAnnotations(base, overlay map[string]string) map[string]string {
	merged := make(map[string]string)
	for k, v := range base {
		merged[k] = v
	}
	for k, v := range overlay {
		merged[k] = v
	}
	return merged
}
