package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// kubernetesToolStopOperation binds audit and idempotency to the registered closure-only operation.
// kubernetesToolStopOperation связывает аудит и идемпотентность с зарегистрированной операцией закрытия.
const kubernetesToolStopOperation = "platform.integrations.kubernetes.tool.stop"

// queryKubernetesToolView reads safe metadata only after the common original-root owner lock.
// queryKubernetesToolView читает безопасные метаданные после общей проверки исходного инициатора.
//
//go:embed sql/kubernetes_tool__view.sql
var queryKubernetesToolView string

// queryKubernetesToolList restricts candidates to the exact tenant, project, original root and node.
// queryKubernetesToolList ограничивает выборку точными tenant, project, исходным инициатором и узлом.
//
//go:embed sql/kubernetes_tool__list.sql
var queryKubernetesToolList string

// kubernetesToolRecord keeps the audit project ID private and separate from the safe public view.
// kubernetesToolRecord хранит project ID для аудита отдельно от безопасной публичной проекции.
type kubernetesToolRecord struct {
	view            entity.KubernetesToolView
	projectID       string
	decisionGateRef string
}

// readKubernetesToolRecord rejects unknown states and operations without granting execution or reading private input.
// readKubernetesToolRecord отклоняет неизвестные состояния и операции, не выдавая исполнения или приватных данных.
func readKubernetesToolRecord(ctx context.Context, tx pgx.Tx, org, id string) (kubernetesToolRecord, error) {
	var record kubernetesToolRecord
	v := &record.view
	var safeReason string
	err := tx.QueryRow(ctx, queryKubernetesToolView, pgx.StrictNamedArgs{"organization_id": org, "invocation_id": id}).Scan(&v.InvocationRef, &v.RunRef, &v.NodeRef, &v.ConnectionRef, &v.TargetRef, &v.TargetRevision, &v.Operation, &v.State, &v.Version, &v.WaitingForGate, &record.projectID, &record.decisionGateRef, &safeReason)
	if errors.Is(err, pgx.ErrNoRows) {
		return record, errs.ErrNotFound
	}
	if err != nil {
		return record, errs.ErrUnavailable
	}
	_, known := integrationpackage.KubernetesOperationByKey(v.Operation)
	if !known || !v.State.Known() || v.Version < 1 || v.TargetRevision < 1 || v.State.Terminal() && v.WaitingForGate {
		return kubernetesToolRecord{}, errs.ErrUnavailable
	}
	v.SafeReasonCode = kubernetesToolSafeReason(v.Operation, v.State, safeReason)
	v.CaptureStatuses, err = readKubernetesCaptureToolSummaries(ctx, tx, org, id)
	if err != nil {
		return kubernetesToolRecord{}, err
	}
	return record, nil
}

// GetKubernetesTool returns current safe metadata after rechecking original-root visibility, even after credential loss.
// GetKubernetesTool возвращает текущие безопасные метаданные после проверки исходного инициатора, в том числе после отзыва ключа.
func (repository *Repository) GetKubernetesTool(ctx context.Context, p value.Principal, inv string) (result entity.KubernetesToolView, resultErr error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := kubernetesInputAPICaller(p, "platform.integrations.kubernetes.tool.get"); err != nil {
		return result, err
	}
	current, err := repository.resolveScope(ctx, p)
	if err != nil {
		return result, err
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return result, errs.ErrUnavailable
	}
	defer rollbackAssistantLockedRead(ctx, tx, &resultErr)
	id, err := repository.lockPublicKubernetesTool(ctx, tx, current, p, inv)
	if err != nil {
		return result, err
	}
	record, err := readKubernetesToolRecord(ctx, tx, current.organizationID, id)
	if err != nil {
		return result, err
	}
	if tx.Commit(ctx) != nil {
		return result, errs.ErrUnavailable
	}
	return record.view, nil
}

// ListKubernetesTools authorizes every bounded candidate using the same predicate as an exact read.
// It returns no first-child shortcut and never treats an invocation locator as authority.
// ListKubernetesTools проверяет каждого кандидата тем же правилом, что точное чтение.
// Метод не выбирает произвольного первого потомка и не считает ссылку источником полномочий.
func (repository *Repository) ListKubernetesTools(ctx context.Context, p value.Principal, input query.KubernetesToolList) (result entity.KubernetesToolPage, resultErr error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := kubernetesInputAPICaller(p, "platform.integrations.kubernetes.tool.list"); err != nil {
		return result, err
	}
	current, err := repository.resolveScope(ctx, p)
	if err != nil {
		return result, err
	}
	limit := input.Page.Size
	if limit == 0 {
		limit = 20
	}
	if limit < 1 || limit > 50 || input.Page.Token != "" && (!managedkubernetes.ValidLocator(input.Page.Token) || len(input.Page.Token) > 128) {
		return result, errs.ErrInvalid
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return result, errs.ErrUnavailable
	}
	defer rollbackAssistantLockedRead(ctx, tx, &resultErr)
	if err := repository.requireAccess(ctx, tx, current, "run.view", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "RUN", ResourceRef: input.RunRef}); err != nil {
		return result, concealAccessDenial(err, errs.ErrNotFound)
	}
	rows, err := tx.Query(ctx, queryKubernetesToolList, pgx.StrictNamedArgs{"organization_id": current.organizationID, "actor_id": current.actorID, "project_ref": p.ProjectRef, "run_ref": input.RunRef, "node_ref": input.NodeRef, "after_ref": input.Page.Token, "limit": limit + 1})
	if err != nil {
		return result, errs.ErrUnavailable
	}
	var refs []string
	for rows.Next() {
		var ref string
		if rows.Scan(&ref) != nil {
			rows.Close()
			return result, errs.ErrUnavailable
		}
		refs = append(refs, ref)
	}
	rows.Close()
	if rows.Err() != nil {
		return result, errs.ErrUnavailable
	}
	result.Items = make([]entity.KubernetesToolView, 0, len(refs))
	for _, ref := range refs {
		id, err := repository.lockPublicKubernetesTool(ctx, tx, current, p, ref)
		if err != nil {
			return entity.KubernetesToolPage{}, err
		}
		record, err := readKubernetesToolRecord(ctx, tx, current.organizationID, id)
		if err != nil {
			return entity.KubernetesToolPage{}, err
		}
		if len(result.Items) == int(limit) {
			result.Next = result.Items[len(result.Items)-1].InvocationRef
			break
		}
		result.Items = append(result.Items, record.view)
	}
	if tx.Commit(ctx) != nil {
		return entity.KubernetesToolPage{}, errs.ErrUnavailable
	}
	return result, nil
}

