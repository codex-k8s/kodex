package grpc

import (
	"context"
	"strings"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
)

func (server *Server) ListProviderDefinitions(ctx context.Context, request *controlplanev1.ListProviderDefinitionsRequest) (*controlplanev1.ListProviderDefinitionsResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_ListProviderDefinitions_FullMethodName)
	if err != nil {
		return nil, err
	}
	items, next, err := server.service.ListProviderDefinitions(ctx, p, query.Filter{Query: request.GetQuery(), Page: page(request.GetPage())})
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ListProviderDefinitionsResponse{Page: &controlplanev1.PageInfo{NextPageToken: next}}
	for _, item := range items {
		response.Definitions = append(response.Definitions, castProviderDefinition(item))
	}
	return response, nil
}

func (server *Server) ListProviderAccounts(ctx context.Context, request *controlplanev1.ListProviderAccountsRequest) (*controlplanev1.ListProviderAccountsResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_ListProviderAccounts_FullMethodName)
	if err != nil {
		return nil, err
	}
	state := ""
	if request.GetState() != controlplanev1.ProviderAccountState_PROVIDER_ACCOUNT_STATE_UNSPECIFIED {
		state = enumSuffix(request.GetState(), "PROVIDER_ACCOUNT_STATE_")
	}
	items, next, actions, err := server.service.ListProviderAccounts(ctx, p, query.Filter{
		Query: request.GetQuery(), State: state, DefinitionKey: request.GetDefinitionKey(), Page: page(request.GetPage()),
	})
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ListProviderAccountsResponse{
		Page: &controlplanev1.PageInfo{NextPageToken: next}, NextActions: nextActions(actions),
	}
	for _, item := range items {
		response.Accounts = append(response.Accounts, castProviderAccount(item))
	}
	return response, nil
}

func (server *Server) GetProviderAccount(ctx context.Context, request *controlplanev1.GetProviderAccountRequest) (*controlplanev1.GetProviderAccountResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_GetProviderAccount_FullMethodName)
	if err != nil {
		return nil, err
	}
	item, err := server.service.GetProviderAccount(ctx, p, request.GetAccountRef())
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.GetProviderAccountResponse{Account: castProviderAccount(item)}, nil
}

func castProviderDefinition(value entity.ProviderDefinition) *controlplanev1.ProviderDefinition {
	result := &controlplanev1.ProviderDefinition{
		Key: value.Key, Name: value.Name, Description: value.Description, ModelIds: value.ModelIDs,
		DefaultModelId: value.DefaultModelID, Available: value.Available, Ready: value.Ready,
		ReadinessBlockers: value.ReadinessBlockers,
	}
	for _, method := range value.AuthorizationMethods {
		result.AuthorizationMethods = append(result.AuthorizationMethods, providerAuthorizationMethod(method))
	}
	return result
}

func castProviderAccount(value entity.ProviderAccount) *controlplanev1.ProviderAccount {
	result := &controlplanev1.ProviderAccount{
		Ref: value.Ref, Version: value.Version, DefinitionKey: value.DefinitionKey, Name: value.Name,
		ExternalAccountMasked: value.ExternalAccountMasked, State: providerAccountState(value.State),
		Enabled: value.Enabled, Ready: value.Ready, NextActions: nextActions(value.NextActions),
		CreatedAt: timestamp(value.CreatedAt), UpdatedAt: timestamp(value.UpdatedAt),
	}
	if value.Authorization != nil {
		result.Authorization = &controlplanev1.ProviderAuthorization{
			Ref: value.Authorization.Ref, Method: providerAuthorizationMethod(value.Authorization.Method),
			State:           providerAuthorizationState(value.Authorization.State),
			VerificationUri: value.Authorization.VerificationURI, UserCode: value.Authorization.UserCode,
			ExpiresAt: optionalTimestamp(value.Authorization.ExpiresAt), SafeFailureCode: value.Authorization.SafeFailureCode,
		}
	}
	return result
}

func providerAuthorizationMethod(value string) controlplanev1.ProviderAuthorizationMethod {
	return controlplanev1.ProviderAuthorizationMethod(controlplanev1.ProviderAuthorizationMethod_value["PROVIDER_AUTHORIZATION_METHOD_"+value])
}

func providerAuthorizationState(value string) controlplanev1.ProviderAuthorizationState {
	return controlplanev1.ProviderAuthorizationState(controlplanev1.ProviderAuthorizationState_value["PROVIDER_AUTHORIZATION_STATE_"+value])
}

func providerAccountState(value string) controlplanev1.ProviderAccountState {
	return controlplanev1.ProviderAccountState(controlplanev1.ProviderAccountState_value["PROVIDER_ACCOUNT_STATE_"+value])
}

