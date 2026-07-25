/**
 * Piece-trigger runtime: runs a trigger's real run()/onEnable()/onDisable()
 * with a persistent store (trigger-store.ts). Invoked over the worker's HTTP
 * surface (options-server.ts) BEFORE a run exists — webhook payloads and
 * polling ticks are transformed into events here, and the Go API then launches
 * runs whose trigger node simply carries the event through. This keeps events
 * out of Temporal workflow code entirely.
 *
 * Business failures are returned ({ok:false}), never thrown, mirroring
 * execute_piece.
 */
import * as fs from 'node:fs';
import * as os from 'node:os';
import * as path from 'node:path';
import { CredentialResolver } from './credentials';
import { jsonSafe, normalizeAuth, normalizeDbAuth, withDefaults } from './execute';
import { LoadedPiece, Registry, triggerOf } from './pieces';
import { TriggerStoreFactory } from './trigger-store';

export interface TriggerRequestBase {
  piece: string; // "@activepieces/piece-github" (or bare slug)
  pieceVersion?: string;
  trigger: string; // trigger name as declared in createTrigger
  props?: Record<string, unknown>;
  credentialId?: string | null;
  authEnvKey?: string | null;
  workflowId: string; // store scope
  nodeKey: string; // store scope
  webhookUrl?: string;
}

export interface RunTriggerRequest extends TriggerRequestBase {
  // Raw webhook delivery; null/absent for a polling tick.
  payload?: { body?: unknown; headers?: Record<string, string>; queryParams?: Record<string, string> } | null;
}

export type RunTriggerResult =
  | { ok: true; events: unknown[] }
  | { ok: false; error: string; errorType: string };

export interface TriggerLifecycleRequest extends TriggerRequestBase {
  op: 'enable' | 'disable';
}

export type TriggerLifecycleResult =
  | { ok: true; output: unknown }
  | { ok: false; error: string; errorType: string };

const RUN_TIMEOUT_MS = 60_000;
const LIFECYCLE_TIMEOUT_MS = 30_000;

export async function runTrigger(
  input: RunTriggerRequest,
  registry: Registry,
  resolveCredential: CredentialResolver | null,
  storeFactory: TriggerStoreFactory,
): Promise<RunTriggerResult> {
  const located = locate(input, registry);
  if ('error' in located) return located;
  const { piece, trigger } = located;

  const auth = await resolveTriggerAuth(input, piece, resolveCredential);
  if (auth === MISSING_AUTH) {
    return { ok: false, error: 'credential not resolvable for trigger', errorType: 'MissingCredential' };
  }

  try {
    const ctx = buildTriggerContext(input, trigger.props ?? {}, auth, storeFactory);
    const raw = await withTimeout(
      Promise.resolve(trigger.run(ctx)),
      RUN_TIMEOUT_MS,
      `trigger run exceeded ${RUN_TIMEOUT_MS / 1000}s`,
    );
    const events = Array.isArray(raw) ? raw : raw == null ? [] : [raw];
    return { ok: true, events: events.map(jsonSafe) };
  } catch (err) {
    return asError(err, 'TriggerExecutionError');
  }
}

export async function triggerLifecycle(
  input: TriggerLifecycleRequest,
  registry: Registry,
  resolveCredential: CredentialResolver | null,
  storeFactory: TriggerStoreFactory,
): Promise<TriggerLifecycleResult> {
  const located = locate(input, registry);
  if ('error' in located) return located;
  const { piece, trigger } = located;

  const auth = await resolveTriggerAuth(input, piece, resolveCredential);
  if (auth === MISSING_AUTH) {
    return { ok: false, error: 'credential not resolvable for trigger', errorType: 'MissingCredential' };
  }

  const fn = input.op === 'enable' ? trigger.onEnable : trigger.onDisable;
  if (typeof fn !== 'function') return { ok: true, output: null }; // nothing to do

  try {
    const ctx = buildTriggerContext(input, trigger.props ?? {}, auth, storeFactory);
    const out = await withTimeout(
      Promise.resolve(fn.call(trigger, ctx)),
      LIFECYCLE_TIMEOUT_MS,
      `trigger ${input.op} exceeded ${LIFECYCLE_TIMEOUT_MS / 1000}s`,
    );
    return { ok: true, output: jsonSafe(out) };
  } catch (err) {
    return asError(err, 'TriggerLifecycleError');
  }
}

