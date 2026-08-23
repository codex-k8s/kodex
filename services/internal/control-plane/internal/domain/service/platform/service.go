// Package platform реализует authority, lifecycle, idempotency и OCC web-first control-plane.
package platform

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	repository "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/service/artifactpolicy"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
)

type Service struct{ repository repository.Repository }

func New(repo repository.Repository) (*Service, error) {
	if repo == nil {
		return nil, errors.New("platform repository is required")
	}
	return &Service{repository: repo}, nil
}

func (service *Service) Bootstrap(ctx context.Context) error {
	return service.repository.Bootstrap(ctx)
}
func (service *Service) Ready(ctx context.Context) error { return service.repository.Ready(ctx) }

func (service *Service) ResolveProofAuthority(ctx context.Context, input repository.ProofPrincipalInput) (repository.ProofAuthority, error) {
	return service.repository.ResolveProofAuthority(ctx, input)
}

func (service *Service) AcceptWorkerGrant(ctx context.Context, input repository.WorkerGrantInput) error {
	return service.repository.AcceptWorkerGrant(ctx, input)
}

func (service *Service) NextAuthorityProofRevision(ctx context.Context) (uint64, error) {
	return service.repository.NextAuthorityProofRevision(ctx)
}

func (service *Service) principal(ctx context.Context, principal value.Principal) (value.Principal, error) {
	if err := principal.Validate(); err != nil {
		return value.Principal{}, errs.ErrUnauthorized
	}
	resolved, err := service.repository.ResolvePrincipal(ctx, principal)
	if err != nil {
		return value.Principal{}, err
	}
	return resolved, nil
}

