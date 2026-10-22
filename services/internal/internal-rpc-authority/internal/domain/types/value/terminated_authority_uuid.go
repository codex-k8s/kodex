package value

import (
	"crypto/sha256"
	"encoding/hex"
)

// TerminatedAuthorityUUID preserves the readback/restore client ID mapping:
// each part is followed by NUL before SHA-256 truncation and version-5/RFC
// variant bits. No parts hash an empty stream; one empty part hashes one NUL.
// This is not standard SHA-1 UUIDv5, a random identifier or an unambiguous
// encoding of arbitrary NUL-containing tuples. Callers own their namespaces.
// TerminatedAuthorityUUID сохраняет отображение ID клиентов readback/restore:
// после каждой части пишется NUL, затем SHA-256 усекается с битами версии 5
// и варианта RFC. Без частей хешируется пустой поток; пустая часть даёт NUL.
// Это не стандартный SHA-1 UUIDv5, не случайный ID и не однозначное кодирование
// произвольных кортежей с NUL. Пространствами имён владеют вызывающие.
func TerminatedAuthorityUUID(parts ...string) string {
	digest := sha256.New()
	for _, part := range parts {
		_, _ = digest.Write([]byte(part))
		_, _ = digest.Write([]byte{0})
	}
	value := digest.Sum(nil)[:16]
	value[6] = (value[6] & 0x0f) | 0x50
	value[8] = (value[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(value)
	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" +
		encoded[16:20] + "-" + encoded[20:32]
}
