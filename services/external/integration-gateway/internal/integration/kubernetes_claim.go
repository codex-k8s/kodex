package integration

import (
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	sb "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	"google.golang.org/protobuf/proto"
)

// PrepareKubernetesInvocation copies the original common claim into a purpose-only request before the activity starts.
// It does not claim, renew or execute work; later calls must still pass ExecuteKubernetes and the owner's current admission.
// PrepareKubernetesInvocation копирует original common claim в purpose-only request перед запуском activity.
// Не выполняет claim, renew либо работу; последующие calls проходят ExecuteKubernetes и current admission владельца.
func PrepareKubernetesInvocation(claim *cp.IntegrationInvocationClaim, instance string) (Request, *sb.ManagedKubernetesInvocationBinding, error) {
	var empty Request
	reject := &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	if claim == nil || len(claim.ProtoReflect().GetUnknown()) != 0 || claim.GetCredentialRevision() != nil || claim.GetCredentialMaterializationRef() != "" ||
		len(claim.GetSshComposeSnapshot()) != 0 || len(claim.GetSshComposePermit()) != 0 ||
		claim.GetCredentialDeliveryKind() != cp.IntegrationCredentialDeliveryKind_INTEGRATION_CREDENTIAL_DELIVERY_KIND_BROKER_IDENTITY ||
		claim.GetBrokerIdentityPurpose() != cp.IntegrationBrokerIdentityPurpose_INTEGRATION_BROKER_IDENTITY_PURPOSE_KUBERNETES_TARGET ||
		!managedkubernetes.ValidLocator(claim.GetBrokerIdentityRef()) || claim.GetBrokerIdentityRevision() < 1 ||
		(claim.GetWorkMode() != cp.IntegrationInvocationWorkMode_INTEGRATION_INVOCATION_WORK_MODE_EXECUTE && claim.GetWorkMode() != cp.IntegrationInvocationWorkMode_INTEGRATION_INVOCATION_WORK_MODE_RECOVER_READ_ONLY) {
		return empty, nil, reject
	}
	lease := claim.GetLease()
	if lease == nil || len(lease.ProtoReflect().GetUnknown()) != 0 || lease.ExpiresAt == nil || lease.ExpiresAt.CheckValid() != nil ||
		len(lease.ExpiresAt.ProtoReflect().GetUnknown()) != 0 || !lease.ExpiresAt.AsTime().After(time.Now()) {
		return empty, nil, reject
	}
	binding := managedkubernetes.Binding{WorkloadInstance: instance, InvocationRef: claim.InvocationRef, LeaseRef: lease.Ref, Fence: lease.Fence, Generation: lease.Generation, InputDigest: claim.InputDigest}
	if binding.Validate() != nil {
		return empty, nil, reject
	}
	owned := proto.Clone(claim).(*cp.IntegrationInvocationClaim)
	request := RequestFromInvocation(owned)
	// Email binding is a generic mapper detail, not part of this closed broker purpose.
	// Email binding является деталью generic mapper, а не частью этого closed broker purpose.
	request.EmailExecution = nil
	wire := &sb.ManagedKubernetesInvocationBinding{WorkloadInstance: binding.WorkloadInstance, InvocationRef: binding.InvocationRef, LeaseRef: binding.LeaseRef, Fence: binding.Fence, Generation: binding.Generation, InputDigest: binding.InputDigest}
	return request, wire, nil
}
