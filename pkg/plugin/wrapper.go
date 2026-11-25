// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/redact"
)

// ProviderWrapper wraps a plugin provider to implement the provider.Provider interface
type ProviderWrapper struct {
	client       *Client
	providerName string
	descriptor   *provider.Descriptor
	configured   bool
	mu           sync.RWMutex
}

// wrapperConfig holds configuration for NewProviderWrapper.
type wrapperConfig struct {
	ctx context.Context
}

// WrapperOption configures NewProviderWrapper.
type WrapperOption func(*wrapperConfig)

// WithContext sets the context used during wrapper initialisation (e.g. for
// the initial GetProviderDescriptor RPC). Defaults to context.Background().
// A nil context is silently replaced with context.Background().
func WithContext(ctx context.Context) WrapperOption {
	return func(c *wrapperConfig) {
		if ctx != nil {
			c.ctx = ctx
		}
	}
}

// NewProviderWrapper creates a new provider wrapper for a plugin provider
func NewProviderWrapper(client *Client, providerName string, opts ...WrapperOption) (*ProviderWrapper, error) {
	wCfg := wrapperConfig{ctx: context.Background()}
	for _, o := range opts {
		o(&wCfg)
	}

	// Get the descriptor to validate the provider exists
	desc, err := client.GetProviderDescriptor(wCfg.ctx, providerName)
	if err != nil {
		return nil, fmt.Errorf("failed to get provider descriptor: %w", err)
	}

	// GetProviders is the authoritative source of externally addressable provider
	// names. Normalize the descriptor name so registry lookup uses the same key
	// that resolver steps and action definitions reference.
	if desc.Name != providerName {
		desc.Name = providerName
	}
	if len(desc.OutputSchemas) == 0 {
		desc.OutputSchemas = fallbackOutputSchemas(desc.Capabilities)
	}

	w := &ProviderWrapper{
		client:       client,
		providerName: providerName,
		descriptor:   desc,
	}

	// Wire up WhatIf to call the plugin over gRPC
	name := providerName
	desc.WhatIf = func(ctx context.Context, input any) (string, error) {
		var inputs map[string]any
		if input != nil {
			var ok bool
			inputs, ok = input.(map[string]any)
			if !ok {
				return "", nil
			}
		}
		return client.DescribeWhatIf(ctx, name, inputs)
	}

	// Wire ExtractDependencies when the plugin declares custom extraction.
	// protoToDescriptor sets a placeholder function when the proto
	// has_extract_dependencies flag is true; replace it with a real RPC call.
	// Note: Descriptor.ExtractDependencies does not accept a context, so we use
	// a timeout to prevent blocking indefinitely on an unresponsive plugin.
	if desc.ExtractDependencies != nil {
		desc.ExtractDependencies = func(inputs map[string]any) []string {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			deps, err := client.ExtractDependencies(ctx, name, inputs)
			if err != nil {
				logger.FromContext(ctx).V(1).Info("plugin ExtractDependencies failed, falling back to generic extraction",
					"provider", name, "error", err)
				return nil
			}
			return deps
		}
	}

	return w, nil
}

func fallbackOutputSchemas(capabilities []provider.Capability) map[provider.Capability]*jsonschema.Schema {
	if len(capabilities) == 0 {
		return nil
	}

	schemas := make(map[provider.Capability]*jsonschema.Schema, len(capabilities))
	for _, capability := range capabilities {
		schemas[capability] = fallbackOutputSchema(capability)
	}

	return schemas
}

func fallbackOutputSchema(capability provider.Capability) *jsonschema.Schema {
	switch capability {
	case provider.CapabilityValidation:
		return &jsonschema.Schema{
			Type: "object",
			Properties: map[string]*jsonschema.Schema{
				"valid":  {Type: "boolean"},
				"errors": {Type: "array"},
			},
		}
	case provider.CapabilityAuthentication:
		return &jsonschema.Schema{
			Type: "object",
			Properties: map[string]*jsonschema.Schema{
				"authenticated": {Type: "boolean"},
				"token":         {Type: "string"},
			},
		}
	case provider.CapabilityAction:
		return &jsonschema.Schema{
			Type: "object",
			Properties: map[string]*jsonschema.Schema{
				"success": {Type: "boolean"},
			},
		}
	case provider.CapabilityFrom, provider.CapabilityTransform, provider.CapabilityState, provider.CapabilityKubeconfig:
		fallthrough
	default:
		return &jsonschema.Schema{
			Type: "object",
			Properties: map[string]*jsonschema.Schema{
				"result": {},
			},
		}
	}
}

