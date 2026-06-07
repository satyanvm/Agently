package platform

import (
	"encoding/base64"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/agently/api/internal/domain"
)

// Paginate applies cursor pagination over an in-memory slice. The cursor encodes
// a numeric offset and is opaque to callers, so swapping to keyset pagination
// later changes only this function. Mirrors packages/core/src/platform/util.ts.
func Paginate[T any](items []T, query domain.PageQuery) domain.Page[T] {
	offset := decodeCursor(query.Cursor)
	limit := query.Limit
	if limit <= 0 {
		limit = 25
	}

	end := offset + limit
	if offset > len(items) {
		offset = len(items)
	}
	if end > len(items) {
		end = len(items)
	}
	slice := items[offset:end]
	// Ensure a non-nil slice so JSON encodes [] rather than null.
	if slice == nil {
		slice = []T{}
	}

	var nextCursor *string
	if next := offset + limit; next < len(items) {
		c := encodeCursor(next)
		nextCursor = &c
	}
	total := len(items)
	return domain.Page[T]{Items: slice, NextCursor: nextCursor, Total: &total}
}

func encodeCursor(offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte("o:" + strconv.Itoa(offset)))
}

func decodeCursor(cursor string) int {
	if cursor == "" {
		return 0
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimPrefix(string(raw), "o:"))
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// SortByNewest sorts items in place, newest first, by an ISO timestamp accessor.
func SortByNewest[T any](items []T, get func(T) string) {
	sort.SliceStable(items, func(i, j int) bool {
		return parseTime(get(items[i])) > parseTime(get(items[j]))
	})
}

func parseTime(s string) int64 {
	if s == "" {
		return 0
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return 0
	}
	return t.UnixMilli()
}
