package nodepullidentity

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
)

// dnsRoot fixes the suffix used to distinguish generation-bound node-pull certificate common names.
// dnsRoot фиксирует суффикс, различающий common names сертификатов чтения узлов с привязкой к поколению.
const dnsRoot = "kodex-node-pull"

// CommonName encodes the first eight SHA-256 bytes of a node name plus its decimal generation and fixed suffix.
// It performs no node lookup or generation validation; certificate issuance must validate those inputs separately.
// CommonName кодирует первые восемь байт SHA-256 имени узла, десятичное поколение и фиксированный суффикс.
// Поиск узла и проверка поколения не выполняются; выдача сертификата обязана проверять входы отдельно.
func CommonName(nodeName string, generation uint64) string {
	nodeHash := sha256.Sum256([]byte(nodeName))
	return hex.EncodeToString(nodeHash[:8]) + ".g" + strconv.FormatUint(generation, 10) + "." + dnsRoot
}

// ValidCommonName requires a positive exact generation, fixed suffix and sixteen lowercase hexadecimal node-hash characters.
// It checks shape only, not which node produced the hash or whether a certificate/credential is trusted or current.
// ValidCommonName требует положительное точное поколение, фиксированный суффикс и шестнадцать hex-символов хеша узла нижнего регистра.
// Проверяется только форма, а не соответствующий хешу узел, доверие к сертификату и реквизитам или их актуальность.
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
