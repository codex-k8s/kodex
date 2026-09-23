package app

import (
	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/serviceidentity"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/transportprofile"
	sttv1 "github.com/codex-k8s/kodex/libs/go/sttapi/gen/stt/v1"
	"google.golang.org/grpc/metadata"
	"testing"
)

func TestTrustedSTTAuthorityRoutesAreExplicitAndCallerBound(t *testing.T) {
	authorizer, err := trustedClusterAuthorizer(transportprofile.TrustedCluster)
	if err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{
		sttv1.TranscriptionAuthorityService_ResolveTranscriptionAuthority_FullMethodName,
		sttv1.TranscriptionAuthorityService_ResolveTranscriptionCatalogAuthority_FullMethodName,
		sttv1.TranscriptionPolicyProjectionService_ResolveTranscriptionPolicy_FullMethodName,
	} {
		for _, caller := range []string{"stt-tts-service", "control-api-gateway", "email-bridge"} {
			ctx := metadata.NewIncomingContext(t.Context(), metadata.Pairs(serviceidentity.ProfileMetadataKey, transportprofile.TrustedCluster,
				serviceidentity.CallerMetadataKey, "spiffe://kodex.local/ns/kodex-system/sa/"+caller))
			admission, err := authorizer.Admit(ctx, method)
			if caller == "stt-tts-service" {
				if err != nil || admission.ActorMode != serviceidentity.UserActor || admission.ProjectRequired {
					t.Fatal("org-only trusted STT route missing")
				}
			} else if err == nil {
				t.Fatal("unexpected caller admitted")
			}
		}
	}
	if _, err := trustedClusterAuthorizer("service-v1"); err == nil {
		t.Fatal("implicit trusted profile accepted")
	}
}

func TestTrustedImageAdmissionControllerRouteIsReadOnlyAndCallerBound(t *testing.T) {
	authorizer, err := trustedClusterAuthorizer(transportprofile.TrustedCluster)
	if err != nil {
		t.Fatal(err)
	}
	for _, caller := range []string{"image-admission-controller", "image-admission", "role-image-builder"} {
		ctx := metadata.NewIncomingContext(t.Context(), metadata.Pairs(
			serviceidentity.ProfileMetadataKey, transportprofile.TrustedCluster,
			serviceidentity.CallerMetadataKey, "spiffe://kodex.local/ns/kodex-system/sa/"+caller,
		))
		admission, admitErr := authorizer.Admit(ctx, controlplanev1.RoleImageService_GetImageSupplyWorkAvailability_FullMethodName)
		if caller == "image-admission-controller" {
			if admitErr != nil || admission.ActorMode != serviceidentity.ServiceActor || admission.ProjectRequired ||
				admission.Permission != "platform.role-images.supply-work.get" {
				t.Fatalf("trusted work availability route missing: admission=%#v err=%v", admission, admitErr)
			}
		} else if admitErr == nil {
			t.Fatalf("unexpected caller %s admitted", caller)
		}
	}
}
