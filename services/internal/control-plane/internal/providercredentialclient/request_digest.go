package providercredentialclient

import (
	"crypto/sha256"
	"encoding/hex"

	"google.golang.org/protobuf/proto"
)

// deterministicRequestDigest hashes the complete deterministic protobuf bytes and returns lowercase SHA-256 text.
// Marshal errors return no digest; the helper neither validates domain bindings nor authorizes the request, and deterministic protobuf is not cross-version canonical encoding.
// deterministicRequestDigest хеширует полные детерминированные байты protobuf и возвращает строчный текст SHA-256.
// При ошибке сериализации дайджест не возвращается; helper не проверяет доменные привязки и не авторизует запрос, а детерминированный protobuf не является межверсионным каноническим кодированием.
func deterministicRequestDigest(request proto.Message) (string, error) {
	encoded, err := (proto.MarshalOptions{Deterministic: true}).Marshal(request)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}
