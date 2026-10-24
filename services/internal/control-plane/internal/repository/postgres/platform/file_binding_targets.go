package platform

import (
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// queryFileBindingTargetArtifact reads exact tenant/authority-project artifact state and its
// project reference, excluding PURGED rows after the caller performs artifact access checks.
// queryFileBindingTargetArtifact читает точное состояние артефакта области
// tenant/authority-project и ссылку проекта, исключая PURGED после проверок доступа к артефакту в
// caller.
//
//go:embed sql/file_binding_target_artifact.sql
var queryFileBindingTargetArtifact string

// queryFileBindingTargetAgents reads at most one hundred ordered project agents and
// binding/capability hints; archived agents remain discoverable for an existing binding or an
// explicit target query.
// queryFileBindingTargetAgents читает до ста упорядоченных агентов проекта и подсказки
// binding/capability; архивный агент остаётся доступным существующей привязке либо явному запросу
// цели.
//
//go:embed sql/file_binding_target_agents.sql
var queryFileBindingTargetAgents string

const (
	// fileTargetAvailable marks an available bind or unbind hint after its separate state predicates;
	// it is not a grant to perform the command.
	// fileTargetAvailable помечает доступную подсказку bind либо unbind после отдельных предикатов
	// состояния; не выдаёт grant на выполнение команды.
	fileTargetAvailable = "AVAILABLE"
	// fileTargetAlreadyBound explains why binding is unavailable when the exact artifact-to-agent
	// relation already exists.
	// fileTargetAlreadyBound объясняет недоступность binding, когда точная связь артефакта с агентом
	// уже существует.
	fileTargetAlreadyBound = "ALREADY_BOUND"
	// fileTargetNotBound explains why unbinding is unavailable when the target agent has no existing
	// artifact relation.
	// fileTargetNotBound объясняет недоступность unbinding, когда у целевого агента нет существующей
	// связи с артефактом.
	fileTargetNotBound = "NOT_BOUND"
	// fileTargetCapabilityRequired explains a bind denial hint when the agent lacks the required
	// artifact capability; it does not block unbinding an existing relation.
	// fileTargetCapabilityRequired объясняет отказ подсказки bind при отсутствии нужной artifact
	// capability у агента; не запрещает удаление существующей привязки.
	fileTargetCapabilityRequired = "AGENT_CAPABILITY_REQUIRED"
	// fileTargetArchived explains why an archived agent cannot receive a new binding while preserving
	// removal of an existing relation.
	// fileTargetArchived объясняет запрет новой привязки архивному агенту с сохранением возможности
	// удалить существующую связь.
	fileTargetArchived = "AGENT_ARCHIVED"
	// fileTargetArtifactUnavailable marks unavailable artifact state; binding requires ACTIVE and
	// CLEAN, while unbinding requires ACTIVE without a clean-scan condition.
	// fileTargetArtifactUnavailable обозначает недоступное состояние артефакта: binding требует ACTIVE
	// и CLEAN, unbinding требует ACTIVE без условия чистого scan.
	fileTargetArtifactUnavailable = "ARTIFACT_UNAVAILABLE"
)

// fileBindingArtifact holds internal and public artifact/project identity, version, lifecycle and
// scan state after authoritative access resolution.
// fileBindingArtifact хранит внутренние и публичные идентификаторы артефакта/проекта, version,
// lifecycle и scan state после авторитетного разрешения доступа.
type fileBindingArtifact struct {
	ID, Ref, ProjectID, ProjectRef, Lifecycle, Scan string
	Version                                         int64
}

// fileBindingAgent adds internal agent/owner identity and capability presence to the public
// binding-target projection used for separate visibility checks.
// fileBindingAgent добавляет внутренние agent/owner identity и наличие capability к публичному
// представлению binding target для отдельной проверки видимости.
type fileBindingAgent struct {
	ID, OwnerRef  string
	HasCapability bool
	entity.ArtifactBindingTarget
}

// fileTargetCursor pins pagination to the caller/artifact/search scope digest, full eligible
// result digest and the last visible agent reference.
// fileTargetCursor связывает пагинацию с дайджестом области caller/artifact/search, дайджестом
// полного допустимого результата и ссылкой последнего видимого агента.
type fileTargetCursor struct{ Scope, Digest, Ref string }

// readFileBindingArtifact requires artifact.bind and artifact projection eligibility before the
// exact tenant/project SQL read, mapping missing rows to not found and driver failures to
// unavailable.
// readFileBindingArtifact требует artifact.bind и допустимость проекции артефакта до точного
// tenant/project SQL read; отсутствие строки даёт not found, отказ драйвера — unavailable.
func (repository *Repository) readFileBindingArtifact(ctx context.Context, tx pgx.Tx, current scope, ref string) (fileBindingArtifact, error) {
	item := fileBindingArtifact{Ref: ref}
	if err := repository.requireAccess(ctx, tx, current, "artifact.bind", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "ARTIFACT", ResourceRef: ref}); err != nil {
		return item, err
	}
	var artifact entity.Artifact
	artifact.Ref = ref
	if err := projectArtifactActions(ctx, tx, current, &artifact); err != nil {
		return item, err
	}
	err := tx.QueryRow(ctx, queryFileBindingTargetArtifact, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "authority_project_id": current.authorityProjectID, "artifact_ref": ref,
	}).Scan(&item.ID, &item.Version, &item.ProjectID, &item.ProjectRef, &item.Lifecycle, &item.Scan)
	if errors.Is(err, pgx.ErrNoRows) {
		return item, errs.ErrNotFound
	}
	if err != nil {
		return item, errs.ErrUnavailable
	}
	return item, nil
}

