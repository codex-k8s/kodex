package grpc

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// managedKubernetesChildToProto preserves the original observation rather than reconstructing it from current state.
// Missing resource evidence is permitted only for registered addressless commands with canonical input.
// managedKubernetesChildToProto сохраняет original observation, не восстанавливая его из current state.
// Отсутствие resource evidence допустимо лишь для registered addressless commands с canonical input.
func managedKubernetesChildToProto(child entity.ManagedKubernetesChildEffect) *cp.ManagedKubernetesChildEffect {
	operation, known := integrationpackage.KubernetesOperationByKey(child.Operation)
	input, canonical, err := managedkubernetes.DecodeOperationInput(child.Operation, child.NormalizedInputJSON)
	sum := sha256.Sum256(canonical)
	if !known || err != nil || !bytes.Equal(canonical, child.NormalizedInputJSON) || !managedkubernetes.ValidLocator(child.Ref) || child.InputDigest != hex.EncodeToString(sum[:]) {
		return nil
	}
	switch child.State {
	case "WAITING_OWNER", "WAITING_PREDECESSOR", "READY", "ATTEMPTING", "SUCCEEDED", "FAILED", "UNKNOWN", "CANCELLED":
	default:
		return nil
	}
	entry := &cp.ManagedKubernetesChildEffect{ChildRef: child.Ref, Operation: child.Operation, InputDigest: child.InputDigest, NormalizedInputJson: append([]byte(nil), canonical...), State: child.State}
	if child.Observation == nil {
		if input.Resource != "" || child.Resource != (managedkubernetes.ResourceAddress{}) || child.NodeTransport != nil || operation.Execution != integrationpackage.KubernetesPlan && operation.Execution != integrationpackage.KubernetesOffline {
			return nil
		}
		return entry
	}
	observed := child.Observation
	if observed.Validate() != nil || observed.Resource != child.Resource || input.Resource == "" {
		return nil
	}
	resource := child.Resource
	all := child.Operation == "kubernetes.list.all-namespaces" || child.Operation == "kubernetes.watch.all-namespaces"
	if operation.Execution == integrationpackage.KubernetesOffline || resource.AllNamespaces != all || resource.Group != input.Group || resource.Version != input.Version || resource.Resource != input.Resource || resource.Namespace != input.Namespace || resource.Name != input.Name || resource.Subresource != input.Subresource || input.UID != "" && resource.UID != input.UID || input.ResourceVersion != "" && resource.ResourceVersion != input.ResourceVersion {
		return nil
	}
	entry.Resource = managedKubernetesResourceToProto(child.Resource)
	entry.Observation = &cp.ManagedKubernetesChildCandidate{Resource: managedKubernetesResourceToProto(observed.Resource), ObservedResourceDigest: observed.ObservedResourceDigest, RegisteredSchemaDigest: observed.RegisteredSchemaDigest, NamespaceUid: observed.NamespaceUID,
		ObservationState: cp.ManagedKubernetesObservationState(cp.ManagedKubernetesObservationState_value["MANAGED_KUBERNETES_OBSERVATION_STATE_"+string(observed.ObservationState)])}
	if witness := observed.PodWitness; witness != nil {
		entry.Observation.PodWitness = &cp.ManagedKubernetesPodNodeWitness{NodeName: witness.NodeName, NodeUid: witness.NodeUID, Container: witness.Container}
	}
	if child.NodeTransport != nil {
		if operation.Execution != integrationpackage.KubernetesStream {
			return nil
		}
		entry.NodeTransport = managedKubernetesNodeToProto(*child.NodeTransport)
		if entry.NodeTransport == nil || observed.PodWitness == nil || observed.PodWitness.NodeName != child.NodeTransport.NodeName || observed.PodWitness.NodeUID != child.NodeTransport.NodeUID {
			return nil
		}
	}
	return entry
}

