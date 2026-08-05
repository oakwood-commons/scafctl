import { execFile } from 'node:child_process';

/** A language+glob document filter for the LSP client's documentSelector. */
export interface SolutionDocumentFilter {
  language: string;
  pattern: string;
}

/**
 * RecognizedFiles mirrors the JSON emitted by `<binary> lsp document-selectors`.
 * It lists the solution/action file names the CLI auto-discovers, partitioned by
 * editor language so the client emits correct per-language document selectors.
 */
export interface RecognizedFiles {
  binaryName: string;
  yamlNames: string[];
  jsonNames: string[];
}

/**
 * DefaultBinaryName is the CLI binary name assumed when the server cannot be
 * queried for its own recognized-file set.
 */
export const DefaultBinaryName = 'scafctl';

/** The scafctl dedicated language id, contributed opt-in in package.json. */
export const ScafctlLanguageId = 'scafctl';

/**
 * defaultRecognizedFiles returns a static fallback set used when the server
 * binary cannot be queried (e.g. an older binary without the
 * `lsp document-selectors` subcommand). It mirrors the CLI's default discovery
 * set for the given binary name. This is a resilience fallback, not a
 * compatibility contract -- the authoritative source is the running binary.
 */
export function defaultRecognizedFiles(binaryName?: string): RecognizedFiles {
  const name = (binaryName ?? '').trim() || DefaultBinaryName;
  return {
    binaryName: name,
    yamlNames: [
      'solution.yaml',
      'solution.yml',
      `${name}.yaml`,
      `${name}.yml`,
      'taskfile.yaml',
      'taskfile.yml',
      'actions.yaml',
      'actions.yml',
    ],
    jsonNames: ['solution.json', `${name}.json`],
  };
}

/**
 * fetchRecognizedFiles runs `<command> lsp document-selectors -o json` and
 * parses the result. It NEVER rejects: on spawn error, non-zero exit, timeout,
 * or malformed output it logs via onError (if provided) and returns
 * defaultRecognizedFiles(fallbackBinaryName), so the extension always activates
 * with a sane selector.
 */
export function fetchRecognizedFiles(
  command: string,
  opts: { timeoutMs?: number; fallbackBinaryName?: string; onError?: (message: string) => void } = {},
): Promise<RecognizedFiles> {
  const { timeoutMs = 5000, fallbackBinaryName, onError } = opts;
  return new Promise((resolve) => {
    execFile(command, ['lsp', 'document-selectors', '-o', 'json'], { timeout: timeoutMs }, (err, stdout) => {
      if (err) {
        onError?.(`scafctl: could not query recognized solution files ('${command} lsp document-selectors' failed: ${err.message}); using defaults.`);
        resolve(defaultRecognizedFiles(fallbackBinaryName));
        return;
      }
      const parsed = parseRecognizedFiles(stdout);
      if (!parsed) {
        onError?.('scafctl: could not parse recognized solution files from the server; using defaults.');
        resolve(defaultRecognizedFiles(fallbackBinaryName));
        return;
      }
      resolve(parsed);
    });
  });
}

/**
 * parseRecognizedFiles validates and normalizes the JSON payload from the CLI.
 * Returns null when the payload is missing required fields or malformed.
 */
export function parseRecognizedFiles(stdout: string): RecognizedFiles | null {
  let raw: unknown;
  try {
    raw = JSON.parse(stdout);
  } catch {
    return null;
  }
  if (typeof raw !== 'object' || raw === null) {
    return null;
  }
  const obj = raw as Record<string, unknown>;
  const yamlNames = asStringArray(obj.yamlNames);
  const jsonNames = asStringArray(obj.jsonNames);
  // A valid payload must recognize at least one file; otherwise treat it as
  // malformed so the caller falls back to sane defaults.
  if (yamlNames.length === 0 && jsonNames.length === 0) {
    return null;
  }
  return {
    binaryName: typeof obj.binaryName === 'string' ? obj.binaryName : DefaultBinaryName,
    yamlNames,
    jsonNames,
  };
}

function asStringArray(value: unknown): string[] {
  if (!Array.isArray(value)) {
    return [];
  }
  return value.filter((v): v is string => typeof v === 'string' && v.length > 0);
}

/**
 * buildDocumentSelector builds the LSP documentSelector from recognized files
 * plus user overrides.
 *
 * - YAML names are emitted as `{ language: 'yaml', pattern }`.
 * - JSON names are emitted as `{ language: 'json', pattern }` (a JSON document
 *   never matches a YAML-scoped selector).
 * - userPatterns are custom globs whose extension is unknown, so they are
 *   matched against yaml AND json (and scafctl when enabled). When
 *   replaceDefaults is true they replace the discovered defaults; otherwise
 *   they extend them.
 * - When languageEnabled, the dedicated `scafctl` language is additionally
 *   matched for every default pattern, so a user who sets a file's language to
 *   `scafctl` still gets LSP features.
 */
export function buildDocumentSelector(
  files: RecognizedFiles,
  userPatterns: string[],
  opts: { replaceDefaults?: boolean; languageEnabled?: boolean } = {},
): SolutionDocumentFilter[] {
  const { replaceDefaults = false, languageEnabled = false } = opts;
  const cleanUser = userPatterns.map((p) => p.trim()).filter((p) => p.length > 0);

  const filters: SolutionDocumentFilter[] = [];

  if (!replaceDefaults) {
    const yamlGlob = bracePattern(files.yamlNames);
    if (yamlGlob) {
      filters.push({ language: 'yaml', pattern: yamlGlob });
      if (languageEnabled) {
        filters.push({ language: ScafctlLanguageId, pattern: yamlGlob });
      }
    }
    const jsonGlob = bracePattern(files.jsonNames);
    if (jsonGlob) {
      filters.push({ language: 'json', pattern: jsonGlob });
      if (languageEnabled) {
        filters.push({ language: ScafctlLanguageId, pattern: jsonGlob });
      }
    }
  }

  for (const pattern of cleanUser) {
    filters.push({ language: 'yaml', pattern });
    filters.push({ language: 'json', pattern });
    if (languageEnabled) {
      filters.push({ language: ScafctlLanguageId, pattern });
    }
  }

  return filters;
}

/**
 * bracePattern builds a recursive glob from a list of file names. Returns an
 * empty string for an empty list, `**\/name` for a single name, and
 * `**\/{a,b,c}` for multiple.
 */
export function bracePattern(names: string[]): string {
  const clean = names.filter((n) => n.length > 0);
  if (clean.length === 0) {
    return '';
  }
  if (clean.length === 1) {
    return `**/${clean[0]}`;
  }
  return `**/{${clean.join(',')}}`;
}
