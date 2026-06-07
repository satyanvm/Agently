package services

import "testing"

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"My Test Flow":            "my-test-flow",
		"  Leading & Trailing  ":  "leading-trailing",
		"Already-slugged":         "already-slugged",
		"Punctuation!!! Heavy???": "punctuation-heavy",
		"UPPER lower 123":         "upper-lower-123",
	}
	for in, want := range cases {
		if got := slugify(in); got != want {
			t.Errorf("slugify(%q) = %q, want %q", in, got, want)
		}
	}
}
