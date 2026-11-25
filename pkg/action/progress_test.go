// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package action

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// unsyncRecorder intentionally uses plain slices without locking so go test -race
// catches any missing mutex serialization in SynchronizeProgressCallback or
// PrefixedProgressCallback.
type unsyncRecorder struct {
	events []string
}

func (r *unsyncRecorder) OnActionStart(name, desc string) {
	r.events = append(r.events, fmt.Sprintf("start:%s:%s", name, desc))
}

func (r *unsyncRecorder) OnActionComplete(name string, res any) {
	r.events = append(r.events, fmt.Sprintf("complete:%s:%v", name, res))
}

func (r *unsyncRecorder) OnActionFailed(name string, err error) {
	r.events = append(r.events, fmt.Sprintf("failed:%s:%v", name, err))
}

func (r *unsyncRecorder) OnActionSkipped(name, reason string) {
	r.events = append(r.events, fmt.Sprintf("skipped:%s:%s", name, reason))
}

func (r *unsyncRecorder) OnActionTimeout(name string, timeout time.Duration) {
	r.events = append(r.events, fmt.Sprintf("timeout:%s:%s", name, timeout))
}

func (r *unsyncRecorder) OnActionCancelled(name string) {
	r.events = append(r.events, fmt.Sprintf("cancelled:%s", name))
}

func (r *unsyncRecorder) OnRetryAttempt(name string, attempt, maxAttempts int, err error) {
	r.events = append(r.events, fmt.Sprintf("retry:%s:%d/%d:%v", name, attempt, maxAttempts, err))
}

func (r *unsyncRecorder) OnForEachProgress(name string, completed, total int) {
	r.events = append(r.events, fmt.Sprintf("foreach:%s:%d/%d", name, completed, total))
}

func (r *unsyncRecorder) OnPhaseStart(phase int, names []string) {
	r.events = append(r.events, fmt.Sprintf("phase-start:%d:%v", phase, names))
}

func (r *unsyncRecorder) OnPhaseComplete(phase int) {
	r.events = append(r.events, fmt.Sprintf("phase-complete:%d", phase))
}

func (r *unsyncRecorder) OnFinallyStart() {
	r.events = append(r.events, "finally-start")
}

func (r *unsyncRecorder) OnFinallyComplete() {
	r.events = append(r.events, "finally-complete")
}

func TestInheritedExecutorConfig_RoundTrip(t *testing.T) {
	ctx := context.Background()
	_, ok := InheritedExecutorConfigFromContext(ctx)
	assert.False(t, ok)

	var out bytes.Buffer
	streams := &provider.IOStreams{Out: &out}
	cb := NoOpProgressCallback{}
	want := InheritedExecutorConfig{
		DefaultTimeout:   12 * time.Second,
		GracePeriod:      3 * time.Second,
		MaxConcurrency:   4,
		ProgressCallback: cb,
		IOStreams:        streams,
		Cwd:              "/workspace/project",
	}

	ctx = WithInheritedExecutorConfig(ctx, want)
	got, ok := InheritedExecutorConfigFromContext(ctx)
	require.True(t, ok)
	assert.Equal(t, want.DefaultTimeout, got.DefaultTimeout)
	assert.Equal(t, want.GracePeriod, got.GracePeriod)
	assert.Equal(t, want.MaxConcurrency, got.MaxConcurrency)
	assert.Equal(t, want.IOStreams, got.IOStreams)
	assert.Equal(t, want.Cwd, got.Cwd)
	assert.NotNil(t, got.ProgressCallback)
}

