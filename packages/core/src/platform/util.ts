import type { Page, PageQuery } from "@agently/contracts";

/**
 * Cursor pagination over an in-memory array. The cursor encodes a numeric
 * offset; it is opaque to callers, so swapping to keyset pagination against
 * Postgres later changes only this function, not any caller.
 */
export function paginate<T>(items: readonly T[], query: PageQuery): Page<T> {
  const offset = decodeCursor(query.cursor);
  const limit = query.limit;
  const slice = items.slice(offset, offset + limit);
  const nextOffset = offset + limit;
  return {
    items: slice,
    nextCursor: nextOffset < items.length ? encodeCursor(nextOffset) : null,
    total: items.length,
  };
}

function encodeCursor(offset: number): string {
  return Buffer.from(`o:${offset}`).toString("base64url");
}

function decodeCursor(cursor: string | undefined): number {
  if (!cursor) return 0;
  try {
    const raw = Buffer.from(cursor, "base64url").toString("utf8");
    const n = Number(raw.replace(/^o:/, ""));
    return Number.isFinite(n) && n >= 0 ? n : 0;
  } catch {
    return 0;
  }
}

/** Stable descending sort by ISO timestamp (newest first). */
export function byNewest<T>(get: (t: T) => string) {
  return (a: T, b: T) => Date.parse(get(b)) - Date.parse(get(a));
}