// Descriptor returns the provider descriptor
func (w *ProviderWrapper) Descriptor() *provider.Descriptor {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.descriptor
}

// UsesDynamicHostCredentials reports that out-of-process plugin providers may
// request secrets or auth tokens from HostService during Execute, requiring
// redacting stream wrappers even when the run tracker starts empty.
func (w *ProviderWrapper) UsesDynamicHostCredentials() bool {
	return true
}

func scrubPluginOutput(tracker *redact.Tracker, out *provider.Output) *provider.Output {
	if out == nil || tracker == nil || tracker.Count() == 0 {
		return out
	}
	cp := *out
	cp.Warnings = tracker.RedactSlice(cp.Warnings)
	cp.Metadata = tracker.RedactMap(cp.Metadata)
	return &cp
}

func scrubPluginOutputOnError(tracker *redact.Tracker, out *provider.Output) *provider.Output {
	if out == nil || tracker == nil || tracker.Count() == 0 {
		return out
	}
	cp := *out
	if cp.Data != nil {
		cp.Data = tracker.RedactAny(cp.Data)
	}
	cp.Warnings = tracker.RedactSlice(cp.Warnings)
	cp.Metadata = tracker.RedactMap(cp.Metadata)
	return &cp
}

// wrapProviderError prefixes err with "<name>: " unless err already carries
// that prefix, preserving wrapped error chains such as exitcode.ExitError.
func wrapProviderError(name string, err error) error {
	if err == nil {
		return nil
	}
	if name == "" || strings.HasPrefix(err.Error(), name+": ") {
		return err
	}
	return fmt.Errorf("%s: %w", name, err)
}

func mergeCapturedStreamChunks(out *provider.Output, capturedStdout, capturedStderr string) *provider.Output {
	logs := provider.CapturedStreamLogs{
		Stdout: capturedStdout,
		Stderr: capturedStderr,
	}
	if logs.Empty() {
		if out == nil || !out.Streamed {
			return out
		}
		cp := *out
		cp.Streamed = false
		return &cp
	}
	if out == nil {
		return &provider.Output{
			Data:     logs.MergeIntoData(nil),
			Streamed: false,
		}
	}
	cp := *out
	cp.Streamed = false
	cp.Data = logs.MergeIntoData(cp.Data)
	return &cp
}

func finalizeStreamOutput(
	tracker *redact.Tracker,
	sink *provider.CapturedStreamSink,
	out *provider.Output,
	streamedBytes, canStream, isError bool,
	capturedStdout, capturedStderr string,
) *provider.Output {
	captured := provider.CapturedStreamLogs{
		Stdout: capturedStdout,
		Stderr: capturedStderr,
	}
	if !canStream && sink != nil {
		sink.Set(capturedStdout, capturedStderr)
	}

	if out == nil {
		if canStream {
			if streamedBytes {
				return &provider.Output{Streamed: true}
			}
			return nil
		}
		if sink != nil {
			if !captured.Empty() {
				return &provider.Output{Streamed: false}
			}
			return nil
		}
		return mergeCapturedStreamChunks(nil, capturedStdout, capturedStderr)
	}

	var scrubbed *provider.Output
	if isError {
		scrubbed = scrubPluginOutputOnError(tracker, out)
	} else {
		scrubbed = scrubPluginOutput(tracker, out)
	}
	if !canStream {
		// When a CapturedStreamSink is attached (e.g. action.Executor), keep
		// Output.Data clean so resultSchema validation behaves identically across
		// terminal and JSON/YAML output modes before captured logs are merged.
		if sink != nil {
			if !scrubbed.Streamed {
				return scrubbed
			}
			cp := *scrubbed
			cp.Streamed = false
			return &cp
		}
		return mergeCapturedStreamChunks(scrubbed, capturedStdout, capturedStderr)
	}
	// Honor the plugin's Result.Streamed flag rather than inferring Streamed from
	// any received chunk so a provider that streams progress to stderr while
	// returning buffered Data["stdout"] with Streamed=false never drops stdout.
	return scrubbed
}

