package service

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/enum"
)

const (
	agentRunExecutionSpecKey             = "agent_run_execution_spec"
	agentRunExecutionSpecRequiredCode    = "agent_run_execution_spec_required"
	agentRunExecutionSpecRequiredMessage = "agent_run execution spec is required before Kubernetes execution"
	agentRunExecutionSpecRequiredAction  = "provide_agent_run_execution_spec"
	maxAgentRunSafeRefBytes              = 512
	maxAgentRunAllowedSecretRefs         = 16
	maxAgentRunReportingTargetRefs       = 8
	maxAgentRunExecutionCallbackRefs     = 8
	maxAgentRunExecutionOutputRefs       = 8
	maxAgentRunExecutionResultRefs       = 8
	maxAgentRunExecutionTimeoutSeconds   = 24 * 60 * 60
	maxAgentRunReportingTargetKindBytes  = 64
	maxAgentRunAllowedSecretPurposeBytes = 64
)

type agentRunJobInputDocument struct {
	AgentRunExecutionSpec *AgentRunExecutionSpecInput `json:"agent_run_execution_spec,omitempty"`
}

func resolveAgentRunJobInput(input CreateJobInput, jobInputJSON []byte) (CreateJobInput, []byte, error) {
	if input.JobType != enum.JobTypeAgentRun {
		return input, jobInputJSON, nil
	}
	if input.AgentRunExecutionSpec == nil {
		if input.AgentRunID == nil || *input.AgentRunID == uuid.Nil {
			return CreateJobInput{}, nil, errs.ErrInvalidArgument
		}
		if !bytes.Equal(jobInputJSON, []byte(`{}`)) {
			return CreateJobInput{}, nil, errs.ErrInvalidArgument
		}
		return input, jobInputJSON, nil
	}
	if !bytes.Equal(jobInputJSON, []byte(`{}`)) {
		return CreateJobInput{}, nil, errs.ErrInvalidArgument
	}
	spec, err := normalizeAgentRunExecutionSpec(*input.AgentRunExecutionSpec)
	if err != nil {
		return CreateJobInput{}, nil, err
	}
	if input.AgentRunID != nil && *input.AgentRunID != spec.AgentRunID {
		return CreateJobInput{}, nil, errs.ErrInvalidArgument
	}
	if input.SlotID != nil && *input.SlotID != spec.SlotID {
		return CreateJobInput{}, nil, errs.ErrInvalidArgument
	}
	input.AgentRunID = &spec.AgentRunID
	input.SlotID = &spec.SlotID
	input.AgentRunExecutionSpec = &spec
	payload, err := marshalAgentRunExecutionSpec(spec)
	if err != nil {
		return CreateJobInput{}, nil, err
	}
	return input, payload, nil
}

func normalizeAgentRunExecutionSpec(spec AgentRunExecutionSpecInput) (AgentRunExecutionSpecInput, error) {
	if spec.AgentRunID == uuid.Nil || spec.SlotID == uuid.Nil || spec.ExpectedMaterializationID == uuid.Nil {
		return AgentRunExecutionSpecInput{}, errs.ErrInvalidArgument
	}
	if spec.RunnerMode != enum.AgentRunRunnerModeCodexAgent {
		return AgentRunExecutionSpecInput{}, errs.ErrInvalidArgument
	}
	normalized := AgentRunExecutionSpecInput{
		AgentRunID:                         spec.AgentRunID,
		SlotID:                             spec.SlotID,
		ExpectedMaterializationID:          spec.ExpectedMaterializationID,
		ExpectedMaterializationFingerprint: strings.TrimSpace(spec.ExpectedMaterializationFingerprint),
		WorkspaceRef:                       strings.TrimSpace(spec.WorkspaceRef),
		WorkspaceMountRef:                  strings.TrimSpace(spec.WorkspaceMountRef),
		WorkspacePVCRef:                    strings.TrimSpace(spec.WorkspacePVCRef),
		ContextRef:                         strings.TrimSpace(spec.ContextRef),
		ContextDigest:                      strings.TrimSpace(spec.ContextDigest),
		RunnerProfileRef:                   strings.TrimSpace(spec.RunnerProfileRef),
		RunnerImageRef:                     strings.TrimSpace(spec.RunnerImageRef),
		RunnerMode:                         spec.RunnerMode,
	}
	requiredRefs := []string{
		normalized.ExpectedMaterializationFingerprint,
		normalized.WorkspaceRef,
		normalized.WorkspaceMountRef,
		normalized.WorkspacePVCRef,
		normalized.ContextRef,
		normalized.ContextDigest,
		normalized.RunnerProfileRef,
		normalized.RunnerImageRef,
	}
	for _, ref := range requiredRefs {
		if !safeAgentRunRef(ref, true) {
			return AgentRunExecutionSpecInput{}, errs.ErrInvalidArgument
		}
	}
	if len(spec.AllowedSecretRefs) > maxAgentRunAllowedSecretRefs || len(spec.ReportingTargetRefs) > maxAgentRunReportingTargetRefs {
		return AgentRunExecutionSpecInput{}, errs.ErrInvalidArgument
	}
	var err error
	normalized.AllowedSecretRefs, err = normalizeAgentRunExecutionRefs(spec.AllowedSecretRefs, maxAgentRunAllowedSecretPurposeBytes)
	if err != nil {
		return AgentRunExecutionSpecInput{}, errs.ErrInvalidArgument
	}
	normalized.ReportingTargetRefs, err = normalizeAgentRunExecutionRefs(spec.ReportingTargetRefs, maxAgentRunReportingTargetKindBytes)
	if err != nil {
		return AgentRunExecutionSpecInput{}, errs.ErrInvalidArgument
	}
	if spec.CodexSessionExecutionSpec != nil {
		codexSpec, err := normalizeCodexSessionExecutionSpec(*spec.CodexSessionExecutionSpec, normalized)
		if err != nil {
			return AgentRunExecutionSpecInput{}, err
		}
		normalized.CodexSessionExecutionSpec = &codexSpec
	}
	return normalized, nil
}

