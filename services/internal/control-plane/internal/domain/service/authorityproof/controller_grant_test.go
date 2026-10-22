package authorityproof

import "testing"

// TestControllerAndUnknownWorkerGrantRemainClosedWithoutTrust requires controller/unknown/empty workload
// inputs to fail before an untrusted envelope can be interpreted without a registered trust key.
// TestControllerAndUnknownWorkerGrantRemainClosedWithoutTrust требует отказа для контроллера,
// неизвестной и пустой нагрузки до разбора недоверенного конверта без зарегистрированного ключа доверия.
func TestControllerAndUnknownWorkerGrantRemainClosedWithoutTrust(t *testing.T) {
	service := &Service{}
	for _, workload := range []string{"image-admission-controller", "unknown-worker", ""} {
		t.Run(workload, func(t *testing.T) {
			// Producer/payload presence cannot substitute for registered target trust.
			// Наличие producer/payload не заменяет зарегистрированное доверие цели.
			if _, err := service.verifyWorkerGrant("untrusted-envelope", proofProducer{CallerWorkloadID: workload}); err == nil {
				t.Fatal("unregistered worker grant must be rejected")
			}
		})
	}
}
