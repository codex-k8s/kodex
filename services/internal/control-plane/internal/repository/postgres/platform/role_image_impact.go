package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	revisionservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/revision"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

var (
	// queryRoleImageImpactTarget locks the latest admitted/promoted artifact mapped to the exact
	// published managed revision, scope and current policy/runtime contract pins.
	// queryRoleImageImpactTarget блокирует последний admitted/promoted artifact точной опубликованной
	// managed revision, области и текущих policy/runtime contract pins.
	//go:embed sql/role_image_impact__target.sql
	queryRoleImageImpactTarget string
	// queryRoleImageImpactConsumers lists up to1001 eligible current environment or agent-binding
	// consumers of the same scoped recipe and a different artifact, preserving exact owner and version
	// pins.
	// queryRoleImageImpactConsumers перечисляет до1001 допустимого current environment либо
	// agent-binding consumer того же scoped recipe и другого artifact с точными owner и version pins.
	//go:embed sql/role_image_impact__consumers.sql
	queryRoleImageImpactConsumers string
	// queryRoleImageImpactGet reads only the exact tenant/actor plan with owner snapshot revision2 and
	// joined configuration/revision/artifact ownership.
	// queryRoleImageImpactGet читает только точный tenant/actor plan с owner snapshot revision2 и
	// согласованным владением configuration/revision/artifact.
	//go:embed sql/role_image_impact__get.sql
	queryRoleImageImpactGet string
	// queryRoleImageImpactInsert stores immutable owner-snapshot revision2 and digest while leaving
	// creation/expiry timestamps to PostgreSQL.
	// queryRoleImageImpactInsert сохраняет неизменяемые owner-snapshot revision2 и digest, оставляя
	// creation/expiry timestamps PostgreSQL.
	//go:embed sql/role_image_impact__insert.sql
	queryRoleImageImpactInsert string
	// queryRoleImageImpactItems reads all plan item snapshots in reference order together with mutable
	// outcomes and result binding/version fields.
	// queryRoleImageImpactItems читает все snapshots элементов плана в порядке reference вместе с
	// изменяемыми outcomes и result binding/version fields.
	//go:embed sql/role_image_impact__items.sql
	queryRoleImageImpactItems string
	// queryRoleImageImpactInsertItem inserts a fixed item reference and immutable snapshot into the
	// exact plan without applying consumer effects.
	// queryRoleImageImpactInsertItem добавляет фиксированные reference и неизменяемый snapshot
	// элемента в точный план без применения эффектов consumer.
	//go:embed sql/role_image_impact__insert_item.sql
	queryRoleImageImpactInsertItem string
	// queryRoleImageImpactOutcome updates exactly a PENDING item and records the resulting
	// environment/binding pins, returning its reference for transition confirmation.
	// queryRoleImageImpactOutcome обновляет ровно PENDING item и записывает результирующие
	// environment/binding pins, возвращая reference для подтверждения перехода.
	//go:embed sql/role_image_impact__outcome.sql
	queryRoleImageImpactOutcome string
	// queryRoleImageImpactFinish moves an unexpired PREPARED plan to APPLIED only after every item has
	// left PENDING and advances plan version.
	// queryRoleImageImpactFinish переводит неистёкший PREPARED plan в APPLIED только после выхода всех
	// элементов из PENDING и увеличивает plan version.
	//go:embed sql/role_image_impact__finish.sql
	queryRoleImageImpactFinish string
	// queryRoleImageImpactSearchItems searches saved plan items through joined
	// environment/project/agent names with literal case-insensitive matching; caller eligibility
	// remains a separate authoritative check.
	// queryRoleImageImpactSearchItems ищет сохранённые элементы плана по связанным
	// environment/project/agent names буквальным совпадением без учёта регистра; eligibility caller
	// проверяется отдельно.
	//go:embed sql/role_image_impact__search_items.sql
	queryRoleImageImpactSearchItems string
)

// maximumRoleImageImpactItems caps prepared and decoded impact snapshots at one thousand items;
// SQL fetches one extra row to detect an excessive consumer set.
// maximumRoleImageImpactItems ограничивает подготовленные и декодированные impact snapshots
// тысячей элементов; SQL читает одну лишнюю строку для обнаружения чрезмерного набора consumers.
const maximumRoleImageImpactItems = 1000

