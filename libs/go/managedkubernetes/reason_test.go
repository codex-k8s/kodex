package managedkubernetes

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

// TestResourceQualificationReasonsUseTheOwnerReceiptRegistry preserves three explicit incomplete
// operator qualification states without accepting local aliases or arbitrary provider diagnostics.
// TestResourceQualificationReasonsUseTheOwnerReceiptRegistry сохраняет три explicit incomplete
// operator qualification states без local aliases либо arbitrary provider diagnostics.
func TestResourceQualificationReasonsUseTheOwnerReceiptRegistry(t *testing.T) {
	raw := []byte(`{}`)
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	for _, reason := range []string{ReasonTargetStoragePolicyRequired, ReasonControllerPolicyRequired, ReasonControllerRuntimeUnavailable, ReasonPortForwardProfileUnsupported, ReasonPortForwardCapacity} {
		if !ValidReasonCode(reason) || ValidateEffectResult(raw, digest, "FAILED", reason) != nil {
			t.Fatal("resource qualification reason failed the actual owner result validator")
		}
	}
	for _, reason := range []string{"TARGET_STORAGE_POLICY_REQUIRED", "CONTROLLER_POLICY_REQUIRED", "CONTROLLER_RUNTIME_UNAVAILABLE", "API_REJECTED", "private provider diagnostic"} {
		if ValidReasonCode(reason) || ValidateEffectResult(raw, digest, "FAILED", reason) == nil {
			t.Fatal("local/unregistered diagnostic escaped owner receipt classification")
		}
	}
}
