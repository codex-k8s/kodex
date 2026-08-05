package kubernetes

import (
	"context"
	"testing"

	runtimerepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/runtime"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestRuntimeMCPTokenRotatesPerExecutionAndRevokesPredecessor(t *testing.T) {
	client := fake.NewSimpleClientset()
	runner := &Runner{client: client, namespace: "mattercodex-system"}
	firstInput := runtimerepo.RuntimeMCPTokenInput{
		SessionKey: "owner:session", ExecutionID: "11111111-1111-4111-8111-111111111111",
		TurnID: "22222222-2222-4222-8222-222222222222", Attempt: 1,
	}
	first, err := runner.EnsureRuntimeMCPToken(context.Background(), firstInput)
	if err != nil {
		t.Fatalf("EnsureRuntimeMCPToken(first) error = %v", err)
	}
	replayed, err := runner.EnsureRuntimeMCPToken(context.Background(), firstInput)
	if err != nil || replayed.SecretName != first.SecretName || replayed.Integrity.ContentSHA256 != first.Integrity.ContentSHA256 {
		t.Fatalf("exact replay changed immutable credential: %#v, %v", replayed, err)
	}
	secondInput := firstInput
	secondInput.ExecutionID = "33333333-3333-4333-8333-333333333333"
	secondInput.Attempt = 2
	second, err := runner.EnsureRuntimeMCPToken(context.Background(), secondInput)
	if err != nil || second.SecretName == first.SecretName || second.Integrity.ContentSHA256 == first.Integrity.ContentSHA256 {
		t.Fatalf("successor did not rotate credential: first=%#v second=%#v error=%v", first, second, err)
	}
	if err := runner.ReconcileRuntimeMCPTokens(context.Background(), firstInput.SessionKey, second.SecretName); err != nil {
		t.Fatalf("ReconcileRuntimeMCPTokens() error = %v", err)
	}
	if _, err := client.CoreV1().Secrets("mattercodex-system").Get(context.Background(), first.SecretName, metav1.GetOptions{}); err == nil {
		t.Fatal("predecessor MCP bearer remained readable after reconciliation")
	}
	if _, err := client.CoreV1().Secrets("mattercodex-system").Get(context.Background(), second.SecretName, metav1.GetOptions{}); err != nil {
		t.Fatalf("current MCP bearer was removed: %v", err)
	}
}
