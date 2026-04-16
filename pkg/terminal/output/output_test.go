// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package output

import (
	"fmt"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/terminal"
)

func TestParseOutputFormat(t *testing.T) {
	tests := []struct {
		input     string
		wantFmt   OutputFormat
		wantFound bool
	}{
		{"auto", OutputFormatAuto, true},
		{"", OutputFormatAuto, true},
		{"table", OutputFormatTable, true},
		{"list", OutputFormatList, true},
		{"json", OutputFormatJSON, true},
		{"yaml", OutputFormatYAML, true},
		{"quiet", OutputFormatQuiet, true},
		{"invalid", "", false},
		{"JSON", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, ok := ParseOutputFormat(tt.input)
			if ok != tt.wantFound {
				t.Errorf("ParseOutputFormat(%q) found=%v, want %v", tt.input, ok, tt.wantFound)
			}
			if got != tt.wantFmt {
				t.Errorf("ParseOutputFormat(%q) = %v, want %v", tt.input, got, tt.wantFmt)
			}
		})
	}
}

func TestBaseOutputFormats(t *testing.T) {
	formats := BaseOutputFormats()
	if len(formats) < 4 {
		t.Errorf("BaseOutputFormats() returned %d formats, want at least 4", len(formats))
	}
}

func TestIsStructuredFormat(t *testing.T) {
	if !IsStructuredFormat(OutputFormatJSON) {
		t.Error("JSON should be a structured format")
	}
	if !IsStructuredFormat(OutputFormatYAML) {
		t.Error("YAML should be a structured format")
	}
	if IsStructuredFormat(OutputFormatTable) {
		t.Error("table should not be a structured format")
	}
	if IsStructuredFormat(OutputFormatAuto) {
		t.Error("auto should not be a structured format")
	}
}

func TestIsKvxFormat(t *testing.T) {
	if !IsKvxFormat(OutputFormatAuto) {
		t.Error("auto should be a kvx format")
	}
	if !IsKvxFormat(OutputFormatTable) {
		t.Error("table should be a kvx format")
	}
	if !IsKvxFormat(OutputFormatList) {
		t.Error("list should be a kvx format")
	}
	if !IsKvxFormat("") {
		t.Error("empty string should be a kvx format")
	}
	if IsKvxFormat(OutputFormatJSON) {
		t.Error("json should not be a kvx format")
	}
}

func TestIsQuietFormat(t *testing.T) {
	if !IsQuietFormat(OutputFormatQuiet) {
		t.Error("quiet should be a quiet format")
	}
	if IsQuietFormat(OutputFormatJSON) {
		t.Error("json should not be a quiet format")
	}
}

func TestOutputFormat_String(t *testing.T) {
	if OutputFormatJSON.String() != "json" {
		t.Errorf("got %q, want %q", OutputFormatJSON.String(), "json")
	}
	if OutputFormatTable.String() != "table" {
		t.Errorf("got %q, want %q", OutputFormatTable.String(), "table")
	}
}

func TestValidateCommands(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{
			name:    "no arguments",
			args:    []string{},
			wantErr: false,
		},
		{
			name:    "single unknown command",
			args:    []string{"foo"},
			wantErr: true,
		},
		{
			name:    "multiple unknown commands",
			args:    []string{"foo", "bar"},
			wantErr: true,
		},
		{
			name:    "argument with spaces",
			args:    []string{"foo bar"},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotErr := ValidateCommands(tt.args)
			if gotErr != nil {
				if !tt.wantErr {
					t.Errorf("ValidateCommands() failed: %v", gotErr)
				}
				return
			}
			if tt.wantErr {
				t.Fatal("ValidateCommands() succeeded unexpectedly")
			}
		})
	}
}

