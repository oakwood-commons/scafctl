// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package messageprovider

import (
	"context"
	"fmt"
	"io"

	"github.com/Masterminds/semver/v3"
	"github.com/charmbracelet/lipgloss"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/schemahelper"
	"github.com/oakwood-commons/scafctl/pkg/ptrs"
	"github.com/oakwood-commons/scafctl/pkg/settings"
)

// messageStyle holds the resolved styling attributes for rendering a message.
// It starts from type defaults and is then merged with any style overrides.
type messageStyle struct {
	icon   string
	color  string
	bold   bool
	italic bool
}

// typeDefaults maps each message type to its default icon and color.
// NOTE: These values are intentionally duplicated from pkg/terminal/styles/styles.go
// because the message provider uses lipgloss directly (not the shared output helpers)
// to support the style-merge behavior. If the shared styles change, update these too.
var typeDefaults = map[string]messageStyle{
	typeSuccess: {icon: "✅", color: "#00FF00", bold: true},
	typeWarning: {icon: "⚠️", color: "#FFFF00"},
	typeError:   {icon: "❌", color: "#FF0000", bold: true},
	typeInfo:    {icon: "💡", color: "#00FFFF"},
	typeDebug:   {icon: "🐛", color: "#FF00FF", bold: true},
	typePlain:   {},
}

// ProviderName is the name of this provider used for error wrapping and identification.
const ProviderName = "message"

// Field name constants for input map keys.
const (
	fieldMessage     = "message"
	fieldType        = "type"
	fieldLabel       = "label"
	fieldStyle       = "style"
	fieldDestination = "destination"
	fieldNewline     = "newline"

	// Style sub-fields.
	fieldStyleColor  = "color"
	fieldStyleBold   = "bold"
	fieldStyleItalic = "italic"
	fieldStyleIcon   = "icon"
)

// Message type constants used by the message provider. These conceptually align
// with the standard terminal output levels but are not a 1:1 mapping to
// pkg/terminal/output message type definitions.
const (
	typeSuccess = "success"
	typeWarning = "warning"
	typeError   = "error"
	typeInfo    = "info"
	typeDebug   = "debug"
	typePlain   = "plain"
)

// Destination constants.
const (
	destStdout = "stdout"
	destStderr = "stderr"
)

// Maximum message length to prevent abuse.
const maxMessageLength = 8192

// Maximum label length.
const maxLabelLength = 100

// labelStyle renders the label prefix in dimmed text.
var labelStyle = lipgloss.NewStyle().Faint(true)

// messageOutputSchema defines the output shape for the message provider.
var messageOutputSchema = schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
	"success": schemahelper.BoolProp("Whether the message was output successfully"),
	"message": schemahelper.StringProp("The rendered message text (plain text, no ANSI codes)"),
})

// MessageProvider outputs styled, feature-rich terminal messages during solution execution.
type MessageProvider struct {
	descriptor *provider.Descriptor
}

