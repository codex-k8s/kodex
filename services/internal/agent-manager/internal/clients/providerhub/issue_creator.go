// Package providerhub adapts provider-hub write commands to agent-manager.
package providerhub

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	providersv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/providers/v1"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/clients/grpcclient"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/errs"
	agentservice "github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/value"
	"google.golang.org/grpc"
)

const (
	callerID            = "agent-manager"
	defaultWriteTimeout = 10 * time.Second
)

// Config contains provider-hub client settings.
type Config struct {
	Addr      string
	AuthToken string
	Timeout   time.Duration
}

type providerHubClient interface {
	CreateIssue(context.Context, *providersv1.CreateIssueRequest, ...grpc.CallOption) (*providersv1.ProviderOperationResponse, error)
}

// IssueCreator calls provider-hub CreateIssue.
type IssueCreator struct {
	client    providerHubClient
	authToken string
	timeout   time.Duration
}

var _ agentservice.ProviderIssueCreator = (*IssueCreator)(nil)

// NewConnection creates a gRPC connection to provider-hub.
func NewConnection(cfg Config) (*grpc.ClientConn, error) {
	return grpcclient.NewConnection(cfg.Addr, "provider-hub")
}

// NewIssueCreator creates a provider-hub issue write client.
func NewIssueCreator(client providersv1.ProviderHubServiceClient, cfg Config) (*IssueCreator, error) {
	return newIssueCreator(client, cfg)
}

func newIssueCreator(client providerHubClient, cfg Config) (*IssueCreator, error) {
	settings, err := grpcclient.RequiredClientSettings(client, cfg.AuthToken, cfg.Timeout, defaultWriteTimeout, "provider-hub")
	if err != nil {
		return nil, err
	}
	creator := &IssueCreator{client: client}
	creator.authToken = settings.AuthToken
	creator.timeout = settings.Timeout
	return creator, nil
}

// CreateIssue sends a typed create Issue command through provider-hub.
func (creator *IssueCreator) CreateIssue(ctx context.Context, input agentservice.ProviderCreateIssueInput) (agentservice.ProviderIssueCommandResult, error) {
	if err := creator.requireReady(); err != nil {
		return agentservice.ProviderIssueCommandResult{}, err
	}
	request := createIssueRequest(input)
	callCtx, cancel := context.WithTimeout(creator.outgoingContext(ctx), creator.timeout)
	defer cancel()
	response, err := creator.client.CreateIssue(callCtx, request)
	if err != nil {
		return agentservice.ProviderIssueCommandResult{}, mapProviderHubWriteError(err)
	}
	return providerIssueCommandResult(response)
}

func (creator *IssueCreator) requireReady() error {
	if creator == nil {
		return errs.ErrDependencyUnavailable
	}
	if creator.client == nil {
		return errs.ErrDependencyUnavailable
	}
	return nil
}

func (creator *IssueCreator) outgoingContext(ctx context.Context) context.Context {
	return grpcclient.OutgoingContext(ctx, creator.authToken, callerID)
}

func createIssueRequest(input agentservice.ProviderCreateIssueInput) *providersv1.CreateIssueRequest {
	return &providersv1.CreateIssueRequest{
		ProjectId:              input.ProjectID.String(),
		RepositoryId:           input.RepositoryID.String(),
		ProviderSlug:           strings.TrimSpace(input.ProviderSlug),
		Title:                  strings.TrimSpace(input.Title),
		Body:                   strings.TrimSpace(input.Body),
		Labels:                 append([]string(nil), input.Labels...),
		AssigneeProviderLogins: append([]string(nil), input.AssigneeProviderLogins...),
		Milestone:              optionalString(strings.TrimSpace(input.Milestone)),
		WorkItemType:           optionalString(strings.TrimSpace(input.WorkItemType)),
		WatermarkJson:          optionalString(string(input.WatermarkJSON)),
		Meta:                   commandMeta(input.Meta, input.OperationPolicyContext, input.ApprovalGateRef),
		ExternalAccountId:      input.ExternalAccountID.String(),
		RepositoryTarget:       providerTarget(input.RepositoryTarget),
	}
}

func commandMeta(meta value.CommandMeta, policy agentservice.ProviderOperationPolicyContext, gate agentservice.ProviderApprovalGateReference) *providersv1.CommandMeta {
	commandID := optionalUUIDString(meta.CommandID)
	idempotencyKey := optionalString(strings.TrimSpace(meta.IdempotencyKey))
	requestID := firstNonEmpty(optionalStringValue(commandID), strings.TrimSpace(meta.IdempotencyKey), strings.TrimSpace(meta.CommandID.String()))
	return &providersv1.CommandMeta{
		CommandId:              commandID,
		IdempotencyKey:         idempotencyKey,
		Actor:                  actor(meta.Actor),
		Reason:                 "agent-follow-up-dispatch",
		RequestId:              requestID,
		RequestContext:         &providersv1.RequestContext{Source: callerID},
		OperationPolicyContext: policyContext(policy),
		ApprovalGateRef:        approvalGateRef(gate),
	}
}

