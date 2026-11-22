package app

import (
	"context"
	"errors"
	"strings"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/grpcserver"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
)

// errBrokerIdentityDeliveryUnresolved stops further claiming after unknown owner writes or malformed successful batches; it never declares those leases absent, expired or joined.
// errBrokerIdentityDeliveryUnresolved останавливает новые claim после unknown owner writes либо malformed successful batches; leases не объявляются absent, expired либо joined.
var errBrokerIdentityDeliveryUnresolved = errors.New("integration invocation delivery is unresolved")

// integrationInvocationClaimer exposes only the existing owner claim facade; capacity reservations neither create a second selector nor change the transport principal.
// integrationInvocationClaimer открывает лишь existing owner claim facade; capacity reservations не создают второй selector и не меняют transport principal.
type integrationInvocationClaimer interface {
	// ClaimIntegrationInvocations atomically assigns original owner leases under the advertised free capacity; it performs no provider request.
	// ClaimIntegrationInvocations атомарно назначает original owner leases в пределах advertised free capacity без provider request.
	ClaimIntegrationInvocations(context.Context, *cp.ClaimIntegrationInvocationsRequest, ...grpc.CallOption) (*cp.ClaimIntegrationInvocationsResponse, error)
}

// brokerClaimReservations owns every pre-claim slot until a single adoption or final unused release; the original owner claim remains the only source of authority.
// brokerClaimReservations владеет каждым pre-claim slot до single adoption либо final unused release; original owner claim остаётся единственным authority source.
type brokerClaimReservations struct {
	capacity *cp.IntegrationInvocationDeliveryCapacity
	slots    map[cp.IntegrationBrokerIdentityPurpose][]brokerIdentityClaimSlot
}

// releaseUnused is safe after successful adoption because the slot contract forbids releasing an active activity; after uncertainty admission is closed before this method runs.
// releaseUnused безопасен после successful adoption: slot contract запрещает освобождение active activity; при uncertainty admission закрывается до вызова.
func (reservations brokerClaimReservations) releaseUnused() {
	for _, slots := range reservations.slots {
		for _, slot := range slots {
			slot.ReleaseUnused()
		}
	}
}

// checkBrokerIdentityDelivery runs at startup before Listen and before each mixed claim; unsupported or missing proof never silently switches a configured mixed worker to ordinary mode.
// checkBrokerIdentityDelivery выполняется при startup до Listen и перед каждым mixed claim; unsupported либо missing proof не переключает configured mixed worker в ordinary mode.
func checkBrokerIdentityDelivery(ctx context.Context, consumers *brokerIdentityConsumers) error {
	if ctx == nil || ctx.Err() != nil {
		return brokerIdentityWorkUnavailable
	}
	if consumers == nil {
		return nil
	}
	if consumers.deliveryContract == nil {
		for _, consumer := range brokerIdentityConsumerSet(consumers) {
			slot, available := consumer.ReserveClaim()
			if slot != nil {
				slot.ReleaseUnused()
			}
			if available || slot != nil {
				return brokerIdentityWorkUnavailable
			}
		}
		return nil
	}
	revision, err := consumers.deliveryContract(ctx)
	if err != nil || ctx.Err() != nil || revision != 1 {
		return brokerIdentityWorkUnavailable
	}
	return nil
}

