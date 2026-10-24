package platform

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// jsonValueDigest hashes the existing encoding/json bytes as lowercase SHA-256, preserving the
// historical ignored-marshal-error hash of nil bytes. It neither validates input nor provides a
// canonical cross-language JSON encoding or an authorization proof.
// jsonValueDigest хеширует прежние encoding/json bytes строчным SHA-256, сохраняя исторический
// hash nil bytes при игнорируемой ошибке Marshal. Не проверяет вход, не задаёт канонический
// межъязыковой JSON и не является доказательством полномочий.
func jsonValueDigest(value any) string {
	encoded, _ := json.Marshal(value)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}
