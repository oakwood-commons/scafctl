// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

import { test } from 'node:test';
import assert from 'node:assert';
import { buildLensArgs, shouldSaveBeforeRun } from './codeLensCommands';

test('buildLensArgs appends the CLI args and the -f file flag', () => {
  assert.deepEqual(buildLensArgs(['run', 'resolver', 'env'], '/tmp/solution.yaml'), [
    'run',
    'resolver',
    'env',
    '-f',
    '/tmp/solution.yaml',
  ]);
});

test('buildLensArgs preserves action preview flags and does not include the binary', () => {
  assert.deepEqual(buildLensArgs(['run', 'action', 'deploy', '--dry-run'], '/w/s.yaml'), [
    'run',
    'action',
    'deploy',
    '--dry-run',
    '-f',
    '/w/s.yaml',
  ]);
});

test('buildLensArgs keeps a path with spaces intact (argv, no shell quoting)', () => {
  // The arg vector is passed to the process directly, so spaces/metacharacters
  // are literal -- no quoting or escaping is needed or applied.
  assert.deepEqual(buildLensArgs(['run', 'resolver', 'env'], '/tmp/my dir/s.yaml'), [
    'run',
    'resolver',
    'env',
    '-f',
    '/tmp/my dir/s.yaml',
  ]);
});

test('shouldSaveBeforeRun only saves dirty on-disk documents', () => {
  assert.equal(shouldSaveBeforeRun('file', true), true);
  assert.equal(shouldSaveBeforeRun('file', false), false); // clean: nothing to save
  assert.equal(shouldSaveBeforeRun('untitled', true), false); // no path to run against
  assert.equal(shouldSaveBeforeRun('vscode-vfs', true), false); // virtual document
});
