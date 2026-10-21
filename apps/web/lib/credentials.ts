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

/* ------------------------------ OAuth sign-in ------------------------------ */

/** GET /api/oauth/{type} — can this credential type sign in via the browser? */
export interface OAuthStatus {
  type: string;
  supported: boolean;
  /** False until the operator sets the provider's client id/secret env vars. */
  configured: boolean;
  redirectUrl: string;
  clientIdEnv?: string;
  clientSecretEnv?: string;
}

export async function getOAuthStatus(type: string): Promise<OAuthStatus> {
  return request<OAuthStatus>(`/api/oauth/${encodeURIComponent(type)}`);
}

/** Message the /oauth/done popup page broadcasts back to the opener. */
export interface OAuthResult {
  type: "agently-oauth";
  status: "connected" | "error";
  credentialId?: string;
  message?: string;
}

export const OAUTH_CHANNEL = "agently-oauth";

/**
 * Open the provider's consent screen in a popup and resolve with the id of the
 * credential the API stored. The popup reports back over a BroadcastChannel:
 * providers like Google send Cross-Origin-Opener-Policy, which severs
 * window.opener (and makes `popup.closed` lie), so neither can be trusted.
 * `cancel()` stops waiting, e.g. when the user gave up on the popup.
 */
export function signInWithPopup(opts: {
  type: string;
  name?: string;
  /** Re-authorize this existing credential instead of creating a new one. */
  credentialId?: string;
  /** Connection props that fill {placeholders} in the provider URLs. */
  props?: Record<string, unknown>;
}): { result: Promise<string>; cancel: () => void } {
  const q = new URLSearchParams();
  if (opts.name) q.set("name", opts.name);
  if (opts.credentialId) q.set("credentialId", opts.credentialId);
  if (opts.props && Object.keys(opts.props).length > 0) q.set("props", JSON.stringify(opts.props));
  const url = `/api/oauth/${encodeURIComponent(opts.type)}/start?${q.toString()}`;

  let cancel = () => {};
  const result = new Promise<string>((resolve, reject) => {
    const width = 520;
    const height = 680;
    const left = window.screenX + Math.max(0, (window.outerWidth - width) / 2);
    const top = window.screenY + Math.max(0, (window.outerHeight - height) / 3);
    const popup = window.open(
      url,
      "agently-oauth",
      `popup,width=${width},height=${height},left=${left},top=${top}`,
    );
    if (!popup) {
      reject(new Error("The sign-in window was blocked — allow pop-ups for this site and try again."));
      return;
    }
    const channel = new BroadcastChannel(OAUTH_CHANNEL);
    const timeout = window.setTimeout(() => finish(new Error("Sign-in timed out.")), 10 * 60 * 1000);
    const finish = (err: Error | null, id?: string) => {
      window.clearTimeout(timeout);
      channel.close();
      if (err) reject(err);
      else resolve(id!);
    };
    channel.onmessage = (e: MessageEvent<OAuthResult>) => {
      const d = e.data;
      if (d?.type !== "agently-oauth") return;
      if (d.status === "connected" && d.credentialId) finish(null, d.credentialId);
      else finish(new Error(d.message || "Sign-in failed."));
    };
    cancel = () => {
      try {
        popup.close();
      } catch {
        /* severed by COOP — nothing to close */
      }
      finish(new Error("cancelled"));
    };
  });
  return { result, cancel };
}

/** GET /api/oauth/apps — the operator's checklist of popular providers' OAuth apps. */
export interface PopularOAuthApp extends OAuthStatus {
  label: string;
  consoleUrl: string;
  /** "google" / "microsoft": one app covers the whole family. */
  family?: string;
  familyClientIdEnv?: string;
  familyClientSecretEnv?: string;
}

export async function listOAuthApps(): Promise<PopularOAuthApp[]> {
  return request<PopularOAuthApp[]>("/api/oauth/apps");
}
