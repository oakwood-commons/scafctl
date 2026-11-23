// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package redact provides run-scoped secret tracking and scrubbing across
// streaming terminal output, provider results, errors, and GitHub Actions logs.
// It intentionally depends only on the Go standard library so leaf packages
// such as pkg/spec can register resolved secrets without import cycles.
package redact

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
)

const (
	// RedactedPlaceholder replaces scrubbed secret substrings.
	RedactedPlaceholder = "[REDACTED]"

	// MinSecretLength is the minimum byte length for tracked secrets and
	// individual lines extracted from multi-line credentials (such as PEM keys
	// or JSON service-account credentials). Very short strings (e.g. "{" or "a")
	// would otherwise corrupt structured output.
	MinSecretLength = 4

	maxRedactDepth = 32
)

type (
	trackerContextKey       struct{}
	suppressMasksContextKey struct{}
)

// Tracker records secret values resolved during a run and scrubs them from
// strings, structured maps/slices, errors, and streaming output.
// All methods are safe for concurrent use, and nil-receiver methods act as
// safe no-ops returning inputs unchanged.
type Tracker struct {
	mu            sync.RWMutex
	parent        *Tracker
	hadCredential bool
	seen          map[string]struct{}
	masked        map[string]struct{}
	sorted        []string // sorted longest-first, then lexicographically
	replacer      *strings.Replacer
	maxLen        int
	hasMulti      bool
}

// NewTracker creates an empty secret Tracker.
func NewTracker() *Tracker {
	return &Tracker{
		seen:   make(map[string]struct{}),
		masked: make(map[string]struct{}),
	}
}

// NewChildTracker creates a scoped view backed by parent's shared secret set.
// Any credential registered through the child is stored on the root tracker
// while also marking HadCredential() on the child, allowing callers (such as
// per-resolver execution) to detect whether that specific step resolved a
// credential even when the secret is shorter than MinSecretLength.
func NewChildTracker(parent *Tracker) *Tracker {
	root := parent.Root()
	if root == nil {
		root = NewTracker()
	}
	return &Tracker{
		parent: root,
	}
}

// Root returns the underlying root Tracker, or nil if t is nil.
func (t *Tracker) Root() *Tracker {
	if t == nil {
		return nil
	}
	for t.parent != nil {
		t = t.parent
	}
	return t
}

// HadCredential reports whether Track was called with a non-empty secret on
// this specific Tracker instance.
func (t *Tracker) HadCredential() bool {
	if t == nil {
		return false
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.hadCredential
}

// WithTracker attaches a Tracker to ctx.
func WithTracker(ctx context.Context, t *Tracker) context.Context {
	if t == nil {
		return ctx
	}
	return context.WithValue(ctx, trackerContextKey{}, t)
}

// FromContext returns the Tracker stored in ctx, or nil if none is present.
func FromContext(ctx context.Context) *Tracker {
	if ctx == nil {
		return nil
	}
	t, _ := ctx.Value(trackerContextKey{}).(*Tracker)
	return t
}

// WithSuppressGitHubActionsMasks disables ::add-mask:: workflow command
// emission on streams derived from ctx (used by in-process test harnesses that
// capture stdout/stderr in memory rather than writing to a CI runner pipe).
func WithSuppressGitHubActionsMasks(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, suppressMasksContextKey{}, true)
}

// SuppressGitHubActionsMasksFromContext reports whether ::add-mask:: emission
// is suppressed on ctx.
func SuppressGitHubActionsMasksFromContext(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	v, _ := ctx.Value(suppressMasksContextKey{}).(bool)
	return v
}

// Track registers a secret string (and, for multi-line values, each trimmed
// non-empty line of at least MinSecretLength bytes) for redaction.
func (t *Tracker) Track(secret string) {
	if t == nil || secret == "" {
		return
	}
	t.mu.Lock()
	t.hadCredential = true
	t.mu.Unlock()

	root := t.Root()
	if root != t {
		root.mu.Lock()
		root.hadCredential = true
		root.mu.Unlock()
	}

	trimmed := strings.TrimSpace(secret)
	if len(trimmed) < MinSecretLength {
		return
	}

	root.mu.Lock()
	defer root.mu.Unlock()

	changed := false
	add := func(s string) {
		if len(s) < MinSecretLength {
			return
		}
		if _, exists := root.seen[s]; exists {
			return
		}
		root.seen[s] = struct{}{}
		changed = true
	}

	lineStripped := strings.Trim(secret, "\r\n")
	add(lineStripped)
	if trimmed != lineStripped {
		add(trimmed)
	}

	if strings.ContainsAny(lineStripped, "\r\n") {
		normalized := strings.ReplaceAll(lineStripped, "\r\n", "\n")
		normalized = strings.ReplaceAll(normalized, "\r", "\n")
		for _, line := range strings.Split(normalized, "\n") {
			lineTrimmed := strings.TrimSpace(line)
			if len(lineTrimmed) >= MinSecretLength {
				add(lineTrimmed)
			}
		}
	}

	if changed {
		root.rebuildLocked()
	}
}

