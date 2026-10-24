package platform

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// queryConfigurationSourceWorkCandidates selects tenant-owned QUEUED or expired CLAIMED work
// references in stable creation/ID order within the caller limit; row locking and fresh
// eligibility occur separately.
// queryConfigurationSourceWorkCandidates выбирает принадлежащие tenant QUEUED либо истёкшие
// CLAIMED work references в устойчивом creation/ID порядке с caller limit; row locking и fresh
// eligibility выполняются отдельно.
//
//go:embed sql/configuration_source__work_candidates.sql
var queryConfigurationSourceWorkCandidates string

// queryConfigurationSourceLockWork locks configuration, source and exact tenant/work rows while
// reading saved input/digest/receipt plus root actor/organization lineage and current lease
// fields.
// queryConfigurationSourceLockWork блокирует configuration, source и точные tenant/work rows при
// чтении сохранённых input/digest/receipt, root actor/organization lineage и текущих lease fields.
//
//go:embed sql/configuration_source__lock_work.sql
var queryConfigurationSourceLockWork string

// queryConfigurationSourceClaim marks open work CLAIMED with the selected attempt, increments
// claim generation and installs claimant/fence/expiry; the caller owns prior eligibility and
// locked-row selection.
// queryConfigurationSourceClaim отмечает открытую work как CLAIMED с выбранной attempt,
// увеличивает claim generation и назначает claimant/fence/expiry; предшествующие eligibility и
// locked-row selection принадлежат caller.
//
//go:embed sql/configuration_source__claim.sql
var queryConfigurationSourceClaim string

// queryConfigurationSourceState updates exact source state/failure/version and schedules its next
// refresh five minutes later; source audit and event writes are separate steps in the same caller
// transaction.
// queryConfigurationSourceState обновляет точные source state/failure/version и назначает
// следующий refresh через пять минут; source audit и event writes выполняются отдельно в той же
// caller transaction.
//
//go:embed sql/configuration_source__state.sql
var queryConfigurationSourceState string

// queryConfigurationSourceRenew updates expiry only for the identified CLAIMED work, with exact
// lease and current root eligibility checked by the caller under its transaction lock.
// queryConfigurationSourceRenew обновляет expiry только указанной CLAIMED work; точный lease и
// текущая root eligibility проверяются caller под transaction lock.
//
//go:embed sql/configuration_source__renew.sql
var queryConfigurationSourceRenew string

// queryConfigurationSourceFinish sets terminal work state, durable receipt/failure/completion
// digest only for open QUEUED/CLAIMED rows, preserving existing terminal records.
// queryConfigurationSourceFinish назначает terminal work state, устойчивые
// receipt/failure/completion digest только открытым QUEUED/CLAIMED rows, сохраняя существующие
// terminal records.
//
//go:embed sql/configuration_source__finish.sql
var queryConfigurationSourceFinish string

// sourceWorkReceipt stores one lease-attempt/claim-generation action, immutable input digest and
// safe source result for exact completion/failure replay, without holding a live grant.
// sourceWorkReceipt хранит action, immutable input digest и безопасный source result одной
// lease-attempt/claim-generation для точного completion/failure replay без владения живым grant.
type sourceWorkReceipt struct {
	Action, Digest string
	Result         entity.ManagedConfigurationGitSource
}

// sourceWorkRow pairs locked internal work/state/root-owner IDs with decoded immutable work,
// current lease/deadline and durable receipts; copied fields alone do not authorize work
// execution.
// sourceWorkRow связывает locked internal work/state/root-owner IDs с декодированной immutable
// work, текущими lease/deadline и устойчивыми receipts; копия fields сама не авторизует исполнение
// работы.
type sourceWorkRow struct {
	id, state, configurationRef, actorRef, organizationRef string
	work                                                   entity.ManagedConfigurationSourceWork
	lease                                                  entity.ManagedConfigurationSourceLease
	deadline                                               time.Time
	receipts                                               map[string]sourceWorkReceipt
}