// NewMessageProvider creates a new message provider instance.
func NewMessageProvider() *MessageProvider {
	version, _ := semver.NewVersion("1.0.0")
	return &MessageProvider{
		descriptor: &provider.Descriptor{
			Name:        ProviderName,
			DisplayName: "Message Provider",
			APIVersion:  "v1",
			Version:     version,
			Description: "Outputs styled, feature-rich terminal messages during solution execution. Supports message types (success, warning, error, info, debug, plain), custom formatting with colors and icons via lipgloss, and respects --quiet and --no-color flags. Use the framework's tmpl: or expr: ValueRef on the message input for dynamic interpolation.",
			Category:    "utility",
			Tags:        []string{"output", "message", "terminal", "logging", "display"},
			WhatIf: func(_ context.Context, input any) (string, error) {
				inputs, ok := input.(map[string]any)
				if !ok {
					return "", nil
				}
				msgType := stringField(inputs, fieldType, typeInfo)
				dest := stringField(inputs, fieldDestination, destStdout)
				label := stringField(inputs, fieldLabel, "")
				msg := stringField(inputs, fieldMessage, "")
				if msg == "" {
					if label != "" {
						return fmt.Sprintf("Would output %s message [%s] to %s", msgType, label, dest), nil
					}
					return fmt.Sprintf("Would output %s message to %s", msgType, dest), nil
				}
				// Truncate long messages in dry-run description.
				if len(msg) > 80 {
					msg = msg[:77] + "..."
				}
				if label != "" {
					return fmt.Sprintf("Would output %s message [%s]: %q to %s", msgType, label, msg, dest), nil
				}
				return fmt.Sprintf("Would output %s message: %q to %s", msgType, msg, dest), nil
			},
			Capabilities: []provider.Capability{
				provider.CapabilityAction,
			},
			Schema: schemahelper.ObjectSchema([]string{fieldMessage}, map[string]*jsonschema.Schema{
				fieldMessage: schemahelper.StringProp(
					"The message text to output. For dynamic interpolation, use tmpl: or expr: ValueRef on this input instead of passing templates directly.",
					schemahelper.WithExample("Deployment completed successfully"),
					schemahelper.WithMaxLength(maxMessageLength),
				),
				fieldLabel: schemahelper.StringProp(
					"Optional contextual prefix displayed in brackets before the message text (e.g., 'deploy', 'step 2/5'). Rendered as dimmed [label] between the icon and message. Supports tmpl: and expr: ValueRef for dynamic labels.",
					schemahelper.WithExample("step 1/3"),
					schemahelper.WithMaxLength(maxLabelLength),
				),
				fieldType: schemahelper.StringProp(
					"The message type that determines icon and color styling. Maps to built-in terminal output styles: success (✅ green), warning (⚠️ yellow), error (❌ red), info (💡 cyan), debug (🐛 magenta), plain (no styling).",
					schemahelper.WithEnum(typeSuccess, typeWarning, typeError, typeInfo, typeDebug, typePlain),
					schemahelper.WithDefault(typeInfo),
					schemahelper.WithMaxLength(*ptrs.IntPtr(10)),
				),
				fieldStyle: schemahelper.ObjectProp(
					"Custom formatting overrides that merge on top of the 'type' defaults. Only the fields you specify are overridden; unset fields keep their type defaults. For example, type=success with style.icon=\"🚀\" gives green+bold from success but replaces the ✅ icon.",
					nil,
					map[string]*jsonschema.Schema{
						fieldStyleColor: schemahelper.StringProp(
							"Text color as ANSI color name (e.g., 'green', 'red', 'cyan') or hex code (e.g., '#FF5733'). Overrides the type's default color.",
							schemahelper.WithExample("#FF5733"),
							schemahelper.WithMaxLength(*ptrs.IntPtr(20)),
						),
						fieldStyleBold: schemahelper.BoolProp(
							"Whether to render the message in bold. When omitted, inherits from the type default (e.g., success and debug types default to bold).",
						),
						fieldStyleItalic: schemahelper.BoolProp(
							"Whether to render the message in italic. When omitted, inherits from the type default.",
						),
						fieldStyleIcon: schemahelper.StringProp(
							"Custom icon or emoji prefix for the message (e.g., '🚀', '📦', '→'). Overrides the type's default icon. Set to empty string to disable the icon entirely.",
							schemahelper.WithExample("🚀"),
							schemahelper.WithMaxLength(*ptrs.IntPtr(10)),
						),
					},
				),
				fieldDestination: schemahelper.StringProp(
					"Where to write the message output.",
					schemahelper.WithEnum(destStdout, destStderr),
					schemahelper.WithDefault(destStdout),
					schemahelper.WithMaxLength(*ptrs.IntPtr(10)),
				),

				fieldNewline: schemahelper.BoolProp(
					"Whether to append a trailing newline after the message.",
					schemahelper.WithDefault(true),
				),
			}),
			OutputSchemas: map[provider.Capability]*jsonschema.Schema{
				provider.CapabilityAction: messageOutputSchema,
			},
			Examples: []provider.Example{
				{
					Name:        "Success message",
					Description: "Output a success message with default styling",
					YAML: `name: deploy-success
provider: message
inputs:
  message: "Deployment completed successfully"
  type: success`,
				},
				{
					Name:        "Labeled message",
					Description: "Output a message with a contextual label prefix",
					YAML: `name: deploy-step
provider: message
inputs:
  message: "Installing dependencies"
  type: info
  label: "step 2/5"`,
				},
				{
					Name:        "Type with style override",
					Description: "Use success type as the base, but override the icon. Color and bold are inherited from success defaults.",
					YAML: `name: deploy-start
provider: message
inputs:
  message: "Starting deployment pipeline"
  type: success
  style:
    icon: "🚀"`,
				},
				{
					Name:        "Fully custom style",
					Description: "Output a message with fully custom color, icon, and bold formatting (no type needed)",
					YAML: `name: custom-deploy
provider: message
inputs:
  message: "Starting deployment pipeline"
  type: plain
  style:
    color: "#FF5733"
    bold: true
    icon: "🚀"`,
				},
			},
		},
	}
}

