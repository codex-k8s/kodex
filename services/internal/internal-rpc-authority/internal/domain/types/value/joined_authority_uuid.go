// Package value contains authority-owned immutable value derivation without
// transport, persistence, authorization decisions or external effects.
// Пакет value содержит принадлежащие authority чистые преобразования значений
// без транспорта, хранения, решений о доступе и внешних эффектов.
package value

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// JoinedAuthorityUUID preserves the established authority ID mapping: SHA-256
// over NUL-joined parts without a trailing separator, truncated to 16 bytes with
// version-5 and RFC variant bits. It is not standard SHA-1 UUIDv5, a random ID,
// a credential or an unambiguous encoding of arbitrary NUL-containing tuples;
// callers own the fixed namespace and validated input grammar.
// JoinedAuthorityUUID сохраняет принятое отображение идентификаторов authority:
// SHA-256 частей, соединённых NUL без завершающего разделителя, усекается до
// 16 байтов с битами версии 5 и варианта RFC. Это не стандартный SHA-1 UUIDv5,
// не случайный ID, не полномочие и не однозначное кодирование произвольных
// кортежей с NUL; фиксированное пространство и допустимый ввод задаёт вызывающий.
func JoinedAuthorityUUID(parts ...string) string {
	digest := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	value := digest[:16]
	value[6] = (value[6] & 0x0f) | 0x50
	value[8] = (value[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(value)
	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" +
		encoded[16:20] + "-" + encoded[20:32]
}