// StopKubernetesTool closes the selected invocation and its existing dependency graph in one owner transaction.
// Receipt replay rechecks current visibility before returning metadata. A started uncertain effect remains unknown;
// neither cancellation nor the receipt proves that a remote process stopped. Closure uses Renew/Get and
// List/Get read paths; an eligible completed origin receives the existing TURN_QUEUED continuation event.
// StopKubernetesTool закрывает выбранный вызов и прежний граф зависимостей одной транзакцией владельца.
// Повтор квитанции проверяет текущую видимость. Неопределённый исход начатого действия сохраняется;
// отмена и квитанция не доказывают остановку удалённого процесса. Закрытие читается через Renew/Get
// и List/Get; допустимый завершённый исходный ход получает прежнее событие продолжения TURN_QUEUED.
func (repository *Repository) StopKubernetesTool(ctx context.Context, p value.Principal, mutation value.Mutation, inv string) (result entity.KubernetesToolView, resultErr error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := kubernetesInputAPICaller(p, kubernetesToolStopOperation); err != nil {
		return result, err
	}
	if mutation.ExpectedVersion == nil || *mutation.ExpectedVersion < 1 || !validInputSubmissionKey(mutation.IdempotencyKey) {
		return result, errs.ErrInvalid
	}
	current, err := repository.resolveScope(ctx, p)
	if err != nil {
		return result, err
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return result, errs.ErrUnavailable
	}
	defer rollbackAssistantLockedRead(ctx, tx, &resultErr)
	id, err := repository.lockPublicKubernetesTool(ctx, tx, current, p, inv)
	if err != nil {
		return result, err
	}
	record, err := readKubernetesToolRecord(ctx, tx, current.organizationID, id)
	if err != nil {
		return result, err
	}
	encoded, err := json.Marshal(struct {
		Invocation string
		Version    int64
	}{inv, *mutation.ExpectedVersion})
	if err != nil {
		return result, errs.ErrUnavailable
	}
	sum := sha256.Sum256(encoded)
	intent := hex.EncodeToString(sum[:])
	var digest string
	var stored []byte
	err = tx.QueryRow(ctx, queryCommandsExecuteSelectIdempotencyReceiptsOrganizationIdActorIdOperation, current.organizationID, current.actorID, kubernetesToolStopOperation, mutation.IdempotencyKey).Scan(&digest, &stored)
	if err == nil {
		if digest != intent {
			return result, errs.ErrIdempotencyReuse
		}
		if tx.Commit(ctx) != nil {
			return result, errs.ErrUnavailable
		}
		return record.view, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return result, errs.ErrUnavailable
	}
	if record.view.Version != *mutation.ExpectedVersion {
		return result, errs.ErrVersionMismatch
	}
	changed := !record.view.State.Terminal()
	decisionGateRef := record.decisionGateRef
	if changed {
		tag, err := tx.Exec(ctx, queryKubernetesInvocationCancel, pgx.StrictNamedArgs{"organization_id": current.organizationID, "invocation_id": id})
		if err != nil {
			return result, mapWriteError(err)
		}
		if tag.RowsAffected() != 1 {
			return result, errs.ErrConflict
		}
	}
	record, err = readKubernetesToolRecord(ctx, tx, current.organizationID, id)
	if err != nil {
		return result, err
	}
	if !record.view.State.Terminal() {
		return result, errs.ErrUnavailable
	}
	if changed && record.view.State == "CANCELLED" {
		var decision []string
		if decisionGateRef != "" {
			decision = []string{decisionGateRef}
		}
		if _, err := repository.scheduleIntegrationContinuation(ctx, tx, current, id, record.projectID, decision...); err != nil {
			return result, err
		}
	}
	stored, err = json.Marshal(record.view)
	if err != nil {
		return result, errs.ErrUnavailable
	}
	if _, err = tx.Exec(ctx, queryCommandsExecuteInsertIdempotencyReceiptsOrganizationIdOperationIntentDigest, current.organizationID, current.actorID, kubernetesToolStopOperation, mutation.IdempotencyKey, intent, "KUBERNETES_TOOL", stored); err != nil {
		return result, errs.ErrUnavailable
	}
	auditRef, err := newRef("aud")
	if err != nil {
		return result, errs.ErrUnavailable
	}
	var projectID any
	if record.projectID != "" {
		projectID = record.projectID
	}
	if _, err = tx.Exec(ctx, queryCommandsExecuteInsertAuditEventsRefProjectIdAction, auditRef, current.organizationID, projectID, current.actorID, kubernetesToolStopOperation, "INTEGRATION_INVOCATION", inv, "i18n:KUBERNETES_TOOL_STOPPED", current.correlationRef); err != nil {
		return result, errs.ErrUnavailable
	}
	if tx.Commit(ctx) != nil {
		return result, errs.ErrUnavailable
	}
	return record.view, nil
}
