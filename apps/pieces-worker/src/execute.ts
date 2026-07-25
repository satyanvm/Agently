/**
 * The `execute_piece` Temporal activity (docs/pieces-runtime-contract.md §4).
 *
 * Runs ONE piece action's real `run(context)` with a minimal, self-contained
 * ActionContext. Piece-level failures are RETURNED as { ok: false } so Temporal
 * never retries business errors; only infra problems (unknown piece/action —
 * i.e. this worker is missing a package) throw and hit the retry policy.
 */
import * as fs from 'node:fs';
import * as os from 'node:os';
import * as path from 'node:path';
import { CredentialResolver } from './credentials';
import { actionOf, LoadedPiece, PieceAction, Registry } from './pieces';

export interface ExecutePieceInput {
  piece: string; // "@activepieces/piece-slack"
  pieceVersion?: string;
  action: string; // "send_channel_message"
  props: Record<string, unknown>;
  authEnvKey?: string | null;
  // DB-backed credential id (docs/credentials-contract.md §7). Resolved HERE
  // against Postgres; the secret never rides the Temporal payload.
  credentialId?: string | null;
}

export type ExecutePieceResult =
  | { ok: true; output: unknown }
  | { ok: false; error: string; errorType: string };

const RUN_TIMEOUT_MS = 150_000; // inside the workflow's 180s start_to_close

export function makeExecutePiece(registry: Registry, resolveCredential?: CredentialResolver | null) {
  return async function executePiece(input: ExecutePieceInput): Promise<ExecutePieceResult> {
    const slug = input.piece.replace(/^@activepieces\/piece-/, '');
    const found = actionOf(registry, slug, input.action);
    if (!found) {
      // Infra error: the reasoner planned an action this worker doesn't have
      // installed. Throwing (not returning) surfaces it as a retryable activity
      // failure — a redeploy with the package added genuinely fixes it.
      throw new Error(
        `piece action not installed on this worker: ${input.piece} / ${input.action} ` +
        `(${registry.pieces.size} pieces loaded)`,
      );
    }
    const { piece, action } = found;

    // Credentials resolve HERE — secrets never ride Temporal payloads
    // (contract §4). Resolution order (credentials-contract §7): the DB
    // credential id when set, else process.env[authEnvKey]. Missing both is a
    // business outcome, not infra. A DB *error* (unlike a missing row) throws
    // out of the resolver and hits the retry policy.
    let auth: unknown;
    if (input.credentialId || input.authEnvKey) {
      if (input.credentialId && resolveCredential) {
        const data = await resolveCredential(input.credentialId);
        if (data !== null) auth = normalizeDbAuth(data, piece);
      }
      if (auth === undefined && input.authEnvKey) {
        const raw = process.env[input.authEnvKey] ?? '';
        if (raw) auth = normalizeAuth(raw, piece);
      }
      if (auth === undefined) {
        const sources = [
          input.credentialId ? `credential ${input.credentialId}` : '',
          input.authEnvKey ? `env var ${input.authEnvKey}` : '',
        ].filter(Boolean).join(' and ');
        return {
          ok: false,
          error: `credential not resolvable on the pieces worker (checked ${sources})`,
          errorType: 'MissingCredential',
        };
      }
    }

    try {
      const output = await withTimeout(
        runAction(piece, action, input.props, auth),
        RUN_TIMEOUT_MS,
        `piece run exceeded ${RUN_TIMEOUT_MS / 1000}s`,
      );
      return { ok: true, output: jsonSafe(output) };
    } catch (err) {
      const e = err as Error & { errorType?: string };
      return {
        ok: false,
        error: e?.message ? String(e.message) : String(err),
        errorType: e?.errorType ?? 'PieceExecutionError',
      };
    }
  };
}

/**
 * Shape a DB credential row's `data` the way the piece's auth property expects
 * (docs/credentials-contract.md §7):
 *   secret_text → data.value string; basic_auth → { username, password };
 *   custom_auth → data object as-is; oauth2 → { access_token, ...data }.
 * The secret_text string keeps the dual String-object access pattern from
 * normalizeAuth (bare token AND .secret_text), and custom_auth keeps the
 * auth.x / auth.props.x dual access.
 */
export function normalizeDbAuth(data: Record<string, unknown>, piece: LoadedPiece): unknown {
  const authType = String(piece.auth?.type ?? '').toUpperCase();
  switch (authType) {
    case 'SECRET_TEXT': {
      const value = String(data.value ?? '');
      // eslint-disable-next-line no-new-wrappers
      const dual = new String(value) as String & { secret_text: string; type: string };
      dual.secret_text = value;
      dual.type = 'SECRET_TEXT';
      return dual;
    }
    case 'BASIC_AUTH':
      return { username: String(data.username ?? ''), password: String(data.password ?? '') };
    case 'CUSTOM_AUTH':
      return { ...data, props: (data.props as Record<string, unknown>) ?? data };
    case 'OAUTH2':
      return { access_token: data.access_token, ...data };
    default:
      return data;
  }
}

/**
 * Shape the env var value the way the piece's auth property expects.
 *
 * The env value is JSON when it parses, else a raw string. Published pieces are
 * compiled against different framework generations that read secret-text auth
 * BOTH ways (`context.auth` as the token string vs `context.auth.secret_text`),
 * so the string case returns a String object carrying the object fields — it
 * concatenates/interpolates as the bare token AND satisfies property access.
 */
