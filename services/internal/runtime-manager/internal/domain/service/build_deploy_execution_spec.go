package service

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/enum"
)

const (
	buildExecutionSpecKey              = "build_execution_spec"
	deployExecutionSpecKey             = "deploy_execution_spec"
	buildExecutionSpecRequiredCode     = "build_execution_spec_required"
	buildExecutionSpecRequiredMessage  = "build execution spec is required before build execution"
	buildExecutionSpecRequiredAction   = "provide_build_execution_spec"
	deployExecutionSpecRequiredCode    = "deploy_execution_spec_required"
	deployExecutionSpecRequiredMessage = "deploy execution spec is required before deploy execution"
	deployExecutionSpecRequiredAction  = "provide_deploy_execution_spec"
	deployExecutorUnavailableCode      = "deploy_executor_unavailable"
	deployExecutorUnavailableMessage   = "deploy execution spec is accepted but rollout executor is not available"
	deployExecutorUnavailableAction    = "wait_for_deploy_executor_contract"
	maxRuntimeJobServiceKeyBytes       = 128
	maxRuntimeJobSecretPurposeBytes    = 64
	maxRuntimeJobOutputKindBytes       = 64
	maxRuntimeJobAllowedSecretRefs     = 16
	maxRuntimeJobOutputRefs            = 16
	maxRuntimeJobRolloutTargets        = 16
	maxRuntimeJobExpectedImageRefs     = 32
)

type buildDeployJobInputDocument struct {
	BuildExecutionSpec  *BuildExecutionSpecInput  `json:"build_execution_spec,omitempty"`
	DeployExecutionSpec *DeployExecutionSpecInput `json:"deploy_execution_spec,omitempty"`
}

func resolveBuildDeployJobInput(input CreateJobInput, jobInputJSON []byte) (CreateJobInput, []byte, error) {
	if input.BuildExecutionSpec != nil && input.JobType != enum.JobTypeBuild {
		return CreateJobInput{}, nil, errs.ErrInvalidArgument
	}
	if input.DeployExecutionSpec != nil && input.JobType != enum.JobTypeDeploy {
		return CreateJobInput{}, nil, errs.ErrInvalidArgument
	}
	switch input.JobType {
	case enum.JobTypeBuild:
		return resolveTypedJobInput(input, jobInputJSON, input.BuildExecutionSpec, normalizeBuildExecutionSpec, marshalBuildExecutionSpec, func(input CreateJobInput, spec BuildExecutionSpecInput) CreateJobInput {
			input.BuildExecutionSpec = &spec
			return input
		})
	case enum.JobTypeDeploy:
		return resolveTypedJobInput(input, jobInputJSON, input.DeployExecutionSpec, normalizeDeployExecutionSpec, marshalDeployExecutionSpec, func(input CreateJobInput, spec DeployExecutionSpecInput) CreateJobInput {
			input.DeployExecutionSpec = &spec
			return input
		})
	default:
		return input, jobInputJSON, nil
	}
}

func resolveTypedJobInput[Spec any](
	input CreateJobInput,
	jobInputJSON []byte,
	rawSpec *Spec,
	normalize func(Spec) (Spec, error),
	marshal func(Spec) ([]byte, error),
	assign func(CreateJobInput, Spec) CreateJobInput,
) (CreateJobInput, []byte, error) {
	if rawSpec == nil {
		if !bytes.Equal(jobInputJSON, []byte(`{}`)) {
			return CreateJobInput{}, nil, errs.ErrInvalidArgument
		}
		return input, jobInputJSON, nil
	}
	if !bytes.Equal(jobInputJSON, []byte(`{}`)) {
		return CreateJobInput{}, nil, errs.ErrInvalidArgument
	}
	spec, err := normalize(*rawSpec)
	if err != nil {
		return CreateJobInput{}, nil, err
	}
	payload, err := marshal(spec)
	if err != nil {
		return CreateJobInput{}, nil, err
	}
	return assign(input, spec), payload, nil
}