func actor(actor value.Actor) *providersv1.Actor {
	return &providersv1.Actor{Type: strings.TrimSpace(actor.Type), Id: strings.TrimSpace(actor.ID)}
}

func providerTarget(target agentservice.ProviderCommandTarget) *providersv1.ProviderTarget {
	return &providersv1.ProviderTarget{
		ProviderSlug:         strings.TrimSpace(target.ProviderSlug),
		RepositoryFullName:   optionalString(strings.TrimSpace(target.RepositoryFullName)),
		ProviderRepositoryId: optionalString(strings.TrimSpace(target.ProviderRepositoryID)),
		WorkItemKind:         optionalWorkItemKind(target.WorkItemKind),
		Number:               optionalPositiveInt64(target.Number),
		ProviderObjectId:     optionalString(strings.TrimSpace(target.ProviderObjectID)),
		WebUrl:               optionalString(strings.TrimSpace(target.WebURL)),
	}
}

func policyContext(policy agentservice.ProviderOperationPolicyContext) *providersv1.ProviderOperationPolicyContext {
	return &providersv1.ProviderOperationPolicyContext{
		ProjectId:         optionalString(policy.ProjectID),
		RepositoryId:      optionalString(policy.RepositoryID),
		Stage:             optionalString(policy.Stage),
		RoleId:            optionalString(policy.RoleID),
		RoleKey:           optionalString(policy.RoleKey),
		OperationType:     operationType(policy.OperationType),
		TargetRef:         optionalString(policy.TargetRef),
		ChangedFields:     append([]string(nil), policy.ChangedFields...),
		RiskTags:          append([]string(nil), policy.RiskTags...),
		RiskLevel:         riskLevel(policy.RiskLevel),
		ApprovalRequired:  policy.ApprovalRequired,
		PolicyVersion:     optionalString(policy.PolicyVersion),
		PolicySnapshotRef: optionalString(policy.PolicySnapshotRef),
	}
}

func approvalGateRef(gate agentservice.ProviderApprovalGateReference) *providersv1.ApprovalGateReference {
	if gate.ApprovalID == "" && gate.GateType == "" && gate.Decision == "" && gate.DecidedByActorID == "" && gate.DecidedAt == "" && gate.EvidenceRef == "" && gate.PolicyVersion == "" {
		return nil
	}
	return &providersv1.ApprovalGateReference{
		ApprovalId:       strings.TrimSpace(gate.ApprovalID),
		GateType:         strings.TrimSpace(gate.GateType),
		Decision:         strings.TrimSpace(gate.Decision),
		DecidedByActorId: optionalString(gate.DecidedByActorID),
		DecidedAt:        optionalString(gate.DecidedAt),
		EvidenceRef:      optionalString(gate.EvidenceRef),
		PolicyVersion:    optionalString(gate.PolicyVersion),
	}
}

func providerIssueCommandResult(response *providersv1.ProviderOperationResponse) (agentservice.ProviderIssueCommandResult, error) {
	if response == nil || response.GetProviderOperation() == nil {
		return agentservice.ProviderIssueCommandResult{}, errs.ErrDependencyUnavailable
	}
	operation := response.GetProviderOperation()
	result := response.GetResult()
	commandResult := agentservice.ProviderIssueCommandResult{
		ProviderOperationRef: providerOperationRef(operation.GetProviderOperationId()),
		Status:               operationStatus(operation.GetStatus()),
		ResultRef:            firstNonEmpty(result.GetResultRef(), operation.GetResultRef()),
		ProviderObjectID:     firstNonEmpty(result.GetProviderObjectId(), ""),
		ProviderVersion:      firstNonEmpty(result.GetProviderVersion(), operation.GetProviderVersion()),
		ErrorCode:            strings.TrimSpace(operation.GetErrorCode()),
		ErrorMessage:         strings.TrimSpace(operation.GetErrorMessage()),
	}
	if target := result.GetTarget(); target != nil {
		commandResult.Target = providerTargetFromProto(target)
	} else if projection := response.GetWorkItemProjection(); projection != nil {
		commandResult.Target = providerTargetFromProjection(projection)
	}
	if commandResult.Status == "" {
		return agentservice.ProviderIssueCommandResult{}, errs.ErrDependencyUnavailable
	}
	return commandResult, nil
}

func providerTargetFromProto(target *providersv1.ProviderTarget) agentservice.ProviderCommandTarget {
	return providerTargetFromValues(
		target.GetProviderSlug(),
		target.GetRepositoryFullName(),
		target.GetProviderRepositoryId(),
		target.GetWorkItemKind(),
		target.GetNumber(),
		target.GetProviderObjectId(),
		target.GetWebUrl(),
	)
}

func providerTargetFromProjection(projection *providersv1.WorkItemProjection) agentservice.ProviderCommandTarget {
	return providerTargetFromValues(
		projection.GetProviderSlug(),
		projection.GetRepositoryFullName(),
		"",
		projection.GetKind(),
		projection.GetNumber(),
		projection.GetProviderWorkItemId(),
		projection.GetWebUrl(),
	)
}

