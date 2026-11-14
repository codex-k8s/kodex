package telegram

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"google.golang.org/protobuf/types/known/structpb"
)

// TestDeployedOperationBudgetReachesHealthAndSend exercises the actual deployment timeout and every accepted budget boundary through provider preflight and committed Begin, with each token-bearing call bounded independently.
// TestDeployedOperationBudgetReachesHealthAndSend проверяет фактический deployment timeout и границы допустимого бюджета через provider preflight и committed Begin с отдельным ограничением каждого token-bearing call.
func TestDeployedOperationBudgetReachesHealthAndSend(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "..", "deploy", "k8s", "base", "interaction-gateway", "configmap.yaml"))
	if err != nil {
		t.Fatal("read deployment operation budget", err)
	}
	values := regexp.MustCompile(`(?m)^  INTERACTION_GATEWAY_OPERATION_TIMEOUT: ([^\s]+)$`).FindAllStringSubmatch(string(raw), -1)
	if len(values) != 1 {
		t.Fatal("exact deployment operation budget is missing")
	}
	deployed, err := time.ParseDuration(values[0][1])
	if err != nil {
		t.Fatal("deployment operation budget is invalid")
	}
	for _, timeout := range []time.Duration{deployed, time.Second, 20 * time.Second, 2 * time.Minute} {
		t.Run(timeout.String(), func(t *testing.T) {
			adapter, claim := telegramAdapterBudgetFixture(t, timeout)
			calls := []string{}
			provider := telegramProviderFixture(t, &calls, http.StatusOK)
			adapter.newTransport = func() http.RoundTripper {
				return fixtureTransport(func(request *http.Request) (*http.Response, error) {
					deadline, ok := request.Context().Deadline()
					if !ok || time.Until(deadline) > min(timeout, 20*time.Second)+50*time.Millisecond {
						t.Fatal("provider call escaped its finite budget")
					}
					return provider.RoundTrip(request)
				})
			}
			configuration := map[string]any{}
			for key, value := range claim.GetTelegram().GetConfiguration() {
				configuration[key] = value
			}
			public, err := structpb.NewStruct(configuration)
			if err != nil {
				t.Fatal("prepare synthetic health configuration")
			}
			health := &cp.IntegrationConnectionTestClaim{DefinitionKey: claim.GetDefinitionKey(), DefinitionVersion: claim.GetDefinitionVersion(), DefinitionDigest: claim.GetDefinitionDigest(),
				DefinitionPackage: claim.GetDefinitionPackage(), CredentialRevision: claim.GetCredentialDescriptor(), PublicConfiguration: public}
			if _, err := adapter.TestConnection(t.Context(), health); err != nil {
				t.Fatalf("deployed health budget rejected before provider: %v; calls=%v", err, calls)
			}
			receipt, err := adapter.Deliver(t.Context(), claim, func(context.Context, *cp.BeginInteractionDeliveryRequest) (*cp.BeginInteractionDeliveryResponse, error) {
				calls = append(calls, "BEGIN")
				return &cp.BeginInteractionDeliveryResponse{DeliveryRef: claim.GetDeliveryRef(), InputDigest: claim.GetTelegram().GetInputDigest(), Allowed: true}, nil
			})
			if err != nil || receipt.GetMessageId() != 123 || adapter.timeout != timeout {
				t.Fatalf("deployed send budget failed or changed outer operation budget: %v", err)
			}
			if !slices.Equal(calls, []string{"getMe", "getChat", "getChatMember", "getMe", "getChat", "getChatMember", "BEGIN", "sendMessage"}) {
				t.Fatal("deployed operation omitted fresh preflight or exact Begin/send order")
			}
		})
	}
}