func normalizeBuildExecutionSpec(spec BuildExecutionSpecInput) (BuildExecutionSpecInput, error) {
	normalized := BuildExecutionSpecInput{
		SourceRef:            strings.TrimSpace(spec.SourceRef),
		SourceCommitSHA:      strings.TrimSpace(strings.ToLower(spec.SourceCommitSHA)),
		ServiceKey:           strings.TrimSpace(spec.ServiceKey),
		ImageRef:             strings.TrimSpace(spec.ImageRef),
		ImageTag:             strings.TrimSpace(spec.ImageTag),
		ImageDigest:          strings.TrimSpace(strings.ToLower(spec.ImageDigest)),
		BuildContextRef:      strings.TrimSpace(spec.BuildContextRef),
		BuildContextDigest:   strings.TrimSpace(strings.ToLower(spec.BuildContextDigest)),
		DockerfileRef:        strings.TrimSpace(spec.DockerfileRef),
		DockerfileDigest:     strings.TrimSpace(strings.ToLower(spec.DockerfileDigest)),
		DockerfileTarget:     strings.TrimSpace(spec.DockerfileTarget),
		BuilderImageRef:      strings.TrimSpace(spec.BuilderImageRef),
		BuildPlanFingerprint: strings.TrimSpace(strings.ToLower(spec.BuildPlanFingerprint)),
	}
	if !safeAgentRunRef(normalized.SourceRef, true) ||
		!validRuntimeJobCommitSHA(normalized.SourceCommitSHA) ||
		!safeAgentRunLabel(normalized.ServiceKey, maxRuntimeJobServiceKeyBytes) ||
		!safeAgentRunRef(normalized.ImageRef, true) ||
		!safeAgentRunLabel(normalized.ImageTag, maxRuntimeJobServiceKeyBytes) ||
		!safeAgentRunRef(normalized.BuildContextRef, true) ||
		!validAgentRunSHA256Digest(normalized.BuildContextDigest) ||
		!safeAgentRunRef(normalized.DockerfileRef, true) ||
		!safeAgentRunLabel(normalized.DockerfileTarget, maxRuntimeJobServiceKeyBytes) ||
		!safeAgentRunRef(normalized.BuilderImageRef, true) ||
		!validAgentRunSHA256Digest(normalized.BuildPlanFingerprint) {
		return BuildExecutionSpecInput{}, errs.ErrInvalidArgument
	}
	if normalized.ImageDigest != "" && !validAgentRunSHA256Digest(normalized.ImageDigest) {
		return BuildExecutionSpecInput{}, errs.ErrInvalidArgument
	}
	if normalized.DockerfileDigest != "" && !validAgentRunSHA256Digest(normalized.DockerfileDigest) {
		return BuildExecutionSpecInput{}, errs.ErrInvalidArgument
	}
	allowedSecretRefs, outputRefs, err := normalizeRuntimeJobExecutionRefs(spec.AllowedSecretRefs, spec.OutputRefs)
	if err != nil {
		return BuildExecutionSpecInput{}, err
	}
	normalized.AllowedSecretRefs = allowedSecretRefs
	normalized.OutputRefs = outputRefs
	return normalized, nil
}

func normalizeDeployExecutionSpec(spec DeployExecutionSpecInput) (DeployExecutionSpecInput, error) {
	normalized := DeployExecutionSpecInput{
		SourceRef:             strings.TrimSpace(spec.SourceRef),
		SourceCommitSHA:       strings.TrimSpace(strings.ToLower(spec.SourceCommitSHA)),
		ServiceKey:            strings.TrimSpace(spec.ServiceKey),
		ImageRef:              strings.TrimSpace(spec.ImageRef),
		ImageTag:              strings.TrimSpace(spec.ImageTag),
		ImageDigest:           strings.TrimSpace(strings.ToLower(spec.ImageDigest)),
		ManifestRef:           strings.TrimSpace(spec.ManifestRef),
		ManifestDigest:        strings.TrimSpace(strings.ToLower(spec.ManifestDigest)),
		KustomizationRef:      strings.TrimSpace(spec.KustomizationRef),
		KustomizationDigest:   strings.TrimSpace(strings.ToLower(spec.KustomizationDigest)),
		TargetNamespace:       strings.TrimSpace(spec.TargetNamespace),
		TargetClusterRef:      strings.TrimSpace(spec.TargetClusterRef),
		TargetSlotID:          strings.TrimSpace(spec.TargetSlotID),
		DeployPlanFingerprint: strings.TrimSpace(strings.ToLower(spec.DeployPlanFingerprint)),
		ManifestBundleRef:     strings.TrimSpace(spec.ManifestBundleRef),
		ManifestBundleDigest:  strings.TrimSpace(strings.ToLower(spec.ManifestBundleDigest)),
	}
	if !safeAgentRunRef(normalized.SourceRef, true) ||
		!validRuntimeJobCommitSHA(normalized.SourceCommitSHA) ||
		!safeAgentRunLabel(normalized.ServiceKey, maxRuntimeJobServiceKeyBytes) ||
		!safeAgentRunRef(normalized.ImageRef, true) ||
		!safeAgentRunLabel(normalized.ImageTag, maxRuntimeJobServiceKeyBytes) ||
		!safeAgentRunRef(normalized.ManifestRef, true) ||
		!validAgentRunSHA256Digest(normalized.ManifestDigest) ||
		!safeAgentRunRef(normalized.KustomizationRef, true) ||
		!validAgentRunSHA256Digest(normalized.KustomizationDigest) ||
		!safeRuntimeJobNamespace(normalized.TargetNamespace) ||
		!safeAgentRunRef(normalized.TargetClusterRef, true) ||
		!safeAgentRunRef(normalized.ManifestBundleRef, true) ||
		!validAgentRunSHA256Digest(normalized.ManifestBundleDigest) ||
		!validAgentRunSHA256Digest(normalized.DeployPlanFingerprint) {
		return DeployExecutionSpecInput{}, errs.ErrInvalidArgument
	}
	if normalized.ImageDigest != "" && !validAgentRunSHA256Digest(normalized.ImageDigest) {
		return DeployExecutionSpecInput{}, errs.ErrInvalidArgument
	}
	if !safeAgentRunRef(normalized.TargetSlotID, false) {
		return DeployExecutionSpecInput{}, errs.ErrInvalidArgument
	}
	rolloutTargets, err := normalizeDeployRolloutTargets(spec.RolloutTargets)
	if err != nil {
		return DeployExecutionSpecInput{}, err
	}
	expectedImageRefs, err := normalizeDeployExpectedImageRefs(spec.ExpectedImageRefs)
	if err != nil {
		return DeployExecutionSpecInput{}, err
	}
	allowedSecretRefs, outputRefs, err := normalizeRuntimeJobExecutionRefs(spec.AllowedSecretRefs, spec.OutputRefs)
	if err != nil {
		return DeployExecutionSpecInput{}, err
	}
	normalized.AllowedSecretRefs = allowedSecretRefs
	normalized.OutputRefs = outputRefs
	normalized.RolloutTargets = rolloutTargets
	normalized.ExpectedImageRefs = expectedImageRefs
	return normalized, nil
}

