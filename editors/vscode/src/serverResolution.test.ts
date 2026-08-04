import { test } from 'node:test';
import assert from 'node:assert';
import { checkBinary, DefaultServerCommand, resolveCommand } from './serverResolution';

test('resolveCommand uses the configured path when set', () => {
  assert.equal(resolveCommand('/usr/local/bin/scafctl'), '/usr/local/bin/scafctl');
  assert.equal(resolveCommand('  /custom/scafctl  '), '/custom/scafctl');
});

test('resolveCommand falls back to the default when empty or undefined', () => {
  assert.equal(resolveCommand(''), DefaultServerCommand);
  assert.equal(resolveCommand('   '), DefaultServerCommand);
  assert.equal(resolveCommand(undefined), DefaultServerCommand);
});

test('checkBinary reports a missing executable', async () => {
  const problem = await checkBinary('scafctl-definitely-not-real-xyz');
  assert.ok(problem, 'expected an error message');
  assert.ok(problem.includes('Could not find'), problem);
});

test('checkBinary succeeds for a scafctl binary that supports lsp', async () => {
  const bin = process.env.SCAFCTL_BIN;
  if (!bin) {
    // No binary provided (e.g. CI without a Go build); the missing-binary path
    // above already exercises checkBinary's error branch.
    return;
  }
  const problem = await checkBinary(bin);
  assert.equal(problem, null, problem ?? '');
});
