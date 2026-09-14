package platform

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateKeepsDatabaseCharacterLimit(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name    string
		value   string
		maximum int
		want    string
	}{
		{name: "unchanged", value: "  готово  ", maximum: 10, want: "готово"},
		{name: "ascii", value: strings.Repeat("a", 2001), maximum: 2000, want: strings.Repeat("a", 1999) + "…"},
		{name: "unicode", value: "абвг", maximum: 3, want: "аб…"},
		{name: "marker only", value: "long", maximum: 1, want: "…"},
		{name: "empty boundary", value: "long", maximum: 0, want: ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			actual := truncate(test.value, test.maximum)
			if actual != test.want {
				t.Fatalf("truncate() = %q, want %q", actual, test.want)
			}
			if utf8.RuneCountInString(actual) > test.maximum && test.maximum >= 0 {
				t.Fatalf("truncate() returned %d characters for maximum %d", utf8.RuneCountInString(actual), test.maximum)
			}
		})
	}
}
