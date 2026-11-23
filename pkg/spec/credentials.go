// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package spec

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// TokenRef describes an authentication token request resolved via an auth handler.
// In YAML and JSON it supports both a string shorthand ("github") and an object
// form ({handler: "gcp", scope: "..."}).
type TokenRef struct {
	Handler     string `json:"handler" yaml:"handler" doc:"Auth handler name" maxLength:"128" pattern:"^[a-z0-9][a-z0-9-]*$" patternDescription:"Lowercase alphanumeric and hyphens" example:"github"`
	Scope       string `json:"scope,omitempty" yaml:"scope,omitempty" doc:"OAuth2/OIDC scope(s) for handlers supporting per-request scopes" maxLength:"2048" example:"https://graph.microsoft.com/.default"`
	Profile     string `json:"profile,omitempty" yaml:"profile,omitempty" doc:"Named auth profile override" maxLength:"128" example:"work"`
	Hostname    string `json:"hostname,omitempty" yaml:"hostname,omitempty" doc:"Target cluster/server hostname or alias (requires token_hostname capability)" maxLength:"512" example:"prod-east"`
	MinValidFor string `json:"minValidFor,omitempty" yaml:"minValidFor,omitempty" doc:"Minimum remaining validity duration (e.g. 5m, 30s)" maxLength:"64" example:"5m"`
}

type rawTokenRef TokenRef

var knownTokenRefKeys = map[string]struct{}{
	"handler":     {},
	"scope":       {},
	"profile":     {},
	"hostname":    {},
	"minValidFor": {},
}

// IsShorthand reports whether only Handler is populated.
func (t TokenRef) IsShorthand() bool {
	return t.Scope == "" && t.Profile == "" && t.Hostname == "" && t.MinValidFor == ""
}

// Summary returns a concise symbolic label safe for dry-run and preview output.
func (t TokenRef) Summary() string {
	return "tkn:" + t.Handler
}

// UnmarshalYAML decodes either a scalar handler name or a strict TokenRef mapping.
func (t *TokenRef) UnmarshalYAML(node *yaml.Node) error {
	*t = TokenRef{}

	switch node.Kind {
	case yaml.ScalarNode:
		if node.Tag != "!!str" {
			return fmt.Errorf("invalid token ref at line %d, column %d: expected string or mapping, got %s", node.Line, node.Column, node.Tag)
		}
		if strings.TrimSpace(node.Value) == "" {
			return fmt.Errorf("invalid token ref at line %d, column %d: handler must not be empty", node.Line, node.Column)
		}
		t.Handler = node.Value
		return nil

	case yaml.MappingNode:
		for i := 0; i+1 < len(node.Content); i += 2 {
			keyNode := node.Content[i]
			valNode := node.Content[i+1]
			if _, ok := knownTokenRefKeys[keyNode.Value]; !ok {
				return fmt.Errorf("invalid token ref at line %d, column %d: unknown field %q", keyNode.Line, keyNode.Column, keyNode.Value)
			}
			if valNode.Kind != yaml.ScalarNode || valNode.Tag != "!!str" {
				return fmt.Errorf("invalid token ref at line %d, column %d: field %q must be a string", valNode.Line, valNode.Column, keyNode.Value)
			}
		}
		var raw rawTokenRef
		if err := node.Decode(&raw); err != nil {
			return fmt.Errorf("invalid token ref at line %d, column %d: %w", node.Line, node.Column, err)
		}
		if strings.TrimSpace(raw.Handler) == "" {
			return fmt.Errorf("invalid token ref at line %d, column %d: handler must not be empty", node.Line, node.Column)
		}
		*t = TokenRef(raw)
		return nil

	case yaml.DocumentNode, yaml.SequenceNode, yaml.AliasNode:
		return fmt.Errorf("invalid token ref at line %d, column %d: expected string or mapping", node.Line, node.Column)

	default:
		return fmt.Errorf("invalid token ref at line %d, column %d: expected string or mapping", node.Line, node.Column)
	}
}

// MarshalYAML encodes shorthand TokenRefs as scalar strings and full TokenRefs as mappings.
func (t TokenRef) MarshalYAML() (any, error) {
	if t.IsShorthand() {
		return t.Handler, nil
	}
	return rawTokenRef(t), nil
}

