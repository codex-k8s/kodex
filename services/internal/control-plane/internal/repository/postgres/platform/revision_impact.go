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
	"unicode/utf8"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

var (
	// queryRevisionImpactInsert inserts an actor/tenant-owned immutable plan snapshot and digest under
	// owner snapshot revision2, returning server creation and expiry timestamps.
	// queryRevisionImpactInsert вставляет неизменяемые plan snapshot и digest владельца actor/tenant с
	// owner snapshot revision2, возвращая server creation и expiry timestamps.
	//go:embed sql/revision_impact__insert.sql
	queryRevisionImpactInsert string
	// queryRevisionImpactGet locks the exact tenant/actor/ref plan only at owner snapshot revision2
	// and returns immutable snapshot plus mutable terminal/version metadata.
	// queryRevisionImpactGet блокирует план точных tenant/actor/ref только с owner snapshot revision2
	// и возвращает immutable snapshot с mutable terminal/version metadata.
	//go:embed sql/revision_impact__get.sql
	queryRevisionImpactGet string
	// queryRevisionImpactInsertItem inserts an exact server item reference and immutable snapshot into
	// its plan without applying a consumer effect.
	// queryRevisionImpactInsertItem вставляет точные server item reference и immutable snapshot в план
	// без применения consumer effect.
	//go:embed sql/revision_impact__insert_item.sql
	queryRevisionImpactInsertItem string
	// queryRevisionImpactItems reads saved item snapshots in reference order with their separate
	// outcomes and resulting revision/binding/consumer version pins.
	// queryRevisionImpactItems читает сохранённые item snapshots в порядке references с отдельными
	// outcomes и result revision/binding/consumer version pins.
	//go:embed sql/revision_impact__items.sql
	queryRevisionImpactItems string
	// queryRevisionImpactOutcome records a completed item outcome and result pins only while that
	// exact plan/item remains PENDING.
	// queryRevisionImpactOutcome записывает завершённый item outcome и result pins только пока точный
	// plan/item остаётся PENDING.
	//go:embed sql/revision_impact__outcome.sql
	queryRevisionImpactOutcome string
	// queryRevisionImpactFinish marks an unexpired PREPARED plan APPLIED with its published revision
	// only after every item leaves PENDING, advancing the plan version.
	// queryRevisionImpactFinish помечает неистёкший PREPARED plan как APPLIED с published revision
	// только после выхода каждого item из PENDING, увеличивая plan version.
	//go:embed sql/revision_impact__finish.sql
	queryRevisionImpactFinish string
	// queryRevisionImpactEnvironmentConsumers reads environment consumers and their exact
	// owner/binding/version snapshots for impact preparation, without changing runtime bindings.
	// queryRevisionImpactEnvironmentConsumers читает environment consumers и точные
	// owner/binding/version snapshots для impact preparation без изменения runtime bindings.
	//go:embed sql/revision_impact__environment_consumers.sql
	queryRevisionImpactEnvironmentConsumers string
	// queryRevisionImpactSearchItems finds saved plan item references by literal case-insensitive
	// consumer metadata search; current owner eligibility is checked separately before counting and
	// paging.
	// queryRevisionImpactSearchItems ищет сохранённые plan item references буквальным
	// регистронезависимым поиском по consumer metadata; текущая owner eligibility отдельно проверяется
	// до count и paging.
	//go:embed sql/revision_impact__search_items.sql
	queryRevisionImpactSearchItems string
)

// maximumRevisionImpactItems caps immutable revision-impact plans at1000 items for snapshot
// materialization and verified readback.
// maximumRevisionImpactItems ограничивает immutable revision-impact plans тысячей items для
// snapshot materialization и проверяемого readback.
const maximumRevisionImpactItems = 1000

// impactSearchRefs returns nil for no search, otherwise reads the exact matching-reference set
// from caller-selected SQL with plan/query arguments, preserving Unavailable for
// query/scan/iteration failure and closing rows.
// impactSearchRefs возвращает nil без поиска, иначе читает точный набор совпавших references из
// выбранного caller SQL с plan/query arguments, сохраняя Unavailable при query/scan/iteration
// failure и закрывая rows.
func impactSearchRefs(ctx context.Context, tx pgx.Tx, statement, planID, search string) (map[string]bool, error) {
	if search == "" {
		return nil, nil
	}
	rows, err := tx.Query(ctx, statement, pgx.StrictNamedArgs{"plan_id": planID, "query": search})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer rows.Close()
	refs := map[string]bool{}
	for rows.Next() {
		var ref string
		if rows.Scan(&ref) != nil {
			return nil, errs.ErrUnavailable
		}
		refs[ref] = true
	}
	if rows.Err() != nil {
		return nil, errs.ErrUnavailable
	}
	return refs, nil
}