// reserveBrokerIdentityClaims fairly rotates the three fixed delivery classes, allocating at most the requested total and four slots per BI purpose; unavailable purpose capacity goes to ordinary work.
// reserveBrokerIdentityClaims по очереди обслуживает три fixed delivery classes, выделяя не более requested total и четырёх slots каждому BI purpose; unavailable purpose capacity передаётся ordinary work.
func reserveBrokerIdentityClaims(ctx context.Context, consumers *brokerIdentityConsumers, limit int32) (brokerClaimReservations, error) {
	reservations := brokerClaimReservations{slots: make(map[cp.IntegrationBrokerIdentityPurpose][]brokerIdentityClaimSlot)}
	if ctx == nil || ctx.Err() != nil || limit < 1 || limit > 32 {
		return reservations, brokerIdentityWorkUnavailable
	}
	if consumers == nil {
		return reservations, nil
	}
	consumers.claimMutex.Lock()
	closed, turn := consumers.claimClosed, consumers.claimTurn
	consumers.claimTurn = (consumers.claimTurn + 1) % 3
	consumers.claimMutex.Unlock()
	if closed {
		return reservations, errBrokerIdentityDeliveryUnresolved
	}
	if err := checkBrokerIdentityDelivery(ctx, consumers); err != nil {
		return reservations, err
	}
	if consumers.deliveryContract == nil {
		return reservations, nil
	}
	reservations.capacity = &cp.IntegrationInvocationDeliveryCapacity{}
	purposes := []cp.IntegrationBrokerIdentityPurpose{0, cp.IntegrationBrokerIdentityPurpose_INTEGRATION_BROKER_IDENTITY_PURPOSE_KUBERNETES_TARGET, cp.IntegrationBrokerIdentityPurpose_INTEGRATION_BROKER_IDENTITY_PURPOSE_SSH_TARGET}
	unavailable := [3]bool{}
	for allocated := int32(0); allocated < limit; {
		index := int(turn)
		turn = (turn + 1) % 3
		if index == 0 {
			reservations.capacity.Ordinary++
			allocated++
			continue
		}
		purpose := purposes[index]
		if unavailable[index] || len(reservations.slots[purpose]) == 4 {
			continue
		}
		consumer := consumers.forPurpose(purpose)
		if consumer == nil {
			unavailable[index] = true
			continue
		}
		slot, available := consumer.ReserveClaim()
		if !available || slot == nil {
			if slot != nil {
				slot.ReleaseUnused()
			}
			unavailable[index] = true
			continue
		}
		reservations.slots[purpose] = append(reservations.slots[purpose], slot)
		if index == 1 {
			reservations.capacity.Kubernetes++
		} else {
			reservations.capacity.Ssh++
		}
		allocated++
	}
	return reservations, nil
}

// closeUnresolvedBrokerDelivery permanently closes this claimant before releasing unused local resources, cancelling known activities without inventing authority to recover unseen owner leases.
// closeUnresolvedBrokerDelivery навсегда закрывает claimant до освобождения unused local resources и отменяет known activities без выдуманной authority восстановления unseen owner leases.
func closeUnresolvedBrokerDelivery(consumers *brokerIdentityConsumers) error {
	result := errBrokerIdentityDeliveryUnresolved
	if consumers == nil {
		return result
	}
	consumers.claimMutex.Lock()
	consumers.claimClosed = true
	consumers.claimMutex.Unlock()
	for _, consumer := range brokerIdentityConsumerSet(consumers) {
		if consumer.Close() != nil {
			result = errors.Join(result, brokerIdentityWorkUnavailable)
		}
	}
	return result
}

