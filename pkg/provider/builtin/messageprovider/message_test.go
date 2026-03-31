// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package messageprovider

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testCtx returns a context with logger, IOStreams backed by buffers, and optional settings.
func testCtx(t *testing.T, runSettings *settings.Run) (context.Context, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	ctx := logger.WithLogger(context.Background(), logger.Get(0))
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	ctx = provider.WithIOStreams(ctx, &provider.IOStreams{Out: stdout, ErrOut: stderr})
	if runSettings != nil {
		ctx = settings.IntoContext(ctx, runSettings)
	} else {
		ctx = settings.IntoContext(ctx, &settings.Run{})
	}
	return ctx, stdout, stderr
}

func TestNewMessageProvider(t *testing.T) {
	p := NewMessageProvider()
	require.NotNil(t, p)
	assert.Equal(t, "message", p.Descriptor().Name)
	assert.Equal(t, "Message Provider", p.Descriptor().DisplayName)
	assert.Equal(t, "v1", p.Descriptor().APIVersion)
	assert.Contains(t, p.Descriptor().Capabilities, provider.CapabilityAction)
	assert.NotContains(t, p.Descriptor().Capabilities, provider.CapabilityFrom)
	assert.Equal(t, "utility", p.Descriptor().Category)
	assert.NotEmpty(t, p.Descriptor().Examples)
}

func TestMessageProvider_Execute_InvalidInput(t *testing.T) {
	p := NewMessageProvider()
	ctx, _, _ := testCtx(t, nil)

	_, err := p.Execute(ctx, "not-a-map")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected map[string]any")
}

func TestMessageProvider_Execute_MissingMessage(t *testing.T) {
	p := NewMessageProvider()
	ctx, _, _ := testCtx(t, nil)

	_, err := p.Execute(ctx, map[string]any{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "'message' must be provided")
}

func TestMessageProvider_Execute_PlainMessage(t *testing.T) {
	p := NewMessageProvider()
	ctx, stdout, _ := testCtx(t, &settings.Run{NoColor: true})

	out, err := p.Execute(ctx, map[string]any{
		"message": "hello world",
		"type":    "plain",
	})
	require.NoError(t, err)
	require.NotNil(t, out)

	data := out.Data.(map[string]any)
	assert.True(t, data["success"].(bool))
	assert.Equal(t, "hello world", data["message"])
	assert.True(t, out.Streamed)
	assert.Equal(t, "hello world\n", stdout.String())
}

func TestMessageProvider_Execute_AllMessageTypes(t *testing.T) {
	types := []string{typeSuccess, typeWarning, typeError, typeInfo, typeDebug, typePlain}
	for _, msgType := range types {
		t.Run(msgType, func(t *testing.T) {
			p := NewMessageProvider()
			ctx, stdout, _ := testCtx(t, &settings.Run{NoColor: true})

			out, err := p.Execute(ctx, map[string]any{
				"message": "test message",
				"type":    msgType,
			})
			require.NoError(t, err)
			require.NotNil(t, out)
			assert.True(t, out.Streamed)
			assert.Contains(t, stdout.String(), "test message")

			data := out.Data.(map[string]any)
			assert.True(t, data["success"].(bool))
			assert.Contains(t, data["message"].(string), "test message")
		})
	}
}

func TestMessageProvider_Execute_TypeStyling_NoColor(t *testing.T) {
	p := NewMessageProvider()
	ctx, stdout, _ := testCtx(t, &settings.Run{NoColor: true})

	_, err := p.Execute(ctx, map[string]any{
		"message": "styled",
		"type":    "success",
	})
	require.NoError(t, err)
	// In noColor mode, default type icons are omitted (consistent with terminal/output).
	assert.Equal(t, "styled\n", stdout.String())
}

func TestMessageProvider_Execute_TypeStyling_WithColor(t *testing.T) {
	types := []string{typeSuccess, typeWarning, typeError, typeInfo, typeDebug}
	for _, msgType := range types {
		t.Run(msgType, func(t *testing.T) {
			p := NewMessageProvider()
			ctx, stdout, _ := testCtx(t, &settings.Run{NoColor: false})

			_, err := p.Execute(ctx, map[string]any{
				"message": "styled",
				"type":    msgType,
			})
			require.NoError(t, err)
			// With color, the output should contain the message text.
			assert.Contains(t, stdout.String(), "styled")
		})
	}
}

func TestMessageProvider_Execute_OutputDataNoANSI(t *testing.T) {
	p := NewMessageProvider()
	ctx, _, _ := testCtx(t, &settings.Run{NoColor: false})

	out, err := p.Execute(ctx, map[string]any{
		"message": "deploy finished",
		"type":    "success",
	})
	require.NoError(t, err)
	data := out.Data.(map[string]any)
	msg := data["message"].(string)
	// Output data must never contain ANSI escape codes, even when terminal uses color.
	assert.NotContains(t, msg, "\x1b[")
	assert.Contains(t, msg, "deploy finished")
}

func TestMessageProvider_Execute_CustomStyle(t *testing.T) {
	p := NewMessageProvider()
	ctx, stdout, _ := testCtx(t, &settings.Run{NoColor: false})

	_, err := p.Execute(ctx, map[string]any{
		"message": "custom styled",
		"style": map[string]any{
			"color":  "#FF5733",
			"bold":   true,
			"italic": true,
			"icon":   "\U0001F680",
		},
	})
	require.NoError(t, err)
	result := stdout.String()
	assert.Contains(t, result, "\U0001F680")
	assert.Contains(t, result, "custom styled")
}

func TestMessageProvider_Execute_CustomStyle_IconOnly(t *testing.T) {
	p := NewMessageProvider()
	ctx, stdout, _ := testCtx(t, &settings.Run{NoColor: false})

	_, err := p.Execute(ctx, map[string]any{
		"message": "with icon",
		"style": map[string]any{
			"icon": "\U0001F4E6",
		},
	})
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "\U0001F4E6")
	assert.Contains(t, stdout.String(), "with icon")
}

