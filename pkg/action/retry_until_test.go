// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package action_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/duration"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/debugprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/messageprovider"
	"github.com/oakwood-commons/scafctl/pkg/redact"
	"github.com/oakwood-commons/scafctl/pkg/schema"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/spec"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExecutor_RetryUntil_PollsUntilMatchAndResolvesActionScope(t *testing.T) {
	t.Parallel()

	var pollCalls atomic.Int32
	registry := newMockRegistry()
	registry.register(&mockProvider{
		name: "check-runner",
		execute: func(_ context.Context, input any) (*provider.Output, error) {
			inputs, _ := input.(map[string]any)
			if inputs["name"] == "seed" {
				return &provider.Output{Data: map[string]any{"targetSuite": "ci"}}, nil
			}

			n := int(pollCalls.Add(1))
			status := "in_progress"
			if n >= 3 {
				status = "completed"
			}
			return &provider.Output{
				Data: map[string]any{
					"suite": "ci",
					"poll":  n,
					"result": map[string]any{
						"check_runs": []any{
							map[string]any{"name": "build", "status": status},
							map[string]any{"name": "lint", "status": "completed"},
						},
					},
				},
			}, nil
		},
	})

	progress := newProgressRecorder()
	exec := action.NewExecutor(
		action.WithRegistry(registry),
		action.WithResolverData(map[string]any{"expectedStatus": "completed"}),
		action.WithProgressCallback(progress),
		action.WithDefaultTimeout(5*time.Second),
	)

	oneMilli := duration.New(time.Millisecond)
	untilExpr := celexp.Expression(`__result.suite == __actions.seed.results.targetSuite && __self.result.check_runs.all(c, c.status == _.expectedStatus)`)

	wf := &action.Workflow{
		Actions: map[string]*action.Action{
			"seed": {
				Provider: "check-runner",
				Inputs:   map[string]*spec.ValueRef{"name": makeLiteral("seed")},
			},
			"waitChecks": {
				Provider: "check-runner",
				Inputs:   map[string]*spec.ValueRef{"name": makeLiteral("poll")},
				Retry: &action.RetryConfig{
					MaxAttempts:  5,
					Backoff:      action.BackoffFixed,
					InitialDelay: &oneMilli,
					Until:        &untilExpr,
				},
			},
		},
	}

	res, err := exec.Execute(redact.WithSuppressGitHubActionsMasks(context.Background()), wf)
	require.NoError(t, err)
	assert.Equal(t, action.ExecutionSucceeded, res.FinalStatus)

	ar := res.Actions["waitChecks"]
	require.NotNil(t, ar)
	assert.Equal(t, action.StatusSucceeded, ar.Status)
	assert.Equal(t, 3, ar.Attempts)
	assert.Equal(t, int32(3), pollCalls.Load())

	resMap, ok := ar.Results.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, 3, resMap["poll"])

	events := progress.getEvents()
	assert.Contains(t, events, "retry:waitChecks:2/5")
	assert.Contains(t, events, "retry:waitChecks:3/5")
	for _, ev := range events {
		assert.False(t, strings.HasPrefix(ev, "failed:"), "intermediate pending polls must not emit failure events: %s", ev)
	}
}

func TestExecutor_RetryUntil_ExhaustionPreservesFinalCandidateResult(t *testing.T) {
	t.Parallel()

	var pollCalls atomic.Int32
	registry := newMockRegistry()
	registry.register(&mockProvider{
		name: "check-runner",
		execute: func(_ context.Context, _ any) (*provider.Output, error) {
			n := int(pollCalls.Add(1))
			return &provider.Output{
				Data: map[string]any{
					"poll": n,
					"result": map[string]any{
						"check_runs": []any{
							map[string]any{"name": "e2e", "status": "in_progress"},
						},
					},
				},
			}, nil
		},
	})

	exec := action.NewExecutor(
		action.WithRegistry(registry),
		action.WithDefaultTimeout(5*time.Second),
	)

	oneMilli := duration.New(time.Millisecond)
	untilExpr := celexp.Expression(`__result.result.check_runs.all(c, c.status == "completed")`)

	wf := &action.Workflow{
		Actions: map[string]*action.Action{
			"waitChecks": {
				Provider: "check-runner",
				Retry: &action.RetryConfig{
					MaxAttempts:  5,
					Backoff:      action.BackoffFixed,
					InitialDelay: &oneMilli,
					Until:        &untilExpr,
				},
			},
		},
	}

	// Verify embedder context with custom binary name does not affect domain behavior.
	ctx := settings.IntoContext(context.Background(), &settings.Run{BinaryName: "mycli"})
	res, err := exec.Execute(ctx, wf)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "condition 'until' not met after 5 attempt(s)")
	assert.Equal(t, action.ExecutionFailed, res.FinalStatus)

	ar := res.Actions["waitChecks"]
	require.NotNil(t, ar)
	assert.Equal(t, action.StatusFailed, ar.Status)
	assert.Equal(t, 5, ar.Attempts)
	assert.Contains(t, ar.Error, "condition 'until' not met after 5 attempt(s)")

	// Final candidate output must be preserved in the action output envelope.
	resMap, ok := ar.Results.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, 5, resMap["poll"])
}

