package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/jackc/pgx/v5"
)

// queryResumableSessionCandidates Embeds the bounded read-only candidate projection: latest
// matching successful run per active session, no active turn, exact run-view and target launch
// eligibility, runtime readiness and pinned catalog binding. Its 10001-row limit detects overflow
// rather than granting execution or locking commands.
// queryResumableSessionCandidates Встраивает ограниченную выборку: последний подходящий успешный
// запуск активной сессии без активного turn, право просмотра и запуска цели, готовность runtime и
// закреплённая привязка каталога. Лимит 10001 выявляет переполнение; выборка не разрешает
// выполнение и не блокирует команды.
//
//go:embed sql/resumable_sessions__candidates.sql
var queryResumableSessionCandidates string

// queryResumableSessionGet Embeds the exact organization/run lookup for a successful run in an
// active session without active turns, scoped by project authority and run-view access. It does
// not require the latest session run; callers separately validate current target and catalog
// compatibility.
// queryResumableSessionGet Встраивает поиск успешного запуска активной сессии по организации и
// ссылке без активных turn, с проверкой проектной границы и просмотра. Не требует последнего
// запуска сессии; совместимость текущей цели и каталога проверяют вызывающие.
//
//go:embed sql/resumable_sessions__get.sql
var queryResumableSessionGet string

// applyContinuationEventAction Leaves nil events/runs unchanged, projects run identity, state and
// actions into a temporary value and copies actions back only after continuation validation
// succeeds; it does not start a turn.
// applyContinuationEventAction Оставляет пустое событие или запуск без изменений, проверяет
// продолжение на временной проекции идентификатора, состояния и действий, возвращая действия в
// событие только при успехе. Новый turn не запускает.
func (repository *Repository) applyContinuationEventAction(ctx context.Context, runner queryRunner, current scope, event *entity.RunEvent) error {
	if event == nil || event.Delta.Run == nil {
		return nil
	}
	run := entity.Run{Ref: event.Delta.Run.Ref, State: event.Delta.Run.State, NextActions: event.Delta.Run.NextActions}
	if err := repository.applyContinuationAction(ctx, runner, current, &run); err != nil {
		return err
	}
	event.Delta.Run.NextActions = run.NextActions
	return nil
}

// applyContinuationAction Removes an existing ADD_TURN hint before validation and only considers
// successful runs. Requires a transaction, treats missing/ineligible candidates as absent hints,
// propagates infrastructure failures and appends the hint only after current target/catalog
// validation; the hint itself grants no authority.
// applyContinuationAction Удаляет прежнюю подсказку ADD_TURN до проверки и рассматривает лишь
// успешные запуски. Требует транзакцию, скрывает подсказку для отсутствующих или неподходящих
// кандидатов, возвращает инфраструктурную ошибку и добавляет действие после проверки цели и
// каталога. Подсказка не выдаёт полномочия.
func (repository *Repository) applyContinuationAction(ctx context.Context, runner queryRunner, current scope, run *entity.Run) error {
	if run == nil {
		return nil
	}
	run.NextActions = slices.DeleteFunc(run.NextActions, func(action string) bool { return action == "ADD_TURN" })
	if run.State != "SUCCEEDED" {
		return nil
	}
	tx, ok := runner.(pgx.Tx)
	if !ok {
		return errs.ErrUnavailable
	}
	var item resumableSessionCandidate
	err := tx.QueryRow(ctx, queryResumableSessionGet, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "actor_id": current.actorID,
		"authority_project_id": current.authorityProjectID, "run_ref": run.Ref,
	}).Scan(&item.RunRef, &item.Version, &item.SessionID, &item.SessionRef, &item.ProjectID, &item.ProjectRef, &item.TargetType, &item.TargetRef, &item.AccountRef, &item.InputJSON)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return errs.ErrUnavailable
	}
	if err := repository.validateContinuationSnapshot(ctx, tx, current, item); err != nil {
		if continuationIneligible(err) {
			return nil
		}
		return err
	}
	run.NextActions = append(run.NextActions, "ADD_TURN")
	return nil
}

