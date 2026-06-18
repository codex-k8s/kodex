package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/value"
)

const selfDeployDeployPlanVersion = int64(1)

var errDeployPlanUnavailable = errors.New("deploy plan unavailable")

// GetSelfDeployDeployPlan возвращает checked project-owned deploy specs для self-deploy.
func (s *Service) GetSelfDeployDeployPlan(ctx context.Context, input GetSelfDeployDeployPlanInput) (SelfDeployDeployPlanResult, error) {
	if err := requireProjectID(input.ProjectID); err != nil {
		return SelfDeployDeployPlanResult{}, err
	}
	keys, err := normalizeBuildPlanServiceKeys(input.AffectedServiceKeys)
	if err != nil {
		return SelfDeployDeployPlanResult{Status: enum.SelfDeployDeployPlanStatusInvalidInput, SafeReason: "invalid_input"}, nil
	}
	if input.RepositoryID == uuid.Nil ||
		strings.TrimSpace(input.SourceRef) == "" ||
		strings.TrimSpace(input.MergeCommitSHA) == "" ||
		strings.TrimSpace(input.ExpectedServicesPolicyDigest) == "" ||
		!safeBuildPlanRef(input.SourceRef, true) ||
		!validBuildPlanCommitSHA(input.MergeCommitSHA) {
		return SelfDeployDeployPlanResult{Status: enum.SelfDeployDeployPlanStatusInvalidInput, SafeReason: "invalid_input"}, nil
	}
	if err := s.authorizeProjectQuery(ctx, input.ProjectID, input.Meta, projectActionPolicyRead, projectAggregateServicesPolicy); err != nil {
		return SelfDeployDeployPlanResult{}, err
	}

	policy, err := s.repository.GetServicesPolicy(ctx, input.ProjectID, nil)
	if err != nil {
		if errors.Is(err, errs.ErrNotFound) {
			return SelfDeployDeployPlanResult{Status: enum.SelfDeployDeployPlanStatusDeployPlanUnavailable, SafeReason: "services_policy_not_found"}, nil
		}
		return SelfDeployDeployPlanResult{}, err
	}
	plan := selfDeployDeployPlanBase(input, policy, keys)
	if !selfDeployPolicyReady(policy) {
		return SelfDeployDeployPlanResult{Status: enum.SelfDeployDeployPlanStatusDeployPlanUnavailable, Plan: plan, SafeReason: "services_policy_not_ready"}, nil
	}
	if policy.SourceRepositoryID == nil || *policy.SourceRepositoryID != input.RepositoryID || strings.TrimSpace(policy.SourcePath) != selfDeployPolicySourcePath {
		return SelfDeployDeployPlanResult{Status: enum.SelfDeployDeployPlanStatusPolicyStale, Plan: plan, SafeReason: "services_policy_source_mismatch"}, nil
	}
	if reason := selfDeployDeployPolicyStaleReason(input, policy); reason != "" {
		return SelfDeployDeployPlanResult{Status: enum.SelfDeployDeployPlanStatusPolicyStale, Plan: plan, SafeReason: reason}, nil
	}
	descriptors, _, err := s.repository.ListServiceDescriptors(ctx, query.ServiceDescriptorFilter{
		ProjectID:    input.ProjectID,
		RepositoryID: &input.RepositoryID,
		ServiceKeys:  keys,
		Statuses:     []enum.ServiceStatus{enum.ServiceStatusActive},
		Page:         value.PageRequest{PageSize: selfDeployMaxServices},
	})
	if err != nil {
		return SelfDeployDeployPlanResult{}, err
	}
	descriptorsByKey := serviceDescriptorsByKey(descriptors)
	if missing := firstMissingServiceKey(keys, descriptorsByKey); missing != "" {
		return SelfDeployDeployPlanResult{Status: enum.SelfDeployDeployPlanStatusServiceNotFound, Plan: plan, SafeReason: "service_not_found:" + missing}, nil
	}
	deploySpecs, err := deploySpecsFromCheckedPolicy(policy)
	if err != nil {
		return SelfDeployDeployPlanResult{Status: enum.SelfDeployDeployPlanStatusDeployPlanUnavailable, Plan: plan, SafeReason: "checked_deploy_specs_unavailable"}, nil
	}
	buildOutputs, reason := normalizeSelfDeployBuildOutputs(input.BuildOutputs, keys)
	if reason != "" {
		return SelfDeployDeployPlanResult{Status: enum.SelfDeployDeployPlanStatusBuildOutputInvalid, Plan: plan, SafeReason: reason}, nil
	}
	contexts, reason := normalizeSelfDeployMaterializedBuildContexts(input.MaterializedBuildContexts, keys)
	if reason != "" {
		return SelfDeployDeployPlanResult{Status: enum.SelfDeployDeployPlanStatusBuildOutputInvalid, Plan: plan, SafeReason: reason}, nil
	}
	status := enum.SelfDeployDeployPlanStatusReady
	for _, key := range keys {
		spec, ok := deploySpecs[key]
		if !ok {
			return SelfDeployDeployPlanResult{Status: enum.SelfDeployDeployPlanStatusDeployPlanUnavailable, Plan: plan, SafeReason: "service_deploy_spec_unavailable:" + key}, nil
		}
		item, itemStatus, itemReason := selfDeployDeployPlanItem(input, descriptorsByKey[key], spec, buildOutputs[key], contexts[key])
		if itemStatus != enum.SelfDeployDeployPlanStatusReady && status == enum.SelfDeployDeployPlanStatusReady {
			status = itemStatus
			reason = itemReason
		}
		plan.DeployItems = append(plan.DeployItems, item)
	}
	plan.PlanFingerprint = selfDeployDeployPlanFingerprint(plan)
	for i := range plan.DeployItems {
		if plan.DeployItems[i].Status == SelfDeployDeployPlanItemStatusReady {
			plan.DeployItems[i].DeployExecutionSpec.DeployPlanFingerprint = plan.PlanFingerprint
		}
		plan.DeployItems[i].PlanItemFingerprint = selfDeployDeployPlanItemFingerprint(plan.DeployItems[i])
	}
	if reason := selfDeployBuildPlanExpectedFingerprintMismatch(input.ExpectedDeployPlanFingerprint, plan.PlanFingerprint); reason != "" {
		return SelfDeployDeployPlanResult{Status: enum.SelfDeployDeployPlanStatusBuildOutputInvalid, Plan: plan, SafeReason: reason}, nil
	}
	if status != enum.SelfDeployDeployPlanStatusReady {
		plan.SafeSummary = "self-deploy deploy plan is waiting for successful build outputs"
		return SelfDeployDeployPlanResult{Status: status, Plan: plan, SafeReason: reason}, nil
	}
	plan.SafeSummary = "self-deploy deploy plan is ready for " + strings.Join(keys, ",")
	return SelfDeployDeployPlanResult{Status: enum.SelfDeployDeployPlanStatusReady, Plan: plan}, nil
}