func TestExecutor_RetryUntil_CallWithDedupReExecutesAcrossPolls(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	registry := newMockRegistry()
	registry.register(&mockProvider{
		name: "status-api",
		execute: func(_ context.Context, _ any) (*provider.Output, error) {
			n := int(calls.Add(1))
			state := "pending"
			if n >= 2 {
				state = "ready"
			}
			return &provider.Output{Data: map[string]any{"state": state, "call": n}}, nil
		},
	})

	exec := action.NewExecutor(
		action.WithRegistry(registry),
		action.WithCalls(map[string]*spec.Call{
			"getStatus": {
				Provider: "status-api",
				Dedup:    true,
			},
		}),
		action.WithDefaultTimeout(5*time.Second),
	)

	oneMilli := duration.New(time.Millisecond)
	untilExpr := celexp.Expression(`__result.state == "ready"`)

	wf := &action.Workflow{
		Actions: map[string]*action.Action{
			"pollViaCall": {
				CallRef: spec.CallRef{Call: "getStatus"},
				Retry: &action.RetryConfig{
					MaxAttempts:  3,
					InitialDelay: &oneMilli,
					Until:        &untilExpr,
				},
			},
		},
	}

	res, err := exec.Execute(context.Background(), wf)
	require.NoError(t, err)
	assert.Equal(t, action.ExecutionSucceeded, res.FinalStatus)
	assert.Equal(t, int32(2), calls.Load())
	assert.Equal(t, 2, res.Actions["pollViaCall"].Attempts)
}

func TestExecutor_RetryUntil_ForEachIterationBindings(t *testing.T) {
	t.Parallel()

	var itemACalls, itemBCalls atomic.Int32
	registry := newMockRegistry()
	registry.register(&mockProvider{
		name: "region-poller",
		execute: func(_ context.Context, input any) (*provider.Output, error) {
			inputs, _ := input.(map[string]any)
			region, _ := inputs["env"].(string)
			var n int
			if region == "us-east" {
				n = int(itemACalls.Add(1))
			} else {
				n = int(itemBCalls.Add(1))
			}
			ready := n >= 2
			return &provider.Output{
				Data: map[string]any{
					"region": region,
					"ready":  ready,
					"idx":    inputs["item"],
				},
			}, nil
		},
	})

	exec := action.NewExecutor(
		action.WithRegistry(registry),
		action.WithResolverData(map[string]any{
			"regions": []any{"us-east", "eu-west"},
		}),
		action.WithDefaultTimeout(5*time.Second),
	)

	oneMilli := duration.New(time.Millisecond)
	untilExpr := celexp.Expression(`__result.ready && __result.region == regionName && __self.region == __item && regionIdx == __index`)
	envExpr := celexp.Expression(`regionName`)
	idxExpr := celexp.Expression(`regionIdx`)
	regionsName := "regions"

	wf := &action.Workflow{
		Actions: map[string]*action.Action{
			"waitRegions": {
				Provider: "region-poller",
				ForEach: &spec.ForEachClause{
					In:    &spec.ValueRef{Resolver: &regionsName},
					Item:  "regionName",
					Index: "regionIdx",
				},
				Inputs: map[string]*spec.ValueRef{
					"env":  {Expr: &envExpr},
					"item": {Expr: &idxExpr},
				},
				Retry: &action.RetryConfig{
					MaxAttempts:  4,
					InitialDelay: &oneMilli,
					Until:        &untilExpr,
				},
			},
		},
	}

	res, err := exec.Execute(context.Background(), wf)
	require.NoError(t, err)
	assert.Equal(t, action.ExecutionSucceeded, res.FinalStatus)
	assert.Equal(t, int32(2), itemACalls.Load())
	assert.Equal(t, int32(2), itemBCalls.Load())
	assert.Equal(t, 2, res.Actions["waitRegions[0]"].Attempts)
	assert.Equal(t, 2, res.Actions["waitRegions[1]"].Attempts)
}

