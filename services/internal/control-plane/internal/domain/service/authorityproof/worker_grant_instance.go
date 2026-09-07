package authorityproof

import "github.com/google/uuid"

// Instance назначается signer из Pod UID и проверяется после подписи workload.
// Старый формат сохраняет прежнюю общую границу revision.
func validWorkerGrantInstance(claims workerGrantClaims) bool {
	if claims.Version == 1 {
		return claims.InstanceID == ""
	}
	instance, err := uuid.Parse(claims.InstanceID)
	return claims.Version == 2 && err == nil && instance != uuid.Nil &&
		instance.String() == claims.InstanceID && claims.IssuedAt > 0 &&
		claims.Revision == uint64(claims.IssuedAt)
}
