// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package messageprovider

import (
	"context"
	"encoding/json"
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
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"gopkg.in/yaml.v3"
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
	typeRaw:     {},
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

	// Data mode fields.
	fieldData        = "data"
	fieldDisplay     = "display"
	fieldFormat      = "format"
	fieldColumnHints = "columnHints"
	fieldColumnOrder = "columnOrder"
	fieldExpand      = "expand"

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
	typeRaw     = "raw"
)

// Destination constants.
const (
	destStdout = "stdout"
	destStderr = "stderr"
)

// Data mode format constants.
const (
	formatAuto    = "auto"
	formatTable   = "table"
	formatList    = "list"
	formatTree    = "tree"
	formatMermaid = "mermaid"
	formatJSON    = "json"
	formatYAML    = "yaml"
	formatQuiet   = "quiet"
)

// Maximum message length to prevent abuse.
const maxMessageLength = 8192

// Maximum label length.
const maxLabelLength = 100

// labelStyle renders the label prefix in dimmed text.
var labelStyle = lipgloss.NewStyle().Faint(true)

// messageOutputSchema defines the output shape for text mode.
var messageOutputSchema = schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
	"success": schemahelper.BoolProp("Whether the message was output successfully"),
	"message": schemahelper.StringProp("The rendered message text (plain text, no ANSI codes)"),
})

// dataOnlyFields are input fields exclusive to data mode.
var dataOnlyFields = []string{fieldDisplay, fieldFormat, fieldColumnHints, fieldColumnOrder, fieldExpand}

// MessageProvider outputs styled, feature-rich terminal messages during solution execution.
type MessageProvider struct {
	descriptor *provider.Descriptor
}

