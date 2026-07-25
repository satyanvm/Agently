/**
 * DB-backed credential resolution for the pieces worker
 * (docs/credentials-contract.md §7).
 *
 * The reasoner sends only the CREDENTIAL ID over the Temporal payload boundary;
 * this worker reads the secret values straight from the shared Agently Postgres
 * (same DATABASE_URL env the reasoner uses — apps/reasoner/reasoner/config.py).
 * A worker without DATABASE_URL simply cannot resolve ids and falls back to the
 * env-var path in execute.ts.
 */
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
 * Build the Postgres-backed resolver, or null when DATABASE_URL is unset
 * (credential-id resolution then degrades to the env fallback).
 *
 * DB ERRORS THROW: a broken connection is infrastructure, so the activity fails
 * and Temporal's retry policy applies — unlike a missing row, which is a
 * business outcome (null → MissingCredential handling in execute.ts).
 */
export function makeDbCredentialResolver(databaseUrl: string | undefined): CredentialResolver | null {
  if (!databaseUrl) return null;
  const pool = new Pool({ connectionString: databaseUrl, max: 3 });
  return async (id: string) => {
    const res = await pool.query('select data from credentials where id = $1', [id]);
    if (res.rowCount === 0) return null;
    const data = res.rows[0]?.data;
    return data && typeof data === 'object' ? (data as Record<string, unknown>) : {};
  };
}