// UnmarshalJSON decodes either a JSON string or a strict JSON object into TokenRef.
func (t *TokenRef) UnmarshalJSON(data []byte) error {
	*t = TokenRef{}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return fmt.Errorf("invalid token ref: null value")
	}

	switch trimmed[0] {
	case '"':
		var handler string
		if err := json.Unmarshal(trimmed, &handler); err != nil {
			return fmt.Errorf("invalid token ref: %w", err)
		}
		if strings.TrimSpace(handler) == "" {
			return fmt.Errorf("invalid token ref: handler must not be empty")
		}
		t.Handler = handler
		return nil

	case '{':
		// Reject non-string field values explicitly (json.Unmarshal rejects type
		// mismatches into string fields, except null which unmarshals as "").
		var rawFields map[string]json.RawMessage
		if err := json.Unmarshal(trimmed, &rawFields); err != nil {
			return fmt.Errorf("invalid token ref: %w", err)
		}
		for k, v := range rawFields {
			if _, ok := knownTokenRefKeys[k]; !ok {
				return fmt.Errorf("invalid token ref: unknown field %q", k)
			}
			vt := bytes.TrimSpace(v)
			if len(vt) == 0 || vt[0] != '"' {
				return fmt.Errorf("invalid token ref: field %q must be a string", k)
			}
		}

		dec := json.NewDecoder(bytes.NewReader(trimmed))
		dec.DisallowUnknownFields()
		var raw rawTokenRef
		if err := dec.Decode(&raw); err != nil {
			return fmt.Errorf("invalid token ref: %w", err)
		}
		if strings.TrimSpace(raw.Handler) == "" {
			return fmt.Errorf("invalid token ref: handler must not be empty")
		}
		*t = TokenRef(raw)
		return nil

	default:
		return fmt.Errorf("invalid token ref: expected string or object")
	}
}

// MarshalJSON encodes shorthand TokenRefs as JSON strings and full TokenRefs as JSON objects.
func (t TokenRef) MarshalJSON() ([]byte, error) {
	if t.IsShorthand() {
		return json.Marshal(t.Handler)
	}
	return json.Marshal(rawTokenRef(t))
}

func parseTokenRefFromAny(v any) (*TokenRef, error) {
	switch val := v.(type) {
	case string:
		if strings.TrimSpace(val) == "" {
			return nil, fmt.Errorf("tkn handler must not be empty")
		}
		return &TokenRef{Handler: val}, nil

	case map[string]any:
		var tr TokenRef
		for k, rawVal := range val {
			if _, ok := knownTokenRefKeys[k]; !ok {
				return nil, fmt.Errorf("tkn unknown field %q", k)
			}
			s, ok := rawVal.(string)
			if !ok {
				return nil, fmt.Errorf("tkn field %q must be a string, got %T", k, rawVal)
			}
			switch k {
			case "handler":
				tr.Handler = s
			case "scope":
				tr.Scope = s
			case "profile":
				tr.Profile = s
			case "hostname":
				tr.Hostname = s
			case "minValidFor":
				tr.MinValidFor = s
			}
		}
		if strings.TrimSpace(tr.Handler) == "" {
			return nil, fmt.Errorf("tkn handler must not be empty")
		}
		return &tr, nil

	default:
		return nil, fmt.Errorf("tkn value must be a string or object, got %T", v)
	}
}

// CredentialResolver resolves host secrets and auth-handler tokens for ValueRef
// evaluation without introducing auth/secrets package dependencies into pkg/spec.
type CredentialResolver interface {
	ResolveSecret(ctx context.Context, name string) (string, error)
	ResolveToken(ctx context.Context, req TokenRef) (string, error)
}

type credentialResolverContextKey struct{}

// WithCredentialResolver attaches a CredentialResolver to ctx.
func WithCredentialResolver(ctx context.Context, resolver CredentialResolver) context.Context {
	if resolver == nil {
		return ctx
	}
	return context.WithValue(ctx, credentialResolverContextKey{}, resolver)
}

// CredentialResolverFromContext returns the CredentialResolver stored in ctx, or nil.
func CredentialResolverFromContext(ctx context.Context) CredentialResolver {
	if ctx == nil {
		return nil
	}
	r, _ := ctx.Value(credentialResolverContextKey{}).(CredentialResolver)
	return r
}

type placeholderCredentialResolver struct{}

func (placeholderCredentialResolver) ResolveSecret(_ context.Context, name string) (string, error) {
	if strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("secret name must not be empty")
	}
	return fmt.Sprintf("<redacted:scrt:%s>", name), nil
}

func (placeholderCredentialResolver) ResolveToken(_ context.Context, req TokenRef) (string, error) {
	if strings.TrimSpace(req.Handler) == "" {
		return "", fmt.Errorf("token handler must not be empty")
	}
	return fmt.Sprintf("<redacted:tkn:%s>", req.Handler), nil
}

// PlaceholderCredentialResolver returns a side-effect-free CredentialResolver
// for render, dry-run, and preview paths. It emits symbolic placeholder strings
// (<redacted:scrt:<name>> and <redacted:tkn:<handler>>) without touching
// keyrings or minting live tokens.
func PlaceholderCredentialResolver() CredentialResolver {
	return placeholderCredentialResolver{}
}
