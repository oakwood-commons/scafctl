import { test } from 'node:test';
import assert from 'node:assert';
import { chmodSync, mkdtempSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import {
  bracePattern,
  buildDocumentSelector,
  defaultRecognizedFiles,
  DefaultBinaryName,
  fetchRecognizedFiles,
  parseRecognizedFiles,
  RecognizedFiles,
  ScafctlLanguageId,
} from './documentSelectors';

const sampleFiles: RecognizedFiles = {
  binaryName: 'scafctl',
  yamlNames: ['solution.yaml', 'solution.yml', 'taskfile.yaml', 'actions.yaml'],
  jsonNames: ['solution.json'],
};

test('defaultRecognizedFiles uses the default binary name when unset', () => {
  const files = defaultRecognizedFiles();
  assert.equal(files.binaryName, DefaultBinaryName);
  assert.ok(files.yamlNames.includes('solution.yaml'));
  assert.ok(files.yamlNames.includes('taskfile.yaml'));
  assert.ok(files.yamlNames.includes('actions.yaml'));
  assert.ok(files.jsonNames.includes('solution.json'));
});

test('defaultRecognizedFiles honors an embedder binary name', () => {
  const files = defaultRecognizedFiles('mycli');
  assert.equal(files.binaryName, 'mycli');
  assert.ok(files.yamlNames.includes('mycli.yaml'));
  assert.ok(files.yamlNames.includes('mycli.yml'));
  assert.ok(files.jsonNames.includes('mycli.json'));
});

test('bracePattern builds recursive globs', () => {
  assert.equal(bracePattern([]), '');
  assert.equal(bracePattern(['solution.yaml']), '**/solution.yaml');
  assert.equal(bracePattern(['a.yaml', 'b.yml']), '**/{a.yaml,b.yml}');
});

test('buildDocumentSelector emits yaml and json filters by default', () => {
  const filters = buildDocumentSelector(sampleFiles, []);
  const yaml = filters.find((f) => f.language === 'yaml');
  const jsonFilter = filters.find((f) => f.language === 'json');
  assert.ok(yaml, 'expected a yaml filter');
  assert.ok(jsonFilter, 'expected a json filter');
  assert.equal(yaml?.pattern, '**/{solution.yaml,solution.yml,taskfile.yaml,actions.yaml}');
  assert.equal(jsonFilter?.pattern, '**/solution.json');
  // No scafctl language unless explicitly enabled.
  assert.ok(!filters.some((f) => f.language === ScafctlLanguageId));
});

test('buildDocumentSelector extends defaults with user patterns', () => {
  const filters = buildDocumentSelector(sampleFiles, ['**/custom.yaml']);
  // Defaults still present.
  assert.ok(filters.some((f) => f.language === 'yaml' && f.pattern?.toString().includes('solution.yaml')));
  // User pattern added for both yaml and json.
  assert.ok(filters.some((f) => f.language === 'yaml' && f.pattern === '**/custom.yaml'));
  assert.ok(filters.some((f) => f.language === 'json' && f.pattern === '**/custom.yaml'));
});

test('buildDocumentSelector replaceDefaults drops the discovered set', () => {
  const filters = buildDocumentSelector(sampleFiles, ['**/custom.yaml'], { replaceDefaults: true });
  assert.ok(!filters.some((f) => f.pattern?.toString().includes('solution.yaml')));
  assert.ok(filters.some((f) => f.pattern === '**/custom.yaml'));
});

test('buildDocumentSelector adds the scafctl language when enabled', () => {
  const filters = buildDocumentSelector(sampleFiles, ['**/custom.yaml'], { languageEnabled: true });
  const scafctlFilters = filters.filter((f) => f.language === ScafctlLanguageId);
  // scafctl language applied to yaml defaults, json defaults, and the user pattern.
  assert.ok(scafctlFilters.length >= 3);
  assert.ok(scafctlFilters.some((f) => f.pattern === '**/custom.yaml'));
});

test('buildDocumentSelector ignores blank user patterns', () => {
  const filters = buildDocumentSelector(sampleFiles, ['  ', '']);
  assert.ok(!filters.some((f) => f.pattern === ''));
});

test('parseRecognizedFiles parses a valid payload', () => {
  const parsed = parseRecognizedFiles(JSON.stringify(sampleFiles));
  assert.deepEqual(parsed, sampleFiles);
});

test('parseRecognizedFiles rejects malformed or empty payloads', () => {
  assert.equal(parseRecognizedFiles('not json'), null);
  assert.equal(parseRecognizedFiles('null'), null);
  assert.equal(parseRecognizedFiles('{}'), null);
  assert.equal(parseRecognizedFiles(JSON.stringify({ yamlNames: [], jsonNames: [] })), null);
});

test('parseRecognizedFiles filters non-string entries', () => {
  const parsed = parseRecognizedFiles(
    JSON.stringify({ binaryName: 'scafctl', yamlNames: ['solution.yaml', 42, ''], jsonNames: 'x' }),
  );
  assert.deepEqual(parsed?.yamlNames, ['solution.yaml']);
  assert.deepEqual(parsed?.jsonNames, []);
});

test('fetchRecognizedFiles falls back on a missing binary', async () => {
  let errored = '';
  const files = await fetchRecognizedFiles('scafctl-definitely-not-real-xyz', {
    fallbackBinaryName: 'scafctl',
    onError: (m) => (errored = m),
  });
  assert.ok(errored.length > 0, 'expected an error message');
  assert.deepEqual(files, defaultRecognizedFiles('scafctl'));
});

test('fetchRecognizedFiles parses output from a stub binary', async () => {
  if (process.platform === 'win32') {
    return; // POSIX shebang script; skip on Windows
  }
  const dir = mkdtempSync(join(tmpdir(), 'scafctl-selectors-'));
  const file = join(dir, 'scafctl');
  const payload = JSON.stringify({
    binaryName: 'mycli',
    yamlNames: ['solution.yaml'],
    jsonNames: ['solution.json'],
  });
  writeFileSync(file, `#!/bin/sh\ncat <<'EOF'\n${payload}\nEOF\n`, { mode: 0o755 });
  chmodSync(file, 0o755);

  const files = await fetchRecognizedFiles(file);
  assert.equal(files.binaryName, 'mycli');
  assert.deepEqual(files.yamlNames, ['solution.yaml']);
  assert.deepEqual(files.jsonNames, ['solution.json']);
});

test('fetchRecognizedFiles falls back when the stub emits malformed JSON', async () => {
  if (process.platform === 'win32') {
    return;
  }
  const dir = mkdtempSync(join(tmpdir(), 'scafctl-selectors-bad-'));
  const file = join(dir, 'scafctl');
  writeFileSync(file, '#!/bin/sh\necho "not json"\n', { mode: 0o755 });
  chmodSync(file, 0o755);

  let errored = '';
  const files = await fetchRecognizedFiles(file, { fallbackBinaryName: 'scafctl', onError: (m) => (errored = m) });
  assert.ok(errored.length > 0);
  assert.deepEqual(files, defaultRecognizedFiles('scafctl'));
});