func (service *Service) GetBootstrapState(ctx context.Context, p value.Principal) (repository.BootstrapState, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return repository.BootstrapState{}, err
	}
	return service.repository.GetBootstrapState(ctx, p)
}
func (service *Service) GetPlatformEventCursor(ctx context.Context, p value.Principal) (string, int64, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return "", 0, err
	}
	return service.repository.GetPlatformEventCursor(ctx, p)
}
func (service *Service) GetOverview(ctx context.Context, p value.Principal, projectRef string) (repository.Overview, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return repository.Overview{}, err
	}
	return service.repository.GetOverview(ctx, p, projectRef)
}
func (service *Service) ListCapabilities(ctx context.Context, p value.Principal) ([]entity.IntegrationCapability, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, err
	}
	return service.repository.ListCapabilities(ctx, p)
}
func (service *Service) ListRuntimes(ctx context.Context, p value.Principal) ([]entity.RuntimeSelection, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, err
	}
	return service.repository.ListRuntimes(ctx, p)
}
func (service *Service) Search(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.SearchResult, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, err
	}
	filter.Query = strings.TrimSpace(filter.Query)
	if len([]rune(filter.Query)) < 2 || len([]rune(filter.Query)) > 200 {
		return nil, errs.ErrInvalid
	}
	return service.repository.Search(ctx, p, filter)
}
func (service *Service) ListProjects(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.Project, string, []string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", nil, err
	}
	return service.repository.ListProjects(ctx, p, filter)
}
func (service *Service) GetProject(ctx context.Context, p value.Principal, ref string) (entity.Project, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.Project{}, err
	}
	return service.repository.GetProject(ctx, p, ref)
}
func (service *Service) ListPlatformMemberships(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.Membership, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", err
	}
	return service.repository.ListPlatformMemberships(ctx, p, filter)
}
func (service *Service) ListPlatformMembershipCandidates(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.User, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", err
	}
	return service.repository.ListPlatformMembershipCandidates(ctx, p, filter)
}
func (service *Service) ListMemberships(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.Membership, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", err
	}
	return service.repository.ListMemberships(ctx, p, filter)
}
func (service *Service) ListMembershipCandidates(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.User, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", err
	}
	return service.repository.ListMembershipCandidates(ctx, p, filter)
}
func (service *Service) ListAgents(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.Agent, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", err
	}
	return service.repository.ListAgents(ctx, p, filter)
}
func (service *Service) GetAgent(ctx context.Context, p value.Principal, ref string) (entity.Agent, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.Agent{}, err
	}
	return service.repository.GetAgent(ctx, p, ref)
}
func (service *Service) ListWorkflows(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.Workflow, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", err
	}
	return service.repository.ListWorkflows(ctx, p, filter)
}
func (service *Service) GetWorkflow(ctx context.Context, p value.Principal, ref string) (entity.Workflow, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.Workflow{}, err
	}
	return service.repository.GetWorkflow(ctx, p, ref)
}
func (service *Service) ListRuns(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.Run, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", err
	}
	return service.repository.ListRuns(ctx, p, filter)
}
func (service *Service) GetRun(ctx context.Context, p value.Principal, ref string) (entity.Run, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.Run{}, err
	}
	return service.repository.GetRun(ctx, p, ref)
}
func (service *Service) GetRunGraph(ctx context.Context, p value.Principal, ref string) (entity.Run, entity.RunGraph, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.Run{}, entity.RunGraph{}, err
	}
	return service.repository.GetRunGraph(ctx, p, ref)
}
func (service *Service) ListRunEvents(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.RunEvent, int64, bool, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, 0, false, err
	}
	return service.repository.ListRunEvents(ctx, p, filter)
}
func (service *Service) ListOwnerGates(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.OwnerGate, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", err
	}
	return service.repository.ListOwnerGates(ctx, p, filter)
}
func (service *Service) GetOwnerGate(ctx context.Context, p value.Principal, ref string) (entity.OwnerGate, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.OwnerGate{}, err
	}
	return service.repository.GetOwnerGate(ctx, p, ref)
}
func (service *Service) ListArtifacts(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.Artifact, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", err
	}
	return service.repository.ListArtifacts(ctx, p, filter)
}
func (service *Service) GetArtifact(ctx context.Context, p value.Principal, ref string) (entity.Artifact, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.Artifact{}, err
	}
	return service.repository.GetArtifact(ctx, p, ref)
}
func (service *Service) UploadArtifact(ctx context.Context, p value.Principal, mutation value.Mutation, input repository.ArtifactUpload) (entity.Artifact, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.Artifact{}, err
	}
	if input.SizeBytes < 0 || input.SizeBytes > repository.MaximumArtifactBytes || input.Reader == nil || strings.TrimSpace(input.ProjectRef) == "" {
		return entity.Artifact{}, errs.ErrInvalid
	}
	body, err := io.ReadAll(io.LimitReader(input.Reader, repository.MaximumArtifactBytes+1))
	if err != nil || int64(len(body)) != input.SizeBytes {
		return entity.Artifact{}, errs.ErrInvalid
	}
	contentDigest := sha256.Sum256(body)
	input.Digest = "sha256:" + hex.EncodeToString(contentDigest[:])
	verdict := artifactpolicy.Inspect(input.FileName, input.MediaType, body)
	input.MediaType = verdict.MediaType
	input.ScanState = verdict.ScanState
	input.PreviewState = verdict.PreviewState
	input.Reader = bytes.NewReader(body)
	mutation.Operation = "artifact.upload"
	mutation.IntentDigest = digest(struct {
		ProjectRef, RunRef, FileName, MediaType, Digest string
		SizeBytes                                       int64
	}{input.ProjectRef, input.RunRef, input.FileName, input.MediaType, input.Digest, input.SizeBytes})
	if err := mutation.Validate(); err != nil {
		return entity.Artifact{}, errs.ErrInvalid
	}
	return service.repository.UploadArtifact(ctx, p, mutation, input)
}
func (service *Service) DownloadArtifact(ctx context.Context, p value.Principal, ref, purpose string) (repository.ArtifactDownload, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return repository.ArtifactDownload{}, err
	}
	if purpose != "DOWNLOAD" && purpose != "PREVIEW" {
		return repository.ArtifactDownload{}, errs.ErrInvalid
	}
	return service.repository.DownloadArtifact(ctx, p, ref, purpose)
}
func (service *Service) ReadExecutionArtifact(ctx context.Context, p value.Principal, leaseRef, fence string, generation int64, artifactRef string) (repository.ArtifactDownload, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return repository.ArtifactDownload{}, err
	}
	if p.CallerWorkload != "runtime-controller" || p.Permission != "platform.runtime.execution.artifact.read" ||
		strings.TrimSpace(leaseRef) == "" || strings.TrimSpace(fence) == "" || generation < 1 || strings.TrimSpace(artifactRef) == "" {
		return repository.ArtifactDownload{}, errs.ErrForbidden
	}
	return service.repository.ReadExecutionArtifact(ctx, p, leaseRef, fence, generation, artifactRef)
}
func (service *Service) ListSchedules(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.Schedule, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", err
	}
	return service.repository.ListSchedules(ctx, p, filter)
}
func (service *Service) ListIntegrationDefinitions(ctx context.Context, p value.Principal, category string) ([]entity.IntegrationDefinition, []string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, nil, err
	}
	return service.repository.ListIntegrationDefinitions(ctx, p, category)
}
func (service *Service) ListIntegrationConnections(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.IntegrationConnection, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", err
	}
	return service.repository.ListIntegrationConnections(ctx, p, filter)
}
func (service *Service) GetIntegrationConnection(ctx context.Context, p value.Principal, ref string) (entity.IntegrationConnection, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.IntegrationConnection{}, err
	}
	return service.repository.GetIntegrationConnection(ctx, p, ref)
}
func (service *Service) GetSystemAssistant(ctx context.Context, p value.Principal) (entity.SystemAssistant, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.SystemAssistant{}, err
	}
	return service.repository.GetSystemAssistant(ctx, p)
}
func (service *Service) ListAssistantConversations(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.AssistantConversation, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", err
	}
	return service.repository.ListAssistantConversations(ctx, p, filter)
}
func (service *Service) GetAdministration(ctx context.Context, p value.Principal) (repository.Administration, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return repository.Administration{}, err
	}
	return service.repository.GetAdministration(ctx, p)
}
func (service *Service) ListAuditEvents(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.AuditEvent, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", err
	}
	return service.repository.ListAuditEvents(ctx, p, filter)
}

