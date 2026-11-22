package query

// KubernetesCaptureStatusRead identifies one owner-resolved capture and optionally the same
// actor's original repair key. No payload, task binding, tenant or actor is accepted here.
// KubernetesCaptureStatusRead указывает owner-resolved capture и при необходимости original
// repair key того же actor. Payload, task binding, tenant либо actor здесь не принимаются.
type KubernetesCaptureStatusRead struct {
	CaptureRef, RepairIdempotencyKey, CleanupIdempotencyKey string
}