func TestMessageProvider_Execute_CustomStyle_NoColor_FallsBackToType(t *testing.T) {
	p := NewMessageProvider()
	ctx, stdout, _ := testCtx(t, &settings.Run{NoColor: true})

	_, err := p.Execute(ctx, map[string]any{
		"message": "fallback",
		"type":    "plain",
		"style": map[string]any{
			"color": "#FF0000",
			"bold":  true,
		},
	})
	require.NoError(t, err)
	// When noColor is true, colors/bold are stripped. Plain type has no icon.
	assert.Equal(t, "fallback\n", stdout.String())
}

func TestMessageProvider_Execute_NoColor_StyleIconStillApplied(t *testing.T) {
	p := NewMessageProvider()
	ctx, stdout, _ := testCtx(t, &settings.Run{NoColor: true})

	_, err := p.Execute(ctx, map[string]any{
		"message": "with icon",
		"type":    "plain",
		"style": map[string]any{
			"icon": "\U0001F680",
		},
	})
	require.NoError(t, err)
	// Explicit style.icon is still honored in noColor mode.
	assert.Equal(t, "\U0001F680 with icon\n", stdout.String())
}

func TestMessageProvider_Execute_NoColor_StyleIconDisabled(t *testing.T) {
	p := NewMessageProvider()
	ctx, stdout, _ := testCtx(t, &settings.Run{NoColor: true})

	_, err := p.Execute(ctx, map[string]any{
		"message": "no icon",
		"type":    "success",
		"style": map[string]any{
			"icon": "",
		},
	})
	require.NoError(t, err)
	// Empty icon override disables the icon.
	assert.Equal(t, "no icon\n", stdout.String())
}

func TestMessageProvider_Execute_UnknownTypeFallsBackToInfo(t *testing.T) {
	p := NewMessageProvider()
	ctx, stdout, _ := testCtx(t, &settings.Run{NoColor: true})

	_, err := p.Execute(ctx, map[string]any{
		"message": "unknown type",
		"type":    "nonexistent",
	})
	require.NoError(t, err)
	// In noColor mode, default type icons are omitted.
	assert.Equal(t, "unknown type\n", stdout.String())
}

func TestMessageProvider_Execute_StyleMergesOnType(t *testing.T) {
	p := NewMessageProvider()
	ctx, stdout, _ := testCtx(t, &settings.Run{NoColor: false})

	// Use success type (✅ green bold) but override only the icon to 🚀.
	// Color and bold should be inherited from success defaults.
	_, err := p.Execute(ctx, map[string]any{
		"message": "merged",
		"type":    "success",
		"style": map[string]any{
			"icon": "🚀",
		},
	})
	require.NoError(t, err)
	result := stdout.String()
	assert.Contains(t, result, "🚀")
	assert.Contains(t, result, "merged")
	// Should NOT contain the default success icon since it was overridden.
	assert.NotContains(t, result, "✅")
}