// continuationIneligible Classifies only conflict, not-found, forbidden, version-mismatch and
// invalid domain errors as a candidate becoming ineligible, including wrapped errors. Other
// failures must abort validation rather than silently suppressing an unavailable dependency.
// continuationIneligible Относит к непригодности кандидата только доменные conflict, not-found,
// forbidden, version-mismatch и invalid, включая обёрнутые ошибки. Остальные сбои прерывают
// проверку, чтобы недоступная зависимость не скрывалась.
func continuationIneligible(err error) bool {
	return errors.Is(err, errs.ErrConflict) || errors.Is(err, errs.ErrNotFound) || errors.Is(err, errs.ErrForbidden) || errors.Is(err, errs.ErrVersionMismatch) || errors.Is(err, errs.ErrInvalid)
}

// resumableSessionCandidate Carries the query-owned run/session/project/target/account identity,
// version/time and immutable input/spec plus current runtime configuration, overlay, agent
// references and session catalog binding used for continuation eligibility. It is not a command
// grant or lock.
// resumableSessionCandidate Хранит полученные выборкой идентификаторы запуска, сессии, проекта,
// цели и аккаунта, версию, время, вход и спецификацию, текущую конфигурацию runtime, overlay,
// агентов и привязку каталога. Это данные проверки пригодности, а не разрешение команды или
// блокировка.
type resumableSessionCandidate struct {
	RunRef, SessionID, SessionRef, ProjectID, ProjectRef string
	TargetType, TargetRef, AccountRef                    string
	Version                                              int64
	CreatedAt                                            time.Time
	TargetSpec                                           []byte
	InputJSON                                            []byte
	AgentRefs                                            []string
	Configuration                                        entity.AgentRuntimeConfiguration
	Overlay                                              string
	Binding                                              sessionCatalogBinding
}

// resumableSessionCursor Carries scope, descending creation-time/reference position and an
// eligible-run-list fingerprint in an unsigned pagination token. The server recomputes scope and
// snapshot; the cursor is not an authority credential or a digest of every dependency.
// resumableSessionCursor Хранит область выборки, позицию по убывающим времени и ссылке и отпечаток
// списка пригодных запусков в неподписанном токене страниц. Сервер пересчитывает область и снимок;
// курсор не подтверждает полномочия и не охватывает все зависимости.
type resumableSessionCursor struct {
	Scope, Ref, Snapshot string
	CreatedAt            time.Time
}

// resumableSessionAfterCursor Accepts all candidates for an empty cursor, otherwise compares
// creation time and then reference strictly below the cursor in descending order. Eligibility and
// snapshot validation remain separate.
// resumableSessionAfterCursor При пустом курсоре пропускает всех кандидатов; иначе выбирает строго
// следующую позицию по убывающим времени создания и ссылке. Пригодность кандидата и снимок
// проверяются отдельно.
func resumableSessionAfterCursor(item resumableSessionCandidate, cursor resumableSessionCursor) bool {
	return cursor.Ref == "" || item.CreatedAt.Before(cursor.CreatedAt) ||
		(item.CreatedAt.Equal(cursor.CreatedAt) && item.RunRef < cursor.Ref)
}

// continuationTargetSnapshot Caches current target runtime configuration, overlay, workflow input
// fields and any target-read failure inside one validation scope. It is a read snapshot, not
// execution admission or a transferable authorization result.
// continuationTargetSnapshot Хранит текущую конфигурацию цели, overlay, поля входа workflow и
// ошибку чтения в пределах одной проверки. Это снимок чтения, а не допуск выполнения или
// переносимое подтверждение полномочий.
type continuationTargetSnapshot struct {
	configuration  entity.AgentRuntimeConfiguration
	overlay        string
	workflowInputs []entity.WorkflowInputField
	err            error
}