func selfDeployDeployPlanBase(input GetSelfDeployDeployPlanInput, policy entity.ServicesPolicy, keys []string) SelfDeployDeployPlan {
	return SelfDeployDeployPlan{
		ProjectRef:          input.ProjectID.String(),
		RepositoryRef:       input.RepositoryID.String(),
		ProviderSignalRef:   strings.TrimSpace(input.ProviderSignalRef),
		SourceRef:           strings.TrimSpace(input.SourceRef),
		MergeCommitSHA:      strings.TrimSpace(input.MergeCommitSHA),
		ServicesYaml:        selfDeployServicesYamlProjection(policy),
		AffectedServiceKeys: keys,
		Version:             selfDeployDeployPlanVersion,
	}
}

func selfDeployDeployPolicyStaleReason(input GetSelfDeployDeployPlanInput, policy entity.ServicesPolicy) string {
	return selfDeployServicesPolicyStaleReason(
		input.SourceRef,
		input.ExpectedServicesPolicyDigest,
		input.ExpectedServicesPolicyFingerprint,
		input.ExpectedServicesPolicyVersion,
		policy,
	)
}

func selfDeployServicesPolicyStaleReason(sourceRef string, expectedDigest string, expectedFingerprint string, expectedVersion *int64, policy entity.ServicesPolicy) string {
	if !selfDeploySourceRefsEquivalent(sourceRef, policy.SourceRef) {
		return "services_policy_source_ref_mismatch"
	}
	if strings.TrimSpace(expectedDigest) != strings.TrimSpace(policy.ContentHash) {
		return "services_policy_digest_mismatch"
	}
	if expected := strings.TrimSpace(expectedFingerprint); expected != "" && expected != selfDeployServicesYamlFingerprint(policy) {
		return "services_policy_fingerprint_mismatch"
	}
	if expectedVersion != nil && *expectedVersion != policy.PolicyVersion {
		return "services_policy_version_mismatch"
	}
	return ""
}

