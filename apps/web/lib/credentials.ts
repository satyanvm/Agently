/**
 * Credentials REST client (docs/credentials-contract.md §4). Same-origin
 * /api/* paths, proxied to the Go API by next.config.ts — the same pattern as
 * lib/api.ts. Secret values are WRITE-ONLY: the API never returns stored
 * values, only which keys are set (`setKeys`).
 */

/** Summary shape returned by every credentials endpoint (never the values). */
export interface CredentialSummary {
  id: string;
  name: string;
  type: string;
  setKeys: string[];
  createdAt: string;
  updatedAt: string;
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, {
    ...init,
    headers: { "content-type": "application/json", ...(init?.headers ?? {}) },
    cache: "no-store",
  });
  if (!res.ok) {
    const body = await res.text();
    throw new Error(`API ${res.status} on ${path}: ${body.slice(0, 200)}`);
  }
  if (res.status === 204) return undefined as T;
  return res.json() as Promise<T>;
}

/** GET /api/credentials → all credential summaries for the workspace. */
export async function listCredentials(): Promise<CredentialSummary[]> {
  const data = await request<unknown>("/api/credentials");
  // Contract: bare array. Tolerate null/enveloped bodies rather than crash.
  if (Array.isArray(data)) return data as CredentialSummary[];
  if (data && Array.isArray((data as { items?: unknown }).items)) {
    return (data as { items: CredentialSummary[] }).items;
  }
  return [];
}

/** POST /api/credentials → 201 summary. `values` holds the secret key/values. */
export async function createCredential(input: {
  name: string;
  type: string;
  values: Record<string, unknown>;
}): Promise<CredentialSummary> {
  return request<CredentialSummary>("/api/credentials", {
    method: "POST",
    body: JSON.stringify(input),
  });
}

/**
 * PUT /api/credentials/{id} → 200 summary. `values` merges per-key: keys the
 * user didn't retype are simply omitted and the stored values are preserved.
 */
export async function updateCredential(
  id: string,
  patch: { name?: string; values?: Record<string, unknown> },
): Promise<CredentialSummary> {
  return request<CredentialSummary>(`/api/credentials/${encodeURIComponent(id)}`, {
    method: "PUT",
    body: JSON.stringify(patch),
  });
}

/** DELETE /api/credentials/{id} → 204. */
export async function deleteCredential(id: string): Promise<void> {
  await request<void>(`/api/credentials/${encodeURIComponent(id)}`, {
    method: "DELETE",
  });
}