func normalizeDeployRolloutTargets(targets []DeployRolloutTargetInput) ([]DeployRolloutTargetInput, error) {
	if len(targets) == 0 || len(targets) > maxRuntimeJobRolloutTargets {
		return nil, errs.ErrInvalidArgument
	}
	normalized := make([]DeployRolloutTargetInput, 0, len(targets))
	for _, target := range targets {
		item := DeployRolloutTargetInput{
			Kind:      strings.TrimSpace(target.Kind),
			Ref:       strings.TrimSpace(target.Ref),
			Namespace: strings.TrimSpace(target.Namespace),
			Name:      strings.TrimSpace(target.Name),
			Digest:    strings.TrimSpace(strings.ToLower(target.Digest)),
		}
		if !safeAgentRunLabel(item.Kind, maxRuntimeJobOutputKindBytes) ||
			!safeAgentRunRef(item.Ref, true) ||
			!safeRuntimeJobNamespace(item.Namespace) ||
			!safeAgentRunLabel(item.Name, maxRuntimeJobServiceKeyBytes) {
			return nil, errs.ErrInvalidArgument
		}
		if item.Digest != "" && !validAgentRunSHA256Digest(item.Digest) {
			return nil, errs.ErrInvalidArgument
		}
		normalized = append(normalized, item)
	}
	return normalized, nil
}

func normalizeDeployExpectedImageRefs(refs []DeployExpectedImageRefInput) ([]DeployExpectedImageRefInput, error) {
	if len(refs) == 0 || len(refs) > maxRuntimeJobExpectedImageRefs {
		return nil, errs.ErrInvalidArgument
	}
	normalized := make([]DeployExpectedImageRefInput, 0, len(refs))
	for _, ref := range refs {
		item := DeployExpectedImageRefInput{
			ContainerName: strings.TrimSpace(ref.ContainerName),
			ImageRef:      strings.TrimSpace(ref.ImageRef),
			ImageDigest:   strings.TrimSpace(strings.ToLower(ref.ImageDigest)),
		}
		if !safeAgentRunLabel(item.ContainerName, maxRuntimeJobServiceKeyBytes) ||
			!safeAgentRunRef(item.ImageRef, true) {
			return nil, errs.ErrInvalidArgument
		}
		if item.ImageDigest != "" && !validAgentRunSHA256Digest(item.ImageDigest) {
			return nil, errs.ErrInvalidArgument
		}
		normalized = append(normalized, item)
	}
	return normalized, nil
}

