package value

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// MetadataDigest binds all operator target fields, including policy and endpoint pins, without credentials.
// MetadataDigest связывает все operator target fields, включая policy и endpoint pins, без credentials.
func (target KubernetesTarget) MetadataDigest() string {
	raw, err := json.Marshal(target)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