func selfDeploySourceRefsEquivalent(left string, right string) bool {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if left == "" || right == "" {
		return false
	}
	if left == right {
		return true
	}
	leftBranch, leftOK := selfDeployBranchRefName(left)
	rightBranch, rightOK := selfDeployBranchRefName(right)
	return leftOK && rightOK && leftBranch == rightBranch
}

func selfDeployBranchRefName(ref string) (string, bool) {
	ref = strings.TrimSpace(ref)
	if ref == "" || strings.ContainsAny(ref, "\x00\r\n") {
		return "", false
	}
	if strings.HasPrefix(ref, "refs/heads/") {
		branch := strings.TrimPrefix(ref, "refs/heads/")
		return selfDeploySafeBranchName(branch)
	}
	if strings.HasPrefix(ref, "refs/") || strings.Contains(ref, "@") || strings.Contains(ref, ":") {
		return "", false
	}
	return selfDeploySafeBranchName(ref)
}

func selfDeploySafeBranchName(branch string) (string, bool) {
	branch = strings.TrimSpace(branch)
	if branch == "" ||
		validBuildPlanCommitSHA(branch) ||
		validBuildPlanSHA256Digest(branch) ||
		strings.HasPrefix(branch, "/") ||
		strings.HasSuffix(branch, "/") ||
		strings.HasSuffix(branch, ".") ||
		strings.HasSuffix(branch, ".lock") ||
		strings.Contains(branch, "//") ||
		strings.Contains(branch, "..") ||
		strings.Contains(branch, "/.") ||
		strings.Contains(branch, "@{") {
		return "", false
	}
	for _, char := range branch {
		if (char >= 'a' && char <= 'z') ||
			(char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') ||
			char == '/' ||
			char == '.' ||
			char == '_' ||
			char == '-' {
			continue
		}
		return "", false
	}
	return branch, true
}

func deploySpecsFromCheckedPolicy(policy entity.ServicesPolicy) (map[string]value.ServicesPolicyDeploySpec, error) {
	document, err := parseServicesPolicyDocument(policy.ValidatedPayload)
	if err != nil {
		return nil, err
	}
	result := map[string]value.ServicesPolicyDeploySpec{}
	for _, entry := range serviceEntries(document) {
		key := serviceKey(entry)
		if key == "" || entry.Deploy == nil {
			continue
		}
		result[key] = *entry.Deploy
	}
	if len(result) == 0 {
		return nil, errDeployPlanUnavailable
	}
	return result, nil
}

func selfDeployDeployPlanItem(
	input GetSelfDeployDeployPlanInput,
	descriptor entity.ServiceDescriptor,
	spec value.ServicesPolicyDeploySpec,
	output SelfDeployBuildOutput,
	context SelfDeployMaterializedBuildContext,
) (SelfDeployDeployPlanItem, enum.SelfDeployDeployPlanStatus, string) {
	item := SelfDeployDeployPlanItem{
		ServiceKey: descriptor.ServiceKey,
		ServiceRef: "project-catalog:service-descriptor:" + descriptor.ID.String() + ":" + descriptor.ServiceKey,
		Status:     SelfDeployDeployPlanItemStatusReady,
	}
	if strings.TrimSpace(output.ServiceKey) == "" {
		item.Status = SelfDeployDeployPlanItemStatusBuildNotReady
		item.SafeReason = "build_not_ready:" + descriptor.ServiceKey
		return item, enum.SelfDeployDeployPlanStatusBuildNotReady, item.SafeReason
	}
	executionSpec, err := selfDeployDeployExecutionSpec(input, item.ServiceKey, spec, output, context)
	if err != nil {
		item.Status = SelfDeployDeployPlanItemStatusDeployPlanUnavailable
		item.SafeReason = "deploy_plan_unavailable:" + item.ServiceKey
		return item, enum.SelfDeployDeployPlanStatusDeployPlanUnavailable, item.SafeReason
	}
	item.DeployExecutionSpec = executionSpec
	item.PlanItemFingerprint = selfDeployDeployPlanItemFingerprint(item)
	return item, enum.SelfDeployDeployPlanStatusReady, ""
}

func selfDeployDeployExecutionSpec(input GetSelfDeployDeployPlanInput, serviceKey string, spec value.ServicesPolicyDeploySpec, output SelfDeployBuildOutput, context SelfDeployMaterializedBuildContext) (SelfDeployDeployExecutionSpec, error) {
	kustomizationRef, err := checkedDeployKustomizationRef(spec.Kustomization)
	if err != nil {
		return SelfDeployDeployExecutionSpec{}, err
	}
	manifestRef, err := checkedDeployManifestRef(firstNonEmpty(spec.ServiceManifest, spec.Kustomization))
	if err != nil {
		return SelfDeployDeployExecutionSpec{}, err
	}
	bundleRef, bundleDigest, err := checkedDeployManifestBundle(spec, context)
	if err != nil {
		return SelfDeployDeployExecutionSpec{}, err
	}
	namespace := firstNonEmpty(strings.TrimSpace(spec.TargetNamespace), "kodex-prod")
	clusterRef := firstNonEmpty(strings.TrimSpace(spec.TargetClusterRef), "fleet://cluster/default")
	rollouts := normalizeDeployPolicyRolloutTargets(spec.RolloutTargets, serviceKey, namespace)
	result := SelfDeployDeployExecutionSpec{
		SourceRef:            strings.TrimSpace(input.SourceRef),
		SourceCommitSHA:      strings.ToLower(strings.TrimSpace(input.MergeCommitSHA)),
		ServiceKey:           strings.TrimSpace(serviceKey),
		ImageRef:             strings.TrimSpace(output.ImageRef),
		ImageTag:             strings.TrimSpace(output.ImageTag),
		ImageDigest:          strings.ToLower(strings.TrimSpace(output.ImageDigest)),
		ManifestRef:          manifestRef,
		ManifestDigest:       checkedDeployDigest("manifest", manifestRef, input.ExpectedServicesPolicyDigest),
		KustomizationRef:     kustomizationRef,
		KustomizationDigest:  checkedDeployDigest("kustomization", kustomizationRef, input.ExpectedServicesPolicyDigest),
		TargetNamespace:      namespace,
		TargetClusterRef:     clusterRef,
		TargetSlotID:         strings.TrimSpace(spec.TargetSlotID),
		ManifestBundleRef:    bundleRef,
		ManifestBundleDigest: bundleDigest,
		RolloutTargets:       rollouts,
		ExpectedImageRefs:    []SelfDeployDeployExpectedImageRef{{ContainerName: serviceKey, ImageRef: output.ImageRef + ":" + output.ImageTag, ImageDigest: output.ImageDigest}},
		AllowedSecretRefs:    normalizeDeployAllowedSecretRefs(spec.AllowedSecretRefs),
		OutputRefs:           normalizeDeployOutputRefs(spec.OutputRefs),
	}
	if !deploySpecRefsSafe(result) {
		return SelfDeployDeployExecutionSpec{}, errDeployPlanUnavailable
	}
	return result, nil
}

func checkedDeployKustomizationRef(raw string) (string, error) {
	normalized, err := normalizeWorkspacePath(strings.TrimSpace(raw))
	if err != nil || normalized == "" {
		return "", errDeployPlanUnavailable
	}
	return "context://" + normalized, nil
}

func checkedDeployManifestRef(raw string) (string, error) {
	normalized, err := normalizeWorkspacePath(strings.TrimSpace(raw))
	if err != nil || normalized == "" {
		return "", errDeployPlanUnavailable
	}
	return "context://" + normalized, nil
}

func checkedDeployManifestBundle(spec value.ServicesPolicyDeploySpec, context SelfDeployMaterializedBuildContext) (string, string, error) {
	if context.BuildContextRef == "" || context.BuildContextDigest == "" {
		return "", "", errDeployPlanUnavailable
	}
	digest := strings.ToLower(strings.TrimSpace(context.ManifestBundleDigest))
	if !validBuildPlanSHA256Digest(digest) {
		return "", "", errDeployPlanUnavailable
	}
	kustomization, err := normalizeWorkspacePath(spec.Kustomization)
	if err != nil || kustomization == "" {
		return "", "", errDeployPlanUnavailable
	}
	bundlePath := path.Dir(kustomization)
	if bundlePath != "deploy/base/"+strings.TrimSpace(context.ServiceKey) {
		return "", "", errDeployPlanUnavailable
	}
	ref := strings.TrimRight(context.BuildContextRef, "/") + "/" + bundlePath
	return ref, digest, nil
}

func normalizeSelfDeployBuildOutputs(values []SelfDeployBuildOutput, affectedServiceKeys []string) (map[string]SelfDeployBuildOutput, string) {
	allowed := make(map[string]struct{}, len(affectedServiceKeys))
	for _, key := range affectedServiceKeys {
		allowed[key] = struct{}{}
	}
	result := make(map[string]SelfDeployBuildOutput, len(values))
	for _, value := range values {
		output := normalizeSelfDeployBuildOutput(value)
		if !selfDeployBuildOutputSafe(output) {
			return nil, "build_output_invalid:" + firstNonEmpty(output.ServiceKey, "unknown")
		}
		if _, ok := allowed[output.ServiceKey]; !ok {
			return nil, "build_output_unexpected:" + output.ServiceKey
		}
		if existing, ok := result[output.ServiceKey]; ok && selfDeployBuildOutputFingerprint(existing) != selfDeployBuildOutputFingerprint(output) {
			return nil, "build_output_conflict:" + output.ServiceKey
		}
		result[output.ServiceKey] = output
	}
	return result, ""
}

func normalizeSelfDeployBuildOutput(value SelfDeployBuildOutput) SelfDeployBuildOutput {
	return SelfDeployBuildOutput{
		ServiceKey:               strings.TrimSpace(value.ServiceKey),
		RuntimeJobRef:            strings.TrimSpace(value.RuntimeJobRef),
		ImageRef:                 strings.TrimSpace(value.ImageRef),
		ImageTag:                 strings.TrimSpace(value.ImageTag),
		ImageDigest:              strings.ToLower(strings.TrimSpace(value.ImageDigest)),
		BuildPlanItemFingerprint: strings.ToLower(strings.TrimSpace(value.BuildPlanItemFingerprint)),
		BuildPlanFingerprint:     strings.ToLower(strings.TrimSpace(value.BuildPlanFingerprint)),
		BuildContextRef:          strings.TrimSpace(value.BuildContextRef),
		BuildContextDigest:       strings.ToLower(strings.TrimSpace(value.BuildContextDigest)),
	}
}

func selfDeployBuildOutputSafe(output SelfDeployBuildOutput) bool {
	return serviceKeyPattern.MatchString(output.ServiceKey) &&
		safeBuildPlanRef(output.RuntimeJobRef, false) &&
		safeBuildPlanRef(output.ImageRef, true) &&
		safeBuildPlanLabel(output.ImageTag, maxSelfDeployBuildPlanLabelBytes) &&
		(output.ImageDigest == "" || validBuildPlanSHA256Digest(output.ImageDigest)) &&
		(output.BuildPlanItemFingerprint == "" || validBuildPlanSHA256Digest(output.BuildPlanItemFingerprint)) &&
		(output.BuildPlanFingerprint == "" || validBuildPlanSHA256Digest(output.BuildPlanFingerprint)) &&
		safeBuildPlanRef(output.BuildContextRef, true) &&
		validBuildPlanSHA256Digest(output.BuildContextDigest)
}

func selfDeployBuildOutputFingerprint(output SelfDeployBuildOutput) string {
	encoded, _ := json.Marshal(output)
	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func normalizeDeployAllowedSecretRefs(values []value.ServicesPolicyAllowedSecretRef) []RuntimeJobAllowedSecretRef {
	refs, err := normalizeBuildAllowedSecretRefs(values)
	if err != nil {
		return nil
	}
	return refs
}

func normalizeDeployOutputRefs(values []value.ServicesPolicyOutputRef) []RuntimeJobOutputRef {
	refs, err := normalizeBuildOutputRefs(values)
	if err != nil {
		return nil
	}
	return refs
}

func normalizeDeployPolicyRolloutTargets(values []value.ServicesPolicyDeployRolloutTarget, serviceKey string, namespace string) []SelfDeployDeployRolloutTarget {
	result := make([]SelfDeployDeployRolloutTarget, 0, max(1, len(values)))
	for _, value := range values {
		target := SelfDeployDeployRolloutTarget{
			Kind:      firstNonEmpty(strings.TrimSpace(value.Kind), "deployment"),
			Ref:       firstNonEmpty(strings.TrimSpace(value.Ref), "kubernetes://deployment/"+serviceKey),
			Namespace: firstNonEmpty(strings.TrimSpace(value.Namespace), namespace),
			Name:      firstNonEmpty(strings.TrimSpace(value.Name), serviceKey),
			Digest:    strings.ToLower(strings.TrimSpace(value.Digest)),
		}
		result = append(result, target)
	}
	if len(result) == 0 {
		result = append(result, SelfDeployDeployRolloutTarget{
			Kind:      "deployment",
			Ref:       "kubernetes://deployment/" + serviceKey,
			Namespace: namespace,
			Name:      serviceKey,
		})
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Kind+result[i].Name < result[j].Kind+result[j].Name
	})
	return result
}

func deploySpecRefsSafe(spec SelfDeployDeployExecutionSpec) bool {
	checks := []func() bool{
		func() bool { return safeBuildPlanRef(spec.SourceRef, true) },
		func() bool { return validBuildPlanCommitSHA(spec.SourceCommitSHA) },
		func() bool { return safeBuildPlanLabel(spec.ServiceKey, maxSelfDeployBuildPlanLabelBytes) },
		func() bool { return safeBuildPlanRef(spec.ImageRef, true) },
		func() bool { return safeBuildPlanLabel(spec.ImageTag, maxSelfDeployBuildPlanLabelBytes) },
		func() bool { return spec.ImageDigest == "" || validBuildPlanSHA256Digest(spec.ImageDigest) },
		func() bool { return safeBuildPlanRef(spec.ManifestRef, true) },
		func() bool { return validBuildPlanSHA256Digest(spec.ManifestDigest) },
		func() bool { return safeBuildPlanRef(spec.KustomizationRef, true) },
		func() bool { return validBuildPlanSHA256Digest(spec.KustomizationDigest) },
		func() bool { return serviceKeyPattern.MatchString(spec.TargetNamespace) },
		func() bool { return safeBuildPlanRef(spec.TargetClusterRef, true) },
		func() bool { return safeBuildPlanRef(spec.ManifestBundleRef, true) },
		func() bool { return validBuildPlanSHA256Digest(spec.ManifestBundleDigest) },
	}
	for _, check := range checks {
		if !check() {
			return false
		}
	}
	return len(spec.RolloutTargets) > 0 && len(spec.ExpectedImageRefs) > 0
}

func checkedDeployDigest(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func selfDeployDeployPlanFingerprint(plan SelfDeployDeployPlan) string {
	encoded, _ := json.Marshal(SelfDeployDeployPlan{
		ProjectRef:          plan.ProjectRef,
		RepositoryRef:       plan.RepositoryRef,
		ProviderSignalRef:   plan.ProviderSignalRef,
		SourceRef:           plan.SourceRef,
		MergeCommitSHA:      plan.MergeCommitSHA,
		ServicesYaml:        plan.ServicesYaml,
		AffectedServiceKeys: plan.AffectedServiceKeys,
		DeployItems:         plan.DeployItems,
		Version:             plan.Version,
	})
	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func selfDeployDeployPlanItemFingerprint(item SelfDeployDeployPlanItem) string {
	encoded, _ := json.Marshal(item)
	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:])
}