// roleImageImpactDigest hashes actor plus normalized plan and reference-sorted copies of items,
// excluding mutable state/timestamps/outcomes/result fields while preserving all original consumer
// pins; marshal failure returns unavailable.
// roleImageImpactDigest хеширует actor, нормализованный план и отсортированные по reference копии
// элементов, исключая mutable state/timestamps/outcomes/result fields и сохраняя исходные consumer
// pins; ошибка Marshal даёт unavailable.
func roleImageImpactDigest(plan entity.RoleImageImpactPlan, actor string, items []entity.RoleImageImpactItem) (string, error) {
	plan.Version, plan.State, plan.Digest = 1, "PREPARED", ""
	plan.CreatedAt, plan.ExpiresAt = time.Time{}, time.Time{}
	canonical := append([]entity.RoleImageImpactItem{}, items...)
	for i := range canonical {
		canonical[i].Outcome = "PENDING"
		canonical[i].ResultEnvironmentVersionRef = ""
		canonical[i].ResultBindingRef = ""
		canonical[i].ResultBindingVersion = 0
	}
	sort.Slice(canonical, func(i, j int) bool { return canonical[i].Ref < canonical[j].Ref })
	raw, err := json.Marshal(struct {
		Plan  entity.RoleImageImpactPlan
		Actor string
		Items []entity.RoleImageImpactItem
	}{plan, actor, canonical})
	if err != nil {
		return "", errs.ErrUnavailable
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

// authorizeRoleImageImpact distinguishes prepared-plan rebind from target preparation, requiring
// authoritative set/revision access and exact configuration/revision/digest for rebind without
// granting permission from the plan locator.
// authorizeRoleImageImpact различает rebind подготовленного плана и подготовку target, требуя
// авторитетный доступ к set/revision и точные configuration/revision/digest для rebind без выдачи
// полномочий по locator плана.
func (r *Repository) authorizeRoleImageImpact(ctx context.Context, tx pgx.Tx, s scope, input command.Command) error {
	payload, ok := input.Payload.(command.ManagedConfigurationInput)
	if !ok {
		return errs.ErrInvalid
	}
	if input.Kind == command.RebindRoleImage {
		row, err := r.roleImageImpact(ctx, tx, s, payload.PlanRef)
		if err != nil {
			return err
		}
		if _, _, err := r.roleImageImpactAccess(ctx, tx, s, row); err != nil {
			return err
		}
		if row.public.ConfigurationRef != payload.ConfigurationRef || row.public.RevisionRef != payload.RevisionRef || row.public.Digest != payload.ImpactDigest {
			return errs.ErrConflict
		}
		return nil
	}
	set, err := r.resolveRoleImageImpactSet(ctx, tx, s, payload)
	if err != nil {
		return err
	}
	if set.ProjectRef == "" {
		if err := r.requireOrganizationRoleImageAccess(ctx, tx, s); err != nil {
			return err
		}
	}
	if err = r.requireManagedSetAccess(ctx, tx, s, set, "project.manage", "organization.manage"); err != nil {
		return errs.ErrNotFound
	}
	revision, err := r.lockManagedRevision(ctx, tx, s, set, payload.RevisionRef)
	if err != nil {
		return err
	}
	_, err = r.roleImageImpactTarget(ctx, tx, s, set, revision)
	return err
}

// roleImageImpactRow pairs the public plan snapshot with internal
// plan/configuration/revision/artifact identifiers used for exact owner joins and writes.
// roleImageImpactRow связывает публичный snapshot плана с внутренними идентификаторами
// plan/configuration/revision/artifact для точных owner joins и writes.
type roleImageImpactRow struct {
	public                                      entity.RoleImageImpactPlan
	id, configurationID, revisionID, artifactID string
}

// roleImageImpactTarget requires a published role-image revision, resolves its admitted/promoted
// target using current policy/contract pins and copies the managed set/revision OCC fields.
// roleImageImpactTarget требует опубликованную role-image revision, разрешает её admitted/promoted
// target по текущим policy/contract pins и копирует managed set/revision OCC fields.
func (r *Repository) roleImageImpactTarget(ctx context.Context, tx pgx.Tx, s scope, set managedSet, revision lockedManagedRevision) (roleImageImpactRow, error) {
	row := roleImageImpactRow{configurationID: set.id, revisionID: revision.RefID}
	if set.Kind != revisionservice.KindRoleImage || revision.PublishedAt == nil {
		return row, errs.ErrConflict
	}
	err := tx.QueryRow(ctx, queryRoleImageImpactTarget, pgx.StrictNamedArgs{
		"organization_id": s.organizationID, "configuration_id": set.id, "revision_ref": revision.Ref,
		"policy_revision": r.roleImages.PolicyRevision, "policy_digest": r.roleImages.PolicySHA256,
		"contract_revision": r.roleImages.RoleRuntimeContractRevision, "contract_digest": r.roleImages.RoleRuntimeContractSHA256,
	}).Scan(&row.artifactID, &row.public.RecipeRef, &row.public.RecipeGeneration, &row.public.BuildRef,
		&row.public.ArtifactRef, &row.public.ArtifactDigest, &row.public.AdmissionPolicyDigest)
	if errors.Is(err, pgx.ErrNoRows) {
		return row, errs.ErrConflict
	}
	if err != nil {
		return row, errs.ErrUnavailable
	}
	row.public.ConfigurationRef, row.public.ConfigurationVersion = set.Ref, set.Version
	row.public.RevisionRef, row.public.RevisionDigest = revision.Ref, revision.Digest
	return row, nil
}

// prepareRoleImageImpact rejects caller-supplied plan or selected consumers, snapshots up to one
// thousand authoritative consumers with server references and actor-bound digest, then stores
// plan/items in the caller transaction.
// prepareRoleImageImpact отклоняет заданные caller план либо выбранных consumers, снимает snapshot
// до тысячи авторитетных consumers с серверными references и actor-bound digest, затем сохраняет
// plan/items в транзакции caller.
func (r *Repository) prepareRoleImageImpact(ctx context.Context, tx pgx.Tx, s scope, set managedSet, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.ManagedConfigurationInput)
	if !ok || payload.PlanRef != "" || len(payload.Consumers) > 0 || len(payload.SelectedItemRefs) > 0 {
		return commandOutcome{}, errs.ErrInvalid
	}
	revision, err := r.lockManagedRevision(ctx, tx, s, set, payload.RevisionRef)
	if err != nil {
		return commandOutcome{}, err
	}
	row, err := r.roleImageImpactTarget(ctx, tx, s, set, revision)
	if err != nil {
		return commandOutcome{}, err
	}
	rows, err := tx.Query(ctx, queryRoleImageImpactConsumers, pgx.StrictNamedArgs{
		"organization_id": s.organizationID, "actor_id": s.actorID, "recipe_ref": row.public.RecipeRef,
		"artifact_ref": row.public.ArtifactRef, "authority_project": s.authorityProjectID, "evaluated_at": time.Now().UTC(),
	})
	if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	items := []entity.RoleImageImpactItem{}
	for rows.Next() {
		item := entity.RoleImageImpactItem{Outcome: "PENDING"}
		if rows.Scan(&item.EnvironmentRef, &item.EnvironmentVersion, &item.SourceVersionRef, &item.SourceVersionDigest,
			&item.Consumer.ProjectRef, &item.Consumer.AgentRef, &item.Consumer.AgentVersion, &item.Consumer.BindingRef, &item.Consumer.BindingVersion, &item.Consumer.ScopeKind, &item.Consumer.OrganizationRef) != nil {
			rows.Close()
			return commandOutcome{}, errs.ErrUnavailable
		}
		item.Consumer.VersionRef = item.SourceVersionRef
		item.Ref, err = newRef("riit")
		if err != nil {
			rows.Close()
			return commandOutcome{}, errs.ErrUnavailable
		}
		items = append(items, item)
	}
	rows.Close()
	if rows.Err() != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if len(items) > maximumRoleImageImpactItems {
		return commandOutcome{}, errs.ErrConflict
	}
	row.public.Ref, err = newRef("riip")
	if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	row.public.Version, row.public.State, row.public.Total = 1, "PREPARED", int64(len(items))
	row.public.Digest, err = roleImageImpactDigest(row.public, s.actorID, items)
	if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	err = tx.QueryRow(ctx, queryRoleImageImpactInsert, pgx.StrictNamedArgs{
		"ref": row.public.Ref, "organization_id": s.organizationID, "actor_id": s.actorID, "configuration_id": row.configurationID,
		"revision_id": row.revisionID, "artifact_id": row.artifactID, "snapshot": string(asJSON(row.public)), "digest": row.public.Digest,
	}).Scan(&row.id, &row.public.CreatedAt, &row.public.ExpiresAt)
	if err != nil {
		return commandOutcome{}, mapWriteError(err)
	}
	for _, item := range items {
		if _, err = tx.Exec(ctx, queryRoleImageImpactInsertItem, pgx.StrictNamedArgs{"plan_id": row.id, "ref": item.Ref, "snapshot": string(asJSON(item))}); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
	}
	outcome := managedOutcome(set, &revision.ManagedConfigurationRevision)
	outcome.result.RoleImageImpactPlan = &row.public
	return outcome, nil
}

// roleImageImpact reads the exact actor-owned plan, validates immutable snapshot
// identity/count/digest shape, overlays mutable row state and projects expiry without writing a
// transition.
// roleImageImpact читает точный actor-owned plan, проверяет identity/count/digest неизменяемого
// snapshot, накладывает mutable row state и отображает expiry без записи перехода.
func (r *Repository) roleImageImpact(ctx context.Context, tx pgx.Tx, s scope, ref string) (roleImageImpactRow, error) {
	var row roleImageImpactRow
	if !strings.HasPrefix(ref, "riip_") || len(ref) > 96 || len(ref) < 13 {
		return row, errs.ErrNotFound
	}
	var raw []byte
	var version int64
	var state, digest string
	var created, expires time.Time
	err := tx.QueryRow(ctx, queryRoleImageImpactGet, pgx.StrictNamedArgs{"organization_id": s.organizationID, "actor_id": s.actorID, "plan_ref": ref}).Scan(
		&row.id, &raw, &version, &state, &created, &expires, &digest, &row.configurationID, &row.revisionID, &row.artifactID)
	if errors.Is(err, pgx.ErrNoRows) {
		return row, errs.ErrNotFound
	}
	if err != nil || json.Unmarshal(raw, &row.public) != nil {
		return row, errs.ErrUnavailable
	}
	if row.public.Ref != ref || row.public.Digest != digest || row.public.Version != 1 || row.public.State != "PREPARED" || row.public.Total < 0 || row.public.Total > maximumRoleImageImpactItems {
		return row, errs.ErrUnavailable
	}
	row.public.Version, row.public.State, row.public.CreatedAt, row.public.ExpiresAt = version, state, created, expires
	if state == "PREPARED" && !time.Now().Before(expires) {
		row.public.State = "EXPIRED"
	}
	return row, nil
}

// roleImageImpactItems decodes ordered immutable item snapshots, validates reference/count and
// explicit owner scope, then overlays mutable outcomes and resulting binding/version pins.
// roleImageImpactItems декодирует упорядоченные неизменяемые snapshots элементов, проверяет
// reference/count и явную owner scope, затем накладывает mutable outcomes и результирующие
// binding/version pins.
func (r *Repository) roleImageImpactItems(ctx context.Context, tx pgx.Tx, id string) ([]entity.RoleImageImpactItem, error) {
	rows, err := tx.Query(ctx, queryRoleImageImpactItems, pgx.StrictNamedArgs{"plan_id": id})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer rows.Close()
	items := []entity.RoleImageImpactItem{}
	for rows.Next() {
		var raw []byte
		var ref, outcome, environment, binding string
		var version int64
		var item entity.RoleImageImpactItem
		if rows.Scan(&ref, &raw, &outcome, &environment, &binding, &version) != nil || json.Unmarshal(raw, &item) != nil || item.Ref != ref || len(items) >= maximumRoleImageImpactItems {
			return nil, errs.ErrUnavailable
		}
		if !validRuntimeOwnerSnapshot(item.Consumer.ScopeKind, item.Consumer.OrganizationRef, item.Consumer.ProjectRef) {
			return nil, errs.ErrNotFound
		}
		item.Outcome, item.ResultEnvironmentVersionRef, item.ResultBindingRef, item.ResultBindingVersion = outcome, environment, binding, version
		items = append(items, item)
	}
	if rows.Err() != nil {
		return nil, errs.ErrUnavailable
	}
	return items, nil
}

// roleImageImpactAccess re-resolves managed set/revision authority and the current admitted
// target, requiring exact stored internal IDs, recipe/build and digest pins before using the plan.
// roleImageImpactAccess повторно разрешает managed set/revision authority и текущую admitted
// target, требуя точные сохранённые internal IDs, recipe/build и digest pins до использования
// плана.
func (r *Repository) roleImageImpactAccess(ctx context.Context, tx pgx.Tx, s scope, row roleImageImpactRow) (managedSet, lockedManagedRevision, error) {
	set, err := r.resolveRoleImageImpactSet(ctx, tx, s, command.ManagedConfigurationInput{ConfigurationRef: row.public.ConfigurationRef})
	if err != nil {
		return set, lockedManagedRevision{}, err
	}
	if set.ProjectRef == "" {
		if err := r.requireOrganizationRoleImageAccess(ctx, tx, s); err != nil {
			return set, lockedManagedRevision{}, err
		}
	}
	if err = r.requireManagedSetAccess(ctx, tx, s, set, "project.manage", "organization.manage"); err != nil {
		return set, lockedManagedRevision{}, errs.ErrNotFound
	}
	revision, err := r.lockManagedRevision(ctx, tx, s, set, row.public.RevisionRef)
	if err != nil {
		return set, revision, err
	}
	target, err := r.roleImageImpactTarget(ctx, tx, s, set, revision)
	if err != nil {
		return set, revision, err
	}
	if row.configurationID != set.id || row.revisionID != revision.RefID || row.artifactID != target.artifactID ||
		row.public.RevisionDigest != target.public.RevisionDigest || row.public.ArtifactDigest != target.public.ArtifactDigest ||
		row.public.AdmissionPolicyDigest != target.public.AdmissionPolicyDigest || row.public.BuildRef != target.public.BuildRef ||
		row.public.RecipeRef != target.public.RecipeRef || row.public.RecipeGeneration != target.public.RecipeGeneration {
		return set, revision, errs.ErrConflict
	}
	return set, revision, nil
}

// GetRoleImageImpactPlan uses a five-second repeatable read to recheck authority and immutable
// commitment, filters current environment/consumer eligibility before search and cursor paging and
// commits before returning visible items.
// GetRoleImageImpactPlan использует пятисекундный repeatable read для повторной проверки authority
// и неизменяемого commitment, фильтрует текущую environment/consumer eligibility до поиска и
// cursor paging и выполняет commit до выдачи видимых элементов.
func (r *Repository) GetRoleImageImpactPlan(ctx context.Context, p value.Principal, ref, search string, page query.Page) (entity.RoleImageImpactPage, error) {
	return readImpactSnapshot(ctx, p, search, r.beginImpactSnapshot, func(ctx context.Context, tx pgx.Tx, s scope, search string) (entity.RoleImageImpactPage, error) {

		var result entity.RoleImageImpactPage

		row, err := r.roleImageImpact(ctx, tx, s, ref)
		if err != nil {
			return result, err
		}
		if _, _, err = r.roleImageImpactAccess(ctx, tx, s, row); err != nil {
			return result, err
		}
		items, err := r.roleImageImpactItems(ctx, tx, row.id)
		if err != nil {
			return result, err
		}
		if int64(len(items)) != row.public.Total {
			return result, errs.ErrUnavailable
		}
		digest, digestErr := roleImageImpactDigest(row.public, s.actorID, items)
		if digestErr != nil || digest != row.public.Digest {
			return result, errs.ErrUnavailable
		}
		filter := query.Filter{ResourceRef: ref, Category: strconv.FormatInt(row.public.Version, 10) + ":" + row.public.State, Query: search, Page: page}
		cursor, err := decodeCatalogCursor(s, "ROLE_IMAGE_IMPACT", filter)
		if err != nil {
			return result, err
		}
		limit := boundedPage(page)
		result.Plan = row.public
		matching, err := impactSearchRefs(ctx, tx, queryRoleImageImpactSearchItems, row.id, search)
		if err != nil {
			return result, err
		}
		for _, item := range items {
			if _, _, err = r.environmentImpactTarget(ctx, tx, s, item.EnvironmentRef, item.SourceVersionRef); err != nil {
				if errors.Is(err, errs.ErrNotFound) || errors.Is(err, errs.ErrForbidden) {
					continue
				}
				return result, err
			}
			if item.Consumer.AgentRef != "" {
				environment, ownerErr := r.getRuntimeEnvironmentTx(ctx, tx, s, item.EnvironmentRef)
				if ownerErr != nil {
					return result, ownerErr
				}
				if err = r.authorizeRuntimeEnvironmentConsumers(ctx, tx, s, environment, []entity.RuntimeEnvironmentConsumer{item.Consumer}); err != nil {
					if errors.Is(err, errs.ErrNotFound) || errors.Is(err, errs.ErrForbidden) {
						continue
					}
					return result, err
				}
			}
			if matching != nil && !matching[item.Ref] {
				continue
			}
			result.Total++
			if item.Ref > cursor && len(result.Items) <= int(limit) {
				result.Items = append(result.Items, item)
			}
		}
		if len(result.Items) > int(limit) {
			result.Items = result.Items[:limit]
			result.NextPageToken = encodeCatalogCursor(s, "ROLE_IMAGE_IMPACT", filter, result.Items[len(result.Items)-1].Ref)
		}
		if tx.Commit(ctx) != nil {
			return result, errs.ErrUnavailable
		}
		return result, nil
	})
}