// TrackValue recursively extracts string and []byte values from v (such as a
// sensitive resolver's output) and registers them in the tracker.
func (t *Tracker) TrackValue(v any) {
	if t == nil || v == nil {
		return
	}
	t.trackValueDepth(v, 0)
}

func (t *Tracker) trackValueDepth(v any, depth int) {
	if depth > maxRedactDepth || v == nil {
		return
	}
	switch val := v.(type) {
	case string:
		t.Track(val)
	case []byte:
		t.Track(string(val))
	case []string:
		for _, item := range val {
			t.Track(item)
		}
	case []any:
		for _, item := range val {
			t.trackValueDepth(item, depth+1)
		}
	case map[string]string:
		for _, item := range val {
			t.Track(item)
		}
	case map[string]any:
		for _, item := range val {
			t.trackValueDepth(item, depth+1)
		}
	}
}

func (t *Tracker) rebuildLocked() {
	sorted := make([]string, 0, len(t.seen))
	maxLen := 0
	hasMulti := false
	for s := range t.seen {
		sorted = append(sorted, s)
		if len(s) > maxLen {
			maxLen = len(s)
		}
		if strings.ContainsAny(s, "\r\n") {
			hasMulti = true
		}
	}
	sort.Slice(sorted, func(i, j int) bool {
		if len(sorted[i]) != len(sorted[j]) {
			return len(sorted[i]) > len(sorted[j])
		}
		return sorted[i] < sorted[j]
	})

	pairs := make([]string, 0, len(sorted)*2)
	for _, s := range sorted {
		pairs = append(pairs, s, RedactedPlaceholder)
	}
	t.sorted = sorted
	t.maxLen = maxLen
	t.hasMulti = hasMulti
	t.replacer = strings.NewReplacer(pairs...)
}

// Count returns the number of unique secret patterns currently tracked.
func (t *Tracker) Count() int {
	root := t.Root()
	if root == nil {
		return 0
	}
	root.mu.RLock()
	defer root.mu.RUnlock()
	return len(root.sorted)
}

// MaxSecretLen returns the byte length of the longest tracked secret, or 0.
func (t *Tracker) MaxSecretLen() int {
	root := t.Root()
	if root == nil {
		return 0
	}
	root.mu.RLock()
	defer root.mu.RUnlock()
	return root.maxLen
}

// HasMultiline reports whether any tracked secret contains a newline.
func (t *Tracker) HasMultiline() bool {
	root := t.Root()
	if root == nil {
		return false
	}
	root.mu.RLock()
	defer root.mu.RUnlock()
	return root.hasMulti
}

// RedactString replaces all occurrences of tracked secrets in s with [REDACTED].
func (t *Tracker) RedactString(s string) string {
	root := t.Root()
	if root == nil || s == "" {
		return s
	}
	root.mu.RLock()
	replacer := root.replacer
	root.mu.RUnlock()
	if replacer == nil {
		return s
	}
	return replacer.Replace(s)
}

// ContainsTracked reports whether v contains any tracked secret substring.
func (t *Tracker) ContainsTracked(v any) bool {
	root := t.Root()
	if root == nil || v == nil || root.Count() == 0 {
		return false
	}
	return root.containsTrackedDepth(v, 0)
}

func (t *Tracker) containsTrackedDepth(v any, depth int) bool {
	if depth > maxRedactDepth || v == nil {
		return false
	}
	switch val := v.(type) {
	case string:
		return val != "" && t.RedactString(val) != val
	case []byte:
		return len(val) > 0 && t.RedactString(string(val)) != string(val)
	case []string:
		for _, item := range val {
			if item != "" && t.RedactString(item) != item {
				return true
			}
		}
	case []any:
		for _, item := range val {
			if t.containsTrackedDepth(item, depth+1) {
				return true
			}
		}
	case map[string]string:
		for k, item := range val {
			if (k != "" && t.RedactString(k) != k) || (item != "" && t.RedactString(item) != item) {
				return true
			}
		}
	case map[string]any:
		for k, item := range val {
			if (k != "" && t.RedactString(k) != k) || t.containsTrackedDepth(item, depth+1) {
				return true
			}
		}
	}
	return false
}