func TestExecutor_RetryUntil_ChunkOnlyStreamingConsistentAcrossOutputModes(t *testing.T) {
	t.Parallel()

	runMode := func(t *testing.T, withLiveStreams bool) {
		t.Helper()
		var calls atomic.Int32
		registry := newMockRegistry()
		registry.register(&mockProvider{
			name: "stream-poller",
			execute: func(ctx context.Context, _ any) (*provider.Output, error) {
				n := int(calls.Add(1))
				chunk := "pending\n"
				if n >= 3 {
					chunk = "completed\n"
				}

				rawStreams, hasIO := provider.IOStreamsFromContext(ctx)
				canStream := hasIO && rawStreams != nil && !provider.IsFallbackIOStreams(ctx)
				sink, _ := provider.CapturedStreamSinkFromContext(ctx)
				if canStream {
					if rawStreams.Out != nil {
						_, _ = rawStreams.Out.Write([]byte(chunk))
					}
					return &provider.Output{Streamed: true}, nil
				}
				if sink != nil {
					sink.Set(chunk, "")
					return &provider.Output{Streamed: false}, nil
				}
				return &provider.Output{Data: map[string]any{"stdout": chunk}}, nil
			},
		})

		var liveOut, liveErr bytes.Buffer
		opts := []action.ExecutorOption{
			action.WithRegistry(registry),
			action.WithDefaultTimeout(5 * time.Second),
		}
		if withLiveStreams {
			opts = append(opts, action.WithIOStreams(&provider.IOStreams{
				Out:    &liveOut,
				ErrOut: &liveErr,
			}))
		}

		exec := action.NewExecutor(opts...)
		oneMilli := duration.New(time.Millisecond)
		untilExpr := celexp.Expression(`__result.stdout == "completed\n"`)

		wf := &action.Workflow{
			Actions: map[string]*action.Action{
				"pollStream": {
					Provider: "stream-poller",
					Retry: &action.RetryConfig{
						MaxAttempts:  5,
						InitialDelay: &oneMilli,
						Until:        &untilExpr,
					},
				},
			},
		}

		res, err := exec.Execute(context.Background(), wf)
		require.NoError(t, err)
		assert.Equal(t, action.ExecutionSucceeded, res.FinalStatus)

		ar := res.Actions["pollStream"]
		require.NotNil(t, ar)
		assert.Equal(t, 3, ar.Attempts)
		resMap, ok := ar.Results.(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "completed\n", resMap["stdout"])

		if withLiveStreams {
			// Only the final matching attempt's output is flushed to live terminal streams.
			assert.Equal(t, "completed\n", liveOut.String())
			assert.True(t, ar.Streamed)
		} else {
			assert.False(t, ar.Streamed)
		}
	}

	t.Run("structured mode without IOStreams", func(t *testing.T) {
		t.Parallel()
		runMode(t, false)
	})

	t.Run("human terminal mode with live IOStreams", func(t *testing.T) {
		t.Parallel()
		runMode(t, true)
	})
}

func TestExplainRetryKind_DocumentsUntilAndErrorMapFields(t *testing.T) {
	t.Parallel()

	kindDef, err := schema.GetKind("retry")
	require.NoError(t, err)

	info, err := schema.IntrospectType(kindDef.TypeInstance)
	require.NoError(t, err)
	info.Description = kindDef.Description

	var out bytes.Buffer
	ioStreams := terminal.IOStreams{Out: &out, ErrOut: &out}
	w := writer.New(&ioStreams, &settings.Run{BinaryName: "mycli", NoColor: true})
	formatter := schema.NewFormatterWithWriter(w)
	formatter.FormatType(info)

	rendered := out.String()
	for _, token := range []string{
		"retryIf",
		"__error.message",
		"__error.type",
		"__error.statusCode",
		"__error.exitCode",
		"__error.attempt",
		"__error.maxAttempts",
		"__error.message.contains('rate limit')",
		"until",
		"__result",
		"__self",
	} {
		assert.Contains(t, rendered, token, "explain retry output missing %q", token)
	}
}

