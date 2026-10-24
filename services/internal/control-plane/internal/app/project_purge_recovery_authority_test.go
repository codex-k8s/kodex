package app

import (
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/serviceidentity"
	"google.golang.org/grpc/metadata"
)

// TestProjectPurgeRecoveryTrustedRoutesRequireGatewayUser checks purge recovery query/resolve
// routes admit only the gateway user with exact operation/permission and no required signed
// project, denying agent/runtime/retention workloads.
// TestProjectPurgeRecoveryTrustedRoutesRequireGatewayUser проверяет допуск purge recovery query и
// resolve routes только gateway user с точными operation и permission без обязательного signed
// project и отказом agent, runtime и retention нагрузкам.
func TestProjectPurgeRecoveryTrustedRoutesRequireGatewayUser(t *testing.T) {
	authorizer, err := trustedClusterAuthorizer("trusted-cluster")
	if err != nil {
		t.Fatal(err)
	}
	for operation, method := range map[string]string{
		"platform.query.projects.purge-recovery.get":       cp.PlatformQueryService_GetProjectPurgeRecovery_FullMethodName,
		"platform.command.projects.purge-recovery.resolve": cp.PlatformCommandService_ResolveProjectPurgeRecovery_FullMethodName,
	} {
		for _, caller := range []string{"control-api-gateway", "agent-runner", "runtime-controller", "artifact-retention"} {
			ctx := metadata.NewIncomingContext(t.Context(), metadata.Pairs(serviceidentity.ProfileMetadataKey, "trusted-cluster", serviceidentity.CallerMetadataKey, "spiffe://kodex.local/ns/kodex-system/sa/"+caller))
			admission, err := authorizer.Admit(ctx, method)
			if caller == "control-api-gateway" {
				if err != nil || admission.ActorMode != serviceidentity.UserActor || admission.OperationID != operation || admission.Permission != operation || admission.ProjectRequired {
					t.Fatalf("owner recovery route missing: %s", operation)
				}
			} else if err == nil {
				t.Fatalf("unexpected recovery caller admitted: %s", caller)
			}
		}
	}
}
