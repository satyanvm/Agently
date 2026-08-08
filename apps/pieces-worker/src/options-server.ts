/**
 * Dynamic-prop options resolver — the worker side of the builder's n8n-style
 * "From list" dropdowns. A tiny HTTP server (NOT Temporal: the builder needs
 * an interactive round-trip, and options lookups are read-only) that loads the
 * piece from the same registry as execute_piece, resolves auth the same way
 * (a selected DB credential must resolve; otherwise the explicit env key), and invokes the prop's real
 * `options()` resolver.
 *
 *   POST /options
 *     { piece, actionOrTrigger, propKey, credentialId?, authEnvKey?, props? }
 *   → { ok: true, options: [{label, value}], disabled?, placeholder? }
 *   | { ok: false, error, errorType }   (business errors returned, never 500)
 */
import * as http from 'node:http';
import { CredentialResolver } from './credentials';
import { jsonSafe, normalizeAuth, normalizeDbAuth } from './execute';
import { actionOf, Registry } from './pieces';
import {
  runTrigger,
  triggerLifecycle,
  RunTriggerRequest,
  TriggerLifecycleRequest,
} from './trigger-runtime';
import { TriggerStoreFactory } from './trigger-store';

export interface OptionsRequest {
  piece: string; // "@activepieces/piece-slack" (or bare slug)
  actionOrTrigger: string;
  propKey: string;
  credentialId?: string | null;
  authEnvKey?: string | null;
  props?: Record<string, unknown>;
}

export type OptionsResponse =
  | { ok: true; options: Array<{ label: string; value: unknown }>; disabled?: boolean; placeholder?: string }
  | { ok: false; error: string; errorType: string };

const OPTIONS_TIMEOUT_MS = 10_000;
const MAX_BODY_BYTES = 1 << 20;

export function startOptionsServer(
  registry: Registry,
  resolveCredential: CredentialResolver | null,
  storeFactory: TriggerStoreFactory,
  port: number = Number(process.env.PIECES_HTTP_PORT ?? 7391),
): http.Server {
  // The worker's interactive HTTP surface: dynamic-prop options lookups plus
  // the trigger runtime (webhook/poll event transformation + enable/disable).
  const routes: Record<string, (body: string) => Promise<unknown>> = {
    '/options': (body) =>
      resolveOptions(JSON.parse(body) as OptionsRequest, registry, resolveCredential),
    '/run-trigger': (body) =>
      runTrigger(JSON.parse(body) as RunTriggerRequest, registry, resolveCredential, storeFactory),
    '/trigger-lifecycle': (body) =>
      triggerLifecycle(JSON.parse(body) as TriggerLifecycleRequest, registry, resolveCredential, storeFactory),
  };

  const server = http.createServer((req, res) => {
    const route = req.method === 'POST' ? routes[req.url ?? ''] : undefined;
    if (!route) {
      res.writeHead(404, { 'content-type': 'application/json' });
      res.end('{"ok":false,"error":"not found","errorType":"NotFound"}');
      return;
    }
    let body = '';
    let overflow = false;
    req.on('data', (chunk) => {
      body += chunk;
      if (body.length > MAX_BODY_BYTES) {
        overflow = true;
        req.destroy();
      }
    });
    req.on('end', async () => {
      let out: unknown;
      if (overflow) {
        out = { ok: false, error: 'request body too large', errorType: 'BadRequest' };
      } else {
        try {
          out = await route(body);
        } catch (err) {
          const e = err as Error & { errorType?: string };
          out = {
            ok: false,
            error: e?.message ? String(e.message) : String(err),
            errorType: e?.errorType ?? 'OptionsResolutionError',
          };
        }
      }
      res.writeHead(200, { 'content-type': 'application/json' });
      res.end(JSON.stringify(out));
    });
  });
  server.listen(port, () =>
    console.log(`piece HTTP runtime on :${port} (POST /options, /run-trigger, /trigger-lifecycle)`),
  );
  server.on('error', (err) => console.error('piece HTTP runtime error:', err));
  return server;
}

async function resolveOptions(
  input: OptionsRequest,
  registry: Registry,
  resolveCredential: CredentialResolver | null,
): Promise<OptionsResponse> {
  const slug = String(input.piece ?? '').replace(/^@activepieces\/piece-/, '');
  const found = actionOf(registry, slug, String(input.actionOrTrigger ?? ''));
  if (!found) {
    return {
      ok: false,
      error: `unknown piece action: ${slug}/${input.actionOrTrigger}`,
      errorType: 'UnknownPieceAction',
    };
  }
  const { piece, action } = found;

  const def = (action.props ?? {})[String(input.propKey ?? '')];
  if (!def || typeof def.options !== 'function') {
    return {
      ok: false,
      error: `prop "${input.propKey}" has no dynamic options resolver`,
      errorType: 'NoOptionsResolver',
    };
  }

  // Same resolution rule as execute_piece: a selected DB credential must
  // resolve; only nodes without a selected id may use the explicit env key.
  let auth: unknown;
  if (input.credentialId) {
    if (!resolveCredential) {
      return { ok: false, error: 'database credential resolver unavailable', errorType: 'MissingCredential' };
    }
    const data = await resolveCredential(String(input.credentialId));
    if (data === null) {
      return { ok: false, error: `credential ${input.credentialId} not found`, errorType: 'MissingCredential' };
    }
    auth = normalizeDbAuth(data, piece);
  } else if (input.authEnvKey) {
    const raw = process.env[String(input.authEnvKey)] ?? '';
    if (raw) auth = normalizeAuth(raw, piece);
  }
  if (auth === undefined && piece.auth) {
    return {
      ok: false,
      error: 'credential not resolvable for options lookup',
      errorType: 'MissingCredential',
    };
  }

  // AP signature: options(propsValue, context) — propsValue carries auth plus
  // the node's current config so refresher-dependent dropdowns see their inputs.
  const propsValue = { auth, ...(input.props ?? {}) };
  const ctx = {
    searchValue: undefined,
    project: { id: 'agently', externalId: async () => undefined },
    server: { apiUrl: 'http://unsupported.invalid/', publicUrl: 'http://unsupported.invalid/', token: '' },
    flows: { current: { id: 'agently-builder', version: { id: 'v1' } } },
  };

  const state = await withTimeout(
    Promise.resolve(def.options(propsValue, ctx)),
    OPTIONS_TIMEOUT_MS,
    `options lookup exceeded ${OPTIONS_TIMEOUT_MS / 1000}s`,
  );

  const rawOptions = Array.isArray((state as any)?.options) ? (state as any).options : [];
  return {
    ok: true,
    options: rawOptions.map((o: any) => ({
      label: String(o?.label ?? o?.value ?? ''),
      value: jsonSafe(o?.value),
    })),
    disabled: (state as any)?.disabled ? true : undefined,
    placeholder: (state as any)?.placeholder ? String((state as any).placeholder) : undefined,
  };
}

function withTimeout<T>(p: Promise<T>, ms: number, msg: string): Promise<T> {
  let timer: NodeJS.Timeout;
  const timeout = new Promise<never>((_, reject) => {
    timer = setTimeout(() => {
      const err = new Error(msg) as Error & { errorType: string };
      err.errorType = 'OptionsTimeout';
      reject(err);
    }, ms);
  });
  return Promise.race([p, timeout]).finally(() => clearTimeout(timer!)) as Promise<T>;
}
