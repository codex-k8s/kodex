package platform

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"time"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/libs/go/mailpolicy"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/emailpolicy"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// queryEmailMailboxPublicationClaim Claims only the latest PENDING/READY publication with an
// absent/expired lease under SKIP LOCKED, incrementing generation and assigning a 45-second lease.
// Returns configuration/policy/readback metadata; claiming does not apply Kubernetes resources or
// confirm bridge acceptance.
// queryEmailMailboxPublicationClaim Захватывает только последнюю PENDING/READY публикацию без
// действующей аренды через SKIP LOCKED, увеличивает поколение и назначает 45-секундную аренду.
// Возвращает конфигурацию, policy и метаданные сверки; ресурсы Kubernetes не применяет и принятие
// bridge не подтверждает.
//
//go:embed sql/email_mailbox_publication_claim.sql
var queryEmailMailboxPublicationClaim string

// queryEmailMailboxPublicationStagePolicy Stores a policy document/digest only for exact live
// ref/claimant/generation and matching configuration revision/digest, allowing repeated identical
// policy. It does not publish the policy externally.
// queryEmailMailboxPublicationStagePolicy Сохраняет документ и хеш policy только для точных
// действующих ref, claimant, generation и совпавших ревизии с хешем конфигурации, допуская повтор
// той же policy. Внешнюю публикацию не выполняет.
//
//go:embed sql/email_mailbox_publication_stage_policy.sql
var queryEmailMailboxPublicationStagePolicy string

// queryEmailMailboxPublicationApplied Records application time once for an exact live publication
// claim with a staged policy. The timestamp records caller-confirmed application rather than
// independently checking Kubernetes.
// queryEmailMailboxPublicationApplied Однократно фиксирует время применения точного действующего
// захвата публикации с подготовленной policy. Время отражает подтверждение вызывающего, а не
// самостоятельную проверку Kubernetes.
//
//go:embed sql/email_mailbox_publication_applied.sql
var queryEmailMailboxPublicationApplied string

// queryEmailMailboxPublicationCallback Records bridge readback only for the latest matching
// revision/digest after application, in PENDING/READY state. Exact bridge workload/permission
// validation belongs to the caller; no lease token is inferred from this callback.
// queryEmailMailboxPublicationCallback Фиксирует сверку bridge только для последней совпавшей
// ревизии и хеша после применения в PENDING/READY. Точные workload и разрешение проверяет
// вызывающий; токен аренды из callback не выводится.
//
//go:embed sql/email_mailbox_publication_callback.sql
var queryEmailMailboxPublicationCallback string

// queryEmailMailboxPublicationRelease Clears claimant/lease for exact ref/claimant/generation in
// PENDING/READY without requiring an unexpired lease. Callers intentionally do not require a
// nonzero affected-row count on release.
// queryEmailMailboxPublicationRelease Очищает claimant и аренду точных ref, claimant, generation в
// PENDING/READY без требования неистёкшей аренды. При освобождении вызывающие намеренно не требуют
// ненулевое число изменённых строк.
//
//go:embed sql/email_mailbox_publication_release.sql
var queryEmailMailboxPublicationRelease string

// queryEmailMailboxPublicationFinishLock Locks exact live applied publication and connection,
// projecting expected connection version from observation receipts or the original version plus
// owner/configuration/callback metadata. Callers still validate callback, expiry, OCC and document
// identity before marking readiness.
// queryEmailMailboxPublicationFinishLock Блокирует точные действующую применённую публикацию и
// соединение. Выводит ожидаемую версию соединения из квитанций наблюдения либо исходной версии,
// сведения владельца, конфигурации и callback. Вызывающий дополнительно проверяет callback, срок,
// OCC и документ до готовности.
//
//go:embed sql/email_mailbox_publication_finish_lock.sql
var queryEmailMailboxPublicationFinishLock string

// queryEmailMailboxPublicationReady Marks a live applied/callback-confirmed publication READY,
// preserves the first ready timestamp and clears its exact claim. Callers require one affected row
// after authoritative binding/document readback in the same transaction.
// queryEmailMailboxPublicationReady Переводит действующую применённую публикацию с callback в
// READY, сохраняет первое время готовности и очищает точный захват. Вызывающий требует одну строку
// после авторитетной сверки привязок и документа в той же транзакции.
//
//go:embed sql/email_mailbox_publication_ready.sql
var queryEmailMailboxPublicationReady string

