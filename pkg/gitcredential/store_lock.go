// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package gitcredential

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
	"github.com/oakwood-commons/scafctl/pkg/secrets"
)

var (
	errAccountLockBusy   = errors.New("git credential account lock busy")
	inProcessAccountLock sync.Map // map[string]chan struct{}
)

type storeSecretsDirProvider interface {
	SecretsDir() string
}

func acquireAccountMutationLock(ctx context.Context, store secrets.Store, accountKey string) (func(), error) {
	memKey, lockDir, lockFileName := resolveAccountLockScope(store, accountKey)
	unlockMem, err := lockAccountInProcess(ctx, memKey)
	if err != nil {
		return nil, err
	}
	if lockDir == "" || lockFileName == "" {
		return unlockMem, nil
	}
	unlockFile, fileErr := lockAccountOnDisk(ctx, lockDir, lockFileName)
	if fileErr != nil {
		unlockMem()
		return nil, fileErr
	}
	return func() {
		unlockFile()
		unlockMem()
	}, nil
}

func resolveAccountLockScope(store secrets.Store, accountKey string) (memScope, lockDir, lockFileName string) {
	if dp, ok := store.(storeSecretsDirProvider); ok {
		if dir := dp.SecretsDir(); dir != "" {
			cleaned := filepath.Clean(dir)
			if abs, err := filepath.Abs(cleaned); err == nil {
				cleaned = abs
			}
			fileName := fmt.Sprintf(".gitcred-lock-%016x", xxhash.Sum64String(accountKey))
			return "dir:" + cleaned + "|" + accountKey, cleaned, fileName
		}
	}
	if store != nil {
		rv := reflect.ValueOf(store)
		if rv.Kind() == reflect.Pointer && !rv.IsNil() {
			return fmt.Sprintf("ptr:%T:0x%x|%s", store, rv.Pointer(), accountKey), "", ""
		}
		return fmt.Sprintf("val:%T|%s", store, accountKey), "", ""
	}
	return "nil|" + accountKey, "", ""
}

func lockAccountInProcess(ctx context.Context, scopeKey string) (func(), error) {
	sem := make(chan struct{}, 1)
	actual, _ := inProcessAccountLock.LoadOrStore(scopeKey, sem)
	lockCh, ok := actual.(chan struct{})
	if !ok {
		lockCh = sem
	}
	select {
	case lockCh <- struct{}{}:
		return func() { <-lockCh }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func lockAccountOnDisk(ctx context.Context, lockDir, lockFileName string) (func(), error) {
	if err := os.MkdirAll(lockDir, 0o700); err != nil {
		return nil, fmt.Errorf("create git credential lock directory: %w", err)
	}
	root, err := os.OpenRoot(lockDir)
	if err != nil {
		return nil, fmt.Errorf("open git credential lock root: %w", err)
	}
	f, err := root.OpenFile(lockFileName, os.O_CREATE|os.O_RDWR, 0o600)
	_ = root.Close()
	if err != nil {
		return nil, fmt.Errorf("open git credential lock file: %w", err)
	}

	backoff := 4 * time.Millisecond
	const maxBackoff = 24 * time.Millisecond
	var waitTimer *time.Timer
	defer func() {
		if waitTimer != nil {
			waitTimer.Stop()
		}
	}()

	for {
		err := tryLockAccountFile(f)
		if err == nil {
			return func() {
				_ = unlockAccountFile(f)
				_ = f.Close()
			}, nil
		}
		if !errors.Is(err, errAccountLockBusy) {
			_ = f.Close()
			return nil, fmt.Errorf("acquire git credential file lock: %w", err)
		}
		if waitTimer == nil {
			waitTimer = time.NewTimer(backoff)
		} else {
			waitTimer.Reset(backoff)
		}
		select {
		case <-ctx.Done():
			_ = f.Close()
			return nil, ctx.Err()
		case <-waitTimer.C:
		}
		if backoff < maxBackoff {
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}
}
