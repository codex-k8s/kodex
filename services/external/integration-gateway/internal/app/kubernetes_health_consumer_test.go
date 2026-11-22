package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"reflect"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/observability"
	sb "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	"github.com/codex-k8s/kodex/services/external/integration-gateway/internal/integration"
	businessmetrics "github.com/codex-k8s/kodex/services/external/integration-gateway/internal/observability/metrics"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

// healthProgressOwner exposes both existing health queues and the original invocation Claim, recording actual common-loop order without creating another selector or SQL authority.
// healthProgressOwner открывает обе existing health queues и original invocation Claim, фиксируя actual common-loop order без нового selector либо SQL authority.
type healthProgressOwner struct {
	cp.RuntimeWorkServiceClient
	cp.PersonalIntegrationCredentialServiceClient
	cp.ManagedKubernetesOwnerServiceClient
	cp.ManagedConfigurationSourceWorkServiceClient
	cp.ManagedConfigurationGitWriteBackWorkServiceClient
	cp.RepositoryBundlePublicationServiceClient
	claim    *cp.IntegrationConnectionTestClaim
	personal bool
	steps    []string
	capacity *cp.IntegrationInvocationDeliveryCapacity
}

// CheckManagedKubernetesOwnerReadiness exposes delivery support while configured execution readiness stays false; the health registry must request only the former.
// CheckManagedKubernetesOwnerReadiness сообщает delivery support при execution readiness=false; health registry обязан запросить только первое.
func (*healthProgressOwner) CheckManagedKubernetesOwnerReadiness(_ context.Context, request *cp.CheckManagedKubernetesOwnerReadinessRequest, _ ...grpc.CallOption) (*cp.CheckManagedKubernetesOwnerReadinessResponse, error) {
	if !request.DeliveryContractOnly {
		return nil, brokerIdentityWorkUnavailable
	}
	return &cp.CheckManagedKubernetesOwnerReadinessResponse{Ready: false, DeliveryContractRevision: 1}, nil
}

// ClaimPersonalIntegrationCredentialTests supplies only the selected original personal test and no generic Begin or Complete implementation.
// ClaimPersonalIntegrationCredentialTests выдаёт лишь выбранный original personal test без generic Begin либо Complete implementation.
func (owner *healthProgressOwner) ClaimPersonalIntegrationCredentialTests(context.Context, *cp.ClaimPersonalIntegrationCredentialTestsRequest, ...grpc.CallOption) (*cp.ClaimPersonalIntegrationCredentialTestsResponse, error) {
	response := &cp.ClaimPersonalIntegrationCredentialTestsResponse{}
	if owner.personal {
		response.Claims = []*cp.IntegrationConnectionTestClaim{owner.claim}
	}
	return response, nil
}

// ClaimIntegrationConnectionTests supplies only the selected shared test; purpose completion stays in the broker path.
// ClaimIntegrationConnectionTests выдаёт лишь выбранный shared test; purpose completion остаётся в broker path.
func (owner *healthProgressOwner) ClaimIntegrationConnectionTests(context.Context, *cp.ClaimIntegrationConnectionTestsRequest, ...grpc.CallOption) (*cp.ClaimIntegrationConnectionTestsResponse, error) {
	response := &cp.ClaimIntegrationConnectionTestsResponse{}
	if !owner.personal {
		response.Claims = []*cp.IntegrationConnectionTestClaim{owner.claim}
	}
	return response, nil
}

// ClaimIntegrationInvocations records ordinary progress after the completed purpose probe; an empty batch performs no business effect.
// ClaimIntegrationInvocations фиксирует ordinary progress после completed purpose probe; пустой batch не выполняет business effect.
func (owner *healthProgressOwner) ClaimIntegrationInvocations(_ context.Context, request *cp.ClaimIntegrationInvocationsRequest, _ ...grpc.CallOption) (*cp.ClaimIntegrationInvocationsResponse, error) {
	owner.steps = append(owner.steps, "ordinary Claim")
	owner.capacity = proto.Clone(request.Capacity).(*cp.IntegrationInvocationDeliveryCapacity)
	return &cp.ClaimIntegrationInvocationsResponse{}, nil
}

// ClaimManagedConfigurationSourceWork keeps later unrelated configuration work empty so the real common cycle can complete without bypassing a queue.
// ClaimManagedConfigurationSourceWork оставляет последующую configuration work пустой, чтобы real common cycle завершился без обхода очереди.
func (*healthProgressOwner) ClaimManagedConfigurationSourceWork(context.Context, *cp.ClaimManagedConfigurationSourceWorkRequest, ...grpc.CallOption) (*cp.ClaimManagedConfigurationSourceWorkResponse, error) {
	return &cp.ClaimManagedConfigurationSourceWorkResponse{}, nil
}

