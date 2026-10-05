// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import "fmt"

// tagForLocalRef derives the OCI tag for ref. Artifacts cached from a remote embed
// the remote host and use the plural kind (matching remote repository paths);
// locally built artifacts use the singular kind. Versions are joined with ":"
// and digests with "@". The reference must carry a version or digest.
//
// This is the write-side derivation: a ref with no origin is stored as built,
// because the persisted annotations can only say built or remote. Reads that
// must not assume an origin gate on canGenerateTag first.
func tagForLocalRef(ref Reference) (string, error) {
	if err := validateIdentity(ref); err != nil {
		return "", err
	}

	return buildLocalTag(ref), nil
}

// writeRef returns ref with its origin made explicit for a write: a ref with no
// remote is stored as built (see tagForLocalRef). Store paths call it before
// their existence check so the check is an exact lookup of the tag about to be
// written, rather than an any-origin search that a pulled copy of the same
// name@version would match.
func writeRef(ref Reference) Reference {
	if ref.Remote == "" {
		ref.Built = true
	}
	return ref
}

// validateIdentity checks the coordinates every tag needs: kind, name, and a
// version or digest.
func validateIdentity(ref Reference) error {
	if ref.Kind == "" || ref.Name == "" {
		return &InvalidReferenceError{
			Input:   ref.String(),
			Message: "reference must have kind and name",
		}
	}
	if !ref.HasVersion() && !ref.HasDigest() {
		return &InvalidReferenceError{
			Input:   ref.String(),
			Message: "reference has no version or digest",
		}
	}
	return nil
}

// canGenerateTag is the read-side gate: the ref must carry full identity,
// including an explicit origin, for its derived tag to be a lookup rather than
// a guess. An origin-less ref means "any origin" and must be searched instead.
func canGenerateTag(ref Reference) error {
	if err := validateIdentity(ref); err != nil {
		return err
	}
	if ref.Remote == "" && !ref.Built {
		return &InvalidReferenceError{
			Input:   ref.String(),
			Message: "reference must be either built or have a remote origin",
		}
	}
	return nil
}

func buildLocalTag(ref Reference) string {
	var repo string
	if ref.Remote != "" {
		repo = fmt.Sprintf("%s/%s/%s", ref.Remote, ref.Kind.Plural(), ref.Name)
	} else {
		repo = fmt.Sprintf("%s/%s", ref.Kind, ref.Name)
	}

	switch {
	case ref.HasVersion():
		return repo + ":" + ref.Version.String()
	case ref.HasDigest():
		return repo + "@" + ref.Digest
	default:
		return repo
	}
}

func queryFromRef(ref Reference) ArtifactQuery {
	q := ArtifactQuery{Name: ref.Name, Kind: ref.Kind, Digest: ref.Digest}
	if ref.HasVersion() {
		q.Version = ref.Version.String()
	}
	switch {
	case ref.Remote != "":
		q.Source = SourceSelector{Kind: SourceRemote, Remote: ref.Remote}
	case ref.Built:
		q.Source = SourceSelector{Kind: SourceBuilt}
	}
	return q
}
