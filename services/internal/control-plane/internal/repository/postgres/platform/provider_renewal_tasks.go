package platform

import (
	"context"
	_ "embed"
	"errors"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var (
	// queryProviderRenewalClaimAccounts Locks at most one enabled authorized openai-codex DEVICE_CODE
	// account whose current credential catalog observation rejected authorization and which has no
	// active/already-authorized same-input renewal. SKIP LOCKED plus a 90-second deadline and next
	// attempt are projections; eligibility is rechecked in a fresh statement after locking.
	// queryProviderRenewalClaimAccounts Блокирует до одного включённого авторизованного openai-codex
	// DEVICE_CODE аккаунта с отказом авторизации текущей credential в каталоге, без активного или уже
	// авторизованного обновления того же входа. SKIP LOCKED, 90-секундный срок и следующая попытка
	// проецируются; пригодность повторно проверяется свежим statement после блокировки.
	//go:embed sql/provider_renewal__claim_accounts.sql
	queryProviderRenewalClaimAccounts string
	// queryProviderRenewalInputEligible Checks exact organization/account/version/current credential
	// plus enabled lifecycle and absence of a CLAIMED runtime lease using that account. It is
	// deliberately reissued after the account lock to avoid relying only on the initial candidate
	// snapshot.
	// queryProviderRenewalInputEligible Проверяет точные организацию, аккаунт, версию и текущую
	// credential, включённость и отсутствие CLAIMED runtime-аренды этого аккаунта. Запрос намеренно
	// повторяется после блокировки аккаунта, чтобы не полагаться лишь на исходный снимок кандидата.
	//go:embed sql/provider_renewal__input_eligible.sql
	queryProviderRenewalInputEligible string
	// queryProviderRenewalCreateTask Inserts a CLAIMED renewal with exact owner account/version/input
	// credential, attempt/claimant/generation/fence, request and owner digests and expiry. It persists
	// no credential value and performs no remote refresh.
	// queryProviderRenewalCreateTask Вставляет CLAIMED обновление с точными аккаунтом, версией и
	// входной credential владельца, attempt, claimant, generation, fence, двумя хешами запроса и
	// сроком. Значение credential не хранит и внешнее обновление не выполняет.
	//go:embed sql/provider_renewal__create_task.sql
	queryProviderRenewalCreateTask string
	// queryProviderRenewalExpireTasks Closes at most 16 expired organization tasks under SKIP LOCKED:
	// never-authorized tasks become FAILED, previously authorized ones UNKNOWN_OUTCOME. Expiration
	// does not authorize a second remote refresh.
	// queryProviderRenewalExpireTasks Закрывает до 16 истёкших задач организации через SKIP LOCKED:
	// неавторизованные становятся FAILED, ранее авторизованные — UNKNOWN_OUTCOME. Истечение не
	// разрешает второе внешнее обновление.
	//go:embed sql/provider_renewal__expire_tasks.sql
	queryProviderRenewalExpireTasks string
	// queryProviderRenewalLockTask Locks exact organization/task and account, projecting the original
	// credential/binding, current input eligibility, database time and terminal output metadata. It
	// does not itself authorize or execute renewal.
	// queryProviderRenewalLockTask Блокирует точные задачу организации и аккаунт, проецирует исходную
	// credential и привязку, текущую пригодность входа, время БД и итоговые метаданные. Самостоятельно
	// не разрешает и не выполняет обновление.
	//go:embed sql/provider_renewal__lock_task.sql
	queryProviderRenewalLockTask string
	// queryProviderRenewalAuthorizeTask Moves only a resolved nonexpired CLAIMED task to AUTHORIZED
	// and records database authorization time. The caller must require one affected row after checking
	// broker authority and exact locked task bindings.
	// queryProviderRenewalAuthorizeTask Переводит только разрешённую неистёкшую CLAIMED задачу в
	// AUTHORIZED с временем БД. Вызывающий требует одну изменённую строку после проверки broker
	// authority и точных привязок заблокированной задачи.
	//go:embed sql/provider_renewal__authorize_task.sql
	queryProviderRenewalAuthorizeTask string
	// queryProviderRenewalRuntimeCandidate Checks current account credential identity and absence of
	// CLAIMED/AUTHORIZED/UNKNOWN_OUTCOME renewal tasks for that input. Unknown outcome blocks runtime
	// candidate use rather than triggering a refresh retry.
	// queryProviderRenewalRuntimeCandidate Проверяет текущую идентичность credential аккаунта и
	// отсутствие обновлений CLAIMED/AUTHORIZED/UNKNOWN_OUTCOME этого входа. Неопределённый исход
	// блокирует runtime-кандидата вместо повторного обновления.
	//go:embed sql/provider_renewal__runtime_candidate.sql
	queryProviderRenewalRuntimeCandidate string
)

// ClaimProviderCredentialRenewalTasks Within five seconds resolves the installation system
// identity, expires old tasks, locks one account, closes rows and freshly checks input eligibility
// before generating ref/fence and encoder-bound request digests. Inserts/commits claims with
// independent bounded rollback handling; an empty result may still commit expiration changes and
// never proves no side effects.
// ClaimProviderCredentialRenewalTasks За пять секунд разрешает системную личность установки,
// закрывает истёкшие задачи, блокирует один аккаунт, закрывает rows и свежим запросом проверяет
// вход перед генерацией ref, fence и привязанных encoder хешей. Вставляет и подтверждает захваты с
// независимым ограниченным rollback; пустой результат всё ещё может сохранить истечение задач и не
// означает отсутствия изменений.
func (repository *Repository) ClaimProviderCredentialRenewalTasks(ctx context.Context, claimant string, encoder port.ProviderCredentialRenewalEncoder) (result []port.ProviderCredentialRenewalTask, resultErr error) {
	if claimant == "" || len(claimant) > 128 || strings.ContainsAny(claimant, "\r\n\x00") || encoder == nil {
		return nil, errs.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	actor, err := repository.resolveProofIdentity(ctx, port.ProofPrincipalInput{CallerWorkload: "control-plane", Operation: port.ProviderCredentialRenewalOperation,
		ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation"})
	if err != nil {
		return nil, err
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer rollbackAssistantLockedRead(ctx, tx, &resultErr)
	if _, err := tx.Exec(ctx, queryProviderRenewalExpireTasks, actor.OrganizationID); err != nil {
		return nil, errs.ErrUnavailable
	}
	rows, err := tx.Query(ctx, queryProviderRenewalClaimAccounts, actor.OrganizationID)
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	// candidate Keeps locked internal account/input-credential IDs alongside the public renewal task
	// while rows are fully consumed before fresh eligibility and insertion queries. It contains only
	// descriptors and request metadata.
	// candidate Хранит внутренние ID заблокированных аккаунта и входной credential рядом с публичной
	// задачей, пока rows полностью читаются до свежей проверки пригодности и вставок. Содержит только
	// дескрипторы и метаданные запроса.
	type candidate struct {
		accountID, credentialID string
		task                    port.ProviderCredentialRenewalTask
	}
	var candidates []candidate
	for rows.Next() {
		var item candidate
		item.task.OrganizationID, item.task.ClaimantID, item.task.State = actor.OrganizationID, claimant, "CLAIMED"
		if rows.Scan(&item.accountID, &item.task.AccountRef, &item.task.AccountVersion, &item.credentialID, &item.task.CredentialRef, &item.task.CredentialRevision,
			&item.task.Previous.SecretName, &item.task.Previous.SecretUID, &item.task.Previous.SecretResourceVersion, &item.task.Previous.ContentSHA256,
			&item.task.ExpiresAt, &item.task.Generation) != nil {
			rows.Close()
			return nil, errs.ErrUnavailable
		}
		candidates = append(candidates, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	for _, item := range candidates {
		// Свежий statement ПОСЛЕ account lock; initial snapshot не закрывает TOCTOU.
		var eligible bool
		if err := tx.QueryRow(ctx, queryProviderRenewalInputEligible, actor.OrganizationID, item.accountID, item.task.AccountVersion, item.credentialID).Scan(&eligible); err != nil {
			return nil, errs.ErrUnavailable
		}
		if !eligible {
			continue
		}
		item.task.Ref, err = newRef("pcrt")
		if err != nil {
			return nil, err
		}
		item.task.Fence, err = newRef("prf")
		if err != nil {
			return nil, err
		}
		item.task.RequestDigest, item.task.OwnerRequestDigest, err = encoder.CredentialRenewalRequestDigests(item.task)
		if err != nil || !validModelCatalogDigest(item.task.RequestDigest) || !validModelCatalogDigest(item.task.OwnerRequestDigest) {
			return nil, errs.ErrInvalid
		}
		if _, err := tx.Exec(ctx, queryProviderRenewalCreateTask, pgx.StrictNamedArgs{
			"ref": item.task.Ref, "organization_id": actor.OrganizationID, "account_id": item.accountID, "account_version": item.task.AccountVersion,
			"credential_id": item.credentialID, "attempt": item.task.Generation, "claimant": claimant, "generation": item.task.Generation,
			"fence": item.task.Fence, "request_digest": item.task.RequestDigest, "owner_request_digest": item.task.OwnerRequestDigest, "expires_at": item.task.ExpiresAt,
		}); err != nil {
			return nil, errs.ErrConflict
		}
		result = append(result, item.task)
	}
	if tx.Commit(ctx) != nil {
		return nil, errs.ErrUnavailable
	}
	return result, nil
}

// lockedProviderRenewal Carries the locked owner task/account/input credential, current
// eligibility/database clock and terminal result/output identity. This internal read projection
// does not itself permit remote refresh or choose a retry.
// lockedProviderRenewal Хранит заблокированные задачу, аккаунт и входную credential владельца,
// текущую пригодность и часы БД, итоговые хеш и идентичность результата. Внутренняя проекция сама
// не разрешает внешнее обновление или повтор.
type lockedProviderRenewal struct {
	id, accountID, credentialID      string
	task                             port.ProviderCredentialRenewalTask
	eligible                         bool
	now                              time.Time
	resultDigest, outputCredentialID string
}

// lockProviderRenewal Scans 24 task/account/credential/fence/result fields under exact
// organization/ref row locks, preserving partial internal data on error. Missing/wrapped-missing
// rows become forbidden and other scan failures unavailable; callers discard errored results.
// lockProviderRenewal Читает 24 поля задачи, аккаунта, credential, fence и результата под
// блокировками точных организации и ссылки, сохраняя частичные внутренние данные при ошибке.
// Отсутствие строки, включая обёрнутое, даёт Forbidden, другой сбой — Unavailable; вызывающие
// отбрасывают ошибочный результат.
func lockProviderRenewal(ctx context.Context, tx pgx.Tx, organizationID, ref string) (lockedProviderRenewal, error) {
	var locked lockedProviderRenewal
	err := tx.QueryRow(ctx, queryProviderRenewalLockTask, organizationID, ref).Scan(&locked.id, &locked.task.State, &locked.task.OrganizationID, &locked.accountID,
		&locked.task.Ref, &locked.task.AccountRef, &locked.task.AccountVersion, &locked.credentialID, &locked.task.CredentialRef, &locked.task.CredentialRevision,
		&locked.task.Previous.SecretName, &locked.task.Previous.SecretUID, &locked.task.Previous.SecretResourceVersion, &locked.task.Previous.ContentSHA256,
		&locked.task.ClaimantID, &locked.task.Generation, &locked.task.Fence, &locked.task.RequestDigest, &locked.task.OwnerRequestDigest, &locked.task.ExpiresAt,
		&locked.eligible, &locked.now, &locked.resultDigest, &locked.outputCredentialID)
	if errors.Is(err, pgx.ErrNoRows) {
		return locked, errs.ErrForbidden
	}
	if err != nil {
		return locked, errs.ErrUnavailable
	}
	return locked, nil
}

// sameProviderRenewalBinding Compares the existing
// ref/account/version/credential/previous-object/claimant/generation/fence/request-digest/expiry
// tuple exactly. Organization, owner-request digest and state are not included here; trusted owner
// lookup and subsequent state checks remain separate.
// sameProviderRenewalBinding Точно сравнивает прежний набор ref, аккаунта, версии, credential,
// исходного объекта, claimant, generation, fence, хеша запроса и срока. Организация, owner-request
// digest и состояние сюда не входят; доверенный поиск владельца и последующие проверки состояния
// остаются отдельными.
func sameProviderRenewalBinding(a, b port.ProviderCredentialRenewalTask) bool {
	return a.Ref == b.Ref && a.AccountRef == b.AccountRef && a.AccountVersion == b.AccountVersion && a.CredentialRef == b.CredentialRef &&
		a.CredentialRevision == b.CredentialRevision && a.Previous == b.Previous && a.ClaimantID == b.ClaimantID && a.Generation == b.Generation &&
		a.Fence == b.Fence && a.RequestDigest == b.RequestDigest && a.ExpiresAt.Equal(b.ExpiresAt)
}

// validProviderRenewalBinding Checks bounded safe ref/fence/claimant strings, pcrt_ ref, digest,
// nonzero expiry, positive JSON-safe numeric pins, canonical UUID and previous-secret descriptor
// shape. It does not validate organization/state/owner-request digest or prove freshness; owner
// locking enforces the runtime boundary.
// validProviderRenewalBinding Проверяет ограниченные безопасные ref, fence и claimant, префикс
// pcrt_, хеш, ненулевой срок, положительные числовые привязки в безопасном JSON-диапазоне,
// канонический UUID и форму исходного секрета. Организацию, состояние, owner-request digest и
// свежесть не проверяет; рабочую границу обеспечивает блокировка владельца.
func validProviderRenewalBinding(task port.ProviderCredentialRenewalTask) bool {
	if !strings.HasPrefix(task.Ref, "pcrt_") || !validModelCatalogDigest(task.RequestDigest) || task.ExpiresAt.IsZero() ||
		task.AccountVersion < 1 || task.CredentialRevision < 1 || task.Generation < 1 || task.AccountVersion > 9007199254740991 || task.CredentialRevision > 9007199254740991 || task.Generation > 9007199254740991 {
		return false
	}
	for _, ref := range []string{task.Ref, task.AccountRef, task.CredentialRef, task.Fence, task.ClaimantID} {
		if len(ref) < 1 || len(ref) > 128 || strings.ContainsAny(ref, "\r\n\x00") {
			return false
		}
	}
	uid, err := uuid.Parse(task.Previous.SecretUID)
	return err == nil && uid.String() == task.Previous.SecretUID && validModelCatalogDigest(task.Previous.ContentSHA256) && validDNSLabel(task.Previous.SecretName) &&
		len(task.Previous.SecretResourceVersion) > 0 && len(task.Previous.SecretResourceVersion) <= 128
}

// AuthorizeProviderCredentialRenewal Delegates to the authorizing owner-check path, requiring
// exact broker permission, current eligible input and nonexpired CLAIMED/AUTHORIZED state. It
// authorizes one existing task rather than performing remote refresh.
// AuthorizeProviderCredentialRenewal Передаёт проверку пути авторизации владельца: точное
// разрешение broker, текущий пригодный вход и неистёкшее состояние CLAIMED/AUTHORIZED. Разрешает
// существующую задачу без внешнего обновления.
func (repository *Repository) AuthorizeProviderCredentialRenewal(ctx context.Context, principal value.Principal, task port.ProviderCredentialRenewalTask) (resultErr error) {
	return repository.checkProviderRenewalOwner(ctx, principal, task, true)
}

// ValidateProviderCredentialRenewalObservation Delegates to readback validation for an already
// AUTHORIZED/UNKNOWN_OUTCOME task. This path checks exact binding/current owner eligibility
// without reauthorizing an expired effect or allowing resend.
// ValidateProviderCredentialRenewalObservation Передаёт проверку наблюдения уже
// AUTHORIZED/UNKNOWN_OUTCOME задачи. Путь сверяет точную привязку и текущую пригодность владельца
// без повторной авторизации истёкшего эффекта или разрешения отправки.
func (repository *Repository) ValidateProviderCredentialRenewalObservation(ctx context.Context, principal value.Principal, task port.ProviderCredentialRenewalTask) (resultErr error) {
	return repository.checkProviderRenewalOwner(ctx, principal, task, false)
}

// checkProviderRenewalOwner Requires exact broker authorize/validate permission, projectless scope
// and valid task shape, then locks and compares the owner task. Authorize checks database
// expiry/current input and conditionally advances one row; observation accepts only
// AUTHORIZED/UNKNOWN_OUTCOME without an expiry refresh. Commit/independent rollback failures
// remain unavailable.
// checkProviderRenewalOwner Требует точное authorize/validate-разрешение broker, отсутствие
// проекта и допустимую форму задачи, затем блокирует и сравнивает задачу владельца. Авторизация
// проверяет срок БД и текущий вход, условно продвигает одну строку; наблюдение допускает лишь
// AUTHORIZED/UNKNOWN_OUTCOME без продления срока. Сбои commit и независимого rollback дают
// Unavailable.
func (repository *Repository) checkProviderRenewalOwner(ctx context.Context, principal value.Principal, task port.ProviderCredentialRenewalTask, authorize bool) (resultErr error) {
	operation := port.ProviderCredentialRenewalValidateOperation
	if authorize {
		operation = port.ProviderCredentialRenewalAuthorizeOperation
	}
	if principal.CallerWorkload != "secret-broker" || principal.Permission != operation || principal.ProjectRef != "" || !validProviderRenewalBinding(task) {
		return errs.ErrForbidden
	}
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return errs.ErrUnavailable
	}
	defer rollbackAssistantLockedRead(ctx, tx, &resultErr)
	locked, err := lockProviderRenewal(ctx, tx, current.organizationID, task.Ref)
	if err != nil {
		return err
	}
	if !sameProviderRenewalBinding(task, locked.task) || !locked.eligible {
		return errs.ErrForbidden
	}
	if authorize {
		if !locked.task.ExpiresAt.After(locked.now) || locked.task.State != "CLAIMED" && locked.task.State != "AUTHORIZED" {
			return errs.ErrForbidden
		}
		var eligible bool
		if err := tx.QueryRow(ctx, queryProviderRenewalInputEligible, current.organizationID, locked.accountID, task.AccountVersion, locked.credentialID).Scan(&eligible); err != nil {
			return errs.ErrUnavailable
		}
		if !eligible {
			return errs.ErrForbidden
		}
		if locked.task.State == "CLAIMED" {
			tag, err := tx.Exec(ctx, queryProviderRenewalAuthorizeTask, locked.id)
			if err != nil || tag.RowsAffected() != 1 {
				return errs.ErrConflict
			}
		}
	} else if locked.task.State != "AUTHORIZED" && locked.task.State != "UNKNOWN_OUTCOME" {
		return errs.ErrForbidden
	}
	if tx.Commit(ctx) != nil {
		return errs.ErrUnavailable
	}
	return nil
}