// continuationValidationCache Holds target snapshots and catalog-validation results, including nil
// success, only for one transaction/request authority scope. Callers initialize writable maps;
// sharing across actors, requests or snapshots would bypass required fresh reads.
// continuationValidationCache Хранит снимки целей и результаты проверки каталога, включая успешный
// nil, только для одной транзакции и области полномочий запроса. Вызывающий создаёт изменяемые
// карты; перенос между actor, запросами или снимками обошёл бы свежую проверку.
type continuationValidationCache struct {
	targets  map[string]continuationTargetSnapshot
	catalogs map[string]error
}

// maximumResumableSessionCandidates Caps a complete eligibility scan at 10000 candidates; the
// extra SQL row makes overflow fail the whole request instead of returning a misleading partial
// total or snapshot.
// maximumResumableSessionCandidates Ограничивает полный просмотр пригодности 10000 кандидатами;
// дополнительная строка SQL заставляет отклонить переполнение целиком вместо неполного total или
// снимка.
const maximumResumableSessionCandidates = 10_000

// listResumableSessions Uses a 15-second repeatable-read read-only transaction to validate
// filters/cursor and optional target access, read at most 10000 candidates, close rows before
// dependent queries and retain only eligible candidates. It fingerprints eligible run
// reference/version/time, rejects changed pagination snapshots, reads page details and commits
// before returning; no turn is claimed or executed.
// listResumableSessions За 15 секунд в read-only repeatable-read транзакции проверяет фильтры,
// курсор и доступ к явной цели, читает не более 10000 кандидатов, закрывает rows перед зависимыми
// запросами и оставляет пригодных. Отпечаток строится по ссылке, версии и времени пригодных
// запусков; изменённый снимок страниц отклоняется, детали возвращаются после commit. Захват и
// запуск turn не выполняются.
func (repository *Repository) listResumableSessions(ctx context.Context, current scope, filter query.Filter) ([]entity.Run, int64, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if len(filter.States) != 0 || len([]rune(filter.Query)) > 200 || !utf8.ValidString(filter.Query) || strings.ContainsRune(filter.Query, '\x00') {
		return nil, 0, "", errs.ErrInvalid
	}
	if (filter.TargetRef == "") != (filter.TargetType == "") || filter.TargetType != "" && !slices.Contains([]string{"AGENT", "WORKFLOW"}, filter.TargetType) || len(filter.TargetRef) > 96 || !utf8.ValidString(filter.TargetRef) || strings.ContainsRune(filter.TargetRef, '\x00') {
		return nil, 0, "", errs.ErrInvalid
	}
	wantScope := catalogScope(current, "RESUMABLE_SESSION", filter)
	var cursor resumableSessionCursor
	if filter.Page.Token != "" {
		raw, err := base64.RawURLEncoding.DecodeString(filter.Page.Token)
		if err != nil || len(filter.Page.Token) > 512 || json.Unmarshal(raw, &cursor) != nil || cursor.Scope != wantScope || cursor.Ref == "" || cursor.CreatedAt.IsZero() || cursor.Snapshot == "" {
			return nil, 0, "", errs.ErrInvalid
		}
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, 0, "", errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if filter.TargetRef != "" {
		launch := command.Command{Kind: command.LaunchRun, Payload: command.LaunchRunInput{ProjectRef: filter.ProjectRef, Target: entity.RunTarget{Type: filter.TargetType, Ref: filter.TargetRef}}}
		if err := repository.authorizeCommand(ctx, tx, current, launch); err != nil {
			return nil, 0, "", err
		}
	}
	limit := boundedPage(filter.Page)
	selected := make([]resumableSessionCandidate, 0, limit+1)
	validationCache := continuationValidationCache{
		targets:  make(map[string]continuationTargetSnapshot),
		catalogs: make(map[string]error),
	}
	fingerprint := sha256.New()
	var total int64
	rows, err := tx.Query(ctx, queryResumableSessionCandidates, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "actor_id": current.actorID,
		"project_ref": filter.ProjectRef, "authority_project_id": current.authorityProjectID,
		"query": filter.Query, "limit": int32(maximumResumableSessionCandidates + 1),
		"target_type": filter.TargetType, "target_ref": filter.TargetRef,
		"role_runtime_contract_revision": repository.roleImages.RoleRuntimeContractRevision,
		"role_runtime_contract_sha256":   repository.roleImages.RoleRuntimeContractSHA256,
		"default_role_image_digest":      repository.roleImages.DefaultImageDigest,
	})
	if err != nil {
		return nil, 0, "", errs.ErrUnavailable
	}
	batch := make([]resumableSessionCandidate, 0, 512)
	for rows.Next() {
		var item resumableSessionCandidate
		var accountCandidates, bindingModels []byte
		if rows.Scan(&item.RunRef, &item.Version, &item.CreatedAt, &item.SessionID, &item.SessionRef, &item.ProjectID, &item.ProjectRef,
			&item.TargetType, &item.TargetRef, &item.AccountRef, &item.InputJSON, &item.TargetSpec, &item.AgentRefs,
			&item.Configuration.Provider, &item.Configuration.Model, &accountCandidates, &item.Overlay,
			&item.Binding.CatalogRevision, &item.Binding.CatalogDigest, &bindingModels, &item.Binding.Provider,
			&item.Binding.PolicyID, &item.Binding.PolicyRef, &item.Binding.PolicyVersion, &item.Binding.PolicyDigest) != nil ||
			decodeStrict(accountCandidates, &item.Configuration.ProviderPolicy.AccountCandidates) != nil ||
			decodeStrict(bindingModels, &item.Binding.Models) != nil {
			rows.Close()
			return nil, 0, "", errs.ErrUnavailable
		}
		batch = append(batch, item)
	}
	rows.Close()
	if rows.Err() != nil || len(batch) > maximumResumableSessionCandidates {
		return nil, 0, "", errs.ErrUnavailable
	}
	for _, item := range batch {
		if err := repository.validatePreparedContinuationSnapshot(ctx, tx, current, item, &validationCache); err != nil {
			if continuationIneligible(err) {
				continue
			}
			return nil, 0, "", err
		}
		total++
		_, _ = fmt.Fprintf(fingerprint, "%s:%d:%s\n", item.RunRef, item.Version, item.CreatedAt.Format(time.RFC3339Nano))
		if resumableSessionAfterCursor(item, cursor) && len(selected) <= int(limit) {
			selected = append(selected, item)
		}
	}
	snapshot := base64.RawURLEncoding.EncodeToString(fingerprint.Sum(nil))
	if cursor.Snapshot != "" && cursor.Snapshot != snapshot {
		return nil, 0, "", errs.ErrVersionMismatch
	}
	next := ""
	if len(selected) > int(limit) {
		selected = selected[:limit]
		last := selected[len(selected)-1]
		raw, _ := json.Marshal(resumableSessionCursor{Scope: wantScope, Snapshot: snapshot, Ref: last.RunRef, CreatedAt: last.CreatedAt})
		next = base64.RawURLEncoding.EncodeToString(raw)
	}
	items := make([]entity.Run, 0, len(selected))
	for _, candidate := range selected {
		item, err := repository.readRunWithIncidents(ctx, tx, current, candidate.RunRef)
		if err != nil {
			return nil, 0, "", err
		}
		item.NextActions = []string{"OPEN", "ADD_TURN"}
		items = append(items, item)
	}
	if ctx.Err() != nil {
		return nil, 0, "", errs.ErrUnavailable
	}
	if tx.Commit(ctx) != nil {
		return nil, 0, "", errs.ErrUnavailable
	}
	return items, total, next, nil
}

