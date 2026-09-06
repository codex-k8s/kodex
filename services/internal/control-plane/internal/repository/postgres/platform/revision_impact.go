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
	//go:embed sql/revision_impact__insert.sql
	queryRevisionImpactInsert string
	//go:embed sql/revision_impact__get.sql
	queryRevisionImpactGet string
	//go:embed sql/revision_impact__insert_item.sql
	queryRevisionImpactInsertItem string
	//go:embed sql/revision_impact__items.sql
	queryRevisionImpactItems string
	//go:embed sql/revision_impact__outcome.sql
	queryRevisionImpactOutcome string
	//go:embed sql/revision_impact__finish.sql
	queryRevisionImpactFinish string
	//go:embed sql/revision_impact__environment_consumers.sql
	queryRevisionImpactEnvironmentConsumers string
	//go:embed sql/revision_impact__search_items.sql
	queryRevisionImpactSearchItems string
)

const maximumRevisionImpactItems = 1000

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

type revisionImpactRow struct {
	id   string
	plan entity.RevisionImpactPlan
}

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
		target, err := r.resolveAccessTarget(ctx, tx, s.organizationID, entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "PROJECT", ResourceRef: draft.ProjectRef})
		if err != nil {
			return err
		}
		if s.authorityProjectID != "" && s.authorityProjectID != target.projectID {
			return errs.ErrNotFound
		}
		return r.requireAccess(ctx, tx, s, "project.manage", target)
	default:
		return errs.ErrNotFound
	}
}

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

func (r *Repository) revisionImpactItemAccess(ctx context.Context, tx pgx.Tx, s scope, item entity.RevisionImpactItem) error {
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
