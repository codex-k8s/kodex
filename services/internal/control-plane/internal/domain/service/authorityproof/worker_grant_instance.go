package authorityproof

import "github.com/google/uuid"

// validWorkerGrantInstance preserves the existing v1 empty-instance rule and v2 canonical nonzero Pod
// UUID with positive issuance time and revision equal to Unix issuance seconds; it does not verify
// signature, workload or lifetime.
// validWorkerGrantInstance сохраняет существующее правило пустого экземпляра v1 и канонического
// ненулевого UUID Pod v2 с положительным временем выпуска и ревизией в Unix-секундах выпуска; подпись,
// нагрузка и срок здесь не проверяются.
func validWorkerGrantInstance(claims workerGrantClaims) bool {
	if claims.Version == 1 {
		return claims.InstanceID == ""
	}
	instance, err := uuid.Parse(claims.InstanceID)
	return claims.Version == 2 && err == nil && instance != uuid.Nil &&
		instance.String() == claims.InstanceID && claims.IssuedAt > 0 &&
		claims.Revision == uint64(claims.IssuedAt)
}
