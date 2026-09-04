package app

import (
	"testing"

	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
)

func TestAuthorityProofProfileIncludesDirectOrganizationScopedSTT(t *testing.T) {
	const operation = "platform.stt.transcribe"
	sttOperations := controlplaneclient.STTGatewayOperations()
	proofOperations := authorityProofOperations()
	if proofOperations[operation] != sttOperations[operation] {
		t.Fatal("STT proof operation is not registered")
	}
	if _, required := authorityProjectRequiredOperations()[operation]; required {
		t.Fatal("organization STT requires an unrelated project")
	}
	if _, routedToControlPlane := controlplaneclient.ControlAPIGatewayOperations()[operation]; routedToControlPlane {
		t.Fatal("direct STT operation must not be routed to control-plane")
	}
}