// claimIntegrationInvocationDelivery sends one owner Claim after reservation, validates the entire batch before any adoption, then starts BI activities before returning ordinary synchronous work. An uncertain acknowledgement is fatal, not a polling retry.
// claimIntegrationInvocationDelivery выполняет один owner Claim после reservation, проверяет весь batch до adoption и запускает BI activities до возврата ordinary synchronous work. Uncertain acknowledgement является fatal, а не polling retry.
func claimIntegrationInvocationDelivery(ctx context.Context, owner integrationInvocationClaimer, consumers *brokerIdentityConsumers, instance string, limit int32, record func(brokerIdentityInvocationProgress)) ([]*cp.IntegrationInvocationClaim, int, error) {
	if owner == nil || record == nil || !brokerWorkReference(instance) {
		return nil, 0, brokerIdentityWorkUnavailable
	}
	reservations, err := reserveBrokerIdentityClaims(ctx, consumers, limit)
	if err != nil {
		return nil, 0, err
	}
	defer reservations.releaseUnused()
	if ctx.Err() != nil {
		return nil, 0, ctx.Err()
	}
	reply, err := owner.ClaimIntegrationInvocations(ctx, &cp.ClaimIntegrationInvocationsRequest{WorkloadInstance: instance, Limit: limit, Capacity: reservations.capacity}, grpc.ForceCodec(grpcserver.StrictProtoCodec()))
	if err != nil || reply == nil || len(reply.ProtoReflect().GetUnknown()) != 0 || len(reply.Claims) > int(limit) {
		return nil, 0, closeUnresolvedBrokerDelivery(consumers)
	}
	// Keep an owned strict copy for local adapters as well; the RPC codec rejects duplicate wire fields before generated decoding can erase them.
	// Сохраняем owned strict copy также для local adapters; RPC codec отвергает duplicate wire fields до потери сведений при generated decoding.
	codec := grpcserver.StrictProtoCodec()
	raw, marshalErr := codec.Marshal(reply)
	if marshalErr != nil {
		return nil, 0, closeUnresolvedBrokerDelivery(consumers)
	}
	var owned cp.ClaimIntegrationInvocationsResponse
	decodeErr := codec.Unmarshal(raw, &owned)
	clear(raw)
	if decodeErr != nil || grpcserver.HasMalformedProto(&owned) {
		return nil, 0, closeUnresolvedBrokerDelivery(consumers)
	}
	reply = &owned
	counts := map[cp.IntegrationBrokerIdentityPurpose]int{}
	ordinaryCount := 0
	seenRefs, seenLeases := map[string]bool{}, map[string]bool{}
	for _, claim := range reply.Claims {
		purpose, err := invocationDeliveryPurpose(claim)
		if err != nil || seenRefs[claim.GetInvocationRef()] || seenLeases[claim.GetLease().GetRef()] {
			return nil, 0, closeUnresolvedBrokerDelivery(consumers)
		}
		seenRefs[claim.InvocationRef], seenLeases[claim.Lease.Ref] = true, true
		if purpose == 0 {
			ordinaryCount++
		} else {
			counts[purpose]++
		}
	}
	if reservations.capacity == nil {
		if len(counts) != 0 {
			return nil, 0, closeUnresolvedBrokerDelivery(consumers)
		}
	} else if ordinaryCount > int(reservations.capacity.Ordinary) {
		return nil, 0, closeUnresolvedBrokerDelivery(consumers)
	}
	for purpose, count := range counts {
		if count > len(reservations.slots[purpose]) {
			return nil, 0, closeUnresolvedBrokerDelivery(consumers)
		}
	}
	ordinary := make([]*cp.IntegrationInvocationClaim, 0, ordinaryCount)
	adopted := 0
	for _, claim := range reply.Claims {
		purpose := claim.GetBrokerIdentityPurpose()
		if purpose == 0 {
			ordinary = append(ordinary, claim)
			continue
		}
		slot := reservations.slots[purpose][0]
		reservations.slots[purpose] = reservations.slots[purpose][1:]
		progress, err := slot.ExecuteClaim(ctx, proto.Clone(claim).(*cp.IntegrationInvocationClaim))
		if progress.Attempted {
			record(progress)
		}
		// Keep the slot in the final release set even if adoption returned an uncertain local acknowledgement.
		// Сохраняем slot в final release set даже при uncertain local acknowledgement adoption.
		defer slot.ReleaseUnused()
		if err != nil || !validBrokerInvocationProgress(claim, progress) {
			return nil, adopted, closeUnresolvedBrokerDelivery(consumers)
		}
		adopted++
	}
	return ordinary, adopted, nil
}

