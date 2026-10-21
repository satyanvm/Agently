/**
 * Typed access to the generated credential-type catalog
 * (docs/credentials-contract.md §3). credential-types.generated.json is written
 * ONLY by packages/nodes/build-web.mjs — never hand-edit it; it may be
 * regenerated (and grow) at any time, so lookups here must tolerate ids that
 * are missing from the current file.
 */

import rawTypes from "./credential-types.generated.json";
import type { NodeField } from "./node-catalog";

export interface CredentialTypeDef {
  id: string;
  label: string;
  /** "catalog" (hand-written providers) or "pieces" (Activepieces). */
  source?: string;
  /** Pieces only: secret_text | basic_auth | oauth2 | custom_auth. */
  authType?: string;
  /** True when the provider supports browser sign-in (GET /api/oauth/{id}/start). */
  oauth?: boolean;
  /** Catalog types only: the piece whose OAuth flow signs this type in. */
  oauthVia?: string;
  /** Catalog types only: the field the signed-in access token is stored under. */
  oauthTokenKey?: string;
  fields: NodeField[];
}

const CREDENTIAL_TYPES = rawTypes as unknown as Record<string, CredentialTypeDef>;

/** "pieces.google-sheets" → "Google Sheets"; "slack" → "Slack". */
function prettyTypeLabel(id: string): string {
  const base = id.replace(/^pieces\./, "");
  return base
    .split(/[._-]+/)
    .filter(Boolean)
    .map((w) => w.charAt(0).toUpperCase() + w.slice(1))
    .join(" ");
}

/**
 * Look up a credential type definition. When the id isn't in the generated
 * file (catalog regeneration race, stale stub), synthesize a minimal def so
 * the credential UI stays usable — existing credentials of that type can
 * still be selected; the create form just has no provider fields.
 */
export function credentialTypeDef(id: string): CredentialTypeDef {
  const def = CREDENTIAL_TYPES[id];
  if (def) return { ...def, fields: def.fields ?? [] };
  return { id, label: prettyTypeLabel(id), fields: [] };
}

/** Token fields the OAuth callback writes itself — hidden behind "paste a token". */
export function oauthTokenKeys(def: CredentialTypeDef): Set<string> {
  return def.oauthTokenKey ? new Set([def.oauthTokenKey]) : new Set(["access_token", "refresh_token"]);
}

/** The field a pasted token must fill for this type to work. */
export function oauthAccessKey(def: CredentialTypeDef): string {
  return def.oauthTokenKey ?? "access_token";
}

/** Does this credential type log in (OAuth / account) rather than take a key? */
export function isLoginType(def: CredentialTypeDef): boolean {
  return Boolean(def.oauth) || def.authType === "oauth2";
}
