/**
 * DB-backed credential resolution for the pieces worker
 * (docs/credentials-contract.md §7).
 *
 * The reasoner sends only the CREDENTIAL ID over the Temporal payload boundary;
 * this worker reads the secret values straight from the shared Agently Postgres
 * (same DATABASE_URL env the reasoner uses — apps/reasoner/reasoner/config.py).
 * DATABASE_URL is mandatory: credentials and trigger state must remain durable
 * and consistent with the API.
 */
import { createDecipheriv } from 'node:crypto';
import * as fs from 'node:fs';
import * as path from 'node:path';
import { Pool } from 'pg';

/** Resolve a credential id to its `data` jsonb, or null when the row is gone. */
export type CredentialResolver = (id: string) => Promise<Record<string, unknown> | null>;

/**
 * Best-effort repo-root .env loader (contract §5: both workers load the shared
 * repo-root .env). Mirrors python-dotenv semantics: real env vars always win,
 * quotes are trimmed, malformed lines are ignored. No dependency needed.
 */
export function loadRepoRootDotEnv(baseDir: string = __dirname): void {
  let dir = baseDir;
  for (let i = 0; i < 8; i++) {
    const cand = path.join(dir, '.env');
    if (fs.existsSync(path.join(dir, 'packages', 'nodes')) && fs.existsSync(cand)) {
      let text: string;
      try {
        text = fs.readFileSync(cand, 'utf8');
      } catch {
        return;
      }
      for (const line of text.split('\n')) {
        const m = /^\s*(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.*)\s*$/.exec(line);
        if (!m) continue;
        const [, key, rawVal] = m;
        if (process.env[key] !== undefined) continue; // real env wins
        let val = rawVal.trim();
        if (
          (val.startsWith('"') && val.endsWith('"')) ||
          (val.startsWith("'") && val.endsWith("'"))
        ) {
          val = val.slice(1, -1);
        }
        process.env[key] = val;
      }
      return;
    }
    const parent = path.dirname(dir);
    if (parent === dir) return;
    dir = parent;
  }
}

/**
 * Build the Postgres-backed resolver. Missing DATABASE_URL is a startup error.
 *
 * DB ERRORS THROW: a broken connection is infrastructure, so the activity fails
 * and Temporal's retry policy applies — unlike a missing row, which is a
 * business outcome (null → MissingCredential handling in execute.ts).
 */
export function makeDbCredentialResolver(databaseUrl: string | undefined): CredentialResolver {
  if (!databaseUrl?.trim()) {
    throw new Error('DATABASE_URL is required — the pieces worker resolves stored credentials from Postgres');
  }
  const pool = new Pool({ connectionString: databaseUrl, max: 3 });
  const refresher = makeOAuthRefresher();
  return async (id: string) => {
    const res = await pool.query('select type, data from credentials where id = $1', [id]);
    if (res.rowCount === 0) return null;
    const row = res.rows[0];
    const data = row?.data && typeof row.data === 'object' ? openCredentialData(row.data as Record<string, unknown>) : {};
    return refresher(id, String(row?.type ?? ''), data);
  };
}

/* ---------------------------- OAuth token refresh ---------------------------- */

/**
 * Sign-in credentials (apps/api oauth_service.go) store `expires_at` (unix
 * seconds), `refresh_token` and `token_url`. When the access token is expired
 * or about to be, swap the refresh token for a fresh one before the piece runs.
 * Refreshed tokens are cached in memory per credential id (the stored row is
 * sealed by the API and stays untouched); a failed refresh falls back to the
 * stored token so the provider's own 401 surfaces as the run error.
 */
