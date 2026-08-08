package domain

import "fmt"

// A single, stable error envelope for every API response. Clients branch on the
// machine-stable code and show the human message. Mirrors
// packages/contracts/src/errors.ts.

type ErrorCode string

const (
	CodeBadRequest       ErrorCode = "bad_request"
	CodeValidationFailed ErrorCode = "validation_failed"
	CodeUnauthorized     ErrorCode = "unauthorized"
	CodeForbidden        ErrorCode = "forbidden"
	CodeNotFound         ErrorCode = "not_found"
	CodeConflict         ErrorCode = "conflict"
	CodeRateLimited      ErrorCode = "rate_limited"
	CodeInternal         ErrorCode = "internal"
	CodeNotImplemented   ErrorCode = "not_implemented"
	// CodeUpstreamFailed is a dependency we do not control saying no: the model
	// provider, the embedding provider, a missing generated artifact. Distinct
	// from CodeInternal because the message is meant to be READ — it names what
	// failed and usually what to do about it — where internal errors are
	// deliberately opaque.
	CodeUpstreamFailed ErrorCode = "upstream_failed"
)

// ErrorStatus is the HTTP status mapping for each code — decided in one place.
var ErrorStatus = map[ErrorCode]int{
	CodeBadRequest:       400,
	CodeValidationFailed: 422,
	CodeUnauthorized:     401,
	CodeForbidden:        403,
	CodeNotFound:         404,
	CodeConflict:         409,
	CodeRateLimited:      429,
	CodeInternal:         500,
	CodeNotImplemented:   501,
	CodeUpstreamFailed:   502,
}

// ErrorDetail carries field-level validation context.
type ErrorDetail struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

// DomainError is a typed domain error carrying an API code. Services return
// these; the HTTP layer turns them into an error envelope response.
type DomainError struct {
	Code    ErrorCode
	Message string
	Details []ErrorDetail
}

func (e *DomainError) Error() string { return e.Message }

func NotFound(what string) *DomainError {
	return &DomainError{Code: CodeNotFound, Message: what + " not found"}
}

func Conflict(message string) *DomainError {
	return &DomainError{Code: CodeConflict, Message: message}
}

func BadRequest(message string) *DomainError {
	return &DomainError{Code: CodeBadRequest, Message: message}
}

// Upstream reports a failed dependency. The message reaches the client verbatim,
// so it should name what failed and, where possible, the fix — these are the
// errors an operator debugs a misconfigured install from.
func Upstream(message string) *DomainError {
	return &DomainError{Code: CodeUpstreamFailed, Message: message}
}

// Errorf builds an internal error with a formatted message.
func Errorf(code ErrorCode, format string, args ...any) *DomainError {
	return &DomainError{Code: code, Message: fmt.Sprintf(format, args...)}
}
