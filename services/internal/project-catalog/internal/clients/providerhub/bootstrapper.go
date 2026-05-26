// Package providerhub adapts project-catalog bootstrap commands to provider-hub.
package providerhub

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	grpcserver "github.com/codex-k8s/kodex/libs/go/grpcserver"
	providersv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/providers/v1"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/errs"
	projectservice "github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/value"
)

const callerID = "project-catalog"

// Config contains provider-hub gRPC connection settings.
type Config struct {
	Addr      string
	AuthToken string
	Timeout   time.Duration
}

// Bootstrapper delegates bootstrap PR creation to provider-hub.
type Bootstrapper struct {
	client    providersv1.ProviderHubServiceClient
	authToken string
	timeout   time.Duration
}

var _ projectservice.BootstrapProvider = (*Bootstrapper)(nil)

// NewConnection creates a lazy gRPC client connection to provider-hub.
func NewConnection(cfg Config) (*grpc.ClientConn, error) {
	return newProviderHubConnection(cfg.Addr)
}

// NewBootstrapper wraps a generated provider-hub client.
func NewBootstrapper(client providersv1.ProviderHubServiceClient, cfg Config) (*Bootstrapper, error) {
	if client == nil {
		return nil, fmt.Errorf("provider-hub client is required")
	}
	authToken, timeout, err := bootstrapperRuntimeConfig(cfg)
	if err != nil {
		return nil, err
	}
	return &Bootstrapper{client: client, authToken: authToken, timeout: timeout}, nil
}

func newProviderHubConnection(rawAddr string) (*grpc.ClientConn, error) {
	addr := strings.TrimSpace(rawAddr)
	if addr == "" {
		return nil, fmt.Errorf("provider-hub address is required")
	}
	return grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
}

func bootstrapperRuntimeConfig(cfg Config) (string, time.Duration, error) {
	authToken := strings.TrimSpace(cfg.AuthToken)
	if authToken == "" {
		return "", 0, fmt.Errorf("provider-hub auth token is required")
	}
	if cfg.Timeout > 0 {
		return authToken, cfg.Timeout, nil
	}
	return authToken, 5 * time.Second, nil
}

// CreateRepositoryBootstrapPullRequest calls provider-hub CreateBootstrapPullRequest.
func (b *Bootstrapper) CreateRepositoryBootstrapPullRequest(ctx context.Context, input projectservice.ProviderBootstrapPullRequestInput) (projectservice.RepositoryBootstrapProviderResult, error) {
	callCtx, cancel := context.WithTimeout(b.outgoingContext(ctx), b.timeout)
	defer cancel()
	response, err := b.client.CreateBootstrapPullRequest(callCtx, createBootstrapRequest(input))
	if err != nil {
		return projectservice.RepositoryBootstrapProviderResult{}, mapProviderError(err)
	}
	if response == nil {
		return projectservice.RepositoryBootstrapProviderResult{}, errs.ErrDependencyUnavailable
	}
	return bootstrapProviderResult(response), nil
}

func (b *Bootstrapper) outgoingContext(ctx context.Context) context.Context {
	return metadata.AppendToOutgoingContext(
		ctx,
		grpcserver.MetadataAuthorization,
		"Bearer "+b.authToken,
		grpcserver.MetadataCallerType,
		"service",
		grpcserver.MetadataCallerID,
		callerID,
	)
}

func createBootstrapRequest(input projectservice.ProviderBootstrapPullRequestInput) *providersv1.CreateBootstrapPullRequestRequest {
	return &providersv1.CreateBootstrapPullRequestRequest{
		ProjectId:         input.ProjectID.String(),
		RepositoryId:      input.RepositoryID.String(),
		ProviderSlug:      strings.TrimSpace(input.ProviderSlug),
		RepositoryTarget:  providerTarget(input.RepositoryTarget),
		BaseBranch:        strings.TrimSpace(input.BaseBranch),
		BootstrapBranch:   strings.TrimSpace(input.BootstrapBranch),
		CommitMessage:     strings.TrimSpace(input.CommitMessage),
		Title:             strings.TrimSpace(input.Title),
		Body:              strings.TrimSpace(input.Body),
		Draft:             input.Draft,
		Files:             bootstrapFiles(input.Files),
		WatermarkJson:     optionalString(string(input.WatermarkJSON), len(input.WatermarkJSON) > 0),
		Meta:              providerCommandMeta(input),
		ExternalAccountId: input.ExternalAccountID.String(),
	}
}

func providerTarget(target projectservice.RepositoryBootstrapProviderTarget) *providersv1.ProviderTarget {
	repositoryFullName := optionalString(target.RepositoryFullName, strings.TrimSpace(target.RepositoryFullName) != "")
	providerRepositoryID := optionalString(target.ProviderRepositoryID, strings.TrimSpace(target.ProviderRepositoryID) != "")
	webURL := optionalString(target.WebURL, strings.TrimSpace(target.WebURL) != "")
	return &providersv1.ProviderTarget{
		ProviderSlug:         strings.TrimSpace(target.ProviderSlug),
		RepositoryFullName:   repositoryFullName,
		ProviderRepositoryId: providerRepositoryID,
		WebUrl:               webURL,
	}
}

