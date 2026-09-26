package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

const maximumAssistantSearchCandidates = 500
const maximumAssistantSearchResults = 10
const maximumAssistantIntegrationDefinitions = 10

//go:embed sql/assistant_search_resolve_lease.sql
var queryAssistantSearchResolveLease string

//go:embed sql/assistant_search_extra.sql
var queryAssistantSearchExtra string

//go:embed sql/assistant_integration_definitions.sql
var queryAssistantIntegrationDefinitions string

func (repository *Repository) ListAssistantIntegrationDefinitions(ctx context.Context, principal value.Principal, leaseRef, fence string, generation int64, search string, offset int32) ([]entity.AssistantIntegrationDefinition, int32, error) {
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, 0, err
	}
	ctx, stop := context.WithTimeout(ctx, 5*time.Second)
	defer stop()
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return nil, 0, errs.ErrUnavailable
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()
	fenceDigest := sha256.Sum256([]byte(fence))
	var actorRef, actorID string
	err = tx.QueryRow(ctx, queryAssistantSearchResolveLease, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "lease_ref": leaseRef,
		"fence_digest": hex.EncodeToString(fenceDigest[:]), "generation": generation,
	}).Scan(&actorRef, &actorID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, 0, errs.ErrNotFound
	}
	if err != nil {
		return nil, 0, errs.ErrUnavailable
	}
	if _, err := repository.resolveAccessSubject(ctx, tx, current.organizationID, actorRef); err != nil {
		return nil, 0, err
	}
	rows, err := tx.Query(ctx, queryAssistantIntegrationDefinitions, pgx.StrictNamedArgs{
		"query": search, "limit": maximumAssistantIntegrationDefinitions + 1, "offset": offset,
	})
	if err != nil {
		return nil, 0, errs.ErrUnavailable
	}
	definitions := make([]entity.AssistantIntegrationDefinition, 0, maximumAssistantIntegrationDefinitions)
	for rows.Next() {
		var item entity.AssistantIntegrationDefinition
		var schema, capabilities []byte
		if err := rows.Scan(&item.Key, &item.Name, &item.Description, &item.Category,
			&item.Adapter, &item.CredentialSecretKey, &schema, &capabilities, &item.Origin); err != nil {
			rows.Close()
			return nil, 0, errs.ErrUnavailable
		}
		if len(definitions) == maximumAssistantIntegrationDefinitions {
			rows.Close()
			if err := tx.Commit(ctx); err != nil {
				return nil, 0, errs.ErrUnavailable
			}
			return definitions, offset + maximumAssistantIntegrationDefinitions, nil
		}
		var entries []entity.IntegrationCapability
		if json.Unmarshal(schema, &item.ConfigurationFields) != nil || json.Unmarshal(capabilities, &entries) != nil ||
			len(item.ConfigurationFields) > 100 || len(entries) > 100 {
			rows.Close()
			return nil, 0, errs.ErrUnavailable
		}
		for _, entry := range entries {
			item.CapabilityKeys = append(item.CapabilityKeys, entry.Key)
		}
		definitions = append(definitions, item)
	}
	if rows.Err() != nil {
		rows.Close()
		return nil, 0, errs.ErrUnavailable
	}
	rows.Close()
	if err := tx.Commit(ctx); err != nil {
		return nil, 0, errs.ErrUnavailable
	}
	return definitions, 0, nil
}

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
	type rankedResult struct {
		entity.SearchResult
		relevance int
		orderTime time.Time
	}
	candidates := make([]rankedResult, 0, maximumAssistantSearchCandidates)
	for _, statement := range []string{queryQueriesSearchSelectEligibleResources, queryAssistantSearchExtra} {
		rows, queryErr := tx.Query(ctx, statement, pgx.StrictNamedArgs{
			"organization_id": current.organizationID, "query": search, "project_ref": "",
		})
		if queryErr != nil {
			return nil, false, errs.ErrUnavailable
		}
		for rows.Next() {
			if len(candidates) == maximumAssistantSearchCandidates {
				rows.Close()
				return nil, false, errs.ErrInvalid
			}
			var item rankedResult
			if err := rows.Scan(&item.Kind, &item.Ref, &item.ProjectRef, &item.Title, &item.Subtitle,
				&item.State, &item.UpdatedAt, &item.relevance, &item.orderTime); err != nil {
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
	}
	sort.Slice(candidates, func(left, right int) bool {
		if candidates[left].relevance != candidates[right].relevance {
			return candidates[left].relevance < candidates[right].relevance
		}
		if !candidates[left].orderTime.Equal(candidates[right].orderTime) {
			return candidates[left].orderTime.After(candidates[right].orderTime)
		}
		return candidates[left].Kind+"\x00"+candidates[left].Ref < candidates[right].Kind+"\x00"+candidates[right].Ref
	})
	result := make([]entity.SearchResult, 0, maximumAssistantSearchResults)
	truncated := false
	evaluatedAt := time.Now().UTC()
	for _, item := range candidates {
		if item.Kind != "PROJECT" && item.Kind != "AGENT" && item.Kind != "WORKFLOW" && item.Kind != "RUN" &&
			item.Kind != "ROLE_IMAGE" && item.Kind != "RUNTIME_ENVIRONMENT" && item.Kind != "SCHEDULE" &&
			item.Kind != "INTEGRATION" && item.Kind != "SECRET" {
			continue
		}
		visible, err := repository.resourceVisible(ctx, tx, current, subject.AccessSubject, bindings,
			item.Kind, item.Ref, item.ProjectRef, evaluatedAt)
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
		item.SearchResult.Title = strings.TrimSpace(item.Title)
		item.SearchResult.Subtitle = strings.TrimSpace(item.Subtitle)
		if len([]rune(item.Subtitle)) > 160 {
			item.SearchResult.Subtitle = string([]rune(item.Subtitle)[:160])
		}
		result = append(result, item.SearchResult)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, false, errs.ErrUnavailable
	}
	return result, truncated, nil
}