func (service *Service) Execute(ctx context.Context, input command.Command) (command.Result, error) {
	principal, err := service.principal(ctx, input.Principal)
	if err != nil {
		return command.Result{}, err
	}
	input.Principal = principal
	if !knownCommand(input.Kind) || input.Payload == nil {
		return command.Result{}, errs.ErrInvalid
	}
	input.Mutation.Operation = "controlplane." + strings.ToLower(string(input.Kind))
	input.Mutation.IntentDigest = digest(struct {
		Kind    command.Kind
		Payload any
	}{input.Kind, input.Payload})
	if err := input.Mutation.Validate(); err != nil {
		return command.Result{}, errs.ErrInvalid
	}
	if input.Kind == command.SetAgentEnabled {
		payload, ok := input.Payload.(command.AgentInput)
		if !ok || payload.Ref == "system-assistant" && !payload.Enabled {
			return command.Result{}, errs.ErrProtected
		}
	}
	if input.Kind == command.ArchiveAgent {
		payload, ok := input.Payload.(command.AgentInput)
		if !ok || payload.Ref == "system-assistant" {
			return command.Result{}, errs.ErrProtected
		}
	}
	return service.repository.Execute(ctx, input)
}

func (service *Service) ReconcileWarmRuntime(ctx context.Context, p value.Principal, instance string) (entity.SystemAssistant, map[string]any, bool, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.SystemAssistant{}, nil, false, err
	}
	if strings.TrimSpace(instance) == "" {
		return entity.SystemAssistant{}, nil, false, errs.ErrInvalid
	}
	return service.repository.ReconcileWarmRuntime(ctx, p, instance)
}
func (service *Service) ClaimDueSchedules(ctx context.Context, p value.Principal, instance string, limit int32) ([]map[string]any, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(instance) == "" || limit < 1 || limit > 128 {
		return nil, errs.ErrInvalid
	}
	return service.repository.ClaimDueSchedules(ctx, p, instance, limit)
}
func (service *Service) ClaimIntegrationConnectionTests(ctx context.Context, p value.Principal, instance string, limit int32) ([]map[string]any, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(instance) == "" || limit < 1 || limit > 32 {
		return nil, errs.ErrInvalid
	}
	return service.repository.ClaimIntegrationConnectionTests(ctx, p, instance, limit)
}
func (service *Service) ResolveIntegrationInvocation(ctx context.Context, p value.Principal, input map[string]string, boundedInput map[string]any) (map[string]any, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, err
	}
	for _, key := range []string{"run_ref", "node_ref", "connection_ref", "capability_key", "idempotency_key"} {
		if strings.TrimSpace(input[key]) == "" {
			return nil, errs.ErrInvalid
		}
	}
	return service.repository.ResolveIntegrationInvocation(ctx, p, input, boundedInput)
}
func (service *Service) ClaimIntegrationInvocations(ctx context.Context, p value.Principal, instance string, limit int32) ([]map[string]any, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(instance) == "" || limit < 1 || limit > 32 {
		return nil, errs.ErrInvalid
	}
	return service.repository.ClaimIntegrationInvocations(ctx, p, instance, limit)
}
func (service *Service) GetIntegrationInvocation(ctx context.Context, p value.Principal, ref string) (map[string]any, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(ref) == "" {
		return nil, errs.ErrInvalid
	}
	return service.repository.GetIntegrationInvocation(ctx, p, ref)
}
func (service *Service) ListInteractionSources(ctx context.Context, p value.Principal) ([]map[string]any, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, err
	}
	return service.repository.ListInteractionSources(ctx, p)
}
func (service *Service) ClaimInteractionDeliveries(ctx context.Context, p value.Principal, instance string, limit int32) ([]map[string]any, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(instance) == "" || limit < 1 || limit > 32 {
		return nil, errs.ErrInvalid
	}
	return service.repository.ClaimInteractionDeliveries(ctx, p, instance, limit)
}

