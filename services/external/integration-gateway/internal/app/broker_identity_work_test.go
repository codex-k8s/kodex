package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/libs/go/observability"
	businessmetrics "github.com/codex-k8s/kodex/services/external/integration-gateway/internal/observability/metrics"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// brokerProbeFixture implements only a typed purpose port and records immutable input without any external effect or owner authority.
// brokerProbeFixture реализует лишь typed purpose port и фиксирует immutable input без external effect либо owner authority.
type brokerProbeFixture struct {
	personal, shared int
	receipt          brokerIdentityProbeReceipt
	err              error
	claim            *cp.IntegrationConnectionTestClaim
}

// ProbePersonal captures a copy and deliberately mutates its owned argument to verify dispatcher isolation.
// ProbePersonal сохраняет копию и намеренно меняет собственный argument для проверки dispatcher isolation.
func (fixture *brokerProbeFixture) ProbePersonal(_ context.Context, claim *cp.IntegrationConnectionTestClaim) (brokerIdentityProbeReceipt, error) {
	fixture.personal++
	fixture.claim = proto.Clone(claim).(*cp.IntegrationConnectionTestClaim)
	claim.TestRef = "pct_consumer_owned_copy"
	return fixture.receipt, fixture.err
}

// ProbeShared captures the original shared claim without running a second Claim or generic Complete.
// ProbeShared сохраняет original shared claim без второго Claim либо generic Complete.
func (fixture *brokerProbeFixture) ProbeShared(_ context.Context, claim *cp.IntegrationConnectionTestClaim) (brokerIdentityProbeReceipt, error) {
	fixture.shared++
	fixture.claim = proto.Clone(claim).(*cp.IntegrationConnectionTestClaim)
	return fixture.receipt, fixture.err
}

// ReserveClaim returns no invocation capacity in this health-only fixture.
// ReserveClaim не выдаёт invocation capacity в health-only fixture.
func (fixture *brokerProbeFixture) ReserveClaim() (brokerIdentityClaimSlot, bool) { return nil, false }

// brokerHealthClaim uses exact shipped Kubernetes package bytes solely to test closed dispatch metadata.
// The fixture grants no admission and does not claim runnable Kubernetes, owner proof or transport readiness.
// brokerHealthClaim использует exact shipped Kubernetes package bytes лишь для проверки closed dispatch metadata.
// Fixture не выдаёт admission и не заявляет runnable Kubernetes, owner proof либо transport readiness.
func brokerHealthClaim(t *testing.T) *cp.IntegrationConnectionTestClaim {
	t.Helper()
	definitions, err := integrationpackage.LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	definition, known := definitions[integrationpackage.KubernetesDefinitionKey]
	if !known || definition.Spec.Readiness != "READY" {
		t.Fatal("source fixture lost its exact shipped profile")
	}
	raw, err := json.Marshal(definition)
	if err != nil {
		t.Fatal(err)
	}
	return &cp.IntegrationConnectionTestClaim{TestRef: "pct_fixture01", ConnectionRef: "int_fixture01",
		DefinitionKey: definition.Metadata.Key, DefinitionVersion: definition.Metadata.Version, DefinitionDigest: definition.Digest, DefinitionPackage: raw,
		CredentialDeliveryKind: cp.IntegrationCredentialDeliveryKind_INTEGRATION_CREDENTIAL_DELIVERY_KIND_BROKER_IDENTITY,
		BrokerIdentityPurpose:  cp.IntegrationBrokerIdentityPurpose_INTEGRATION_BROKER_IDENTITY_PURPOSE_KUBERNETES_TARGET,
		BrokerIdentityRef:      "bir_fixture01", BrokerIdentityRevision: 1, InputDigest: strings.Repeat("a", 64),
		Lease: &cp.WorkLease{Ref: "lea_fixture01", Fence: "fnc_fixture01", Generation: 1, ExpiresAt: timestamppb.New(time.Now().Add(30 * time.Second))}}
}

