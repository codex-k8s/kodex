// Package runtime adapts runtime-manager workspace preparation to agent-manager.
package runtime

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	runtimev1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/runtime/v1"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/clients/grpcclient"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/errs"
	agentservice "github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/value"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	callerID              = "agent-manager"
	defaultPrepareTimeout = 10 * time.Second
)

// Config contains runtime-manager client settings.
type Config struct {
	Addr      string
	AuthToken string
	Timeout   time.Duration
}

type runtimeManagerClient interface {
	PrepareRuntime(context.Context, *runtimev1.PrepareRuntimeRequest, ...grpc.CallOption) (*runtimev1.PrepareRuntimeResponse, error)
}

// Preparer calls runtime-manager PrepareRuntime.
type Preparer struct {
	client    runtimeManagerClient
	authToken string
	timeout   time.Duration
}

var _ agentservice.RuntimePreparer = (*Preparer)(nil)

// NewConnection creates a gRPC connection to runtime-manager.
func NewConnection(cfg Config) (*grpc.ClientConn, error) {
	return grpcclient.NewConnection(cfg.Addr, "runtime-manager")
}

// NewPreparer creates a runtime-manager PrepareRuntime client.
func NewPreparer(client runtimev1.RuntimeManagerServiceClient, cfg Config) (*Preparer, error) {
	return newPreparer(client, cfg)
}

func newPreparer(client runtimeManagerClient, cfg Config) (*Preparer, error) {
	settings, err := grpcclient.RequiredClientSettings(client, cfg.AuthToken, cfg.Timeout, defaultPrepareTimeout, "runtime-manager")
	if err != nil {
		return nil, err
	}
	return &Preparer{
		client:    client,
		authToken: settings.AuthToken,
		timeout:   settings.Timeout,
	}, nil
}

// PrepareRuntime reserves a runtime slot and starts workspace materialization.
func (p *Preparer) PrepareRuntime(ctx context.Context, input agentservice.RuntimePreparationInput) (agentservice.RuntimePreparationResult, error) {
	if p == nil || p.client == nil {
		return agentservice.RuntimePreparationResult{}, errs.ErrDependencyUnavailable
	}
	callCtx, cancel := context.WithTimeout(p.outgoingContext(ctx), p.timeout)
	defer cancel()
	response, err := p.client.PrepareRuntime(callCtx, prepareRuntimeRequest(input))
	if err != nil {
		return agentservice.RuntimePreparationResult{}, mapRuntimeError(err)
	}
	return prepareRuntimeResult(input, response)
}

func (p *Preparer) outgoingContext(ctx context.Context) context.Context {
	return grpcclient.OutgoingContext(ctx, p.authToken, callerID)
}

func prepareRuntimeRequest(input agentservice.RuntimePreparationInput) *runtimev1.PrepareRuntimeRequest {
	agentRunID := input.AgentRunID.String()
	return &runtimev1.PrepareRuntimeRequest{
		AgentRunId:           &agentRunID,
		RuntimeProfile:       strings.TrimSpace(input.RuntimeProfile),
		RuntimeMode:          runtimeMode(input.RuntimeMode),
		WorkspacePolicy:      workspacePolicy(input.WorkspacePolicy),
		PlacementConstraints: placementConstraints(input.PlacementConstraints),
		Meta:                 commandMeta(input.Meta),
	}
}

func workspacePolicy(policy agentservice.RuntimeWorkspacePolicy) *runtimev1.WorkspacePolicyInput {
	return &runtimev1.WorkspacePolicyInput{
		ProjectId:               policy.ProjectID.String(),
		PolicyDigest:            strings.TrimSpace(policy.PolicyDigest),
		PolicyVersion:           policy.PolicyVersion,
		Sources:                 workspaceSources(policy.Sources),
		ActivePolicyOverrideIds: trimmedStrings(policy.ActivePolicyOverrideIDs),
	}
}