func TestExecutor_RetryUntil_MessageProviderPreservesFinalTerminalOutput(t *testing.T) {
	t.Parallel()

	t.Run("native message provider flushes in terminal mode and normalizes Streamed in structured mode", func(t *testing.T) {
		t.Parallel()

		runCase := func(t *testing.T, withLiveStreams bool) {
			t.Helper()

			registry := newMockRegistry()
			registry.register(messageprovider.NewMessageProvider())

			var liveOut, liveErr bytes.Buffer
			opts := []action.ExecutorOption{
				action.WithRegistry(registry),
				action.WithDefaultTimeout(5 * time.Second),
			}
			if withLiveStreams {
				opts = append(opts, action.WithIOStreams(&provider.IOStreams{
					Out:    &liveOut,
					ErrOut: &liveErr,
				}))
			}

			exec := action.NewExecutor(opts...)
			untilExpr := celexp.Expression(`true`)
			wf := &action.Workflow{
				Actions: map[string]*action.Action{
					"announce": {
						Provider: messageprovider.ProviderName,
						Inputs: map[string]*spec.ValueRef{
							"message": makeLiteral("ready-now"),
							"type":    makeLiteral("plain"),
						},
						Retry: &action.RetryConfig{
							MaxAttempts: 2,
							Until:       &untilExpr,
						},
					},
				},
			}

			ctx := settings.IntoContext(context.Background(), &settings.Run{NoColor: true})
			res, err := exec.Execute(ctx, wf)
			require.NoError(t, err)
			assert.Equal(t, action.ExecutionSucceeded, res.FinalStatus)

			ar := res.Actions["announce"]
			require.NotNil(t, ar)
			assert.Equal(t, 1, ar.Attempts)
			assert.Equal(t, map[string]any{
				"success": true,
				"message": "ready-now",
			}, ar.Results)

			if withLiveStreams {
				assert.Equal(t, "ready-now\n", liveOut.String())
				assert.Empty(t, liveErr.String())
				assert.True(t, ar.Streamed)
			} else {
				assert.False(t, ar.Streamed)
			}
		}

		runCase(t, true)
		runCase(t, false)
	})

	t.Run("multi-attempt message polling flushes only the final attempt", func(t *testing.T) {
		t.Parallel()

		msgProv := messageprovider.NewMessageProvider()
		var attempts atomic.Int32
		registry := newMockRegistry()
		registry.register(&mockProvider{
			name: "polled-message",
			execute: func(ctx context.Context, _ any) (*provider.Output, error) {
				n := int(attempts.Add(1))
				text := fmt.Sprintf("attempt-%d", n)
				return msgProv.Execute(ctx, map[string]any{
					"message": text,
					"type":    "plain",
				})
			},
		})

		var liveOut, liveErr bytes.Buffer
		exec := action.NewExecutor(
			action.WithRegistry(registry),
			action.WithIOStreams(&provider.IOStreams{Out: &liveOut, ErrOut: &liveErr}),
			action.WithDefaultTimeout(5*time.Second),
		)

		oneMilli := duration.New(time.Millisecond)
		untilExpr := celexp.Expression(`__result.message == "attempt-3"`)
		wf := &action.Workflow{
			Actions: map[string]*action.Action{
				"pollMsg": {
					Provider: "polled-message",
					Retry: &action.RetryConfig{
						MaxAttempts:  4,
						InitialDelay: &oneMilli,
						Until:        &untilExpr,
					},
				},
			},
		}

		ctx := settings.IntoContext(context.Background(), &settings.Run{NoColor: true})
		res, err := exec.Execute(ctx, wf)
		require.NoError(t, err)
		assert.Equal(t, 3, res.Actions["pollMsg"].Attempts)
		assert.Equal(t, "attempt-3\n", liveOut.String())
		assert.True(t, res.Actions["pollMsg"].Streamed)
	})
}

type debugPollingDelegate struct {
	inner             *debugprovider.DebugProvider
	attempts          *atomic.Int32
	mu                sync.Mutex
	capturedEachRound []bool
}

func (d *debugPollingDelegate) Descriptor() *provider.Descriptor {
	return d.inner.Descriptor()
}

func (d *debugPollingDelegate) Execute(ctx context.Context, input any) (*provider.Output, error) {
	n := int(d.attempts.Add(1))
	ioStreams, ok := provider.IOStreamsFromContext(ctx)
	buffered := ok && ioStreams != nil &&
		ioStreams.Out != nil && ioStreams.Out != os.Stdout &&
		ioStreams.ErrOut != nil && ioStreams.ErrOut != os.Stderr
	d.mu.Lock()
	d.capturedEachRound = append(d.capturedEachRound, buffered)
	d.mu.Unlock()

	ctx = provider.WithResolverContext(ctx, map[string]any{"step": n})
	return d.inner.Execute(ctx, input)
}

