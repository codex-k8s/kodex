package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/libs/go/observability"
	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
	businessmetrics "github.com/codex-k8s/kodex/services/external/integration-gateway/internal/observability/metrics"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// claimConsumerFixture uses the actual bounded gate for local reservations and retains adopted credit until explicit test completion; it performs no provider work or owner authorization.
// claimConsumerFixture использует actual bounded gate для local reservations и сохраняет adopted credit до explicit test completion; provider work и owner authorization отсутствуют.
type claimConsumerFixture struct {
	lifecycleConsumerFixture
	mu          sync.Mutex
	adopted     []*claimSlotFixture
	mutate      func(*brokerIdentityInvocationProgress)
	closeActive context.CancelFunc
}

// claimSlotFixture permits one adoption and distinguishes final unused release from active completion using its own lock and the real shared gate.
// claimSlotFixture допускает одно adoption и отличает final unused release от active completion через собственный lock и real shared gate.
type claimSlotFixture struct {
	mu                sync.Mutex
	consumer          *claimConsumerFixture
	claim             *cp.IntegrationInvocationClaim
	release           func()
	adopted, released bool
}

// newClaimConsumerFixture creates only local test capacity and registers joined cleanup so no test failure leaks a reservation or adopted slot.
// newClaimConsumerFixture создаёт лишь local test capacity и регистрирует joined cleanup, чтобы test failure не оставил reservation либо adopted slot.
func newClaimConsumerFixture(t *testing.T, size int) *claimConsumerFixture {
	t.Helper()
	gate, err := serviceruntime.NewActivityGate(size)
	if err != nil {
		t.Fatal(err)
	}
	f := &claimConsumerFixture{lifecycleConsumerFixture: lifecycleConsumerFixture{gate: gate, failure: make(chan struct{})}}
	t.Cleanup(func() {
		_ = f.Close()
		f.mu.Lock()
		for _, slot := range f.adopted {
			slot.finish()
		}
		f.mu.Unlock()
	})
	return f
}

// Close shuts admission before signalling existing activity cleanup; a cancellation request does not itself release any active gate credit.
// Close закрывает admission до сигнала existing activity cleanup; cancellation request сам не освобождает active gate credit.
func (f *claimConsumerFixture) Close() error {
	err := f.gate.Close()
	if f.closeActive != nil {
		f.closeActive()
	}
	return err
}

// ReserveClaim reserves a real local gate slot without starting a goroutine, running Claim or accessing credentials.
// ReserveClaim резервирует real local gate slot без goroutine, Claim либо доступа к credentials.
func (f *claimConsumerFixture) ReserveClaim() (brokerIdentityClaimSlot, bool) {
	release, err := f.gate.Enter()
	if err != nil {
		return nil, false
	}
	return &claimSlotFixture{consumer: f, release: release}, true
}

// ExecuteClaim adopts one exact copied claim and deliberately mutates only its owned argument to verify isolation from the owner response.
// ExecuteClaim принимает один exact copied claim и намеренно меняет лишь owned argument для проверки изоляции owner response.
func (s *claimSlotFixture) ExecuteClaim(ctx context.Context, claim *cp.IntegrationInvocationClaim) (brokerIdentityInvocationProgress, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx.Err() != nil || s.released || s.adopted || claim == nil {
		return brokerIdentityInvocationProgress{}, brokerIdentityWorkUnavailable
	}
	s.adopted, s.claim = true, proto.Clone(claim).(*cp.IntegrationInvocationClaim)
	s.consumer.mu.Lock()
	s.consumer.adopted = append(s.consumer.adopted, s)
	s.consumer.mu.Unlock()
	progress := brokerIdentityInvocationProgress{InvocationRef: claim.InvocationRef, LeaseRef: claim.Lease.Ref, Generation: claim.Lease.Generation, InputDigest: claim.InputDigest, State: "RUNNING", RemoteOutcome: "NOT_STARTED"}
	claim.InvocationRef = "consumer_owned_argument"
	if s.consumer.mutate != nil {
		s.consumer.mutate(&progress)
	}
	return progress, nil
}

