package serviceidentity

import (
	"os"
	"testing"
)

func TestGeneratedControlPlanePolicyLoadsInActualAuthorizer(t *testing.T) {
	raw, err := os.ReadFile("../../../../services/internal/control-plane/internal/app/service-identity-policy.json")
	if err != nil {
		t.Fatal(err)
	}
	authorizer, digest, err := FromPolicy(raw, target, &revocationCheck{})
	if err != nil || len(digest) != 64 || len(authorizer.bindings) != 375 {
		t.Fatal("generated policy rejected")
	}
	for _, binding := range authorizer.bindings {
		if binding.OperationID == "platform.stt.policy.resolve" {
			t.Fatal("delegation downgraded")
		}
	}
}

func TestPolicyRejectsDuplicateAndUnknownFields(t *testing.T) {
	for _, raw := range []string{
		`{"version":1,"version":1,"target_spiffe_id":"` + target + `","bindings":[]}`,
		`{"version":1,"target_spiffe_id":"` + target + `","bindings":[],"allow_all":true}`,
	} {
		if _, _, err := FromPolicy([]byte(raw), target, &revocationCheck{}); err == nil {
			t.Fatal("malformed policy accepted")
		}
	}
}
