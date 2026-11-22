package app

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	sharedhttp "github.com/codex-k8s/kodex/libs/go/httpserver"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/service/kubernetesoperation"
	kubernetesstore "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/kubernetes"
	"google.golang.org/grpc"
	"k8s.io/apimachinery/pkg/runtime"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

// identityHealthReadinessOwner models actual f1 readiness semantics: normal execution is false without OUTPUT; only the typed core profile may report the separately selected core result.
// identityHealthReadinessOwner моделирует actual f1 semantics: normal execution=false без OUTPUT; только typed core profile возвращает отдельно выбранный core result.
type identityHealthReadinessOwner struct {
	cp.ManagedKubernetesOwnerServiceClient
	ready              bool
	err                error
	revision           uint32
	calls, normalCalls int
}

// CheckManagedKubernetesOwnerReadiness preserves errors and rejects delivery-only substitution for mandatory core checks; normal readiness stays false independently from core readiness.
// CheckManagedKubernetesOwnerReadiness сохраняет errors и отвергает delivery-only substitution обязательных core checks; normal readiness=false независимо от core readiness.
func (owner *identityHealthReadinessOwner) CheckManagedKubernetesOwnerReadiness(_ context.Context, request *cp.CheckManagedKubernetesOwnerReadinessRequest, _ ...grpc.CallOption) (*cp.CheckManagedKubernetesOwnerReadinessResponse, error) {
	owner.calls++
	if request.DeliveryContractOnly {
		return nil, errors.New("core readiness was skipped")
	}
	if !request.IdentityHealthOnly {
		owner.normalCalls++
		return &cp.CheckManagedKubernetesOwnerReadinessResponse{Ready: false, DeliveryContractRevision: owner.revision}, owner.err
	}
	return &cp.CheckManagedKubernetesOwnerReadinessResponse{Ready: owner.ready, DeliveryContractRevision: owner.revision}, owner.err
}

// TestKubernetesIdentityStartupUsesCoreHealthReadiness exercises the actual setup component checker with local stream admission and the real Secret store adapter. Missing OUTPUT cannot stop bootstrap, while false core, incompatible owner, infrastructure, store and local-lifecycle failures remain closed.
// TestKubernetesIdentityStartupUsesCoreHealthReadiness проверяет actual setup component checker с local stream admission и real Secret store adapter. Отсутствие OUTPUT не блокирует bootstrap; corefalse, incompatible owner, infrastructure, store и local-lifecycle failures остаются закрытыми.
func TestKubernetesIdentityStartupUsesCoreHealthReadiness(t *testing.T) {
	for _, scenario := range []string{"core ready OUTPUT absent", "core false", "core unavailable", "missing delivery", "store unavailable", "local admission closed"} {
		t.Run(scenario, func(t *testing.T) {
			owner := &identityHealthReadinessOwner{ready: true, revision: 1}
			client := kubernetesfake.NewSimpleClientset()
			if scenario == "store unavailable" {
				client.PrependReactor("get", "secrets", func(ktesting.Action) (bool, runtime.Object, error) {
					return true, nil, errors.New("synthetic store unavailable")
				})
			}
			store, err := kubernetesstore.New(client, "kodex-integration-identities")
			if err != nil {
				t.Fatal(err)
			}
			gate, err := sharedhttp.NewRequestGate(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), 1)
			if err != nil {
				t.Fatal(err)
			}
			streams, err := kubernetesoperation.NewStreamOperations(t.Context(), &blockedStreamOwner{}, unusedStreamPreparation{}, 1)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				_ = gate.Close()
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				_ = streams.Close(ctx)
			})
			upload := &kubernetesIdentityUpload{store: store, gate: gate, streams: streams, control: &controlplaneclient.Client{KubernetesOperations: owner}}
			switch scenario {
			case "core false":
				owner.ready = false
			case "core unavailable":
				owner.err = errors.New("synthetic owner unavailable")
			case "missing delivery":
				owner.revision = 0
			case "local admission closed":
				_ = gate.Close()
			}
			err = upload.Check(t.Context())
			if (err == nil) != (scenario == "core ready OUTPUT absent") || owner.normalCalls != 0 {
				t.Fatal("bootstrap confused core health and full execution readiness", err)
			}
			if scenario != "local admission closed" && owner.calls != 1 {
				t.Fatal("core owner check was skipped or retried")
			}
		})
	}
}
