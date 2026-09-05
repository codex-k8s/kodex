package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

var (
	//go:embed sql/secret_draft_impact_get.sql
	querySecretDraftImpactGet string
	//go:embed sql/secret_draft_impact_insert.sql
	querySecretDraftImpactInsert string
	//go:embed sql/secret_draft_impact_items_insert.sql
	querySecretDraftImpactItemsInsert string
	//go:embed sql/secret_draft_impact_items.sql
	querySecretDraftImpactItems string
	//go:embed sql/secret_draft_impact_bind.sql
	querySecretDraftImpactBind string
	//go:embed sql/secret_draft_impact_select.sql
	querySecretDraftImpactSelect string
	//go:embed sql/secret_draft_impact_outcome.sql
	querySecretDraftImpactOutcome string
	//go:embed sql/secret_draft_impact_finish.sql
	querySecretDraftImpactFinish string
	//go:embed sql/secret_draft_impact_cancel.sql
	querySecretDraftImpactCancel string
)

const maximumSecretDraftImpactItems = 1000

type secretDraftImpactRow struct {
	public                  entity.RuntimeSecretDraftImpactPlan
	id, intent, operationID string
}

func (r *Repository) secretDraftImpact(ctx context.Context, tx pgx.Tx, s scope, ref, key, operationID string) (secretDraftImpactRow, error) {
	var row secretDraftImpactRow
	err := tx.QueryRow(ctx, querySecretDraftImpactGet, pgx.StrictNamedArgs{"organization_id": s.organizationID, "actor_id": s.actorID, "plan_ref": ref, "idempotency_key": key, "operation_id": operationID}).Scan(&row.id, &row.public.Ref, &row.public.DraftRef, &row.public.DraftVersion, &row.public.SecretRef, &row.public.SecretVersion, &row.public.SourceRevision, &row.public.Digest, &row.public.State, &row.public.ExpiresAt, &row.intent, &row.operationID, &row.public.Total)
	if errors.Is(err, pgx.ErrNoRows) {
		return row, errs.ErrNotFound
	}
	if err != nil {
		return row, errs.ErrUnavailable
	}
	return row, nil
}

func (r *Repository) secretDraftImpactItems(ctx context.Context, tx pgx.Tx, id string) ([]entity.RuntimeSecretDraftImpactItem, error) {
	rows, err := tx.Query(ctx, querySecretDraftImpactItems, pgx.StrictNamedArgs{"plan_id": id})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer rows.Close()
	var result []entity.RuntimeSecretDraftImpactItem
	for rows.Next() {
		var item entity.RuntimeSecretDraftImpactItem
		var raw []byte
		if rows.Scan(&item.Ref, &raw, &item.Outcome, &item.ResultEnvironmentVersionRef, &item.ResultBindingRef, &item.ResultBindingVersion) != nil || json.Unmarshal(raw, &item.Consumer) != nil || len(result) >= maximumSecretDraftImpactItems {
			return nil, errs.ErrUnavailable
		}
		result = append(result, item)
	}
	if rows.Err() != nil {
		return nil, errs.ErrUnavailable
	}
	return result, nil
}

