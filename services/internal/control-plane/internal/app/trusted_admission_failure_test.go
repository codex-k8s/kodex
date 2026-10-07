package app

import (
	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/serviceidentity"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/transportprofile"
	"google.golang.org/grpc/metadata"
	"testing"
)

func TestTrustedAdmissionTechnicalCommandsAreExactAndLeastPrivilege(t *testing.T) {
	authorizer, err := trustedClusterAuthorizer(transportprofile.TrustedCluster)
	if err != nil {
		t.Fatal(err)
	}
	for method, permission := range map[string]string{cp.RoleImageService_FailImageAdmission_FullMethodName: "platform.role-images.admission.fail", cp.RoleImageService_ExpireImageAdmissionClaim_FullMethodName: "platform.role-images.admission.expire", cp.RoleImageService_GetImageAdmissionTerminal_FullMethodName: "platform.role-images.admission.terminal.get"} {
		for _, caller := range []string{"image-admission", "image-admission-controller", "role-image-builder", "image-promotion", "control-api-gateway"} {
			ctx := metadata.NewIncomingContext(t.Context(), metadata.Pairs(serviceidentity.ProfileMetadataKey, transportprofile.TrustedCluster, serviceidentity.CallerMetadataKey, "spiffe://kodex.local/ns/kodex-system/sa/"+caller))
			admission, err := authorizer.Admit(ctx, method)
			if caller == "image-admission" {
				if err != nil || admission.ActorMode != serviceidentity.ServiceActor || admission.ProjectRequired || admission.Permission != permission {
					t.Fatal("exact admission technical route unavailable")
				}
			} else if err == nil {
				t.Fatal("technical admission permission leaked to another workload")
			}
		}
	}
}

func TestTrustedAdmissionRecoveryTerminalIsControllerReadOnly(t *testing.T) {
	authorizer, err := trustedClusterAuthorizer(transportprofile.TrustedCluster)
	if err != nil {
		t.Fatal(err)
	}
	for _, caller := range []string{"image-admission-controller", "image-admission", "role-image-builder", "image-promotion", "control-api-gateway"} {
		ctx := metadata.NewIncomingContext(t.Context(), metadata.Pairs(serviceidentity.ProfileMetadataKey, transportprofile.TrustedCluster, serviceidentity.CallerMetadataKey, "spiffe://kodex.local/ns/kodex-system/sa/"+caller))
		admission, err := authorizer.Admit(ctx, cp.RoleImageService_GetImageAdmissionRecoveryTerminal_FullMethodName)
		if caller == "image-admission-controller" {
			if err != nil || admission.ActorMode != serviceidentity.ServiceActor || admission.ProjectRequired || admission.Permission != "platform.role-images.admission.recovery-terminal.get" {
				t.Fatal("closed controller recovery route unavailable")
			}
		} else if err == nil {
			t.Fatal("controller terminal read leaked to another caller")
		}
	}
}
