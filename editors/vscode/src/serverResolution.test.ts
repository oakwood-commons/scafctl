import { test } from 'node:test';
import assert from 'node:assert';
import { mkdtempSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { checkBinary, DefaultServerCommand, resolveCommand } from './serverResolution';
import { binaryNameFromCommand } from './serverResolution';

test('resolveCommand uses the configured path when set', () => {
  assert.equal(resolveCommand('/usr/local/bin/scafctl'), '/usr/local/bin/scafctl');
  assert.equal(resolveCommand('  /custom/scafctl  '), '/custom/scafctl');
});

test('resolveCommand falls back to the default when empty or undefined', () => {
  assert.equal(resolveCommand(''), DefaultServerCommand);
  assert.equal(resolveCommand('   '), DefaultServerCommand);
  assert.equal(resolveCommand(undefined), DefaultServerCommand);
});

test('binaryNameFromCommand extracts the binary name from a path', () => {
  assert.equal(binaryNameFromCommand('scafctl'), 'scafctl');
  assert.equal(binaryNameFromCommand('/opt/tools/mycli'), 'mycli');
  assert.equal(binaryNameFromCommand('C:\\tools\\mycli.exe'.replace(/\\/g, '/')), 'mycli');
  assert.equal(binaryNameFromCommand('  /opt/mycli  '), 'mycli');
});

test('binaryNameFromCommand falls back to the default for empty input', () => {
  assert.equal(binaryNameFromCommand(''), DefaultServerCommand);
  assert.equal(binaryNameFromCommand('   '), DefaultServerCommand);
});

test('checkBinary reports a missing executable', async () => {
  const problem = await checkBinary('scafctl-definitely-not-real-xyz');
  assert.ok(problem, 'expected an error message');
  assert.ok(problem.includes('Could not find'), problem);
});

test('checkBinary reports a non-executable file (permission denied)', async () => {
  if (process.platform === 'win32') {
    return; // POSIX execute permissions do not apply on Windows
  }
  const dir = mkdtempSync(join(tmpdir(), 'scafctl-perm-'));
  const file = join(dir, 'scafctl');
  writeFileSync(file, 'not executable\n', { mode: 0o644 });
  const problem = await checkBinary(file);
  assert.ok(problem, 'expected an error message');
  assert.ok(problem.includes('permission denied'), problem);
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