// TestBrokerIdentityPersonalWorkerNeverUsesTokenProbe checks the actual existing worker before its generic descriptor guard.
// Purpose completion and attempted accounting survive lost owner replies without any generic Begin/Complete call.
// TestBrokerIdentityPersonalWorkerNeverUsesTokenProbe проверяет actual existing worker до generic descriptor guard.
// Purpose completion и attempted accounting сохраняются при lost owner reply без generic Begin/Complete.
func TestBrokerIdentityPersonalWorkerNeverUsesTokenProbe(t *testing.T) {
	for _, scenario := range []string{"success", "owner-cancelled", "lost-completion", "foreign-receipt", "missing-consumer"} {
		t.Run(scenario, func(t *testing.T) {
			claim := brokerHealthClaim(t)
			original := proto.Clone(claim)
			purpose := &brokerProbeFixture{receipt: brokerIdentityProbeReceipt{TestRef: claim.TestRef, State: "SUCCEEDED", Applied: true, Attempted: true, Authenticated: true}}
			registry := &brokerIdentityConsumers{Kubernetes: purpose}
			switch scenario {
			case "owner-cancelled":
				purpose.receipt.State, purpose.receipt.Applied = "CANCELLED", false
			case "lost-completion":
				purpose.err = errors.New("synthetic private owner detail")
			case "foreign-receipt":
				purpose.receipt.TestRef = "pct_foreign"
			case "missing-consumer":
				registry.Kubernetes = nil
			}
			worker := &personalProbeFixture{t: t, claims: &cp.ClaimPersonalIntegrationCredentialTestsResponse{Claims: []*cp.IntegrationConnectionTestClaim{claim}}}
			observed := []bool{}
			processed, err := processPersonalCredentialTests(t.Context(), worker, worker, Config{InstanceID: "personal-fixture-worker", RequestTimeout: time.Second}, func(success bool) { observed = append(observed, success) }, registry)
			wantError := scenario == "lost-completion" || scenario == "foreign-receipt" || scenario == "missing-consumer"
			if (err != nil) != wantError || !proto.Equal(original, claim) || !reflect.DeepEqual(worker.steps, []string{"claim"}) || worker.completion != nil || purpose.shared != 0 {
				t.Fatal("BI health used generic token lifecycle or changed original claim")
			}
			if scenario == "missing-consumer" {
				if processed != 0 || purpose.personal != 0 || len(observed) != 0 {
					t.Fatal("missing purpose fell back to another consumer")
				}
			} else if processed != 1 || purpose.personal != 1 || !reflect.DeepEqual(observed, []bool{true}) {
				t.Fatal("attempted purpose work lost accounting before owner completion")
			}
		})
	}
}

// TestBrokerIdentityHealthRejectsMixedUnknownAndChangedSources validates fresh metadata without interpreting missing kind as TOKEN.
// TestBrokerIdentityHealthRejectsMixedUnknownAndChangedSources проверяет fresh metadata без трактовки missing kind как TOKEN.
func TestBrokerIdentityHealthRejectsMixedUnknownAndChangedSources(t *testing.T) {
	for _, change := range []func(*cp.IntegrationConnectionTestClaim){
		func(c *cp.IntegrationConnectionTestClaim) { c.CredentialDeliveryKind = 0 },
		func(c *cp.IntegrationConnectionTestClaim) { c.CredentialDeliveryKind = 999 },
		func(c *cp.IntegrationConnectionTestClaim) {
			c.CredentialDeliveryKind = cp.IntegrationCredentialDeliveryKind_INTEGRATION_CREDENTIAL_DELIVERY_KIND_TOKEN
		},
		func(c *cp.IntegrationConnectionTestClaim) {
			c.BrokerIdentityPurpose = cp.IntegrationBrokerIdentityPurpose_INTEGRATION_BROKER_IDENTITY_PURPOSE_SSH_TARGET
		},
		func(c *cp.IntegrationConnectionTestClaim) { c.BrokerIdentityPurpose = 999 },
		func(c *cp.IntegrationConnectionTestClaim) { c.BrokerIdentityRef = "" },
		func(c *cp.IntegrationConnectionTestClaim) { c.BrokerIdentityRevision = 0 },
		func(c *cp.IntegrationConnectionTestClaim) { c.DefinitionDigest = strings.Repeat("b", 64) },
		func(c *cp.IntegrationConnectionTestClaim) { c.InputDigest = "" },
		func(c *cp.IntegrationConnectionTestClaim) {
			c.CredentialRevision = &cp.IntegrationCredentialRevision{Ref: "icr_mixed"}
		},
		func(c *cp.IntegrationConnectionTestClaim) { c.CredentialMaterializationRef = "mixed" },
	} {
		claim := brokerHealthClaim(t)
		change(claim)
		if _, err := healthClaimRepresentation(claim); err == nil {
			t.Fatal("mixed or unknown BI source reached purpose dispatch")
		}
	}
}