func TestExecutor_RetryUntil_DebugProviderSuppressesPendingAttemptsAndStdioLeak(t *testing.T) {
	t.Parallel()

	t.Run("terminal mode flushes only final debug attempt", func(t *testing.T) {
		t.Parallel()

		var attempts atomic.Int32
		delegate := &debugPollingDelegate{
			inner:    debugprovider.NewDebugProvider(),
			attempts: &attempts,
		}
		registry := newMockRegistry()
		registry.register(delegate)

		var liveOut, liveErr bytes.Buffer
		exec := action.NewExecutor(
			action.WithRegistry(registry),
			action.WithIOStreams(&provider.IOStreams{Out: &liveOut, ErrOut: &liveErr}),
			action.WithDefaultTimeout(5*time.Second),
		)

		oneMilli := duration.New(time.Millisecond)
		untilExpr := celexp.Expression(`__result.result == 3`)
		wf := &action.Workflow{
			Actions: map[string]*action.Action{
				"pollDebug": {
					Provider: debugprovider.ProviderName,
					Inputs: map[string]*spec.ValueRef{
						"expression":  makeLiteral("_.step"),
						"format":      makeLiteral("pretty"),
						"destination": makeLiteral("stdout"),
					},
					Retry: &action.RetryConfig{
						MaxAttempts:  4,
						InitialDelay: &oneMilli,
						Until:        &untilExpr,
					},
				},
			},
		}

		res, err := exec.Execute(context.Background(), wf)
		require.NoError(t, err)
		ar := res.Actions["pollDebug"]
		require.NotNil(t, ar)
		assert.Equal(t, 3, ar.Attempts)
		assert.Equal(t, "3\n", liveOut.String())
		assert.Empty(t, liveErr.String())
		assert.True(t, ar.Streamed)
		assert.Equal(t, []bool{true, true, true}, delegate.capturedEachRound)
	})

	t.Run("structured mode without live IOStreams buffers all debug attempts away from process stdio", func(t *testing.T) {
		t.Parallel()

		for _, dest := range []string{"stdout", "stderr"} {
			var attempts atomic.Int32
			delegate := &debugPollingDelegate{
				inner:    debugprovider.NewDebugProvider(),
				attempts: &attempts,
			}
			registry := newMockRegistry()
			registry.register(delegate)

			execInst := action.NewExecutor(
				action.WithRegistry(registry),
				action.WithDefaultTimeout(5*time.Second),
			)
			oneMilli := duration.New(time.Millisecond)
			untilExpr := celexp.Expression(`__result.result == 3`)
			wf := &action.Workflow{
				Actions: map[string]*action.Action{
					"dbg": {
						Provider: debugprovider.ProviderName,
						Inputs: map[string]*spec.ValueRef{
							"expression":  makeLiteral("_.step"),
							"format":      makeLiteral("pretty"),
							"destination": makeLiteral(dest),
						},
						Retry: &action.RetryConfig{
							MaxAttempts:  4,
							InitialDelay: &oneMilli,
							Until:        &untilExpr,
						},
					},
				},
			}

			res, err := execInst.Execute(context.Background(), wf)
			require.NoError(t, err)
			ar := res.Actions["dbg"]
			require.NotNil(t, ar)
			assert.Equal(t, 3, ar.Attempts)
			assert.False(t, ar.Streamed)
			assert.Equal(t, []bool{true, true, true}, delegate.capturedEachRound, "destination=%s", dest)
		}
	})
}

