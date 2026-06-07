// Package validate is the Go replacement for the zod schemas in
// packages/contracts. Each Parse* function validates and coerces raw input
// (query strings or decoded JSON bodies) into a typed value, returning a
// *ValidationError on failure. The error carries field-level details that the
// HTTP layer renders as { code: "validation_failed", details: [{path, message}] }.
package validate

import (
	"strconv"

	"github.com/agently/api/internal/domain"
)

// ValidationError accumulates field-level errors. It implements error and is
// recognized by the HTTP layer to produce a 422 envelope.
type ValidationError struct {
	Errors []domain.ErrorDetail
}

func (v *ValidationError) Error() string { return "request validation failed" }

func (v *ValidationError) HasErrors() bool { return len(v.Errors) > 0 }

func (v *ValidationError) add(path, message string) {
	v.Errors = append(v.Errors, domain.ErrorDetail{Path: path, Message: message})
}

// stringRequired records an error when value is empty.
func (v *ValidationError) stringRequired(path, value string) {
	if value == "" {
		v.add(path, "is required")
	}
}

func (v *ValidationError) stringMaxLen(path, value string, max int) {
	if len(value) > max {
		v.add(path, "must be at most "+strconv.Itoa(max)+" characters")
	}
}

func (v *ValidationError) stringMinLen(path, value string, min int) {
	if len(value) < min {
		v.add(path, "must be at least "+strconv.Itoa(min)+" characters")
	}
}

// enumValue records an error when value is non-empty and not in valid.
func (v *ValidationError) enumValue(path, value string, valid map[string]bool) {
	if value != "" && !valid[value] {
		v.add(path, "invalid value \""+value+"\"")
	}
}

// coerceInt parses a non-negative integer from a query string. Returns (0,
// false) and no error when raw is empty (caller applies a default).
func (v *ValidationError) coerceInt(path, raw string) (int, bool) {
	if raw == "" {
		return 0, false
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		v.add(path, "must be an integer")
		return 0, false
	}
	return n, true
}

// coerceBool parses zod-style boolean coercion from a query string: any
// non-empty value except "false"/"0" is true (mirrors z.coerce.boolean()).
func (v *ValidationError) coerceBool(raw string) (bool, bool) {
	if raw == "" {
		return false, false
	}
	switch raw {
	case "false", "0":
		return false, true
	default:
		return true, true
	}
}

/* ----------------------- shared helpers ----------------------- */

// getString reads a string field from a decoded JSON object.
func getString(obj map[string]any, key string) string {
	if v, ok := obj[key].(string); ok {
		return v
	}
	return ""
}

// getStringSlice reads a []string field from a decoded JSON object.
func getStringSlice(obj map[string]any, key string) []string {
	raw, ok := obj[key].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// getMap reads a map[string]any field from a decoded JSON object.
func getMap(obj map[string]any, key string) map[string]any {
	if m, ok := obj[key].(map[string]any); ok {
		return m
	}
	return nil
}

// hasKey reports whether a key is present (and non-null) in the object.
func hasKey(obj map[string]any, key string) bool {
	v, ok := obj[key]
	return ok && v != nil
}