// ReleaseUnused is idempotent and cannot release an adopted activity, matching the production port contract.
// ReleaseUnused идемпотентен и не освобождает adopted activity согласно production port contract.
func (s *claimSlotFixture) ReleaseUnused() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.adopted && !s.released {
		s.released = true
		s.release()
	}
}

// finish releases credit only after the test explicitly models positive activity completion.
// finish освобождает credit лишь после явного positive activity completion в тесте.
func (s *claimSlotFixture) finish() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.released {
		s.released = true
		s.release()
	}
}

// claimOwnerFixture models only the protected RPC seam, keeping a distinct committed-lease marker before transport failure; actual SQL authority is qualified separately by the owner native suite.
// claimOwnerFixture моделирует лишь protected RPC seam, сохраняя отдельный committed-lease marker до transport failure; actual SQL authority отдельно проверяет owner native suite.
type claimOwnerFixture struct {
	cp.RuntimeWorkServiceClient
	cp.PersonalIntegrationCredentialServiceClient
	calls     int
	committed bool
	request   *cp.ClaimIntegrationInvocationsRequest
	reply     *cp.ClaimIntegrationInvocationsResponse
	err       error
}

// ClaimIntegrationInvocations retains the exact capacity/instance request and can lose its reply after the fixture's owner commit marker, never returning a synthetic second claim batch.
// ClaimIntegrationInvocations сохраняет exact capacity/instance request и может потерять reply после owner commit marker без выдачи synthetic second claim batch.
func (f *claimOwnerFixture) ClaimIntegrationInvocations(_ context.Context, request *cp.ClaimIntegrationInvocationsRequest, _ ...grpc.CallOption) (*cp.ClaimIntegrationInvocationsResponse, error) {
	f.calls++
	f.request = proto.Clone(request).(*cp.ClaimIntegrationInvocationsRequest)
	f.committed = true
	return f.reply, f.err
}

// ClaimPersonalIntegrationCredentialTests provides an empty preceding queue so the actual common loop reaches only the claim seam under review.
// ClaimPersonalIntegrationCredentialTests задаёт пустую preceding queue, чтобы actual common loop достиг лишь проверяемого claim seam.
func (f *claimOwnerFixture) ClaimPersonalIntegrationCredentialTests(context.Context, *cp.ClaimPersonalIntegrationCredentialTestsRequest, ...grpc.CallOption) (*cp.ClaimPersonalIntegrationCredentialTestsResponse, error) {
	return &cp.ClaimPersonalIntegrationCredentialTestsResponse{}, nil
}

// ClaimIntegrationConnectionTests provides no health effects or generic completions in the delivery failure test.
// ClaimIntegrationConnectionTests исключает health effects и generic completions в delivery failure test.
func (f *claimOwnerFixture) ClaimIntegrationConnectionTests(context.Context, *cp.ClaimIntegrationConnectionTestsRequest, ...grpc.CallOption) (*cp.ClaimIntegrationConnectionTestsResponse, error) {
	return &cp.ClaimIntegrationConnectionTestsResponse{}, nil
}