// readFileBindingAgents reads one bounded agent batch with binding/capability hints and validates
// positive versions and closed state values before returning it.
// readFileBindingAgents читает одну ограниченную пачку агентов с binding/capability hints и
// проверяет положительные версии и закрытые значения состояния до возврата.
func (repository *Repository) readFileBindingAgents(ctx context.Context, tx pgx.Tx, current scope, artifact fileBindingArtifact, agentRef, after, search string) ([]fileBindingAgent, error) {
	rows, err := tx.Query(ctx, queryFileBindingTargetAgents, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "project_id": artifact.ProjectID,
		"artifact_id": artifact.ID, "capability": runtimecontract.ArtifactCapability,
		"agent_ref": agentRef, "after_ref": after, "query": search,
	})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer rows.Close()
	items := []fileBindingAgent{}
	for rows.Next() {
		var item fileBindingAgent
		if rows.Scan(&item.ID, &item.AgentRef, &item.AgentVersion, &item.Name, &item.State, &item.OwnerRef, &item.HasCapability, &item.Bound) != nil {
			return nil, errs.ErrUnavailable
		}
		if item.AgentVersion < 1 || !contains([]string{"DRAFT", "READY", "RUNNING", "DISABLED", "ARCHIVED"}, item.State) {
			return nil, errs.ErrUnavailable
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		return nil, errs.ErrUnavailable
	}
	return items, nil
}

// fileBindingAgentVisible checks agent.view on the exact resolved owner/project target; an
// archived binding tombstone does not expand the general agent catalog.
// fileBindingAgentVisible проверяет agent.view для точной разрешённой owner/project цели; архивный
// tombstone привязки не расширяет общий каталог агентов.
func (repository *Repository) fileBindingAgentVisible(ctx context.Context, tx pgx.Tx, current scope, artifact fileBindingArtifact, item fileBindingAgent) error {
	// Tombstone разрешается owner-запросом только для существующей связи. Общий
	// каталог Agent не расширяется и продолжает исключать архивные записи.
	target := resolvedAccessTarget{resourceID: item.ID, projectID: artifact.ProjectID, ownerSubjectRef: item.OwnerRef,
		scope: entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "AGENT", ResourceRef: item.AgentRef,
			ProjectRef: artifact.ProjectRef, RelatedResourceRefs: map[string]string{"PROJECT": artifact.ProjectRef}}}
	return repository.requireAccess(ctx, tx, current, "agent.view", target)
}

// projectFileBindingTarget derives bind and unbind reasons separately, preserving denial
// precedence and allowing unbinding of archived or incapable agents when the artifact remains
// ACTIVE and bound.
// projectFileBindingTarget выводит причины bind и unbind раздельно, сохраняя приоритет отказов и
// разрешая unbind архивного агента либо агента без capability при ACTIVE артефакте и существующей
// связи.
func projectFileBindingTarget(artifact fileBindingArtifact, item fileBindingAgent) entity.ArtifactBindingTarget {
	result := item.ArtifactBindingTarget
	result.BindReason, result.UnbindReason = fileTargetAvailable, fileTargetAvailable
	switch {
	case artifact.Lifecycle != "ACTIVE" || artifact.Scan != "CLEAN":
		result.BindReason = fileTargetArtifactUnavailable
	case item.State == "ARCHIVED":
		result.BindReason = fileTargetArchived
	case !item.HasCapability:
		result.BindReason = fileTargetCapabilityRequired
	case item.Bound:
		result.BindReason = fileTargetAlreadyBound
	}
	switch {
	case artifact.Lifecycle != "ACTIVE":
		result.UnbindReason = fileTargetArtifactUnavailable
	case !item.Bound:
		result.UnbindReason = fileTargetNotBound
	}
	result.CanBind, result.CanUnbind = result.BindReason == fileTargetAvailable, result.UnbindReason == fileTargetAvailable
	return result
}

