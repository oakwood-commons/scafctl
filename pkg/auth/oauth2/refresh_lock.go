// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package oauth2

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"time"

	"github.com/cespare/xxhash/v2"
)

var (
	errLockBusy       = errors.New("refresh lock busy")
	inProcessRefLocks sync.Map // map[string]chan struct{}
)

type secretsDirProvider interface {
	SecretsDir() string
}

func acquireInProcessKeyLock(ctx context.Context, key string) (func(), error) {
	ch := make(chan struct{}, 1)
	actual, _ := inProcessRefLocks.LoadOrStore(key, ch)
	sem, ok := actual.(chan struct{})
	if !ok {
		sem = ch
	}
	select {
	case sem <- struct{}{}:
		return func() { <-sem }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (h *Handler) persistedLockKey(kind, profile string) string {
	suffix := "refresh"
	if kind == "commit" {
		suffix = "session"
	}
	return h.secretKeyWithProfile(profile, suffix)
}

func (h *Handler) scopedLockIdentity(kind, profile string) (scopeKey, lockFilePath string) {
	lockKey := h.persistedLockKey(kind, profile)
	if dp, ok := h.secretStore.(secretsDirProvider); ok {
		if dir := dp.SecretsDir(); dir != "" {
			cleanDir := filepath.Clean(dir)
			lockName := fmt.Sprintf(".oauth2-%s-%016x", kind, xxhash.Sum64String(lockKey))
			return "dir:" + cleanDir + "|" + lockKey, filepath.Join(cleanDir, lockName)
		}
	}
	if h.secretStore != nil {
		rv := reflect.ValueOf(h.secretStore)
		if rv.Kind() == reflect.Pointer && !rv.IsNil() {
			return fmt.Sprintf("ptr:%T:%x|%s", h.secretStore, rv.Pointer(), lockKey), ""
		}
	}
	return fmt.Sprintf("inst:%p|%s", h, lockKey), ""
}

func (h *Handler) acquireScopedLock(ctx context.Context, kind, profile string) (func(), error) {
	scopeKey, lockPath := h.scopedLockIdentity(kind, profile)
	unlockMem, err := acquireInProcessKeyLock(ctx, scopeKey)
	if err != nil {
		return nil, err
	}
	if lockPath == "" {
		return unlockMem, nil
	}
	unlockFileFn, fileErr := acquireFileRefreshLock(ctx, lockPath)
	if fileErr != nil {
		unlockMem()
		return nil, fileErr
	}
	return func() {
		unlockFileFn()
		unlockMem()
	}, nil
}

// acquireRefreshLock serializes single-use refresh-token reload, exchange, and
// rotation persistence across goroutines, Handler instances sharing a store,
// and separate OS processes sharing a filesystem secrets directory.
func (h *Handler) acquireRefreshLock(ctx context.Context, profile string) (func(), error) {
	return h.acquireScopedLock(ctx, "refresh", profile)
}

// acquireSessionCommitLock serializes fast local session/credential commits
// across Login, patLogin, Logout, and post-HTTP refresh/mint persistence without
// ever being held across network I/O.
func (h *Handler) acquireSessionCommitLock(ctx context.Context, profile string) (func(), error) {
	return h.acquireScopedLock(ctx, "commit", profile)
}

func acquireFileRefreshLock(ctx context.Context, lockPath string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
		return nil, fmt.Errorf("prepare refresh lock directory: %w", err)
	}
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600) //nolint:gosec // G304: lock path is constructed from configured secrets directory and hashed key
	if err != nil {
		return nil, fmt.Errorf("open refresh lock file: %w", err)
	}

	wait := 5 * time.Millisecond
	const maxWait = 25 * time.Millisecond
	var timer *time.Timer
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()

	for {
		lockErr := tryLockFile(f)
		if lockErr == nil {
			return func() {
				_ = unlockFile(f)
				_ = f.Close()
			}, nil
		}
		if !errors.Is(lockErr, errLockBusy) {
			_ = f.Close()
			return nil, fmt.Errorf("lock refresh file: %w", lockErr)
		}
		if timer == nil {
			timer = time.NewTimer(wait)
		} else {
			timer.Reset(wait)
		}
		select {
		case <-ctx.Done():
			_ = f.Close()
			return nil, ctx.Err()
		case <-timer.C:
		}
		if wait < maxWait {
			wait *= 2
			if wait > maxWait {
				wait = maxWait
			}
		}
	}
}