// brokerInvocationClaim converts the unchanged shipped Kubernetes source package into metadata-only delivery data, never claiming an executable adapter or actual owner admission.
// brokerInvocationClaim превращает unchanged shipped Kubernetes source package в metadata-only delivery data без заявления executable adapter либо actual owner admission.
func brokerInvocationClaim(t *testing.T) *cp.IntegrationInvocationClaim {
	t.Helper()
	health := brokerHealthClaim(t)
	definition, err := integrationpackage.Parse(health.DefinitionPackage)
	if err != nil {
		t.Fatal(err)
	}
	capability, _ := definition.CapabilityByOperation(definition.Spec.HealthCheck.Operation)
	return &cp.IntegrationInvocationClaim{InvocationRef: "inv_fixture01", ConnectionRef: health.ConnectionRef, DefinitionKey: health.DefinitionKey, DefinitionVersion: health.DefinitionVersion, DefinitionDigest: health.DefinitionDigest, DefinitionPackage: health.DefinitionPackage, CapabilityKey: capability.Key, Operation: capability.Operation, Risk: cp.IntegrationRisk(cp.IntegrationRisk_value["INTEGRATION_RISK_"+capability.Risk]), ApprovalPolicy: cp.IntegrationApprovalPolicy(cp.IntegrationApprovalPolicy_value["INTEGRATION_APPROVAL_POLICY_"+capability.ApprovalPolicy]), InputDigest: health.InputDigest, Lease: health.Lease, WorkMode: cp.IntegrationInvocationWorkMode_INTEGRATION_INVOCATION_WORK_MODE_EXECUTE,
		CredentialDeliveryKind: health.CredentialDeliveryKind, BrokerIdentityPurpose: health.BrokerIdentityPurpose, BrokerIdentityRef: health.BrokerIdentityRef, BrokerIdentityRevision: health.BrokerIdentityRevision,
		ResourcePermissionProfile: cp.IntegrationCredentialResourcePermissionProfile_INTEGRATION_CREDENTIAL_RESOURCE_PERMISSION_PROFILE_KUBERNETES_SCOPED_RESOURCE_V1}
}

// TestBrokerIdentityClaimReservationFairness checks every finite capacity bound, including limit one rotation and zero configured purpose capacity without starving ordinary delivery.
// TestBrokerIdentityClaimReservationFairness проверяет каждый finite capacity bound, включая limit one rotation и zero configured purpose capacity без starvation ordinary delivery.
func TestBrokerIdentityClaimReservationFairness(t *testing.T) {
	for _, limit := range []int32{1, 2, 3, 8, 32} {
		kube, ssh := newClaimConsumerFixture(t, 4), newClaimConsumerFixture(t, 4)
		registry := &brokerIdentityConsumers{Kubernetes: kube, SSH: ssh, deliveryContract: func(context.Context) (uint32, error) { return 1, nil }}
		totals := [3]uint32{}
		for cycle := 0; cycle < 3; cycle++ {
			slots, err := reserveBrokerIdentityClaims(t.Context(), registry, limit)
			if err != nil || slots.capacity == nil {
				t.Fatal("local capacity reservation failed")
			}
			capacity := slots.capacity
			if capacity.Ordinary+capacity.Kubernetes+capacity.Ssh != uint32(limit) || capacity.Kubernetes > 4 || capacity.Ssh > 4 {
				t.Fatal("owner capacity exceeds real reservations")
			}
			totals[0], totals[1], totals[2] = totals[0]+capacity.Ordinary, totals[1]+capacity.Kubernetes, totals[2]+capacity.Ssh
			slots.releaseUnused()
		}
		if totals[0] == 0 || totals[1] == 0 || totals[2] == 0 {
			t.Fatal("one delivery class was starved")
		}
	}
	registry := &brokerIdentityConsumers{deliveryContract: func(context.Context) (uint32, error) { return 1, nil }}
	slots, err := reserveBrokerIdentityClaims(t.Context(), registry, 3)
	if err != nil || slots.capacity.Ordinary != 3 || slots.capacity.Kubernetes != 0 || slots.capacity.Ssh != 0 {
		t.Fatal("optional absent purpose blocked ordinary progress")
	}
}

// TestBrokerIdentityClaimAdoptsOriginalOnce verifies metadata-only handoff, exact claim cloning and retained active capacity after the dispatcher's final unused release.
// TestBrokerIdentityClaimAdoptsOriginalOnce проверяет metadata-only handoff, exact claim cloning и retained active capacity после final unused release dispatcher.
func TestBrokerIdentityClaimAdoptsOriginalOnce(t *testing.T) {
	consumer := newClaimConsumerFixture(t, 1)
	registry := &brokerIdentityConsumers{Kubernetes: consumer, deliveryContract: func(context.Context) (uint32, error) { return 1, nil }, claimTurn: 1}
	claim := brokerInvocationClaim(t)
	original := proto.Clone(claim)
	owner := &claimOwnerFixture{reply: &cp.ClaimIntegrationInvocationsResponse{Claims: []*cp.IntegrationInvocationClaim{claim}}}
	ordinary, adopted, err := claimIntegrationInvocationDelivery(t.Context(), owner, registry, "worker_fixture", 1, func(brokerIdentityInvocationProgress) { t.Fatal("adoption became an attempted external effect") })
	if err != nil || len(ordinary) != 0 || adopted != 1 || owner.calls != 1 || owner.request.Capacity.Kubernetes != 1 || !proto.Equal(original, claim) || len(consumer.adopted) != 1 || !proto.Equal(original, consumer.adopted[0].claim) {
		t.Fatal("single original claim handoff changed lineage")
	}
	if _, ok := consumer.ReserveClaim(); ok {
		t.Fatal("active adoption was released as unused")
	}
}