func knownCommand(kind command.Kind) bool {
	switch kind {
	case command.CompleteOnboarding, command.CreateProject, command.UpdateProject,
		command.AddPlatformMembership, command.ChangePlatformMembership, command.RemovePlatformMembership,
		command.AddMembership, command.ChangeMembership, command.RemoveMembership,
		command.CreateAgent, command.UpdateAgent, command.SetAgentEnabled, command.ArchiveAgent,
		command.CreateInstructions, command.ValidateInstructions, command.PublishInstructions,
		command.RollbackInstructions, command.ChangeAgentCapability, command.ChangeAgentGrant,
		command.CreateWorkflow, command.UpdateWorkflow,
		command.ValidateWorkflow, command.PublishWorkflow, command.ArchiveWorkflow,
		command.LaunchRun, command.AddSessionTurn, command.CancelRun, command.RetryRun,
		command.ResolveOwnerGate, command.ChangeArtifactBinding, command.CreateSchedule,
		command.UpdateSchedule, command.SetScheduleEnabled, command.CreateConnection,
		command.TestConnection, command.SetConnectionEnabled, command.ChangeIntegrationGrant,
		command.CreateAssistantConversation, command.AddAssistantTurn, command.ApplyAssistantPlan,
		command.UpdateAssistantInstructions, command.RecoverAssistant, command.ClaimExecution,
		command.RenewExecution, command.ReportExecutionProgress, command.CompleteExecution,
		command.DelegateExecution, command.ProposeAssistantPlan, command.DeliverCallback, command.ReportWarmRuntime,
		command.MaterializeOccurrence, command.CompleteOccurrence, command.CompleteConnectionTest,
		command.CompleteIntegrationInvocation, command.CompleteInteractionDelivery,
		command.AcceptInteractionMessage:
		return true
	default:
		return false
	}
}

func digest(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		encoded = []byte(fmt.Sprintf("unsupported:%T", value))
	}
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:])
}
