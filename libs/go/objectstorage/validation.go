package objectstorage

import (
	"encoding/hex"
	"path"
	"strings"
)

// ValidKey accepts a bounded relative slash-separated path without NUL, empty/trailing segments, or path traversal after cleaning.
// ValidKey принимает ограниченный относительный путь с разделителями «/» без NUL, пустого начала или конца и обхода каталогов после нормализации.
// It validates syntax only; tenant ownership and reserved prefixes must be checked by the caller.
// Функция проверяет только синтаксис; владение арендатором и зарезервированные префиксы должна проверять вызывающая сторона.
func ValidKey(value string) bool {
	if value == "" || len(value) > 1024 || strings.HasPrefix(value, "/") || strings.HasSuffix(value, "/") || strings.ContainsRune(value, '\x00') {
		return false
	}
	cleaned := path.Clean(value)
	return cleaned == value && cleaned != "." && !strings.HasPrefix(cleaned, "../")
}

// ValidDigest accepts only a sha256-prefixed hexadecimal value decoding to exactly 32 bytes; it does not hash object content.
// ValidDigest принимает только шестнадцатеричное значение с префиксом sha256, декодируемое ровно в 32 байта; содержимое объекта функция не хеширует.
func ValidDigest(value string) bool {
	if !strings.HasPrefix(value, "sha256:") || len(value) != len("sha256:")+64 {
		return false
	}
	decoded, err := hex.DecodeString(strings.TrimPrefix(value, "sha256:"))
	return err == nil && len(decoded) == 32
}