// lockSourceWork reads the exact tenant/work fourteen-column lock tuple, rejects missing/driver
// failures distinctly and decodes one snapshot with unknown-field/trailing-value rejection plus
// receipt JSON. It verifies the re-encoded typed JSON digest and exact work/configuration/deadline
// pins, derives source generation from the snapshot and preserves nullable expiry; all failures
// return zero output, and operation/root authority is checked elsewhere.
// lockSourceWork читает четырнадцать колонок lock tuple точных tenant/work, различает
// missing/driver failures и декодирует один snapshot с запретом unknown fields/trailing values и
// receipt JSON. Проверяет digest повторно закодированного typed JSON и точные
// work/configuration/deadline pins, берёт source generation из snapshot и сохраняет nullable
// expiry; ошибки дают нулевой результат, operation/root authority проверяется отдельно.
func lockSourceWork(ctx context.Context, tx pgx.Tx, current scope, ref string) (sourceWorkRow, error) {
	var row sourceWorkRow
	var raw, receipts []byte
	var digest string
	var expires *time.Time
	row.lease.WorkRef = ref
	err := tx.QueryRow(ctx, queryConfigurationSourceLockWork, current.organizationID, ref).Scan(&row.id, &row.state, &row.lease.Attempt, &row.lease.ClaimGeneration, &row.lease.Claimant, &row.lease.Fence, &expires,
		&row.deadline, &raw, &digest, &receipts, &row.configurationRef, &row.actorRef, &row.organizationRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return sourceWorkRow{}, errs.ErrNotFound
	}
	if err != nil {
		return sourceWorkRow{}, errs.ErrUnavailable
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&row.work) != nil || decoder.Decode(&struct{}{}) != io.EOF || json.Unmarshal(receipts, &row.receipts) != nil {
		return sourceWorkRow{}, errs.ErrUnavailable
	}
	if jsonValueDigest(row.work) != digest || row.work.Lease.WorkRef != ref || row.work.ConfigurationRef != row.configurationRef || !row.work.Deadline.Equal(row.deadline) {
		return sourceWorkRow{}, errs.ErrUnavailable
	}
	row.lease.SourceGeneration = row.work.Lease.SourceGeneration
	if expires != nil {
		row.lease.ExpiresAt = *expires
	}
	return row, nil
}

// sourceWorkEligibility re-resolves source generation/identity and non-detached state, then
// current root actor membership and exact source actor/tenant ownership. It reruns canonical
// configuration/connection/read-capability authority and compares saved definition, credential
// descriptor and public configuration with current work inputs; returned partial source/actor
// allows callers to record a blocked result instead of fabricating authority.
// sourceWorkEligibility повторно разрешает source generation/identity и non-detached state, затем
// текущую root actor membership и точное source actor/tenant ownership. Повторяет канонические
// configuration/connection/read-capability проверки и сравнивает сохранённые definition,
// credential descriptor и public configuration с текущими work inputs; частичные source/actor
// позволяют caller записать blocked result без выдумывания authority.
func (repository *Repository) sourceWorkEligibility(ctx context.Context, tx pgx.Tx, row sourceWorkRow, current scope) (configurationSource, scope, error) {
	source, err := readConfigurationSource(ctx, tx, current.organizationID, row.configurationRef)
	if err != nil {
		return configurationSource{}, scope{}, errs.ErrForbidden
	}
	if source.Generation != row.lease.SourceGeneration || source.Ref != row.work.SourceRef || source.State == entity.ConfigurationSourceDetached {
		return source, scope{}, errs.ErrForbidden
	}
	var actor scope
	if err := tx.QueryRow(ctx, queryRepositoryResolvescopeSelectMembershipsOrganizationIdSubjectIdActive, row.actorRef, row.organizationRef).Scan(
		&actor.organizationID, &actor.organizationRef, &actor.actorID, &actor.actorRef, &actor.actorName, &actor.role); err != nil {
		return source, scope{}, errs.ErrForbidden
	}
	actor.correlationRef = current.correlationRef
	if actor.organizationID != current.organizationID || actor.actorID != source.actorID {
		return source, actor, errs.ErrForbidden
	}
	kind := command.RefreshIntegrationDefinitionGitSource
	if row.work.Kind == "ROLE_IMAGE" {
		kind = command.RefreshRoleImageGitSource
	}
	set, err := repository.configurationSourceAuthority(ctx, tx, actor, command.Command{Kind: kind, Payload: command.ManagedConfigurationGitSourceInput{ConfigurationRef: row.configurationRef}})
	if err != nil || set.ManagedBy != "GIT" || set.Source != source.Ref {
		return source, actor, errs.ErrForbidden
	}
	payload := command.ManagedConfigurationGitSourceInput{ConfigurationRef: set.Ref, ConnectionRef: row.work.ConnectionRef, ExpectedConnectionVersion: row.work.ConnectionVersion,
		RepositoryRef: row.work.RepositoryRef, RefName: row.work.RefName, Path: row.work.Path, ContentFormat: row.work.ContentFormat}
	fresh, _, _, err := repository.sourceInput(ctx, tx, actor, set, payload)
	if err != nil {
		return source, actor, err
	}
	if fresh.DefinitionDigest != row.work.DefinitionDigest || fresh.DefinitionVersion != row.work.DefinitionVersion ||
		!bytes.Equal(asJSON(fresh.CredentialRevision), asJSON(row.work.CredentialRevision)) || !bytes.Equal(asJSON(fresh.PublicConfiguration), asJSON(row.work.PublicConfiguration)) {
		return source, actor, errs.ErrForbidden
	}
	return source, actor, nil
}