// TestBrokerIdentityClaimValidatesWholeBatchBeforeAdoption rejects every malformed representation and over-capacity result before invoking any purpose slot, preserving no generic TOKEN fallback.
// TestBrokerIdentityClaimValidatesWholeBatchBeforeAdoption отклоняет malformed representation и over-capacity result до вызова purpose slot без generic TOKEN fallback.
func TestBrokerIdentityClaimValidatesWholeBatchBeforeAdoption(t *testing.T) {
	for _, variant := range []string{"unknown-kind", "mixed-token", "foreign-purpose", "wrong-profile", "duplicate-lease", "over-capacity", "unknown-wrapper", "unknown-claim", "unknown-resource-scope"} {
		t.Run(variant, func(t *testing.T) {
			consumer := newClaimConsumerFixture(t, 1)
			registry := &brokerIdentityConsumers{Kubernetes: consumer, deliveryContract: func(context.Context) (uint32, error) { return 1, nil }, claimTurn: 1}
			claim := brokerInvocationClaim(t)
			reply := &cp.ClaimIntegrationInvocationsResponse{Claims: []*cp.IntegrationInvocationClaim{claim}}
			switch variant {
			case "unknown-kind":
				claim.CredentialDeliveryKind = 0
			case "mixed-token":
				claim.CredentialRevision = &cp.IntegrationCredentialRevision{Ref: "credential_fixture"}
			case "foreign-purpose":
				claim.BrokerIdentityPurpose = cp.IntegrationBrokerIdentityPurpose_INTEGRATION_BROKER_IDENTITY_PURPOSE_SSH_TARGET
			case "wrong-profile":
				claim.ResourcePermissionProfile = 0
			case "duplicate-lease", "over-capacity":
				second := proto.Clone(claim).(*cp.IntegrationInvocationClaim)
				second.InvocationRef = "inv_fixture02"
				if variant == "over-capacity" {
					second.Lease.Ref = "lease_fixture02"
				}
				reply.Claims = append(reply.Claims, second)
			case "unknown-wrapper":
				reply.ProtoReflect().SetUnknown([]byte{0xa0, 0x06, 1})
			case "unknown-claim":
				claim.ProtoReflect().SetUnknown([]byte{0xa0, 0x06, 1})
			case "unknown-resource-scope":
				claim.ResourceScope = &cp.IntegrationResourceScope{}
				claim.ResourceScope.ProtoReflect().SetUnknown([]byte{0xa0, 0x06, 1})
			}
			owner := &claimOwnerFixture{reply: reply}
			_, adopted, err := claimIntegrationInvocationDelivery(t.Context(), owner, registry, "worker_fixture", 2, func(brokerIdentityInvocationProgress) {})
			if !errors.Is(err, errBrokerIdentityDeliveryUnresolved) || adopted != 0 || len(consumer.adopted) != 0 || owner.calls != 1 {
				t.Fatal("malformed batch partially adopted or lost fatal classification")
			}
			if _, ok := consumer.ReserveClaim(); ok {
				t.Fatal("unresolved owner delivery reopened local admission")
			}
		})
	}
}