// queryEmailMailboxBindingApply Upserts the resolved connection-to-configuration revision binding,
// incrementing version on conflict. Tenant/connection ownership and publication eligibility must
// already be established by the locked caller path.
// queryEmailMailboxBindingApply Вставляет или обновляет разрешённую привязку соединения к ревизии
// конфигурации, увеличивая версию при конфликте. Организация, владение соединением и пригодность
// публикации заранее проверяются заблокированным путём вызывающего.
//
//go:embed sql/email_mailbox_binding_apply.sql
var queryEmailMailboxBindingApply string

// queryEmailMailboxBindingRemove Deletes only the exact organization/connection mailbox binding.
// It neither deletes the connection nor changes a remote mailbox; caller locks/authority establish
// eligibility.
// queryEmailMailboxBindingRemove Удаляет только точную привязку почтового ящика организации и
// соединения. Соединение и внешний ящик не удаляет; пригодность устанавливают блокировки и
// полномочия вызывающего.
//
//go:embed sql/email_mailbox_binding_remove.sql
var queryEmailMailboxBindingRemove string

// queryEmailMailboxConnectionApply Updates mailbox ID/from-address public configuration under
// exact organization/internal connection/version, without incrementing connection version itself.
// Callers require one affected row and atomically reconcile bindings/readiness.
// queryEmailMailboxConnectionApply Меняет публичные mailbox ID и from-address по точным
// организации, внутреннему ID соединения и версии без самостоятельного увеличения версии
// соединения. Вызывающий требует одну строку и атомарно сверяет привязки с готовностью.
//
//go:embed sql/email_mailbox_connection_apply.sql
var queryEmailMailboxConnectionApply string

// ClaimEmailMailboxPublication Validates a bounded nonempty claimant, reads one claim, validates
// decoded configuration and its exact digest, then assigns claimant locally. Missing rows return
// no work; scan/validation failure is unavailable and may retain partial internal work with the
// error. No external application occurs.
// ClaimEmailMailboxPublication Проверяет ограниченного непустого claimant, читает захват,
// валидирует декодированную конфигурацию и точный хеш, затем назначает claimant локально.
// Отсутствие строки означает отсутствие работы; сбой чтения или проверки даёт Unavailable с
// возможным частичным внутренним work. Внешнего применения нет.
func (repository *Repository) ClaimEmailMailboxPublication(ctx context.Context, claimant string) (entity.EmailMailboxPublicationWork, bool, error) {
	var work entity.EmailMailboxPublicationWork
	if claimant == "" || len(claimant) > 96 {
		return work, false, errs.ErrInvalid
	}
	var raw []byte
	var digest string
	err := repository.pool.QueryRow(ctx, queryEmailMailboxPublicationClaim, claimant).Scan(&work.Ref, &work.State, &work.ClaimGeneration, &raw, &digest, &work.PolicyDocument, &work.Applied, &work.Callback, &work.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return work, false, nil
	}
	if err != nil {
		return work, false, errs.ErrUnavailable
	}
	if api.Decode(raw, &work.Configuration) != nil || api.ValidateConfiguration(work.Configuration) != nil || api.Digest(work.Configuration) != digest {
		return work, false, errs.ErrUnavailable
	}
	work.Claimant = claimant
	return work, true, nil
}

// StageEmailMailboxPolicy Validates the policy and exact work configuration revision/digest,
// bounds marshaled bytes, then requires one staged-policy update under the exact claim. Invalid
// policy is invalid, SQL failure unavailable and a missed update conflict.
// StageEmailMailboxPolicy Проверяет policy и точные ревизию с хешем конфигурации work,
// ограничивает JSON и требует одну запись policy точного захвата. Неверная policy даёт Invalid,
// SQL-сбой — Unavailable, отсутствие изменения — Conflict.
func (repository *Repository) StageEmailMailboxPolicy(ctx context.Context, work entity.EmailMailboxPublicationWork, document mailpolicy.MailDocument) error {
	if document.Validate() != nil || document.ConfigurationRevision != work.Configuration.Revision || document.ConfigurationDigest != api.Digest(work.Configuration) {
		return errs.ErrInvalid
	}
	raw, err := json.Marshal(document)
	if err != nil || len(raw) > mailpolicy.MaximumFileBytes {
		return errs.ErrInvalid
	}
	return emailMailboxPublicationWriteResult(repository.pool.Exec(ctx, queryEmailMailboxPublicationStagePolicy, work.Ref, work.Claimant, work.ClaimGeneration, raw, document.Digest(), document.ConfigurationDigest, document.ConfigurationRevision))
}