// NewMessageProvider creates a new message provider instance.
func NewMessageProvider() *MessageProvider {
	version, _ := semver.NewVersion("2.0.0")
	return &MessageProvider{
		descriptor: &provider.Descriptor{
			Name:        ProviderName,
			DisplayName: "Message Provider",
			APIVersion:  "v1",
			Version:     version,
			Description: "Outputs styled terminal messages or renders structured data using kvx during solution execution. Text mode supports message types (success, warning, error, info, debug, plain) with custom formatting. Data mode renders arrays and objects as tables, lists, trees, mermaid diagrams, or rich card-list/detail views via kvx DisplaySchema. Respects --quiet and --no-color flags.",
			Category:    "utility",
			Tags:        []string{"output", "message", "terminal", "logging", "display", "data", "kvx"},
			WhatIf:      whatIf,
			Capabilities: []provider.Capability{
				provider.CapabilityAction,
			},
			Schema: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
				fieldMessage: schemahelper.StringProp(
					"The message text to output (text mode). Mutually exclusive with 'data'. For dynamic interpolation, use tmpl: or expr: ValueRef on this input instead of passing templates directly. Limited to 8192 characters unless type is 'raw'.",
					schemahelper.WithExample("Deployment completed successfully"),
				),
				fieldData: schemahelper.AnyProp(
					"Structured data to render (data mode). Mutually exclusive with 'message'. Arrays render as tables or card lists; objects render as key-value views or sectioned detail views. Supports rslvr:/expr:/tmpl: ValueRef.",
				),
				fieldFormat: schemahelper.StringProp(
					"Output format for data mode rendering. Controls the visual layout when 'data' is set. Defaults to 'auto' when used with 'data'.",
					schemahelper.WithEnum(formatAuto, formatTable, formatList, formatTree, formatMermaid, formatJSON, formatYAML, formatQuiet),
					schemahelper.WithMaxLength(*ptrs.IntPtr(10)),
				),
				fieldDisplay: schemahelper.ObjectProp(
					"Display schema for rich card-list and detail views via kvx. Maps directly to kvx DisplaySchema: list (titleField, subtitleField, badgeFields, secondaryFields), detail (titleField, sections with fields and layout), collectionTitle, and icon. Requires 'data'.",
					nil,
					nil,
				),
				fieldColumnHints: schemahelper.ObjectProp(
					"JSON Schema with x-kvx-* extensions for column-level rendering control. Supports x-kvx-header (rename), x-kvx-maxWidth, and x-kvx-visible (hide columns). Requires 'data'.",
					nil,
					nil,
				),
				fieldColumnOrder: schemahelper.ArrayProp(
					"Preferred column display order for table rendering. Fields not listed are appended in their natural order. Recommended for deterministic column layout. Requires 'data'.",
					schemahelper.WithItems(schemahelper.StringProp("Column field name")),
					schemahelper.WithMaxItems(50),
				),
				fieldExpand: schemahelper.BoolProp(
					"When true, the raw data is returned directly as the action output (for downstream action consumption). When false (default), output is wrapped as {success: true, data: <raw>}. Requires 'data'.",
				),
				fieldLabel: schemahelper.StringProp(
					"Optional contextual prefix displayed in brackets before the message text (e.g., 'deploy', 'step 2/5'). Rendered as dimmed [label] between the icon and message. Supports tmpl: and expr: ValueRef for dynamic labels.",
					schemahelper.WithExample("step 1/3"),
					schemahelper.WithMaxLength(maxLabelLength),
				),
				fieldType: schemahelper.StringProp(
					"The message type that determines icon and color styling. Maps to built-in terminal output styles: success (✅ green), warning (⚠️ yellow), error (❌ red), info (💡 cyan), debug (🐛 magenta), plain (no styling), raw (no formatting, bypasses maxLength).",
					schemahelper.WithEnum(typeSuccess, typeWarning, typeError, typeInfo, typeDebug, typePlain, typeRaw),
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
				{
					Name:        "Data table",
					Description: "Render structured data as a default kvx table",
					YAML: `name: show-admins
provider: message
inputs:
  data:
    rslvr: administrators
  label: Administrators
  columnOrder: [name, email, role]`,
				},
				{
					Name:        "Data table with column hints",
					Description: "Render data as a table with renamed headers and hidden columns",
					YAML: `name: show-admins
provider: message
inputs:
  data:
    rslvr: administrators
  label: Administrators
  columnOrder: [name, email, role]
  columnHints:
    properties:
      name:
        x-kvx-header: "Full Name"
      metadata:
        x-kvx-visible: false`,
				},
				{
					Name:        "Data card list with DisplaySchema",
					Description: "Render an array as a rich card list with badge fields and sectioned detail view",
					YAML: `name: show-projects
provider: message
inputs:
  data:
    rslvr: gcp_projects
  display:
    collectionTitle: GCP Projects
    list:
      titleField: name
      subtitleField: type
      badgeFields: [environmentCode]
    detail:
      titleField: name
      sections:
        - title: Identity
          fields: [name, number, folderID]
        - title: Environment
          fields: [environment, environmentCode]
          layout: inline`,
				},
				{
					Name:        "Data tree view",
					Description: "Render nested data as an ASCII tree",
					YAML: `name: show-deps
provider: message
inputs:
  data:
    rslvr: dependency_tree
  format: tree
  label: Dependencies`,
				},
				{
					Name:        "Raw output",
					Description: "Write raw content to stdout without formatting or length limits",
					YAML: `name: emit-json
provider: message
inputs:
  message:
    rslvr: registry_index_json
  type: raw`,
				},
			},
		},
	}
}

// Descriptor returns the provider descriptor.
func (p *MessageProvider) Descriptor() *provider.Descriptor {
	return p.descriptor
}

// Execute outputs a styled message or renders structured data to the terminal.
func (p *MessageProvider) Execute(ctx context.Context, input any) (*provider.Output, error) {
	inputs, ok := input.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: expected map[string]any, got %T", ProviderName, input)
	}

	// Determine mode: data or text.
	_, hasData := inputs[fieldData]
	_, hasMessage := inputs[fieldMessage]

	if hasData && hasMessage {
		return nil, fmt.Errorf("%s: 'data' and 'message' are mutually exclusive", ProviderName)
	}
	if !hasData && !hasMessage {
		return nil, fmt.Errorf("%s: either 'data' or 'message' must be provided", ProviderName)
	}

	// Validate that data-only fields are not used without data.
	if !hasData {
		for _, field := range dataOnlyFields {
			if _, ok := inputs[field]; ok {
				return nil, fmt.Errorf("%s: '%s' requires 'data' to be set", ProviderName, field)
			}
		}
	}

	// Check for dry-run mode.
	if provider.DryRunFromContext(ctx) {
		return p.executeDryRun(inputs)
	}

	if hasData {
		return p.executeDataMode(ctx, inputs)
	}
	return p.executeTextMode(ctx, inputs)
}