// TestBrokerIdentityClaimRequiresContractBeforeOwnerWrite checks zero, future and unavailable readiness without performing Claim, while nil ordinary-only composition advertises no BI capacity.
// TestBrokerIdentityClaimRequiresContractBeforeOwnerWrite проверяет zero, future и unavailable readiness без Claim; nil ordinary-only composition не рекламирует BI capacity.
func TestBrokerIdentityClaimRequiresContractBeforeOwnerWrite(t *testing.T) {
	for _, revision := range []uint32{0, 2} {
		owner := &claimOwnerFixture{}
		registry := &brokerIdentityConsumers{deliveryContract: func(context.Context) (uint32, error) { return revision, nil }}
		if _, _, err := claimIntegrationInvocationDelivery(t.Context(), owner, registry, "worker_fixture", 1, func(brokerIdentityInvocationProgress) {}); err == nil || owner.calls != 0 {
			t.Fatal("unknown startup contract reached Claim")
		}
	}
	unavailableOwner := &claimOwnerFixture{}
	unavailableRegistry := &brokerIdentityConsumers{deliveryContract: func(context.Context) (uint32, error) { return 0, errors.New("synthetic readiness unavailable") }}
	if _, _, err := claimIntegrationInvocationDelivery(t.Context(), unavailableOwner, unavailableRegistry, "worker_fixture", 1, func(brokerIdentityInvocationProgress) {}); err == nil || unavailableOwner.calls != 0 {
		t.Fatal("unavailable startup contract reached Claim")
	}
	missingOwner := &claimOwnerFixture{}
	missingRegistry := &brokerIdentityConsumers{Kubernetes: newClaimConsumerFixture(t, 1)}
	if _, _, err := claimIntegrationInvocationDelivery(t.Context(), missingOwner, missingRegistry, "worker_fixture", 1, func(brokerIdentityInvocationProgress) {}); err == nil || missingOwner.calls != 0 {
		t.Fatal("configured mixed consumer without a startup contract reached Claim")
	}
	owner := &claimOwnerFixture{reply: &cp.ClaimIntegrationInvocationsResponse{}}
	if _, _, err := claimIntegrationInvocationDelivery(t.Context(), owner, nil, "worker_fixture", 1, func(brokerIdentityInvocationProgress) {}); err != nil || owner.calls != 1 || owner.request.Capacity != nil {
		t.Fatal("ordinary-only call changed owner capacity semantics")
	}
}