// validatePreparedContinuationSnapshot Validates inherited workflow input against a decoded
// versioned specification with a coordinator, then resolves the SQL-prepared pinned catalog
// binding and checks its runtime catalog candidate. Assumes target eligibility/readiness was
// already enforced by the candidate SQL; performs no independent target authorization.
// validatePreparedContinuationSnapshot Проверяет унаследованный вход workflow по декодированной
// версионированной спецификации с координатором, затем разрешает подготовленную SQL привязку
// каталога и проверяет её runtime-кандидата. Пригодность и готовность цели предполагаются уже
// проверенными выборкой; отдельную авторизацию цели метод не выполняет.
func (repository *Repository) validatePreparedContinuationSnapshot(ctx context.Context, tx pgx.Tx, current scope, candidate resumableSessionCandidate, cache *continuationValidationCache) error {
	if candidate.TargetType == "WORKFLOW" {
		var version entity.WorkflowVersion
		if json.Unmarshal(candidate.TargetSpec, &version) != nil || !validWorkflowVersion(version) ||
			version.CoordinatorAgentRef == "" || !validContinuationWorkflowInput(version.Inputs, candidate.InputJSON) {
			return errs.ErrConflict
		}
	}
	selected, _, err := resolveSessionModelCatalogCandidate(candidate.AccountRef, candidate.Configuration, candidate.Binding)
	if err != nil {
		return err
	}
	return repository.validateContinuationCatalogCandidate(ctx, tx, current, candidate.Configuration, candidate.Overlay, selected, cache)
}