func normalizeCodexSessionExecutionSpec(
	spec CodexSessionExecutionSpecInput,
	agentRunSpec AgentRunExecutionSpecInput,
) (CodexSessionExecutionSpecInput, error) {
	normalized := CodexSessionExecutionSpecInput{
		InstructionObjectRef:    strings.TrimSpace(spec.InstructionObjectRef),
		InstructionObjectDigest: strings.TrimSpace(spec.InstructionObjectDigest),
		ResultSchemaRef:         strings.TrimSpace(spec.ResultSchemaRef),
		ResultSchemaDigest:      strings.TrimSpace(spec.ResultSchemaDigest),
		SessionSnapshotRef:      strings.TrimSpace(spec.SessionSnapshotRef),
		WorkspaceSnapshotRef:    strings.TrimSpace(spec.WorkspaceSnapshotRef),
		HookEndpointRef:         strings.TrimSpace(spec.HookEndpointRef),
		TimeoutSeconds:          spec.TimeoutSeconds,
		RunnerProfileRef:        strings.TrimSpace(spec.RunnerProfileRef),
		RunnerMode:              spec.RunnerMode,
	}
	requiredRefs := []string{
		normalized.InstructionObjectRef,
		normalized.InstructionObjectDigest,
		normalized.ResultSchemaRef,
		normalized.ResultSchemaDigest,
		normalized.HookEndpointRef,
		normalized.RunnerProfileRef,
	}
	for _, ref := range requiredRefs {
		if !safeAgentRunRef(ref, true) {
			return CodexSessionExecutionSpecInput{}, errs.ErrInvalidArgument
		}
	}
	if normalized.SessionSnapshotRef == "" && normalized.WorkspaceSnapshotRef == "" {
		return CodexSessionExecutionSpecInput{}, errs.ErrInvalidArgument
	}
	if !safeAgentRunRef(normalized.SessionSnapshotRef, false) || !safeAgentRunRef(normalized.WorkspaceSnapshotRef, false) {
		return CodexSessionExecutionSpecInput{}, errs.ErrInvalidArgument
	}
	if normalized.TimeoutSeconds <= 0 || normalized.TimeoutSeconds > maxAgentRunExecutionTimeoutSeconds {
		return CodexSessionExecutionSpecInput{}, errs.ErrInvalidArgument
	}
	if normalized.RunnerProfileRef != agentRunSpec.RunnerProfileRef || normalized.RunnerMode != enum.AgentRunRunnerModeCodexAgent {
		return CodexSessionExecutionSpecInput{}, errs.ErrInvalidArgument
	}
	if len(spec.CallbackRefs) > maxAgentRunExecutionCallbackRefs ||
		len(spec.OutputRefs) > maxAgentRunExecutionOutputRefs ||
		len(spec.ResultRefs) > maxAgentRunExecutionResultRefs ||
		len(spec.AllowedSecretRefs) > maxAgentRunAllowedSecretRefs {
		return CodexSessionExecutionSpecInput{}, errs.ErrInvalidArgument
	}
	var err error
	normalized.CallbackRefs, err = normalizeAgentRunExecutionRefs(spec.CallbackRefs, maxAgentRunReportingTargetKindBytes)
	if err != nil || len(normalized.CallbackRefs) == 0 {
		return CodexSessionExecutionSpecInput{}, errs.ErrInvalidArgument
	}
	normalized.OutputRefs, err = normalizeAgentRunExecutionRefs(spec.OutputRefs, maxAgentRunReportingTargetKindBytes)
	if err != nil || len(normalized.OutputRefs) == 0 {
		return CodexSessionExecutionSpecInput{}, errs.ErrInvalidArgument
	}
	normalized.ResultRefs, err = normalizeAgentRunExecutionRefs(spec.ResultRefs, maxAgentRunReportingTargetKindBytes)
	if err != nil || len(normalized.ResultRefs) == 0 {
		return CodexSessionExecutionSpecInput{}, errs.ErrInvalidArgument
	}
	normalized.AllowedSecretRefs, err = normalizeAgentRunExecutionRefs(spec.AllowedSecretRefs, maxAgentRunAllowedSecretPurposeBytes)
	if err != nil {
		return CodexSessionExecutionSpecInput{}, errs.ErrInvalidArgument
	}
	return normalized, nil
}

