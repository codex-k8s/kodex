// Package digests provides synthetic lexical boundary cases shared by control-plane tests.
// Package digests предоставляет синтетические lexical boundary cases для общих control-plane tests.
package digests

import "strings"

// TextCase specifies a named SHA-256 text sample and its expected lexical acceptance independently
// of any production validator; Text contains synthetic bytes only.
// TextCase задаёт имя SHA-256 text sample и ожидаемый lexical acceptance независимо от production
// validator; Text содержит только синтетические байты.
type TextCase struct {
	Name, Text string
	Valid      bool
}

// SHA256TextCases returns a fresh fourteen-case corpus covering exact lowercase length, zero,
// invalid alphabet, prefixes, whitespace, controls and UTF-8 boundaries without evaluating a validator.
// SHA256TextCases возвращает новый corpus из четырнадцати случаев exact lowercase length, zero,
// неверного алфавита, prefixes, whitespace, controls и UTF-8 без вызова validator.
func SHA256TextCases() []TextCase {
	return []TextCase{
		{"zero", strings.Repeat("0", 64), true},
		{"lowercase", strings.Repeat("f", 64), true},
		{"alphabet", strings.Repeat("0123456789abcdef", 4), true},
		{"empty", "", false},
		{"short", strings.Repeat("a", 63), false},
		{"long", strings.Repeat("a", 65), false},
		{"uppercase", strings.Repeat("A", 64), false},
		{"nonhex", strings.Repeat("g", 64), false},
		{"prefixed", "sha256:" + strings.Repeat("a", 64), false},
		{"space", strings.Repeat("a", 63) + " ", false},
		{"newline", strings.Repeat("a", 63) + "\n", false},
		{"nul", strings.Repeat("a", 63) + "\x00", false},
		{"utf8", strings.Repeat("a", 62) + "é", false},
		{"invalid utf8", strings.Repeat("a", 63) + string([]byte{0xff}), false},
	}
}