/* ------------------------------- internals ------------------------------- */

function locate(
  input: TriggerRequestBase,
  registry: Registry,
): { piece: LoadedPiece; trigger: NonNullable<ReturnType<typeof triggerOf>>['trigger'] } | { ok: false; error: string; errorType: string } {
  const slug = String(input.piece ?? '').replace(/^@activepieces\/piece-/, '');
  const found = triggerOf(registry, slug, String(input.trigger ?? ''));
  if (!found) {
    return { ok: false, error: `unknown piece trigger: ${slug}/${input.trigger}`, errorType: 'UnknownPieceTrigger' };
  }
  return found;
}

const MISSING_AUTH = Symbol('missing-auth');

async function resolveTriggerAuth(
  input: TriggerRequestBase,
  piece: LoadedPiece,
  resolveCredential: CredentialResolver | null,
): Promise<unknown> {
  let auth: unknown;
  if (input.credentialId && resolveCredential) {
    const data = await resolveCredential(String(input.credentialId));
    if (data !== null) auth = normalizeDbAuth(data, piece);
  }
  if (auth === undefined && input.authEnvKey) {
    const raw = process.env[String(input.authEnvKey)] ?? '';
    if (raw) auth = normalizeAuth(raw, piece);
  }
  if (auth === undefined && piece.auth && piece.auth.required !== false) return MISSING_AUTH;
  return auth;
}

function buildTriggerContext(
  input: RunTriggerRequest | TriggerLifecycleRequest,
  propDefs: Record<string, any>,
  auth: unknown,
  storeFactory: TriggerStoreFactory,
) {
  const store = storeFactory(String(input.workflowId), String(input.nodeKey));
  const payload = (input as RunTriggerRequest).payload ?? { body: {}, headers: {}, queryParams: {} };
  const filesDir = fs.mkdtempSync(path.join(os.tmpdir(), 'agently-trigger-'));
  return {
    auth,
    propsValue: withDefaults((input.props ?? {}) as Record<string, unknown>, propDefs),
    store,
    payload: {
      body: payload.body ?? {},
      headers: payload.headers ?? {},
      queryParams: payload.queryParams ?? {},
      rawBody: undefined,
    },
    webhookUrl: input.webhookUrl ?? '',
    project: { id: 'agently', externalId: async () => undefined },
    server: { apiUrl: 'http://unsupported.invalid/', publicUrl: 'http://unsupported.invalid/', token: '' },
    flows: { current: { id: String(input.workflowId), version: { id: 'v1' } } },
    files: {
      write: async ({ fileName, data }: { fileName: string; data: Buffer }) => {
        const p = path.join(filesDir, path.basename(fileName));
        await fs.promises.writeFile(p, data);
        return `file://${p}`;
      },
    },
    // Polling helpers read these; harmless for webhook triggers.
    maxItemsToPoll: undefined,
    app: undefined,
    setSchedule: () => undefined,
  };
}

function asError(err: unknown, fallbackType: string): { ok: false; error: string; errorType: string } {
  const e = err as Error & { errorType?: string };
  return {
    ok: false,
    error: e?.message ? String(e.message) : String(err),
    errorType: e?.errorType ?? fallbackType,
  };
}

function withTimeout<T>(p: Promise<T>, ms: number, msg: string): Promise<T> {
  let timer: NodeJS.Timeout;
  const timeout = new Promise<never>((_, reject) => {
    timer = setTimeout(() => {
      const err = new Error(msg) as Error & { errorType: string };
      err.errorType = 'TriggerTimeout';
      reject(err);
    }, ms);
  });
  return Promise.race([p, timeout]).finally(() => clearTimeout(timer!)) as Promise<T>;
}
