package runtimecontract

import (
	"regexp"
	"strings"
	"testing"
)

// TestValidDNSLabelBoundaries checks numeric-leading names, internal hyphens and exact byte limits,
// rejecting separators, controls, uppercase, Unicode and malformed UTF-8 without normalizing input.
// TestValidDNSLabelBoundaries проверяет имена с первой цифрой, внутренние дефисы и точные байтовые
// границы, отклоняя разделители, управляющие символы, верхний регистр, Unicode и неверный UTF-8.
func TestValidDNSLabelBoundaries(t *testing.T) {
	for _, value := range []string{"a", "0", "9x", "a9", "a-0", "a--b", "xn--bcher-kva", strings.Repeat("a", 63), "0" + strings.Repeat("-", 61) + "9"} {
		if !ValidDNSLabel(value) {
			t.Fatal("valid resource label rejected")
		}
	}
	for _, value := range []string{"", "-a", "a-", "a.b", "a/b", "a_b", "A", " a", "a ", "a\n", "a\x00b", "а", "é", "a\u200bb", string([]byte{0xff}), strings.Repeat("a", 64)} {
		if ValidDNSLabel(value) {
			t.Fatal("invalid resource label accepted")
		}
	}
}

// TestValidDNSLabelMatchesGrammar compares the loop with an independent anchored grammar for all
// byte pairs and every byte at each position near the length boundaries; this is lexical validation,
// not a DNS lookup, Kubernetes admission test or proof of owner scope.
// TestValidDNSLabelMatchesGrammar сравнивает цикл с независимой закреплённой грамматикой для всех
// пар байтов и каждого байта во всех позициях около границ длины; это лексическая проверка,
// а не DNS-запрос, проверка допуска Kubernetes или доказательство области владельца.
func TestValidDNSLabelMatchesGrammar(t *testing.T) {
	grammar := regexp.MustCompile(`^[a-z0-9](?:[-a-z0-9]{0,61}[a-z0-9])?$`)
	check := func(value string) {
		if ValidDNSLabel(value) != grammar.MatchString(value) {
			t.Fatal("resource label validator differs from its closed grammar")
		}
	}
	check("")
	for first := 0; first < 256; first++ {
		check(string([]byte{byte(first)}))
		for last := 0; last < 256; last++ {
			check(string([]byte{byte(first), byte(last)}))
		}
	}
	for _, length := range []int{1, 2, 3, 62, 63, 64} {
		value := []byte(strings.Repeat("a", length))
		for position := range value {
			for character := 0; character < 256; character++ {
				value[position] = byte(character)
				check(string(value))
			}
			value[position] = 'a'
		}
	}
}