func workspaceSources(sources []agentservice.RuntimeWorkspaceSource) []*runtimev1.WorkspaceSource {
	result := make([]*runtimev1.WorkspaceSource, 0, len(sources))
	for _, source := range sources {
		result = append(result, &runtimev1.WorkspaceSource{
			SourceId:      strings.TrimSpace(source.SourceID),
			Kind:          workspaceSourceKind(source.Kind),
			RepositoryId:  optionalUUIDString(source.RepositoryID),
			Provider:      optionalString(source.Provider),
			ProviderOwner: optionalString(source.ProviderOwner),
			ProviderName:  optionalString(source.ProviderName),
			SourceRef:     optionalString(source.SourceRef),
			CommitSha:     optionalString(source.CommitSHA),
			LocalPath:     strings.TrimSpace(source.LocalPath),
			AccessMode:    workspaceSourceAccessMode(source.AccessMode),
			Digest:        optionalString(source.Digest),
			MetadataJson:  defaultJSON(source.MetadataJSON),
		})
	}
	return result
}

func placementConstraints(constraints agentservice.RuntimePlacementConstraints) *runtimev1.PlacementConstraints {
	return &runtimev1.PlacementConstraints{
		ProjectId:             constraints.ProjectID.String(),
		RepositoryIds:         uuidStrings(constraints.RepositoryIDs),
		ServiceKeys:           trimmedStrings(constraints.ServiceKeys),
		RuntimeProfile:        strings.TrimSpace(constraints.RuntimeProfile),
		PreferredFleetScopeId: optionalUUIDString(constraints.PreferredFleetScopeID),
		RequiredCapabilities:  trimmedStrings(constraints.RequiredCapabilities),
		MetadataJson:          defaultJSON(constraints.MetadataJSON),
	}
}

func commandMeta(meta value.CommandMeta) *runtimev1.CommandMeta {
	commandID := meta.CommandID.String()
	return &runtimev1.CommandMeta{
		CommandId: &commandID,
		Actor: &runtimev1.Actor{
			Type: strings.TrimSpace(meta.Actor.Type),
			Id:   strings.TrimSpace(meta.Actor.ID),
		},
		RequestContext: &runtimev1.RequestContext{Source: callerID},
	}
}

func prepareRuntimeResult(input agentservice.RuntimePreparationInput, response *runtimev1.PrepareRuntimeResponse) (agentservice.RuntimePreparationResult, error) {
	if response == nil {
		return agentservice.RuntimePreparationResult{}, agentservice.NewRuntimePreparationError(true, "dependency_unavailable", "runtime-manager returned an empty response")
	}
	slot := response.GetSlot()
	materialization := response.GetWorkspaceMaterialization()
	runtimeContext := response.GetRuntimeContext()
	slotRef := strings.TrimSpace(runtimeContext.GetSlotId())
	if slotRef == "" {
		slotRef = strings.TrimSpace(slot.GetSlotId())
	}
	workspaceRef := strings.TrimSpace(materialization.GetWorkspaceMaterializationId())
	if slotRef == "" || workspaceRef == "" {
		return agentservice.RuntimePreparationResult{}, agentservice.NewRuntimePreparationError(true, "dependency_unavailable", "runtime-manager returned incomplete runtime refs")
	}
	fingerprint := firstNonEmpty(
		runtimeContext.GetMaterializationFingerprint(),
		materialization.GetFingerprint(),
		materialization.GetPolicyDigest(),
		slot.GetFingerprint(),
		input.WorkspacePolicy.PolicyDigest,
	)
	return agentservice.RuntimePreparationResult{
		SlotRef:                    slotRef,
		WorkspaceRef:               workspaceRef,
		ContextRef:                 fingerprint,
		MaterializationFingerprint: fingerprint,
		DiagnosticSummary:          runtimeDiagnosticSummary(slot, materialization),
	}, nil
}

func runtimeDiagnosticSummary(slot *runtimev1.Slot, materialization *runtimev1.WorkspaceMaterialization) string {
	parts := []string{}
	if slot != nil && slot.GetStatus() != runtimev1.SlotStatus_SLOT_STATUS_UNSPECIFIED {
		parts = append(parts, "slot_status="+slot.GetStatus().String())
	}
	if materialization != nil && materialization.GetStatus() != runtimev1.WorkspaceMaterializationStatus_WORKSPACE_MATERIALIZATION_STATUS_UNSPECIFIED {
		parts = append(parts, "workspace_status="+materialization.GetStatus().String())
	}
	return strings.Join(parts, ";")
}