// MarkEmailMailboxApplied Requires one application-marker update for the exact
// ref/claimant/generation and staged policy. It maps SQL failure to unavailable and count mismatch
// to conflict; callers own the actual Kubernetes apply/readback.
// MarkEmailMailboxApplied Требует одну запись маркера применения по точным ref, claimant,
// generation и подготовленной policy. SQL-сбой даёт Unavailable, иное число строк — Conflict; само
// применение и сверку Kubernetes выполняет вызывающий.
func (repository *Repository) MarkEmailMailboxApplied(ctx context.Context, work entity.EmailMailboxPublicationWork) error {
	return emailMailboxPublicationWriteResult(repository.pool.Exec(ctx, queryEmailMailboxPublicationApplied, work.Ref, work.Claimant, work.ClaimGeneration))
}

// ReleaseEmailMailboxPublication Releases the exact publication claim, mapping SQL failure to
// unavailable while accepting zero affected rows as before. It does not roll back already applied
// resources or accepted configuration.
// ReleaseEmailMailboxPublication Освобождает точный захват публикации, возвращая Unavailable при
// SQL-сбое и по-прежнему допуская ноль изменённых строк. Уже применённые ресурсы и принятую
// конфигурацию не откатывает.
func (repository *Repository) ReleaseEmailMailboxPublication(ctx context.Context, work entity.EmailMailboxPublicationWork) error {
	_, err := repository.pool.Exec(ctx, queryEmailMailboxPublicationRelease, work.Ref, work.Claimant, work.ClaimGeneration)
	if err != nil {
		return errs.ErrUnavailable
	}
	return nil
}

// ReportEmailConfigurationReadback Requires exact projectless email-bridge report permission and
// resolved scope, then validates positive revision/digest and requires one latest
// applied-publication callback update. The report records bridge readback without granting
// publication authority to arbitrary payload identities.
// ReportEmailConfigurationReadback Требует точное report-разрешение email-bridge без проекта и
// разрешённую область, проверяет положительную ревизию с хешем и одну callback-запись последней
// применённой публикации. Фиксирует сверку bridge без выдачи полномочий произвольной личности из
// payload.
func (repository *Repository) ReportEmailConfigurationReadback(ctx context.Context, principal value.Principal, revision int64, digest string) error {
	if principal.CallerWorkload != "email-bridge" || principal.ProjectRef != "" || principal.Permission != "platform.email.configuration.report" {
		return errs.ErrForbidden
	}
	if _, err := repository.resolveScope(ctx, principal); err != nil {
		return err
	}
	if revision < 1 || !emailpolicy.ValidDigest(digest) {
		return errs.ErrInvalid
	}
	return emailMailboxPublicationWriteResult(repository.pool.Exec(ctx, queryEmailMailboxPublicationCallback, revision, digest))
}

