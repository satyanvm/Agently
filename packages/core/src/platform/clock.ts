/**
 * Time is a dependency, not a global. Passing a Clock lets the seed be
 * deterministic, lets tests freeze time, and keeps `Date.now()` out of business
 * logic. The interface is tiny on purpose.
 */
export interface Clock {
  now(): number;
  iso(): string;
}

export const systemClock: Clock = {
  now: () => Date.now(),
  iso: () => new Date().toISOString(),
};

/** A clock frozen at a fixed instant — used to seed deterministic data. */
export function fixedClock(at: string | number): Clock {
  const ms = typeof at === "string" ? Date.parse(at) : at;
  return { now: () => ms, iso: () => new Date(ms).toISOString() };
}