func normalizeAgentRunExecutionRefs(refs []AgentRunExecutionRefInput, kindMaxBytes int) ([]AgentRunExecutionRefInput, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	normalized := make([]AgentRunExecutionRefInput, 0, len(refs))
	for _, item := range refs {
		kind := strings.TrimSpace(item.Kind)
		ref := strings.TrimSpace(item.Ref)
		if !safeAgentRunLabel(kind, kindMaxBytes) || !safeAgentRunRef(ref, true) {
			return nil, errs.ErrInvalidArgument
		}
		normalized = append(normalized, AgentRunExecutionRefInput{Kind: kind, Ref: ref})
	}
	return normalized, nil
}

func (s *Service) validateAgentRunExecutionSpecState(ctx context.Context, spec AgentRunExecutionSpecInput, slot entity.Slot) error {
	if slot.ID != spec.SlotID {
		return errs.ErrConflict
	}
	if slot.AgentRunID == nil || *slot.AgentRunID != spec.AgentRunID {
		return errs.ErrConflict
	}
	if slot.Status != enum.SlotStatusReady || slot.Fingerprint != spec.ExpectedMaterializationFingerprint {
		return errs.ErrConflict
	}
	materialization, err := s.repository.GetWorkspaceMaterialization(ctx, spec.ExpectedMaterializationID)
	if err != nil {
		return err
	}
	if materialization.SlotID != spec.SlotID ||
		materialization.Status != enum.WorkspaceMaterializationStatusCompleted ||
		materialization.Fingerprint != spec.ExpectedMaterializationFingerprint {
		return errs.ErrConflict
	}
	return nil
}

func marshalAgentRunExecutionSpec(spec AgentRunExecutionSpecInput) ([]byte, error) {
	raw, err := json.Marshal(agentRunJobInputDocument{AgentRunExecutionSpec: &spec})
	if err != nil {
		return nil, errs.ErrInvalidArgument
	}
	return normalizedJSONObject(raw)
}

func agentRunJobInputHasExecutionSpec(payload []byte) bool {
	spec, ok := AgentRunExecutionSpecFromJobInput(payload)
	return ok && spec != nil
}

// AgentRunExecutionSpecFromJobInput extracts typed agent_run execution input from persisted job input.
func AgentRunExecutionSpecFromJobInput(payload []byte) (*AgentRunExecutionSpecInput, bool) {
	normalized, err := normalizedJSONObject(payload)
	if err != nil || bytes.Equal(normalized, []byte(`{}`)) {
		return nil, false
	}
	var document agentRunJobInputDocument
	if err := json.Unmarshal(normalized, &document); err != nil || document.AgentRunExecutionSpec == nil {
		return nil, false
	}
	spec, err := normalizeAgentRunExecutionSpec(*document.AgentRunExecutionSpec)
	if err != nil {
		return nil, false
	}
	return &spec, true
}

func safeAgentRunRef(value string, required bool) bool {
	if value == "" {
		return !required
	}
	if len(value) > maxAgentRunSafeRefBytes || !utf8.ValidString(value) || strings.ContainsAny(value, "\r\n\t") {
		return false
	}
	return !strings.ContainsAny(value, "{}")
}

func safeAgentRunLabel(value string, maxBytes int) bool {
	if value == "" || len(value) > maxBytes || !utf8.ValidString(value) || strings.ContainsAny(value, "\r\n\t {}") {
		return false
	}
	return true
}
