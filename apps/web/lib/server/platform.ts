import { createPlatform, type Platform } from "@agently/core/platform";

/**
 * A single platform instance per server process. The in-memory store lives for
 * the lifetime of the process; stashing it on `globalThis` keeps it stable
 * across Next's dev hot-reloads so launched runs / read notifications persist
 * while you click around.
 *
 * This is the ONE place the API talks to the core. Swapping the in-memory store
 * for a Postgres-backed `Repositories` happens here and nowhere else.
 */
const g = globalThis as unknown as { __agentlyPlatform?: Platform };

export const platform: Platform = g.__agentlyPlatform ?? (g.__agentlyPlatform = createPlatform());
