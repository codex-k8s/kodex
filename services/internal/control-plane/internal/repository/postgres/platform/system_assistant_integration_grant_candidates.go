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
	"unicode/utf8"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// querySystemAssistantIntegrationGrantsConnection embeds the owner-scoped connection version, exact definition pins and readiness query.
// querySystemAssistantIntegrationGrantsConnection встраивает ограниченный владельцем запрос connection version, точных definition pins и readiness.
//
//go:embed sql/system_assistant_integration_grants__connection.sql
var querySystemAssistantIntegrationGrantsConnection string

// querySystemAssistantIntegrationGrantsCurrent embeds the six-column grant query restricted to one organization, connection and target agent.
// querySystemAssistantIntegrationGrantsCurrent встраивает шестиколоночный grant query, ограниченный одной organization, connection и target agent.
//
//go:embed sql/system_assistant_integration_grants__current.sql
var querySystemAssistantIntegrationGrantsCurrent string

// GetSystemAssistantIntegrationGrantCandidates resolves the trusted principal and system assistant
// owner in a read-only repeatable-read transaction, preserving exact package/connection pins and
// readiness before shared projection/paging and commit.
// GetSystemAssistantIntegrationGrantCandidates разрешает trusted principal и владельца системного
// помощника в read-only repeatable-read transaction, сохраняя точные package и connection pins и
// readiness до общих projection, paging и commit.
func (repository *Repository) GetSystemAssistantIntegrationGrantCandidates(ctx context.Context, principal value.Principal, connectionRef, search string, page query.Page) (entity.SystemAssistantIntegrationGrantCandidates, error) {
	result := entity.SystemAssistantIntegrationGrantCandidates{Items: []entity.SystemAssistantIntegrationGrantCandidate{}}
	if connectionRef == "" || len(connectionRef) > 96 || strings.ContainsAny(connectionRef, "\x00\r\n/ ") || !utf8.ValidString(search) || len([]rune(search)) > 200 || strings.ContainsRune(search, '\x00') || page.Size < 0 || page.Size > 100 || len(page.Token) > 512 {
		return result, errs.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return result, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return result, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	target, err := repository.systemAssistantIntegrationGrantOwner(ctx, tx, current, connectionRef)
	if err != nil {
		return result, err
	}
	result.ScopeKind, result.OrganizationRef, result.AssistantRef, result.AssistantVersion, result.ConnectionRef = "ORGANIZATION", current.organizationRef, target.ref, target.version, connectionRef
	var definitionKey string
	var connectionReady bool
	if err = tx.QueryRow(ctx, querySystemAssistantIntegrationGrantsConnection, current.organizationID, connectionRef).Scan(&result.ConnectionVersion, &definitionKey, &result.DefinitionVersion, &result.DefinitionDigest, &connectionReady); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return result, errs.ErrNotFound
		}
		return result, errs.ErrUnavailable
	}
	definition, err := repository.integrationPackage(ctx, tx, current.organizationID, connectionRef, definitionKey, result.DefinitionVersion, result.DefinitionDigest)
	if err != nil {
		return result, err
	}
	rows, err := tx.Query(ctx, querySystemAssistantIntegrationGrantsCurrent, current.organizationID, connectionRef, target.ref)
	if err != nil {
		return result, errs.ErrUnavailable
	}
	grants, err := scanAssistantIntegrationGrants(rows)
	if err != nil {
		return result, err
	}
	encoded, _ := json.Marshal(struct {
		Tenant, Actor, Connection, Assistant, Definition, Search string
		ConnectionVersion, AssistantVersion                      int64
	}{current.organizationID, current.actorID, connectionRef, target.ref, result.DefinitionDigest, search, result.ConnectionVersion, target.version})
	digest := sha256.Sum256(encoded)
	contextDigest := hex.EncodeToString(digest[:])
	after, err := assistantIntegrationCandidateAfter(page.Token, contextDigest)
	if err != nil {
		return result, err
	}
	items := []entity.SystemAssistantIntegrationGrantCandidate{}
	for _, capability := range definition.Spec.Capabilities {
		if !capability.CallableByAgent() || search != "" && !strings.Contains(strings.ToLower(capability.Key+" "+capability.Name), strings.ToLower(search)) {
			continue
		}
		item := grants[capability.Key]
		item.CurrentApprovalScopePaths = append([]string{}, item.CurrentApprovalScopePaths...)
		item.Grantable = connectionReady && target.ready
		item.Reason = "READY"
		if !connectionReady {
			item.Reason = "CONNECTION_UNAVAILABLE"
		} else if !target.ready {
			item.Reason = "RECIPIENT_UNAVAILABLE"
		}
		item.Capability, err = integrationCapabilityProjection(capability)
		if err != nil {
			return result, err
		}
		items = append(items, item)
	}
	result.Items, result.Total, result.NextPageToken = assistantIntegrationCandidatePage(items, page.Size, contextDigest, after)
	if err = tx.Commit(ctx); err != nil {
		return result, errs.ErrUnavailable
	}
	return result, nil
}
