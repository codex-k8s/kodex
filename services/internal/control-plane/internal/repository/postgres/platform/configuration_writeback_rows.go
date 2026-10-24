package platform

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

var (
	// queryWriteBackBase embeds the tenant/source/version/commit/content-pinned accepted raw content
	// query; it requires READY and does not read a provider.
	// queryWriteBackBase встраивает чтение принятого исходного содержимого по
	// tenant/source/version/commit/content; требует READY и не обращается к провайдеру.
	//go:embed sql/configuration_writeback__base.sql
	queryWriteBackBase string
	// queryWriteBackInsert embeds insertion of immutable proposal/source/content pins in
	// WAITING_APPROVAL, returning the server ID and timestamps without approval or external effects.
	// queryWriteBackInsert встраивает создание неизменяемых proposal/source/content pins в
	// WAITING_APPROVAL; возвращает серверные ID и время без одобрения или внешних эффектов.
	//go:embed sql/configuration_writeback__insert.sql
	queryWriteBackInsert string
	// queryWriteBackLock embeds the exact tenant/proposal join through configuration, source,
	// connection and credential, locking only the work row; decoder checks follow separately.
	// queryWriteBackLock встраивает точное tenant/proposal соединение с configuration, source,
	// connection и credential, блокируя только строку work; проверки decoder выполняются отдельно.
	//go:embed sql/configuration_writeback__lock.sql
	queryWriteBackLock string
	// queryWriteBackUpdate embeds the tenant/ID/version-guarded mutable work update and version
	// increment, leaving the immutable snapshot intact.
	// queryWriteBackUpdate встраивает обновление изменяемой работы с tenant/ID/version guard и
	// увеличением версии, сохраняя неизменяемый snapshot.
	//go:embed sql/configuration_writeback__update.sql
	queryWriteBackUpdate string
	// queryWriteBackList embeds ordered proposal-reference pagination inside one tenant/configuration;
	// it returns references for subsequent locked decoding, not content or authority.
	// queryWriteBackList встраивает упорядоченную пагинацию ссылок предложений внутри
	// tenant/configuration; возвращает ссылки для последующего чтения с блокировкой, а не содержимое
	// или полномочия.
	//go:embed sql/configuration_writeback__list.sql
	queryWriteBackList string
	// queryWriteBackCount embeds the total proposal count for the exact tenant/configuration,
	// including historical terminal proposals.
	// queryWriteBackCount встраивает общий счётчик предложений точного tenant/configuration, включая
	// исторические завершённые предложения.
	//go:embed sql/configuration_writeback__count.sql
	queryWriteBackCount string
	// queryWriteBackCandidates embeds bounded updated-time/ID selection of expired approvals, queued
	// work and expired/null claims; selection does not grant authority or lock the selected rows.
	// queryWriteBackCandidates встраивает ограниченный выбор по updated-time/ID для истёкших
	// одобрений, очереди и истёкших/null claims; выбор не выдаёт полномочия и не блокирует выбранные
	// строки.
	//go:embed sql/configuration_writeback__candidates.sql
	queryWriteBackCandidates string
)

// writeBackSnapshot stores the immutable proposal, source work and base/proposed text used for
// approval and integrity hashes; mutable lifecycle columns are overlaid only after validation.
// writeBackSnapshot хранит неизменяемые proposal, source work и исходный/предлагаемый текст для
// approval и integrity hashes; изменяемые lifecycle columns накладываются только после проверки.
type writeBackSnapshot struct {
	Proposal                     entity.ConfigurationWriteBack
	Source                       entity.ManagedConfigurationSourceWork
	BaseContent, ProposedContent string
}

// writeBackReceipt binds one action digest to its historical proposal response for replay; the
// receipt alone does not authorize another external effect.
// writeBackReceipt связывает digest действия с историческим ответом proposal для replay; сама
// квитанция не разрешает новый внешний эффект.
type writeBackReceipt struct {
	Digest   string
	Proposal entity.ConfigurationWriteBack
}