// validateContinuationSnapshot Checks current target authorization/runtime readiness and pinned
// session catalog compatibility without a cache. It performs read-side eligibility only; command
// locking, admission and turn creation remain with the command path.
// validateContinuationSnapshot Без кэша проверяет текущие полномочия и готовность runtime цели,
// совместимость закреплённого каталога сессии. Выполняет только проверку чтения; блокировки,
// допуск и создание turn принадлежат командному пути.
func (repository *Repository) validateContinuationSnapshot(ctx context.Context, tx pgx.Tx, current scope, candidate resumableSessionCandidate) error {
	return repository.validateContinuationSnapshotCached(ctx, tx, current, candidate, nil)
}

// validateContinuationSnapshotCached Reuses or reads a target snapshot keyed by project ID, target
// type and reference within the caller-owned cache, then validates this candidate against it.
// Cache lifetime must remain inside the same transaction and authority scope.
// validateContinuationSnapshotCached Повторно использует либо читает снимок цели по ID проекта,
// типу и ссылке из кэша вызывающего, затем проверяет кандидата. Кэш должен оставаться в той же
// транзакции и области полномочий.
func (repository *Repository) validateContinuationSnapshotCached(ctx context.Context, tx pgx.Tx, current scope, candidate resumableSessionCandidate, cache *continuationValidationCache) error {
	targetKey := candidate.ProjectID + "\x00" + candidate.TargetType + "\x00" + candidate.TargetRef
	if cache != nil {
		if snapshot, ok := cache.targets[targetKey]; ok {
			return repository.validateContinuationWithTargetSnapshot(ctx, tx, current, candidate, snapshot, cache)
		}
	}
	snapshot := repository.readContinuationTargetSnapshot(ctx, tx, current, candidate)
	if cache != nil {
		cache.targets[targetKey] = snapshot
	}
	return repository.validateContinuationWithTargetSnapshot(ctx, tx, current, candidate, snapshot, cache)
}

// validateContinuationWithTargetSnapshot Propagates a failed target snapshot, validates inherited
// workflow input when applicable and checks the candidate session catalog against the snapshot
// configuration and overlay. It neither refreshes cached target authority nor creates execution
// state.
// validateContinuationWithTargetSnapshot Возвращает ошибку снимка цели, при необходимости
// проверяет унаследованный вход workflow и каталог сессии относительно конфигурации и overlay
// снимка. Не обновляет полномочия кэшированной цели и не создаёт состояние выполнения.
func (repository *Repository) validateContinuationWithTargetSnapshot(ctx context.Context, tx pgx.Tx, current scope, candidate resumableSessionCandidate, snapshot continuationTargetSnapshot, cache *continuationValidationCache) error {
	if snapshot.err != nil {
		return snapshot.err
	}
	if candidate.TargetType == "WORKFLOW" && !validContinuationWorkflowInput(snapshot.workflowInputs, candidate.InputJSON) {
		return errs.ErrConflict
	}
	return repository.validateContinuationSessionCatalog(ctx, tx, current, candidate, snapshot.configuration, snapshot.overlay, cache)
}

