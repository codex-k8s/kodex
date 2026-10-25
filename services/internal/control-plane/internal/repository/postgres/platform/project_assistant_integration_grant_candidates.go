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

// GetProjectAssistantIntegrationGrantCandidates validates filters, resolves the trusted principal
// and project assistant under the owner transaction, then returns candidates only after the
// existing transaction commit.
// GetProjectAssistantIntegrationGrantCandidates проверяет filters, разрешает trusted principal и
// проектного помощника внутри owner transaction, затем возвращает кандидатов после прежнего
// transaction commit.
func (repository *Repository) GetProjectAssistantIntegrationGrantCandidates(ctx context.Context, principal value.Principal, projectRef, connectionRef, search string, page query.Page) (entity.ProjectAssistantIntegrationGrantCandidates, error) {
	result := entity.ProjectAssistantIntegrationGrantCandidates{}
	if projectRef == "" || connectionRef == "" || len(connectionRef) > 96 || strings.ContainsAny(connectionRef, "\x00\r\n/ ") || !utf8.ValidString(search) || len([]rune(search)) > 200 || strings.ContainsRune(search, '\x00') || page.Size < 0 || page.Size > 100 || len(page.Token) > 512 {
		return result, errs.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return result, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return result, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	profile, err := scanProjectAssistant(tx.QueryRow(ctx, queryProjectAssistantGet, pgx.StrictNamedArgs{"organization_id": current.organizationID, "project_ref": projectRef, "authority_project": current.authorityProjectID}))
	if err != nil {
		return result, err
	}
	result, err = repository.projectAssistantIntegrationGrantCandidatesTx(ctx, tx, current, profile.AgentRef, connectionRef, search, page)
	if err != nil {
		return result, err
	}
	if err = tx.Commit(ctx); err != nil {
		return result, errs.ErrUnavailable
	}
	return result, nil
}

// projectAssistantIntegrationGrantCandidatesTx resolves exact project/profile/connection ownership
// before reading grants, derives public scopes and project-bound cursor pins, and retains
// package/recipient readiness distinctions before shared projection/paging.
// projectAssistantIntegrationGrantCandidatesTx разрешает точное project, profile и connection
// ownership до чтения grants, выводит public scopes и project-bound cursor pins и сохраняет
// различия package и recipient readiness до общих projection и paging.
func (repository *Repository) projectAssistantIntegrationGrantCandidatesTx(ctx context.Context, tx pgx.Tx, current scope, assistantRef, connectionRef, search string, page query.Page) (entity.ProjectAssistantIntegrationGrantCandidates, error) {
	result := entity.ProjectAssistantIntegrationGrantCandidates{SystemAssistantIntegrationGrantCandidates: entity.SystemAssistantIntegrationGrantCandidates{Items: []entity.SystemAssistantIntegrationGrantCandidate{}}}
	target, err := repository.projectAssistantIntegrationGrantOwner(ctx, tx, current, assistantRef, connectionRef)
	if err != nil {
		return result, err
	}
	result.ProjectRef, result.ProfileRef, result.ProfileVersion = target.projectRef, target.profileRef, target.profileVersion
	result.ScopeKind, result.OrganizationRef, result.AssistantRef, result.AssistantVersion, result.ConnectionRef = "ORGANIZATION", current.organizationRef, target.ref, target.version, connectionRef
	var definitionKey string
	var connectionReady bool
	if err = tx.QueryRow(ctx, querySystemAssistantIntegrationGrantsConnection, current.organizationID, connectionRef).Scan(&result.ConnectionVersion, &definitionKey, &result.DefinitionVersion, &result.DefinitionDigest, &connectionReady); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return result, errs.ErrNotFound
		}
		return result, errs.ErrUnavailable
	}
	definition, executable, err := repository.assistantRecipientCatalogPackage(ctx, tx, current.organizationID, connectionRef, definitionKey, result.DefinitionVersion, result.DefinitionDigest)
	if err != nil {
		return result, err
	}
	configuration := map[string]string{}
	if definition.Metadata.Key == "github" {
		configuration, _, _, err = integrationCandidateScope(ctx, tx, current, integrationCandidateRow{ConnectionRef: connectionRef, RecipientKind: "AGENT", RecipientRef: target.ref})
		if err != nil {
			return result, err
		}
	}
	publicScopes := map[string]map[string]string{}
	publicDigests := map[string]string{}
	scopeCommitment := ""
	for _, capability := range definition.Spec.Capabilities {
		if !capability.CallableByAgent() {
			continue
		}
		values, digest, err := projectAssistantPublicResourceScope(definition, capability, configuration)
		if err != nil {
			return result, err
		}
		publicScopes[capability.Key], publicDigests[capability.Key] = values, digest
		if scopeCommitment != "" && digest != scopeCommitment {
			return result, errs.ErrUnavailable
		}
		scopeCommitment = digest
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
		Tenant, Actor, Connection, Assistant, Definition, Search, Project, Profile, PublicScope string
		ConnectionVersion, AssistantVersion, ProfileVersion                                     int64
	}{current.organizationID, current.actorID, connectionRef, target.ref, result.DefinitionDigest, search, target.projectRef, target.profileRef, scopeCommitment, result.ConnectionVersion, target.version, target.profileVersion})
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
		item.ResourceScope, item.ResourceScopeDigest = publicScopes[capability.Key], publicDigests[capability.Key]
		item.CurrentApprovalScopePaths = append([]string{}, item.CurrentApprovalScopePaths...)
		item.Grantable = executable && connectionReady && target.ready
		item.Reason = "READY"
		if !connectionReady {
			item.Reason = "CONNECTION_UNAVAILABLE"
		} else if !target.ready {
			item.Reason = "RECIPIENT_UNAVAILABLE"
		}
		if !executable {
			item.Reason = "PACKAGE_UNAVAILABLE"
		}
		if capability.Operation == repositoryBundleOperation {
			item.Grantable, item.Reason = false, "RECIPIENT_UNAVAILABLE"
		}
		item.Capability, err = integrationCapabilityProjection(capability)
		if err != nil {
			return result, err
		}
		items = append(items, item)
	}
	result.Items, result.Total, result.NextPageToken = assistantIntegrationCandidatePage(items, page.Size, contextDigest, after)
	return result, nil
}
