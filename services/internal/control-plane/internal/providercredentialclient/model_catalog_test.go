package providercredentialclient

import (
	"context"
	"testing"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type catalogClientStub struct {
	controlplanev1.ProviderCredentialMaterializerServiceClient
	response *controlplanev1.ObserveProviderModelCatalogResponse
	calls    int
}

func (stub *catalogClientStub) ObserveProviderModelCatalog(context.Context, *controlplanev1.ObserveProviderModelCatalogRequest, ...grpc.CallOption) (*controlplanev1.ObserveProviderModelCatalogResponse, error) {
	stub.calls++
	return stub.response, nil
}

func catalogTask() platformrepo.ProviderModelCatalogTask {
	return platformrepo.ProviderModelCatalogTask{Ref: "mcattsk_testtask123", AccountRef: cleanupAccountRef, AccountVersion: 3, CredentialRef: "pcred_testrevision123", CredentialRevision: 4, Credential: cleanupCredential, AuthorizationMethod: "API_KEY", ClaimantID: "catalog-worker", ClaimGeneration: 1, Fence: "mcf_testfence123", ExpiresAt: time.Date(2026, 9, 5, 12, 0, 15, 0, time.UTC)}
}

func TestModelCatalogRequestRejectsChangedOwnerBindingBeforeRPC(t *testing.T) {
	stub := &catalogClientStub{}
	client, _ := New(&controlplaneclient.Client{ProviderCredentials: stub})
	base := catalogTask()
	var err error
	base.RequestDigest, err = client.ModelCatalogRequestDigest(base)
	if err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*platformrepo.ProviderModelCatalogTask){
		"account version":     func(task *platformrepo.ProviderModelCatalogTask) { task.AccountVersion++ },
		"credential revision": func(task *platformrepo.ProviderModelCatalogTask) { task.CredentialRevision++ },
		"credential UID": func(task *platformrepo.ProviderModelCatalogTask) {
			task.Credential.SecretUID = "61000000-0000-4000-8000-000000000003"
		},
		"claim":         func(task *platformrepo.ProviderModelCatalogTask) { task.ClaimGeneration++ },
		"expiry":        func(task *platformrepo.ProviderModelCatalogTask) { task.ExpiresAt = task.ExpiresAt.Add(time.Second) },
		"authorization": func(task *platformrepo.ProviderModelCatalogTask) { task.AuthorizationMethod = "DEVICE_CODE" },
	} {
		t.Run(name, func(t *testing.T) {
			task := base
			change(&task)
			if _, err := client.ObserveProviderModelCatalog(t.Context(), task); err == nil {
				t.Fatal("changed binding accepted")
			}
		})
	}
	if stub.calls != 0 {
		t.Fatal("broker reached before owner digest validation")
	}
}

func TestModelCatalogObservationPreservesCapabilitiesAndRejectsUnverifiedModels(t *testing.T) {
	task := catalogTask()
	base := &controlplanev1.ObserveProviderModelCatalogResponse{AccountRef: task.AccountRef, CredentialRevisionRef: task.CredentialRef, ObservedAt: timestamppb.New(task.ExpiresAt.Add(-time.Second)), Source: controlplanev1.ProviderModelCatalogSource_PROVIDER_MODEL_CATALOG_SOURCE_REMOTE_API, Failure: controlplanev1.ProviderModelCatalogFailure_PROVIDER_MODEL_CATALOG_FAILURE_NONE, Models: []*controlplanev1.ProviderModelCatalogRecord{{Id: "future-model", DefaultReasoningEffort: "adaptive", ReasoningEfforts: []string{"adaptive"}}, {Id: "non-reasoning"}}}
	result, err := modelCatalogObservation(task, base)
	if err != nil || len(result.Models) != 2 || result.Models[0].DefaultReasoningEffort != "adaptive" || result.Models[1].DefaultReasoningEffort != "" {
		t.Fatalf("capabilities lost: %+v %v", result, err)
	}
	for name, change := range map[string]func(*controlplanev1.ObserveProviderModelCatalogResponse){
		"unknown source": func(r *controlplanev1.ObserveProviderModelCatalogResponse) { r.Source = 99 },
		"wrong auth source": func(r *controlplanev1.ObserveProviderModelCatalogResponse) {
			r.Source = controlplanev1.ProviderModelCatalogSource_PROVIDER_MODEL_CATALOG_SOURCE_REMOTE_CODEX
		},
		"failure with models": func(r *controlplanev1.ObserveProviderModelCatalogResponse) {
			r.Failure = controlplanev1.ProviderModelCatalogFailure_PROVIDER_MODEL_CATALOG_FAILURE_UNVERIFIED_SOURCE
		},
		"foreign credential": func(r *controlplanev1.ObserveProviderModelCatalogResponse) { r.CredentialRevisionRef = "other" },
		"invented default": func(r *controlplanev1.ObserveProviderModelCatalogResponse) {
			r.Models[1].DefaultReasoningEffort = "none"
		},
		"unsupported default": func(r *controlplanev1.ObserveProviderModelCatalogResponse) {
			r.Models[0].DefaultReasoningEffort = "high"
		},
		"duplicate effort": func(r *controlplanev1.ObserveProviderModelCatalogResponse) {
			r.Models[0].ReasoningEfforts = []string{"adaptive", "adaptive"}
		},
	} {
		t.Run(name, func(t *testing.T) {
			value := proto.Clone(base).(*controlplanev1.ObserveProviderModelCatalogResponse)
			change(value)
			if _, err := modelCatalogObservation(task, value); err == nil {
				t.Fatal("invalid catalog accepted")
			}
		})
	}
}