// executeTextMode renders a styled text message to the terminal.
func (p *MessageProvider) executeTextMode(ctx context.Context, inputs map[string]any) (*provider.Output, error) {
	lgr := logger.FromContext(ctx)

	msgStr, ok := inputs[fieldMessage].(string)
	if !ok || msgStr == "" {
		return nil, fmt.Errorf("%s: 'message' must be a non-empty string", ProviderName)
	}

	// Get configuration fields.
	msgType := stringField(inputs, fieldType, typeInfo)
	dest := stringField(inputs, fieldDestination, destStdout)
	newline := boolField(inputs, fieldNewline, true)

	// Raw mode: write content directly without formatting or length limits.
	if msgType == typeRaw {
		return p.executeRawMode(ctx, inputs, msgStr, dest, newline)
	}

	// Enforce maxLength at runtime for non-raw types (schema no longer carries it
	// because JSON Schema cannot conditionally apply maxLength based on type).
	if len(msgStr) > maxMessageLength {
		return nil, fmt.Errorf("%s: message exceeds maximum length of %d characters (use type: raw for large output)", ProviderName, maxMessageLength)
	}

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

// rawOnlyRejectedFields are input fields that are not supported in raw mode.
var rawOnlyRejectedFields = []string{fieldStyle, fieldLabel}

// executeRawMode writes content directly to stdout without formatting, color,
// icons, or length limits. This is intended for machine-readable output such as
// JSON or YAML blobs produced by solutions.
func (p *MessageProvider) executeRawMode(ctx context.Context, inputs map[string]any, content, dest string, newline bool) (*provider.Output, error) {
	lgr := logger.FromContext(ctx)

	// Reject fields that are meaningless in raw mode.
	for _, field := range rawOnlyRejectedFields {
		if _, ok := inputs[field]; ok {
			return nil, fmt.Errorf("%s: '%s' is not supported with type: raw", ProviderName, field)
		}
	}

	// Build the raw output (no ANSI, no icon, no wrapping).
	output := content
	if newline {
		output += "\n"
	}

	// Write to the terminal. Raw mode ignores --quiet because it is intended
	// for machine-readable output that callers may pipe or redirect.
	streamed := false
	ioStreams, ok := provider.IOStreamsFromContext(ctx)
	if ok && ioStreams != nil {
		if err := p.writeToTerminal(ioStreams, output, dest); err != nil {
			return nil, fmt.Errorf("%s: failed to write raw output: %w", ProviderName, err)
		}
		streamed = true
	}

	lgr.V(1).Info("Raw output",
		fieldDestination, dest,
		"length", len(content),
		"written", streamed,
	)

	return &provider.Output{
		Data: map[string]any{
			"success": true,
			"message": content,
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

// whatIf generates a dry-run description for the message provider.
func whatIf(_ context.Context, input any) (string, error) {
	inputs, ok := input.(map[string]any)
	if !ok {
		return "", nil
	}

	// Data mode WhatIf.
	if _, hasData := inputs[fieldData]; hasData {
		format := stringField(inputs, fieldFormat, formatAuto)
		label := stringField(inputs, fieldLabel, "")
		hasDisplay := false
		if _, ok := inputs[fieldDisplay].(map[string]any); ok {
			hasDisplay = true
		}
		desc := fmt.Sprintf("Would render data as %s", format)
		if hasDisplay {
			desc += " with display schema"
		}
		if label != "" {
			desc += fmt.Sprintf(" [%s]", label)
		}
		return desc, nil
	}

	// Text mode WhatIf.
	msgType := stringField(inputs, fieldType, typeInfo)
	dest := stringField(inputs, fieldDestination, destStdout)
	label := stringField(inputs, fieldLabel, "")
	msg := stringField(inputs, fieldMessage, "")

	// Raw mode: describe the raw output with content length.
	if msgType == typeRaw {
		if msg == "" {
			return fmt.Sprintf("Would write raw output to %s", dest), nil
		}
		return fmt.Sprintf("Would write raw output (%d bytes) to %s", len(msg), dest), nil
	}

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
}

// executeDataMode renders structured data using kvx.
func (p *MessageProvider) executeDataMode(ctx context.Context, inputs map[string]any) (*provider.Output, error) {
	lgr := logger.FromContext(ctx)
	data := inputs[fieldData]

	ioStreams, _ := provider.IOStreamsFromContext(ctx)

	noColor := false
	isQuiet := false
	if runSettings, ok := settings.FromContext(ctx); ok {
		noColor = runSettings.NoColor
		isQuiet = runSettings.IsQuiet
	}

	format := stringField(inputs, fieldFormat, formatAuto)
	expand := boolField(inputs, fieldExpand, false)

	// Validate display and columnHints types early.
	if v, exists := inputs[fieldDisplay]; exists {
		if _, ok := v.(map[string]any); !ok {
			return nil, fmt.Errorf("%s: 'display' must be an object, got %T", ProviderName, v)
		}
	}
	if v, exists := inputs[fieldColumnHints]; exists {
		if _, ok := v.(map[string]any); !ok {
			return nil, fmt.Errorf("%s: 'columnHints' must be an object, got %T", ProviderName, v)
		}
	}

	// Resolve the output writer based on destination, honoring stderr when requested.
	dest := stringField(inputs, fieldDestination, destStdout)
	out := resolveDataWriter(ioStreams, dest)
	streamed := false
	if out != nil && !isQuiet {
		switch format {
		case formatJSON:
			enc := json.NewEncoder(out)
			enc.SetIndent("", "  ")
			if err := enc.Encode(data); err != nil {
				return nil, fmt.Errorf("%s: failed to encode JSON: %w", ProviderName, err)
			}
			streamed = true
		case formatYAML:
			yEnc := yaml.NewEncoder(out)
			if err := yEnc.Encode(data); err != nil {
				return nil, fmt.Errorf("%s: failed to encode YAML: %w", ProviderName, err)
			}
			if err := yEnc.Close(); err != nil {
				return nil, fmt.Errorf("%s: failed to flush YAML output: %w", ProviderName, err)
			}
			streamed = true
		case formatQuiet:
			// No output.
		default:
			if err := p.renderVisual(ctx, data, inputs, out, noColor, format); err != nil {
				return nil, err
			}
			streamed = true
		}
	}

	lgr.V(1).Info("Data output",
		fieldFormat, format,
		"expand", expand,
		"written", streamed,
	)

	// Build output based on expand flag.
	var outputData any
	if expand {
		outputData = data
	} else {
		outputData = map[string]any{
			"success": true,
			"data":    data,
		}
	}

	return &provider.Output{
		Data:     outputData,
		Streamed: streamed,
	}, nil
}

// resolveDataWriter selects the output writer for data mode based on the destination input.
// Returns nil when IOStreams are unavailable (embedder/structured-output mode).
func resolveDataWriter(ioStreams *provider.IOStreams, dest string) io.Writer {
	if ioStreams == nil {
		return nil
	}
	switch dest {
	case destStderr:
		return ioStreams.ErrOut
	default:
		return ioStreams.Out
	}
}

// renderVisual renders structured data using kvx visual output (table, list, tree, mermaid,
// or DisplaySchema-driven card-list/detail views). Writes directly to the provided writer.
func (p *MessageProvider) renderVisual(ctx context.Context, data any, inputs map[string]any, out io.Writer, noColor bool, format string) error {
	// Default AppName to the runtime binary name for embedder compatibility.
	appName := settings.BinaryNameFromContext(ctx)
	if label := stringField(inputs, fieldLabel, ""); label != "" {
		appName = label
	}

	// If display schema is present, use Snapshot() for rich rendering.
	if displayMap, ok := inputs[fieldDisplay].(map[string]any); ok {
		displayJSON, err := displaySchemaFromMap(displayMap)
		if err != nil {
			return fmt.Errorf("%s: invalid display config: %w", ProviderName, err)
		}
		opts := []kvx.Option{
			kvx.WithIO(nil, out),
			kvx.WithNoColor(noColor),
			kvx.WithDisplaySchemaJSON(displayJSON),
			kvx.WithAppName(appName),
		}
		if hintsMap, ok := inputs[fieldColumnHints].(map[string]any); ok {
			hintsJSON, err := columnHintsToJSON(hintsMap)
			if err != nil {
				return fmt.Errorf("%s: invalid column hints: %w", ProviderName, err)
			}
			opts = append(opts, kvx.WithSchemaJSON(hintsJSON))
		}
		snapshot, err := kvx.Snapshot(data, opts...)
		if err != nil {
			return fmt.Errorf("%s: failed to render display: %w", ProviderName, err)
		}
		_, err = fmt.Fprint(out, snapshot)
		return err
	}

	// No display schema -- use View() for basic layout rendering.
	opts := []kvx.Option{
		kvx.WithIO(nil, out),
		kvx.WithNoColor(noColor),
		kvx.WithLayout(format),
		kvx.WithAppName(appName),
	}
	if order, ok := inputs[fieldColumnOrder].([]any); ok {
		strOrder := make([]string, 0, len(order))
		for i, v := range order {
			s, ok := v.(string)
			if !ok {
				return fmt.Errorf("%s: columnOrder[%d] must be a string, got %T", ProviderName, i, v)
			}
			strOrder = append(strOrder, s)
		}
		opts = append(opts, kvx.WithColumnOrder(strOrder))
	}
	if hintsMap, ok := inputs[fieldColumnHints].(map[string]any); ok {
		hintsJSON, err := columnHintsToJSON(hintsMap)
		if err != nil {
			return fmt.Errorf("%s: invalid column hints: %w", ProviderName, err)
		}
		opts = append(opts, kvx.WithSchemaJSON(hintsJSON))
	}
	return kvx.View(data, opts...)
}

// executeDryRun returns a simulated output for dry-run mode.
func (p *MessageProvider) executeDryRun(inputs map[string]any) (*provider.Output, error) {
	_, hasData := inputs[fieldData]

	if hasData {
		format := stringField(inputs, fieldFormat, formatAuto)
		label := stringField(inputs, fieldLabel, "")
		hasDisplay := false
		if _, ok := inputs[fieldDisplay].(map[string]any); ok {
			hasDisplay = true
		}
		desc := fmt.Sprintf("[dry-run] Would render data as %s", format)
		if hasDisplay {
			desc += " with display schema"
		}
		if label != "" {
			desc += fmt.Sprintf(" [%s]", label)
		}

		// Return the same output shape as non-dry-run so downstream
		// actions/resolvers see the correct structure during dry-run.
		data := inputs[fieldData]
		expand := boolField(inputs, fieldExpand, false)
		var outputData any
		if expand {
			outputData = data
		} else {
			outputData = map[string]any{
				"success": true,
				"data":    data,
			}
		}
		return &provider.Output{
			Data: outputData,
			Metadata: map[string]any{
				"description": desc,
			},
		}, nil
	}

	msgType := stringField(inputs, fieldType, typeInfo)
	dest := stringField(inputs, fieldDestination, destStdout)
	label := stringField(inputs, fieldLabel, "")
	msg := stringField(inputs, fieldMessage, "<dynamic>")

	// Raw mode dry-run.
	if msgType == typeRaw {
		desc := fmt.Sprintf("[dry-run] Would write raw output (%d bytes) to %s", len(msg), dest)
		return &provider.Output{
			Data: map[string]any{
				"success": true,
				"message": msg,
			},
			Metadata: map[string]any{
				"description": desc,
			},
		}, nil
	}

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
