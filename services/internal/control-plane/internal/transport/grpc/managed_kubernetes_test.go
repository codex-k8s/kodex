package grpc

import (
	"context"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// TestManagedKubernetesIdentityLocatorCannotBecomeGenericCredential requires one exact owner
// identity/target and complete private purpose locator before returning broker-only metadata.
// TestManagedKubernetesIdentityLocatorCannotBecomeGenericCredential требует exact owner
// identity/target и полный приватный purpose locator до возврата broker-only metadata.
func TestManagedKubernetesIdentityLocatorCannotBecomeGenericCredential(t *testing.T) {
	name, err := managedkubernetes.IdentitySecretName("kbi_synthetic_identity", 3)
	if err != nil {
		t.Fatal("derive synthetic private storage name")
	}
	locator := value.ManagedKubernetesIdentityLocator{IdentityRef: "kbi_synthetic_identity", TargetRef: "target_synthetic", Revision: 3,
		Namespace: "broker-private", Name: name, UID: "76b0b43a-84c2-44d6-81a4-7c477a92ef2e", ResourceVersion: "42", ContentSHA256: strings.Repeat("a", 64)}
	result := managedKubernetesIdentityToProto(locator, locator.IdentityRef, locator.TargetRef)
	if result == nil || result.GetIdentityRef() != locator.IdentityRef || result.GetTargetRef() != locator.TargetRef ||
		result.GetRevision() != 3 || result.GetNamespace() != locator.Namespace || result.GetSecretName() != locator.Name ||
		result.GetSecretUid() != locator.UID || result.GetSecretResourceVersion() != locator.ResourceVersion || result.GetContentSha256() != locator.ContentSHA256 {
		t.Fatal("broker purpose locator lost an immutable owner pin")
	}
	if managedKubernetesIdentityToProto(locator, "foreign_identity", locator.TargetRef) != nil ||
		managedKubernetesIdentityToProto(locator, locator.IdentityRef, "foreign_target") != nil ||
		managedKubernetesIdentityToProto(value.ManagedKubernetesIdentityLocator{}, locator.IdentityRef, locator.TargetRef) != nil {
		t.Fatal("foreign or missing private locator became an executable descriptor")
	}
	locator.UID = ""
	if managedKubernetesIdentityToProto(locator, locator.IdentityRef, locator.TargetRef) != nil {
		t.Fatal("partial purpose locator borrowed generic credential defaults")
	}
}

// TestManagedKubernetesWireRejectsNestedUnknownAndUnknownEnums prevents silent field loss
// before owner validation, including deep proposal resources and future terminal outcomes.
// TestManagedKubernetesWireRejectsNestedUnknownAndUnknownEnums запрещает silent field loss
// до owner validation, включая deep proposal resources и future terminal outcomes.
func TestManagedKubernetesWireRejectsNestedUnknownAndUnknownEnums(t *testing.T) {
	var absent *cp.ResolveManagedKubernetesOperationRequest
	if managedKubernetesWireValid(absent) {
		t.Fatal("typed nil request became a valid owner command")
	}
	proposal := &cp.PrepareManagedKubernetesChildrenRequest{Binding: &cp.ManagedKubernetesOwnerBinding{},
		Candidates: []*cp.ManagedKubernetesChildCandidate{{Resource: &cp.ManagedKubernetesResourceAddress{Version: "v1", Resource: "pods"}}}}
	if !managedKubernetesWireValid(proposal) {
		t.Fatal("known request shape was rejected before semantic validation")
	}
	for _, path := range []func(*cp.PrepareManagedKubernetesChildrenRequest) proto.Message{
		func(input *cp.PrepareManagedKubernetesChildrenRequest) proto.Message { return input },
		func(input *cp.PrepareManagedKubernetesChildrenRequest) proto.Message { return input.Binding },
		func(input *cp.PrepareManagedKubernetesChildrenRequest) proto.Message { return input.Candidates[0] },
		func(input *cp.PrepareManagedKubernetesChildrenRequest) proto.Message {
			return input.Candidates[0].Resource
		},
	} {
		changed := proto.Clone(proposal).(*cp.PrepareManagedKubernetesChildrenRequest)
		path(changed).ProtoReflect().SetUnknown([]byte{0x98, 0x06, 0x01})
		if managedKubernetesWireValid(changed) {
			t.Fatal("unknown nested field was dropped before owner semantics")
		}
	}
	if managedKubernetesWireValid(&cp.CloseManagedKubernetesStreamRequest{RemoteOutcome: cp.ManagedKubernetesEffectOutcome(99)}) ||
		managedKubernetesWireValid(&cp.ManagedKubernetesChildCandidate{ObservationState: cp.ManagedKubernetesObservationState(99)}) ||
		managedKubernetesWireValid(&cp.CompleteManagedKubernetesEffectRequest{ResultJson: make([]byte, (256<<10)+1)}) {
		t.Fatal("unknown enum or oversized envelope reached owner semantics")
	}
	if managedKubernetesOutcomeFromProto(cp.ManagedKubernetesEffectOutcome(99)) != "" ||
		managedKubernetesDirectionFromProto(cp.ManagedKubernetesFrameDirection(99)) != "" ||
		managedKubernetesStateFromProto(cp.ManagedKubernetesOwnerStreamState(99)) != "" {
		t.Fatal("future enum value acquired a registered outcome, direction or state")
	}
}

// TestManagedKubernetesStreamGrantPreservesHistoricalCredit bounds wire credit without refreshing
// an expired persisted receipt or borrowing mutable caller slices.
// TestManagedKubernetesStreamGrantPreservesHistoricalCredit ограничивает wire credit без refresh
// expired persisted receipt либо заимствования mutable caller slices.
func TestManagedKubernetesStreamGrantPreservesHistoricalCredit(t *testing.T) {
	input := entity.ManagedKubernetesStreamGrant{Ref: "kst_synthetic", Generation: 2,
		ExpiresAt: time.Date(2020, 1, 2, 3, 4, 5, 123456000, time.UTC), MaximumFrameBytes: managedkubernetes.MaximumFrameBytes,
		MaximumTotalBytes: 8 << 20, InputChannels: []string{"STDIN"}, OutputChannels: []string{"STDOUT", "STDERR"}}
	result := managedKubernetesStreamGrantToProto(input)
	if result == nil || result.GetExpiresAt().AsTime() != input.ExpiresAt || result.GetGeneration() != input.Generation {
		t.Fatal("historical stream admission was refreshed or changed")
	}
	input.InputChannels[0] = "FOREIGN"
	if result.GetInputChannels()[0] != "STDIN" {
		t.Fatal("caller mutation changed serialized owner stream credit")
	}
	input.InputChannels[0] = "STDIN"
	for _, mutate := range []func(*entity.ManagedKubernetesStreamGrant){
		func(input *entity.ManagedKubernetesStreamGrant) { input.Ref = "" },
		func(input *entity.ManagedKubernetesStreamGrant) { input.Generation = 0 },
		func(input *entity.ManagedKubernetesStreamGrant) { input.ExpiresAt = time.Time{} },
		func(input *entity.ManagedKubernetesStreamGrant) { input.MaximumFrameBytes = 0 },
		func(input *entity.ManagedKubernetesStreamGrant) { input.MaximumFrameBytes++ },
		func(input *entity.ManagedKubernetesStreamGrant) { input.MaximumTotalBytes = 0 },
		func(input *entity.ManagedKubernetesStreamGrant) {
			input.MaximumTotalBytes = managedkubernetes.MaximumStreamBytes + 1
		},
		func(input *entity.ManagedKubernetesStreamGrant) { input.InputChannels = []string{"STDIN", "STDIN"} },
		func(input *entity.ManagedKubernetesStreamGrant) { input.OutputChannels = []string{"RAW_HTTP"} },
	} {
		changed := input
		mutate(&changed)
		if managedKubernetesStreamGrantToProto(changed) != nil {
			t.Fatal("malformed persisted stream credit became executable wire metadata")
		}
	}
}

// TestManagedKubernetesOwnerRPCsRequireExactVerifiedContext exercises every actual handler without
// any service/repository: missing method authority must fail before reaching the nil domain service.
// TestManagedKubernetesOwnerRPCsRequireExactVerifiedContext проверяет каждый actual handler без
// service/repository: missing method authority отклоняется до nil domain service.
func TestManagedKubernetesOwnerRPCsRequireExactVerifiedContext(t *testing.T) {
	server := &Server{}
	ctx := context.Background()
	for _, invoke := range []func() error{
		func() error {
			_, err := server.ResolveManagedKubernetesOperation(ctx, &cp.ResolveManagedKubernetesOperationRequest{})
			return err
		},
		func() error {
			_, err := server.PrepareManagedKubernetesChildren(ctx, &cp.PrepareManagedKubernetesChildrenRequest{})
			return err
		},
		func() error {
			_, err := server.BeginManagedKubernetesEffect(ctx, &cp.BeginManagedKubernetesEffectRequest{})
			return err
		},
		func() error {
			_, err := server.OpenManagedKubernetesStream(ctx, &cp.OpenManagedKubernetesStreamRequest{})
			return err
		},
		func() error {
			_, err := server.CompleteManagedKubernetesEffect(ctx, &cp.CompleteManagedKubernetesEffectRequest{})
			return err
		},
		func() error {
			_, err := server.BeginManagedKubernetesFrame(ctx, &cp.BeginManagedKubernetesFrameRequest{})
			return err
		},
		func() error {
			_, err := server.CompleteManagedKubernetesFrame(ctx, &cp.CompleteManagedKubernetesFrameRequest{})
			return err
		},
		func() error {
			_, err := server.CloseManagedKubernetesStream(ctx, &cp.CloseManagedKubernetesStreamRequest{})
			return err
		},
		func() error {
			_, err := server.ClaimManagedKubernetesInputFrame(ctx, &cp.ClaimManagedKubernetesInputFrameRequest{})
			return err
		},
		func() error {
			_, err := server.CheckManagedKubernetesOwnerReadiness(ctx, &cp.CheckManagedKubernetesOwnerReadinessRequest{})
			return err
		},
	} {
		if err := invoke(); status.Code(err) != codes.Unauthenticated {
			t.Fatal("unverified caller reached a Kubernetes owner handler")
		}
	}
}

// TestManagedKubernetesObservationCasterPreservesUnknown checks the actual generated enum mapping
// without supplying fabricated resource identity for absent objects or collection reads.
// TestManagedKubernetesObservationCasterPreservesUnknown проверяет generated enum mapping
// без выдуманной resource identity для отсутствующего объекта либо collection reads.
func TestManagedKubernetesObservationCasterPreservesUnknown(t *testing.T) {
	for wire, want := range map[cp.ManagedKubernetesObservationState]managedkubernetes.ObservationState{
		cp.ManagedKubernetesObservationState_MANAGED_KUBERNETES_OBSERVATION_STATE_UNSPECIFIED: "",
		cp.ManagedKubernetesObservationState_MANAGED_KUBERNETES_OBSERVATION_STATE_PRESENT:     managedkubernetes.ObservationPresent,
		cp.ManagedKubernetesObservationState_MANAGED_KUBERNETES_OBSERVATION_STATE_ABSENT:      managedkubernetes.ObservationAbsent,
		cp.ManagedKubernetesObservationState_MANAGED_KUBERNETES_OBSERVATION_STATE_COLLECTION:  managedkubernetes.ObservationCollection,
		cp.ManagedKubernetesObservationState(99):                                              "",
	} {
		if got := managedKubernetesObservationFromProto(wire); got != want {
			t.Fatal("observation enum widened or lost explicit presence")
		}
	}
}

// TestManagedKubernetesDiscoveryWirePreservesTypedCoordinates checks that non-resource reads
// retain their registered wire kind/hash and never become a raw resource address.
// TestManagedKubernetesDiscoveryWirePreservesTypedCoordinates проверяет сохранение registered
// wire kind/hash non-resource read без превращения в raw resource address.
func TestManagedKubernetesDiscoveryWirePreservesTypedCoordinates(t *testing.T) {
	address := managedkubernetes.DiscoveryAddress{Kind: managedkubernetes.DiscoveryOpenAPISchema, Group: "apps", Version: "v1", ImmutableSchemaHash: strings.Repeat("a", 128)}
	view := managedKubernetesDiscoveryToProto(address)
	if view == nil || view.GetKind() != cp.ManagedKubernetesDiscoveryKind_MANAGED_KUBERNETES_DISCOVERY_KIND_OPENAPI_V3_SCHEMA || view.GetGroup() != "apps" || view.GetVersion() != "v1" || view.GetImmutableSchemaHash() != address.ImmutableSchemaHash {
		t.Fatal("typed immutable discovery wire coordinates changed")
	}
	address.Group = "apps/../../secrets"
	if managedKubernetesDiscoveryToProto(address) != nil {
		t.Fatal("unsafe discovery address became an owner wire response")
	}
	if managedKubernetesWireValid(&cp.ManagedKubernetesDiscoveryAddress{Kind: cp.ManagedKubernetesDiscoveryKind(99)}) {
		t.Fatal("unknown discovery kind survived wire validation")
	}
}

// TestManagedKubernetesAddressClasses rejects fabricated resource addresses for unknown/offline
// commands while preserving the registered address-free identity and local metadata variants.
// TestManagedKubernetesAddressClasses отвергает fabricated resource addresses для unknown/offline
// commands, сохраняя registered address-free identity и local metadata варианты.
func TestManagedKubernetesAddressClasses(t *testing.T) {
	for _, test := range []struct {
		operation, raw string
		resource       value.ManagedKubernetesResourceAddress
		discovery      *value.ManagedKubernetesDiscoveryAddress
		accepted       bool
	}{
		{operation: "kubernetes.identity", raw: `{}`, accepted: true},
		{operation: "kubernetes.help", raw: `{}`, accepted: true},
		{operation: "kubernetes.unknown", raw: `{}`},
		{operation: "kubernetes.identity", raw: `{"actor":"caller"}`},
		{operation: "kubernetes.help", raw: `{}`, resource: value.ManagedKubernetesResourceAddress{Version: "v1", Resource: "pods", Namespaced: true, Namespace: "apps"}},
		{operation: "kubernetes.identity", raw: `{}`, resource: value.ManagedKubernetesResourceAddress{Version: "v1", Resource: "pods", Namespaced: true, Namespace: "apps"}, discovery: &value.ManagedKubernetesDiscoveryAddress{Kind: managedkubernetes.DiscoveryVersion}},
	} {
		input := entity.ManagedKubernetesOperationSnapshot{Operation: test.operation, NormalizedInputJSON: []byte(test.raw), Resource: test.resource, Discovery: test.discovery}
		if managedKubernetesSnapshotAddressValid(input) != test.accepted {
			t.Fatal("owner address class classification changed", test.operation)
		}
	}
	candidate := &cp.ManagedKubernetesChildCandidate{PodWitness: &cp.ManagedKubernetesPodNodeWitness{NodeName: "node-one", NodeUid: "20000000-0000-4000-8000-000000000001", Container: "worker"}}
	candidate.PodWitness.ProtoReflect().SetUnknown([]byte{0x98, 0x06, 0x01})
	if managedKubernetesWireValid(candidate) {
		t.Fatal("future Pod witness fields disappeared before owner validation")
	}
}

// TestManagedKubernetesTypedReceiptProjection preserves the original claim and proven zero exit,
// rejecting missing exit evidence instead of interpreting it as process success.
// TestManagedKubernetesTypedReceiptProjection сохраняет original claim и proven zero exit,
// отвергая отсутствие exit evidence вместо трактовки его как process success.
func TestManagedKubernetesTypedReceiptProjection(t *testing.T) {
	origin := &cp.ManagedKubernetesStreamReceiptOrigin{SnapshotDigest: strings.Repeat("a", 64), BindingDigest: strings.Repeat("b", 64), ChildInputDigest: strings.Repeat("c", 64), EffectRef: "kef_receipt", EffectGeneration: 1, StreamRef: "kst_receipt", StreamGeneration: 2, Operation: "kubernetes.exec", TargetRef: "target_receipt", TargetRevision: 3}
	wire := &cp.ManagedKubernetesHandshakeReceipt{Origin: origin, TransportRef: "nodepin_receipt", TransportGeneration: 4, Protocol: string(managedkubernetes.WebSocketV5), HttpStatus: 101, NegotiatedProtocol: managedkubernetes.WebSocketV5.WireProtocol(), PeerSpkiSha256: strings.Repeat("d", 64)}
	handshake := managedKubernetesHandshakeFromProto(wire)
	digest, err := handshake.Digest()
	if err != nil || handshake.Origin.StreamGeneration != 2 || handshake.TransportGeneration != 4 || handshake.Origin.TargetRevision != 3 {
		t.Fatal("typed handshake lost origin or transport observations")
	}
	zero := int32(0)
	terminal := &cp.ManagedKubernetesTerminalReceipt{Origin: origin, HandshakeDigest: digest, Protocol: wire.Protocol, TransportJoined: true, LocalState: cp.ManagedKubernetesOwnerStreamState_MANAGED_KUBERNETES_OWNER_STREAM_STATE_COMPLETED, RemoteOutcome: cp.ManagedKubernetesEffectOutcome_MANAGED_KUBERNETES_EFFECT_OUTCOME_SUCCEEDED, ExitCode: &zero, ReasonCode: "REMOTE_EXIT"}
	projected := managedKubernetesTerminalFromProto(terminal)
	if projected.Validate() != nil || projected.ExitCode == nil || *projected.ExitCode != 0 || projected.ExitCode == terminal.ExitCode {
		t.Fatal("zero exit was lost or remained caller-owned")
	}
	terminal.ExitCode = nil
	if managedKubernetesTerminalFromProto(terminal).Validate() == nil {
		t.Fatal("missing exit became successful remote completion")
	}
	if managedKubernetesHandshakeFromProto(nil).Validate() == nil || managedKubernetesTerminalFromProto(nil).Validate() == nil {
		t.Fatal("absent receipt acquired a valid observation")
	}
}

// TestManagedKubernetesIdentityHealthProfileRejectsAmbiguity closes contradictory readiness profiles before any owner read and retains the verified-principal requirement for the new typed profile.
// TestManagedKubernetesIdentityHealthProfileRejectsAmbiguity закрывает contradictory readiness profiles до owner read и сохраняет verified-principal requirement нового typed profile.
func TestManagedKubernetesIdentityHealthProfileRejectsAmbiguity(t *testing.T) {
	server := &Server{}
	if _, err := server.CheckManagedKubernetesOwnerReadiness(t.Context(), &cp.CheckManagedKubernetesOwnerReadinessRequest{DeliveryContractOnly: true, IdentityHealthOnly: true}); status.Code(err) != codes.InvalidArgument {
		t.Fatal("ambiguous readiness profile reached owner")
	}
	if _, err := server.CheckManagedKubernetesOwnerReadiness(t.Context(), &cp.CheckManagedKubernetesOwnerReadinessRequest{IdentityHealthOnly: true}); status.Code(err) != codes.Unauthenticated {
		t.Fatal("identity readiness bypassed verified principal")
	}
	request := &cp.CheckManagedKubernetesOwnerReadinessRequest{IdentityHealthOnly: true}
	request.ProtoReflect().SetUnknown([]byte{0xa0, 0x06, 1})
	if _, err := server.CheckManagedKubernetesOwnerReadiness(t.Context(), request); status.Code(err) != codes.InvalidArgument {
		t.Fatal("unknown identity readiness profile reached owner")
	}
}
