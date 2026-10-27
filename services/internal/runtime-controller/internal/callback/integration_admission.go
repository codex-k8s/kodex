package callback

import (
	"errors"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
)

// integrationAdmissionCurrentDenied is a closed admission reason, not an external effect outcome.
// integrationAdmissionCurrentDenied задаёт closed admission reason, а не внешний effect outcome.
const integrationAdmissionCurrentDenied = "CURRENT_ADMISSION_DENIED"

// errIntegrationAdmissionMarkerInvalid hides malformed owner metadata without reflecting its bytes.
// errIntegrationAdmissionMarkerInvalid скрывает повреждённые owner metadata без отражения bytes.
var errIntegrationAdmissionMarkerInvalid = errors.New("integration admission marker is invalid")

// validIntegrationAdmissionMarker permits legacy absence or one exact closed admission refusal.
// A block is attention metadata, never a successful result or a replacement external outcome.
// validIntegrationAdmissionMarker допускает legacy absence либо один exact closed admission отказ.
// Block является attention metadata, а не успешным результатом или заменой внешнего outcome.
func validIntegrationAdmissionMarker(blocked bool, reason, state string) bool {
	if !blocked {
		return reason == ""
	}
	if reason != integrationAdmissionCurrentDenied {
		return false
	}
	switch state {
	case "READY", "UNKNOWN_OUTCOME", "FAILED", "CANCELLED":
		return true
	default:
		return false
	}
}

// ownerIntegrationAdmissionMarker translates the typed owner marker before state polling.
// READY cannot wait for a lease that admission has forbidden, while UNKNOWN remains unknown.
// ownerIntegrationAdmissionMarker преобразует typed owner marker до state polling.
// READY не ждёт запрещённого admission lease, а UNKNOWN остаётся неизвестным.
func ownerIntegrationAdmissionMarker(value *cp.GetIntegrationInvocationResponse) (bool, string, error) {
	if value == nil {
		return false, "", errIntegrationAdmissionMarkerInvalid
	}
	reason := ""
	switch value.GetAdmissionBlockReason() {
	case cp.SSHComposeAdmissionBlockReason_SSH_COMPOSE_ADMISSION_BLOCK_REASON_UNSPECIFIED:
	case cp.SSHComposeAdmissionBlockReason_SSH_COMPOSE_ADMISSION_BLOCK_REASON_CURRENT_ADMISSION_DENIED:
		reason = integrationAdmissionCurrentDenied
	default:
		return false, "", errIntegrationAdmissionMarkerInvalid
	}
	if !validIntegrationAdmissionMarker(value.GetAdmissionBlocked(), reason, value.GetState()) ||
		(value.GetAdmissionBlocked() && value.GetEffectReceiptRef() != "") {
		return false, "", errIntegrationAdmissionMarkerInvalid
	}
	return value.GetAdmissionBlocked(), reason, nil
}