// managedKubernetesWireValid rejects unknown nested fields/enums before constructing domain input.
// Only the fixed current message tree is admitted; maps, oversized envelopes and excessive nesting
// require an explicit contract change rather than a permissive intermediate representation.
// managedKubernetesWireValid отклоняет unknown nested fields/enums до domain input.
// Допускает только fixed current message tree; maps, oversized envelope и excessive nesting
// требуют явного contract change вместо permissive intermediate representation.
func managedKubernetesWireValid(input proto.Message) bool {
	if input == nil || proto.Size(input) > 256<<10 {
		return false
	}
	nodes := 0
	var walk func(protoreflect.Message, int) bool
	walk = func(message protoreflect.Message, depth int) bool {
		nodes++
		if !message.IsValid() || len(message.GetUnknown()) != 0 || depth > 8 || nodes > 1024 {
			return false
		}
		valid := true
		message.Range(func(field protoreflect.FieldDescriptor, fieldValue protoreflect.Value) bool {
			if field.IsMap() {
				valid = false
				return false
			}
			check := func(item protoreflect.Value) bool {
				switch field.Kind() {
				case protoreflect.MessageKind, protoreflect.GroupKind:
					return walk(item.Message(), depth+1)
				case protoreflect.EnumKind:
					return field.Enum().Values().ByNumber(item.Enum()) != nil
				default:
					return true
				}
			}
			if field.IsList() {
				list := fieldValue.List()
				for index := 0; index < list.Len(); index++ {
					if !check(list.Get(index)) {
						valid = false
						return false
					}
				}
			} else {
				valid = check(fieldValue)
			}
			return valid
		})
		return valid
	}
	return walk(input.ProtoReflect(), 0)
}

// managedKubernetesBindingFromProto copies only locators; authority is never taken from payload.
// managedKubernetesBindingFromProto копирует только locators без authority из payload.
func managedKubernetesBindingFromProto(input *cp.ManagedKubernetesOwnerBinding) value.ManagedKubernetesBinding {
	return value.ManagedKubernetesBinding{WorkloadInstance: input.GetWorkloadInstance(), InvocationRef: input.GetInvocationRef(),
		ConnectionTestRef: input.GetConnectionTestRef(), LeaseRef: input.GetLeaseRef(), Fence: input.GetFence(), Generation: input.GetGeneration(), InputDigest: input.GetInputDigest()}
}

// managedKubernetesResourceFromProto preserves explicit scope, UID and resource version for owner validation.
// managedKubernetesResourceFromProto сохраняет explicit scope, UID и resource version для owner validation.
func managedKubernetesResourceFromProto(input *cp.ManagedKubernetesResourceAddress) value.ManagedKubernetesResourceAddress {
	return value.ManagedKubernetesResourceAddress{Group: input.GetGroup(), Version: input.GetVersion(), Resource: input.GetResource(),
		Namespaced: input.GetNamespaced(), AllNamespaces: input.GetAllNamespaces(), Namespace: input.GetNamespace(), Name: input.GetName(), Subresource: input.GetSubresource(), UID: input.GetUid(), ResourceVersion: input.GetResourceVersion()}
}

// managedKubernetesResourceToProto emits only the typed owner-selected locator, never a raw URI.
// managedKubernetesResourceToProto выдаёт только typed owner-selected locator, не raw URI.
func managedKubernetesResourceToProto(input value.ManagedKubernetesResourceAddress) *cp.ManagedKubernetesResourceAddress {
	return &cp.ManagedKubernetesResourceAddress{Group: input.Group, Version: input.Version, Resource: input.Resource,
		Namespaced: input.Namespaced, AllNamespaces: input.AllNamespaces, Namespace: input.Namespace, Name: input.Name, Subresource: input.Subresource, Uid: input.UID, ResourceVersion: input.ResourceVersion}
}

// managedKubernetesTimestamp refuses absent or invalid owner expiries without refreshing historical receipts.
// managedKubernetesTimestamp отклоняет absent/invalid owner expiries без refresh historical receipts.
func managedKubernetesTimestamp(input time.Time) *timestamppb.Timestamp {
	if input.IsZero() {
		return nil
	}
	result := timestamppb.New(input)
	if result.CheckValid() != nil {
		return nil
	}
	return result
}

