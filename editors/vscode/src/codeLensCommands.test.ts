// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

import { test } from 'node:test';
import assert from 'node:assert';
import { buildLensArgv, quoteArg, toShellCommand } from './codeLensCommands';

test('buildLensArgv appends the CLI args and the -f file flag', () => {
  assert.deepEqual(buildLensArgv('scafctl', ['run', 'resolver', 'env'], '/tmp/solution.yaml'), [
    'scafctl',
    'run',
    'resolver',
    'env',
    '-f',
    '/tmp/solution.yaml',
  ]);
});

test('buildLensArgv preserves action preview flags', () => {
  assert.deepEqual(buildLensArgv('/opt/mycli', ['run', 'action', 'deploy', '--dry-run'], '/w/s.yaml'), [
    '/opt/mycli',
    'run',
    'action',
    'deploy',
    '--dry-run',
    '-f',
    '/w/s.yaml',
  ]);
});

test('quoteArg double-quotes arguments containing whitespace', () => {
  assert.equal(quoteArg('resolver'), 'resolver');
  assert.equal(quoteArg('/tmp/my dir/solution.yaml'), '"/tmp/my dir/solution.yaml"');
  assert.equal(quoteArg(''), '""');
});

test('quoteArg quotes and escapes shell metacharacters (no injection)', () => {
  // Metacharacters without whitespace must still be neutralized.
  assert.equal(quoteArg('a&&calc'), '"a&&calc"');
  assert.equal(quoteArg('s$(id).yaml'), '"s\\$(id).yaml"');
  assert.equal(quoteArg('a;b'), '"a;b"');
  // Plain Windows/Unix paths without metacharacters pass through.
  assert.equal(quoteArg('C:\\tmp\\s.yaml'), 'C:\\tmp\\s.yaml');
  assert.equal(quoteArg('/tmp/s.yaml'), '/tmp/s.yaml');
});

test('toShellCommand renders a quoted command line', () => {
  const argv = buildLensArgv('scafctl', ['run', 'resolver', 'env'], '/tmp/my dir/s.yaml');
  assert.equal(toShellCommand(argv), 'scafctl run resolver env -f "/tmp/my dir/s.yaml"');
});