// revisionImpactRow pairs the internal locked plan ID with its validated public snapshot and
// current lifecycle projection; it is not an authority capability.
// revisionImpactRow связывает внутренний locked plan ID с проверенным публичным snapshot и текущим
// lifecycle projection; authority capability не является.
type revisionImpactRow struct {
	id   string
	plan entity.RevisionImpactPlan
}

// revisionImpactDigest normalizes mutable plan timestamps/state/version/publication and item
// outcomes/results, sorts a copied item slice by reference and hashes the exact typed
// Plan/Actor/Items JSON. It preserves immutable owner/input pins, leaves caller values unchanged
// and returns Unavailable on marshal failure; this is encoding/json commitment, not JCS.
// revisionImpactDigest нормализует mutable plan timestamps/state/version/publication и item
// outcomes/results, сортирует копию item slice по reference и хеширует точный typed
// Plan/Actor/Items JSON. Сохраняет immutable owner/input pins, не изменяет caller values и даёт
// Unavailable при marshal failure; это encoding/json commitment, а не JCS.
func revisionImpactDigest(plan entity.RevisionImpactPlan, actor string, items []entity.RevisionImpactItem) (string, error) {
	plan.Version, plan.State, plan.Digest, plan.PublishedRevisionRef = 1, "PREPARED", "", ""
	plan.CreatedAt, plan.ExpiresAt = time.Time{}, time.Time{}
	canonical := append([]entity.RevisionImpactItem{}, items...)
	for i := range canonical {
		canonical[i].Outcome = "PENDING"
		canonical[i].ResultRevisionRef, canonical[i].ResultBindingRef = "", ""
		canonical[i].ResultBindingVersion, canonical[i].ResultConsumerVersion = 0, 0
	}
	sort.Slice(canonical, func(i, j int) bool { return canonical[i].Ref < canonical[j].Ref })
	raw, err := json.Marshal(struct {
		Plan  entity.RevisionImpactPlan
		Actor string
		Items []entity.RevisionImpactItem
	}{plan, actor, canonical})
	if err != nil {
		return "", errs.ErrUnavailable
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

// revisionImpact checks rvip prefix and reference length, locks exact tenant/actor ownership and checks
// decoded ref/kind/digest/total against saved metadata. It overlays mutable DB
// lifecycle/times/publication, projects elapsed PREPARED as EXPIRED without persisting expiry and
// leaves permission checks to the caller.
// revisionImpact проверяет rvip prefix и длину reference, блокирует точное tenant/actor ownership и
// сверяет decoded ref/kind/digest/total с сохранёнными metadata. Накладывает mutable DB
// lifecycle/times/publication, показывает истёкший PREPARED как EXPIRED без сохранения expiry и
// оставляет permission checks вызывающему методу.
func (r *Repository) revisionImpact(ctx context.Context, tx pgx.Tx, s scope, ref string) (revisionImpactRow, error) {
	var row revisionImpactRow
	if !strings.HasPrefix(ref, "rvip_") || len(ref) < 13 || len(ref) > 94 {
		return row, errs.ErrNotFound
	}
	var raw []byte
	var kind, digest, state, published string
	var version int64
	var created, expires time.Time
	err := tx.QueryRow(ctx, queryRevisionImpactGet, pgx.StrictNamedArgs{"organization_id": s.organizationID, "actor_id": s.actorID, "ref": ref}).Scan(&row.id, &kind, &raw, &digest, &version, &state, &created, &expires, &published)
	if errors.Is(err, pgx.ErrNoRows) {
		return row, errs.ErrNotFound
	}
	if err != nil || json.Unmarshal(raw, &row.plan) != nil {
		return row, errs.ErrUnavailable
	}
	if row.plan.Ref != ref || row.plan.Kind != kind || row.plan.Digest != digest || row.plan.Total < 0 || row.plan.Total > maximumRevisionImpactItems {
		return row, errs.ErrUnavailable
	}
	row.plan.Version, row.plan.State, row.plan.CreatedAt, row.plan.ExpiresAt, row.plan.PublishedRevisionRef = version, state, created, expires, published
	if state == "PREPARED" && !time.Now().Before(expires) {
		row.plan.State = "EXPIRED"
	}
	return row, nil
}

// revisionImpactItems reads every immutable item plus mutable outcomes/results, requires valid
// owner snapshots, exact bounded total and recomputed actor-bound commitment before returning. It
// rejects damaged owner metadata or digest and does not filter by current per-item access.
// revisionImpactItems читает каждый immutable item с mutable outcomes/results, требует допустимые
// owner snapshots, точный ограниченный total и пересчитанный actor-bound commitment до ответа.
// Отклоняет повреждённые owner metadata либо digest и не фильтрует по текущему per-item access.
func (r *Repository) revisionImpactItems(ctx context.Context, tx pgx.Tx, row revisionImpactRow, actor string) ([]entity.RevisionImpactItem, error) {
	rows, err := tx.Query(ctx, queryRevisionImpactItems, row.id)
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer rows.Close()
	items := []entity.RevisionImpactItem{}
	for rows.Next() {
		var item entity.RevisionImpactItem
		var raw []byte
		var outcome, revision, binding string
		var bindingVersion, consumerVersion int64
		if rows.Scan(&raw, &outcome, &revision, &binding, &bindingVersion, &consumerVersion) != nil || json.Unmarshal(raw, &item) != nil {
			return nil, errs.ErrUnavailable
		}
		if !validRuntimeOwnerSnapshot(item.ScopeKind, item.OrganizationRef, item.ProjectRef) {
			return nil, errs.ErrNotFound
		}
		item.Outcome, item.ResultRevisionRef, item.ResultBindingRef, item.ResultBindingVersion, item.ResultConsumerVersion = outcome, revision, binding, bindingVersion, consumerVersion
		items = append(items, item)
		if len(items) > maximumRevisionImpactItems {
			return nil, errs.ErrUnavailable
		}
	}
	if rows.Err() != nil || int64(len(items)) != row.plan.Total {
		return nil, errs.ErrUnavailable
	}
	digest, err := revisionImpactDigest(row.plan, actor, items)
	if err != nil || digest != row.plan.Digest {
		return nil, errs.ErrUnavailable
	}
	return items, nil
}

// revisionImpactAccess dispatches current source authority by the closed PROMPT_TEMPLATE,
// AGENT_INSTRUCTIONS and RUNTIME_ENVIRONMENT plan kinds, including exact environment draft/source
// linkage; unknown kinds return NotFound.
// revisionImpactAccess выбирает текущую source authority по закрытым plan kinds PROMPT_TEMPLATE,
// AGENT_INSTRUCTIONS и RUNTIME_ENVIRONMENT, включая точную environment draft/source linkage;
// неизвестные kinds дают NotFound.
func (r *Repository) revisionImpactAccess(ctx context.Context, tx pgx.Tx, s scope, row revisionImpactRow) error {
	switch row.plan.Kind {
	case "PROMPT_TEMPLATE":
		permission, target, err := r.commandAccessTarget(ctx, tx, s, command.Command{Kind: command.PreparePromptTemplateImpact, Payload: command.ManagedConfigurationInput{ConfigurationRef: row.plan.SourceRef}})
		if err != nil {
			return err
		}
		return r.requireAccess(ctx, tx, s, permission, target)
	case "AGENT_INSTRUCTIONS":
		_, _, err := r.resolveCommandTarget(ctx, tx, s, "agent.manage", "AGENT", row.plan.SourceRef, "")
		if err != nil {
			return err
		}
		return r.requireAccess(ctx, tx, s, "agent.manage", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "AGENT", ResourceRef: row.plan.SourceRef})
	case "RUNTIME_ENVIRONMENT":
		draft, err := scanEnvironmentDraft(tx.QueryRow(ctx, queryEnvironmentDraftGet, s.organizationID, row.plan.DraftRef))
		if err != nil {
			return err
		}
		if draft.EnvironmentRef != row.plan.SourceRef {
			return errs.ErrUnavailable
		}
		return r.requireRuntimeEnvironmentOwnerAccess(ctx, tx, s, draft.ScopeKind, draft.ProjectRef)
	default:
		return errs.ErrNotFound
	}
}

// GetRevisionImpactPlan validates search and uses a five-second RepeatableRead transaction to
// verify plan ownership/source access and the full immutable item commitment. It binds cursor to
// plan version/state/query and applies current item access before count/page, skipping only
// NotFound/Forbidden, then commits; other failures remain visible.
// GetRevisionImpactPlan проверяет search и в RepeatableRead транзакции на пять секунд подтверждает
// plan ownership/source access и полный immutable item commitment. Связывает cursor с plan
// version/state/query и применяет текущий item access до count/page, пропуская только
// NotFound/Forbidden, затем делает commit; прочие ошибки передаются.
func (r *Repository) GetRevisionImpactPlan(ctx context.Context, p value.Principal, ref, search string, page query.Page) (entity.RevisionImpactPage, error) {
	var result entity.RevisionImpactPage
	search = strings.TrimSpace(search)
	if !utf8.ValidString(search) || utf8.RuneCountInString(search) > 200 || strings.ContainsRune(search, 0) {
		return result, errs.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	s, err := r.resolveScope(ctx, p)
	if err != nil {
		return result, err
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return result, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	row, err := r.revisionImpact(ctx, tx, s, ref)
	if err != nil {
		return result, err
	}
	if err = r.revisionImpactAccess(ctx, tx, s, row); err != nil {
		return result, err
	}
	items, err := r.revisionImpactItems(ctx, tx, row, s.actorID)
	if err != nil {
		return result, err
	}
	filter := query.Filter{ResourceRef: ref, Category: strconv.FormatInt(row.plan.Version, 10) + ":" + row.plan.State, Query: search, Page: page}
	cursor, err := decodeCatalogCursor(s, "REVISION_IMPACT", filter)
	if err != nil {
		return result, err
	}
	result.Plan, result.Items = row.plan, []entity.RevisionImpactItem{}
	matching, err := impactSearchRefs(ctx, tx, queryRevisionImpactSearchItems, row.id, search)
	if err != nil {
		return result, err
	}
	limit := int(boundedPage(page))
	for _, item := range items {
		if err = r.revisionImpactItemAccess(ctx, tx, s, item); err != nil {
			if errors.Is(err, errs.ErrNotFound) || errors.Is(err, errs.ErrForbidden) {
				continue
			}
			return result, err
		}
		if matching != nil && !matching[item.Ref] {
			continue
		}
		result.Total++
		if item.Ref > cursor && len(result.Items) <= limit {
			result.Items = append(result.Items, item)
		}
	}
	if len(result.Items) > limit {
		result.Items = result.Items[:limit]
		result.NextPageToken = encodeCatalogCursor(s, "REVISION_IMPACT", filter, result.Items[limit-1].Ref)
	}
	if err = tx.Commit(ctx); err != nil {
		return entity.RevisionImpactPage{}, errs.ErrUnavailable
	}
	return result, nil
}

// revisionImpactItemAccess resolves the authoritative consumer owner and matches its immutable
// scope tuple before exact agent-runtime or workflow/schedule manage authority; unsupported
// consumer kinds return Unavailable rather than becoming visible.
// revisionImpactItemAccess разрешает авторитетного consumer owner и сверяет immutable scope tuple
// до точных agent-runtime либо workflow/schedule manage authority; неподдержанные consumer kinds
// дают Unavailable вместо выдачи видимости.
func (r *Repository) revisionImpactItemAccess(ctx context.Context, tx pgx.Tx, s scope, item entity.RevisionImpactItem) error {
	owner, err := r.impactConsumerOwner(ctx, tx, s, item.ConsumerKind, item.ConsumerRef)
	if err != nil {
		return err
	}
	if err := matchRuntimeOwnerSnapshot(owner.ScopeKind, owner.OrganizationRef, owner.ProjectRef, item.ScopeKind, item.OrganizationRef, item.ProjectRef); err != nil {
		return err
	}
	if item.ConsumerKind == "AGENT" || item.ConsumerKind == "AGENT_CONTINUATION" {
		permission, target, err := r.resolveRuntimeConfigurationTarget(ctx, tx, s, "agent.manage", item.ConsumerRef)
		if err != nil {
			return err
		}
		return r.requireAccess(ctx, tx, s, permission, target)
	}
	if item.ConsumerKind != "WORKFLOW" && item.ConsumerKind != "SCHEDULE" {
		return errs.ErrUnavailable
	}
	permission, target, err := r.resolveCommandTarget(ctx, tx, s, strings.ToLower(item.ConsumerKind)+".manage", item.ConsumerKind, item.ConsumerRef, item.ProjectRef)
	if err != nil {
		return err
	}
	return r.requireAccess(ctx, tx, s, permission, target)
}