// managedKubernetesStreamGrantToProto validates the executable wire bounds before exposing credit.
// Unknown channels and duplicate entries are refused rather than dropped from the granted scope.
// managedKubernetesStreamGrantToProto проверяет executable wire bounds до выдачи credit.
// Unknown channels и duplicate entries отклоняются, а не удаляются из granted scope.
func managedKubernetesStreamGrantToProto(input entity.ManagedKubernetesStreamGrant) *cp.ManagedKubernetesOwnerStreamGrant {
	expires := managedKubernetesTimestamp(input.ExpiresAt)
	if !managedkubernetes.ValidLocator(input.Ref) || input.Generation < 1 || expires == nil || input.MaximumFrameBytes == 0 ||
		input.MaximumFrameBytes > managedkubernetes.MaximumFrameBytes || input.MaximumTotalBytes == 0 || input.MaximumTotalBytes > managedkubernetes.MaximumStreamBytes ||
		len(input.InputChannels)+len(input.OutputChannels) == 0 || len(input.InputChannels) > 7 || len(input.OutputChannels) > 7 {
		return nil
	}
	for _, channels := range [][]string{input.InputChannels, input.OutputChannels} {
		seen := make(map[string]bool, len(channels))
		for _, channel := range channels {
			if !managedkubernetes.ValidChannel(channel) || seen[channel] {
				return nil
			}
			seen[channel] = true
		}
	}
	return &cp.ManagedKubernetesOwnerStreamGrant{StreamRef: input.Ref, Generation: input.Generation, ExpiresAt: expires,
		MaximumFrameBytes: input.MaximumFrameBytes, MaximumTotalBytes: input.MaximumTotalBytes,
		InputChannels: append([]string(nil), input.InputChannels...), OutputChannels: append([]string(nil), input.OutputChannels...)}
}

// managedKubernetesOutcomeFromProto maps only known observations; absent/unknown values remain invalid.
// managedKubernetesOutcomeFromProto переносит только known observations; absent/unknown values недопустимы.
func managedKubernetesOutcomeFromProto(input cp.ManagedKubernetesEffectOutcome) enum.ManagedKubernetesEffectOutcome {
	switch input {
	case cp.ManagedKubernetesEffectOutcome_MANAGED_KUBERNETES_EFFECT_OUTCOME_SUCCEEDED:
		return enum.KubernetesEffectSucceeded
	case cp.ManagedKubernetesEffectOutcome_MANAGED_KUBERNETES_EFFECT_OUTCOME_FAILED:
		return enum.KubernetesEffectFailed
	case cp.ManagedKubernetesEffectOutcome_MANAGED_KUBERNETES_EFFECT_OUTCOME_UNKNOWN:
		return enum.KubernetesEffectUnknown
	default:
		return ""
	}
}

// managedKubernetesOutcomeToProto never converts an unrecognized owner outcome to success.
// managedKubernetesOutcomeToProto не превращает unrecognized owner outcome в success.
func managedKubernetesOutcomeToProto(input enum.ManagedKubernetesEffectOutcome) cp.ManagedKubernetesEffectOutcome {
	switch input {
	case enum.KubernetesEffectSucceeded:
		return cp.ManagedKubernetesEffectOutcome_MANAGED_KUBERNETES_EFFECT_OUTCOME_SUCCEEDED
	case enum.KubernetesEffectFailed:
		return cp.ManagedKubernetesEffectOutcome_MANAGED_KUBERNETES_EFFECT_OUTCOME_FAILED
	case enum.KubernetesEffectUnknown:
		return cp.ManagedKubernetesEffectOutcome_MANAGED_KUBERNETES_EFFECT_OUTCOME_UNKNOWN
	default:
		return cp.ManagedKubernetesEffectOutcome_MANAGED_KUBERNETES_EFFECT_OUTCOME_UNSPECIFIED
	}
}

// managedKubernetesDirectionFromProto keeps absent and future enum values invalid for owner admission.
// managedKubernetesDirectionFromProto сохраняет absent/future enum values недопустимыми для owner admission.
func managedKubernetesDirectionFromProto(input cp.ManagedKubernetesFrameDirection) enum.ManagedKubernetesFrameDirection {
	switch input {
	case cp.ManagedKubernetesFrameDirection_MANAGED_KUBERNETES_FRAME_DIRECTION_INPUT:
		return enum.KubernetesFrameInput
	case cp.ManagedKubernetesFrameDirection_MANAGED_KUBERNETES_FRAME_DIRECTION_OUTPUT:
		return enum.KubernetesFrameOutput
	default:
		return ""
	}
}