func (r *Repository) PrepareRuntimeSecretDraftImpact(ctx context.Context, p value.Principal, ref string, mutation value.Mutation) (entity.RuntimeSecretDraftImpactPlan, error) {
	var empty entity.RuntimeSecretDraftImpactPlan
	s, err := r.resolveScope(ctx, p)
	if err != nil {
		return empty, err
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return empty, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	d, err := r.lockSecretDraft(ctx, tx, s.organizationID, ref)
	if err != nil {
		return empty, err
	}
	permission := "secret.rotate"
	if d.secretState == "PROVISIONING" {
		permission = "secret.create"
	}
	if err = r.secretDraftAccess(ctx, tx, s, d, permission); err != nil {
		return empty, err
	}
	existing, err := r.secretDraftImpact(ctx, tx, s, "", mutation.IdempotencyKey, "")
	if err == nil {
		if existing.public.DraftRef != ref || existing.intent != mutation.IntentDigest {
			return empty, errs.ErrConflict
		}
		if tx.Commit(ctx) != nil {
			return empty, errs.ErrConflict
		}
		return existing.public, nil
	}
	if !errors.Is(err, errs.ErrNotFound) {
		return empty, err
	}
	if mutation.ExpectedVersion == nil || *mutation.ExpectedVersion != d.public.Version {
		return empty, errs.ErrVersionMismatch
	}
	if d.public.State != "VALID" || !time.Now().Before(d.public.ExpiresAt) {
		return empty, errs.ErrConflict
	}
	secret, err := r.lockRuntimeSecret(ctx, tx, s.organizationID, d.public.SecretRef)
	if err != nil {
		return empty, err
	}
	rows, err := tx.Query(ctx, querySecretImpactConsumers, pgx.StrictNamedArgs{"organization_id": s.organizationID, "actor_id": s.actorID, "query": "", "authority_project": s.authorityProjectID, "secret_ref": secret.ref, "target_revision": int64(0), "evaluated_at": time.Now().UTC(), "cursor_ref": "", "page_size": maximumSecretDraftImpactItems + 1})
	if err != nil {
		return empty, errs.ErrUnavailable
	}
	var items []entity.RuntimeSecretDraftImpactItem
	var total int64
	for rows.Next() {
		var key string
		var item entity.RuntimeSecretDraftImpactItem
		c := &item.Consumer
		if rows.Scan(&key, &c.EnvironmentRef, &c.EnvironmentVersion, &c.EnvironmentVersionRef, &c.SecretRevisions, &c.Consumer.AgentRef, &c.Consumer.AgentVersion, &c.Consumer.BindingRef, &c.Consumer.BindingVersion, &c.Consumer.ProjectRef, &total) != nil {
			rows.Close()
			return empty, errs.ErrUnavailable
		}
		if key != "" {
			item.Ref, err = newRef("sdit")
			if err != nil {
				rows.Close()
				return empty, errs.ErrUnavailable
			}
			c.Consumer.VersionRef = c.EnvironmentVersionRef
			item.Outcome = "PENDING"
			items = append(items, item)
		}
	}
	rows.Close()
	if rows.Err() != nil {
		return empty, errs.ErrUnavailable
	}
	if total > maximumSecretDraftImpactItems {
		return empty, errs.ErrConflict
	}
	plan := entity.RuntimeSecretDraftImpactPlan{DraftRef: ref, DraftVersion: d.public.Version, SecretRef: secret.ref, SecretVersion: secret.version, SourceRevision: secret.currentRevision, Total: total, State: "PREPARED"}
	plan.Ref, err = newRef("sdip")
	if err != nil {
		return empty, errs.ErrUnavailable
	}
	raw, err := json.Marshal(struct {
		Plan       entity.RuntimeSecretDraftImpactPlan
		Actor      string
		Credential uint64
		Items      []entity.RuntimeSecretDraftImpactItem
	}{plan, s.actorID, p.CredentialRevision, items})
	if err != nil {
		return empty, errs.ErrUnavailable
	}
	digest := sha256.Sum256(raw)
	plan.Digest = hex.EncodeToString(digest[:])
	var id string
	err = tx.QueryRow(ctx, querySecretDraftImpactInsert, pgx.StrictNamedArgs{"ref": plan.Ref, "organization_id": s.organizationID, "actor_id": s.actorID, "draft_id": d.id, "draft_version": plan.DraftVersion, "secret_version": plan.SecretVersion, "source_revision": plan.SourceRevision, "credential_revision": p.CredentialRevision, "digest": plan.Digest, "idempotency_key": mutation.IdempotencyKey, "intent_digest": mutation.IntentDigest}).Scan(&id, &plan.ExpiresAt)
	if err != nil {
		return empty, mapWriteError(err)
	}
	if items == nil {
		items = []entity.RuntimeSecretDraftImpactItem{}
	}
	if _, err = tx.Exec(ctx, querySecretDraftImpactItemsInsert, pgx.StrictNamedArgs{"plan_id": id, "items": string(asJSON(items))}); err != nil {
		return empty, errs.ErrUnavailable
	}
	if err = r.auditSecretDraft(ctx, tx, s, d, secretDraftOperationRow{kind: "IMPACT", actorID: s.actorID, correlation: p.CorrelationRef}, "SUCCEEDED"); err != nil {
		return empty, err
	}
	if tx.Commit(ctx) != nil {
		return empty, errs.ErrConflict
	}
	return plan, nil
}

func (r *Repository) GetRuntimeSecretDraftImpact(ctx context.Context, p value.Principal, ref, search string, page query.Page) (entity.RuntimeSecretDraftImpactPage, error) {
	var result entity.RuntimeSecretDraftImpactPage
	s, err := r.resolveScope(ctx, p)
	if err != nil {
		return result, err
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return result, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	plan, err := r.secretDraftImpact(ctx, tx, s, ref, "", "")
	if err != nil {
		return result, err
	}
	d, err := scanSecretDraft(tx.QueryRow(ctx, querySecretDraftGet, pgx.StrictNamedArgs{"organization_id": s.organizationID, "draft_ref": plan.public.DraftRef}))
	if err != nil {
		return result, err
	}
	permission := "secret.rotate"
	if d.secretState == "PROVISIONING" {
		permission = "secret.create"
	}
	if err = r.secretDraftAccess(ctx, tx, s, d, permission); err != nil {
		return result, err
	}
	filter := query.Filter{ResourceRef: ref, Query: strings.TrimSpace(search), Category: plan.public.Digest + "/" + plan.public.State, Page: page}
	cursor, err := decodeCatalogCursor(s, "SECRET_DRAFT_IMPACT", filter)
	if err != nil {
		return result, err
	}
	items, err := r.secretDraftImpactItems(ctx, tx, plan.id)
	if err != nil {
		return result, err
	}
	result.Plan = plan.public
	limit := boundedPage(page)
	for _, item := range items {
		c := item.Consumer
		if r.requireAccess(ctx, tx, s, "project.manage", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "PROJECT", ResourceRef: c.Consumer.ProjectRef}) != nil {
			continue
		}
		if c.Consumer.AgentRef != "" && r.requireAccess(ctx, tx, s, "agent.manage", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "AGENT", ResourceRef: c.Consumer.AgentRef}) != nil {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(c.EnvironmentRef+" "+c.Consumer.AgentRef+" "+c.Consumer.ProjectRef), strings.ToLower(strings.TrimSpace(search))) {
			continue
		}
		result.Total++
		if item.Ref > cursor && len(result.Items) <= int(limit) {
			result.Items = append(result.Items, item)
		}
	}
	if len(result.Items) > int(limit) {
		result.Items = result.Items[:limit]
		result.NextPageToken = encodeCatalogCursor(s, "SECRET_DRAFT_IMPACT", filter, result.Items[limit-1].Ref)
	}
	if tx.Commit(ctx) != nil {
		return result, errs.ErrUnavailable
	}
	return result, nil
}
