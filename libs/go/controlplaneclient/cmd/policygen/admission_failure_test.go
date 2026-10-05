package main

import (
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"testing"
)

func TestAdmissionTechnicalCommandsBindCanonicalExactTuple(t *testing.T) {
	for _, operation := range []string{"platform.role-images.admission.fail", "platform.role-images.admission.expire"} {
		method := controlplaneclient.ImageAdmissionOperations()[operation]
		want := requestProfile{Mode: "UNARY_PROTO_SHA256", Resource: "FORBIDDEN", Version: "FORBIDDEN", Attempt: "FORBIDDEN", Idempotency: "FORBIDDEN"}
		if method == "" || operationRequestProfile(operation, method) != want {
			t.Fatal("technical failure canonical request profile missing")
		}
		for _, profile := range []map[string]string{controlplaneclient.ImageAdmissionControllerOperations(), controlplaneclient.RoleImageBuilderOperations(), controlplaneclient.ImagePromotionOperations(), controlplaneclient.ControlAPIGatewayOperations()} {
			if profile[operation] != "" {
				t.Fatal("technical admission command leaked to another workload")
			}
		}
	}
}