// managedKubernetesStateFromProto selects a registered owner state without string coercion.
// managedKubernetesStateFromProto выбирает registered owner state без string coercion.
func managedKubernetesStateFromProto(input cp.ManagedKubernetesOwnerStreamState) enum.ManagedKubernetesStreamState {
	switch input {
	case cp.ManagedKubernetesOwnerStreamState_MANAGED_KUBERNETES_OWNER_STREAM_STATE_PREPARED:
		return enum.KubernetesStreamPrepared
	case cp.ManagedKubernetesOwnerStreamState_MANAGED_KUBERNETES_OWNER_STREAM_STATE_ACTIVE:
		return enum.KubernetesStreamActive
	case cp.ManagedKubernetesOwnerStreamState_MANAGED_KUBERNETES_OWNER_STREAM_STATE_COMPLETED:
		return enum.KubernetesStreamCompleted
	case cp.ManagedKubernetesOwnerStreamState_MANAGED_KUBERNETES_OWNER_STREAM_STATE_CANCELED:
		return enum.KubernetesStreamCanceled
	case cp.ManagedKubernetesOwnerStreamState_MANAGED_KUBERNETES_OWNER_STREAM_STATE_REVOKED:
		return enum.KubernetesStreamRevoked
	case cp.ManagedKubernetesOwnerStreamState_MANAGED_KUBERNETES_OWNER_STREAM_STATE_EXPIRED:
		return enum.KubernetesStreamExpired
	case cp.ManagedKubernetesOwnerStreamState_MANAGED_KUBERNETES_OWNER_STREAM_STATE_LOST:
		return enum.KubernetesStreamLost
	default:
		return ""
	}
}

// managedKubernetesStateToProto returns zero for corrupt owner states so callers can fail closed.
// managedKubernetesStateToProto возвращает zero для corrupt owner states для закрытого отказа caller.
func managedKubernetesStateToProto(input enum.ManagedKubernetesStreamState) cp.ManagedKubernetesOwnerStreamState {
	return cp.ManagedKubernetesOwnerStreamState(cp.ManagedKubernetesOwnerStreamState_value["MANAGED_KUBERNETES_OWNER_STREAM_STATE_"+string(input)])
}

// managedKubernetesObservationFromProto preserves unknown state as invalid rather than assuming
// a present object; shared owner validation requires explicit item/absence/collection semantics.
// managedKubernetesObservationFromProto сохраняет unknown state как invalid без предположения
// о present object; общий owner validator требует явные item/absence/collection semantics.
func managedKubernetesObservationFromProto(input cp.ManagedKubernetesObservationState) managedkubernetes.ObservationState {
	switch input {
	case cp.ManagedKubernetesObservationState_MANAGED_KUBERNETES_OBSERVATION_STATE_PRESENT:
		return managedkubernetes.ObservationPresent
	case cp.ManagedKubernetesObservationState_MANAGED_KUBERNETES_OBSERVATION_STATE_ABSENT:
		return managedkubernetes.ObservationAbsent
	case cp.ManagedKubernetesObservationState_MANAGED_KUBERNETES_OBSERVATION_STATE_COLLECTION:
		return managedkubernetes.ObservationCollection
	case cp.ManagedKubernetesObservationState_MANAGED_KUBERNETES_OBSERVATION_STATE_UNVERSIONED_READ:
		return managedkubernetes.ObservationUnversionedRead
	default:
		return ""
	}
}

// managedKubernetesDiscoveryToProto emits only a validated registered non-resource address,
// refusing unknown or mixed coordinates instead of making up a GVR or forwarding a raw URL.
// managedKubernetesDiscoveryToProto выдаёт только valid registered non-resource address,
// отвергая unknown/mixed coordinates без выдуманного GVR либо передачи raw URL.
func managedKubernetesDiscoveryToProto(input value.ManagedKubernetesDiscoveryAddress) *cp.ManagedKubernetesDiscoveryAddress {
	if _, err := input.RelativeURL(); err != nil {
		return nil
	}
	var kind cp.ManagedKubernetesDiscoveryKind
	switch input.Kind {
	case managedkubernetes.DiscoveryVersion:
		kind = cp.ManagedKubernetesDiscoveryKind_MANAGED_KUBERNETES_DISCOVERY_KIND_VERSION
	case managedkubernetes.DiscoveryCoreVersions:
		kind = cp.ManagedKubernetesDiscoveryKind_MANAGED_KUBERNETES_DISCOVERY_KIND_CORE_VERSIONS
	case managedkubernetes.DiscoveryGroups:
		kind = cp.ManagedKubernetesDiscoveryKind_MANAGED_KUBERNETES_DISCOVERY_KIND_GROUPS
	case managedkubernetes.DiscoveryGroup:
		kind = cp.ManagedKubernetesDiscoveryKind_MANAGED_KUBERNETES_DISCOVERY_KIND_GROUP
	case managedkubernetes.DiscoveryResources:
		kind = cp.ManagedKubernetesDiscoveryKind_MANAGED_KUBERNETES_DISCOVERY_KIND_RESOURCES
	case managedkubernetes.DiscoveryOpenAPIIndex:
		kind = cp.ManagedKubernetesDiscoveryKind_MANAGED_KUBERNETES_DISCOVERY_KIND_OPENAPI_V3_INDEX
	case managedkubernetes.DiscoveryOpenAPISchema:
		kind = cp.ManagedKubernetesDiscoveryKind_MANAGED_KUBERNETES_DISCOVERY_KIND_OPENAPI_V3_SCHEMA
	default:
		return nil
	}
	return &cp.ManagedKubernetesDiscoveryAddress{Kind: kind, Group: input.Group, Version: input.Version, ImmutableSchemaHash: input.ImmutableSchemaHash}
}