func TestMessageProvider_Execute_StyleDisableIcon(t *testing.T) {
	p := NewMessageProvider()
	ctx, stdout, _ := testCtx(t, &settings.Run{NoColor: false})

	// Use success type but explicitly disable the icon with empty string.
	_, err := p.Execute(ctx, map[string]any{
		"message": "no icon",
		"type":    "success",
		"style": map[string]any{
			"icon": "",
		},
	})
	require.NoError(t, err)
	result := stdout.String()
	assert.Contains(t, result, "no icon")
	assert.NotContains(t, result, "✅")
}

func TestMessageProvider_Execute_StyleAddsBoldToType(t *testing.T) {
	p := NewMessageProvider()
	ctx, stdout, _ := testCtx(t, &settings.Run{NoColor: false})

	// Warning type (⚠️ yellow, no bold) + style adds italic.
	_, err := p.Execute(ctx, map[string]any{
		"message": "italic warning",
		"type":    "warning",
		"style": map[string]any{
			"italic": true,
		},
	})
	require.NoError(t, err)
	result := stdout.String()
	assert.Contains(t, result, "⚠️")
	assert.Contains(t, result, "italic warning")
}

func TestMessageProvider_Execute_StyleOverridesColor(t *testing.T) {
	p := NewMessageProvider()
	ctx, stdout, _ := testCtx(t, &settings.Run{NoColor: false})

	// Use info type but override color. Icon (💡) should be preserved.
	_, err := p.Execute(ctx, map[string]any{
		"message": "custom color",
		"type":    "info",
		"style": map[string]any{
			"color": "#FF5733",
		},
	})
	require.NoError(t, err)
	result := stdout.String()
	assert.Contains(t, result, "💡")
	assert.Contains(t, result, "custom color")
}

func TestMessageProvider_Execute_Label(t *testing.T) {
	p := NewMessageProvider()
	ctx, stdout, _ := testCtx(t, &settings.Run{NoColor: true})

	_, err := p.Execute(ctx, map[string]any{
		"message": "Installing dependencies",
		"type":    "plain",
		"label":   "step 2/5",
	})
	require.NoError(t, err)
	assert.Equal(t, "[step 2/5] Installing dependencies\n", stdout.String())
}

func TestMessageProvider_Execute_Label_WithTypeIcon(t *testing.T) {
	p := NewMessageProvider()
	ctx, stdout, _ := testCtx(t, &settings.Run{NoColor: true})

	_, err := p.Execute(ctx, map[string]any{
		"message": "Deploying service",
		"type":    "success",
		"label":   "deploy",
	})
	require.NoError(t, err)
	// noColor: no default icon + label + message
	assert.Equal(t, "[deploy] Deploying service\n", stdout.String())
}

func TestMessageProvider_Execute_Label_WithColor(t *testing.T) {
	p := NewMessageProvider()
	ctx, stdout, _ := testCtx(t, &settings.Run{NoColor: false})

	_, err := p.Execute(ctx, map[string]any{
		"message": "Deploying service",
		"type":    "success",
		"label":   "deploy",
	})
	require.NoError(t, err)
	result := stdout.String()
	assert.Contains(t, result, "✅")
	assert.Contains(t, result, "[deploy]")
	assert.Contains(t, result, "Deploying service")
}

func TestMessageProvider_Execute_Label_Empty(t *testing.T) {
	p := NewMessageProvider()
	ctx, stdout, _ := testCtx(t, &settings.Run{NoColor: true})

	_, err := p.Execute(ctx, map[string]any{
		"message": "no label",
		"type":    "plain",
		"label":   "",
	})
	require.NoError(t, err)
	// Empty label should not add brackets.
	assert.Equal(t, "no label\n", stdout.String())
}

func TestMessageProvider_Execute_Label_NoLabel(t *testing.T) {
	p := NewMessageProvider()
	ctx, stdout, _ := testCtx(t, &settings.Run{NoColor: true})

	_, err := p.Execute(ctx, map[string]any{
		"message": "no label field",
		"type":    "plain",
	})
	require.NoError(t, err)
	// Missing label field should not add brackets.
	assert.Equal(t, "no label field\n", stdout.String())
}

