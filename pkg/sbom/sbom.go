// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package sbom generates SPDX SBOM documents for scafctl catalog artifacts.
package sbom

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/solution"
)

// MediaType is the IANA media type for SPDX JSON SBOM documents.
const MediaType = "application/spdx+json"

// Document represents an SPDX 2.3 JSON document.
type Document struct {
	SPDXVersion    string          `json:"spdxVersion"`
	DataLicense    string          `json:"dataLicense"`
	SPDXID         string          `json:"SPDXID"`
	Name           string          `json:"name"`
	Namespace      string          `json:"documentNamespace"`
	CreationInfo   CreationInfo    `json:"creationInfo"`
	Packages       []Package       `json:"packages"`
	Relationships  []Relationship  `json:"relationships,omitempty"`
	ExtractedTexts []ExtractedText `json:"hasExtractedLicensingInfos,omitempty"`
}

// CreationInfo contains metadata about when and how the SBOM was created.
type CreationInfo struct {
	Created  string   `json:"created"`
	Creators []string `json:"creators"`
}

// Package represents an SPDX package (the solution or a dependency).
type Package struct {
	SPDXID           string           `json:"SPDXID"`
	Name             string           `json:"name"`
	Version          string           `json:"versionInfo,omitempty"`
	Supplier         string           `json:"supplier,omitempty"`
	DownloadLocation string           `json:"downloadLocation"`
	FilesAnalyzed    bool             `json:"filesAnalyzed"`
	Checksums        []Checksum       `json:"checksums,omitempty"`
	ExternalRefs     []ExternalRef    `json:"externalRefs,omitempty"`
	Description      string           `json:"description,omitempty"`
	PrimaryPurpose   string           `json:"primaryPackagePurpose,omitempty"`
	Annotations      []AnnotationSPDX `json:"annotations,omitempty"`
}

// Checksum is a hash for an SPDX package.
type Checksum struct {
	Algorithm string `json:"algorithm"`
	Value     string `json:"checksumValue"`
}

// ExternalRef links to an external identifier (e.g., purl).
type ExternalRef struct {
	Category string `json:"referenceCategory"`
	Type     string `json:"referenceType"`
	Locator  string `json:"referenceLocator"`
}

// Relationship describes a dependency or composition link.
type Relationship struct {
	Element string `json:"spdxElementId"`
	Type    string `json:"relationshipType"`
	Related string `json:"relatedSpdxElement"`
}

// AnnotationSPDX is a free-form annotation on a package.
type AnnotationSPDX struct {
	Annotator string `json:"annotator"`
	Date      string `json:"annotationDate"`
	Type      string `json:"annotationType"`
	Comment   string `json:"comment"`
}

// ExtractedText holds extracted license text for custom licenses.
type ExtractedText struct {
	ID   string `json:"licenseId"`
	Name string `json:"name"`
	Text string `json:"extractedText"`
}

// GenerateOptions configures SBOM generation.
type GenerateOptions struct {
	// Namespace is the SPDX document namespace URI.
	// If empty, a default based on the solution name and version is used.
	Namespace string

	// ContentDigest is the sha256 digest of the primary content layer.
	ContentDigest string

	// BundleDigest is the sha256 digest of the bundle layer (empty if no bundle).
	BundleDigest string

	// BinaryName is the tool name (e.g., "scafctl") for the creator field.
	BinaryName string
}

