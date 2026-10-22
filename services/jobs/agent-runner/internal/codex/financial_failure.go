package codex

import (
	"encoding/json"
)

// codexNoRateLimitSnapshots is the exact pinned internal-error message that can mean optional financial snapshots are unavailable; suffixes, other failures or data-bearing errors do not match it.
// codexNoRateLimitSnapshots — точное pinned internal-error message отсутствующих optional financial snapshots; suffixes, прочие failures и ошибки с data ему не соответствуют.
const codexNoRateLimitSnapshots = "failed to fetch codex rate limits: no snapshots returned"

// financialSnapshotsUnavailable recognizes only the correlated account/rateLimits/read internal-error envelope with absent data and the exact pinned no-snapshots message. It does not bypass authentication, policy or execution-limit failures, and never persists the source message.
// financialSnapshotsUnavailable распознаёт лишь коррелированный account/rateLimits/read internal-error envelope без data с точным pinned no-snapshots message. Не обходит authentication, policy и execution-limit failures и не сохраняет исходное message.
// Исторический источник прежнего сопоставления: a956835d020762cb2b570053af06f643a11c0ecc/account_processor.rs.
func financialSnapshotsUnavailable(method string, raw json.RawMessage) bool {
	if method != "account/rateLimits/read" {
		return false
	}
	fields, valid := decodeAccountRPCErrorFields(raw, -32603)
	if !valid {
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