func TestMessageProvider_Execute_DestinationStderr(t *testing.T) {
	p := NewMessageProvider()
	ctx, stdout, stderr := testCtx(t, &settings.Run{NoColor: true})

	_, err := p.Execute(ctx, map[string]any{
		"message":     "error log",
		"type":        "plain",
		"destination": "stderr",
	})
	require.NoError(t, err)
	assert.Empty(t, stdout.String())
	assert.Equal(t, "error log\n", stderr.String())
}

func TestMessageProvider_Execute_NewlineFalse(t *testing.T) {
	p := NewMessageProvider()
	ctx, stdout, _ := testCtx(t, &settings.Run{NoColor: true})

	_, err := p.Execute(ctx, map[string]any{
		"message": "no newline",
		"type":    "plain",
		"newline": false,
	})
	require.NoError(t, err)
	assert.Equal(t, "no newline", stdout.String())
}

func TestMessageProvider_Execute_NewlineTrue(t *testing.T) {
	p := NewMessageProvider()
	ctx, stdout, _ := testCtx(t, &settings.Run{NoColor: true})

	_, err := p.Execute(ctx, map[string]any{
		"message": "with newline",
		"type":    "plain",
		"newline": true,
	})
	require.NoError(t, err)
	assert.Equal(t, "with newline\n", stdout.String())
}

func TestMessageProvider_Execute_NewlineDefault(t *testing.T) {
	p := NewMessageProvider()
	ctx, stdout, _ := testCtx(t, &settings.Run{NoColor: true})

	_, err := p.Execute(ctx, map[string]any{
		"message": "default newline",
		"type":    "plain",
	})
	require.NoError(t, err)
	// Default is newline=true.
	assert.Equal(t, "default newline\n", stdout.String())
}

func TestMessageProvider_Execute_NewlineFalse_CustomStyle(t *testing.T) {
	p := NewMessageProvider()
	ctx, stdout, _ := testCtx(t, &settings.Run{NoColor: false})

	_, err := p.Execute(ctx, map[string]any{
		"message": "no trailing",
		"newline": false,
		"style": map[string]any{
			"color": "green",
		},
	})
	require.NoError(t, err)
	assert.NotContains(t, stdout.String(), "\n")
}

func TestMessageProvider_Execute_QuietSuppressed(t *testing.T) {
	p := NewMessageProvider()
	ctx, stdout, _ := testCtx(t, &settings.Run{IsQuiet: true, NoColor: true})

	out, err := p.Execute(ctx, map[string]any{
		"message": "should be suppressed",
		"type":    "plain",
	})
	require.NoError(t, err)
	assert.Empty(t, stdout.String())
	assert.False(t, out.Streamed)
	// Rendered message still available in output data.
	data := out.Data.(map[string]any)
	assert.Equal(t, "should be suppressed", data["message"])
}

func TestMessageProvider_Execute_NotQuiet(t *testing.T) {
	p := NewMessageProvider()
	ctx, stdout, _ := testCtx(t, &settings.Run{IsQuiet: false, NoColor: true})

	out, err := p.Execute(ctx, map[string]any{
		"message": "visible",
		"type":    "plain",
	})
	require.NoError(t, err)
	assert.Equal(t, "visible\n", stdout.String())
	assert.True(t, out.Streamed)
}

func TestMessageProvider_Execute_DryRun(t *testing.T) {
	p := NewMessageProvider()
	ctx, stdout, _ := testCtx(t, nil)
	ctx = provider.WithDryRun(ctx, true)

	out, err := p.Execute(ctx, map[string]any{
		"message":     "deploy now",
		"type":        "warning",
		"destination": "stderr",
	})
	require.NoError(t, err)
	assert.Empty(t, stdout.String()) // Dry-run doesn't write to terminal.

	data := out.Data.(map[string]any)
	assert.True(t, data["success"].(bool))
	assert.Contains(t, data["message"].(string), "[dry-run]")
	assert.Contains(t, data["message"].(string), "warning")
	assert.Contains(t, data["message"].(string), "stderr")
}