// readContinuationTargetSnapshot Authorizes exact target launch, reads the agent or published
// workflow and coordinator, validates runtime contracts for every involved agent, then reads
// configuration/overlay for the first agent or coordinator. It returns a classified read failure
// in the snapshot and performs no provider execution or claim.
// readContinuationTargetSnapshot Проверяет право запуска точной цели, читает агента либо
// опубликованный workflow с координатором, проверяет runtime-контракт всех агентов и получает
// конфигурацию с overlay первого агента или координатора. Ошибка чтения помещается в снимок;
// провайдер не вызывается и захват не создаётся.
func (repository *Repository) readContinuationTargetSnapshot(ctx context.Context, tx pgx.Tx, current scope, candidate resumableSessionCandidate) continuationTargetSnapshot {
	failure := func(err error) continuationTargetSnapshot { return continuationTargetSnapshot{err: err} }
	launch := command.Command{Kind: command.LaunchRun, Payload: command.LaunchRunInput{
		ProjectRef: candidate.ProjectRef, Target: entity.RunTarget{Type: candidate.TargetType, Ref: candidate.TargetRef},
	}}
	if err := repository.authorizeCommand(ctx, tx, current, launch); err != nil {
		return failure(err)
	}
	var agentRefs []string
	var workflowInputs []entity.WorkflowInputField
	switch candidate.TargetType {
	case "AGENT":
		var name string
		if err := tx.QueryRow(ctx, queryCommandsLaunchrunSelectAgentsOrganizationIdProjectIdRef, current.organizationID, candidate.ProjectID, candidate.TargetRef).Scan(&name); err != nil {
			return failure(continuationReadError(err))
		}
		agentRefs = []string{candidate.TargetRef}
	case "WORKFLOW":
		var name, versionID, versionRef, digest, coordinatorRef, coordinatorName string
		var raw []byte
		if err := tx.QueryRow(ctx, queryCommandsLaunchrunSelectWorkflowsOrganizationIdProjectIdRef, pgx.StrictNamedArgs{
			"organization_id": current.organizationID, "project_id": candidate.ProjectID, "workflow_ref": candidate.TargetRef,
		}).Scan(&name, &versionID, &versionRef, &raw, &digest, &coordinatorRef, &coordinatorName); err != nil {
			return failure(continuationReadError(err))
		}
		var version entity.WorkflowVersion
		if json.Unmarshal(raw, &version) != nil || !validWorkflowVersion(version) || version.CoordinatorAgentRef != coordinatorRef {
			return failure(errs.ErrConflict)
		}
		agentRefs = append(agentRefs, coordinatorRef)
		workflowInputs = version.Inputs
		for _, step := range version.Steps {
			agentRefs = append(agentRefs, step.AgentRef)
		}
	default:
		return failure(errs.ErrConflict)
	}
	var ready bool
	if err := tx.QueryRow(ctx, queryCommandsLaunchrunValidateAgentRuntimeContract, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "project_id": candidate.ProjectID, "agent_refs": agentRefs,
		"role_runtime_contract_revision": repository.roleImages.RoleRuntimeContractRevision,
		"role_runtime_contract_sha256":   repository.roleImages.RoleRuntimeContractSHA256,
		"default_role_image_digest":      repository.roleImages.DefaultImageDigest,
	}).Scan(&ready); err != nil {
		return failure(errs.ErrUnavailable)
	}
	if !ready {
		return failure(errs.ErrConflict)
	}
	configuration, overlay, err := readRuntimeCatalogConfiguration(ctx, tx, current.organizationID, agentRefs[0], "")
	if err != nil {
		return failure(err)
	}
	return continuationTargetSnapshot{configuration: configuration, overlay: overlay, workflowInputs: workflowInputs}
}