// TestBrokerIdentityClaimUnknownAcknowledgementStopsActualLoop commits the fixture owner lease before Unavailable, runs the actual common loop, then verifies no second Claim and positive join of separately held known activity cleanup before dependencies close.
// TestBrokerIdentityClaimUnknownAcknowledgementStopsActualLoop фиксирует fixture owner lease до Unavailable, запускает actual common loop и проверяет отсутствие второго Claim и positive join отдельно удерживаемой known activity cleanup до dependency closure.
func TestBrokerIdentityClaimUnknownAcknowledgementStopsActualLoop(t *testing.T) {
	consumer := newClaimConsumerFixture(t, 2)
	known, cancelKnown := context.WithCancel(context.Background())
	consumer.closeActive = cancelKnown
	release, gateErr := consumer.gate.Enter()
	if gateErr != nil {
		t.Fatal("reserve known activity")
	}
	held, joined := make(chan struct{}), make(chan struct{})
	var releaseHeld sync.Once
	defer func() { cancelKnown(); releaseHeld.Do(func() { close(held) }); <-joined }()
	go func() { <-known.Done(); <-held; release(); close(joined) }()
	registry := &brokerIdentityConsumers{Kubernetes: consumer, deliveryContract: func(context.Context) (uint32, error) { return 1, nil }, claimTurn: 1}
	owner := &claimOwnerFixture{err: status.Error(codes.Unavailable, "synthetic acknowledgement lost after commit")}
	control := &controlplaneclient.Client{Runtime: owner, PersonalCredentials: owner}
	metrics, err := businessmetrics.New(observability.NewMetrics("claim_delivery_fixture", "test", nil))
	if err != nil {
		t.Fatal(err)
	}
	worker := runIntegrationLoop(control, nil, &workCycleHealth{}, metrics, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{InstanceID: "worker_fixture", ClaimLimit: 1, PollInterval: time.Millisecond, RequestTimeout: time.Second, OperationTimeout: time.Second}, nil, registry)
	workers := serviceruntime.StartWorkers(t.Context(), worker)
	defer func() {
		workers.Stop()
		cleanup, finish := context.WithTimeout(context.Background(), time.Second)
		defer finish()
		_ = workers.Wait(cleanup)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	observed := workers.Wait(ctx)
	if !workers.Joined() {
		workers.Stop()
		cleanup, finish := context.WithTimeout(context.Background(), time.Second)
		_ = workers.Wait(cleanup)
		finish()
	}
	if !errors.Is(observed, errBrokerIdentityDeliveryUnresolved) || !owner.committed || owner.calls != 1 {
		t.Fatalf("lost owner acknowledgement was retried or hidden; claim calls=%d", owner.calls)
	}
	if _, _, err := claimIntegrationInvocationDelivery(t.Context(), owner, registry, "worker_fixture", 1, func(brokerIdentityInvocationProgress) {}); !errors.Is(err, errBrokerIdentityDeliveryUnresolved) || owner.calls != 1 {
		t.Fatal("closed claimant sent a second Claim")
	}
	closed := 0
	dependency := serviceruntime.ShutdownOperation{Name: "protected client", Timeout: time.Second, Run: func(context.Context) error { closed++; return nil }}
	if err := joinIntegrationWork(context.Background(), 10*time.Millisecond, workers, registry); !errors.Is(err, errIntegrationWorkUnjoined) || !errors.Is(shutdownJoinedIntegrationDependencies(context.Background(), workers, registry, dependency), errIntegrationWorkUnjoined) || closed != 0 {
		t.Fatal("cancellation or timeout replaced known activity join")
	}
	releaseHeld.Do(func() { close(held) })
	<-joined
	if err := joinIntegrationWork(context.Background(), time.Second, workers, registry); !errors.Is(err, errBrokerIdentityDeliveryUnresolved) || errors.Is(err, errIntegrationWorkUnjoined) {
		t.Fatal("positive cleanup join hid the original delivery failure")
	}
	if err := shutdownJoinedIntegrationDependencies(context.Background(), workers, registry, dependency); err != nil || closed != 1 {
		t.Fatal("joined cleanup did not permit dependency closure once")
	}
}

// TestBrokerIdentityClaimOrdinaryRepresentation preserves NONE and TOKEN metadata without creating a second credential selector or reinterpreting missing kind as TOKEN.
// TestBrokerIdentityClaimOrdinaryRepresentation сохраняет NONE и TOKEN metadata без второго credential selector либо вывода TOKEN из missing kind.
func TestBrokerIdentityClaimOrdinaryRepresentation(t *testing.T) {
	definitions, err := integrationpackage.LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"email", "github"} {
		definition := definitions[key]
		health, _ := definition.CapabilityByOperation(definition.Spec.HealthCheck.Operation)
		raw, err := json.Marshal(definition)
		if err != nil {
			t.Fatal(err)
		}
		claim := brokerInvocationClaim(t)
		claim.DefinitionKey, claim.DefinitionVersion, claim.DefinitionDigest, claim.DefinitionPackage = key, definition.Metadata.Version, definition.Digest, raw
		claim.Operation, claim.CapabilityKey = health.Operation, health.Key
		claim.Risk = cp.IntegrationRisk(cp.IntegrationRisk_value["INTEGRATION_RISK_"+health.Risk])
		claim.ApprovalPolicy = cp.IntegrationApprovalPolicy(cp.IntegrationApprovalPolicy_value["INTEGRATION_APPROVAL_POLICY_"+health.ApprovalPolicy])
		claim.BrokerIdentityPurpose, claim.BrokerIdentityRef, claim.BrokerIdentityRevision = 0, "", 0
		claim.ResourcePermissionProfile = 0
		claim.CredentialDeliveryKind = cp.IntegrationCredentialDeliveryKind_INTEGRATION_CREDENTIAL_DELIVERY_KIND_NONE
		if key == "github" {
			claim.CredentialDeliveryKind, claim.CredentialAdmissionRequired = cp.IntegrationCredentialDeliveryKind_INTEGRATION_CREDENTIAL_DELIVERY_KIND_TOKEN, true
		}
		if purpose, err := invocationDeliveryPurpose(claim); err != nil || purpose != 0 {
			t.Fatalf("ordinary representation rejected for %s", key)
		}
		claim.CredentialDeliveryKind = 0
		if _, err := invocationDeliveryPurpose(claim); err == nil {
			t.Fatal("missing kind inferred an ordinary credential representation")
		}
	}
}