func bootstrapFiles(files []projectservice.RepositoryBootstrapFile) []*providersv1.BootstrapFile {
	result := make([]*providersv1.BootstrapFile, 0, len(files))
	for _, file := range files {
		result = append(result, &providersv1.BootstrapFile{
			Path:       strings.TrimSpace(file.Path),
			Content:    file.Content,
			Executable: file.Executable,
		})
	}
	return result
}

func providerCommandMeta(input projectservice.ProviderBootstrapPullRequestInput) *providersv1.CommandMeta {
	meta := input.Meta
	return &providersv1.CommandMeta{
		CommandId:      optionalUUID(meta.CommandID),
		IdempotencyKey: optionalString(meta.IdempotencyKey, strings.TrimSpace(meta.IdempotencyKey) != ""),
		Actor: &providersv1.Actor{
			Type: strings.TrimSpace(meta.Actor.Type),
			Id:   strings.TrimSpace(meta.Actor.ID),
		},
		Reason:                 strings.TrimSpace(meta.Reason),
		RequestId:              strings.TrimSpace(meta.RequestID),
		RequestContext:         providerRequestContext(meta.RequestContext),
		OperationPolicyContext: bootstrapPolicyContext(input),
	}
}

func providerRequestContext(context value.RequestContext) *providersv1.RequestContext {
	requestContext := &providersv1.RequestContext{Source: strings.TrimSpace(context.Source)}
	if traceID := strings.TrimSpace(context.TraceID); traceID != "" {
		requestContext.TraceId = &traceID
	}
	if sessionID := strings.TrimSpace(context.SessionID); sessionID != "" {
		requestContext.SessionId = &sessionID
	}
	if clientIPHash := strings.TrimSpace(context.ClientIPHash); clientIPHash != "" {
		requestContext.ClientIpHash = &clientIPHash
	}
	return requestContext
}

func bootstrapPolicyContext(input projectservice.ProviderBootstrapPullRequestInput) *providersv1.ProviderOperationPolicyContext {
	changedFields := []string{
		"repository_target",
		"base_branch",
		"bootstrap_branch",
		"commit_message",
		"title",
		"body",
		"files",
		"draft",
	}
	if len(input.WatermarkJSON) > 0 {
		changedFields = append(changedFields, "watermark_json")
	}
	return &providersv1.ProviderOperationPolicyContext{
		ProjectId:         optionalString(input.ProjectID.String(), input.ProjectID != uuid.Nil),
		RepositoryId:      optionalString(input.RepositoryID.String(), input.RepositoryID != uuid.Nil),
		Stage:             optionalString("repository_onboarding", true),
		RoleKey:           optionalString("project-catalog.bootstrap", true),
		OperationType:     providersv1.ProviderOperationType_PROVIDER_OPERATION_TYPE_CREATE_BOOTSTRAP_PULL_REQUEST,
		ChangedFields:     changedFields,
		RiskTags:          []string{"repository_bootstrap", "empty_repository"},
		RiskLevel:         providersv1.ProviderOperationRiskLevel_PROVIDER_OPERATION_RISK_LEVEL_MEDIUM,
		ApprovalRequired:  false,
		PolicyVersion:     optionalString("project-catalog:onb-1", true),
		PolicySnapshotRef: optionalString("services_policy:"+strings.TrimSpace(input.ServicesPolicy.ContentHash), strings.TrimSpace(input.ServicesPolicy.ContentHash) != ""),
	}
}

func bootstrapProviderResult(response *providersv1.ProviderOperationResponse) projectservice.RepositoryBootstrapProviderResult {
	var result projectservice.RepositoryBootstrapProviderResult
	if operation := response.GetProviderOperation(); operation != nil {
		result.ProviderOperationID = strings.TrimSpace(operation.GetProviderOperationId())
	}
	if commandResult := response.GetResult(); commandResult != nil {
		result.ProviderResultRef = strings.TrimSpace(commandResult.GetResultRef())
		result.ProviderObjectID = strings.TrimSpace(commandResult.GetProviderObjectId())
		if target := commandResult.GetTarget(); target != nil {
			result.ProviderWebURL = strings.TrimSpace(target.GetWebUrl())
		}
	}
	if projection := response.GetWorkItemProjection(); projection != nil {
		result.ProviderWorkItemProjectionID = strings.TrimSpace(projection.GetWorkItemProjectionId())
		if webURL := strings.TrimSpace(projection.GetWebUrl()); webURL != "" {
			result.ProviderWebURL = webURL
		}
	}
	return result
}

func optionalUUID(id uuid.UUID) *string {
	return optionalString(id.String(), id != uuid.Nil)
}

func optionalString(raw string, include bool) *string {
	if !include {
		return nil
	}
	value := strings.TrimSpace(raw)
	return &value
}

func mapProviderError(err error) error {
	switch status.Code(err) {
	case codes.InvalidArgument:
		return errs.ErrInvalidArgument
	case codes.Unauthenticated, codes.PermissionDenied:
		return errs.ErrForbidden
	case codes.NotFound:
		return errs.ErrNotFound
	case codes.AlreadyExists, codes.Aborted:
		return errs.ErrConflict
	case codes.FailedPrecondition:
		return errs.ErrPreconditionFailed
	case codes.Unavailable, codes.DeadlineExceeded, codes.ResourceExhausted:
		return errs.ErrDependencyUnavailable
	default:
		return errs.ErrDependencyUnavailable
	}
}
