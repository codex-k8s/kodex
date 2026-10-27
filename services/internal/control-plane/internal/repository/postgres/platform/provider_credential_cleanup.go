package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	domainrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

const (
	// providerCredentialCleanupRetention Defines the 24-hour grace period used when scheduling retired
	// credential revisions after rotation/renewal. It is an eligibility delay, not permission to
	// delete a credential still referenced by an owner/runtime consumer.
	// providerCredentialCleanupRetention Задаёт 24-часовую отсрочку очистки прежних ревизий credential
	// после ротации или обновления. Это задержка пригодности, а не разрешение удалить ревизию, ещё
	// используемую владельцем или runtime.
	providerCredentialCleanupRetention = 24 * time.Hour
	// providerCredentialCleanupLease Defines the two-minute claim duration added to the claim cycle
	// application time before SQL returns the exact stored deadline. Owner/generation/expiry checks
	// remain required for completion.
	// providerCredentialCleanupLease Задаёт двухминутную аренду от времени приложения в цикле захвата;
	// SQL возвращает точный сохранённый срок. При завершении остаются обязательны проверки владельца,
	// поколения и срока.
	providerCredentialCleanupLease = 2 * time.Minute
	// providerCredentialCleanupMaxAttempts Supplies five maximum attempts when scheduling new
	// credential cleanup tasks. Existing locked tasks keep their own persisted limit and dead-letter
	// policy.
	// providerCredentialCleanupMaxAttempts Задаёт максимум пять попыток для новых задач очистки
	// credential. Существующие заблокированные задачи сохраняют свой предел и правило dead-letter.
	providerCredentialCleanupMaxAttempts = 5
)

// providerCredentialCleanupCandidate Buffers resolved task/account/object/materializer metadata
// and recovery identity after candidate rows close and before claim updates. It contains
// descriptors rather than secret bytes and does not independently authorize cleanup.
// providerCredentialCleanupCandidate Буферизует разрешённые метаданные задачи, аккаунта, объекта,
// материализатора и recovery identity после закрытия rows до обновления захвата. Содержит
// дескрипторы вместо значений секретов и самостоятельно не разрешает очистку.
type providerCredentialCleanupCandidate struct {
	recovery                                                                            entity.ProviderCleanupRecoveryIdentity
	id, ref, accountRef                                                                 string
	secretName, secretUID, secretVersion, contentHash                                   string
	targetKind, authorizationRef, materializerRef, materializerUID, materializerVersion string
}

// lockedProviderCredentialCleanupTask Carries locked owner/account/task lifecycle, lease
// generation/deadline, attempt policy and terminal receipt/completion descriptor for replay and
// fresh-claim checks. These internal fields are not caller authority.
// lockedProviderCredentialCleanupTask Хранит заблокированные владение, аккаунт, жизненный цикл
// задачи, поколение и срок аренды, предел попыток, итоговую квитанцию и дескриптор для replay и
// свежей проверки. Внутренние поля не назначают полномочия клиента.
type lockedProviderCredentialCleanupTask struct {
	state, leaseOwner, safeErrorCode, terminalReceipt string
	organizationID, accountID, accountRef, targetKind string
	authorizationRef, materializerRef                 string
	completionDescriptor                              []byte
	generation                                        int64
	attempts, maximumAttempts                         int32
	leaseExpiresAt                                    *time.Time
}