// sourceLeaseMatches compares CLAIMED state and exact work/source generation/attempt/claim
// generation/claimant/nonempty fence, requires a nonzero submitted expiry no later than the stored
// expiry and checks liveness against stored expiry/deadline. It neither reauthorizes the root
// actor nor requires the submitted older expiry itself to remain in the future.
// sourceLeaseMatches сравнивает CLAIMED state и точные work/source generation/attempt/claim
// generation/claimant/nonempty fence, требует ненулевой переданный expiry не позже сохранённого и
// проверяет срок по stored expiry/deadline. Не переавторизует root actor и не требует, чтобы
// переданный прежний expiry сам оставался в будущем.
func sourceLeaseMatches(row sourceWorkRow, lease entity.ManagedConfigurationSourceLease, now time.Time) bool {
	return row.state == "CLAIMED" && lease.WorkRef == row.lease.WorkRef && lease.SourceGeneration == row.lease.SourceGeneration &&
		lease.Attempt == row.lease.Attempt && lease.ClaimGeneration == row.lease.ClaimGeneration && lease.Claimant == row.lease.Claimant &&
		lease.Fence != "" && lease.Fence == row.lease.Fence && !lease.ExpiresAt.IsZero() && !lease.ExpiresAt.After(row.lease.ExpiresAt) &&
		now.Before(row.lease.ExpiresAt) && now.Before(row.deadline)
}

// sourceReceiptKey formats attempt/claim-generation as the stable per-lease receipt key; caller
// work locking and action digest bind the remaining source/lease coordinates.
// sourceReceiptKey формирует attempt/claim-generation как устойчивый per-lease receipt key; caller
// work locking и action digest связывают остальные source/lease координаты.
func sourceReceiptKey(lease entity.ManagedConfigurationSourceLease) string {
	return strconv.FormatInt(lease.Attempt, 10) + "/" + strconv.FormatInt(lease.ClaimGeneration, 10)
}