func providerTargetFromValues(slug, fullName, repositoryID string, kind providersv1.WorkItemKind, number int64, objectID, webURL string) agentservice.ProviderCommandTarget {
	target := agentservice.ProviderCommandTarget{}
	target.ProviderSlug = strings.TrimSpace(slug)
	target.RepositoryFullName = strings.TrimSpace(fullName)
	target.ProviderRepositoryID = strings.TrimSpace(repositoryID)
	target.WorkItemKind = workItemKind(kind)
	target.Number = number
	target.ProviderObjectID = strings.TrimSpace(objectID)
	target.WebURL = strings.TrimSpace(webURL)
	return target
}

func mapProviderHubWriteError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) {
		return err
	}
	return grpcclient.MapReadError(err, "provider-hub write command failed")
}

func providerOperationRef(id string) string {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return ""
	}
	return "provider_operation:" + trimmed
}

func optionalString(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func optionalStringValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func optionalUUIDString(id uuid.UUID) *string {
	if id == uuid.Nil {
		return nil
	}
	value := id.String()
	return &value
}

func optionalPositiveInt64(value int64) *int64 {
	if value <= 0 {
		return nil
	}
	return &value
}

func optionalWorkItemKind(kind string) *providersv1.WorkItemKind {
	mapped := workItemKindToProto(kind)
	if mapped == providersv1.WorkItemKind_WORK_ITEM_KIND_UNSPECIFIED {
		return nil
	}
	return &mapped
}

func workItemKindToProto(kind string) providersv1.WorkItemKind {
	switch strings.TrimSpace(kind) {
	case "issue":
		return providersv1.WorkItemKind_WORK_ITEM_KIND_ISSUE
	case "pull_request":
		return providersv1.WorkItemKind_WORK_ITEM_KIND_PULL_REQUEST
	case "merge_request":
		return providersv1.WorkItemKind_WORK_ITEM_KIND_MERGE_REQUEST
	default:
		return providersv1.WorkItemKind_WORK_ITEM_KIND_UNSPECIFIED
	}
}

func workItemKind(kind providersv1.WorkItemKind) string {
	switch kind {
	case providersv1.WorkItemKind_WORK_ITEM_KIND_ISSUE:
		return "issue"
	case providersv1.WorkItemKind_WORK_ITEM_KIND_PULL_REQUEST:
		return "pull_request"
	case providersv1.WorkItemKind_WORK_ITEM_KIND_MERGE_REQUEST:
		return "merge_request"
	default:
		return ""
	}
}

func operationType(operationType string) providersv1.ProviderOperationType {
	switch strings.TrimSpace(operationType) {
	case agentservice.ProviderOperationTypeCreateIssue:
		return providersv1.ProviderOperationType_PROVIDER_OPERATION_TYPE_CREATE_ISSUE
	default:
		return providersv1.ProviderOperationType_PROVIDER_OPERATION_TYPE_UNSPECIFIED
	}
}

func riskLevel(level string) providersv1.ProviderOperationRiskLevel {
	levels := map[string]providersv1.ProviderOperationRiskLevel{
		agentservice.ProviderRiskLevelLow:      providersv1.ProviderOperationRiskLevel_PROVIDER_OPERATION_RISK_LEVEL_LOW,
		agentservice.ProviderRiskLevelMedium:   providersv1.ProviderOperationRiskLevel_PROVIDER_OPERATION_RISK_LEVEL_MEDIUM,
		agentservice.ProviderRiskLevelHigh:     providersv1.ProviderOperationRiskLevel_PROVIDER_OPERATION_RISK_LEVEL_HIGH,
		agentservice.ProviderRiskLevelCritical: providersv1.ProviderOperationRiskLevel_PROVIDER_OPERATION_RISK_LEVEL_CRITICAL,
	}
	if mapped, ok := levels[strings.TrimSpace(level)]; ok {
		return mapped
	}
	return providersv1.ProviderOperationRiskLevel_PROVIDER_OPERATION_RISK_LEVEL_UNSPECIFIED
}

func operationStatus(status providersv1.ProviderOperationStatus) agentservice.ProviderOperationStatus {
	statuses := map[providersv1.ProviderOperationStatus]agentservice.ProviderOperationStatus{
		providersv1.ProviderOperationStatus_PROVIDER_OPERATION_STATUS_SUCCEEDED:        agentservice.ProviderOperationStatusSucceeded,
		providersv1.ProviderOperationStatus_PROVIDER_OPERATION_STATUS_FAILED:           agentservice.ProviderOperationStatusFailed,
		providersv1.ProviderOperationStatus_PROVIDER_OPERATION_STATUS_RETRYABLE_FAILED: agentservice.ProviderOperationStatusRetryableFailed,
		providersv1.ProviderOperationStatus_PROVIDER_OPERATION_STATUS_DENIED:           agentservice.ProviderOperationStatusDenied,
		providersv1.ProviderOperationStatus_PROVIDER_OPERATION_STATUS_IN_PROGRESS:      agentservice.ProviderOperationStatusInProgress,
	}
	if mapped, ok := statuses[status]; ok {
		return mapped
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" && trimmed != uuid.Nil.String() {
			return trimmed
		}
	}
	return ""
}
