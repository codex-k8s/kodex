package platform

import (
	"context"
	_ "embed"
	"errors"
	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"sync/atomic"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// kubernetesCaptureRepairAbortSQL closes an exact independently issued attempt with a technical
// unknown outcome before any prepared upload. It cannot fabricate a new grant or reset counters.
// kubernetesCaptureRepairAbortSQL закрывает exact independently issued attempt техническим
// unknown outcome до prepared upload. Новый grant либо reset counters не создаются.
//
//go:embed testdata/sql/kubernetes_capture_repair_abort.sql
var kubernetesCaptureRepairAbortSQL string

// kubernetesCaptureRepairClaimSQL uses the independently authenticated database task function.
// kubernetesCaptureRepairClaimSQL вызывает task function independently authenticated database identity.
//
//go:embed testdata/sql/kubernetes_capture_repair_claim.sql
var kubernetesCaptureRepairClaimSQL string

// kubernetesCaptureLostPutStore loses one successful write acknowledgement while preserving exact
// immutable provider versions. It observes publication replay without changing owner eligibility.
// kubernetesCaptureLostPutStore теряет один successful write acknowledgement с сохранением exact
// immutable provider versions. Наблюдает publication replay без изменения owner eligibility.
type kubernetesCaptureLostPutStore struct {
	entered chan struct{}
	release <-chan struct{}
	objectstorage.Store
	puts atomic.Int64
}

// Put delegates the real versioned write once, then reports uncertainty without a provider receipt.
// Put выполняет real versioned write один раз, затем сообщает uncertainty без provider receipt.
func (store *kubernetesCaptureLostPutStore) Put(ctx context.Context, input objectstorage.PutInput) (objectstorage.Receipt, error) {
	count := store.puts.Add(1)
	if count == 1 && store.entered != nil {
		close(store.entered)
		select {
		case <-store.release:
		case <-ctx.Done():
			return objectstorage.Receipt{}, ctx.Err()
		}
	}
	receipt, err := store.Store.Put(ctx, input)
	if err == nil && count == 1 {
		return objectstorage.Receipt{}, objectstorage.ErrUnavailable
	}
	return receipt, err
}

// CheckVersioning preserves the original fixture's explicit immutable-version admission.
// CheckVersioning сохраняет explicit immutable-version admission original fixture.
func (store *kubernetesCaptureLostPutStore) CheckVersioning(ctx context.Context) error {
	checker, ok := store.Store.(objectstorage.VersionedChecker)
	if !ok {
		return objectstorage.ErrUnavailable
	}
	return checker.CheckVersioning(ctx)
}

// CheckDeleteOnce forwards the concrete adapter capability without granting an attempt.
// CheckDeleteOnce передаёт concrete adapter capability без выдачи attempt.
func (store *kubernetesCaptureLostPutStore) CheckDeleteOnce(ctx context.Context) error {
	deleter, ok := store.Store.(objectstorage.SingleAttemptDeleter)
	if !ok {
		return objectstorage.ErrUnavailable
	}
	return deleter.CheckDeleteOnce(ctx)
}

// DeleteOnce preserves the underlying optional exact transport without ordinary Delete fallback.
// DeleteOnce сохраняет underlying optional exact transport без ordinary Delete fallback.
func (store *kubernetesCaptureLostPutStore) DeleteOnce(ctx context.Context, key, version string) error {
	deleter, ok := store.Store.(objectstorage.SingleAttemptDeleter)
	if !ok {
		return objectstorage.ErrUnavailable
	}
	return deleter.DeleteOnce(ctx, key, version)
}

// exerciseKubernetesCaptureRepair proves public discovery, upload permission, OCC, idempotency and
// a monotonic new retry cycle through the real service using the production caller registry.
// Native execution remains closed; the surrounding Seal component subsequently publishes and
// downloads the original accepted prefix using task generation four and one prepared-content PUT.
// exerciseKubernetesCaptureRepair проверяет public discovery, upload permission, OCC, idempotency
// и monotonic retry cycle через real service с production caller registry. Native execution остаётся
// закрытым; окружающий Seal component затем публикует и скачивает original accepted prefix через
// task generation four и один prepared-content PUT.
func exerciseKubernetesCaptureRepair(t *testing.T, ctx context.Context, repository *Repository, service *platformservice.Service, consumer *pgxpool.Pool, actor, publisher value.Principal, ref, digest string, source kubernetesCaptureSource) string {
	t.Helper()
	for generation := int64(1); generation <= 3; generation++ {
		var gotRef, lease, fence, gotDigest string
		var gotGeneration int64
		var expires time.Time
		if err := consumer.QueryRow(ctx, kubernetesCaptureRepairClaimSQL, "capture-repair-failed-worker").Scan(&gotRef, &lease, &fence, &gotGeneration, &gotDigest, &expires); err != nil || gotRef != ref || gotDigest != digest || gotGeneration != generation || !expires.After(time.Now()) {
			t.Fatal("issue exact failed capture task", err)
		}
		if generation == 1 {
			task := value.ManagedKubernetesCaptureTaskBinding{CaptureRef: ref, LeaseRef: lease, Fence: fence, Generation: generation, WorkloadInstance: "capture-repair-failed-worker", SnapshotDigest: digest}
			if _, err := service.FinalizeManagedKubernetesCapture(ctx, publisher, task); !errors.Is(err, errs.ErrUnavailable) {
				t.Fatal("publication did not retain unknown one-PUT outcome", err)
			}
		}
		tag, err := repository.pool.Exec(ctx, kubernetesCaptureRepairAbortSQL, ref, lease, generation)
		if err != nil || tag.RowsAffected() != 1 {
			t.Fatal("close exact failed capture task", err)
		}
	}
	var unused string
	if err := consumer.QueryRow(ctx, kubernetesCaptureRepairClaimSQL, "capture-repair-exhaustion").Scan(&unused); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("exhausted capture received another task", err)
	}
	// A repairable publication must retain its only prepared one-PUT intent; deleting it
	// cannot be reconciled by a future task under the same immutable unique intent.
	// Repairable publication сохраняет единственный prepared one-PUT intent; его удаление
	// не восстанавливается будущим task с тем же immutable unique intent.
	var cleanupKind, cleanupID, cleanupKey, cleanupVersion, cleanupETag, cleanupDigest string
	var cleanupSize, cleanupGeneration int64
	var cleanupUncertain bool
	cleanupErr := consumer.QueryRow(ctx, kubernetesCleanupClaimSQL, "capture-repair-retention-probe").Scan(&cleanupKind, &cleanupID, &cleanupKey, &cleanupVersion, &cleanupETag, &cleanupDigest, &cleanupSize, &cleanupGeneration, &cleanupUncertain)
	if !errors.Is(cleanupErr, pgx.ErrNoRows) {
		t.Fatal("repairable prepared content became cleanup-claimable", cleanupKind, cleanupErr)
	}
	// Public operations are derived from the real client map, not an independent fixture permission.
	// Public operations берутся из real client map, не независимой fixture permission.
	permission := func(method string) string {
		for operation, target := range controlplaneclient.ControlAPIGatewayOperations() {
			if target == method {
				return operation
			}
		}
		t.Fatal("capture public operation is not registered")
		return ""
	}
	reader := actor
	reader.CallerWorkload = "control-api-gateway"
	reader.Permission = permission(cp.ManagedKubernetesCaptureService_GetManagedKubernetesCaptureStatus_FullMethodName)
	read, err := service.GetKubernetesCaptureStatus(ctx, reader, query.KubernetesCaptureStatusRead{CaptureRef: ref})
	if err != nil || read.View.State != enum.KubernetesCaptureWaitingOwner || read.View.AttemptCount != 3 || read.View.RepairGeneration != 0 || len(read.View.Artifacts) != 0 {
		t.Fatal("public capture discovery before first artifact", err)
	}
	toolActor := reader
	toolActor.Permission = "platform.integrations.kubernetes.tool.get"
	tool, err := service.GetKubernetesTool(ctx, toolActor, source.InvocationRef)
	if err != nil || len(tool.CaptureStatuses) != 1 || tool.CaptureStatuses[0].CaptureRef != ref || tool.CaptureStatuses[0].Version != read.View.Version {
		t.Fatal("Tool has no reachable waiting capture", err)
	}
	exerciseKubernetesCaptureHistoryWithoutIntegration(t, ctx, repository, service, actor, ref, source)
	repair := reader
	repair.Permission = permission(cp.ManagedKubernetesCaptureService_RepairManagedKubernetesCapture_FullMethodName)
	stale := read.View.Version - 1
	if _, err := service.RepairKubernetesCapture(ctx, repair, value.Mutation{IdempotencyKey: "capture-repair-stale", ExpectedVersion: &stale}, command.KubernetesCaptureRepair{CaptureRef: ref}); !errors.Is(err, errs.ErrVersionMismatch) {
		t.Fatal("stale capture repair was admitted", err)
	}
	resolvedActor, err := repository.ResolvePrincipal(ctx, actor)
	if err != nil {
		t.Fatal("normalize repair owner fixture", err)
	}
	current, err := repository.resolveScope(ctx, resolvedActor)
	if err != nil {
		t.Fatal("resolve repair owner fixture", err)
	}
	minimal := kubernetesSetupOwnerFixture{repository: repository, pool: repository.pool, current: current, transport: actor}
	restore := limitKubernetesInitiatorToExecutionPermissions(t, ctx, minimal, service, source.ConnectionRef)
	version := read.View.Version
	if _, err := service.RepairKubernetesCapture(ctx, repair, value.Mutation{IdempotencyKey: "capture-repair-no-upload", ExpectedVersion: &version}, command.KubernetesCaptureRepair{CaptureRef: ref}); !errors.Is(err, errs.ErrNotFound) {
		restore()
		t.Fatal("run visibility replaced artifact upload permission", err)
	}
	restore()
	exerciseKubernetesCaptureRepairACL(t, ctx, repository, service, actor, repair, ref, source, version)
	key := "capture-owner-repair-original"
	accepted, err := service.RepairKubernetesCapture(ctx, repair, value.Mutation{IdempotencyKey: key, ExpectedVersion: &version}, command.KubernetesCaptureRepair{CaptureRef: ref})
	if err != nil || accepted.RepairReceipt == nil || accepted.View.State != enum.KubernetesCaptureReady || accepted.View.AttemptCount != 3 || accepted.View.RepairGeneration != 1 || accepted.RepairReceipt.AttemptCount != 3 || accepted.RepairReceipt.Version != version+1 {
		t.Fatal("repair did not preserve original attempt history", err)
	}
	replay, err := service.RepairKubernetesCapture(ctx, repair, value.Mutation{IdempotencyKey: key, ExpectedVersion: &version}, command.KubernetesCaptureRepair{CaptureRef: ref})
	if err != nil || replay.RepairReceipt == nil || *replay.RepairReceipt != *accepted.RepairReceipt {
		t.Fatal("same-key repair allocated another cycle", err)
	}
	recovered, err := service.GetKubernetesCaptureStatus(ctx, reader, query.KubernetesCaptureStatusRead{CaptureRef: ref, RepairIdempotencyKey: key})
	if err != nil || recovered.RepairReceipt == nil || *recovered.RepairReceipt != *accepted.RepairReceipt {
		t.Fatal("lost repair ACK has no original-key metadata read", err)
	}
	nextVersion := accepted.View.Version
	if _, err := service.RepairKubernetesCapture(ctx, repair, value.Mutation{IdempotencyKey: key, ExpectedVersion: &nextVersion}, command.KubernetesCaptureRepair{CaptureRef: ref}); !errors.Is(err, errs.ErrIdempotencyReuse) {
		t.Fatal("repair key accepted a changed intent", err)
	}
	after, err := service.GetKubernetesTool(ctx, toolActor, source.InvocationRef)
	if err != nil || !tool.State.Terminal() || after.State != tool.State || after.Version != tool.Version || after.WaitingForGate {
		t.Fatal("capture repair changed original closed execution truth", err)
	}
	return key
}

// VerifyVersionAbsent preserves only the underlying adapter's explicitly qualified local proof.
// RU: VerifyVersionAbsent сохраняет лишь явно квалифицированный local proof underlying adapter.
func (store *kubernetesCaptureLostPutStore) VerifyVersionAbsent(ctx context.Context, key, version string) error {
	return objectstorage.VerifyVersionAbsent(ctx, store.Store, key, version)
}