export function makeOAuthRefresher(
  doFetch: typeof fetch = fetch,
  env: NodeJS.ProcessEnv = process.env,
  now: () => number = () => Date.now() / 1000,
) {
  const cache = new Map<string, { access_token: string; expires_at: number; extra: Record<string, unknown> }>();
  return async (id: string, type: string, data: Record<string, unknown>): Promise<Record<string, unknown>> => {
    const expiresAt = Number(data.expires_at ?? 0);
    const refreshToken = typeof data.refresh_token === 'string' ? data.refresh_token : '';
    const tokenUrl = typeof data.token_url === 'string' ? data.token_url : '';
    if (!expiresAt || !refreshToken || !tokenUrl) return data;

    const cached = cache.get(id);
    if (cached && cached.expires_at - 60 > now()) {
      return { ...data, ...cached.extra, access_token: cached.access_token, expires_at: cached.expires_at };
    }
    if (expiresAt - 60 > now()) return data;

    const [clientId, clientSecret] = oauthClient(type, env);
    if (!clientId || !clientSecret) return data;
    const form = new URLSearchParams({ grant_type: 'refresh_token', refresh_token: refreshToken });
    const headers: Record<string, string> = {
      'content-type': 'application/x-www-form-urlencoded',
      accept: 'application/json',
    };
    if (data.authorization_method === 'header') {
      headers.authorization = 'Basic ' + Buffer.from(`${clientId}:${clientSecret}`).toString('base64');
    } else {
      form.set('client_id', clientId);
      form.set('client_secret', clientSecret);
    }
    try {
      const res = await doFetch(tokenUrl, { method: 'POST', headers, body: form.toString() });
      if (!res.ok) return data;
      const tok = (await res.json()) as Record<string, unknown>;
      const access = typeof tok.access_token === 'string' ? tok.access_token : '';
      if (!access) return data;
      const entry = {
        access_token: access,
        expires_at: now() + Number(tok.expires_in ?? 3600),
        // Providers that rotate refresh tokens hand back a new one.
        extra: typeof tok.refresh_token === 'string' ? { refresh_token: tok.refresh_token } : {},
      };
      cache.set(id, entry);
      return { ...data, ...entry.extra, access_token: entry.access_token, expires_at: entry.expires_at };
    } catch {
      return data;
    }
  };
}

/** Same env naming as the API: pieces.google-sheets → OAUTH_GOOGLE_SHEETS_CLIENT_ID. */
export function oauthClient(type: string, env: NodeJS.ProcessEnv = process.env): [string, string] {
  const slug = type.replace(/^pieces\./, '');
  const key = slug.toUpperCase().replace(/[-.]/g, '_');
  let id = env[`OAUTH_${key}_CLIENT_ID`] ?? '';
  let secret = env[`OAUTH_${key}_CLIENT_SECRET`] ?? '';
  // One Google / Microsoft app covers every product in the family (apps/api oauth_apps.go).
  if ((!id || !secret) && (slug.startsWith('google') || slug === 'gmail' || slug === 'youtube')) {
    id = env.GOOGLE_CLIENT_ID ?? '';
    secret = env.GOOGLE_CLIENT_SECRET ?? '';
  } else if ((!id || !secret) && slug.startsWith('microsoft')) {
    id = env.MICROSOFT_CLIENT_ID ?? '';
    secret = env.MICROSOFT_CLIENT_SECRET ?? '';
  }
  return [id, secret];
}

/**
 * Decrypt a credentials.data value sealed by the API (platform/secretbox.go):
 * `{"$enc": "v1:<base64(nonce || ciphertext || tag)>"}` under AES-256-GCM with
 * AGENTLY_CREDENTIALS_KEY. Anything else is legacy plaintext.
 */
export function openCredentialData(
  data: Record<string, unknown>,
  rawKey: string | undefined = process.env.AGENTLY_CREDENTIALS_KEY,
): Record<string, unknown> {
  const enc = data['$enc'];
  if (typeof enc !== 'string') return data;
  if (!enc.startsWith('v1:')) throw new Error('credential envelope is malformed');
  const key = credentialsKey(rawKey);
  const sealed = Buffer.from(enc.slice(3), 'base64');
  const nonce = sealed.subarray(0, 12);
  const tag = sealed.subarray(sealed.length - 16);
  const ciphertext = sealed.subarray(12, sealed.length - 16);
  try {
    const decipher = createDecipheriv('aes-256-gcm', key, nonce);
    decipher.setAuthTag(tag);
    const plain = Buffer.concat([decipher.update(ciphertext), decipher.final()]);
    return JSON.parse(plain.toString('utf8')) as Record<string, unknown>;
  } catch {
    throw new Error('credential could not be decrypted — wrong AGENTLY_CREDENTIALS_KEY?');
  }
}

function credentialsKey(raw: string | undefined): Buffer {
  const value = raw?.trim();
  if (!value) throw new Error('credential is encrypted but AGENTLY_CREDENTIALS_KEY is not set');
  if (/^[0-9a-fA-F]{64}$/.test(value)) return Buffer.from(value, 'hex');
  const b64 = Buffer.from(value, 'base64');
  if (b64.length === 32) return b64;
  throw new Error('AGENTLY_CREDENTIALS_KEY must be 32 bytes, hex or base64 encoded');
}