func normalizeRuntimeJobExecutionRefs(
	allowedSecretRefs []RuntimeJobExecutionRefInput,
	outputRefs []RuntimeJobExecutionRefInput,
) ([]RuntimeJobExecutionRefInput, []RuntimeJobExecutionRefInput, error) {
	if len(allowedSecretRefs) > maxRuntimeJobAllowedSecretRefs || len(outputRefs) > maxRuntimeJobOutputRefs {
		return nil, nil, errs.ErrInvalidArgument
	}
	normalizedSecrets, err := normalizeAgentRunExecutionRefs(allowedSecretRefs, maxRuntimeJobSecretPurposeBytes)
	if err != nil {
		return nil, nil, err
	}
	normalizedOutputs, err := normalizeAgentRunExecutionRefs(outputRefs, maxRuntimeJobOutputKindBytes)
	if err != nil {
		return nil, nil, err
	}
	return normalizedSecrets, normalizedOutputs, nil
}

func marshalBuildExecutionSpec(spec BuildExecutionSpecInput) ([]byte, error) {
	return marshalBuildDeployExecutionSpec(buildDeployJobInputDocument{BuildExecutionSpec: &spec})
}

func marshalDeployExecutionSpec(spec DeployExecutionSpecInput) ([]byte, error) {
	return marshalBuildDeployExecutionSpec(buildDeployJobInputDocument{DeployExecutionSpec: &spec})
}

func marshalBuildDeployExecutionSpec(document buildDeployJobInputDocument) ([]byte, error) {
	raw, err := json.Marshal(document)
	if err != nil {
		return nil, errs.ErrInvalidArgument
	}
	return normalizedJSONObject(raw)
}

func buildJobInputHasExecutionSpec(payload []byte) bool {
	spec, ok := BuildExecutionSpecFromJobInput(payload)
	return ok && spec != nil
}

func deployJobInputHasExecutionSpec(payload []byte) bool {
	spec, ok := DeployExecutionSpecFromJobInput(payload)
	return ok && spec != nil
}

// BuildExecutionSpecFromJobInput extracts typed build execution input from persisted job input.
func BuildExecutionSpecFromJobInput(payload []byte) (*BuildExecutionSpecInput, bool) {
	return typedBuildDeploySpecFromJobInput(payload, func(document buildDeployJobInputDocument) *BuildExecutionSpecInput {
		return document.BuildExecutionSpec
	}, normalizeBuildExecutionSpec)
}

// DeployExecutionSpecFromJobInput extracts typed deploy execution input from persisted job input.
func DeployExecutionSpecFromJobInput(payload []byte) (*DeployExecutionSpecInput, bool) {
	return typedBuildDeploySpecFromJobInput(payload, func(document buildDeployJobInputDocument) *DeployExecutionSpecInput {
		return document.DeployExecutionSpec
	}, normalizeDeployExecutionSpec)
}

func typedBuildDeploySpecFromJobInput[Spec any](
	payload []byte,
	selectSpec func(buildDeployJobInputDocument) *Spec,
	normalize func(Spec) (Spec, error),
) (*Spec, bool) {
	normalized, err := normalizedJSONObject(payload)
	if err != nil || bytes.Equal(normalized, []byte(`{}`)) {
		return nil, false
	}
	var document buildDeployJobInputDocument
	if err := json.Unmarshal(normalized, &document); err != nil {
		return nil, false
	}
	rawSpec := selectSpec(document)
	if rawSpec == nil {
		return nil, false
	}
	spec, err := normalize(*rawSpec)
	if err != nil {
		return nil, false
	}
	return &spec, true
}

func buildDeployJobInputHasRequiredExecutionSpec(jobType enum.JobType, payload []byte) bool {
	switch jobType {
	case enum.JobTypeBuild:
		return buildJobInputHasExecutionSpec(payload)
	case enum.JobTypeDeploy:
		return deployJobInputHasExecutionSpec(payload)
	case enum.JobTypeAgentRun:
		return agentRunJobInputHasExecutionSpec(payload)
	default:
		return true
	}
}

func safeRuntimeJobNamespace(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || len(trimmed) > 63 {
		return false
	}
	if trimmed[0] == '-' || trimmed[len(trimmed)-1] == '-' {
		return false
	}
	for _, char := range trimmed {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || char == '-' {
			continue
		}
		return false
	}
	return true
}

func validRuntimeJobCommitSHA(value string) bool {
	trimmed := strings.TrimSpace(strings.ToLower(value))
	if len(trimmed) != 40 && len(trimmed) != 64 {
		return false
	}
	for _, char := range trimmed {
		if (char >= '0' && char <= '9') || (char >= 'a' && char <= 'f') {
			continue
		}
		return false
	}
	return true
}