// ClaimManagedConfigurationGitWriteBackWork provides no effect-capable work after ordinary claim progress.
// ClaimManagedConfigurationGitWriteBackWork не выдаёт effect-capable work после ordinary claim progress.
func (*healthProgressOwner) ClaimManagedConfigurationGitWriteBackWork(context.Context, *cp.ClaimManagedConfigurationGitWriteBackWorkRequest, ...grpc.CallOption) (*cp.ClaimManagedConfigurationGitWriteBackWorkResponse, error) {
	return &cp.ClaimManagedConfigurationGitWriteBackWorkResponse{}, nil
}

// ClaimRepositoryBundlePublications closes the final existing queue with an empty valid owner response.
// ClaimRepositoryBundlePublications завершает последнюю existing queue пустым valid owner response.
func (*healthProgressOwner) ClaimRepositoryBundlePublications(context.Context, *cp.ClaimRepositoryBundlePublicationsRequest, ...grpc.CallOption) (*cp.ClaimRepositoryBundlePublicationsResponse, error) {
	return &cp.ClaimRepositoryBundlePublicationsResponse{}, nil
}

// healthProgressBroker models the existing completed-purpose receipt seam, preserving the original binding and never accepting resource invocation or stream/readiness calls. Native owner SQL and TLS authority are qualified separately.
// healthProgressBroker моделирует existing completed-purpose receipt seam, сохраняет original binding и не принимает resource invocation либо stream/readiness calls. Native owner SQL и TLS authority квалифицируются отдельно.
type healthProgressBroker struct {
	sb.ManagedKubernetesServiceClient
	owner         *healthProgressOwner
	authenticated bool
	binding       *sb.ManagedKubernetesInvocationBinding
}

// ExecuteManagedKubernetesOperation returns only a health receipt after the purpose-completion marker, with the canonical bounded object and exact test ref used by the actual adapter validator.
// ExecuteManagedKubernetesOperation возвращает лишь health receipt после purpose-completion marker с canonical bounded object и exact test ref для actual adapter validator.
func (broker *healthProgressBroker) ExecuteManagedKubernetesOperation(_ context.Context, request *sb.ExecuteManagedKubernetesOperationRequest, _ ...grpc.CallOption) (*sb.ExecuteManagedKubernetesOperationResponse, error) {
	broker.binding = proto.Clone(request.Binding).(*sb.ManagedKubernetesInvocationBinding)
	broker.owner.steps = append(broker.owner.steps, "purpose completion")
	raw, _ := json.Marshal(map[string]any{"authenticated": broker.authenticated, "resourcePermissionChecks": "PER_OPERATION_REQUIRED"})
	sum := sha256.Sum256(raw)
	result := &sb.ManagedKubernetesSafeResult{EffectRef: request.Binding.ConnectionTestRef, ResultJson: raw, ResultDigest: hex.EncodeToString(sum[:]), RemoteOutcome: sb.ManagedKubernetesRemoteOutcome_MANAGED_KUBERNETES_REMOTE_OUTCOME_SUCCEEDED}
	if !broker.authenticated {
		result.RemoteOutcome = sb.ManagedKubernetesRemoteOutcome_MANAGED_KUBERNETES_REMOTE_OUTCOME_FAILED
		result.ReasonCode = "HEALTH_CHECK_FAILED"
	}
	return &sb.ExecuteManagedKubernetesOperationResponse{Result: &sb.ExecuteManagedKubernetesOperationResponse_Completed{Completed: result}}, nil
}