// managedKubernetesIdentityToProto refuses mismatched owner identity/target and partial purpose pins.
// No generic runtime-secret caster or inferred namespace can fill an incomplete private locator.
// managedKubernetesIdentityToProto отклоняет несовпадение owner identity/target и неполные purpose pins.
// Общий runtime-secret caster и предполагаемый namespace не восполняют приватный locator.
func managedKubernetesIdentityToProto(input value.ManagedKubernetesIdentityLocator, identity, target string) *cp.ManagedKubernetesIdentityLocator {
	if input.Validate(input.Namespace) != nil || input.IdentityRef != identity || input.TargetRef != target {
		return nil
	}
	return &cp.ManagedKubernetesIdentityLocator{IdentityRef: input.IdentityRef, TargetRef: input.TargetRef,
		Revision: input.Revision, Namespace: input.Namespace, SecretName: input.Name, SecretUid: input.UID,
		SecretResourceVersion: input.ResourceVersion, ContentSha256: input.ContentSHA256}
}

// managedKubernetesSnapshotAddressValid admits address-free input only for a registered Plan/Offline class.
// Discovery and resource cannot be combined; unknown operations never inherit a permissive nil variant.
// managedKubernetesSnapshotAddressValid допускает address-free input лишь для registered Plan/Offline class.
// Discovery и resource несовместимы; unknown operation не получает permissive nil variant.
func managedKubernetesSnapshotAddressValid(input entity.ManagedKubernetesOperationSnapshot) bool {
	operation, known := integrationpackage.KubernetesOperationByKey(input.Operation)
	if !known {
		return false
	}
	if _, _, err := managedkubernetes.DecodeOperationInput(input.Operation, input.NormalizedInputJSON); err != nil {
		return false
	}
	hasResource := input.Resource != (value.ManagedKubernetesResourceAddress{})
	if input.Discovery != nil {
		if hasResource || operation.Execution != integrationpackage.KubernetesPlan {
			return false
		}
		_, err := input.Discovery.RelativeURL()
		return err == nil
	}
	if operation.Execution == integrationpackage.KubernetesOffline {
		return !hasResource
	}
	if !hasResource {
		return operation.Execution == integrationpackage.KubernetesPlan
	}
	return input.Resource.Validate() == nil
}

// managedKubernetesNodeToProto serializes only complete registered transport metadata, never credentials.
// Runtime eligibility/retirement and current time remain owner checks before effect and frame credit.
// managedKubernetesNodeToProto сериализует только полную registered transport metadata, не credentials.
// Runtime eligibility/retirement и current time проверяет owner до effect и frame credit.
func managedKubernetesNodeToProto(pin value.ManagedKubernetesNodeTransport) *cp.ManagedKubernetesNodeTransportSnapshot {
	before, after := managedKubernetesTimestamp(pin.NotBefore), managedKubernetesTimestamp(pin.ExpiresAt)
	if before == nil || after == nil || !pin.ExpiresAt.After(pin.NotBefore) || pin.Protocol.WireProtocol() == "" || pin.Generation < 1 || pin.TargetRevision < 1 {
		return nil
	}
	return &cp.ManagedKubernetesNodeTransportSnapshot{PinRef: pin.Ref, Generation: pin.Generation,
		TargetRef: pin.TargetRef, TargetRevision: pin.TargetRevision, NodeName: pin.NodeName, NodeUid: pin.NodeUID,
		ServerOrigin: pin.ServerOrigin, TlsServerName: pin.TLSServerName, CertificateAuthoritySha256: pin.CertificateAuthoritySHA256,
		CertificateAuthorityPem: append([]byte(nil), pin.CertificateAuthorityPEM...), ServerPublicKeySha256: pin.ServerPublicKeySHA256,
		AddressIps: append([]string(nil), pin.AddressIPs...), NotBefore: before, ExpiresAt: after, Protocol: string(pin.Protocol)}
}

