package callback

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// decodeAssistantConfigurationSnapshot verifies the exact lowercase SHA-256 of
// raw bytes before decoding one JSON value with unknown struct fields rejected.
// It preserves decoder partial writes and does not enforce EOF, canonical bytes,
// duplicate-key rejection, size or owner authority; each caller retains those
// existing shape/canonical/semantic checks and discards an invalid local target.
// decodeAssistantConfigurationSnapshot проверяет exact lowercase SHA-256 raw bytes
// до decode одного JSON value с отказом unknown struct fields. Partial writes
// decoder сохраняются; EOF,canonical bytes,duplicate keys,size и owner authority
// этот helper не проверяет. Каждый caller сохраняет свои shape/canonical/semantic
// проверки и отбрасывает неверный локальный target.
func decodeAssistantConfigurationSnapshot(raw []byte, expectedDigest string, target any) bool {
	hash := sha256.Sum256(raw)
	if hex.EncodeToString(hash[:]) != expectedDigest {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target) == nil
}