func TestSuccessMessage(t *testing.T) {
	tests := []struct {
		name     string
		msg      string
		noColor  bool
		expected string
	}{
		{
			name:     "noColor true returns plain message",
			msg:      "Operation completed",
			noColor:  true,
			expected: "Operation completed",
		},
		{
			name:     "noColor false returns styled message",
			msg:      "Operation completed",
			noColor:  false,
			expected: " ✅ Operation completed", // styles.SuccessStyle.Render("✅") returns "✅" in tests
		},
		{
			name:     "empty message with noColor true",
			msg:      "",
			noColor:  true,
			expected: "",
		},
		{
			name:     "empty message with noColor false",
			msg:      "",
			noColor:  false,
			expected: " ✅ ", // styles.SuccessStyle.Render("✅") returns "✅" in tests
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SuccessMessage(tt.msg, tt.noColor)
			if got != tt.expected {
				t.Errorf("SuccessMessage(%q, %v) = %q; want %q", tt.msg, tt.noColor, got, tt.expected)
			}
		})
	}
}

func TestWarningMessage(t *testing.T) {
	tests := []struct {
		name     string
		msg      string
		noColor  bool
		expected string
	}{
		{
			name:     "noColor true returns plain message",
			msg:      "Check your input",
			noColor:  true,
			expected: "Check your input",
		},
		{
			name:     "noColor false returns styled message",
			msg:      "Check your input",
			noColor:  false,
			expected: " ⚠️ Check your input", // styles.WarningStyle.Render("⚠️") returns "⚠️" in tests
		},
		{
			name:     "empty message with noColor true",
			msg:      "",
			noColor:  true,
			expected: "",
		},
		{
			name:     "empty message with noColor false",
			msg:      "",
			noColor:  false,
			expected: " ⚠️ ", // styles.WarningStyle.Render("⚠️") returns "⚠️" in tests
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := WarningMessage(tt.msg, tt.noColor)
			if got != tt.expected {
				t.Errorf("WarningMessage(%q, %v) = %q; want %q", tt.msg, tt.noColor, got, tt.expected)
			}
		})
	}
}

func TestErrorMessage(t *testing.T) {
	tests := []struct {
		name     string
		msg      string
		noColor  bool
		expected string
	}{
		{
			name:     "noColor true returns plain message",
			msg:      "An error occurred",
			noColor:  true,
			expected: "An error occurred",
		},
		{
			name:     "noColor false returns styled message",
			msg:      "An error occurred",
			noColor:  false,
			expected: " ❌ An error occurred", // styles.ErrorStyle.Render("❌") returns "❌" in tests
		},
		{
			name:     "empty message with noColor true",
			msg:      "",
			noColor:  true,
			expected: "",
		},
		{
			name:     "empty message with noColor false",
			msg:      "",
			noColor:  false,
			expected: " ❌ ", // styles.ErrorStyle.Render("❌") returns "❌" in tests
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ErrorMessage(tt.msg, tt.noColor)
			if got != tt.expected {
				t.Errorf("ErrorMessage(%q, %v) = %q; want %q", tt.msg, tt.noColor, got, tt.expected)
			}
		})
	}
}

func TestInfoMessage(t *testing.T) {
	tests := []struct {
		name     string
		msg      string
		noColor  bool
		expected string
	}{
		{
			name:     "noColor true returns plain message",
			msg:      "Here is some info",
			noColor:  true,
			expected: "Here is some info",
		},
		{
			name:     "noColor false returns styled message",
			msg:      "Here is some info",
			noColor:  false,
			expected: " 💡 Here is some info", // styles.InfoStyle.Render("💡") returns "💡" in tests
		},
		{
			name:     "empty message with noColor true",
			msg:      "",
			noColor:  true,
			expected: "",
		},
		{
			name:     "empty message with noColor false",
			msg:      "",
			noColor:  false,
			expected: " 💡 ", // styles.InfoStyle.Render("💡") returns "💡" in tests
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := InfoMessage(tt.msg, tt.noColor)
			if got != tt.expected {
				t.Errorf("InfoMessage(%q, %v) = %q; want %q", tt.msg, tt.noColor, got, tt.expected)
			}
		})
	}
}

