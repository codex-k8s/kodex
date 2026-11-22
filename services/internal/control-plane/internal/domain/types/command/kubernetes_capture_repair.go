package command

// KubernetesCaptureRepair identifies only a retained capture; the server independently decides
// whether its current WAITING_OWNER cycle can be repaired. No caller outcome or reset count exists.
// KubernetesCaptureRepair указывает лишь retained capture; server независимо определяет, можно ли
// исправить current WAITING_OWNER cycle. Caller outcome либо reset count отсутствуют.
type KubernetesCaptureRepair struct {
	CaptureRef string
}
