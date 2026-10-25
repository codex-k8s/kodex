package grpc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"strings"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// assistantGrantScopeIdentifier bounds GitHub owner/repository scope identifiers to the configured
// ASCII character set and length; dot-only exclusions are checked separately.
// assistantGrantScopeIdentifier ограничивает owner/repository scope identifiers GitHub заданным
// ASCII-набором и длиной; запрет одиночной и двойной точки проверяется отдельно.
var assistantGrantScopeIdentifier = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,100}$`)

// castAssistantIntegrationGrantCandidate checks the closed reason and optional exact GitHub
// repository scope shape, identifier grammar and JSON digest before projecting candidate and
// current-grant metadata.
// castAssistantIntegrationGrantCandidate проверяет закрытую причину и необязательные точные форму
// GitHub repository scope, грамматику идентификаторов и JSON digest до преобразования кандидата и
// текущего grant.
func castAssistantIntegrationGrantCandidate(item entity.SystemAssistantIntegrationGrantCandidate) (*cp.SystemAssistantIntegrationGrantCandidate, error) {
	reason, err := integrationCandidateReason(item.Reason)
	if err != nil {
		return nil, err
	}
	result := &cp.SystemAssistantIntegrationGrantCandidate{Capability: castIntegrationCapability(item.Capability), Grantable: item.Grantable, Reason: reason,
		CurrentGrantRef: item.CurrentGrantRef, CurrentGrantVersion: item.CurrentGrantVersion, CurrentApprovalPolicy: integrationApprovalPolicy(item.CurrentApprovalPolicy),
		CurrentApprovalScopePaths: append([]string{}, item.CurrentApprovalScopePaths...), CurrentGrantEnabled: item.CurrentGrantEnabled}
	if item.ResourceScope != nil {
		if !strings.HasPrefix(item.Capability.Key, "github.") || item.Capability.ResourceKind != "GITHUB_REPOSITORY" || len(item.ResourceScope) != 2 {
			return nil, errs.ErrUnavailable
		}
		for _, key := range []string{"owner", "repository"} {
			v := item.ResourceScope[key]
			if !assistantGrantScopeIdentifier.MatchString(v) || v == "." || v == ".." {
				return nil, errs.ErrUnavailable
			}
		}
		encoded, err := json.Marshal(item.ResourceScope)
		if err != nil {
			return nil, errs.ErrUnavailable
		}
		digest := sha256.Sum256(encoded)
		if hex.EncodeToString(digest[:]) != item.ResourceScopeDigest {
			return nil, errs.ErrUnavailable
		}
		result.ResourceScope = &cp.IntegrationResourceScope{Kind: integrationResourceKind(item.Capability.ResourceKind), Values: item.ResourceScope, Digest: item.ResourceScopeDigest}
	} else if item.ResourceScopeDigest != "" {
		return nil, errs.ErrUnavailable
	}
	return result, nil
}

// GetProjectAssistantIntegrationGrantCandidates reads the authorized project-assistant candidate
// page with exact profile, assistant, connection and definition pins and rejects unrepresentable
// candidates.
// GetProjectAssistantIntegrationGrantCandidates читает авторизованную страницу кандидатов
// проектного ассистента с точными привязками профиля, ассистента, подключения и определения,
// отклоняя непредставимых кандидатов.
func (server *Server) GetProjectAssistantIntegrationGrantCandidates(ctx context.Context, request *cp.GetProjectAssistantIntegrationGrantCandidatesRequest) (*cp.GetProjectAssistantIntegrationGrantCandidatesResponse, error) {
	return queryResponse(ctx, cp.PlatformQueryService_GetProjectAssistantIntegrationGrantCandidates_FullMethodName, func(p value.Principal) (*cp.GetProjectAssistantIntegrationGrantCandidatesResponse, error) {
		result, err := server.service.GetProjectAssistantIntegrationGrantCandidates(ctx, p, request.GetProjectRef(), request.GetConnectionRef(), request.GetQuery(), page(request.GetPage()))
		if err != nil {
			return nil, err
		}
		response := &cp.GetProjectAssistantIntegrationGrantCandidatesResponse{ScopeKind: runtimeSecretScopeKind(result.ScopeKind), OrganizationRef: result.OrganizationRef,
			ProjectRef: result.ProjectRef, AssistantProfileRef: result.ProfileRef, ProfileVersion: result.ProfileVersion, AssistantRef: result.AssistantRef, AssistantVersion: result.AssistantVersion,
			ConnectionRef: result.ConnectionRef, ConnectionVersion: result.ConnectionVersion, DefinitionVersion: result.DefinitionVersion, DefinitionDigest: result.DefinitionDigest,
			Items: []*cp.SystemAssistantIntegrationGrantCandidate{}, Total: result.Total, Page: &cp.PageInfo{NextPageToken: result.NextPageToken}}
		for _, item := range result.Items {
			candidate, err := castAssistantIntegrationGrantCandidate(item)
			if err != nil {
				return nil, err
			}
			response.Items = append(response.Items, candidate)
		}
		return response, nil

	})
}
