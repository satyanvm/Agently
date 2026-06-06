import { z } from "zod";

/**
 * A single, stable error envelope for every API response. Clients can branch on
 * `code` (machine-stable) and show `message` (human). This shape is independent
 * of transport and survives any backend rewrite.
 */

export const ErrorCode = z.enum([
  "bad_request",
  "validation_failed",
  "unauthorized",
  "forbidden",
  "not_found",
  "conflict",
  "rate_limited",
  "internal",
  "not_implemented",
]);
export type ErrorCode = z.infer<typeof ErrorCode>;

export const ApiError = z.object({
  error: z.object({
    code: ErrorCode,
    message: z.string(),
    /** Field-level validation details, when applicable. */
    details: z
      .array(z.object({ path: z.string(), message: z.string() }))
      .optional(),
    /** Correlation id echoed from the request, for log tracing. */
    requestId: z.string().optional(),
  }),
});
export type ApiError = z.infer<typeof ApiError>;

/** HTTP status mapping for each code — the one place this is decided. */
export const ERROR_STATUS: Record<ErrorCode, number> = {
  bad_request: 400,
  validation_failed: 422,
  unauthorized: 401,
  forbidden: 403,
  not_found: 404,
  conflict: 409,
  rate_limited: 429,
  internal: 500,
  not_implemented: 501,
};

/** A throwable domain error carrying an API code. Services throw these; the
 *  HTTP layer turns them into an {@link ApiError} response. */
export class DomainError extends Error {
  constructor(
    public readonly code: ErrorCode,
    message: string,
    public readonly details?: { path: string; message: string }[],
  ) {
    super(message);
    this.name = "DomainError";
  }

  static notFound(what: string): DomainError {
    return new DomainError("not_found", `${what} not found`);
  }
  static conflict(message: string): DomainError {
    return new DomainError("conflict", message);
  }
  static badRequest(message: string): DomainError {
    return new DomainError("bad_request", message);
  }
}