// sourceState updates source state/version and clears public failure outside SYNC_BLOCKED, then
// writes the existing source audit and MANAGED_CONFIGURATION_CHANGED event snapshot in the caller
// transaction. It returns the updated safe source projection without committing; each invocation
// retains these actual audit/event effects, including repeated state labels.
// sourceState обновляет source state/version и очищает публичный failure вне SYNC_BLOCKED, затем
// записывает прежние source audit и MANAGED_CONFIGURATION_CHANGED event snapshot в caller
// transaction. Возвращает обновлённое безопасное source projection без commit; каждый вызов
// сохраняет эти фактические audit/event effects, включая повторные state labels.
func (repository *Repository) sourceState(ctx context.Context, tx pgx.Tx, current scope, source configurationSource, state, failure string) (entity.ManagedConfigurationGitSource, error) {
	if state != entity.ConfigurationSourceBlocked {
		failure = ""
	}
	if _, err := tx.Exec(ctx, queryConfigurationSourceState, source.id, state, failure); err != nil {
		return entity.ManagedConfigurationGitSource{}, errs.ErrUnavailable
	}
	source.State, source.FailureCode, source.Version = state, failure, source.Version+1
	auditRef, err := newRef("aud")
	if err != nil {
		return entity.ManagedConfigurationGitSource{}, errs.ErrUnavailable
	}
	if _, err := tx.Exec(ctx, queryCommandsExecuteInsertAuditEventsRefProjectIdAction, auditRef, current.organizationID, nil,
		current.actorID, "configuration-source."+state, "MANAGED_CONFIGURATION_SOURCE", source.Ref, "i18n:MANAGED_CONFIGURATION_CHANGED", current.correlationRef); err != nil {
		return entity.ManagedConfigurationGitSource{}, errs.ErrUnavailable
	}
	if err := repository.emitPlatformEventSnapshot(ctx, tx, current, "MANAGED_CONFIGURATION_CHANGED", source.projectRef,
		source.Ref, "i18n:MANAGED_CONFIGURATION_CHANGED", source.Version, state); err != nil {
		return entity.ManagedConfigurationGitSource{}, err
	}
	return source.ManagedConfigurationGitSource, nil
}

// ClaimConfigurationSourceWork runs due-source refresh and bounded candidate reads in one
// Serializable transaction, closing rows before exact work locks and fresh root eligibility.
// Ineligible/expired/exhausted work is blocked and terminalized; eligible work gets server fence,
// monotonic claim generation and a one-minute lease capped by immutable deadline. It preserves
// initial/retry/reclaim attempt rules and commits maintenance even when no new claim is returned;
// the domain service validates claimant/limit and worker permission.
// ClaimConfigurationSourceWork выполняет due-source refresh и ограниченное чтение кандидатов одной
// Serializable transaction, закрывая rows до точных work locks и fresh root eligibility.
// Ineligible/expired/exhausted work блокируется и завершается; допустимая получает server fence,
// монотонную claim generation и минутный lease с пределом immutable deadline. Сохраняет
// initial/retry/reclaim attempt rules и делает commit обслуживания даже без нового claim; domain
// service проверяет claimant/limit и worker permission.
func (repository *Repository) ClaimConfigurationSourceWork(ctx context.Context, p value.Principal, claimant string, limit int32) ([]entity.ManagedConfigurationSourceWork, error) {
	current, tx, err := repository.beginSourceWorkTransaction(ctx, p)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := repository.refreshConfigurationSources(ctx, tx, current, limit); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, queryConfigurationSourceWorkCandidates, current.organizationID, limit)
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	refs := []string{}
	for rows.Next() {
		var ref string
		if rows.Scan(&ref) != nil {
			rows.Close()
			return nil, errs.ErrUnavailable
		}
		refs = append(refs, ref)
	}
	rows.Close()
	if rows.Err() != nil {
		return nil, errs.ErrUnavailable
	}
	result := make([]entity.ManagedConfigurationSourceWork, 0, len(refs))
	for _, ref := range refs {
		row, err := lockSourceWork(ctx, tx, current, ref)
		if err != nil {
			return nil, err
		}
		now := time.Now().UTC().Truncate(time.Microsecond)
		if row.state != "QUEUED" && (row.state != "CLAIMED" || now.Before(row.lease.ExpiresAt)) {
			continue
		}
		source, actor, eligibilityErr := repository.sourceWorkEligibility(ctx, tx, row, current)
		if eligibilityErr != nil || !now.Before(row.deadline) || row.state == "CLAIMED" && row.lease.Attempt >= 3 {
			if source.id == "" {
				return nil, errs.ErrUnavailable
			}
			failure := entity.ConfigurationSourceAccess
			if eligibilityErr == nil {
				failure = entity.ConfigurationSourceUnavailable
			}
			if _, err := repository.sourceState(ctx, tx, current, source, entity.ConfigurationSourceBlocked, failure); err != nil {
				return nil, err
			}
			if _, err := tx.Exec(ctx, queryConfigurationSourceFinish, row.id, "EXPIRED", asJSON(row.receipts), failure, ""); err != nil {
				return nil, errs.ErrUnavailable
			}
			continue
		}
		attempt := row.lease.Attempt
		if row.state == "CLAIMED" {
			attempt++
		}
		fence, err := newRef("mcsf")
		if err != nil {
			return nil, errs.ErrUnavailable
		}
		expires := now.Truncate(time.Microsecond).Add(time.Minute)
		if expires.After(row.deadline) {
			expires = row.deadline
		}
		if _, err := tx.Exec(ctx, queryConfigurationSourceClaim, row.id, attempt, claimant, fence, expires); err != nil {
			return nil, errs.ErrConflict
		}
		if _, err := repository.sourceState(ctx, tx, actor, source, entity.ConfigurationSourceClaimed, ""); err != nil {
			return nil, err
		}
		row.work.Lease = entity.ManagedConfigurationSourceLease{WorkRef: ref, SourceGeneration: row.lease.SourceGeneration, Attempt: attempt, ClaimGeneration: row.lease.ClaimGeneration + 1, Claimant: claimant, Fence: fence, ExpiresAt: expires}
		result = append(result, row.work)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, errs.ErrConflict
	}
	return result, nil
}

