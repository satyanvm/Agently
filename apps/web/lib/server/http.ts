import { NextResponse } from "next/server";
import { z } from "zod";
import { DomainError, ERROR_STATUS, type ErrorCode } from "@agently/contracts";

/**
 * Thin HTTP plumbing shared by every route handler: JSON helpers, a single
 * error→envelope mapping, and zod-validated query/body parsing. Keeping this in
 * one place means every endpoint returns the same success/error shape.
 */

export function ok<T>(data: T, status = 200): NextResponse {
  return NextResponse.json(data, { status });
}

/** Cast a path string into a branded id type at the trust boundary. */
export const asId = <T>(s: string): T => s as unknown as T;

function errorBody(code: ErrorCode, message: string, details?: { path: string; message: string }[]) {
  return { error: { code, message, ...(details ? { details } : {}) } };
}

export function fail(code: ErrorCode, message: string, details?: { path: string; message: string }[]): NextResponse {
  return NextResponse.json(errorBody(code, message, details), { status: ERROR_STATUS[code] });
}

/** Wrap a handler so domain/validation/unknown errors become the error envelope. */
export async function handle(fn: () => Promise<NextResponse> | NextResponse): Promise<NextResponse> {
  try {
    return await fn();
  } catch (err) {
    if (err instanceof DomainError) {
      return fail(err.code, err.message, err.details);
    }
    if (err instanceof z.ZodError) {
      return fail(
        "validation_failed",
        "Request validation failed",
        err.issues.map((i) => ({ path: i.path.join("."), message: i.message })),
      );
    }
    // Unknown — never leak internals.
    console.error("[api] unhandled error:", err);
    return fail("internal", "Something went wrong");
  }
}

/** Parse a URL's query string with a zod schema (throws ZodError → 422). */
export function parseQuery<T extends z.ZodTypeAny>(schema: T, url: string): z.infer<T> {
  const params = new URL(url).searchParams;
  const obj: Record<string, string> = {};
  for (const [k, v] of params.entries()) obj[k] = v;
  return schema.parse(obj);
}

/** Parse a JSON body with a zod schema (tolerates empty body as {}). */
export async function parseBody<T extends z.ZodTypeAny>(schema: T, req: Request): Promise<z.infer<T>> {
  let raw: unknown = {};
  try {
    const text = await req.text();
    raw = text ? JSON.parse(text) : {};
  } catch {
    raw = {};
  }
  return schema.parse(raw);
}