func TestExecutor_RetryUntil_InheritedChildExecutorBuffersPerAttempt(t *testing.T) {
	t.Parallel()

	runMode := func(t *testing.T, withLiveStreams bool) {
		t.Helper()

		var parentCalls atomic.Int32
		registry := newMockRegistry()
		registry.register(messageprovider.NewMessageProvider())
		registry.register(debugprovider.NewDebugProvider())
		registry.register(&mockProvider{
			name: "nested-solution",
			execute: func(ctx context.Context, _ any) (*provider.Output, error) {
				n := int(parentCalls.Add(1))

				var childStreams *provider.IOStreams
				if inherited, ok := action.InheritedExecutorConfigFromContext(ctx); ok && inherited.IOStreams != nil {
					childStreams = inherited.IOStreams
				} else if !provider.IsFallbackIOStreams(ctx) {
					if streams, ok := provider.IOStreamsFromContext(ctx); ok && streams != nil {
						childStreams = streams
					}
				}

				childOpts := []action.ExecutorOption{
					action.WithRegistry(registry),
					action.WithResolverData(map[string]any{"childAttempt": n}),
					action.WithDefaultTimeout(5 * time.Second),
				}
				if childStreams != nil {
					childOpts = append(childOpts, action.WithIOStreams(childStreams))
				}

				childExec := action.NewExecutor(childOpts...)
				childWf := &action.Workflow{
					Actions: map[string]*action.Action{
						"childMsg": {
							Provider: messageprovider.ProviderName,
							Inputs: map[string]*spec.ValueRef{
								"message": makeLiteral(fmt.Sprintf("child-attempt-%d", n)),
								"type":    makeLiteral("plain"),
							},
						},
						"childDbg": {
							Provider:  debugprovider.ProviderName,
							DependsOn: []string{"childMsg"},
							Inputs: map[string]*spec.ValueRef{
								"expression":  makeLiteral("_.childAttempt"),
								"format":      makeLiteral("pretty"),
								"destination": makeLiteral("stderr"),
							},
						},
					},
				}

				childRes, childErr := childExec.Execute(ctx, childWf)
				if childErr != nil {
					return nil, childErr
				}
				return &provider.Output{
					Data: map[string]any{
						"attempt": n,
						"status":  string(childRes.FinalStatus),
					},
				}, nil
			},
		})

		var liveOut, liveErr bytes.Buffer
		opts := []action.ExecutorOption{
			action.WithRegistry(registry),
			action.WithDefaultTimeout(5 * time.Second),
		}
		if withLiveStreams {
			opts = append(opts, action.WithIOStreams(&provider.IOStreams{
				Out:    &liveOut,
				ErrOut: &liveErr,
			}))
		}

		exec := action.NewExecutor(opts...)
		oneMilli := duration.New(time.Millisecond)
		untilExpr := celexp.Expression(`__result.attempt == 3 && __result.status == "succeeded"`)
		wf := &action.Workflow{
			Actions: map[string]*action.Action{
				"runNested": {
					Provider: "nested-solution",
					Retry: &action.RetryConfig{
						MaxAttempts:  4,
						InitialDelay: &oneMilli,
						Until:        &untilExpr,
					},
				},
			},
		}

		ctx := settings.IntoContext(context.Background(), &settings.Run{NoColor: true})
		res, err := exec.Execute(ctx, wf)
		require.NoError(t, err)
		assert.Equal(t, action.ExecutionSucceeded, res.FinalStatus)

		ar := res.Actions["runNested"]
		require.NotNil(t, ar)
		assert.Equal(t, 3, ar.Attempts)
		assert.Equal(t, map[string]any{
			"attempt": 3,
			"status":  "succeeded",
		}, ar.Results)

		if withLiveStreams {
			assert.Equal(t, "child-attempt-3\n", liveOut.String())
			assert.Equal(t, "3\n", liveErr.String())
			assert.True(t, ar.Streamed)
		} else {
			assert.False(t, ar.Streamed)
		}
	}

	t.Run("terminal mode with live IOStreams", func(t *testing.T) {
		t.Parallel()
		runMode(t, true)
	})

	t.Run("structured mode without IOStreams", func(t *testing.T) {
		t.Parallel()
		runMode(t, false)
	})
}

