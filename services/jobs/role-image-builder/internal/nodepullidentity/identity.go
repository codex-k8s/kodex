package nodepullidentity

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
)

const dnsRoot = "kodex-node-pull"

func CommonName(nodeName string, generation uint64) string {
	nodeHash := sha256.Sum256([]byte(nodeName))
	return hex.EncodeToString(nodeHash[:8]) + ".g" + strconv.FormatUint(generation, 10) + "." + dnsRoot
}

func ValidCommonName(commonName string, generation uint64) bool {
	if generation == 0 {
		return false
	}
	suffix := ".g" + strconv.FormatUint(generation, 10) + "." + dnsRoot
	if !strings.HasSuffix(commonName, suffix) {
		return false
	}
	nodeHash := strings.TrimSuffix(commonName, suffix)
	return len(nodeHash) == 16 && strings.Trim(nodeHash, "0123456789abcdef") == ""
}
