package draftowner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/secretdrafts"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/service/secretdraft"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
)

type recoveryQueueClient struct {
	ownerStub
	queue          []*cp.RuntimeSecretDraftWork
	retained       map[string]bool
	visited        map[string]int
	starts         []string
	listingFailure error
	cyclePages     int
}

func newRecoveryQueue(t *testing.T, size int) (*recoveryQueueClient, *Owner) {
	t.Helper()
	client := &recoveryQueueClient{retained: map[string]bool{}, visited: map[string]int{}}
	for index := range size {
		work := proto.Clone(workFixture()).(*cp.RuntimeSecretDraftWork)
		work.OperationRef, work.Draft.Ref = fmt.Sprintf("sdop_%06d", index), fmt.Sprintf("drf_%06d", index)
		digest := sha256.Sum256([]byte(work.Draft.Ref))
		work.StagedSecretName = "runtime-secret-draft-" + hex.EncodeToString(digest[:16])
		client.queue = append(client.queue, work)
	}
	owner, err := New(client, "pod_fixture")
	if err != nil {
		t.Fatal("create recovery owner")
	}
	return client, owner
}

func (client *recoveryQueueClient) ListRuntimeSecretDraftRecoveryWork(ctx context.Context, request *cp.ListRuntimeSecretDraftRecoveryWorkRequest, _ ...grpc.CallOption) (*cp.ListRuntimeSecretDraftRecoveryWorkResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if client.listingFailure != nil {
		return nil, client.listingFailure
	}
	token := request.GetPage().GetPageToken()
	client.starts = append(client.starts, token)
	if client.cyclePages > 0 {
		index := (len(client.starts) - 1) % client.cyclePages
		return &cp.ListRuntimeSecretDraftRecoveryWorkResponse{
			Operations: []*cp.RuntimeSecretDraftWork{client.queue[index]},
			Page:       &cp.PageInfo{NextPageToken: fmt.Sprintf("cursor_%03d", index)},
		}, nil
	}
	result := &cp.ListRuntimeSecretDraftRecoveryWorkResponse{Page: &cp.PageInfo{}}
	for _, work := range client.queue {
		if work.OperationRef <= token {
			continue
		}
		if len(result.Operations) == int(request.GetPage().GetPageSize()) {
			result.Page.NextPageToken = result.Operations[len(result.Operations)-1].OperationRef
			break
		}
		result.Operations = append(result.Operations, work)
	}
	return result, nil
}

func (client *recoveryQueueClient) RecoverRuntimeSecretDraftMaterialization(_ context.Context, request *cp.RecoverRuntimeSecretDraftMaterializationRequest, _ ...grpc.CallOption) (*cp.RecoverRuntimeSecretDraftMaterializationResponse, error) {
	for index, work := range client.queue {
		if work.OperationRef != request.OperationRef {
			continue
		}
		client.visited[work.OperationRef]++
		draft := proto.Clone(work.Draft).(*cp.RuntimeSecretDraft)
		draft.State = cp.RuntimeSecretDraftState_RUNTIME_SECRET_DRAFT_STATE_FAILED
		// Контракт companion CP: отсутствие всех эффектов завершает owner intent
		// внутри Recover. KEEP не даёт broker право удалить retained material.
		if !client.retained[work.OperationRef] {
			client.queue = append(client.queue[:index], client.queue[index+1:]...)
		}
		return &cp.RecoverRuntimeSecretDraftMaterializationResponse{
			Draft: draft, OperationState: cp.RuntimeSecretOperationState_RUNTIME_SECRET_OPERATION_STATE_FAILED,
			EncryptedAction:       cp.RuntimeSecretRecoveryAction_RUNTIME_SECRET_RECOVERY_ACTION_KEEP,
			MaterializationAction: cp.RuntimeSecretRecoveryAction_RUNTIME_SECRET_RECOVERY_ACTION_KEEP,
		}, nil
	}
	return nil, secretdrafts.ErrNotFound
}

type recoveryAbsentEncrypted struct{ secretdrafts.EncryptedStore }

func (recoveryAbsentEncrypted) Lookup(context.Context, value.DraftWork) (value.DraftEncryptedDescriptor, error) {
	return value.DraftEncryptedDescriptor{}, secretdrafts.ErrNotFound
}

type recoveryAbsentRuntime struct{ secretdrafts.RuntimeStore }

func (recoveryAbsentRuntime) Lookup(context.Context, value.DraftWork) (value.DraftMaterialization, error) {
	return value.DraftMaterialization{}, secretdrafts.ErrNotFound
}

type recoveryUnusedCipher struct{ secretdrafts.Cipher }
type recoveryUnusedChecker struct{ secretdrafts.Checker }

