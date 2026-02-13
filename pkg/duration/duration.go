// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package duration provides a Duration type that supports string-based and
// numeric YAML/JSON marshalling. It wraps time.Duration and is intended for
// use in any configuration struct that needs human-readable durations like
// "30s", "2m", or "1m30s". Numeric values (nanoseconds) are also accepted
// during unmarshalling for backward compatibility.
package duration

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gopkg.in/yaml.v3"
)

// Duration is a time.Duration with string-based YAML/JSON marshalling.
// Supports Go duration strings like "30s", "2m", "1m30s".
// Numeric values (nanoseconds) are accepted during unmarshalling.
type Duration struct {
	time.Duration
}

// New creates a Duration from a time.Duration.
func New(d time.Duration) Duration {
	return Duration{Duration: d}
}

// NewPtr creates a *Duration from a time.Duration.
func NewPtr(d time.Duration) *Duration {
	return &Duration{Duration: d}
}

// UnmarshalYAML implements yaml.Unmarshaler for Duration.
// Accepts both string durations ("30s") and integer nanoseconds (1000000000).
func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	// Try integer (nanoseconds) first
	var n int64
	if err := value.Decode(&n); err == nil {
		d.Duration = time.Duration(n)
		return nil
	}

	// Otherwise, try as a string
	var s string
	if err := value.Decode(&s); err != nil {
		return fmt.Errorf("duration must be a string or integer: %w", err)
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	d.Duration = parsed
	return nil
}

// MarshalYAML implements yaml.Marshaler for Duration.
func (d Duration) MarshalYAML() (any, error) {
	return d.String(), nil
}

// UnmarshalJSON implements json.Unmarshaler for Duration.
// Accepts both string durations ("30s") and numeric nanoseconds (1000000000).
func (d *Duration) UnmarshalJSON(data []byte) error {
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	switch value := v.(type) {
	case float64:
		d.Duration = time.Duration(value)
		return nil
	case string:
		parsed, err := time.ParseDuration(value)
		if err != nil {
			return fmt.Errorf("invalid duration %q: %w", value, err)
		}
		d.Duration = parsed
		return nil
	default:
		return errors.New("duration must be a string or number")
	}
}

// MarshalJSON implements json.Marshaler for Duration.
func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.String())
}
