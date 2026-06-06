import { z } from "zod";

/**
 * Reusable value objects and envelope types used across entities and the API.
 * These are deliberately small and storage-agnostic.
 */

/** ISO-8601 UTC timestamp. We keep timestamps as strings end-to-end so the
 *  same value crosses the JSON boundary unchanged (no Date drift). */
export const Timestamp = z.string().datetime({ offset: true });
export type Timestamp = z.infer<typeof Timestamp>;

/** A monetary amount. Stored as a number of USD for now; wrapped so a future
 *  multi-currency/minor-unit migration touches one type, not every call site. */
export const Money = z.object({
  amountUsd: z.number().nonnegative(),
});
export type Money = z.infer<typeof Money>;

/** Token accounting for a run or agent. */
export const Usage = z.object({
  tokensIn: z.number().int().nonnegative(),
  tokensOut: z.number().int().nonnegative(),
});
export type Usage = z.infer<typeof Usage>;

export const Viewport = z.object({
  width: z.number().int().positive(),
  height: z.number().int().positive(),
});
export type Viewport = z.infer<typeof Viewport>;

/** done/total step counter shown on run progress bars. */
export const StepProgress = z.object({
  done: z.number().int().nonnegative(),
  total: z.number().int().nonnegative(),
});
export type StepProgress = z.infer<typeof StepProgress>;

/** Per-agent live metrics surfaced in the graph and inspector. */
export const AgentMetrics = z.object({
  tokens: z.number().int().nonnegative(),
  costUsd: z.number().nonnegative(),
  runtimeMs: z.number().int().nonnegative().nullable(),
  toolCalls: z.number().int().nonnegative(),
  /** 0..1 */
  progress: z.number().min(0).max(1),
});
export type AgentMetrics = z.infer<typeof AgentMetrics>;

/** Person reference used for owners / triggered-by / actors. */
export const Principal = z.object({
  name: z.string(),
  initials: z.string().min(1).max(3),
});
export type Principal = z.infer<typeof Principal>;

/* ------------------------------------------------------------------ *
   Pagination — cursor-based, the only model that survives growth.
 * ------------------------------------------------------------------ */

export const PageQuery = z.object({
  /** Opaque cursor returned by a previous page. */
  cursor: z.string().optional(),
  limit: z.coerce.number().int().min(1).max(100).default(25),
});
export type PageQuery = z.infer<typeof PageQuery>;

export interface Page<T> {
  items: T[];
  /** Cursor to fetch the next page, or null at the end. */
  nextCursor: string | null;
  /** Total count when cheaply available (in-memory store); else undefined. */
  total?: number;
}

/** Build the response schema for a paginated list of `item`. */
export function pageOf<T extends z.ZodTypeAny>(item: T) {
  return z.object({
    items: z.array(item),
    nextCursor: z.string().nullable(),
    total: z.number().int().nonnegative().optional(),
  });
}
