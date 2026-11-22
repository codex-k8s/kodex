package platform

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// manualPurgeReceiptReadbackSQL reads a disposable owner's immutable intent without logging it.
// manualPurgeReceiptReadbackSQL читает disposable owner immutable intent без записи в logs.
//
//go:embed testdata/sql/kubernetes_manual_purge_receipt.sql
var manualPurgeReceiptReadbackSQL string

// manualPurgeTerminalSQL checks canonical prepared scrub after a genuinely claimed manual purge.
// manualPurgeTerminalSQL проверяет canonical prepared scrub после genuinely claimed manual purge.
//
//go:embed testdata/sql/kubernetes_manual_purge_terminal.sql
var manualPurgeTerminalSQL string

// manualPurgeProjectSnapshotSQL records only the disposable project's unchanged lifecycle/version.
// manualPurgeProjectSnapshotSQL фиксирует только unchanged lifecycle/version disposable проекта.
//
//go:embed testdata/sql/kubernetes_manual_project_snapshot.sql
var manualPurgeProjectSnapshotSQL string

// manualPurgeInvalidBudgetSQL reaches the typed owner with the real retention database identity.
// manualPurgeInvalidBudgetSQL вызывает typed owner через real retention database identity.
//
//go:embed testdata/sql/kubernetes_manual_purge_invalid_budget.sql
var manualPurgeInvalidBudgetSQL string

// manualPurgeLostDeleteACKStore loses the response after its underlying exact DeleteOnce succeeds.
// It counts irreversible calls independently of Head replay and never falls back to ordinary Delete.
// manualPurgeLostDeleteACKStore теряет ответ после successful underlying exact DeleteOnce.
// Считает irreversible calls отдельно от Head replay и не использует ordinary Delete fallback.
type manualPurgeLostDeleteACKStore struct {
	objectstorage.Store
	deletes, heads, checks int
	unavailable            bool
	deleteVersions         map[string]int
}

// CheckDeleteOnce forwards the concrete adapter capability without granting an attempt.
// CheckDeleteOnce передаёт concrete adapter capability без выдачи attempt.
func (store *manualPurgeLostDeleteACKStore) CheckDeleteOnce(ctx context.Context) error {
	store.checks++
	if store.unavailable {
		return objectstorage.ErrUnavailable
	}
	deleter, ok := store.Store.(objectstorage.SingleAttemptDeleter)
	if !ok {
		return objectstorage.ErrUnavailable
	}
	return deleter.CheckDeleteOnce(ctx)
}

// Head retains the existing immutable-version read path while deletion capability is unavailable.
// Head сохраняет existing immutable-version read path при недоступной deletion capability.
func (store *manualPurgeLostDeleteACKStore) Head(ctx context.Context, key, version string) (objectstorage.Receipt, error) {
	store.heads++
	return store.Store.Head(ctx, key, version)
}

// DeleteOnce executes one exact deletion and makes its first successful result uncertain.
// DeleteOnce выполняет exact deletion и делает первый successful result неопределённым.
func (store *manualPurgeLostDeleteACKStore) DeleteOnce(ctx context.Context, key, version string) error {
	deleter, ok := store.Store.(objectstorage.SingleAttemptDeleter)
	if !ok {
		return objectstorage.ErrUnavailable
	}
	store.deletes++
	if store.deleteVersions == nil {
		store.deleteVersions = make(map[string]int)
	}
	store.deleteVersions[key+"\x00"+version]++
	if err := deleter.DeleteOnce(ctx, key, version); err != nil {
		return err
	}
	if store.deletes == 1 {
		return objectstorage.ErrUnavailable
	}
	return nil
}

