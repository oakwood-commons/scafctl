// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/oakwood-commons/scafctl-plugin-sdk/plugin/proto"
	sdkprovider "github.com/oakwood-commons/scafctl-plugin-sdk/provider"
	"github.com/oakwood-commons/scafctl/pkg/plugin/plugintest"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	defaultCrashCode = 3
	crashFlushDelay  = 100 * time.Millisecond
)

// errNoHostService is reported when the host did not offer a HostService.
var errNoHostService = errors.New("host service not available")

// execute runs the per-call behavior selected by the "action" input key.
func (f *fixture) execute(ctx context.Context, name string, in map[string]any) (*proto.ExecuteProviderResponse, error) {
	switch actionOf(in) {
	case plugintest.ActionEnv:
		return okResponse(f.env(name, in))
	case plugintest.ActionSleep:
		select {
		case <-time.After(time.Duration(num(in, "ms", 0)) * time.Millisecond):
		case <-ctx.Done():
			return nil, status.FromContextError(ctx.Err()).Err()
		}
	case plugintest.ActionHang:
		// Deliberately ignores ctx: a wedged provider does not honor
		// cancellation.
		select {}
	case plugintest.ActionCrash:
		crash(in)
	case plugintest.ActionPanic:
		panic("lifecycleplugin: induced panic")
	case plugintest.ActionError:
		return &proto.ExecuteProviderResponse{
			Error:    str(in, "message"),
			ExitCode: int32(num(in, "exitCode", 0)), //nolint:gosec // test input, small values
		}, nil
	case plugintest.ActionMalformed:
		return &proto.ExecuteProviderResponse{Output: malformedOutput(str(in, "kind"))}, nil
	case plugintest.ActionAuthToken, plugintest.ActionAuthHandlers, plugintest.ActionSecret, plugintest.ActionSecrets:
		return f.hostCall(ctx, in)
	}
	return okResponse(f.echo(name, in))
}

// echo reports where and how the call was served: the provider, the process,
// a process-wide sequence number, and the input. The sequence number is the
// fixture's shared state, so tests can tell calls served by one process apart.
func (f *fixture) echo(name string, in map[string]any) map[string]any {
	return map[string]any{
		"provider": name,
		"pid":      os.Getpid(),
		"seq":      f.seq.Add(1),
		"input":    in,
	}
}

func (f *fixture) env(name string, in map[string]any) map[string]any {
	values := map[string]string{}
	for _, key := range strs(in, "keys") {
		if v, ok := os.LookupEnv(key); ok {
			values[key] = v
		}
	}
	out := map[string]any{
		"provider": name,
		"pid":      os.Getpid(),
		"seq":      f.seq.Add(1),
		"values":   values,
	}
	if all, _ := in["all"].(bool); all {
		out["environ"] = os.Environ()
	}
	return out
}

// hostClient returns the client for the host's HostService callback server,
// dialing it on first use.
func (f *fixture) hostClient() (proto.HostServiceClient, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.host != nil {
		return f.host, nil
	}
	if f.hostID == 0 || f.broker == nil {
		return nil, errNoHostService
	}
	conn, err := f.broker.Dial(f.hostID)
	if err != nil {
		return nil, fmt.Errorf("dial host service: %w", err)
	}
	f.host = proto.NewHostServiceClient(conn)
	return f.host, nil
}

// hostCall performs a HostService callback to the host and reports what came
// back, so tests can see exactly what the host is willing to hand a plugin.
func (f *fixture) hostCall(ctx context.Context, in map[string]any) (*proto.ExecuteProviderResponse, error) {
	host, err := f.hostClient()
	if err != nil {
		return okResponse(map[string]any{"error": err.Error()})
	}

	out := map[string]any{}
	var callErr error
	switch actionOf(in) {
	case plugintest.ActionAuthToken:
		var resp *proto.GetAuthTokenResponse
		resp, callErr = host.GetAuthToken(ctx, &proto.GetAuthTokenRequest{
			HandlerName: str(in, "handler"),
			Scope:       str(in, "scope"),
		})
		out["accessToken"] = resp.GetAccessToken()
		out["error"] = resp.GetError()
	case plugintest.ActionAuthHandlers:
		var resp *proto.ListAuthHandlersResponse
		resp, callErr = host.ListAuthHandlers(ctx, &proto.ListAuthHandlersRequest{})
		out["handlers"] = resp.GetHandlerNames()
		out["default"] = resp.GetDefaultHandler()
	case plugintest.ActionSecret:
		var resp *proto.GetSecretResponse
		resp, callErr = host.GetSecret(ctx, &proto.GetSecretRequest{Name: str(in, "name")})
		out["value"] = resp.GetValue()
		out["found"] = resp.GetFound()
		out["error"] = resp.GetError()
	case plugintest.ActionSecrets:
		var resp *proto.ListSecretsResponse
		resp, callErr = host.ListSecrets(ctx, &proto.ListSecretsRequest{Pattern: str(in, "pattern")})
		out["names"] = resp.GetNames()
		out["error"] = resp.GetError()
	}
	if callErr != nil {
		out["rpcError"] = callErr.Error()
	}
	return okResponse(out)
}

// okResponse wraps data in the output envelope the host decodes.
func okResponse(data any) (*proto.ExecuteProviderResponse, error) {
	out, err := json.Marshal(&sdkprovider.Output{Data: data})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "encode output: %v", err)
	}
	return &proto.ExecuteProviderResponse{Output: out}, nil
}

func malformedOutput(kind string) []byte {
	switch kind {
	case plugintest.MalformedArray:
		return []byte(`[1,2,3]`)
	case plugintest.MalformedEmpty:
		return nil
	case plugintest.MalformedTruncated:
		return []byte(`{"data":{"key":`)
	case plugintest.MalformedInvalid:
	}
	return []byte(`this is not json`)
}

// crash ends the whole process without any cleanup, as a plugin that dies
// mid-call would.
func crash(in map[string]any) {
	os.Exit(num(in, "exitCode", defaultCrashCode))
}

func actionOf(in map[string]any) string {
	return str(in, "action")
}

func str(in map[string]any, key string) string {
	s, _ := in[key].(string)
	return s
}

// num reads a JSON number; JSON decoding yields float64.
func num(in map[string]any, key string, def int) int {
	if v, ok := in[key].(float64); ok {
		return int(v)
	}
	return def
}

func strs(in map[string]any, key string) []string {
	raw, _ := in[key].([]any)
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
