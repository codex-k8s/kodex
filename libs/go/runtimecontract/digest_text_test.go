package runtimecontract

import (
	"strings"
	"testing"
)

// TestValidSHA256TextBoundaries checks exact length/case, zero digests and rejection of prefixes, whitespace, controls and malformed UTF-8.
// Only lexical acceptance is tested; no file is read or compared to its alleged digest.
// TestValidSHA256TextBoundaries проверяет точную длину/регистр, нулевые дайджесты и запрет префиксов, пробелов, управляющих символов и неверного UTF-8.
// Проверяется только лексический допуск; файлы не читаются и не сравниваются с заявленным дайджестом.
func TestValidSHA256TextBoundaries(t *testing.T) {
	for _, test := range []struct {
		value string
		want  bool
	}{
		{"", false},
		{"0", false},
		{strings.Repeat("0", 64), true},
		{strings.Repeat("f", 64), true},
		{strings.Repeat("0123456789abcdef", 4), true},
		{strings.Repeat("a", 63), false},
		{strings.Repeat("a", 65), false},
		{strings.Repeat("A", 64), false},
		{strings.Repeat("g", 64), false},
		{"sha256:" + strings.Repeat("a", 64), false},
		{" " + strings.Repeat("a", 63), false},
		{strings.Repeat("a", 63) + " ", false},
		{strings.Repeat("a", 63) + "\n", false},
		{strings.Repeat("a", 63) + "\x00", false},
		{strings.Repeat("a", 63) + "/", false},
		{strings.Repeat("a", 62) + "é", false},
		{strings.Repeat("a", 61) + "０", false},
		{strings.Repeat("a", 63) + string([]byte{0xff}), false},
	} {
		if ValidSHA256Text(test.value) != test.want {
			t.Fatal("digest text boundary differs from the canonical grammar")
		}
	}
}

// TestValidSHA256TextMatchesAlphabet checks every byte at each position around the exact length and all first/last byte pairs.
// Expected acceptance is computed from a literal hexadecimal alphabet, independently of the shared regular expression.
// TestValidSHA256TextMatchesAlphabet проверяет каждый байт в каждой позиции около точной длины и все пары первого/последнего байтов.
// Ожидаемый допуск вычисляется по буквальному шестнадцатеричному алфавиту независимо от общего регулярного выражения.
func TestValidSHA256TextMatchesAlphabet(t *testing.T) {
	// alphabet lists the accepted ASCII bytes without reusing the validator's regular expression.
	// alphabet перечисляет допустимые ASCII-байты без повторного использования регулярного выражения валидатора.
	const alphabet = "0123456789abcdef"
	for _, length := range []int{63, 64, 65} {
		value := []byte(strings.Repeat("a", length))
		for position := range value {
			for character := 0; character < 256; character++ {
				value[position] = byte(character)
				want := length == 64 && strings.IndexByte(alphabet, byte(character)) >= 0
				if ValidSHA256Text(string(value)) != want {
					t.Fatal("digest text validator differs from the literal alphabet")
				}
			}
			value[position] = 'a'
		}
	}
	value := []byte(strings.Repeat("a", 64))
	for first := 0; first < 256; first++ {
		value[0] = byte(first)
		for last := 0; last < 256; last++ {
			value[len(value)-1] = byte(last)
			want := strings.IndexByte(alphabet, byte(first)) >= 0 && strings.IndexByte(alphabet, byte(last)) >= 0
			if ValidSHA256Text(string(value)) != want {
				t.Fatal("digest text validator differs at its boundary byte pair")
			}
		}
	}
}