func TestMessageProvider_Execute_DryRun_NoMessage(t *testing.T) {
	p := NewMessageProvider()
	ctx, _, _ := testCtx(t, nil)
	ctx = provider.WithDryRun(ctx, true)

	_, err := p.Execute(ctx, map[string]any{
		"type": "info",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "'message' must be provided")
}

func TestMessageProvider_Execute_DryRun_WithLabel(t *testing.T) {
	p := NewMessageProvider()
	ctx, stdout, _ := testCtx(t, nil)
	ctx = provider.WithDryRun(ctx, true)

	out, err := p.Execute(ctx, map[string]any{
		"message": "deploy now",
		"type":    "info",
		"label":   "step 1",
	})
	require.NoError(t, err)
	assert.Empty(t, stdout.String())

	data := out.Data.(map[string]any)
	assert.Contains(t, data["message"].(string), "[dry-run]")
	assert.Contains(t, data["message"].(string), "[step 1]")
}

func TestMessageProvider_Execute_NilWriter(t *testing.T) {
	p := NewMessageProvider()
	// Create IOStreams with nil Out to trigger the nil writer error path.
	ctx := logger.WithLogger(context.Background(), logger.Get(0))
	ctx = settings.IntoContext(ctx, &settings.Run{NoColor: true})
	ctx = provider.WithIOStreams(ctx, &provider.IOStreams{Out: nil, ErrOut: nil})

	_, err := p.Execute(ctx, map[string]any{
		"message": "should fail",
		"type":    "plain",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no writer available")
}

func TestMessageProvider_Execute_NilStderrWriter(t *testing.T) {
	p := NewMessageProvider()
	ctx := logger.WithLogger(context.Background(), logger.Get(0))
	ctx = settings.IntoContext(ctx, &settings.Run{NoColor: true})
	ctx = provider.WithIOStreams(ctx, &provider.IOStreams{Out: &bytes.Buffer{}, ErrOut: nil})

	_, err := p.Execute(ctx, map[string]any{
		"message":     "should fail",
		"type":        "plain",
		"destination": "stderr",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no writer available")
}

func TestMessageProvider_Execute_NoIOStreams(t *testing.T) {
	p := NewMessageProvider()
	// Context WITHOUT IOStreams.
	ctx := logger.WithLogger(context.Background(), logger.Get(0))
	ctx = settings.IntoContext(ctx, &settings.Run{NoColor: true})

	out, err := p.Execute(ctx, map[string]any{
		"message": "will succeed without streaming",
		"type":    "plain",
	})
	require.NoError(t, err)
	// Message is still in output data even without IOStreams.
	data := out.Data.(map[string]any)
	assert.Equal(t, "will succeed without streaming", data["message"])
	assert.True(t, data["success"].(bool))
	// Streamed should be false since no IOStreams were available.
	assert.False(t, out.Streamed)
}

func TestMessageProvider_Execute_PlainMessage_NoSettings(t *testing.T) {
	p := NewMessageProvider()
	ctx := logger.WithLogger(context.Background(), logger.Get(0))
	stdout := &bytes.Buffer{}
	ctx = provider.WithIOStreams(ctx, &provider.IOStreams{Out: stdout, ErrOut: &bytes.Buffer{}})
	// No settings in context - defaults to noColor=false, isQuiet=false.

	out, err := p.Execute(ctx, map[string]any{
		"message": "no settings",
		"type":    "plain",
	})
	require.NoError(t, err)
	assert.True(t, out.Streamed)
	assert.Equal(t, "no settings\n", stdout.String())
}

func TestMessageProvider_WhatIf(t *testing.T) {
	p := NewMessageProvider()
	desc := p.Descriptor()
	require.NotNil(t, desc.WhatIf)

	tests := []struct {
		name     string
		input    any
		contains string
	}{
		{
			name:     "with message",
			input:    map[string]any{"message": "hello", "type": "success"},
			contains: "Would output success message",
		},
		{
			name:     "with expression (via ValueRef)",
			input:    map[string]any{"type": "info"},
			contains: "Would output info message",
		},
		{
			name:     "no message",
			input:    map[string]any{"type": "warning", "destination": "stderr"},
			contains: "Would output warning message to stderr",
		},
		{
			name: "long message truncated",
			input: map[string]any{
				"message": strings.Repeat("x", 100),
			},
			contains: "...",
		},
		{
			name:     "with label",
			input:    map[string]any{"message": "hello", "type": "success", "label": "deploy"},
			contains: "[deploy]",
		},
		{
			name:     "no message with label",
			input:    map[string]any{"type": "info", "label": "step 1"},
			contains: "[step 1]",
		},
		{
			name:     "invalid input type",
			input:    "not-a-map",
			contains: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg, err := desc.WhatIf(context.Background(), tt.input)
			require.NoError(t, err)
			if tt.contains != "" {
				assert.Contains(t, msg, tt.contains)
			}
		})
	}
}

func TestStringField(t *testing.T) {
	m := map[string]any{"key": "val", "empty": ""}
	assert.Equal(t, "val", stringField(m, "key", "def"))
	assert.Equal(t, "def", stringField(m, "missing", "def"))
	assert.Equal(t, "def", stringField(m, "empty", "def"))
}

func TestBoolField(t *testing.T) {
	m := map[string]any{"yes": true, "no": false}
	assert.True(t, boolField(m, "yes", false))
	assert.False(t, boolField(m, "no", true))
	assert.True(t, boolField(m, "missing", true))
}

func TestDescriptorValidation(t *testing.T) {
	p := NewMessageProvider()
	err := provider.ValidateDescriptor(p.Descriptor())
	require.NoError(t, err, "message provider descriptor should be valid")
}

// --- Benchmarks ---

func BenchmarkExecutePlainMessage(b *testing.B) {
	p := NewMessageProvider()
	ctx := logger.WithLogger(context.Background(), logger.Get(0))
	ctx = settings.IntoContext(ctx, &settings.Run{NoColor: true})
	stdout := &bytes.Buffer{}
	ctx = provider.WithIOStreams(ctx, &provider.IOStreams{Out: stdout, ErrOut: &bytes.Buffer{}})

	input := map[string]any{
		"message": "benchmark message",
		"type":    "plain",
	}

	b.ResetTimer()
	for b.Loop() {
		stdout.Reset()
		_, _ = p.Execute(ctx, input)
	}
}

func BenchmarkExecuteStyledMessage(b *testing.B) {
	p := NewMessageProvider()
	ctx := logger.WithLogger(context.Background(), logger.Get(0))
	ctx = settings.IntoContext(ctx, &settings.Run{NoColor: false})
	stdout := &bytes.Buffer{}
	ctx = provider.WithIOStreams(ctx, &provider.IOStreams{Out: stdout, ErrOut: &bytes.Buffer{}})

	input := map[string]any{
		"message": "benchmark styled",
		"type":    "success",
	}

	b.ResetTimer()
	for b.Loop() {
		stdout.Reset()
		_, _ = p.Execute(ctx, input)
	}
}

func BenchmarkExecuteCustomStyle(b *testing.B) {
	p := NewMessageProvider()
	ctx := logger.WithLogger(context.Background(), logger.Get(0))
	ctx = settings.IntoContext(ctx, &settings.Run{NoColor: false})
	stdout := &bytes.Buffer{}
	ctx = provider.WithIOStreams(ctx, &provider.IOStreams{Out: stdout, ErrOut: &bytes.Buffer{}})

	input := map[string]any{
		"message": "benchmark custom",
		"style": map[string]any{
			"color":  "#FF5733",
			"bold":   true,
			"italic": true,
			"icon":   "\U0001F680",
		},
	}

	b.ResetTimer()
	for b.Loop() {
		stdout.Reset()
		_, _ = p.Execute(ctx, input)
	}
}

func BenchmarkExecuteStyleMerge(b *testing.B) {
	p := NewMessageProvider()
	ctx := logger.WithLogger(context.Background(), logger.Get(0))
	ctx = settings.IntoContext(ctx, &settings.Run{NoColor: false})
	stdout := &bytes.Buffer{}
	ctx = provider.WithIOStreams(ctx, &provider.IOStreams{Out: stdout, ErrOut: &bytes.Buffer{}})

	input := map[string]any{
		"message": "benchmark merge",
		"type":    "success",
		"style": map[string]any{
			"icon": "🚀",
		},
	}

	b.ResetTimer()
	for b.Loop() {
		stdout.Reset()
		_, _ = p.Execute(ctx, input)
	}
}

func BenchmarkExecuteWithLabel(b *testing.B) {
	p := NewMessageProvider()
	ctx := logger.WithLogger(context.Background(), logger.Get(0))
	ctx = settings.IntoContext(ctx, &settings.Run{NoColor: false})
	stdout := &bytes.Buffer{}
	ctx = provider.WithIOStreams(ctx, &provider.IOStreams{Out: stdout, ErrOut: &bytes.Buffer{}})

	input := map[string]any{
		"message": "benchmark label",
		"type":    "info",
		"label":   "step 3/5",
	}

	b.ResetTimer()
	for b.Loop() {
		stdout.Reset()
		_, _ = p.Execute(ctx, input)
	}
}