// TestBrokerIdentitySharedProbePreservesLeaseAndPurpose checks the shared route and closing-only cancellation receipt independently of TOKEN completion.
// TestBrokerIdentitySharedProbePreservesLeaseAndPurpose проверяет shared route и closing-only cancellation receipt отдельно от TOKEN completion.
func TestBrokerIdentitySharedProbePreservesLeaseAndPurpose(t *testing.T) {
	claim := brokerHealthClaim(t)
	purpose := &brokerProbeFixture{receipt: brokerIdentityProbeReceipt{TestRef: claim.TestRef, State: "FAILED", Applied: true, Attempted: true}}
	processed, err := runBrokerIdentityProbe(t.Context(), &brokerIdentityConsumers{Kubernetes: purpose}, claim, false, Config{RequestTimeout: time.Second}, func(bool) {})
	if err != nil || processed != 1 || purpose.shared != 1 || purpose.personal != 0 || !proto.Equal(claim, purpose.claim) {
		t.Fatal("shared BI health changed original lease or purpose route")
	}
	claim.Lease.Generation = 2
	if _, err := runBrokerIdentityProbe(t.Context(), &brokerIdentityConsumers{Kubernetes: purpose}, claim, false, Config{RequestTimeout: time.Second}, func(bool) {}); err == nil || purpose.shared != 1 {
		t.Fatal("expired-generation policy acquired a second BI probe")
	}
}

// brokerSharedRuntimeFixture marks the next existing queue boundary without simulating invocation work or external effects.
// brokerSharedRuntimeFixture отмечает следующую existing queue boundary без имитации invocation work либо external effects.
type brokerSharedRuntimeFixture struct {
	cp.RuntimeWorkServiceClient
	claim                    *cp.IntegrationConnectionTestClaim
	boundary                 error
	completions, invocations int
}

// ClaimIntegrationConnectionTests supplies one exact already-issued health claim through the actual common worker callsite.
// ClaimIntegrationConnectionTests выдаёт exact already-issued health claim через actual common worker callsite.
func (fixture *brokerSharedRuntimeFixture) ClaimIntegrationConnectionTests(context.Context, *cp.ClaimIntegrationConnectionTestsRequest, ...grpc.CallOption) (*cp.ClaimIntegrationConnectionTestsResponse, error) {
	return &cp.ClaimIntegrationConnectionTestsResponse{Claims: []*cp.IntegrationConnectionTestClaim{fixture.claim}}, nil
}

// ClaimIntegrationInvocations stops the fixture after health dispatch, before unrelated work can run.
// ClaimIntegrationInvocations останавливает fixture после health dispatch до unrelated work.
func (fixture *brokerSharedRuntimeFixture) ClaimIntegrationInvocations(context.Context, *cp.ClaimIntegrationInvocationsRequest, ...grpc.CallOption) (*cp.ClaimIntegrationInvocationsResponse, error) {
	fixture.invocations++
	return nil, fixture.boundary
}