// RenewConfigurationSourceWork locks exact work in a Serializable transaction, checks current
// lease and root eligibility, then extends from current microsecond-truncated time by one minute
// without exceeding deadline or shortening stored expiry. It commits before returning the updated
// stored lease and does not create a separate renew receipt or source-state event in this method.
// RenewConfigurationSourceWork блокирует точную work в Serializable transaction, проверяет текущие
// lease и root eligibility, затем продлевает на минуту от текущего времени с точностью до
// микросекунды без выхода за deadline либо сокращения stored expiry. Делает commit до возврата
// обновлённого stored lease; отдельные renew receipt либо source-state event в этом методе не
// создаются.
func (repository *Repository) RenewConfigurationSourceWork(ctx context.Context, p value.Principal, lease entity.ManagedConfigurationSourceLease) (entity.ManagedConfigurationSourceLease, error) {
	current, tx, err := repository.beginSourceWorkTransaction(ctx, p)
	if err != nil {
		return entity.ManagedConfigurationSourceLease{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	row, err := lockSourceWork(ctx, tx, current, lease.WorkRef)
	if err != nil {
		return entity.ManagedConfigurationSourceLease{}, err
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	if !sourceLeaseMatches(row, lease, now) {
		return entity.ManagedConfigurationSourceLease{}, errs.ErrForbidden
	}
	if _, _, err := repository.sourceWorkEligibility(ctx, tx, row, current); err != nil {
		return entity.ManagedConfigurationSourceLease{}, err
	}
	expires := now.Add(time.Minute)
	if expires.After(row.deadline) {
		expires = row.deadline
	}
	if expires.Before(row.lease.ExpiresAt) {
		expires = row.lease.ExpiresAt
	}
	if _, err := tx.Exec(ctx, queryConfigurationSourceRenew, row.id, expires); err != nil {
		return entity.ManagedConfigurationSourceLease{}, errs.ErrUnavailable
	}
	if err := tx.Commit(ctx); err != nil {
		return entity.ManagedConfigurationSourceLease{}, errs.ErrConflict
	}
	row.lease.ExpiresAt = expires
	return row.lease, nil
}

// beginSourceWorkTransaction resolves the unchanged principal scope before opening a Serializable
// transaction for source claim/renew/fail/complete, preserving resolve errors and mapping only begin
// failure to Unavailable. It does not check worker/root eligibility, acquire work locks, set a new
// context budget or own rollback/commit; each successful caller retains its original defer and flow.
// beginSourceWorkTransaction разрешает scope неизменённого principal до открытия Serializable
// transaction для source claim/renew/fail/complete, сохраняя resolve errors и преобразуя только begin
// failure в Unavailable. Не проверяет worker/root eligibility, не блокирует work, не меняет context
// budget и не владеет rollback/commit; успешный caller сохраняет прежние defer и порядок действий.
func (repository *Repository) beginSourceWorkTransaction(ctx context.Context, p value.Principal) (scope, pgx.Tx, error) {
	current, err := repository.resolveScope(ctx, p)
	if err != nil {
		return scope{}, nil, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return scope{}, nil, errs.ErrUnavailable
	}
	return current, tx, nil
}