func TestSynchronizeProgressCallback(t *testing.T) {
	t.Run("nil returns nil", func(t *testing.T) {
		assert.Nil(t, SynchronizeProgressCallback(nil))
	})

	t.Run("idempotent on already synchronized callbacks", func(t *testing.T) {
		rec := &unsyncRecorder{}
		syncCb := SynchronizeProgressCallback(rec)
		require.NotNil(t, syncCb)
		assert.Same(t, syncCb, SynchronizeProgressCallback(syncCb))

		prefixed := NewSolutionProgressCallback(syncCb, "child")
		assert.Same(t, prefixed, SynchronizeProgressCallback(prefixed))
	})

	t.Run("serializes concurrent calls across goroutines", func(t *testing.T) {
		rec := &unsyncRecorder{}
		syncCb := SynchronizeProgressCallback(rec)

		var wg sync.WaitGroup
		for i := range 16 {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				name := fmt.Sprintf("act-%d", idx)
				syncCb.OnPhaseStart(0, []string{name})
				syncCb.OnActionStart(name, "desc")
				syncCb.OnRetryAttempt(name, 1, 2, errors.New("boom"))
				syncCb.OnForEachProgress(name, 1, 2)
				syncCb.OnActionComplete(name, "ok")
				syncCb.OnActionSkipped(name, "cond")
				syncCb.OnActionTimeout(name, time.Second)
				syncCb.OnActionCancelled(name)
				syncCb.OnActionFailed(name, errors.New("fail"))
				syncCb.OnPhaseComplete(0)
				syncCb.OnFinallyStart()
				syncCb.OnFinallyComplete()
			}(i)
		}
		wg.Wait()

		assert.Len(t, rec.events, 16*12)
	})
}

func TestNewSolutionProgressCallback(t *testing.T) {
	t.Run("nil parent returns nil", func(t *testing.T) {
		assert.Nil(t, NewSolutionProgressCallback(nil, "child"))
	})

	t.Run("prefixes action events and suppresses child phase boundaries", func(t *testing.T) {
		rec := &unsyncRecorder{}
		cb := NewSolutionProgressCallback(rec, "deploy@1.0.0")

		cb.OnPhaseStart(0, []string{"step1"})
		cb.OnActionStart("step1", "Run step")
		cb.OnRetryAttempt("step1", 1, 3, errors.New("transient"))
		cb.OnForEachProgress("step1", 2, 4)
		cb.OnActionComplete("step1", "done")
		cb.OnActionSkipped("step2", "condition")
		cb.OnActionTimeout("step3", 5*time.Second)
		cb.OnActionCancelled("step4")
		cb.OnActionFailed("step5", errors.New("bad"))
		cb.OnPhaseComplete(0)
		cb.OnFinallyStart()
		cb.OnFinallyComplete()

		expected := []string{
			"start:[solution:deploy@1.0.0] step1:Run step",
			"retry:[solution:deploy@1.0.0] step1:1/3:transient",
			"foreach:[solution:deploy@1.0.0] step1:2/4",
			"complete:[solution:deploy@1.0.0] step1:done",
			"skipped:[solution:deploy@1.0.0] step2:condition",
			"timeout:[solution:deploy@1.0.0] step3:5s",
			"cancelled:[solution:deploy@1.0.0] step4",
			"failed:[solution:deploy@1.0.0] step5:bad",
		}
		assert.Equal(t, expected, rec.events)
	})

	t.Run("nested prefixes compose and share root synchronization", func(t *testing.T) {
		rec := &unsyncRecorder{}
		root := SynchronizeProgressCallback(rec)
		child := NewSolutionProgressCallback(root, "outer")
		grandchild := NewSolutionProgressCallback(child, "inner")

		var wg sync.WaitGroup
		for i := range 8 {
			wg.Add(2)
			go func(idx int) {
				defer wg.Done()
				child.OnActionStart(fmt.Sprintf("c-%d", idx), "")
			}(i)
			go func(idx int) {
				defer wg.Done()
				grandchild.OnActionComplete(fmt.Sprintf("g-%d", idx), nil)
			}(i)
		}
		wg.Wait()

		assert.Len(t, rec.events, 16)
		assert.Contains(t, rec.events, "start:[solution:outer] c-0:")
		assert.Contains(t, rec.events, "complete:[solution:outer] [solution:inner] g-0:<nil>")
	})
}

func BenchmarkNewSolutionProgressCallback(b *testing.B) {
	cb := NewSolutionProgressCallback(NoOpProgressCallback{}, "deploy-k8s@2.0.0")
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		cb.OnActionStart("provision", "Provision cluster")
		cb.OnActionComplete("provision", nil)
	}
}