func TestDebugMessage(t *testing.T) {
	tests := []struct {
		name     string
		msg      string
		noColor  bool
		expected string
	}{
		{
			name:     "noColor true returns plain message",
			msg:      "Debug information",
			noColor:  true,
			expected: "Debug information",
		},
		{
			name:     "noColor false returns styled message",
			msg:      "Debug information",
			noColor:  false,
			expected: " 🐛 Debug information", // styles.DebugStyle.Render("🐛") returns "🐛" in tests
		},
		{
			name:     "empty message with noColor true",
			msg:      "",
			noColor:  true,
			expected: "",
		},
		{
			name:     "empty message with noColor false",
			msg:      "",
			noColor:  false,
			expected: " 🐛 ", // styles.DebugStyle.Render("🐛") returns "🐛" in tests
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DebugMessage(tt.msg, tt.noColor)
			if got != tt.expected {
				t.Errorf("DebugMessage(%q, %v) = %q; want %q", tt.msg, tt.noColor, got, tt.expected)
			}
		})
	}
}

func TestVerboseMessage(t *testing.T) {
	tests := []struct {
		name     string
		msg      string
		noColor  bool
		expected string
	}{
		{
			name:     "noColor true returns plain message",
			msg:      "Verbose info",
			noColor:  true,
			expected: "Verbose info",
		},
		{
			name:     "noColor false returns styled message",
			msg:      "Verbose info",
			noColor:  false,
			expected: " ▸ Verbose info",
		},
		{
			name:     "empty message with noColor true",
			msg:      "",
			noColor:  true,
			expected: "",
		},
		{
			name:     "empty message with noColor false",
			msg:      "",
			noColor:  false,
			expected: " ▸ ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := VerboseMessage(tt.msg, tt.noColor)
			if got != tt.expected {
				t.Errorf("VerboseMessage(%q, %v) = %q; want %q", tt.msg, tt.noColor, got, tt.expected)
			}
		})
	}
}

func TestWriteDebug(t *testing.T) {
	tests := []struct {
		name     string
		msg      string
		noColor  bool
		expected string
	}{
		{
			name:     "noColor true returns plain message",
			msg:      "Debug output",
			noColor:  true,
			expected: "Debug output\n",
		},
		{
			name:     "noColor false returns styled message",
			msg:      "Debug output",
			noColor:  false,
			expected: " 🐛 Debug output\n",
		},
		{
			name:     "empty message with noColor true",
			msg:      "",
			noColor:  true,
			expected: "\n",
		},
		{
			name:     "empty message with noColor false",
			msg:      "",
			noColor:  false,
			expected: " 🐛 \n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ioStreams, outBuf, _ := terminal.NewTestIOStreams()
			WriteDebug(ioStreams, tt.msg, tt.noColor)
			got := outBuf.String()
			if got != tt.expected {
				t.Errorf("WriteDebug(%q, %v) wrote %q; want %q", tt.msg, tt.noColor, got, tt.expected)
			}
		})
	}
}

