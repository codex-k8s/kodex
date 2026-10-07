package platform

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

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
	grants := map[string]entity.SystemAssistantIntegrationGrantCandidate{}
	rows, err := tx.Query(ctx, querySystemAssistantIntegrationGrantsCurrent, current.organizationID, connectionRef, target.ref)
	if err != nil {
		return result, errs.ErrUnavailable
	}
	for rows.Next() {
		var key string
		var grant entity.SystemAssistantIntegrationGrantCandidate
		if rows.Scan(&grant.CurrentGrantRef, &grant.CurrentGrantVersion, &key, &grant.CurrentApprovalPolicy, &grant.CurrentApprovalScopePaths, &grant.CurrentGrantEnabled) != nil {
			rows.Close()
			return result, errs.ErrUnavailable
		}
		grants[key] = grant
	}
	rows.Close()
	if rows.Err() != nil {
		return result, errs.ErrUnavailable
	}
	encoded, _ := json.Marshal(struct {
		Tenant, Actor, Connection, Assistant, Definition, Search, Project, Profile string
		ConnectionVersion, AssistantVersion, ProfileVersion                        int64
	}{current.organizationID, current.actorID, connectionRef, target.ref, result.DefinitionDigest, search, target.projectRef, target.profileRef, result.ConnectionVersion, target.version, target.profileVersion})
	digest := sha256.Sum256(encoded)
	contextDigest := hex.EncodeToString(digest[:])
	after := ""
	if page.Token != "" {
		raw, err := base64.RawURLEncoding.DecodeString(page.Token)
		var cursor integrationCandidateCursor
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if err != nil || decoder.Decode(&cursor) != nil || decoder.Decode(new(any)) != io.EOF || cursor.Scope != contextDigest || !validCapabilityKey(cursor.After) {
			return result, errs.ErrInvalid
		}
		after = cursor.After
	}
	items := []entity.SystemAssistantIntegrationGrantCandidate{}
	for _, capability := range definition.Spec.Capabilities {
		if !capability.CallableByAgent() || search != "" && !strings.Contains(strings.ToLower(capability.Key+" "+capability.Name), strings.ToLower(search)) {
			continue
		}
		item := grants[capability.Key]
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
		schema, err := capability.InputSchema()
		if err != nil {
			return result, errs.ErrUnavailable
		}
		schemaDigest, err := capability.InputSchemaDigest()
		if err != nil {
			return result, errs.ErrUnavailable
		}
		item.Capability = entity.IntegrationCapability{Key: capability.Key, Name: capability.Name, Description: capability.Description, Operation: capability.Operation, Risk: capability.Risk, ApprovalPolicy: capability.ApprovalPolicy, AllowedApprovalPolicies: append([]string{}, capability.AllowedApprovalPolicies...), ResourceKind: capability.ResourceScope.Kind, InputFields: integrationConfigurationFields(capability.InputFields), InputSchema: string(schema), InputSchemaSHA256: schemaDigest}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Capability.Key < items[j].Capability.Key })
	result.Total = int64(len(items))
	limit := page.Size
	if limit == 0 {
		limit = 50
	}
	for _, item := range items {
		if item.Capability.Key <= after {
			continue
		}
		if len(result.Items) == int(limit) {
			cursor, _ := json.Marshal(integrationCandidateCursor{Scope: contextDigest, After: result.Items[len(result.Items)-1].Capability.Key})
			result.NextPageToken = base64.RawURLEncoding.EncodeToString(cursor)
			break
		}
		result.Items = append(result.Items, item)
	}
	return result, nil
}