export function normalizeAuth(raw: string, piece: LoadedPiece): unknown {
  let parsed: unknown = undefined;
  try {
    parsed = JSON.parse(raw);
  } catch {
    /* raw string credential */
  }
  const authType = String(piece.auth?.type ?? '').toUpperCase();

  if (parsed !== undefined && typeof parsed === 'object' && parsed !== null) {
    // Operator provided the full connection-value JSON — trust it, but keep the
    // custom-auth dual access pattern working (auth.x and auth.props.x).
    if (authType === 'CUSTOM_AUTH') {
      const obj = parsed as Record<string, unknown>;
      return { ...obj, props: (obj.props as Record<string, unknown>) ?? obj };
    }
    return parsed;
  }

  switch (authType) {
    case 'OAUTH2':
      return { access_token: raw, token_type: 'Bearer', data: {} };
    case 'BASIC_AUTH': {
      const i = raw.indexOf(':');
      return i > 0
        ? { username: raw.slice(0, i), password: raw.slice(i + 1) }
        : { username: raw, password: '' };
    }
    case 'SECRET_TEXT': {
      // eslint-disable-next-line no-new-wrappers
      const dual = new String(raw) as String & { secret_text: string; type: string };
      dual.secret_text = raw;
      dual.type = 'SECRET_TEXT';
      return dual;
    }
    default:
      return raw;
  }
}

/** Build the minimal ActionContext and invoke the action's real run(). */
async function runAction(
  piece: LoadedPiece,
  action: PieceAction,
  props: Record<string, unknown>,
  auth: unknown,
): Promise<unknown> {
  const unsupported = (feature: string) => {
    const err = new Error(
      `${feature} is not supported by the Agently pieces runtime (piece ${piece.slug}); ` +
      'this action needs Activepieces platform services',
    ) as Error & { errorType: string };
    err.errorType = 'UnsupportedPieceFeature';
    return err;
  };

  // In-memory, invocation-scoped store. Good enough for actions that stash
  // intermediates; polling-trigger persistence is out of scope this phase.
  const kv = new Map<string, unknown>();
  const store = {
    put: async <T,>(key: string, value: T) => (kv.set(key, value), value),
    get: async <T,>(key: string) => (kv.has(key) ? (kv.get(key) as T) : null),
    delete: async (key: string) => void kv.delete(key),
  };

  const filesDir = await fs.promises.mkdtemp(path.join(os.tmpdir(), 'agently-piece-'));
  const files = {
    write: async ({ fileName, data }: { fileName: string; data: Buffer }) => {
      const p = path.join(filesDir, path.basename(fileName));
      await fs.promises.writeFile(p, data);
      return `file://${p}`;
    },
  };

  const propsValue = withDefaults(props, action.props ?? {});

  const context = {
    executionType: 'BEGIN',
    auth,
    propsValue,
    store,
    files,
    project: { id: 'agently', externalId: async () => undefined },
    connections: { get: async () => null },
    tags: { add: async () => undefined },
    server: { apiUrl: 'http://unsupported.invalid/', publicUrl: 'http://unsupported.invalid/', token: '' },
    flows: {
      list: async () => { throw unsupported('flows.list'); },
      current: { id: 'agently-run', version: { id: 'v1' } },
    },
    step: { name: action.name },
    output: { update: async () => undefined },
    agent: { tools: async () => { throw unsupported('agent.tools'); } },
    run: {
      id: 'agently-run',
      stop: () => undefined,
      respond: () => undefined,
      pause: () => { throw unsupported('run.pause (waitpoints)'); },
      createWaitpoint: async () => { throw unsupported('createWaitpoint'); },
      waitForWaitpoint: () => { throw unsupported('waitForWaitpoint'); },
    },
    generateResumeUrl: () => { throw unsupported('generateResumeUrl'); },
    serverUrl: 'http://unsupported.invalid/',
  };

  // filesDir is left for the OS tmp reaper — piece runs are short-lived and
  // the file:// URLs in the output must outlive the run() call.
  return action.run(context);
}

/** Apply the action's declared prop defaults for keys the caller left unset. */
export function withDefaults(
  props: Record<string, unknown>,
  propDefs: Record<string, any>,
): Record<string, unknown> {
  const out: Record<string, unknown> = { ...props };
  for (const [key, def] of Object.entries(propDefs)) {
    if (out[key] === undefined && def && def.defaultValue !== undefined) {
      out[key] = def.defaultValue;
    }
  }
  return out;
}

/** Round-trip through JSON so Temporal's converter always accepts the output. */
export function jsonSafe(v: unknown): unknown {
  if (v === undefined) return null;
  try {
    return JSON.parse(
      JSON.stringify(v, (_k, val) => (typeof val === 'bigint' ? val.toString() : val)),
    );
  } catch {
    return String(v);
  }
}

function withTimeout<T>(p: Promise<T>, ms: number, msg: string): Promise<T> {
  let timer: NodeJS.Timeout;
  const timeout = new Promise<never>((_, reject) => {
    timer = setTimeout(() => {
      const err = new Error(msg) as Error & { errorType: string };
      err.errorType = 'PieceTimeout';
      reject(err);
    }, ms);
  });
  return Promise.race([p, timeout]).finally(() => clearTimeout(timer!)) as Promise<T>;
}