func TestWriteMessageOptions_WriteMessage(t *testing.T) {
	tests := []struct {
		name string // description of this test case
		// Named input parameters for receiver constructor.
		messageType MessageType
		noColor     bool
		exitOnError bool
		// Named input parameters for target function.
		msg            string
		wantErr        bool
		wantOut        string
		wantErrOut     string
		exitFuncCalled bool
	}{
		{
			name:        "success_message_with_color",
			messageType: MessageTypeSuccess,
			noColor:     false,
			exitOnError: false,
			msg:         "Operation completed",
			wantErr:     false,
			wantOut:     " ✅ Operation completed\n",
			wantErrOut:  "",
		},
		{
			name:        "success_message_no_color",
			messageType: MessageTypeSuccess,
			noColor:     true,
			exitOnError: false,
			msg:         "Operation completed",
			wantErr:     false,
			wantOut:     "Operation completed\n",
			wantErrOut:  "",
		},
		{
			name:        "warning_message_with_color",
			messageType: MessageTypeWarning,
			noColor:     false,
			exitOnError: false,
			msg:         "Check your input",
			wantErr:     false,
			wantOut:     " ⚠️ Check your input\n",
			wantErrOut:  "",
		},
		{
			name:        "warning_message_no_color",
			messageType: MessageTypeWarning,
			noColor:     true,
			exitOnError: false,
			msg:         "Check your input",
			wantErr:     false,
			wantOut:     "Check your input\n",
			wantErrOut:  "",
		},
		{
			name:        "info_message_with_color",
			messageType: MessageTypeInfo,
			noColor:     false,
			exitOnError: false,
			msg:         "Here is some info",
			wantErr:     false,
			wantOut:     " 💡 Here is some info\n",
			wantErrOut:  "",
		},
		{
			name:        "info_message_no_color",
			messageType: MessageTypeInfo,
			noColor:     true,
			exitOnError: false,
			msg:         "Here is some info",
			wantErr:     false,
			wantOut:     "Here is some info\n",
			wantErrOut:  "",
		},
		{
			name:        "error_message_with_color_no_exit",
			messageType: MessageTypeError,
			noColor:     false,
			exitOnError: false,
			msg:         "An error occurred",
			wantErr:     true,
			wantOut:     "",
			wantErrOut:  " ❌ An error occurred\n",
		},
		{
			name:        "error_message_no_color_no_exit",
			messageType: MessageTypeError,
			noColor:     true,
			exitOnError: false,
			msg:         "An error occurred",
			wantErr:     true,
			wantOut:     "",
			wantErrOut:  "An error occurred\n",
		},
		{
			name:           "error_message_with_exit",
			messageType:    MessageTypeError,
			noColor:        true,
			exitOnError:    true,
			msg:            "Fatal error",
			wantErr:        true,
			wantOut:        "",
			wantErrOut:     "Fatal error\n",
			exitFuncCalled: true,
		},
		{
			name:        "empty_message_success",
			messageType: MessageTypeSuccess,
			noColor:     true,
			exitOnError: false,
			msg:         "",
			wantErr:     false,
			wantOut:     "\n",
			wantErrOut:  "",
		},
		{
			name:        "default_message_type",
			messageType: MessageType("unknown"),
			noColor:     false,
			exitOnError: false,
			msg:         "Plain message",
			wantErr:     false,
			wantOut:     "Plain message\n",
			wantErrOut:  "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ioStreams, outBuf, errOutBuf := terminal.NewTestIOStreams()

			exitCalled := false
			exitFunc := func(code int) {
				exitCalled = true
			}

			o := NewWriteMessageOptions(ioStreams, tt.messageType, tt.noColor, tt.exitOnError)
			o.ExitFunc = exitFunc

			o.WriteMessage(tt.msg)

			gotOut := outBuf.String()
			if gotOut != tt.wantOut {
				t.Errorf("WriteMessage() Out = %q; want %q", gotOut, tt.wantOut)
			}

			gotErrOut := errOutBuf.String()
			if gotErrOut != tt.wantErrOut {
				t.Errorf("WriteMessage() ErrOut = %q; want %q", gotErrOut, tt.wantErrOut)
			}

			if exitCalled != tt.exitFuncCalled {
				t.Errorf("WriteMessage() exitFunc called = %v; want %v", exitCalled, tt.exitFuncCalled)
			}
		})
	}
}

