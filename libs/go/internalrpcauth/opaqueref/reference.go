// Package opaqueref provides the shared byte-bounded grammar of opaque resource references used by
// request context and image commands. It performs no identity, ownership, or authorization checks.
// Package opaqueref задаёт общую ограниченную байтами грамматику непрозрачных ссылок ресурсов для
// контекста запроса и команд образов. Проверки личности, владения и полномочий не выполняются.
package opaqueref

import "strings"

// Valid checks the exact prefix followed by an underscore and at least eight ASCII letters, digits,
// hyphens, or underscores, with a maximum total length of ninety-six bytes. The caller supplies its
// contract prefix; the prefix itself is not normalized or validated. Success proves only syntax,
// not resource existence, ownership, or permission, and is not a catalogue-key validation rule.
// Valid проверяет точный префикс с подчёркиванием и минимум восемью ASCII-буквами, цифрами, дефисами
// или подчёркиваниями при общей длине максимум девяносто шесть байтов. Вызывающий код передаёт
// префикс своего контракта; сам префикс не нормализуется и не проверяется. Успех доказывает только
// синтаксис, а не существование ресурса, владение или доступ, и не проверяет ключи каталога.
func Valid(reference, prefix string) bool {
	if len(reference) < len(prefix)+9 || len(reference) > 96 || !strings.HasPrefix(reference, prefix+"_") {
		return false
	}
	for _, character := range reference[len(prefix)+1:] {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || character == '-' || character == '_' {
			continue
		}
		return false
	}
	return true
}
