package httptransport

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
)

type providerCommandStub struct {
	controlplanev1.PlatformCommandServiceClient
	verify      *controlplanev1.VerifyProviderAccountDeviceAuthorizationRequest
	reauthorize *controlplanev1.ReauthorizeProviderAccountDeviceCodeRequest
	delete      *controlplanev1.DeleteProviderAccountRequest
}

func (stub *providerCommandStub) VerifyProviderAccountDeviceAuthorization(_ context.Context, request *controlplanev1.VerifyProviderAccountDeviceAuthorizationRequest, _ ...grpc.CallOption) (*controlplanev1.VerifyProviderAccountDeviceAuthorizationResponse, error) {
	stub.verify = request
	return &controlplanev1.VerifyProviderAccountDeviceAuthorizationResponse{Account: providerTestAccount()}, nil
}

func (stub *providerCommandStub) ReauthorizeProviderAccountDeviceCode(_ context.Context, request *controlplanev1.ReauthorizeProviderAccountDeviceCodeRequest, _ ...grpc.CallOption) (*controlplanev1.ReauthorizeProviderAccountDeviceCodeResponse, error) {
	stub.reauthorize = request
	return &controlplanev1.ReauthorizeProviderAccountDeviceCodeResponse{Account: providerTestAccount()}, nil
}

func (stub *providerCommandStub) DeleteProviderAccount(_ context.Context, request *controlplanev1.DeleteProviderAccountRequest, _ ...grpc.CallOption) (*controlplanev1.DeleteProviderAccountResponse, error) {
	stub.delete = request
	return &controlplanev1.DeleteProviderAccountResponse{Account: providerTestAccount()}, nil
}

func providerTestAccount() *controlplanev1.ProviderAccount {
	return &controlplanev1.ProviderAccount{Ref: "pacc_primary01", Version: 4, State: controlplanev1.ProviderAccountState_PROVIDER_ACCOUNT_STATE_AUTHORIZED}
}

func TestProviderDeviceAndDeleteMappingsUseExactRPCs(t *testing.T) {
	command := &providerCommandStub{}
	server := &Server{control: &controlplaneclient.Client{Command: command}}
	parameters := generated.VerifyProviderAccountDeviceAuthorizationParams{IfMatch: `"4"`, IdempotencyKey: "provider-request-01"}
	response := httptest.NewRecorder()
	server.VerifyProviderAccountDeviceAuthorization(response, httptest.NewRequest(http.MethodPost, "/", nil), "pacc_primary01", parameters)
	if response.Code != http.StatusOK || command.verify.GetAccountRef() != "pacc_primary01" || command.verify.GetMutation().GetExpectedVersion() != 4 {
		t.Fatalf("verify mapping failed: status=%d request=%v", response.Code, command.verify)
	}

	response = httptest.NewRecorder()
	server.ReauthorizeProviderAccountDeviceCode(response, httptest.NewRequest(http.MethodPost, "/", nil), "pacc_primary01", generated.ReauthorizeProviderAccountDeviceCodeParams{IfMatch: `"4"`, IdempotencyKey: "provider-request-02"})
	if response.Code != http.StatusAccepted || command.reauthorize.GetAccountRef() != "pacc_primary01" {
		t.Fatalf("reauthorize mapping failed: status=%d request=%v", response.Code, command.reauthorize)
	}

	response = httptest.NewRecorder()
	server.DeleteProviderAccount(response, httptest.NewRequest(http.MethodDelete, "/", nil), "pacc_primary01", generated.DeleteProviderAccountParams{IfMatch: `"4"`, IdempotencyKey: "provider-request-03"})
	if response.Code != http.StatusOK || command.delete.GetAccountRef() != "pacc_primary01" {
		t.Fatalf("delete mapping failed: status=%d request=%v", response.Code, command.delete)
	}
}

func TestProviderAccountSafeStatusReasonMatchesOwnerRegistry(t *testing.T) {
	for _, reason := range []string{"AUTHORIZED", "ACCOUNT_DISABLED", "ACCOUNT_REVOKED", "REAUTHORIZATION_REQUIRED", "DEVICE_AUTHORIZATION_PENDING", "CREDENTIAL_CONFIGURATION_REQUIRED", "ACCOUNT_STATE_UNKNOWN", "DEVICE_AUTHORIZATION_EXPIRED", "DEVICE_AUTHORIZATION_FAILED", "CREDENTIAL_MATERIALIZATION_FAILED", "private upstream text", "FUTURE_REASON"} {
		account := providerTestAccount()
		account.SafeStatusReason = reason
		for _, list := range []bool{false, true} {
			var response proto.Message = &controlplanev1.GetProviderAccountResponse{Account: account}
			path := "/api/v1/provider-accounts/pacc_primary01"
			if list {
				response = &controlplanev1.ListProviderAccountsResponse{Accounts: []*controlplanev1.ProviderAccount{account}}
				path = "/api/v1/provider-accounts"
			}
			client := &catalogRPCRecorder{response: response}
			w := httptest.NewRecorder()
			catalogTestHandler(client).ServeHTTP(w, httptest.NewRequest("GET", path, nil))
			if reason == "private upstream text" || reason == "FUTURE_REASON" {
				if w.Code != 502 || strings.Contains(w.Body.String(), reason) {
					t.Fatal("unregistered provider reason leaked")
				}
				continue
			}
			var result generated.ProviderAccount
			if list {
				var page generated.ProviderAccountPage
				if json.Unmarshal(w.Body.Bytes(), &page) != nil || len(page.Items) != 1 {
					t.Fatal("provider page changed")
				}
				result = page.Items[0]
			} else if json.Unmarshal(w.Body.Bytes(), &result) != nil {
				t.Fatal("provider read changed")
			}
			if w.Code != 200 || result.SafeStatusReason == nil || string(*result.SafeStatusReason) != reason {
				t.Fatal("safe owner reason lost")
			}
		}
	}
}
