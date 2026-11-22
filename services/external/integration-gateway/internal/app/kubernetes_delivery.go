package app

import (
	"context"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/grpcserver"
	"google.golang.org/grpc"
)

// kubernetesDeliveryContract uses the protected typed owner client to verify common mixed-delivery compatibility.
// Optional target readiness is independent; this callback cannot authorize an effect or enable an incomplete consumer.
// kubernetesDeliveryContract проверяет common mixed-delivery compatibility через protected typed owner client.
// Optional target readiness независима; callback не разрешает effect и не включает неполный consumer.
func kubernetesDeliveryContract(owner cp.ManagedKubernetesOwnerServiceClient) func(context.Context) (uint32, error) {
	return func(ctx context.Context) (uint32, error) {
		if owner == nil || ctx == nil || ctx.Err() != nil {
			return 0, brokerIdentityWorkUnavailable
		}
		check, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		response, err := owner.CheckManagedKubernetesOwnerReadiness(check, &cp.CheckManagedKubernetesOwnerReadinessRequest{DeliveryContractOnly: true}, grpc.ForceCodec(grpcserver.StrictProtoCodec()))
		if err != nil || check.Err() != nil || response == nil || len(response.ProtoReflect().GetUnknown()) != 0 || response.DeliveryContractRevision != 1 {
			return 0, brokerIdentityWorkUnavailable
		}
		return response.DeliveryContractRevision, nil
	}
}