// TestKubernetesHealthOnlyPreservesOrdinaryProgress executes real common-loop and adapter paths for both existing health queues and both owner-completed outcomes, without INPUT/OUTPUT source, stream readiness or generic completion. It proves local composition, not a PostgreSQL owner or network-authentication fixture.
// TestKubernetesHealthOnlyPreservesOrdinaryProgress выполняет real common-loop и adapter paths для обеих existing health queues и обоих owner-completed outcomes без INPUT/OUTPUT source, stream readiness и generic completion. Проверяется local composition, не PostgreSQL owner либо network-authentication fixture.
func TestKubernetesHealthOnlyPreservesOrdinaryProgress(t *testing.T) {
	adapter, err := integration.New(integration.Config{CredentialDirectory: t.TempDir(), Timeout: time.Second, ProxyURL: "http://egress-gateway.kodex-system.svc.cluster.local:8080", OpenAPIProxyURL: "http://egress-gateway-openapi.kodex-system.svc.cluster.local:8083", SyntheticBaseURL: "http://integration-synthetic.kodex-system.svc.cluster.local:8080"})
	if err != nil {
		t.Fatal(err)
	}
	for _, personal := range []bool{false, true} {
		for _, authenticated := range []bool{false, true} {
			t.Run(map[bool]string{false: "shared", true: "personal"}[personal]+map[bool]string{false: " failed", true: " succeeded"}[authenticated], func(t *testing.T) {
				claim := brokerHealthClaim(t)
				claim.PublicConfiguration, _ = structpb.NewStruct(map[string]any{"target_ref": "target_fixture"})
				before := proto.Clone(claim)
				owner := &healthProgressOwner{claim: claim, personal: personal}
				broker := &healthProgressBroker{owner: owner, authenticated: authenticated}
				registry, err := newKubernetesHealthRegistry(t.Context(), "gateway_fixture", adapter, broker, owner)
				if err != nil {
					t.Fatal(err)
				}
				consumer, _ := registry.Kubernetes.(*kubernetesConsumer)
				t.Cleanup(func() {
					if consumer != nil {
						_ = consumer.Close()
						if consumer.Wait(context.Background()) != nil || !consumer.Joined() {
							t.Error("health-only consumer failed to join")
						}
					}
				})
				if checkBrokerIdentityDelivery(t.Context(), registry) != nil {
					t.Fatal("health startup required execution readiness")
				}
				metrics, err := businessmetrics.New(observability.NewMetrics("health_delivery_fixture", "test", nil))
				if err != nil {
					t.Fatal(err)
				}
				processed, err := processIntegrationWork(t.Context(), &controlplaneclient.Client{Runtime: owner, PersonalCredentials: owner, ConfigurationSources: owner, ConfigurationWriteBacks: owner, RepositoryBundles: owner}, adapter, metrics, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{InstanceID: "gateway_fixture", ClaimLimit: 1, RequestTimeout: time.Second}, registry)
				if err != nil || processed != 1 || !reflect.DeepEqual(owner.steps, []string{"purpose completion", "ordinary Claim"}) {
					t.Fatal("health-only work prevented ordinary claim progress", err)
				}
				if owner.capacity == nil || owner.capacity.Ordinary != 1 || owner.capacity.Kubernetes != 0 || owner.capacity.Ssh != 0 {
					t.Fatal("health-only consumer advertised execution capacity")
				}
				expected := &sb.ManagedKubernetesInvocationBinding{WorkloadInstance: "gateway_fixture", ConnectionTestRef: claim.TestRef, LeaseRef: claim.Lease.Ref, Fence: claim.Lease.Fence, Generation: claim.Lease.Generation, InputDigest: claim.InputDigest}
				if !proto.Equal(expected, broker.binding) || !proto.Equal(before, claim) {
					t.Fatal("health-only consumer changed original claim lineage")
				}
				if consumer == nil {
					t.Fatal("health consumer was not registered")
				}
				if slot, available := consumer.ReserveClaim(); available || slot != nil {
					t.Fatal("health-only consumer admitted a resource operation")
				}
			})
		}
	}
}

// TestKubernetesHealthOnlyBrokerOperationProfile permits exactly the existing SSH projection and typed health method, retaining fresh-map ownership and excluding stream/readiness capabilities.
// TestKubernetesHealthOnlyBrokerOperationProfile разрешает ровно existing SSH projection и typed health method, сохраняет fresh-map ownership и исключает stream/readiness capabilities.
func TestKubernetesHealthOnlyBrokerOperationProfile(t *testing.T) {
	expected := controlplaneclient.SSHComposeCredentialProjectionOperations()
	expected["kubernetes.execute"] = sb.ManagedKubernetesService_ExecuteManagedKubernetesOperation_FullMethodName
	actual := integrationGatewayHealthBrokerOperations()
	if !reflect.DeepEqual(actual, expected) {
		t.Fatal("health wiring changed protected broker operation profile")
	}
	actual["forged"] = "/forged"
	if !reflect.DeepEqual(integrationGatewayHealthBrokerOperations(), expected) {
		t.Fatal("broker profile aliases mutable caller state")
	}
}

// TestKubernetesHealthOnlyLeavesSSHProducerClosed proves the KUBE-only constructor cannot implicitly register another purpose, even when SSH definitions exist in the assembled source. Common composition must add its explicit SSH health or execution consumer.
// TestKubernetesHealthOnlyLeavesSSHProducerClosed доказывает отсутствие implicit регистрации другого purpose в KUBE-only constructor даже при наличии SSH definitions в assembled source. Общая composition обязана добавить explicit SSH health либо execution consumer.
func TestKubernetesHealthOnlyLeavesSSHProducerClosed(t *testing.T) {
	adapter, err := integration.New(integration.Config{CredentialDirectory: t.TempDir(), Timeout: time.Second, ProxyURL: "http://egress-gateway.kodex-system.svc.cluster.local:8080", OpenAPIProxyURL: "http://egress-gateway-openapi.kodex-system.svc.cluster.local:8083", SyntheticBaseURL: "http://integration-synthetic.kodex-system.svc.cluster.local:8080"})
	if err != nil {
		t.Fatal(err)
	}
	owner := &healthProgressOwner{}
	registry, err := newKubernetesHealthRegistry(t.Context(), "gateway_fixture", adapter, &healthProgressBroker{owner: owner}, owner)
	if err != nil || registry.SSH != nil {
		t.Fatal("KUBE-only factory registered an implicit SSH purpose")
	}
	if registry.Kubernetes.Close() != nil || registry.Kubernetes.Wait(context.Background()) != nil || !registry.Kubernetes.Joined() {
		t.Fatal("KUBE health factory did not join")
	}
}