// validContinuationWorkflowInput Requires nonempty JSON decodable as an object and validates it
// against workflow input fields. This shape check does not authorize the workflow, resolve a
// provider or admit a turn.
// validContinuationWorkflowInput Требует непустой JSON, декодируемый в объект, и проверяет его по
// полям входа workflow. Проверка формы не авторизует workflow, не выбирает провайдера и не
// допускает turn.
func validContinuationWorkflowInput(fields []entity.WorkflowInputField, raw []byte) bool {
	var values map[string]any
	return len(raw) > 0 && json.Unmarshal(raw, &values) == nil && validWorkflowRunInput(fields, values)
}

// validateContinuationSessionCatalog Reads the exact organization/session/account catalog binding,
// resolves its pinned candidate and validates that candidate against current runtime configuration
// and overlay. Read failures use continuation error classification; no replacement account is
// selected.
// validateContinuationSessionCatalog Читает точную привязку каталога организации, сессии и
// аккаунта, разрешает закреплённого кандидата и проверяет его по текущей конфигурации runtime и
// overlay. Ошибки классифицируются для продолжения; другой аккаунт не выбирается.
func (repository *Repository) validateContinuationSessionCatalog(ctx context.Context, tx pgx.Tx, current scope, candidate resumableSessionCandidate, configuration entity.AgentRuntimeConfiguration, overlay string, cache *continuationValidationCache) error {
	binding, err := readSessionModelCatalog(ctx, tx, current.organizationID, candidate.SessionID, candidate.AccountRef)
	if err != nil {
		return err
	}
	selected, _, err := resolveSessionModelCatalogCandidate(candidate.AccountRef, configuration, binding)
	if err != nil {
		return err
	}
	return repository.validateContinuationCatalogCandidate(ctx, tx, current, configuration, overlay, selected, cache)
}

// validateContinuationCatalogCandidate Caches validation success/failure by provider, model,
// overlay and selected candidate within one caller-owned scope, then uses the catalog snapshot
// validator with organization scope and both optional modes disabled. It checks compatibility
// without issuing a grant or selecting a replacement candidate.
// validateContinuationCatalogCandidate Кэширует успех или ошибку по провайдеру, модели, overlay и
// выбранному кандидату в области вызывающего; использует проверку снимка каталога с организацией и
// двумя выключенными дополнительными режимами. Проверяет совместимость, не выдавая grant и не
// подменяя кандидата.
func (repository *Repository) validateContinuationCatalogCandidate(ctx context.Context, tx pgx.Tx, current scope, configuration entity.AgentRuntimeConfiguration, overlay string, selected entity.ProviderAccountCandidate, cache *continuationValidationCache) error {
	rawKey, _ := json.Marshal(struct {
		Provider  string
		Model     string
		Overlay   string
		Candidate entity.ProviderAccountCandidate
	}{configuration.Provider, configuration.Model, overlay, selected})
	catalogKey := string(rawKey)
	if cache != nil {
		if cached, ok := cache.catalogs[catalogKey]; ok {
			return cached
		}
	}
	_, _, err := validateRuntimeCatalogCandidatesSnapshot(ctx, tx, scope{organizationID: current.organizationID}, configuration.Provider, configuration.Model, overlay, []entity.ProviderAccountCandidate{selected}, false, false)
	if cache != nil {
		cache.catalogs[catalogKey] = err
	}
	return err
}

// continuationReadError Maps missing rows, including wrapped pgx.ErrNoRows, to a continuation
// conflict and all other inputs to unavailable. Callers invoke it only for a non-nil read error;
// it hides driver details without retrying.
// continuationReadError Преобразует отсутствие строки, включая обёрнутый pgx.ErrNoRows, в конфликт
// продолжения, остальные входы — в недоступность. Вызывается только при ненулевой ошибке чтения;
// скрывает детали драйвера без повторного запроса.
func continuationReadError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrConflict
	}
	return errs.ErrUnavailable
}