// CompleteIntegrationConnectionTest records any forbidden generic BI completion without accepting it.
// CompleteIntegrationConnectionTest фиксирует forbidden generic BI completion без его принятия.
func (fixture *brokerSharedRuntimeFixture) CompleteIntegrationConnectionTest(context.Context, *cp.CompleteIntegrationConnectionTestRequest, ...grpc.CallOption) (*cp.CompleteIntegrationConnectionTestResponse, error) {
	fixture.completions++
	return nil, brokerIdentityWorkUnavailable
}

// TestBrokerIdentitySharedWorkerSkipsGenericCompletion exercises the actual shared worker, including failure and unknown-kind routing.
// TestBrokerIdentitySharedWorkerSkipsGenericCompletion проверяет actual shared worker, включая failure и unknown-kind routing.
func TestBrokerIdentitySharedWorkerSkipsGenericCompletion(t *testing.T) {
	for _, scenario := range []string{"accepted", "lost-owner-completion", "unknown-kind"} {
		t.Run(scenario, func(t *testing.T) {
			claim := brokerHealthClaim(t)
			purpose := &brokerProbeFixture{receipt: brokerIdentityProbeReceipt{TestRef: claim.TestRef, State: "SUCCEEDED", Applied: true, Attempted: true, Authenticated: true}}
			if scenario == "lost-owner-completion" {
				purpose.err = errors.New("synthetic private owner detail")
			}
			if scenario == "unknown-kind" {
				claim.CredentialDeliveryKind = 0
			}
			boundary := errors.New("synthetic next queue boundary")
			runtime := &brokerSharedRuntimeFixture{claim: claim, boundary: boundary}
			personal := &personalProbeFixture{t: t, claims: &cp.ClaimPersonalIntegrationCredentialTestsResponse{}}
			metrics, err := businessmetrics.New(observability.NewMetrics("broker_dispatch_test", "synthetic", nil))
			if err != nil {
				t.Fatal(err)
			}
			processed, err := processIntegrationWork(t.Context(), &controlplaneclient.Client{Runtime: runtime, PersonalCredentials: personal}, nil, metrics,
				slog.New(slog.NewTextHandler(io.Discard, nil)), Config{InstanceID: "personal-fixture-worker", ClaimLimit: 1, RequestTimeout: time.Second}, &brokerIdentityConsumers{Kubernetes: purpose})
			if runtime.completions != 0 || purpose.personal != 0 || err == nil {
				t.Fatal("shared BI work used generic completion or lost owner error")
			}
			if scenario == "unknown-kind" {
				if processed != 0 || purpose.shared != 0 || runtime.invocations != 0 {
					t.Fatal("unknown representation dispatched or completed work")
				}
			} else if processed != 1 || purpose.shared != 1 || (runtime.invocations == 1) != (scenario == "accepted") || errors.Is(err, errBrokerIdentityDeliveryUnresolved) != (scenario == "accepted") {
				t.Fatal("shared purpose result changed queue ordering or completion ownership")
			}
		})
	}
}

// Close records no asynchronous work in the health-only domain fixture.
// Close не владеет asynchronous work в health-only domain fixture.
func (*brokerProbeFixture) Close() error { return nil }

// Wait has nothing asynchronous to join in this health-only fixture.
// Wait не имеет asynchronous work для join в health-only fixture.
func (*brokerProbeFixture) Wait(context.Context) error { return nil }

// Joined never certifies a remote effect; this fixture has no asynchronous activity.
// Joined не подтверждает remote effect; fixture не имеет asynchronous activity.
func (*brokerProbeFixture) Joined() bool { return true }

// Failure is disabled because the fixture has no lifecycle producer.
// Failure отключён, поскольку fixture не имеет lifecycle producer.
func (*brokerProbeFixture) Failure() <-chan struct{} { return nil }
