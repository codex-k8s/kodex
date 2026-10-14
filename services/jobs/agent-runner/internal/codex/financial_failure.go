package codex

import (
	"encoding/json"
	"unicode/utf8"
)

const codexNoRateLimitSnapshots = "failed to fetch codex rate limits: no snapshots returned"

// Исключение относится только к завершённому коррелированному RPC закреплённого
// Codex 0.160.0. Message используется локально, но не сохраняется в диагностике.
// Источник: a956835d020762cb2b570053af06f643a11c0ecc/account_processor.rs.
func financialSnapshotsUnavailable(method string, raw json.RawMessage) bool {
	if method != "account/rateLimits/read" || len(raw) == 0 || len(raw) > maximumDiagnosticBytes || !utf8.Valid(raw) {
		return false
	}
	fields, err := decodeObject(raw, schema([]string{"code", "message"}, "code", "message", "data"))
	if err != nil {
		return false
	}
	var code int64
	if strictDecode(fields["code"], &code) != nil || code != -32603 {
		return false
	}
	// Pinned internal_error не содержит data. Неизвестное расширение ошибки
	// не доказывает отсутствие snapshots и сохраняет обычный закрытый отказ.
	if _, present := fields["data"]; present {
		return false
	}
	message, err := decodeBoundedString(fields["message"], maximumDiagnosticBytes)
	return err == nil && message == codexNoRateLimitSnapshots
}