// Descriptor returns the provider descriptor.
func (p *MessageProvider) Descriptor() *provider.Descriptor {
	return p.descriptor
}

// Execute outputs a styled message to the terminal.
func (p *MessageProvider) Execute(ctx context.Context, input any) (*provider.Output, error) {
	inputs, ok := input.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: expected map[string]any, got %T", ProviderName, input)
	}
	lgr := logger.FromContext(ctx)

	// Resolve the message text (required for both normal and dry-run execution).
	msgStr, ok := inputs[fieldMessage].(string)
	if !ok || msgStr == "" {
		return nil, fmt.Errorf("%s: 'message' must be provided", ProviderName)
	}

	// Check for dry-run mode.
	if provider.DryRunFromContext(ctx) {
		return p.executeDryRun(inputs)
	}

	// Get configuration fields.
	msgType := stringField(inputs, fieldType, typeInfo)
	dest := stringField(inputs, fieldDestination, destStdout)
	newline := boolField(inputs, fieldNewline, true)

	// Get settings from context for quiet/noColor.
	noColor := false
	isQuiet := false
	if runSettings, ok := settings.FromContext(ctx); ok {
		noColor = runSettings.NoColor
		isQuiet = runSettings.IsQuiet
	}

	// Format the message for terminal output.
	styled := p.formatMessage(msgStr, msgType, inputs, noColor, newline)

	// Build a plain-text version for output data (no ANSI codes, no trailing newline).
	// The newline is a terminal-output concern; structured data should not embed it.
	plain := p.formatMessage(msgStr, msgType, inputs, true, false)

	// Write to the terminal unless suppressed by --quiet.
	streamed := false
	if !isQuiet {
		ioStreams, ok := provider.IOStreamsFromContext(ctx)
		if ok && ioStreams != nil {
			if err := p.writeToTerminal(ioStreams, styled, dest); err != nil {
				return nil, fmt.Errorf("%s: failed to write output: %w", ProviderName, err)
			}
			streamed = true
		}
	}

	lgr.V(1).Info("Message output",
		fieldType, msgType,
		fieldDestination, dest,
		"written", !isQuiet,
	)

	return &provider.Output{
		Data: map[string]any{
			"success": true,
			"message": plain,
		},
		Streamed: streamed,
	}, nil
}