// Execute executes the provider. Streaming execution is attempted whenever
// caller-supplied IOStreams are present (so incremental output reaches the
// terminal in real time) OR when executing an action without terminal
// IOStreams (so structured -o json/-o yaml runs capture/redact streamed chunks
// and retain StreamChunk.Result on failure for scafctl-plugin-sdk providers).
// Falls back to the unary RPC when the plugin returns ErrStreamingNotSupported.
//
// Streaming error contract: the GRPCClient.ExecuteProviderStream callback
// receives stdout/stderr chunks as they arrive. Terminal errors are delivered
// in two ways: (1) the callback receives a chunk with Error set, and
// (2) ExecuteProviderStream returns a non-nil error. This wrapper checks
// the return value first — if it signals ErrStreamingNotSupported it falls
// back to unary; for other errors it returns immediately. When the stream
// completes successfully (nil return), any chunk-level error is surfaced.
func (w *ProviderWrapper) Execute(ctx context.Context, input any) (*provider.Output, error) {
	lgr := logger.FromContext(ctx)

	inputs, ok := input.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: expected map[string]any, got %T", w.descriptor.Name, input)
	}

	tracker := redact.FromContext(ctx)
	if tracker == nil {
		tracker = redact.NewTracker()
		ctx = redact.WithTracker(ctx, tracker)
	}

	lgr.V(1).Info("executing plugin provider", "provider", w.descriptor.Name)

	// Attempt streaming execution whenever caller-supplied IOStreams are present
	// (live terminal streaming) OR when executing an action (capturing and
	// redacting streamed chunks and preserving StreamChunk.Result on failure for
	// structured -o json/-o yaml envelopes). Terminal writes are gated on canStream.
	rawStreams, hasIO := provider.IOStreamsFromContext(ctx)
	canStream := hasIO && rawStreams != nil && !provider.IsFallbackIOStreams(ctx)
	execMode, _ := provider.ExecutionModeFromContext(ctx)
	sink, _ := provider.CapturedStreamSinkFromContext(ctx)
	if canStream || execMode == provider.CapabilityAction {
		streamCtx := ctx
		flushStreams := func() {}
		var ioStreams *provider.IOStreams
		var capturedOutBuf, capturedErrBuf bytes.Buffer
		var captureOutWriter, captureErrWriter *redact.StreamWriter
		if canStream {
			streamCtx, flushStreams = provider.WrapDirectiveAlignedRedactingIOStreams(ctx, tracker, w)
			ioStreams, _ = provider.IOStreamsFromContext(streamCtx)
		} else {
			captureOutWriter = redact.NewStreamWriter(&capturedOutBuf, tracker)
			captureErrWriter = redact.NewStreamWriter(&capturedErrBuf, tracker)
		}
		var lastOutput *provider.Output
		var streamChunkErr string
		var streamedBytes bool
		streamErr := w.client.ExecuteProviderStream(streamCtx, w.providerName, inputs, func(chunk StreamChunk) {
			if len(chunk.Stdout) > 0 {
				if canStream {
					if ioStreams != nil && ioStreams.Out != nil {
						_, _ = ioStreams.Out.Write(chunk.Stdout)
						streamedBytes = true
					}
				} else if captureOutWriter != nil {
					_, _ = captureOutWriter.Write(chunk.Stdout)
				}
			}
			if len(chunk.Stderr) > 0 {
				if canStream {
					if ioStreams != nil && ioStreams.ErrOut != nil {
						_, _ = ioStreams.ErrOut.Write(chunk.Stderr)
						streamedBytes = true
					}
				} else if captureErrWriter != nil {
					_, _ = captureErrWriter.Write(chunk.Stderr)
				}
			}
			if chunk.Error != "" {
				streamChunkErr = chunk.Error
			}
			if chunk.Result != nil {
				lastOutput = chunk.Result
			}
		})
		flushStreams()
		if captureOutWriter != nil {
			_ = captureOutWriter.Flush()
		}
		if captureErrWriter != nil {
			_ = captureErrWriter.Flush()
		}
		capturedOut := capturedOutBuf.String()
		capturedErr := capturedErrBuf.String()
		if streamErr == nil {
			if streamChunkErr != "" {
				return finalizeStreamOutput(tracker, sink, lastOutput, streamedBytes, canStream, true, capturedOut, capturedErr),
					tracker.RedactError(wrapProviderError(w.descriptor.Name, errors.New(streamChunkErr)))
			}
			lgr.V(1).Info("plugin provider completed (streaming)", "provider", w.descriptor.Name)
			if out := finalizeStreamOutput(tracker, sink, lastOutput, streamedBytes, canStream, false, capturedOut, capturedErr); out != nil {
				return out, nil
			}
			return &provider.Output{}, nil
		}
		// Only fall through for streaming-not-supported; other errors are real failures.
		if !errors.Is(streamErr, ErrStreamingNotSupported) {
			return finalizeStreamOutput(tracker, sink, lastOutput, streamedBytes, canStream, true, capturedOut, capturedErr),
				tracker.RedactError(wrapProviderError(w.descriptor.Name, streamErr))
		}
		lgr.V(2).Info("plugin does not support streaming, falling back to unary", "provider", w.descriptor.Name)
	}

	result, err := w.client.ExecuteProvider(ctx, w.providerName, inputs)
	if err != nil {
		return scrubPluginOutputOnError(tracker, result), tracker.RedactError(wrapProviderError(w.descriptor.Name, err))
	}

	lgr.V(1).Info("plugin provider completed", "provider", w.descriptor.Name)
	return scrubPluginOutput(tracker, result), nil
}