// exerciseManualCapturePurge proves the actual service retains unknown intent, rejects stale and
// foreign custody, then finalizes on authoritative absence with one physical DeleteOnce per immutable version.
// exerciseManualCapturePurge проверяет actual service: unknown intent сохраняется, stale/foreign
// custody закрыты, authoritative absence завершает purge с одним physical DeleteOnce на immutable version.
func exerciseManualCapturePurge(t *testing.T, ctx context.Context, repository *Repository, service *platformservice.Service, consumer *pgxpool.Pool, actor, foreign value.Principal, artifactRef, impact string, version int64) {
	t.Helper()
	for _, budget := range [][2]any{{nil, 60}, {1, nil}} {
		_, err := consumer.Exec(ctx, manualPurgeInvalidBudgetSQL, "manual-purge-invalid-budget", budget[0], budget[1])
		var failure *pgconn.PgError
		if !errors.As(err, &failure) || failure.Code != "P0001" {
			t.Fatal("manual retention accepted NULL budget or failed outside owner validation")
		}
	}
	original := repository.objects
	lost := &manualPurgeLostDeleteACKStore{Store: original, unavailable: true}
	repository.objects = lost
	defer func() { repository.objects = original }()
	mutation := value.Mutation{IdempotencyKey: "capture-artifact-purge", ExpectedVersion: &version}
	if _, err := service.PurgeArtifact(ctx, foreign, mutation, artifactRef, impact); !errors.Is(err, errs.ErrNotFound) || lost.deletes != 0 {
		t.Fatal("foreign actor acquired manual capture purge custody", err)
	}
	if lost.checks != 0 {
		t.Fatal("hidden artifact probed deletion capability before eligibility")
	}
	if _, err := service.PurgeArtifact(ctx, actor, mutation, artifactRef, impact); !errors.Is(err, errs.ErrUnavailable) || lost.deletes != 0 {
		t.Fatal("new manual reservation consumed unavailable deletion capability", err)
	}
	normalized, err := repository.ResolvePrincipal(ctx, actor)
	if err != nil {
		t.Fatal("normalize manual purge fixture actor", err)
	}
	current, err := repository.resolveScope(ctx, normalized)
	if err != nil {
		t.Fatal("resolve manual purge fixture owner", err)
	}
	var digest string
	var encoded []byte
	if err := repository.pool.QueryRow(ctx, manualPurgeReceiptReadbackSQL, current.organizationID, current.actorID, mutation.IdempotencyKey).Scan(&digest, &encoded); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("unavailable delete transport left a durable manual intent", err)
	}
	lost.unavailable = false
	if _, err := service.PurgeArtifact(ctx, actor, mutation, artifactRef, impact); !errors.Is(err, errs.ErrUnavailable) || lost.deletes != 1 {
		t.Fatal("manual purge did not preserve unknown one-delete outcome", err)
	}
	if err := repository.pool.QueryRow(ctx, manualPurgeReceiptReadbackSQL, current.organizationID, current.actorID, mutation.IdempotencyKey).Scan(&digest, &encoded); err != nil {
		t.Fatal("read durable manual purge intent", err)
	}
	defer clear(encoded)
	var receipt artifactPurgeReceipt
	decodeErr := json.Unmarshal(encoded, &receipt)
	if decodeErr != nil || receipt.ClaimGeneration < 1 || len(receipt.Objects) != 2 || receipt.ClaimOwner == "" {
		t.Fatalf("manual purge custody receipt changed: malformed=%t generation=%d versions=%d ownerPresent=%t", decodeErr != nil, receipt.ClaimGeneration, len(receipt.Objects), receipt.ClaimOwner != "")
	}
	lost.unavailable = true
	mutation.Operation = "artifact.purge"
	mutation.IntentDigest = digest
	if err := repository.ConfigureProjectPurgeStorage("https://disposable.invalid", "fixture", "fixture", true); err != nil {
		t.Fatal("configure disposable project purge identity", err)
	}
	var projectVersion, afterVersion int64
	var lifecycle, afterLifecycle string
	if err := repository.pool.QueryRow(ctx, manualPurgeProjectSnapshotSQL, current.organizationID, receipt.ProjectID).Scan(&projectVersion, &lifecycle); err != nil {
		t.Fatal("read original manual project", err)
	}
	if _, err := service.Execute(ctx, command.Command{Kind: command.TrashProject, Principal: actor, Mutation: value.Mutation{IdempotencyKey: "manual-purge-unknown-project-trash", ExpectedVersion: &projectVersion}, Payload: command.ProjectLifecycleInput{Ref: receipt.ProjectRef}}); !errors.Is(err, errs.ErrArtifactPurgeReconciliationRequired) {
		t.Fatal("project purge could repeat an uncertain manual Delete", err)
	}
	if err := repository.pool.QueryRow(ctx, manualPurgeProjectSnapshotSQL, current.organizationID, receipt.ProjectID).Scan(&afterVersion, &afterLifecycle); err != nil || afterVersion != projectVersion || afterLifecycle != lifecycle {
		t.Fatal("manual custody blocker changed project eligibility", err)
	}
	reader := actor
	reader.Permission = "platform.query.projects.purge-recovery.get"
	readback, err := service.GetProjectPurgeRecovery(ctx, reader, receipt.ProjectRef)
	if err != nil || readback.ReasonCode != errs.ArtifactPurgeReconciliationReason || readback.UnknownContentCount != 1 || readback.State != "BLOCKED" || readback.CanResolve || !readback.ProjectPresent {
		t.Fatal("manual project blocker lost reachable safe readback", err)
	}
	for _, change := range []func(*artifactPurgeReceipt){
		func(copy *artifactPurgeReceipt) { copy.ClaimOwner = "foreign-owner" },
		func(copy *artifactPurgeReceipt) { copy.ClaimGeneration-- },
	} {
		stale := receipt
		change(&stale)
		if _, _, err := repository.admitManualArtifactDelete(ctx, current, mutation, stale, receipt.Objects[0]); !errors.Is(err, errs.ErrConflict) {
			t.Fatal("stale manual purge custody admitted", err)
		}
	}
	headsBeforeRecovery := lost.heads
	if _, err := service.PurgeArtifact(ctx, actor, mutation, artifactRef, impact); !errors.Is(err, errs.ErrUnavailable) || lost.deletes != 1 || lost.heads != headsBeforeRecovery+1 {
		t.Fatal("started revision lost Head-only recovery or unstarted version bypassed capability", err)
	}
	var attempted int64
	if err := repository.pool.QueryRow(ctx, queryArtifactManualPurgeAttemptCount, pgx.StrictNamedArgs{"organization_id": current.organizationID, "actor_id": current.actorID, "idempotency_key": mutation.IdempotencyKey, "intent_digest": mutation.IntentDigest}).Scan(&attempted); err != nil || attempted != 1 {
		t.Fatal("unavailable new version consumed an irreversible admission", err)
	}
	lost.unavailable = false
	if state, err := service.PurgeArtifact(ctx, actor, mutation, artifactRef, impact); err != nil || state != "PURGED" || lost.deletes != len(receipt.Objects) {
		t.Fatal("manual purge repeated a prior Delete or failed remaining-version finalization", err)
	}
	lost.unavailable = true
	heads, checks := lost.heads, lost.checks
	if state, err := service.PurgeArtifact(ctx, actor, mutation, artifactRef, impact); err != nil || state != "PURGED" || lost.heads != heads || lost.checks != checks || lost.deletes != len(receipt.Objects) {
		t.Fatal("terminal purge replay depended on unavailable storage mutation path", err)
	}
	var terminal, scrubbed bool
	if err := repository.pool.QueryRow(ctx, manualPurgeTerminalSQL, receipt.ArtifactID).Scan(&terminal, &scrubbed); err != nil || !terminal || !scrubbed {
		t.Fatal("manual purge did not scrub exact prepared custody", err)
	}
	if err := repository.pool.QueryRow(ctx, manualPurgeReceiptReadbackSQL, current.organizationID, current.actorID, mutation.IdempotencyKey).Scan(&digest, &encoded); err != nil {
		t.Fatal("read terminal manual intent", err)
	}
	var terminalReceipt artifactPurgeReceipt
	if json.Unmarshal(encoded, &terminalReceipt) != nil || terminalReceipt.LifecycleState != "PURGED" || terminalReceipt.DeleteAttemptCount != 2 || len(terminalReceipt.Objects) != 0 || terminalReceipt.ClaimOwner != "" {
		t.Fatal("manual terminal receipt lost history or retained private custody")
	}
	if len(lost.deleteVersions) != 2 {
		t.Fatal("manual purge lost one immutable object version")
	}
	for _, count := range lost.deleteVersions {
		if count != 1 {
			t.Fatal("manual purge physically deleted one version more than once")
		}
	}
}

// VerifyVersionAbsent preserves only the underlying adapter's explicitly qualified local proof.
// RU: VerifyVersionAbsent сохраняет лишь явно квалифицированный local proof underlying adapter.
func (store *manualPurgeLostDeleteACKStore) VerifyVersionAbsent(ctx context.Context, key, version string) error {
	return objectstorage.VerifyVersionAbsent(ctx, store.Store, key, version)
}