// authorizeFileBindingTarget resolves the artifact with bind access, requires exactly one explicit
// agent result and checks its visibility; mutation/version validation remains with the command
// owner.
// authorizeFileBindingTarget разрешает артефакт с bind-доступом, требует ровно один результат
// явного агента и проверяет его видимость; mutation/version validation остаётся у владельца
// команды.
func (repository *Repository) authorizeFileBindingTarget(ctx context.Context, tx pgx.Tx, current scope, payload command.ArtifactBindingInput) error {
	artifact, err := repository.readFileBindingArtifact(ctx, tx, current, payload.ArtifactRef)
	if err != nil {
		return err
	}
	items, err := repository.readFileBindingAgents(ctx, tx, current, artifact, payload.AgentRef, "", "")
	if err != nil {
		return err
	}
	if len(items) != 1 {
		return errs.ErrNotFound
	}
	return repository.fileBindingAgentVisible(ctx, tx, current, artifact, items[0])
}

// ListArtifactBindingTargets uses a five-second read-only repeatable-read transaction, rejects a
// scan batch once the total exceeds ten thousand and pages only visible results with
// scope/result-bound cursors; commits before returning the evaluated page.
// ListArtifactBindingTargets использует пятисекундную read-only repeatable-read транзакцию,
// отклоняет пачку при суммарном чтении свыше десяти тысяч и выдаёт страницы только видимых
// результатов с cursor по scope/result; выполняет commit до возврата страницы.
func (repository *Repository) ListArtifactBindingTargets(ctx context.Context, principal value.Principal, artifactRef string, filter query.Filter) (entity.ArtifactBindingTargets, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	result := entity.ArtifactBindingTargets{ArtifactRef: artifactRef, Items: []entity.ArtifactBindingTarget{}}
	filter.Query = strings.TrimSpace(filter.Query)
	if !validOverlayHistoryRef(artifactRef) || !utf8.ValidString(filter.Query) || len([]rune(filter.Query)) > 200 || strings.ContainsRune(filter.Query, '\x00') || len(filter.Page.Token) > 2048 {
		return result, errs.ErrInvalid
	}
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return result, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return result, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	artifact, err := repository.readFileBindingArtifact(ctx, tx, current, artifactRef)
	if err != nil {
		return result, err
	}
	result.ArtifactVersion, result.ProjectRef = artifact.Version, artifact.ProjectRef
	all := []entity.ArtifactBindingTarget{}
	var after string
	for scanned := 0; ; {
		items, err := repository.readFileBindingAgents(ctx, tx, current, artifact, "", after, filter.Query)
		if err != nil {
			return result, err
		}
		scanned += len(items)
		if scanned > 10000 {
			return result, errs.ErrUnavailable
		}
		for _, item := range items {
			err := repository.fileBindingAgentVisible(ctx, tx, current, artifact, item)
			if errors.Is(err, errs.ErrNotFound) {
				continue
			}
			if err != nil {
				return result, err
			}
			all = append(all, projectFileBindingTarget(artifact, item))
		}
		if len(items) < 100 {
			break
		}
		after = items[len(items)-1].AgentRef
	}
	scopeDigest := jsonValueDigest([]any{current.organizationID, current.actorID, current.authorityProjectID, artifactRef, filter.Query})
	result.Digest = jsonValueDigest([]any{scopeDigest, artifact.Version, artifact.Lifecycle, artifact.Scan, all})
	start := 0
	if filter.Page.Token != "" {
		raw, err := base64.RawURLEncoding.DecodeString(filter.Page.Token)
		var cursor fileTargetCursor
		if err != nil || decodeStrict(raw, &cursor) != nil || cursor.Scope != scopeDigest || cursor.Ref == "" {
			return result, errs.ErrInvalid
		}
		if cursor.Digest != result.Digest {
			return result, errs.ErrVersionMismatch
		}
		found := false
		for index, item := range all {
			if item.AgentRef == cursor.Ref {
				start, found = index+1, true
				break
			}
		}
		if !found {
			return result, errs.ErrVersionMismatch
		}
	}
	end := min(start+int(boundedPage(filter.Page)), len(all))
	result.Items = all[start:end]
	result.Total = int64(len(all))
	if end < len(all) {
		raw, _ := json.Marshal(fileTargetCursor{Scope: scopeDigest, Digest: result.Digest, Ref: all[end-1].AgentRef})
		result.NextPageToken = base64.RawURLEncoding.EncodeToString(raw)
	}
	result.EvaluatedAt = time.Now().UTC()
	if err := tx.Commit(ctx); err != nil {
		return entity.ArtifactBindingTargets{}, errs.ErrUnavailable
	}
	return result, nil
}