// invocationDeliveryPurpose validates only representation and original metadata, not effect authority; every BI consumer still obtains its own fresh owner admission.
// invocationDeliveryPurpose проверяет лишь representation и original metadata, не effect authority; каждый BI consumer получает собственный fresh owner admission.
func invocationDeliveryPurpose(claim *cp.IntegrationInvocationClaim) (cp.IntegrationBrokerIdentityPurpose, error) {
	if claim == nil || len(claim.ProtoReflect().GetUnknown()) != 0 || !brokerWorkReference(claim.InvocationRef) || !workInputDigest(claim.InputDigest) {
		return 0, brokerIdentityWorkUnavailable
	}
	lease := claim.Lease
	if lease == nil || len(lease.ProtoReflect().GetUnknown()) != 0 || !brokerWorkReference(lease.Ref) || !brokerWorkReference(lease.Fence) || lease.Generation < 1 || lease.Generation > 9007199254740991 ||
		lease.ExpiresAt == nil || lease.ExpiresAt.CheckValid() != nil || len(lease.ExpiresAt.ProtoReflect().GetUnknown()) != 0 || !lease.ExpiresAt.AsTime().After(time.Now()) ||
		(claim.WorkMode != cp.IntegrationInvocationWorkMode_INTEGRATION_INVOCATION_WORK_MODE_EXECUTE && claim.WorkMode != cp.IntegrationInvocationWorkMode_INTEGRATION_INVOCATION_WORK_MODE_RECOVER_READ_ONLY) {
		return 0, brokerIdentityWorkUnavailable
	}
	definition, err := integrationpackage.Parse(claim.DefinitionPackage)
	delivery, known := definition.ConnectionCredentialDelivery()
	capability, exists := definition.Capability(claim.CapabilityKey)
	if err != nil || !known || !exists || definition.Metadata.Key != claim.DefinitionKey || definition.Metadata.Version != claim.DefinitionVersion || definition.Digest != claim.DefinitionDigest ||
		definition.Spec.AdapterOwner != "integration-gateway" || definition.Spec.ExecutionRoute != "MANAGED_MCP" || capability.Operation != claim.Operation || capability.Risk != strings.TrimPrefix(claim.Risk.String(), "INTEGRATION_RISK_") || !capability.AllowsApprovalPolicy(strings.TrimPrefix(claim.ApprovalPolicy.String(), "INTEGRATION_APPROVAL_POLICY_")) {
		return 0, brokerIdentityWorkUnavailable
	}
	broker := claim.BrokerIdentityPurpose != 0 || claim.BrokerIdentityRef != "" || claim.BrokerIdentityRevision != 0
	switch claim.CredentialDeliveryKind {
	case cp.IntegrationCredentialDeliveryKind_INTEGRATION_CREDENTIAL_DELIVERY_KIND_NONE:
		if delivery.Kind != integrationpackage.CredentialDeliveryNone || broker || claim.CredentialRevision != nil || claim.CredentialMaterializationRef != "" || claim.CredentialAdmissionRequired {
			return 0, brokerIdentityWorkUnavailable
		}
	case cp.IntegrationCredentialDeliveryKind_INTEGRATION_CREDENTIAL_DELIVERY_KIND_TOKEN:
		if delivery.Kind != integrationpackage.CredentialDeliveryToken || broker || claim.CredentialAdmissionRequired && (claim.CredentialRevision != nil || claim.CredentialMaterializationRef != "") ||
			!claim.CredentialAdmissionRequired && (claim.CredentialRevision == nil) == (claim.CredentialMaterializationRef == "") {
			return 0, brokerIdentityWorkUnavailable
		}
	case cp.IntegrationCredentialDeliveryKind_INTEGRATION_CREDENTIAL_DELIVERY_KIND_BROKER_IDENTITY:
		purpose := strings.TrimPrefix(claim.BrokerIdentityPurpose.String(), "INTEGRATION_BROKER_IDENTITY_PURPOSE_")
		if delivery.Kind != integrationpackage.CredentialDeliveryBrokerIdentity || delivery.BrokerPurpose != purpose || (purpose != "KUBERNETES_TARGET" && purpose != "SSH_TARGET") ||
			!brokerWorkReference(claim.BrokerIdentityRef) || claim.BrokerIdentityRevision < 1 || claim.BrokerIdentityRevision > 9007199254740991 || claim.CredentialRevision != nil || claim.CredentialMaterializationRef != "" {
			return 0, brokerIdentityWorkUnavailable
		}
		expectedProfile := cp.IntegrationCredentialResourcePermissionProfile_INTEGRATION_CREDENTIAL_RESOURCE_PERMISSION_PROFILE_SSH_SCOPED_RESOURCE_V1
		if purpose == "KUBERNETES_TARGET" {
			expectedProfile = cp.IntegrationCredentialResourcePermissionProfile_INTEGRATION_CREDENTIAL_RESOURCE_PERMISSION_PROFILE_KUBERNETES_SCOPED_RESOURCE_V1
		}
		if claim.ResourcePermissionProfile != expectedProfile {
			return 0, brokerIdentityWorkUnavailable
		}
		return claim.BrokerIdentityPurpose, nil
	default:
		return 0, brokerIdentityWorkUnavailable
	}
	return 0, nil
}

// validBrokerInvocationProgress accepts only original-claim local handoff metadata; it never completes an invocation or interprets nil handoff error as effect success.
// validBrokerInvocationProgress принимает лишь original-claim local handoff metadata без completion invocation либо вывода effect success из nil handoff error.
func validBrokerInvocationProgress(claim *cp.IntegrationInvocationClaim, progress brokerIdentityInvocationProgress) bool {
	if progress.InvocationRef != claim.InvocationRef || progress.LeaseRef != claim.Lease.Ref || progress.Generation != claim.Lease.Generation || progress.InputDigest != claim.InputDigest {
		return false
	}
	switch progress.State {
	case "RUNNING":
		return progress.RemoteOutcome == "NOT_STARTED" && !progress.Attempted
	case "TERMINAL":
		return progress.RemoteOutcome == "NOT_STARTED" || progress.RemoteOutcome == "SUCCEEDED" || progress.RemoteOutcome == "FAILED" || progress.RemoteOutcome == "UNKNOWN"
	case "WAITING_OWNER":
		return progress.RemoteOutcome == "NOT_STARTED" || progress.RemoteOutcome == "RUNNING"
	case "STREAM_ACTIVE":
		return progress.RemoteOutcome == "RUNNING"
	case "RECONCILING":
		return progress.RemoteOutcome == "UNKNOWN"
	default:
		return false
	}
}