// Generate creates an SPDX 2.3 JSON SBOM document from a solution.
func Generate(sol *solution.Solution, opts GenerateOptions) ([]byte, error) {
	if sol == nil {
		return nil, fmt.Errorf("solution must not be nil")
	}

	name := sol.Metadata.Name
	version := ""
	if sol.Metadata.Version != nil {
		version = sol.Metadata.Version.String()
	}

	binaryName := opts.BinaryName
	if binaryName == "" {
		binaryName = "scafctl"
	}

	namespace := opts.Namespace
	if namespace == "" {
		// Build a deterministic namespace from stable inputs (name, version, content digest)
		// so identical content produces identical SBOMs.
		hashInput := name + version + opts.ContentDigest + opts.BundleDigest
		namespace = fmt.Sprintf("https://spdx.org/spdxdocs/%s-%s-%s",
			binaryName, name, shortHash(hashInput))
	}

	now := time.Now().UTC().Format(time.RFC3339)

	doc := Document{
		SPDXVersion: "SPDX-2.3",
		DataLicense: "CC0-1.0",
		SPDXID:      "SPDXRef-DOCUMENT",
		Name:        fmt.Sprintf("%s-%s", name, version),
		Namespace:   namespace,
		CreationInfo: CreationInfo{
			Created:  now,
			Creators: []string{fmt.Sprintf("Tool: %s", binaryName)},
		},
	}

	// Root package — the solution itself
	rootPkg := Package{
		SPDXID:           "SPDXRef-Solution",
		Name:             name,
		Version:          version,
		DownloadLocation: "NOASSERTION",
		FilesAnalyzed:    false,
		PrimaryPurpose:   "APPLICATION",
		Description:      sol.Metadata.Description,
	}

	if opts.ContentDigest != "" {
		rootPkg.Checksums = append(rootPkg.Checksums, Checksum{
			Algorithm: "SHA256",
			Value:     normalizeDigest(opts.ContentDigest),
		})
	}

	doc.Packages = append(doc.Packages, rootPkg)
	doc.Relationships = append(doc.Relationships, Relationship{
		Element: "SPDXRef-DOCUMENT",
		Type:    "DESCRIBES",
		Related: "SPDXRef-Solution",
	})

	// Bundle as a sub-package if present
	if opts.BundleDigest != "" {
		bundlePkg := Package{
			SPDXID:           "SPDXRef-Bundle",
			Name:             name + "-bundle",
			Version:          version,
			DownloadLocation: "NOASSERTION",
			FilesAnalyzed:    false,
			PrimaryPurpose:   "APPLICATION",
			Description:      "Bundled files for the solution",
		}
		bundlePkg.Checksums = append(bundlePkg.Checksums, Checksum{
			Algorithm: "SHA256",
			Value:     normalizeDigest(opts.BundleDigest),
		})
		doc.Packages = append(doc.Packages, bundlePkg)
		doc.Relationships = append(doc.Relationships, Relationship{
			Element: "SPDXRef-Solution",
			Type:    "CONTAINS",
			Related: "SPDXRef-Bundle",
		})
	}

	// Resolver providers as dependencies
	if sol.Spec.HasResolvers() {
		seen := make(map[string]bool)
		for rName, r := range sol.Spec.Resolvers {
			if r == nil || r.Resolve == nil {
				continue
			}
			for _, src := range r.Resolve.With {
				providerName := src.Provider
				if providerName == "" || seen[providerName] {
					continue
				}
				seen[providerName] = true

				spdxID := fmt.Sprintf("SPDXRef-Provider-%s", sanitizeSPDXID(providerName))
				pkg := Package{
					SPDXID:           spdxID,
					Name:             providerName,
					DownloadLocation: "NOASSERTION",
					FilesAnalyzed:    false,
					PrimaryPurpose:   "LIBRARY",
					Description:      fmt.Sprintf("Provider used by resolver %q", rName),
				}
				doc.Packages = append(doc.Packages, pkg)
				doc.Relationships = append(doc.Relationships, Relationship{
					Element: "SPDXRef-Solution",
					Type:    "DEPENDS_ON",
					Related: spdxID,
				})
			}
		}
	}

	// Plugin dependencies
	for _, plugin := range sol.Bundle.Plugins {
		spdxID := fmt.Sprintf("SPDXRef-Plugin-%s", sanitizeSPDXID(plugin.Name))
		pkg := Package{
			SPDXID:           spdxID,
			Name:             plugin.Name,
			Version:          plugin.Version,
			DownloadLocation: "NOASSERTION",
			FilesAnalyzed:    false,
			PrimaryPurpose:   "LIBRARY",
			Description:      fmt.Sprintf("Plugin dependency (kind: %s)", plugin.Kind),
		}
		doc.Packages = append(doc.Packages, pkg)
		doc.Relationships = append(doc.Relationships, Relationship{
			Element: "SPDXRef-Solution",
			Type:    "DEPENDS_ON",
			Related: spdxID,
		})
	}

	return json.MarshalIndent(doc, "", "  ")
}

// shortHash returns a short hex hash for namespace uniqueness.
func shortHash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:8])
}

// sanitizeSPDXID replaces characters not allowed in SPDX identifiers.
func sanitizeSPDXID(s string) string {
	out := make([]byte, 0, len(s))
	for i := range len(s) {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '.' {
			out = append(out, c)
		} else {
			out = append(out, '-')
		}
	}
	return string(out)
}

// normalizeDigest strips the "sha256:" prefix from OCI-style digests,
// returning only the raw hex value required by SPDX checksum fields.
func normalizeDigest(d string) string {
	return strings.TrimPrefix(d, "sha256:")
}
