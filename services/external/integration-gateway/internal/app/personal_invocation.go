package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/external/integration-gateway/internal/integration"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
)

// personalInvocationAdmissionClient exposes only the one-time owner call-start operation.
// personalInvocationAdmissionClient открывает только одноразовую owner operation начала вызова.
type personalInvocationAdmissionClient interface {
	// BeginPersonalIntegrationInvocation consumes a live owner lease before exactly one call start.
	// BeginPersonalIntegrationInvocation расходует live owner lease перед единственным началом вызова.
	BeginPersonalIntegrationInvocation(context.Context, *cp.BeginPersonalIntegrationInvocationRequest, ...grpc.CallOption) (*cp.BeginPersonalIntegrationInvocationResponse, error)
}

// invocationExecutionAdapter is the bounded existing execution/recovery boundary used by the loop.
// invocationExecutionAdapter — bounded граница execution/recovery, используемая рабочим циклом.
type invocationExecutionAdapter interface {
	// Execute dispatches only the validated operation and returns its safe effect receipt.
	// Execute выполняет лишь проверенную операцию и возвращает безопасный effect receipt.
	Execute(context.Context, integration.Request) (integration.Result, error)
	// Recover rejoins an uncertain effect through supported readback, never blindly replaying WRITE.
	// Recover согласует неизвестный эффект поддержанным readback без слепого повтора WRITE.
	Recover(context.Context, integration.Request) (integration.Result, error)
}

// invocationExecution records completed adapter work separately from owner/reporting errors.
// invocationExecution отделяет выполненную работу adapter от owner/reporting errors.
type invocationExecution struct {
	claim        *cp.IntegrationInvocationClaim
	result       integration.Result
	operationErr error
	executed     bool
}

// executeInvocationClaim performs no external request until personal admission succeeds once.
// A denial, lost ACK or invalid reply produces no execution/completion fact; the owner lease
// lifecycle retains uncertainty. Executed adapter errors remain observable before completion.
// executeInvocationClaim не обращается наружу до единственного успешного personal admission.
// Отказ, потерянный ACK и неверный ответ не создают execution/completion fact; неопределённость
// сохраняет owner lease lifecycle. Ошибки выполненного adapter наблюдаются до completion.
func executeInvocationClaim(ctx context.Context, owner personalInvocationAdmissionClient, adapter invocationExecutionAdapter, claim *cp.IntegrationInvocationClaim, config Config) (invocationExecution, error) {
	var empty invocationExecution
	lease := claim.GetLease()
	budget := config.OperationTimeout + config.RequestTimeout
	if claim.GetCredentialAdmissionRequired() {
		budget += config.RequestTimeout
	}
	if claim == nil || lease == nil || lease.GetExpiresAt() == nil || lease.GetExpiresAt().CheckValid() != nil ||
		lease.GetExpiresAt().AsTime().Before(time.Now().Add(budget)) {
		return empty, errors.New("integration lease budget is insufficient")
	}
	work, err := admitPersonalInvocation(ctx, owner, claim, config)
	if err != nil || work == nil {
		return empty, err
	}
	operation, cancel := context.WithTimeout(ctx, config.OperationTimeout)
	defer cancel()
	executed := invocationExecution{claim: work, executed: true}
	if work.GetWorkMode() == cp.IntegrationInvocationWorkMode_INTEGRATION_INVOCATION_WORK_MODE_RECOVER_READ_ONLY {
		executed.result, executed.operationErr = adapter.Recover(operation, integration.RequestFromInvocation(work))
	} else if work.GetWorkMode() == cp.IntegrationInvocationWorkMode_INTEGRATION_INVOCATION_WORK_MODE_EXECUTE {
		executed.result, executed.operationErr = adapter.Execute(operation, integration.RequestFromInvocation(work))
	} else {
		return empty, errors.New("integration invocation work mode is invalid")
	}
	return executed, nil
}