func runtimeMode(mode string) runtimev1.RuntimeMode {
	switch strings.TrimSpace(mode) {
	case "code_only":
		return runtimev1.RuntimeMode_RUNTIME_MODE_CODE_ONLY
	case agentservice.RuntimeModeFullEnv:
		return runtimev1.RuntimeMode_RUNTIME_MODE_FULL_ENV
	case "read_only_production":
		return runtimev1.RuntimeMode_RUNTIME_MODE_READ_ONLY_PRODUCTION
	default:
		return runtimev1.RuntimeMode_RUNTIME_MODE_FULL_ENV
	}
}

func workspaceSourceKind(kind string) runtimev1.WorkspaceSourceKind {
	switch strings.TrimSpace(kind) {
	case agentservice.WorkspaceSourceKindCode:
		return runtimev1.WorkspaceSourceKind_WORKSPACE_SOURCE_KIND_CODE
	case agentservice.WorkspaceSourceKindDocumentation:
		return runtimev1.WorkspaceSourceKind_WORKSPACE_SOURCE_KIND_DOCUMENTATION
	case agentservice.WorkspaceSourceKindGuidancePackage:
		return runtimev1.WorkspaceSourceKind_WORKSPACE_SOURCE_KIND_GUIDANCE_PACKAGE
	case agentservice.WorkspaceSourceKindGeneratedContext:
		return runtimev1.WorkspaceSourceKind_WORKSPACE_SOURCE_KIND_GENERATED_CONTEXT
	default:
		return runtimev1.WorkspaceSourceKind_WORKSPACE_SOURCE_KIND_UNSPECIFIED
	}
}

func workspaceSourceAccessMode(mode string) runtimev1.WorkspaceSourceAccessMode {
	switch strings.TrimSpace(mode) {
	case agentservice.WorkspaceSourceAccessWrite:
		return runtimev1.WorkspaceSourceAccessMode_WORKSPACE_SOURCE_ACCESS_MODE_WRITE
	default:
		return runtimev1.WorkspaceSourceAccessMode_WORKSPACE_SOURCE_ACCESS_MODE_READ
	}
}

func mapRuntimeError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) {
		return err
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return agentservice.NewRuntimePreparationError(true, "deadline_exceeded", "runtime-manager did not finish workspace preparation request in time")
	}
	switch status.Code(err) {
	case codes.InvalidArgument:
		return agentservice.NewRuntimePreparationError(false, "invalid_argument", "runtime-manager rejected the workspace preparation request")
	case codes.NotFound:
		return agentservice.NewRuntimePreparationError(false, "not_found", "runtime-manager could not find required preparation state")
	case codes.PermissionDenied, codes.Unauthenticated:
		return agentservice.NewRuntimePreparationError(false, "permission_denied", "runtime-manager rejected the preparation caller")
	case codes.FailedPrecondition:
		return agentservice.NewRuntimePreparationError(false, "failed_precondition", "runtime-manager rejected workspace preparation preconditions")
	case codes.Aborted:
		return agentservice.NewRuntimePreparationError(true, "conflict", "runtime-manager reported a retryable preparation conflict")
	case codes.Unavailable, codes.DeadlineExceeded, codes.ResourceExhausted:
		return agentservice.NewRuntimePreparationError(true, "dependency_unavailable", "runtime-manager is temporarily unavailable")
	default:
		return agentservice.NewRuntimePreparationError(true, "runtime_prepare_failed", "runtime-manager workspace preparation failed")
	}
}

func optionalString(text string) *string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func optionalUUIDString(id *uuid.UUID) *string {
	if id == nil || *id == uuid.Nil {
		return nil
	}
	value := strings.TrimSpace(id.String())
	if value == "" {
		return nil
	}
	return &value
}

func uuidStrings(ids []uuid.UUID) []string {
	result := make([]string, 0, len(ids))
	for _, id := range ids {
		if id != uuid.Nil {
			result = append(result, id.String())
		}
	}
	return result
}

func trimmedStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func defaultJSON(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return "{}"
	}
	return trimmed
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}