// Client returns the underlying plugin client
func (w *ProviderWrapper) Client() *Client {
	return w.client
}

// Configure sends host-side configuration to the plugin provider.
// This should be called once after wrapper creation, before any Execute calls.
func (w *ProviderWrapper) Configure(ctx context.Context, cfg ProviderConfig) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.configured {
		return nil
	}

	if err := w.client.ConfigureProvider(ctx, w.providerName, cfg); err != nil {
		return fmt.Errorf("%s: configure: %w", w.descriptor.Name, err)
	}

	w.configured = true
	return nil
}

// ExecuteStream executes the provider with streaming output.
// The callback receives chunks as they arrive from the plugin.
func (w *ProviderWrapper) ExecuteStream(ctx context.Context, input any, cb func(StreamChunk)) error {
	inputs, ok := input.(map[string]any)
	if !ok {
		return fmt.Errorf("%s: expected map[string]any, got %T", w.descriptor.Name, input)
	}

	return w.client.ExecuteProviderStream(ctx, w.providerName, inputs, cb)
}

// RegisterPluginProviders discovers plugins and registers them with the provider registry.
// After registration, each wrapper is configured with the provided ProviderConfig.
// If cfg is nil, configuration is skipped (providers will use defaults).
// Returns the created clients; the caller should defer calling KillAll(clients)
// to clean up plugin processes on exit.
func RegisterPluginProviders(ctx context.Context, registry *provider.Registry, pluginDirs []string, cfg *ProviderConfig, clientOpts ...ClientOption) ([]*Client, error) {
	// Discover plugins
	clients, err := Discover(pluginDirs, clientOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to discover plugins: %w", err)
	}

	// Register providers from each plugin
	for _, client := range clients {
		providers, err := client.GetProviders(ctx)
		if err != nil {
			logger.FromContext(ctx).V(1).Info("failed to get providers from plugin", "plugin", client.Name(), "error", err)
			client.Kill()
			continue
		}

		lgr := logger.FromContext(ctx)
		for _, providerName := range providers {
			wrapper, err := NewProviderWrapper(client, providerName, WithContext(ctx))
			if err != nil {
				lgr.V(1).Info("failed to create provider wrapper", "provider", providerName, "plugin", client.Name(), "error", err)
				continue
			}

			if err := registry.Register(wrapper); err != nil {
				lgr.V(1).Info("failed to register plugin provider", "provider", providerName, "plugin", client.Name(), "error", err)
				continue
			}

			// Send host configuration to the plugin provider
			if cfg != nil {
				if err := wrapper.Configure(ctx, *cfg); err != nil {
					lgr := logger.FromContext(ctx)
					lgr.V(1).Info("failed to configure plugin provider",
						"provider", providerName,
						"error", err)
				}
			}
		}
	}

	return clients, nil
}

// KillAll terminates all plugin processes in the given client list.
// This is safe to call with a nil or empty slice.
func KillAll(clients []*Client) {
	for _, c := range clients {
		if c != nil {
			c.Kill()
		}
	}
}