// formatMessage applies styling to the message text. It starts with the type
// defaults (icon, color, bold) and merges any style overrides on top. Only the
// fields explicitly set in style are overridden; everything else keeps the type
// default. The trailing newline is baked into the output.
func (p *MessageProvider) formatMessage(text, msgType string, inputs map[string]any, noColor, newline bool) string {
	// Start with type defaults; fall back to info for unknown types.
	ms, ok := typeDefaults[msgType]
	if !ok {
		ms = typeDefaults[typeInfo]
	}

	// Merge style overrides on top (even in noColor — we need icon resolution).
	hasExplicitIcon := false
	if styleMap, ok := inputs[fieldStyle].(map[string]any); ok {
		if icon, ok := styleMap[fieldStyleIcon].(string); ok {
			ms.icon = icon // empty string disables the icon
			hasExplicitIcon = true
		}
		if !noColor {
			if color, ok := styleMap[fieldStyleColor].(string); ok {
				ms.color = color
			}
			if bold, ok := styleMap[fieldStyleBold].(bool); ok {
				ms.bold = bold
			}
			if italic, ok := styleMap[fieldStyleItalic].(bool); ok {
				ms.italic = italic
			}
		}
	}

	// In noColor mode, omit the default type icon (consistent with pkg/terminal/output)
	// but still honor an explicit style.icon override.
	if noColor && !hasExplicitIcon {
		ms.icon = ""
	}

	if noColor {
		s := text
		if label, ok := inputs[fieldLabel].(string); ok && label != "" {
			s = "[" + label + "] " + s
		}
		if ms.icon != "" {
			s = ms.icon + " " + s
		}
		if newline {
			s += "\n"
		}
		return s
	}

	// Build lipgloss style.
	style := lipgloss.NewStyle()
	if ms.color != "" {
		style = style.Foreground(lipgloss.Color(ms.color))
	}
	if ms.bold {
		style = style.Bold(true)
	}
	if ms.italic {
		style = style.Italic(true)
	}

	// Render: icon + [label] + styled message
	styled := style.Render(text)

	// Prepend label in dimmed brackets if provided.
	if label, ok := inputs[fieldLabel].(string); ok && label != "" {
		styled = labelStyle.Render("["+label+"]") + " " + styled
	}

	if ms.icon != "" {
		styled = ms.icon + " " + styled
	}

	if newline {
		styled += "\n"
	}

	return styled
}

// writeToTerminal writes the fully-rendered message to the appropriate stream.
// The caller is responsible for verifying IOStreams are available before calling.
func (p *MessageProvider) writeToTerminal(ioStreams *provider.IOStreams, msg, dest string) error {
	var w io.Writer
	switch dest {
	case destStderr:
		w = ioStreams.ErrOut
	default:
		w = ioStreams.Out
	}

	if w == nil {
		return fmt.Errorf("no writer available for destination %q", dest)
	}

	_, err := fmt.Fprint(w, msg)
	return err
}

// executeDryRun returns a simulated output for dry-run mode.
func (p *MessageProvider) executeDryRun(inputs map[string]any) (*provider.Output, error) {
	msgType := stringField(inputs, fieldType, typeInfo)
	dest := stringField(inputs, fieldDestination, destStdout)
	label := stringField(inputs, fieldLabel, "")
	msg := stringField(inputs, fieldMessage, "<dynamic>")

	desc := fmt.Sprintf("[dry-run] Would output %s message to %s: %s", msgType, dest, msg)
	if label != "" {
		desc = fmt.Sprintf("[dry-run] Would output %s message [%s] to %s: %s", msgType, label, dest, msg)
	}

	return &provider.Output{
		Data: map[string]any{
			"success": true,
			"message": desc,
		},
	}, nil
}

// stringField extracts a string value from the inputs map with a default fallback.
func stringField(inputs map[string]any, key, def string) string {
	if v, ok := inputs[key].(string); ok && v != "" {
		return v
	}
	return def
}

// boolField extracts a bool value from the inputs map with a default fallback.
func boolField(inputs map[string]any, key string, def bool) bool {
	if v, ok := inputs[key].(bool); ok {
		return v
	}
	return def
}
