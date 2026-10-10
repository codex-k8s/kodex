package authorityproof

import "testing"

func TestControllerAndUnknownWorkerGrantRemainClosedWithoutTrust(t *testing.T) {
	service := &Service{}
	for _, workload := range []string{"image-admission-controller", "unknown-worker", ""} {
		t.Run(workload, func(t *testing.T) {
			// Наличие producer/payload не заменяет зарегистрированный target trust.
			if _, err := service.verifyWorkerGrant("untrusted-envelope", proofProducer{CallerWorkloadID: workload}); err == nil {
				t.Fatal("unregistered worker grant must be rejected")
			}
		})
	}
}
