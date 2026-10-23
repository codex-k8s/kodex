package value

import (
	"crypto/rand"
	"encoding/hex"
)

// NewAuthorityUUID preserves the authority unit's sixteen-byte crypto/rand
// encoding with version-four and RFC variant bits in lowercase UUID form.
// It retains the existing error-returning API; random generation neither
// registers an identifier nor guarantees durable uniqueness or authority.
// NewAuthorityUUID сохраняет кодирование unit authority из шестнадцати байтов
// crypto/rand с битами версии четыре и варианта RFC в строчной форме UUID.
// Прежний API с возвратом ошибки сохранён; случайная генерация не регистрирует
// идентификатор и не гарантирует устойчивую уникальность или полномочия.
func NewAuthorityUUID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(value[:])
	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" +
		encoded[16:20] + "-" + encoded[20:32], nil
}
