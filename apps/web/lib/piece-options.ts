/**
 * Client for POST /api/pieces/options — live authenticated dynamic-prop
 * options (the builder's n8n-style "From list" dropdowns). Same same-origin
 * /api/* proxy pattern as lib/credentials.ts. Failures come back as
 * {ok:false} business results, never throws for the common cases.
 */

export interface PieceOption {
  label: string;
  value: unknown;
}

export type PieceOptionsResult =
  | { ok: true; options: PieceOption[]; disabled?: boolean; placeholder?: string }
  | { ok: false; error: string; errorType: string };

export interface PieceOptionsRequest {
  piece: string; // "@activepieces/piece-slack"
  actionOrTrigger: string; // "send_channel_message"
  propKey: string;
  credentialId?: string;
  authEnvKey?: string;
  props?: Record<string, unknown>;
}

export async function fetchPieceOptions(input: PieceOptionsRequest): Promise<PieceOptionsResult> {
  try {
    const res = await fetch("/api/pieces/options", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify(input),
      cache: "no-store",
    });
    if (!res.ok) {
      return { ok: false, error: `API ${res.status}`, errorType: "OptionsUnavailable" };
    }
    const data = (await res.json()) as PieceOptionsResult;
    if (data && typeof data === "object" && "ok" in data) return data;
    return { ok: false, error: "malformed options response", errorType: "OptionsUnavailable" };
  } catch (err) {
    return { ok: false, error: String(err), errorType: "OptionsUnavailable" };
  }
}

/** Derive the pieces-worker request coordinates from a `pieces.<slug>.<action>`
 *  catalog id, or null for non-pieces nodes. */
export function pieceCoordinates(
  nodeTypeId: string,
): { piece: string; actionOrTrigger: string; authEnvKey: string } | null {
  if (!nodeTypeId.startsWith("pieces.")) return null;
  const rest = nodeTypeId.slice("pieces.".length);
  const dot = rest.indexOf(".");
  if (dot <= 0) return null;
  const slug = rest.slice(0, dot);
  return {
    piece: `@activepieces/piece-${slug}`,
    actionOrTrigger: rest.slice(dot + 1),
    authEnvKey: `AP_${slug.toUpperCase().replace(/-/g, "_")}_AUTH`,
  };
}