// writeBackRow combines decoded immutable input, current proposal/lease/effect columns, actor
// references and receipt history under a work-row lock; callers retain transaction ownership.
// writeBackRow объединяет декодированный неизменяемый ввод, текущие proposal/lease/effect columns,
// ссылки actors и историю квитанций под блокировкой work; транзакцией владеют callers.
type writeBackRow struct {
	id, rootRef, approverRef, organizationRef, projectRef string
	snapshot                                              writeBackSnapshot
	proposal                                              entity.ConfigurationWriteBack
	lease                                                 entity.ConfigurationWriteBackLease
	effect, tree, blob                                    string
	started, deadline                                     *time.Time
	receipts                                              map[string]writeBackReceipt
}

// writeBackDigest returns lowercase SHA-256 of the supplied bytes, including nil; JSON encoding,
// content validation and authority belong to callers.
// writeBackDigest возвращает строчный SHA-256 переданных bytes, включая nil; JSON-кодирование,
// проверка содержимого и полномочия принадлежат callers.
func writeBackDigest(raw []byte) string {
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

// lockWriteBack locks the tenant/proposal row, strictly decodes one snapshot JSON value, decodes
// receipts and verifies re-encoded snapshot/content hashes and relational pins. It overlays
// current lifecycle fields and nullable lease expiry only after these checks; no rows maps to
// NotFound and other failures to Unavailable, possibly with a partial internal row.
// lockWriteBack блокирует строку tenant/proposal, строго декодирует одно snapshot JSON value,
// декодирует receipts и сверяет hashes повторно закодированного snapshot/содержимого и relational
// pins. Накладывает текущие lifecycle fields и nullable lease expiry после проверок; отсутствие
// строки даёт NotFound, остальные ошибки Unavailable, возможно с частичной внутренней строкой.
func lockWriteBack(ctx context.Context, tx pgx.Tx, current scope, ref string) (writeBackRow, error) {
	var row writeBackRow
	var raw, receipts []byte
	var digest, approval, configurationRef, sourceRef, connectionRef, credentialRef string
	var lease *time.Time
	row.lease.ProposalRef = ref
	p := &row.proposal
	err := tx.QueryRow(ctx, queryWriteBackLock, current.organizationID, ref).Scan(&row.id, &raw, &digest, &approval, &p.Version, &p.State, &row.effect,
		&row.lease.Attempt, &row.lease.ClaimGeneration, &row.lease.Claimant, &row.lease.Fence, &lease,
		&p.CandidateCommitSHA, &row.tree, &row.blob, &row.started, &p.BranchConfirmedAt, &p.PullRequestConfirmedAt, &p.PullRequestRef, &p.PullRequestURL,
		&p.FailureCode, &receipts, &p.ApprovedAt, &row.deadline, &p.ExpiresAt, &p.CompletedAt, &p.CreatedAt,
		&row.rootRef, &row.approverRef, &row.organizationRef, &configurationRef, &row.projectRef, &sourceRef, &connectionRef, &credentialRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return row, errs.ErrNotFound
	}
	if err != nil {
		return row, errs.ErrUnavailable
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&row.snapshot) != nil || decoder.Decode(&struct{}{}) != io.EOF || json.Unmarshal(receipts, &row.receipts) != nil || writeBackDigest(asJSON(row.snapshot)) != digest {
		return row, errs.ErrUnavailable
	}
	immutable := row.snapshot.Proposal
	if immutable.Ref != ref || immutable.ConfigurationRef != configurationRef || immutable.SourceRef != sourceRef || immutable.ConnectionRef != connectionRef ||
		immutable.ApprovalDigest != approval || row.snapshot.Source.CredentialRevision.Ref != credentialRef ||
		writeBackDigest([]byte(row.snapshot.BaseContent)) != immutable.BaseContentSHA256 || writeBackDigest([]byte(row.snapshot.ProposedContent)) != immutable.ProposedContentSHA256 {
		return row, errs.ErrUnavailable
	}
	immutable.Version, immutable.State, immutable.FailureCode = p.Version, p.State, p.FailureCode
	immutable.CandidateCommitSHA, immutable.PullRequestRef, immutable.PullRequestURL = p.CandidateCommitSHA, p.PullRequestRef, p.PullRequestURL
	immutable.CreatedAt, immutable.ExpiresAt, immutable.ApprovedAt, immutable.CompletedAt = p.CreatedAt, p.ExpiresAt, p.ApprovedAt, p.CompletedAt
	immutable.BranchConfirmedAt, immutable.PullRequestConfirmedAt = p.BranchConfirmedAt, p.PullRequestConfirmedAt
	row.proposal = immutable
	if lease != nil {
		row.lease.ExpiresAt = *lease
	}
	return row, nil
}

// saveWriteBack updates one version-guarded work row, increments the in-memory proposal version,
// appends audit and optionally a generic MANAGED_CONFIGURATION_CHANGED snapshot event in the
// caller transaction. Zero affected rows is Conflict; it neither commits nor performs provider
// writes.
// saveWriteBack обновляет одну строку work с version guard, увеличивает версию proposal в памяти,
// добавляет audit и при запросе общее событие-снимок MANAGED_CONFIGURATION_CHANGED в транзакции
// caller. Ноль обновлённых строк даёт Conflict; не фиксирует транзакцию и не выполняет запись
// провайдеру.
func (repository *Repository) saveWriteBack(ctx context.Context, tx pgx.Tx, current scope, row *writeBackRow, emitRealtime bool) error {
	var lease *time.Time
	if !row.lease.ExpiresAt.IsZero() {
		lease = &row.lease.ExpiresAt
	}
	p := &row.proposal
	tag, err := tx.Exec(ctx, queryWriteBackUpdate, pgx.StrictNamedArgs{
		"id": row.id, "organization_id": current.organizationID, "version": p.Version, "state": p.State, "effect": row.effect,
		"attempt": row.lease.Attempt, "claim_generation": row.lease.ClaimGeneration, "claimant": row.lease.Claimant, "fence": row.lease.Fence, "lease": lease,
		"candidate_commit": p.CandidateCommitSHA, "candidate_tree": row.tree, "candidate_blob": row.blob, "effect_started": row.started,
		"branch_confirmed": p.BranchConfirmedAt, "pr_confirmed": p.PullRequestConfirmedAt, "pr_ref": p.PullRequestRef, "pr_url": p.PullRequestURL,
		"failure": p.FailureCode, "receipts": asJSON(row.receipts), "approved": p.ApprovedAt, "approver": row.approverRef, "deadline": row.deadline, "completed": p.CompletedAt,
	})
	if err != nil {
		return errs.ErrUnavailable
	}
	if tag.RowsAffected() != 1 {
		return errs.ErrConflict
	}
	p.Version++
	auditRef, err := newRef("aud")
	if err != nil {
		return errs.ErrUnavailable
	}
	_, err = tx.Exec(ctx, queryCommandsExecuteInsertAuditEventsRefProjectIdAction, auditRef, current.organizationID, nil, current.actorID,
		"configuration-writeback."+p.State, "MANAGED_CONFIGURATION_WRITEBACK", p.Ref, "i18n:MANAGED_CONFIGURATION_CHANGED", current.correlationRef)
	if err != nil {
		return errs.ErrUnavailable
	}
	if emitRealtime {
		if err = repository.emitPlatformEventSnapshot(ctx, tx, current, "MANAGED_CONFIGURATION_CHANGED", row.projectRef,
			p.Ref, "i18n:MANAGED_CONFIGURATION_CHANGED", p.Version, p.State); err != nil {
			return err
		}
	}
	return nil
}

// writeBackTerminal recognizes only succeeded, rejected, cancelled, expired and failed states;
// UNKNOWN_OUTCOME remains recoverable and is deliberately nonterminal.
// writeBackTerminal распознаёт только succeeded, rejected, cancelled, expired и failed;
// UNKNOWN_OUTCOME допускает восстановление и намеренно не считается завершением.
func writeBackTerminal(state string) bool {
	switch state {
	case entity.WriteBackSucceeded, entity.WriteBackRejected, entity.WriteBackCancelled, entity.WriteBackExpired, entity.WriteBackFailed:
		return true
	default:
		return false
	}
}