func TestValidateOutputType(t *testing.T) {
	tests := []struct {
		name            string
		output          string
		validTypes      []string
		wantErr         bool
		expectedErrText string
	}{
		{
			name:       "empty output returns nil",
			output:     "",
			validTypes: []string{"json", "yaml"},
			wantErr:    false,
		},
		{
			name:       "valid output type json",
			output:     "json",
			validTypes: []string{"json", "yaml"},
			wantErr:    false,
		},
		{
			name:       "valid output type yaml",
			output:     "yaml",
			validTypes: []string{"json", "yaml"},
			wantErr:    false,
		},
		{
			name:            "invalid output type xml",
			output:          "xml",
			validTypes:      []string{"json", "yaml"},
			wantErr:         true,
			expectedErrText: "invalid output type: 'xml'. Valid types are: json, yaml",
		},
		{
			name:            "invalid output type with empty validTypes",
			output:          "foo",
			validTypes:      []string{},
			wantErr:         true,
			expectedErrText: "invalid output type: 'foo'. Valid types are: ",
		},
		{
			name:            "output type case sensitive",
			output:          "JSON",
			validTypes:      []string{"json", "yaml"},
			wantErr:         true,
			expectedErrText: "invalid output type: 'JSON'. Valid types are: json, yaml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateOutputType(tt.output, tt.validTypes)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ValidateOutputType(%q, %v) = nil; want error", tt.output, tt.validTypes)
				}
				if err.Error() != tt.expectedErrText {
					t.Errorf("ValidateOutputType(%q, %v) error = %q; want %q", tt.output, tt.validTypes, err.Error(), tt.expectedErrText)
				}
			} else if err != nil {
				t.Errorf("ValidateOutputType(%q, %v) = %v; want nil", tt.output, tt.validTypes, err)
			}
		})
	}
}

func TestWriteOutput(t *testing.T) {
	type testStruct struct {
		Foo string `json:"foo" yaml:"foo"`
		Bar int    `json:"bar" yaml:"bar"`
	}

	sample := testStruct{Foo: "baz", Bar: 42}

	tests := []struct {
		name        string
		outputType  string
		data        testStruct
		customFunc  CustomWriteOutputFunc[testStruct]
		wantOut     string
		wantErrOut  string
		wantErr     bool
		expectedErr string
	}{
		{
			name:       "json output",
			outputType: "json",
			data:       sample,
			wantOut:    `{"foo":"baz","bar":42}` + "\n",
			wantErr:    false,
		},
		{
			name:       "yaml output",
			outputType: "yaml",
			data:       sample,
			wantOut:    "foo: baz\nbar: 42\n\n",
			wantErr:    false,
		},
		{
			name:       "empty outputType with custom func",
			outputType: "",
			data:       sample,
			customFunc: func(ioStreams *terminal.IOStreams, data testStruct) error {
				fmt.Fprintf(ioStreams.Out, "custom: %s-%d", data.Foo, data.Bar)
				return nil
			},
			wantOut: "custom: baz-42",
			wantErr: false,
		},
		{
			name:        "empty outputType without custom func",
			outputType:  "",
			data:        sample,
			wantErr:     true,
			expectedErr: "no output type specified and no custom output function provided. This is an issue with the application",
		},
		{
			name:        "unsupported outputType",
			outputType:  "xml",
			data:        sample,
			wantErr:     true,
			expectedErr: "unsupported output type: xml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ioStreams, outBuf, errOutBuf := terminal.NewTestIOStreams()
			err := WriteOutput(ioStreams, tt.outputType, tt.data, tt.customFunc)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("WriteOutput() error = nil, want error")
				}
				if tt.expectedErr != "" && err.Error() != tt.expectedErr {
					t.Errorf("WriteOutput() error = %q, want %q", err.Error(), tt.expectedErr)
				}
			} else {
				if err != nil {
					t.Fatalf("WriteOutput() error = %v, want nil", err)
				}
				if got := outBuf.String(); got != tt.wantOut {
					t.Errorf("WriteOutput() out = %q, want %q", got, tt.wantOut)
				}
				if got := errOutBuf.String(); got != tt.wantErrOut {
					t.Errorf("WriteOutput() errOut = %q, want %q", got, tt.wantErrOut)
				}
			}
		})
	}
}