func TestExecutor_RetryUntil_RedactsSecretsAndPreservesHybridStreams(t *testing.T) {
	t.Parallel()

	runCase := func(t *testing.T, isQuiet bool) {
		t.Helper()

		var calls atomic.Int32
		registry := newMockRegistry()
		registry.register(&mockProvider{
			name: "hybrid-streamer",
			execute: func(ctx context.Context, _ any) (*provider.Output, error) {
				n := int(calls.Add(1))
				if tr := redact.FromContext(ctx); tr != nil {
					tr.Track("dyn-secret-xyz")
				}

				outProgress := fmt.Sprintf("step=%d tok=pre-secret-abc\n", n)
				errProgress := fmt.Sprintf("poll=%d tok=pre-secret-abc dyn=dyn-secret-xyz\n", n)
				rawStreams, hasIO := provider.IOStreamsFromContext(ctx)
				canStream := hasIO && rawStreams != nil && !provider.IsFallbackIOStreams(ctx)
				if canStream {
					if rawStreams.Out != nil {
						_, _ = rawStreams.Out.Write([]byte(outProgress))
					}
					if rawStreams.ErrOut != nil {
						_, _ = rawStreams.ErrOut.Write([]byte(errProgress))
					}
				} else if sink, ok := provider.CapturedStreamSinkFromContext(ctx); ok {
					sink.Set(outProgress, errProgress)
				}

				state := "pending"
				if n >= 2 {
					state = "ready"
				}
				return &provider.Output{
					Data: map[string]any{
						"state":  state,
						"stdout": fmt.Sprintf("summary=%s tok=pre-secret-abc\n", state),
						"stderr": fmt.Sprintf("diag=%s dyn=dyn-secret-xyz\n", state),
					},
					Streamed: false,
				}, nil
			},
		})

		var liveOut, liveErr bytes.Buffer
		exec := action.NewExecutor(
			action.WithRegistry(registry),
			action.WithIOStreams(&provider.IOStreams{Out: &liveOut, ErrOut: &liveErr}),
			action.WithDefaultTimeout(5*time.Second),
		)

		tracker := redact.NewTracker()
		tracker.Track("pre-secret-abc")
		ctx := redact.WithSuppressGitHubActionsMasks(redact.WithTracker(context.Background(), tracker))
		ctx = settings.IntoContext(ctx, &settings.Run{IsQuiet: isQuiet})

		oneMilli := duration.New(time.Millisecond)
		untilExpr := celexp.Expression(`__result.state == "ready" && !__result.stdout.contains("pre-secret-abc") && !__result.stderr.contains("dyn-secret-xyz")`)
		wf := &action.Workflow{
			Actions: map[string]*action.Action{
				"pollHybrid": {
					Provider: "hybrid-streamer",
					Retry: &action.RetryConfig{
						MaxAttempts:  3,
						InitialDelay: &oneMilli,
						Until:        &untilExpr,
					},
				},
			},
		}

		res, err := exec.Execute(ctx, wf)
		require.NoError(t, err)
		assert.Equal(t, action.ExecutionSucceeded, res.FinalStatus)

		ar := res.Actions["pollHybrid"]
		require.NotNil(t, ar)
		assert.Equal(t, 2, ar.Attempts)

		if isQuiet {
			assert.False(t, ar.Streamed)
			assert.Empty(t, liveOut.String())
			assert.Empty(t, liveErr.String())
		} else {
			assert.True(t, ar.Streamed)
			assert.Equal(t, "step=2 tok=[REDACTED]\nsummary=ready tok=[REDACTED]\n", liveOut.String())
			assert.Equal(t, "poll=2 tok=[REDACTED] dyn=[REDACTED]\ndiag=ready dyn=[REDACTED]\n", liveErr.String())
		}

		resMap, ok := ar.Results.(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "summary=ready tok=[REDACTED]\n", resMap["stdout"])
		assert.Equal(t, "diag=ready dyn=[REDACTED]\n", resMap["stderr"])
	}

	t.Run("same-channel streamed progress and explicit buffered output flush together", func(t *testing.T) {
		t.Parallel()
		runCase(t, false)
	})

	t.Run("quiet mode suppresses captured polling flush while preserving redacted results", func(t *testing.T) {
		t.Parallel()
		runCase(t, true)
	})
}