// admitPersonalInvocation retains shared claims and obtains a personal descriptor only once.
// Denial returns no work; an error, lost ACK or malformed reply never calls an adapter or retries
// Begin. Claim and response are cloned so their immutable owner snapshots are not overwritten.
// admitPersonalInvocation сохраняет shared claims и получает personal descriptor лишь один раз.
// Отказ не выдаёт работу; ошибка, потерянный ACK и malformed reply не вызывают adapter и не
// повторяют Begin. Claim/response копируются, не переписывая immutable owner snapshots.
func admitPersonalInvocation(ctx context.Context, client personalInvocationAdmissionClient, claim *cp.IntegrationInvocationClaim, config Config) (*cp.IntegrationInvocationClaim, error) {
	if claim == nil {
		return nil, errors.New("integration invocation claim is missing")
	}
	if !validPersonalResourcePermissionProfile(claim) {
		return nil, errors.New("personal invocation resource permission profile is invalid")
	}
	if !claim.GetCredentialAdmissionRequired() {
		return claim, nil
	}
	lease := claim.GetLease()
	if claim.GetCredentialRevision() != nil || claim.GetCredentialMaterializationRef() != "" ||
		len(claim.ProtoReflect().GetUnknown()) != 0 || lease == nil || lease.GetRef() == "" || lease.GetFence() == "" ||
		lease.GetGeneration() < 1 || lease.GetExpiresAt() == nil || lease.GetExpiresAt().CheckValid() != nil ||
		lease.GetExpiresAt().AsTime().Before(time.Now().Add(config.OperationTimeout+2*config.RequestTimeout)) ||
		(claim.GetWorkMode() != cp.IntegrationInvocationWorkMode_INTEGRATION_INVOCATION_WORK_MODE_EXECUTE &&
			claim.GetWorkMode() != cp.IntegrationInvocationWorkMode_INTEGRATION_INVOCATION_WORK_MODE_RECOVER_READ_ONLY) {
		return nil, errors.New("personal invocation claim admission boundary is invalid")
	}
	request, cancel := context.WithTimeout(ctx, config.RequestTimeout)
	response, err := client.BeginPersonalIntegrationInvocation(request, &cp.BeginPersonalIntegrationInvocationRequest{
		InvocationRef: claim.GetInvocationRef(), LeaseRef: lease.GetRef(), Fence: lease.GetFence(),
		Generation: lease.GetGeneration(), WorkloadInstance: config.InstanceID,
	})
	cancel()
	if err != nil {
		return nil, err
	}
	if response == nil || len(response.ProtoReflect().GetUnknown()) != 0 {
		return nil, errors.New("personal invocation admission response is invalid")
	}
	if !response.GetAdmitted() {
		if response.GetCredentialRevision() != nil || response.GetWorkMode() != cp.IntegrationInvocationWorkMode_INTEGRATION_INVOCATION_WORK_MODE_UNSPECIFIED || response.GetResourcePermissionProfile() != 0 {
			return nil, errors.New("personal invocation denial contains work authority")
		}
		return nil, nil
	}
	credential := response.GetCredentialRevision()
	if response.GetWorkMode() != claim.GetWorkMode() || response.GetResourcePermissionProfile() != claim.GetResourcePermissionProfile() || credential == nil || len(credential.ProtoReflect().GetUnknown()) != 0 ||
		credential.GetRef() == "" || credential.GetRevision() < 1 || credential.GetSecretRef() == "" || credential.GetSecretUid() == "" ||
		credential.GetSecretResourceVersion() == "" || len(credential.GetContentSha256()) != 64 ||
		credential.GetCreatedAt() == nil || credential.GetCreatedAt().CheckValid() != nil ||
		!lease.GetExpiresAt().AsTime().After(time.Now().Add(config.OperationTimeout+config.RequestTimeout)) {
		return nil, errors.New("personal invocation admitted descriptor is invalid")
	}
	work := proto.Clone(claim).(*cp.IntegrationInvocationClaim)
	work.CredentialRevision = proto.Clone(credential).(*cp.IntegrationCredentialRevision)
	return work, nil
}

// validPersonalResourcePermissionProfile requires a known exact object-check profile for personal
// registered object READs/writes. Shared calls retain their contract; version skew never drops the check.
// validPersonalResourcePermissionProfile требует известный exact object-check профиль personal
// зарегистрированных object READ/записей. Shared сохраняет контракт; version skew не отбрасывает проверку.
func validPersonalResourcePermissionProfile(claim *cp.IntegrationInvocationClaim) bool {
	expected := cp.IntegrationCredentialResourcePermissionProfile_INTEGRATION_CREDENTIAL_RESOURCE_PERMISSION_PROFILE_UNSPECIFIED
	profile := strings.TrimPrefix(claim.GetResourcePermissionProfile().String(), "INTEGRATION_CREDENTIAL_RESOURCE_PERMISSION_PROFILE_")
	contract, registered := integrationpackage.CredentialResourcePermissionContractForProfile(claim.GetDefinitionKey(), claim.GetCapabilityKey(), profile)
	if claim.GetCredentialAdmissionRequired() && registered {
		if claim.GetOperation() != contract.Operation || strings.TrimPrefix(claim.GetRisk().String(), "INTEGRATION_RISK_") != contract.Risk ||
			!personalContractAllowsClaimPolicy(contract, claim) ||
			strings.TrimPrefix(claim.GetResourceScope().GetKind().String(), "INTEGRATION_RESOURCE_KIND_") != contract.ResourceKind {
			return false
		}
		value, exists := cp.IntegrationCredentialResourcePermissionProfile_value["INTEGRATION_CREDENTIAL_RESOURCE_PERMISSION_PROFILE_"+contract.Profile]
		if !exists || value == 0 {
			return false
		}
		expected = cp.IntegrationCredentialResourcePermissionProfile(value)
	}
	return claim.GetResourcePermissionProfile() == expected
}

// personalContractAllowsClaimPolicy retains exact legacy policy while the scoped successor accepts a known selected policy later verified against the exact package/grant.
// personalContractAllowsClaimPolicy сохраняет exact legacy policy, а scoped successor допускает известную selected policy с последующей проверкой exact package/grant.
func personalContractAllowsClaimPolicy(contract integrationpackage.CredentialResourcePermissionContract, claim *cp.IntegrationInvocationClaim) bool {
	policy := strings.TrimPrefix(claim.GetApprovalPolicy().String(), "INTEGRATION_APPROVAL_POLICY_")
	if policy == contract.ApprovalPolicy {
		return true
	}
	if policy != "NONE" && policy != "HUMAN_EACH_EFFECT" && policy != "HUMAN_SCOPED" {
		return false
	}
	definition, err := integrationpackage.Parse(claim.GetDefinitionPackage())
	if err != nil || definition.Metadata.Key != claim.GetDefinitionKey() || definition.Metadata.Version != claim.GetDefinitionVersion() || definition.Digest != claim.GetDefinitionDigest() {
		return false
	}
	capability, ok := definition.Capability(claim.GetCapabilityKey())
	if !ok || capability.ApprovalScope == nil || integrationpackage.CredentialResourcePermissionProfile(definition, capability) != contract.Profile {
		return false
	}
	raw, err := json.Marshal(claim.GetBoundedInput().AsMap())
	return err == nil && definition.ValidateInvocationApprovalPolicy(capability, policy, raw) == nil
}
