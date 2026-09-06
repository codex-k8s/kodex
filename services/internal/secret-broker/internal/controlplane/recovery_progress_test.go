package controlplane

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"google.golang.org/grpc"
)

type runtimeRecoveryPages struct {
	fakeRuntimeSecretClient
	operations []*cp.RuntimeSecretRecoveryWork
	response   *cp.ListRuntimeSecretRecoveryWorkResponse
	firstToken string
	calls      int
}

func (client *runtimeRecoveryPages) ListRuntimeSecretRecoveryWork(ctx context.Context, request *cp.ListRuntimeSecretRecoveryWorkRequest, _ ...grpc.CallOption) (*cp.ListRuntimeSecretRecoveryWorkResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if client.calls == 0 {
		client.firstToken = request.GetPage().GetPageToken()
	}
	client.calls++
	if client.response != nil {
		return client.response, nil
	}
	response := &cp.ListRuntimeSecretRecoveryWorkResponse{Page: &cp.PageInfo{}}
	for _, item := range client.operations {
		if item.OperationRef <= request.GetPage().GetPageToken() {
			continue
		}
		if len(response.Operations) == int(request.GetPage().GetPageSize()) {
			response.Page.NextPageToken = response.Operations[len(response.Operations)-1].OperationRef
			break
		}
		response.Operations = append(response.Operations, item)
	}
	return response, nil
}

func TestRuntimeSecretRecoveryAdvancesPastBudgetWithoutLosingClaims(t *testing.T) {
	client := &runtimeRecoveryPages{}
	for index := range 2101 {
		client.operations = append(client.operations, &cp.RuntimeSecretRecoveryWork{
			OperationRef: fmt.Sprintf("secop_%06d", index), ClaimantId: "old-pod", ClaimGeneration: 1,
			Namespace: "kodex-runtime", SecretRef: "sec_fixture", TargetRevision: 1, SecretKey: "value",
		})
	}
	owner, err := New(&controlplaneclient.Client{RuntimeSecrets: client}, "pod-fixture")
	if err != nil {
		t.Fatal("create runtime recovery owner")
	}
	for cycle, size := range []int{1000, 1000, 101} {
		client.calls = 0
		work, err := owner.ListRecoveryWork(t.Context())
		if err != nil || len(work) != size || client.calls > 10 {
			t.Fatalf("cycle %d did not return bounded work", cycle+1)
		}
		if work[0].OperationRef != fmt.Sprintf("secop_%06d", cycle*1000) {
			t.Fatal("recovery restarted at retained prefix")
		}
	}
	if owner.recoveryCursor != "" {
		t.Fatal("completed traversal did not wrap")
	}
	// Авторитетные незавершённые claims не исчезают после прохождения cursor.
	work, err := owner.ListRecoveryWork(t.Context())
	if err != nil || len(work) != 1000 || work[0].OperationRef != "secop_000000" {
		t.Fatal("unfinished claims were skipped permanently")
	}
}

func TestRuntimeSecretRecoveryRejectsInvalidCursorAndCancelsWaitingReader(t *testing.T) {
	for _, token := range []string{"same", "bad\x00cursor", strings.Repeat("x", 513)} {
		client := &runtimeRecoveryPages{response: &cp.ListRuntimeSecretRecoveryWorkResponse{
			Operations: []*cp.RuntimeSecretRecoveryWork{{OperationRef: "secop_fixture"}}, Page: &cp.PageInfo{NextPageToken: token},
		}}
		owner, err := New(&controlplaneclient.Client{RuntimeSecrets: client}, "pod-fixture")
		if err != nil {
			t.Fatal("create runtime recovery owner")
		}
		owner.recoveryCursor = "same"
		if work, err := owner.ListRecoveryWork(t.Context()); err == nil || len(work) != 0 || owner.recoveryCursor != "" {
			t.Fatal("invalid cursor returned partial work or remained active")
		}
		owner.recoveryReader <- struct{}{}
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
		_, err = owner.ListRecoveryWork(ctx)
		cancel()
		<-owner.recoveryReader
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal("waiting reader ignored cancellation")
		}
	}
}