func TestExecutor_RetryUntil_OverlappingDynamicSecretsAcrossChunks(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	registry := newMockRegistry()
	registry.register(&mockProvider{
		name: "split-chunk-poller",
		execute: func(ctx context.Context, _ any) (*provider.Output, error) {
			n := int(calls.Add(1))
			if tr := redact.FromContext(ctx); tr != nil {
				tr.Track("example-secret")
				tr.Track("example-secret-long")
			}

			state := "pending"
			if n >= 2 {
				state = "ready"
			}

			rawStreams, hasIO := provider.IOStreamsFromContext(ctx)
			if hasIO && rawStreams != nil {
				if rawStreams.Out != nil {
					_, _ = rawStreams.Out.Write([]byte("state=" + state + " auth=example-secret-lo"))
					_, _ = rawStreams.Out.Write([]byte("ng!\n"))
				}
				if rawStreams.ErrOut != nil {
					_, _ = rawStreams.ErrOut.Write([]byte("err=example-secret-lo"))
					_, _ = rawStreams.ErrOut.Write([]byte("ng!\n"))
				}
			}
			return &provider.Output{Streamed: true}, nil
		},
	})

	var liveOut, liveErr bytes.Buffer
	exec := action.NewExecutor(
		action.WithRegistry(registry),
		action.WithIOStreams(&provider.IOStreams{Out: &liveOut, ErrOut: &liveErr}),
		action.WithDefaultTimeout(5*time.Second),
	)

	oneMilli := duration.New(time.Millisecond)
	untilExpr := celexp.Expression(`__result.stdout.contains("state=ready") && !__result.stdout.contains("long") && !__result.stderr.contains("long")`)
	wf := &action.Workflow{
		Actions: map[string]*action.Action{
			"pollSplit": {
				Provider: "split-chunk-poller",
				Retry: &action.RetryConfig{
					MaxAttempts:  3,
					InitialDelay: &oneMilli,
					Until:        &untilExpr,
				},
			},
		},
	}

	// Start with an empty context (no pre-populated tracker) so dynamic secret
	// registration occurs while raw stream writers are already active.
	res, err := exec.Execute(redact.WithSuppressGitHubActionsMasks(context.Background()), wf)
	require.NoError(t, err)
	assert.Equal(t, action.ExecutionSucceeded, res.FinalStatus)

	ar := res.Actions["pollSplit"]
	require.NotNil(t, ar)
	assert.Equal(t, 2, ar.Attempts)
	assert.True(t, ar.Streamed)
	assert.Equal(t, "state=ready auth=[REDACTED]!\n", liveOut.String())
	assert.Equal(t, "err=[REDACTED]!\n", liveErr.String())
	assert.Equal(t, map[string]any{
		"stdout": "state=ready auth=[REDACTED]!\n",
		"stderr": "err=[REDACTED]!\n",
	}, ar.Results)
}

func TestExecutor_RetryUntil_SplitSourceSecretAcrossStreamAndBufferedOutput(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	registry := newMockRegistry()
	registry.register(&mockProvider{
		name: "split-source-poller",
		execute: func(ctx context.Context, _ any) (*provider.Output, error) {
			n := int(calls.Add(1))
			state := "pending"
			if n >= 2 {
				state = "ready"
			}

			rawStreams, hasIO := provider.IOStreamsFromContext(ctx)
			if hasIO && rawStreams != nil {
				if rawStreams.Out != nil {
					_, _ = rawStreams.Out.Write([]byte("auth=example-secret-lo"))
				}
				if rawStreams.ErrOut != nil {
					_, _ = rawStreams.ErrOut.Write([]byte("err=example-secret-lo"))
				}
			}

			return &provider.Output{
				Data: map[string]any{
					"state":  state,
					"stdout": "ng\n",
					"stderr": "ng\n",
				},
				Streamed: false,
			}, nil
		},
	})

	var liveOut, liveErr bytes.Buffer
	exec := action.NewExecutor(
		action.WithRegistry(registry),
		action.WithIOStreams(&provider.IOStreams{Out: &liveOut, ErrOut: &liveErr}),
		action.WithDefaultTimeout(5*time.Second),
	)

	tracker := redact.NewTracker()
	tracker.Track("example-secret")
	tracker.Track("example-secret-long")
	ctx := redact.WithSuppressGitHubActionsMasks(redact.WithTracker(context.Background(), tracker))

	oneMilli := duration.New(time.Millisecond)
	untilExpr := celexp.Expression(`__result.state == "ready"`)
	wf := &action.Workflow{
		Actions: map[string]*action.Action{
			"pollSplitSource": {
				Provider: "split-source-poller",
				Retry: &action.RetryConfig{
					MaxAttempts:  3,
					InitialDelay: &oneMilli,
					Until:        &untilExpr,
				},
			},
		},
	}

	res, err := exec.Execute(ctx, wf)
	require.NoError(t, err)
	assert.Equal(t, action.ExecutionSucceeded, res.FinalStatus)

	ar := res.Actions["pollSplitSource"]
	require.NotNil(t, ar)
	assert.Equal(t, 2, ar.Attempts)
	assert.True(t, ar.Streamed)
	assert.Equal(t, "auth=[REDACTED]\n", liveOut.String())
	assert.Equal(t, "err=[REDACTED]\n", liveErr.String())
	assert.NotContains(t, liveOut.String(), "example-secret-long")
	assert.NotContains(t, liveErr.String(), "example-secret-long")
}