func (server *Server) GetRuntimeEnvironmentReadiness(ctx context.Context, request *controlplanev1.GetRuntimeEnvironmentReadinessRequest) (*controlplanev1.GetRuntimeEnvironmentReadinessResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_GetRuntimeEnvironmentReadiness_FullMethodName)
	if err != nil {
		return nil, err
	}
	item, err := server.service.GetRuntimeEnvironmentReadiness(ctx, p, request.GetEnvironmentRef())
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.GetRuntimeEnvironmentReadinessResponse{Readiness: &controlplanev1.RuntimeEnvironmentReadiness{
		EnvironmentRef: item.EnvironmentRef, EnvironmentVersion: item.EnvironmentVersion,
		PublishedVersionRef: item.PublishedVersionRef, PublishedVersionDigest: item.PublishedVersionDigest,
		Ready: item.Ready, Blockers: item.Blockers, ObservedAt: timestamp(item.ObservedAt),
	}}, nil
}

func (server *Server) ListRuntimeEnvironmentAgents(ctx context.Context, request *controlplanev1.ListRuntimeEnvironmentAgentsRequest) (*controlplanev1.ListRuntimeEnvironmentAgentsResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_ListRuntimeEnvironmentAgents_FullMethodName)
	if err != nil {
		return nil, err
	}
	items, next, err := server.service.ListRuntimeEnvironmentAgents(ctx, p, query.Filter{
		ResourceRef: request.GetEnvironmentRef(), Query: request.GetQuery(), Page: page(request.GetPage()),
	})
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ListRuntimeEnvironmentAgentsResponse{Page: &controlplanev1.PageInfo{NextPageToken: next}}
	for _, item := range items {
		response.Agents = append(response.Agents, castAgent(item))
	}
	return response, nil
}

func (server *Server) ListScheduleRevisions(ctx context.Context, request *controlplanev1.ListScheduleRevisionsRequest) (*controlplanev1.ListScheduleRevisionsResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_ListScheduleRevisions_FullMethodName)
	if err != nil {
		return nil, err
	}
	items, next, err := server.service.ListScheduleRevisions(ctx, p, query.Filter{ResourceRef: request.GetScheduleRef(), Page: page(request.GetPage())})
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ListScheduleRevisionsResponse{Page: &controlplanev1.PageInfo{NextPageToken: next}}
	for _, item := range items {
		response.Revisions = append(response.Revisions, castScheduleRevision(item))
	}
	return response, nil
}

func castScheduleRevision(value entity.ScheduleRevision) *controlplanev1.ScheduleRevision {
	return &controlplanev1.ScheduleRevision{
		Ref: value.Ref, Revision: value.Revision, Digest: value.Digest, Name: value.Name,
		Target: castRunTarget(value.Target), Preset: value.Preset, CronExpression: value.CronExpression,
		Timezone: value.Timezone, Input: structure(value.Input), SessionPolicy: value.SessionPolicy,
		NotificationPolicy: value.NotificationPolicy, CreatedAt: timestamp(value.CreatedAt),
	}
}

func (server *Server) ListScheduleRuns(ctx context.Context, request *controlplanev1.ListScheduleRunsRequest) (*controlplanev1.ListScheduleRunsResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_ListScheduleRuns_FullMethodName)
	if err != nil {
		return nil, err
	}
	items, next, err := server.service.ListScheduleRuns(ctx, p, query.Filter{ResourceRef: request.GetScheduleRef(), Page: page(request.GetPage())})
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ListScheduleRunsResponse{Page: &controlplanev1.PageInfo{NextPageToken: next}}
	for _, item := range items {
		response.Occurrences = append(response.Occurrences, &controlplanev1.ScheduleRunOccurrence{
			ScheduleRef: item.ScheduleRef, ScheduleRevisionRef: item.ScheduleRevisionRef,
			ScheduleRevision: item.ScheduleRevision, Run: castRun(item.Run),
		})
	}
	return response, nil
}

func (server *Server) ListRoleImageRecipeRevisions(ctx context.Context, request *controlplanev1.ListRoleImageRecipeRevisionsRequest) (*controlplanev1.ListRoleImageRecipeRevisionsResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_ListRoleImageRecipeRevisions_FullMethodName)
	if err != nil {
		return nil, err
	}
	items, next, err := server.service.ListRoleImageRecipeRevisions(ctx, p, query.Filter{ResourceRef: request.GetRecipeRef(), Page: page(request.GetPage())})
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ListRoleImageRecipeRevisionsResponse{Page: &controlplanev1.PageInfo{NextPageToken: next}}
	for _, item := range items {
		response.Revisions = append(response.Revisions, &controlplanev1.RoleImageRecipeRevision{
			Ref: item.Ref, RecipeRef: item.RecipeRef, Revision: int64(item.Revision), RecipeVersion: int64(item.RecipeVersion),
			RecipeGeneration: int64(item.RecipeGeneration), SpecSha256: item.SpecSHA256,
			ProvenanceSha256: item.ProvenanceSHA256, SourceSha256: item.SourceSHA256,
			ImmutableBuildSha256: item.ImmutableBuildSHA256, ImageArtifactRef: item.ImageArtifactRef,
			ManifestDigest: item.ManifestDigest, PromotedReference: item.PromotedReference,
			PromotionReceiptSha256: item.PromotionReceiptSHA256, CreatedAt: timestamp(item.CreatedAt),
		})
	}
	return response, nil
}

func normalizedProviderState(value controlplanev1.ProviderAccountState) string {
	return strings.TrimPrefix(value.String(), "PROVIDER_ACCOUNT_STATE_")
}
