package services

import (
	"regexp"
	"strconv"
	"strings"
)

func itoa(n int) string { return strconv.Itoa(n) }

var nonAlphanum = regexp.MustCompile(`[^a-z0-9]+`)
var trimDashes = regexp.MustCompile(`^-+|-+$`)

// slugify mirrors the TS slug generation: lowercase, collapse non-alphanumerics
// to a single dash, trim leading/trailing dashes.
func slugify(name string) string {
	s := strings.ToLower(name)
	s = nonAlphanum.ReplaceAllString(s, "-")
	s = trimDashes.ReplaceAllString(s, "")
	return s
}
