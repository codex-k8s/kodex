package platform

import (
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
)

// TestNormalizePlatformSearchQueryPreservesTextAndRuneBounds checks the original trim and two-to-two-hundred-rune limits with ASCII, Unicode, combining characters, invalid UTF-8 and interior whitespace. It deliberately preserves the old byte-return behavior rather than silently adding a stricter UTF-8 or normalization policy; no repository or search engine runs.
// TestNormalizePlatformSearchQueryPreservesTextAndRuneBounds проверяет исходное обрезание внешних пробелов и предел от двух до двухсот рун на ASCII, Unicode, комбинируемых символах, некорректном UTF-8 и внутренних пробелах. Намеренно сохраняется прежний возврат байтов вместо тихого добавления строгой проверки UTF-8 или нормализации; репозиторий и поисковый движок не запускаются.
func TestNormalizePlatformSearchQueryPreservesTextAndRuneBounds(t *testing.T) {
	for _, test := range []struct {
		name, input, want string
		invalid           bool
	}{
		{name: "empty", invalid: true},
		{name: "whitespace only", input: "\t\n\u2003\u00a0", invalid: true},
		{name: "single rune", input: " a ", invalid: true},
		{name: "lower bound", input: "ab", want: "ab"},
		{name: "unicode outer spaces", input: "\u2003ab\u00a0", want: "ab"},
		{name: "internal whitespace", input: " a\tb\nc ", want: "a\tb\nc"},
		{name: "emoji runes", input: "😀😀", want: "😀😀"},
		{name: "single emoji", input: "😀", invalid: true},
		{name: "combining code points", input: "e\u0301", want: "e\u0301"},
		{name: "ascii upper bound", input: strings.Repeat("a", 200), want: strings.Repeat("a", 200)},
		{name: "unicode upper bound", input: strings.Repeat("я", 200), want: strings.Repeat("я", 200)},
		{name: "trim before counting", input: " " + strings.Repeat("a", 200) + "\n", want: strings.Repeat("a", 200)},
		{name: "above upper bound", input: strings.Repeat("a", 201), invalid: true},
		{name: "invalid bytes retained", input: "\xff\xff", want: "\xff\xff"},
		{name: "single invalid byte", input: "\xff", invalid: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := normalizePlatformSearchQuery(test.input)
			if test.invalid {
				if got != "" || !errors.Is(err, errs.ErrInvalid) {
					t.Fatal("search-query rejection boundary changed")
				}
			} else if err != nil || got != test.want {
				t.Fatal("search-query bytes or accepted range changed")
			}
		})
	}
}