// managedKubernetesReceiptOriginFromProto copies only the explicit private receipt lineage.
// Its refs are compared with owner rows later and never select authority on their own.
// managedKubernetesReceiptOriginFromProto копирует только explicit private receipt lineage.
// Refs затем сверяются с owner rows и сами по себе не выбирают authority.
func managedKubernetesReceiptOriginFromProto(input *cp.ManagedKubernetesStreamReceiptOrigin) managedkubernetes.StreamReceiptOrigin {
	return managedkubernetes.StreamReceiptOrigin{SnapshotDigest: input.GetSnapshotDigest(), BindingDigest: input.GetBindingDigest(), ChildInputDigest: input.GetChildInputDigest(),
		EffectRef: input.GetEffectRef(), EffectGeneration: input.GetEffectGeneration(), StreamRef: input.GetStreamRef(), StreamGeneration: input.GetStreamGeneration(),
		Operation: input.GetOperation(), TargetRef: input.GetTargetRef(), TargetRevision: input.GetTargetRevision()}
}

// managedKubernetesHandshakeFromProto preserves all observations so owner validation can recompute the digest.
// managedKubernetesHandshakeFromProto сохраняет observations для независимого пересчёта digest владельцем.
func managedKubernetesHandshakeFromProto(input *cp.ManagedKubernetesHandshakeReceipt) managedkubernetes.HandshakeReceipt {
	return managedkubernetes.HandshakeReceipt{Origin: managedKubernetesReceiptOriginFromProto(input.GetOrigin()), TransportRef: input.GetTransportRef(),
		TransportGeneration: input.GetTransportGeneration(), Protocol: input.GetProtocol(), HTTPStatus: int(input.GetHttpStatus()),
		NegotiatedProtocol: input.GetNegotiatedProtocol(), PeerSPKISHA256: input.GetPeerSpkiSha256()}
}

// managedKubernetesTerminalFromProto retains the distinction between no exit status and a proven exit zero.
// managedKubernetesTerminalFromProto сохраняет различие отсутствующего exit status и proven exit zero.
func managedKubernetesTerminalFromProto(input *cp.ManagedKubernetesTerminalReceipt) managedkubernetes.TerminalReceipt {
	var exit *int32
	if input != nil && input.ExitCode != nil {
		value := *input.ExitCode
		exit = &value
	}
	return managedkubernetes.TerminalReceipt{Origin: managedKubernetesReceiptOriginFromProto(input.GetOrigin()), HandshakeDigest: input.GetHandshakeDigest(),
		Protocol: input.GetProtocol(), TransportJoined: input.GetTransportJoined(), LocalState: string(managedKubernetesStateFromProto(input.GetLocalState())),
		RemoteOutcome: string(managedKubernetesOutcomeFromProto(input.GetRemoteOutcome())), ExitCode: exit, CleanEOF: input.GetCleanEof(),
		RemoteStatusCode: int(input.GetRemoteStatusCode()), ReasonCode: input.GetReasonCode()}
}

// managedKubernetesOptionalHandshakeFromProto preserves absence separately from a captured witness.
// The owner validates the immutable origin/pin before recording this metadata without new credit.
// managedKubernetesOptionalHandshakeFromProto отделяет отсутствие от captured witness.
// Owner проверяет immutable origin/pin до записи metadata без нового credit.
func managedKubernetesOptionalHandshakeFromProto(input *cp.ManagedKubernetesHandshakeReceipt) *managedkubernetes.HandshakeReceipt {
	if input == nil {
		return nil
	}
	receipt := managedKubernetesHandshakeFromProto(input)
	return &receipt
}