func recoveryQueueService(t *testing.T, owner *Owner) *secretdraft.Service {
	t.Helper()
	service, err := secretdraft.New(owner, recoveryUnusedCipher{}, recoveryUnusedChecker{}, recoveryAbsentEncrypted{}, recoveryAbsentRuntime{},
		1024, "kodex-system", "kodex-runtime")
	if err != nil {
		t.Fatal("create recovery service")
	}
	return service
}

func TestRecoveryDrainsMoreThanOneThousandThroughOwnerAndService(t *testing.T) {
	client, owner := newRecoveryQueue(t, 2101)
	service := recoveryQueueService(t, owner)
	for cycle, remaining := range []int{1101, 101, 0} {
		if err := service.ReconcileOnce(t.Context()); err != nil || len(client.queue) != remaining {
			t.Fatalf("cycle %d did not reduce durable queue to %d", cycle+1, remaining)
		}
	}
	if len(client.visited) != 2101 || owner.recoveryCursor != "" {
		t.Fatal("recovery lost work or did not wrap")
	}
	for _, count := range client.visited {
		if count != 1 {
			t.Fatal("completed work was repeated")
		}
	}
}

func TestRecoveryCursorPassesRetainedPrefixAndSurvivesRestart(t *testing.T) {
	client, owner := newRecoveryQueue(t, 1001)
	for _, work := range client.queue[:1000] {
		client.retained[work.OperationRef] = true
	}
	service := recoveryQueueService(t, owner)
	if err := service.ReconcileOnce(t.Context()); err != nil || len(client.queue) != 1001 {
		t.Fatal("retained prefix reconciliation failed")
	}
	if err := service.ReconcileOnce(t.Context()); err != nil || len(client.queue) != 1000 || client.visited["sdop_001000"] != 1 {
		t.Fatal("retained prefix starved the tail")
	}
	if err := service.ReconcileOnce(t.Context()); err != nil || client.visited["sdop_000000"] != 2 {
		t.Fatal("retained work was not revisited after wrap")
	}
	// Потеря процесса после List не ACK-ает работу. После restart она читается
	// снова; новый adapter не наследует локальную cursor-подсказку.
	_, restart := newRecoveryQueue(t, 0)
	restart.client = client
	items, err := restart.ListRecovery(t.Context())
	if err != nil || len(items) != 1000 || items[0].OperationRef != "sdop_000000" {
		t.Fatal("restart lost retained work")
	}
}

func TestRecoveryReaderWaitIsCancellableAndCursorErrorResets(t *testing.T) {
	client, owner := newRecoveryQueue(t, 1001)
	if _, err := owner.ListRecovery(t.Context()); err != nil || owner.recoveryCursor == "" {
		t.Fatal("first batch has no continuation")
	}
	owner.recoveryReader <- struct{}{}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if _, err := owner.ListRecovery(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("waiting reader ignored cancellation")
	}
	<-owner.recoveryReader
	client.listingFailure = secretdrafts.ErrUnavailable
	if _, err := owner.ListRecovery(t.Context()); err == nil || owner.recoveryCursor != "" {
		t.Fatal("failed cursor remained authoritative")
	}
	client.listingFailure = nil
	items, err := owner.ListRecovery(t.Context())
	if err != nil || len(items) != 1000 || items[0].OperationRef != "sdop_000000" {
		t.Fatal("failed batch did not retry from owner state")
	}
}

func TestRecoveryRejectsRepeatedCursorAcrossBatchBoundary(t *testing.T) {
	_, stub, owner := nativeFixture(t)
	owner.recoveryCursor = "same-cursor"
	stub.pages = []*cp.ListRuntimeSecretDraftRecoveryWorkResponse{{Operations: []*cp.RuntimeSecretDraftWork{stub.work}, Page: &cp.PageInfo{NextPageToken: "same-cursor"}}}
	if work, err := owner.ListRecovery(t.Context()); !errors.Is(err, secretdrafts.ErrConflict) || len(work) != 0 || owner.recoveryCursor != "" {
		t.Fatal("repeated batch cursor was accepted")
	}
}

func TestRecoveryRejectsCursorCycleLongerThanOneBatch(t *testing.T) {
	client, owner := newRecoveryQueue(t, 13)
	client.cyclePages = 13
	for cycle := 0; cycle < 5; cycle++ {
		work, err := owner.ListRecovery(t.Context())
		if errors.Is(err, secretdrafts.ErrConflict) {
			if len(work) != 0 || owner.recoveryCursor != "" {
				t.Fatal("cycled batch returned work")
			}
			return
		}
		if err != nil || len(work) != 10 {
			t.Fatal("unexpected bounded cycle response")
		}
	}
	t.Fatal("cursor cycle across batches was not detected")
}