// ClaimProviderCredentialCleanupTasks Validates owner/limit 1..16, begins ReadCommitted, advances
// reservations/deletions and terminal expirations, then locks accounts and eligible tasks with
// closed rows before claim updates. Returns bounded descriptor-only tasks after commit, preserving
// per-candidate recovery identity. An empty poll may commit maintenance changes; no external
// deletion occurs here.
// ClaimProviderCredentialCleanupTasks Проверяет владельца и предел 1..16, открывает ReadCommitted,
// продвигает reservations, удаления аккаунтов и истечения, блокирует аккаунты и пригодные задачи.
// Закрывает rows до обновления захватов и возвращает ограниченные дескрипторы после commit с
// recovery identity каждого кандидата. Пустой опрос может сохранить служебные изменения; внешнего
// удаления здесь нет.
func (repository *Repository) ClaimProviderCredentialCleanupTasks(
	ctx context.Context,
	leaseOwner string,
	limit int32,
) ([]domainrepo.ProviderCredentialCleanupTask, error) {
	leaseOwner = strings.TrimSpace(leaseOwner)
	if leaseOwner == "" || len(leaseOwner) > 128 || limit < 1 || limit > 16 {
		return nil, errs.ErrInvalid
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := repository.expireProviderAuthorizationReservations(ctx, tx, limit); err != nil {
		return nil, err
	}
	if err := repository.advanceProviderAccountDeletions(ctx, tx, limit); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, queryProviderCredentialCleanupExpireTerminalClaims); err != nil {
		return nil, fmt.Errorf("expire terminal provider credential cleanup claims: %w", errs.ErrUnavailable)
	}
	accountRows, err := tx.Query(ctx, queryProviderCredentialCleanupLockClaimableAccounts,
		pgx.StrictNamedArgs{"limit": limit})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	accountIDs := make([]string, 0, limit)
	for accountRows.Next() {
		var accountID, accountRef string
		if err := accountRows.Scan(&accountID, &accountRef); err != nil {
			accountRows.Close()
			return nil, errs.ErrUnavailable
		}
		accountIDs = append(accountIDs, accountID)
	}
	if err := accountRows.Err(); err != nil {
		accountRows.Close()
		return nil, errs.ErrUnavailable
	}
	accountRows.Close()

	now := time.Now().UTC()
	items := make([]domainrepo.ProviderCredentialCleanupTask, 0, limit)
	for _, accountID := range accountIDs {
		remaining := limit - int32(len(items))
		if remaining == 0 {
			break
		}
		rows, err := tx.Query(ctx, queryProviderCredentialCleanupSelectClaimableTasks,
			pgx.StrictNamedArgs{"account_id": accountID, "limit": remaining})
		if err != nil {
			return nil, errs.ErrUnavailable
		}
		candidates := make([]providerCredentialCleanupCandidate, 0, remaining)
		for rows.Next() {
			var candidate providerCredentialCleanupCandidate
			if err := rows.Scan(&candidate.id, &candidate.ref, &candidate.accountRef,
				&candidate.secretName, &candidate.secretUID, &candidate.secretVersion,
				&candidate.contentHash, &candidate.targetKind, &candidate.authorizationRef,
				&candidate.materializerRef, &candidate.materializerUID, &candidate.materializerVersion,
				&candidate.recovery.TaskRef, &candidate.recovery.Generation, &candidate.recovery.LegacyLastGeneration); err != nil {
				rows.Close()
				return nil, errs.ErrUnavailable
			}
			candidates = append(candidates, candidate)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, errs.ErrUnavailable
		}
		rows.Close()
		for _, candidate := range candidates {
			expiresAt := now.Add(providerCredentialCleanupLease)
			var attempt int32
			var generation int64
			if err := tx.QueryRow(ctx, queryProviderCredentialCleanupClaimTask, pgx.StrictNamedArgs{
				"task_id": candidate.id, "lease_owner": leaseOwner, "lease_expires_at": expiresAt,
			}).Scan(&attempt, &generation, &expiresAt); errors.Is(err, pgx.ErrNoRows) {
				return nil, errs.ErrConflict
			} else if err != nil {
				return nil, errs.ErrUnavailable
			}
			items = append(items, domainrepo.ProviderCredentialCleanupTask{
				Recovery: &candidate.recovery,
				Ref:      candidate.ref, AccountRef: candidate.accountRef, Attempt: attempt,
				Generation: generation, LeaseExpiresAt: expiresAt,
				TargetKind: candidate.targetKind,
				Authorization: entity.ProviderAuthorizationCleanupTarget{
					Recovery: &candidate.recovery,
					TaskRef:  candidate.ref, AccountRef: candidate.accountRef, Generation: generation,
					AuthorizationAttemptRef: candidate.authorizationRef, MaterializerAttemptRef: candidate.materializerRef,
					UID: candidate.materializerUID, ResourceVersion: candidate.materializerVersion,
					Kind: providerAuthorizationCleanupKind(candidate.targetKind),
				},
				Credential: entity.ProviderCredentialDescriptor{
					SecretName: candidate.secretName, SecretUID: candidate.secretUID,
					SecretResourceVersion: candidate.secretVersion, ContentSHA256: candidate.contentHash,
				},
			})
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, errs.ErrConflict
	}
	return items, nil
}

// CompleteProviderCredentialCleanupTask Validates completion shape, locks account then task and
// accepts exact generation/receipt terminal replay with canonical descriptor comparison when
// stored. Fresh completion requires a live owner claim, applies owner-specific completion and
// records one terminal task update before commit. Replay does not repeat cleanup; commit failure
// stays conflict.
// CompleteProviderCredentialCleanupTask Проверяет форму завершения, блокирует аккаунт затем
// задачу, допускает terminal replay точных поколения и квитанции со сверкой сохранённого
// канонического дескриптора. Свежее завершение требует действующий захват владельца, применяет
// профильное завершение и одну terminal-запись до commit. Replay не повторяет очистку; ошибка
// commit даёт Conflict.
func (repository *Repository) CompleteProviderCredentialCleanupTask(
	ctx context.Context,
	taskRef, leaseOwner string,
	generation int64,
	completion entity.ProviderAuthorizationCleanupResult,
) (domainrepo.ProviderCredentialCleanupResult, error) {
	leaseOwner = strings.TrimSpace(leaseOwner)
	terminalReceipt := strings.TrimSpace(completion.TerminalReceipt)
	if !strings.HasPrefix(taskRef, "pcct_") || leaseOwner == "" || len(leaseOwner) > 128 ||
		generation < 1 || (terminalReceipt == "" && completion.Observation == nil) || len(terminalReceipt) > 512 {
		return domainrepo.ProviderCredentialCleanupResult{}, errs.ErrInvalid
	}
	tx, task, err := repository.lockProviderCredentialCleanupTask(ctx, taskRef)
	if err != nil {
		return domainrepo.ProviderCredentialCleanupResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	encoded, err := json.Marshal(completion)
	if err != nil {
		return domainrepo.ProviderCredentialCleanupResult{}, errs.ErrInvalid
	}
	if completion.Observation != nil {
		terminalReceipt = providerCleanupObservationReceipt(taskRef, generation, encoded)
	}
	if task.state == "COMPLETED" && task.generation == generation && task.terminalReceipt == terminalReceipt {
		if len(task.completionDescriptor) > 0 && !sameCanonicalJSON(task.completionDescriptor, encoded) {
			return domainrepo.ProviderCredentialCleanupResult{}, errs.ErrConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return domainrepo.ProviderCredentialCleanupResult{}, errs.ErrConflict
		}
		return providerCredentialCleanupResult(taskRef, task, false), nil
	}
	if !validProviderCredentialCleanupClaim(task, leaseOwner, generation) {
		return domainrepo.ProviderCredentialCleanupResult{}, errs.ErrConflict
	}
	if err := repository.applyProviderCleanupCompletion(ctx, tx, taskRef, task, completion); err != nil {
		return domainrepo.ProviderCredentialCleanupResult{}, err
	}
	tag, err := tx.Exec(ctx, queryProviderCredentialCleanupCompleteTask, pgx.StrictNamedArgs{
		"task_ref": taskRef, "lease_owner": leaseOwner, "lease_generation": generation,
		"terminal_receipt":      terminalReceipt,
		"completion_descriptor": encoded,
	})
	if err != nil {
		return domainrepo.ProviderCredentialCleanupResult{}, errs.ErrUnavailable
	}
	if tag.RowsAffected() != 1 {
		return domainrepo.ProviderCredentialCleanupResult{}, errs.ErrConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return domainrepo.ProviderCredentialCleanupResult{}, errs.ErrConflict
	}
	task.state, task.safeErrorCode, task.terminalReceipt = "COMPLETED", "", terminalReceipt
	return providerCredentialCleanupResult(taskRef, task, false), nil
}

// FailProviderCredentialCleanupTask Replays matching terminal/pending generation/error outcomes,
// otherwise requires a live exact claim and a closed safe code. Exhausted attempts dead-letter
// first; eligible authorization CAS changes create a successor and close no-effect parent,
// otherwise bounded backoff schedules retry. Requires one fenced update before commit without
// performing remote cleanup.
// FailProviderCredentialCleanupTask Повторно читает совпавший terminal/pending исход поколения и
// кода, иначе требует действующий точный захват и закрытый безопасный код. Исчерпание попыток
// сначала ведёт в dead-letter; допустимый authorization CAS создаёт преемника и закрывает родителя
// без эффекта, иначе повтор откладывается. Требует одну fenced-запись до commit без внешней
// очистки.
func (repository *Repository) FailProviderCredentialCleanupTask(
	ctx context.Context,
	taskRef, leaseOwner string,
	generation int64,
	safeErrorCode string,
) (domainrepo.ProviderCredentialCleanupResult, error) {
	leaseOwner = strings.TrimSpace(leaseOwner)
	if !strings.HasPrefix(taskRef, "pcct_") || leaseOwner == "" || len(leaseOwner) > 128 ||
		generation < 1 || !validProviderCredentialCleanupError(safeErrorCode) {
		return domainrepo.ProviderCredentialCleanupResult{}, errs.ErrInvalid
	}
	tx, task, err := repository.lockProviderCredentialCleanupTask(ctx, taskRef)
	if err != nil {
		return domainrepo.ProviderCredentialCleanupResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if (task.state == "PENDING" || task.state == "DEAD_LETTER" || task.state == "COMPLETED") && task.generation == generation &&
		task.safeErrorCode == safeErrorCode {
		if err := tx.Commit(ctx); err != nil {
			return domainrepo.ProviderCredentialCleanupResult{}, errs.ErrConflict
		}
		return providerCredentialCleanupResult(taskRef, task, task.state == "PENDING"), nil
	}
	if !validProviderCredentialCleanupClaim(task, leaseOwner, generation) {
		return domainrepo.ProviderCredentialCleanupResult{}, errs.ErrConflict
	}
	if safeErrorCode == "PROVIDER_CREDENTIAL_CLEANUP_CAS_CHANGED" && task.targetKind != "AUTHORIZATION_ATTEMPT" && task.targetKind != "AUTHORIZATION_ABSENCE" {
		return domainrepo.ProviderCredentialCleanupResult{}, errs.ErrInvalid
	}
	now := time.Now().UTC()
	state, receipt := "PENDING", ""
	var completedAt *time.Time
	if task.attempts >= task.maximumAttempts {
		state = "DEAD_LETTER"
		completedAt = &now
		receipt = fmt.Sprintf("dead-letter:%s:g%d:a%d:%s", taskRef, generation, task.attempts, safeErrorCode)
	} else if safeErrorCode == "PROVIDER_CREDENTIAL_CLEANUP_CAS_CHANGED" {
		nextRef, err := newRef("pcct")
		if err != nil {
			return domainrepo.ProviderCredentialCleanupResult{}, err
		}
		tag, err := tx.Exec(ctx, queryProviderCleanupCASSuccessor, pgx.StrictNamedArgs{"next_ref": nextRef, "parent_ref": taskRef})
		if err != nil {
			return domainrepo.ProviderCredentialCleanupResult{}, errs.ErrUnavailable
		}
		if tag.RowsAffected() != 1 {
			return domainrepo.ProviderCredentialCleanupResult{}, errs.ErrConflict
		}
		state, completedAt = "COMPLETED", &now
		receipt = fmt.Sprintf("no-effect-cas:%s:g%d:a%d", taskRef, generation, task.attempts)
	}
	eligibleAt := now.Add(providerCredentialCleanupBackoff(task.attempts))
	tag, err := tx.Exec(ctx, queryProviderCredentialCleanupFailTask, pgx.StrictNamedArgs{
		"task_ref": taskRef, "lease_owner": leaseOwner, "lease_generation": generation,
		"state": state, "eligible_at": eligibleAt, "safe_error_code": safeErrorCode,
		"terminal_receipt": receipt, "completed_at": completedAt,
	})
	if err != nil {
		return domainrepo.ProviderCredentialCleanupResult{}, errs.ErrUnavailable
	}
	if tag.RowsAffected() != 1 {
		return domainrepo.ProviderCredentialCleanupResult{}, errs.ErrConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return domainrepo.ProviderCredentialCleanupResult{}, errs.ErrConflict
	}
	task.state, task.safeErrorCode, task.terminalReceipt = state, safeErrorCode, receipt
	return providerCredentialCleanupResult(taskRef, task, state == "PENDING"), nil
}

// lockProviderCredentialCleanupTask Begins ReadCommitted and locks account before task by the
// internal task ref, returning the live transaction only after both scans succeed. Missing rows
// are not-found; other reads unavailable. Error paths roll back with the request context and
// preserve the existing partial first-scan result contract.
// lockProviderCredentialCleanupTask Открывает ReadCommitted и блокирует аккаунт перед задачей по
// внутренней ссылке, возвращая транзакцию лишь после двух успешных чтений. Отсутствие строки даёт
// NotFound, другой сбой — Unavailable. При ошибке откатывает с контекстом запроса и сохраняет
// прежний частичный результат первого чтения.
func (repository *Repository) lockProviderCredentialCleanupTask(
	ctx context.Context,
	taskRef string,
) (pgx.Tx, lockedProviderCredentialCleanupTask, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, lockedProviderCredentialCleanupTask{}, errs.ErrUnavailable
	}
	var task lockedProviderCredentialCleanupTask
	if err := tx.QueryRow(ctx, queryProviderCleanupLockAccount, taskRef).Scan(&task.organizationID, &task.accountID, &task.accountRef); err != nil {
		_ = tx.Rollback(ctx)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, task, errs.ErrNotFound
		}
		return nil, task, errs.ErrUnavailable
	}
	err = tx.QueryRow(ctx, queryProviderCredentialCleanupLockTask,
		pgx.StrictNamedArgs{"task_ref": taskRef}).Scan(
		&task.state, &task.leaseOwner, &task.generation, &task.leaseExpiresAt,
		&task.attempts, &task.maximumAttempts, &task.safeErrorCode, &task.terminalReceipt,
		&task.targetKind, &task.authorizationRef, &task.materializerRef, &task.completionDescriptor,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		_ = tx.Rollback(ctx)
		return nil, lockedProviderCredentialCleanupTask{}, errs.ErrNotFound
	}
	if err != nil {
		_ = tx.Rollback(ctx)
		return nil, lockedProviderCredentialCleanupTask{}, errs.ErrUnavailable
	}
	return tx, task, nil
}

// validProviderCredentialCleanupClaim Requires CLAIMED state, exact owner/generation and a nonnil
// deadline strictly after the application clock. Terminal replay uses separate receipt rules;
// equality with now is expired.
// validProviderCredentialCleanupClaim Требует CLAIMED, точные owner и generation, непустой срок
// строго позже часов приложения. Terminal replay имеет отдельные правила квитанции; равенство
// текущему времени означает истечение.
func validProviderCredentialCleanupClaim(task lockedProviderCredentialCleanupTask, owner string, generation int64) bool {
	return task.state == "CLAIMED" && task.leaseOwner == owner && task.generation == generation &&
		task.leaseExpiresAt != nil && task.leaseExpiresAt.After(time.Now())
}

// providerCredentialCleanupResult Projects only task ref/state/safe code/terminal receipt and the
// caller-selected retry flag. It omits object descriptors, credentials and internal owner/account
// identifiers.
// providerCredentialCleanupResult Проецирует только ссылку задачи, состояние, безопасный код,
// terminal-квитанцию и выбранный вызывающим признак повтора. Дескрипторы объектов, credential и
// внутренние ID владельца с аккаунтом не выдаёт.
func providerCredentialCleanupResult(
	ref string,
	task lockedProviderCredentialCleanupTask,
	retryScheduled bool,
) domainrepo.ProviderCredentialCleanupResult {
	return domainrepo.ProviderCredentialCleanupResult{
		Ref: ref, State: task.state, SafeErrorCode: task.safeErrorCode,
		TerminalReceipt: task.terminalReceipt, RetryScheduled: retryScheduled,
	}
}

// providerCredentialCleanupBackoff Returns a five-second exponential delay, clamping the shift
// to 0..6 and final duration to five minutes; attempts at or below one use five seconds. It does
// not itself schedule or authorize retry.
// providerCredentialCleanupBackoff Возвращает экспоненциальную задержку от пяти секунд,
// ограничивает степень диапазоном 0..6 и итог пятью минутами. Для попытки не выше первой даёт пять
// секунд; сам повтор не планирует и не разрешает.
func providerCredentialCleanupBackoff(attempt int32) time.Duration {
	shift := max(attempt-1, 0)
	if shift > 6 {
		shift = 6
	}
	backoff := 5 * time.Second * time.Duration(1<<shift)
	return min(backoff, 5*time.Minute)
}

// providerAuthorizationCleanupKind Maps the AUTHORIZATION_METADATA successor kind to the existing
// empty broker cleanup kind and preserves every other value. This compatibility mapping does not
// choose target ownership.
// providerAuthorizationCleanupKind Преобразует AUTHORIZATION_METADATA преемника в прежний пустой
// вид очистки broker, остальные значения сохраняет. Совместимость не назначает владельца цели.
func providerAuthorizationCleanupKind(kind string) string {
	if kind == "AUTHORIZATION_METADATA" {
		return ""
	}
	return kind
}

// validProviderCredentialCleanupError Accepts only the five closed cleanup failure codes, keeping
// arbitrary remote text out of the persisted safe-error field. It does not prove whether an
// external cleanup effect happened.
// validProviderCredentialCleanupError Допускает только пять закрытых кодов ошибки очистки, не
// пропуская произвольный внешний текст в безопасное поле. Не доказывает, произошёл ли внешний
// эффект очистки.
func validProviderCredentialCleanupError(value string) bool {
	switch value {
	case "PROVIDER_CREDENTIAL_CLEANUP_UNAVAILABLE",
		"PROVIDER_CREDENTIAL_CLEANUP_REJECTED",
		"PROVIDER_CREDENTIAL_CLEANUP_TIMEOUT",
		"PROVIDER_CREDENTIAL_CLEANUP_CAS_CHANGED",
		"PROVIDER_CREDENTIAL_CLEANUP_FAILED":
		return true
	default:
		return false
	}
}

// scheduleProviderCredentialCleanup Skips an empty credential revision and otherwise schedules its
// exact organization/account descriptor with caller expiry and the fixed attempt policy in the
// supplied transaction. SQL failure is unavailable; neither claim nor external deletion occurs.
// scheduleProviderCredentialCleanup Пропускает пустую ревизию credential, иначе планирует её
// точный дескриптор организации и аккаунта со сроком вызывающего и фиксированным пределом попыток
// в переданной транзакции. SQL-сбой даёт Unavailable; захвата или внешнего удаления нет.
func (repository *Repository) scheduleProviderCredentialCleanup(
	ctx context.Context,
	tx pgx.Tx,
	organizationID, accountID, credentialRevisionID string,
	eligibleAt time.Time,
) error {
	if credentialRevisionID == "" {
		return nil
	}
	if _, err := tx.Exec(ctx, queryProviderCredentialCleanupScheduleRevision, pgx.StrictNamedArgs{
		"organization_id": organizationID, "account_id": accountID,
		"credential_revision_id": credentialRevisionID, "eligible_at": eligibleAt,
		"maximum_attempts": providerCredentialCleanupMaxAttempts,
	}); err != nil {
		return errs.ErrUnavailable
	}
	return nil
}
