package grpc

import (
	"context"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// ListModelDiscovery exposes bounded static metadata after exact-method authentication and owner authorization; request fields supply filters, never actor or account authority.
// ListModelDiscovery выдаёт ограниченные статические metadata после аутентификации точного метода и owner authorization; request fields задают фильтры, а не actor или account authority.
func (server *Server) ListModelDiscovery(ctx context.Context, request *cp.ListModelDiscoveryRequest) (*cp.ListModelDiscoveryResponse, error) {
	return queryResponse(ctx, cp.PlatformQueryService_ListModelDiscovery_FullMethodName, func(principal value.Principal) (*cp.ListModelDiscoveryResponse, error) {
		result, err := server.service.ListModelDiscovery(ctx, principal, query.Filter{Query: request.GetQuery(), Page: page(request.GetPage())}, query.ModelDiscoveryFilter{ProviderID: request.GetProviderId(), Family: request.GetFamily(), MinimumContextTokens: request.GetMinimumContextTokens(), InputModality: request.GetInputModality(), OutputModality: request.GetOutputModality(), Tools: request.GetTools(), Reasoning: request.GetReasoning(), Cache: request.GetCache(), GroupBy: request.GetGroupBy()})
		if err != nil {
			return nil, err
		}
		source := result.Source
		response := &cp.ListModelDiscoveryResponse{Models: []*cp.ModelDiscoveryModel{}, Total: result.Total, GroupBy: result.GroupBy, Page: &cp.PageInfo{NextPageToken: result.NextPageToken}, Source: &cp.ModelDiscoverySource{Schema: source.Schema, Source: source.Source, License: source.License, SourceRevision: source.SourceRevision, UpstreamSchema: source.UpstreamSchema, SchemaSha256: source.SchemaSHA256, UpstreamSha256: source.UpstreamSHA256, SnapshotSha256: source.SnapshotSHA256, SourceUrl: source.SourceURL, ModelCount: int32(source.ModelCount), OverlaySha256: source.OverlaySHA256, CatalogSha256: source.CatalogSHA256, RetrievedAt: source.RetrievedAt, ProjectionSchema: source.ProjectionSchema, ProjectionSchemaSha256: source.ProjectionSchemaSHA256, UpstreamSchemaValidation: source.UpstreamSchemaValidation}}
		for _, record := range result.Items {
			response.Models = append(response.Models, &cp.ModelDiscoveryModel{ProviderId: record.ProviderID, ProviderName: record.ProviderName, ModelId: record.ModelID, Name: record.Name, Type: record.Type, ContextTokens: record.ContextTokens, OutputTokens: record.OutputTokens, Tools: record.Tools, Reasoning: record.Reasoning, Family: record.Family, Cache: record.Cache, CacheImplicit: record.CacheImplicit, CacheExplicit: record.CacheExplicit, ContextKnown: record.ContextKnown, OutputKnown: record.OutputKnown, ReasoningEfforts: record.ReasoningEfforts, InputModalities: record.InputModalities, OutputModalities: record.OutputModalities, UpstreamStatus: record.UpstreamStatus, Known: record.Known, Adapter: record.Adapter, Account: record.Account, Allowed: record.Allowed, Tested: record.Tested, RouteConfigured: record.RouteConfigured})
		}
		return response, nil
	})
}
