// Package handler is the HTTP layer: thin route handlers that validate input,
// call a platform service, and return JSON in the shared success/error shape.
// Mirrors apps/web/app/api/** plus lib/server/http.ts.
package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/agently/api/internal/domain"
	"github.com/agently/api/internal/domain/validate"
)

// writeJSON encodes data as a JSON response with the given status.
func writeJSON(w http.ResponseWriter, data any, status int) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

type errorEnvelope struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Code    domain.ErrorCode     `json:"code"`
	Message string               `json:"message"`
	Details []domain.ErrorDetail `json:"details,omitempty"`
}

// fail writes the canonical error envelope with the status mapped from code.
func fail(w http.ResponseWriter, code domain.ErrorCode, message string, details []domain.ErrorDetail) {
	status, ok := domain.ErrorStatus[code]
	if !ok {
		status = http.StatusInternalServerError
	}
	writeJSON(w, errorEnvelope{Error: errorBody{Code: code, Message: message, Details: details}}, status)
}

// handle runs fn and maps its result/error to a response. Domain and validation
// errors become the error envelope; anything else is a 500 that never leaks
// internals. A status of 0 from fn defaults to 200.
func handle(w http.ResponseWriter, fn func() (any, int, error)) {
	data, status, err := fn()
	if err != nil {
		var de *domain.DomainError
		var ve *validate.ValidationError
		switch {
		case errors.As(err, &de):
			fail(w, de.Code, de.Message, de.Details)
		case errors.As(err, &ve):
			fail(w, domain.CodeValidationFailed, "Request validation failed", ve.Errors)
		default:
			fail(w, domain.CodeInternal, "Something went wrong", nil)
		}
		return
	}
	if status == 0 {
		status = http.StatusOK
	}
	writeJSON(w, data, status)
}

// decodeBody reads a JSON object body, tolerating an empty body as {}.
func decodeBody(r *http.Request) (map[string]any, error) {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		return map[string]any{}, nil
	}
	if len(raw) == 0 {
		return map[string]any{}, nil
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		// Malformed JSON → treat as empty so validation reports missing fields.
		return map[string]any{}, nil
	}
	if obj == nil {
		return map[string]any{}, nil
	}
	return obj, nil
}