// RedactedError wraps an error whose message contained tracked secret material.
// It preserves errors.Is and errors.As matching without exposing raw Unwrap()
// to callers that walk cause chains to format display messages.
type RedactedError struct {
	msg   string
	inner error
}

// Error returns the scrubbed error message.
func (e *RedactedError) Error() string {
	return e.msg
}

// Is delegates error matching to the underlying error.
func (e *RedactedError) Is(target error) bool {
	return errors.Is(e.inner, target)
}

// As delegates typed error matching to the underlying error.
func (e *RedactedError) As(target any) bool {
	return errors.As(e.inner, target)
}

// RedactError scrubs any tracked secrets from err.Error(). When err is nil or
// contains no tracked secrets, err is returned unchanged.
func (t *Tracker) RedactError(err error) error {
	if t == nil || err == nil {
		return err
	}
	orig := err.Error()
	scrubbed := t.RedactString(orig)
	if scrubbed == orig {
		return err
	}
	return &RedactedError{
		msg:   scrubbed,
		inner: err,
	}
}

// RedactSlice returns a new slice with tracked secrets scrubbed from each element.
func (t *Tracker) RedactSlice(s []string) []string {
	if t == nil || len(s) == 0 || t.Count() == 0 {
		return s
	}
	out := make([]string, len(s))
	for i, v := range s {
		out[i] = t.RedactString(v)
	}
	return out
}

// RedactMap deep-clones m and scrubs tracked secrets from string keys and values.
func (t *Tracker) RedactMap(m map[string]any) map[string]any {
	if t == nil || m == nil || t.Count() == 0 {
		return m
	}
	out, _ := t.redactAnyDepth(m, 0).(map[string]any)
	return out
}

// RedactAny deep-clones structured maps/slices in v and scrubs tracked secrets
// from strings and byte slices without mutating the input structure.
func (t *Tracker) RedactAny(v any) any {
	if t == nil || v == nil || t.Count() == 0 {
		return v
	}
	return t.redactAnyDepth(v, 0)
}

func (t *Tracker) redactAnyDepth(v any, depth int) any {
	if depth > maxRedactDepth || v == nil {
		return v
	}
	switch val := v.(type) {
	case string:
		return t.RedactString(val)
	case []byte:
		if len(val) == 0 {
			return val
		}
		orig := string(val)
		scrubbed := t.RedactString(orig)
		if scrubbed == orig {
			out := make([]byte, len(val))
			copy(out, val)
			return out
		}
		return []byte(scrubbed)
	case []string:
		return t.RedactSlice(val)
	case []any:
		out := make([]any, len(val))
		for i, item := range val {
			out[i] = t.redactAnyDepth(item, depth+1)
		}
		return out
	case map[string]string:
		out := make(map[string]string, len(val))
		for k, item := range val {
			out[t.RedactString(k)] = t.RedactString(item)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, item := range val {
			out[t.RedactString(k)] = t.redactAnyDepth(item, depth+1)
		}
		return out
	default:
		return v
	}
}

// EmitGitHubActionsMasks writes "::add-mask::<line>\n" to w once per unique
// single-line secret when GITHUB_ACTIONS=true. Callers wrapping stdout in a
// StreamWriter must pass the underlying unredacted writer before streaming
// starts so the mask command itself is not scrubbed.
func (t *Tracker) EmitGitHubActionsMasks(w io.Writer) {
	root := t.Root()
	if root == nil || w == nil || os.Getenv("GITHUB_ACTIONS") != "true" {
		return
	}

	root.mu.Lock()
	var toEmit []string
	for _, s := range root.sorted {
		if strings.ContainsAny(s, "\r\n") {
			continue
		}
		if _, already := root.masked[s]; already {
			continue
		}
		root.masked[s] = struct{}{}
		toEmit = append(toEmit, s)
	}
	root.mu.Unlock()

	for _, line := range toEmit {
		_, _ = fmt.Fprintf(w, "::add-mask::%s\n", line)
	}
}

// matchLongestAt returns the length of the longest tracked secret that is a
// prefix of buf, or 0 if none matches. Caller must hold t.mu.RLock on the root tracker.
func (t *Tracker) matchLongestAt(buf string) int {
	for _, s := range t.sorted {
		if strings.HasPrefix(buf, s) {
			return len(s)
		}
	}
	return 0
}