// TestBrokerIdentityClaimPartialAttemptRemainsObservable retains the attempted-operation metric even when the same consumer returns malformed terminal metadata; delivery closes instead of hiding an effect or using TOKEN completion.
// TestBrokerIdentityClaimPartialAttemptRemainsObservable сохраняет attempted-operation metric при malformed terminal metadata того же consumer; delivery закрывается без скрытия effect либо TOKEN completion.
func TestBrokerIdentityClaimPartialAttemptRemainsObservable(t *testing.T) {
	consumer := newClaimConsumerFixture(t, 1)
	consumer.mutate = func(progress *brokerIdentityInvocationProgress) {
		progress.State, progress.RemoteOutcome, progress.Attempted = "INVALID_TERMINAL", "UNKNOWN", true
	}
	registry := &brokerIdentityConsumers{Kubernetes: consumer, deliveryContract: func(context.Context) (uint32, error) { return 1, nil }, claimTurn: 1}
	owner := &claimOwnerFixture{reply: &cp.ClaimIntegrationInvocationsResponse{Claims: []*cp.IntegrationInvocationClaim{brokerInvocationClaim(t)}}}
	attempts := 0
	_, _, err := claimIntegrationInvocationDelivery(t.Context(), owner, registry, "worker_fixture", 1, func(progress brokerIdentityInvocationProgress) {
		if !progress.Attempted || progress.RemoteOutcome != "UNKNOWN" {
			t.Fatal("partial effect metadata changed")
		}
		attempts++
	})
	if !errors.Is(err, errBrokerIdentityDeliveryUnresolved) || attempts != 1 || owner.calls != 1 {
		t.Fatal("partial attempted effect was hidden or retried")
	}
}

// TestBrokerIdentityClaimProgressClosedTuple rejects a live outcome disguised as terminal and a successful outcome disguised as an active stream, without promoting a handoff into result authority.
// TestBrokerIdentityClaimProgressClosedTuple отклоняет live outcome под видом terminal и successful outcome под видом active stream без повышения handoff до result authority.
func TestBrokerIdentityClaimProgressClosedTuple(t *testing.T) {
	claim := brokerInvocationClaim(t)
	for _, example := range []struct {
		state, outcome   string
		attempted, valid bool
	}{
		{"RUNNING", "NOT_STARTED", false, true}, {"RUNNING", "SUCCEEDED", false, false}, {"RUNNING", "NOT_STARTED", true, false},
		{"TERMINAL", "UNKNOWN", false, true}, {"TERMINAL", "SUCCEEDED", true, true}, {"TERMINAL", "RUNNING", false, false},
		{"WAITING_OWNER", "NOT_STARTED", false, true}, {"WAITING_OWNER", "SUCCEEDED", true, false},
		{"STREAM_ACTIVE", "RUNNING", true, true}, {"STREAM_ACTIVE", "SUCCEEDED", true, false},
		{"RECONCILING", "UNKNOWN", true, true}, {"RECONCILING", "RUNNING", true, false},
	} {
		progress := brokerIdentityInvocationProgress{InvocationRef: claim.InvocationRef, LeaseRef: claim.Lease.Ref, Generation: claim.Lease.Generation, InputDigest: claim.InputDigest, State: example.state, RemoteOutcome: example.outcome, Attempted: example.attempted}
		if validBrokerInvocationProgress(claim, progress) != example.valid {
			t.Fatal("mixed handoff state/outcome tuple accepted")
		}
	}
}