// CompleteEmailMailboxPublication After caller Kubernetes readback, locks the global publication
// boundary and exact live applied claim/connection, verifies callback/expiry/OCC/document
// identity, reconciles kind-specific bindings and accepted document readback, then marks ready
// with audit/event in one transaction. READY replay only releases/commits; existing direct commit
// errors and ignored request-context rollback remain unchanged.
// CompleteEmailMailboxPublication После сверки Kubernetes вызывающим блокирует общую границу
// публикации и точный действующий применённый захват с соединением, проверяет callback, срок, OCC
// и документ. Сверяет привязки по виду и принятый документ, затем пишет READY с аудитом и событием
// одной транзакцией. READY replay лишь освобождает и подтверждает; прямые ошибки commit и
// игнорируемый rollback с контекстом запроса сохраняются.
func (repository *Repository) CompleteEmailMailboxPublication(ctx context.Context, work entity.EmailMailboxPublicationWork) error {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, queryEmailMailboxPublicationLock); err != nil {
		return errs.ErrUnavailable
	}
	var current scope
	var state, connectionID, connectionRef, setID, revisionID, kind, digest string
	var expectedVersion, connectionVersion int64
	var enabled, callback bool
	var expiresAt time.Time
	var raw []byte
	err = tx.QueryRow(ctx, queryEmailMailboxPublicationFinishLock, work.Ref, work.Claimant, work.ClaimGeneration).Scan(
		&state, &current.organizationID, &current.organizationRef, &connectionID, &connectionRef, &expectedVersion, &connectionVersion, &enabled,
		&setID, &revisionID, &kind, &raw, &digest, &callback, &expiresAt, &current.actorID)
	if errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrConflict
	}
	if err != nil {
		return errs.ErrUnavailable
	}
	if state == "READY" {
		if _, err := tx.Exec(ctx, queryEmailMailboxPublicationRelease, work.Ref, work.Claimant, work.ClaimGeneration); err != nil {
			return errs.ErrUnavailable
		}
		return tx.Commit(ctx)
	}
	if !callback || !time.Now().Before(expiresAt) || expectedVersion != connectionVersion || !enabled && kind == "BIND" {
		return errs.ErrConflict
	}
	var configuration api.Configuration
	if api.Decode(raw, &configuration) != nil || api.Digest(configuration) != digest || configuration.Revision != work.Configuration.Revision || api.Digest(work.Configuration) != digest {
		return errs.ErrUnavailable
	}
	mailboxRef, sender := "", ""
	for _, mailbox := range configuration.Mailboxes {
		if mailbox.TenantId == current.organizationRef && mailbox.ConnectionId == connectionRef {
			mailboxRef, sender = mailbox.Id, mailbox.Sender
		}
	}
	if kind == "BIND" && (mailboxRef == "" || sender == "") {
		return errs.ErrUnavailable
	}
	if kind == "BIND" || kind == "UNBIND" {
		tag, err := tx.Exec(ctx, queryEmailMailboxConnectionApply, current.organizationID, connectionID, connectionVersion, mailboxRef, sender)
		if err != nil || tag.RowsAffected() != 1 {
			return errs.ErrConflict
		}
		if kind == "BIND" {
			if _, err := tx.Exec(ctx, queryEmailMailboxBindingApply, current.organizationID, connectionID, setID, revisionID); err != nil {
				return errs.ErrUnavailable
			}
		} else if _, err := tx.Exec(ctx, queryEmailMailboxBindingRemove, current.organizationID, connectionID); err != nil {
			return errs.ErrUnavailable
		}
	}
	if kind == "GIT_SYNC" || kind == "RECOVERY" {
		if err := repository.applyMailboxPublicationBindings(ctx, tx, current, work.Ref, configuration, connectionRef); err != nil {
			return err
		}
	}
	if kind == "RECOVERY" && mailboxRef == "" {
		if _, err := tx.Exec(ctx, queryEmailMailboxBindingRemove, current.organizationID, connectionID); err != nil {
			return errs.ErrUnavailable
		}
	}
	projection, err := emailpolicy.DecodeConfiguration(raw)
	if err != nil {
		return errs.ErrUnavailable
	}
	var accepted bool
	if err := tx.QueryRow(ctx, queryEmailConfigurationAccept, projection.Revision, projection.Digest, asJSON(projection.Mailboxes)).Scan(&accepted); err != nil || !accepted {
		return errs.ErrConflict
	}
	if _, err := tx.Exec(ctx, queryEmailConfigurationDocumentInsert, projection.Revision, projection.Digest, raw); err != nil {
		return errs.ErrUnavailable
	}
	var stored []byte
	var storedDigest string
	if err := tx.QueryRow(ctx, queryEmailConfigurationDocumentRead).Scan(&stored, &storedDigest); err != nil || storedDigest != digest {
		return errs.ErrConflict
	}
	var decoded api.Configuration
	if api.Decode(stored, &decoded) != nil || api.Digest(decoded) != digest {
		return errs.ErrUnavailable
	}
	canonical, _ := json.Marshal(configuration)
	readback, _ := json.Marshal(decoded)
	if !bytes.Equal(canonical, readback) {
		return errs.ErrUnavailable
	}
	tag, err := tx.Exec(ctx, queryEmailMailboxPublicationReady, work.Ref, work.Claimant, work.ClaimGeneration)
	if err != nil || tag.RowsAffected() != 1 {
		return errs.ErrConflict
	}
	current.correlationRef = work.Ref
	if err := auditMailboxOwner(ctx, tx, current, connectionRef, "controlplane.email-mailbox.publication-ready", "i18n:EMAIL_MAILBOX_PUBLICATION_READY"); err != nil {
		return err
	}
	if err := repository.emitPlatformEventSnapshot(ctx, tx, current, "INTEGRATION_CONNECTION_CHANGED", "", connectionRef, "i18n:EMAIL_MAILBOX_PUBLICATION_READY", connectionVersion, "READY"); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// emailMailboxPublicationWriteResult requires one row for stage/applied/callback writes, mapping
// every execution error to unavailable before count validation and zero/multiple rows to conflict.
// Callers retain exact SQL, claim/revision/digest arguments and preceding validation/authority.
// Release and completion use different existing contracts and intentionally do not call this helper.
// emailMailboxPublicationWriteResult требует одну строку записи stage/applied/callback, переводит
// любую ошибку исполнения в Unavailable до проверки числа строк, ноль или несколько — в Conflict.
// Точные SQL, аргументы claim/revision/digest, проверки входа и полномочия остаются у вызывающих.
// Release и complete имеют другие прежние контракты и намеренно не используют эту функцию.
func emailMailboxPublicationWriteResult(tag pgconn.CommandTag, err error) error {
	if err != nil {
		return errs.ErrUnavailable
	}
	if tag.RowsAffected() != 1 {
		return errs.ErrConflict
	}
	return nil
}
