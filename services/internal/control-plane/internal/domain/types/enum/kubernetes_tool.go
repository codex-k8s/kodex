package enum

// KubernetesToolState includes pre-claim states without granting private execution authority.
// KubernetesToolState включает состояния до claim, не выдавая полномочий на исполнение.
type KubernetesToolState string

// Known rejects unregistered owner states instead of presenting them as completed.
// Known отклоняет незарегистрированные состояния, не представляя их завершёнными.
func (state KubernetesToolState) Known() bool {
	switch state {
	case "READY", "WAITING_APPROVAL", "RUNNING", "SUCCEEDED", "FAILED", "REJECTED", "CANCELLED", "UNKNOWN_OUTCOME":
		return true
	default:
		return false
	}
}

// Terminal recognizes only closed owner states; it proves neither native join nor remote rollback.
// Terminal распознаёт только закрытые состояния владельца, не доказывая native join или откат внешнего действия.
func (state KubernetesToolState) Terminal() bool {
	switch state {
	case "SUCCEEDED", "FAILED", "REJECTED", "CANCELLED", "UNKNOWN_OUTCOME":
		return true
	default:
		return false
	}
}
