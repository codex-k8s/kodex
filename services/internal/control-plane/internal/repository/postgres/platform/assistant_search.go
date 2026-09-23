package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

const maximumAssistantSearchCandidates = 500
const maximumAssistantSearchResults = 10

//go:embed sql/assistant_search_resolve_lease.sql
var queryAssistantSearchResolveLease string

func (repository *Repository) SearchAssistantResources(ctx context.Context, principal value.Principal, leaseRef, fence string, generation int64, search string) ([]entity.SearchResult, bool, error) {
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, false, err
	}
	ctx, stop := context.WithTimeout(ctx, 5*time.Second)
	defer stop()
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return nil, false, errs.ErrUnavailable
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()
	fenceDigest := sha256.Sum256([]byte(fence))
	err = tx.QueryRow(ctx, queryAssistantSearchResolveLease, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "lease_ref": leaseRef,
		"fence_digest": hex.EncodeToString(fenceDigest[:]), "generation": generation,
	}).Scan(&current.actorRef, &current.actorID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, errs.ErrNotFound
	}
	if err != nil {
		return nil, false, errs.ErrUnavailable
	}
	subject, err := repository.resolveAccessSubject(ctx, tx, current.organizationID, current.actorRef)
	if err != nil {
		return nil, false, err
	}
	bindings, err := repository.loadAccessBindings(ctx, tx, current.organizationID, subject)
	if err != nil {
		return nil, false, err
	}
	rows, err := tx.Query(ctx, queryQueriesSearchSelectEligibleResources, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "query": search, "project_ref": "",
	})
	if err != nil {
		return nil, false, errs.ErrUnavailable
	}
	candidates := make([]entity.SearchResult, 0, maximumAssistantSearchCandidates)
	for rows.Next() {
		if len(candidates) == maximumAssistantSearchCandidates {
			rows.Close()
			return nil, false, errs.ErrInvalid
		}
		var item entity.SearchResult
		var relevance int
		var orderTime time.Time
		if err := rows.Scan(&item.Kind, &item.Ref, &item.ProjectRef, &item.Title, &item.Subtitle,
			&item.State, &item.UpdatedAt, &relevance, &orderTime); err != nil {
			rows.Close()
			return nil, false, errs.ErrUnavailable
		}
		candidates = append(candidates, item)
	}
	if rows.Err() != nil {
		rows.Close()
		return nil, false, errs.ErrUnavailable
	}
	rows.Close()
	result := make([]entity.SearchResult, 0, maximumAssistantSearchResults)
	truncated := false
	for _, item := range candidates {
		if item.Kind != "PROJECT" && item.Kind != "AGENT" && item.Kind != "WORKFLOW" && item.Kind != "RUN" {
			continue
		}
		visible, err := repository.resourceVisible(ctx, tx, current, subject.AccessSubject, bindings,
			item.Kind, item.Ref, item.ProjectRef, time.Now().UTC())
		if err != nil {
			return nil, false, err
		}
		if !visible {
			continue
		}
		if len(result) == maximumAssistantSearchResults {
			truncated = true
			continue
		}
		item.Title = strings.TrimSpace(item.Title)
		item.Subtitle = strings.TrimSpace(item.Subtitle)
		if len([]rune(item.Subtitle)) > 160 {
			item.Subtitle = string([]rune(item.Subtitle)[:160])
		}
		result = append(result, item)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, false, errs.ErrUnavailable
	}
	return result, truncated, nil
}
