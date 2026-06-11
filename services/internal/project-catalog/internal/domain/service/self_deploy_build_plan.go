package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
	"text/template"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/value"
)

const selfDeployBuildPlanVersion = int64(1)

var (
	errBuildPlanUnavailable    = errors.New("build plan unavailable")
	errBuildContextUnavailable = errors.New("build context unavailable")
)

// GetSelfDeployBuildPlan возвращает checked project-owned build specs для self-deploy.
func (s *Service) GetSelfDeployBuildPlan(ctx context.Context, input GetSelfDeployBuildPlanInput) (SelfDeployBuildPlanResult, error) {
	if err := requireProjectID(input.ProjectID); err != nil {
		return SelfDeployBuildPlanResult{}, err
	}
	keys, err := normalizeBuildPlanServiceKeys(input.AffectedServiceKeys)
	if err != nil {
		return SelfDeployBuildPlanResult{Status: enum.SelfDeployBuildPlanStatusInvalidInput, SafeReason: "invalid_input"}, nil
	}
	if input.RepositoryID == uuid.Nil ||
		strings.TrimSpace(input.SourceRef) == "" ||
		strings.TrimSpace(input.MergeCommitSHA) == "" ||
		strings.TrimSpace(input.ExpectedServicesPolicyDigest) == "" ||
		selfDeployBuildPlanProviderSignalRef(input) == "" ||
		!safeBuildPlanRef(input.SourceRef, true) ||
		!validBuildPlanCommitSHA(input.MergeCommitSHA) {
		return SelfDeployBuildPlanResult{Status: enum.SelfDeployBuildPlanStatusInvalidInput, SafeReason: "invalid_input"}, nil
	}
	if err := s.authorizeProjectQuery(ctx, input.ProjectID, input.Meta, projectActionPolicyRead, projectAggregateServicesPolicy); err != nil {
		return SelfDeployBuildPlanResult{}, err
	}

	policy, err := s.repository.GetServicesPolicy(ctx, input.ProjectID, nil)
	if err != nil {
		if errors.Is(err, errs.ErrNotFound) {
			return SelfDeployBuildPlanResult{Status: enum.SelfDeployBuildPlanStatusBuildPlanUnavailable, SafeReason: "services_policy_not_found"}, nil
		}
		return SelfDeployBuildPlanResult{}, err
	}
	plan := selfDeployBuildPlanBase(input, policy, keys)
	if !selfDeployPolicyReady(policy) {
		return SelfDeployBuildPlanResult{Status: enum.SelfDeployBuildPlanStatusBuildPlanUnavailable, Plan: plan, SafeReason: "services_policy_not_ready"}, nil
	}
	if policy.SourceRepositoryID == nil || *policy.SourceRepositoryID != input.RepositoryID || strings.TrimSpace(policy.SourcePath) != selfDeployPolicySourcePath {
		return SelfDeployBuildPlanResult{Status: enum.SelfDeployBuildPlanStatusPolicyStale, Plan: plan, SafeReason: "services_policy_source_mismatch"}, nil
	}
	if reason := selfDeployBuildPolicyStaleReason(input, policy); reason != "" {
		return SelfDeployBuildPlanResult{Status: enum.SelfDeployBuildPlanStatusPolicyStale, Plan: plan, SafeReason: reason}, nil
	}

	descriptors, _, err := s.repository.ListServiceDescriptors(ctx, query.ServiceDescriptorFilter{
		ProjectID:    input.ProjectID,
		RepositoryID: &input.RepositoryID,
		ServiceKeys:  keys,
		Statuses:     []enum.ServiceStatus{enum.ServiceStatusActive},
		Page:         value.PageRequest{PageSize: selfDeployMaxServices},
	})
	if err != nil {
		return SelfDeployBuildPlanResult{}, err
	}
	descriptorsByKey := serviceDescriptorsByKey(descriptors)
	if missing := firstMissingServiceKey(keys, descriptorsByKey); missing != "" {
		return SelfDeployBuildPlanResult{Status: enum.SelfDeployBuildPlanStatusServiceNotFound, Plan: plan, SafeReason: "service_not_found:" + missing}, nil
	}

	buildSpecs, err := buildSpecsFromCheckedPolicy(policy)
	if err != nil {
		return SelfDeployBuildPlanResult{Status: enum.SelfDeployBuildPlanStatusBuildPlanUnavailable, Plan: plan, SafeReason: "checked_build_specs_unavailable"}, nil
	}
	items := make([]SelfDeployBuildPlanItem, 0, len(keys))
	for _, key := range keys {
		spec, ok := buildSpecs[key]
		if !ok {
			return SelfDeployBuildPlanResult{Status: enum.SelfDeployBuildPlanStatusBuildPlanUnavailable, Plan: plan, SafeReason: "service_build_spec_unavailable:" + key}, nil
		}
		item, err := selfDeployBuildPlanRecipeItem(descriptorsByKey[key], spec)
		if err != nil {
			return SelfDeployBuildPlanResult{Status: enum.SelfDeployBuildPlanStatusBuildPlanUnavailable, Plan: plan, SafeReason: "service_build_spec_unavailable:" + key}, nil
		}
		item.PlanItemFingerprint = selfDeployBuildPlanItemFingerprint(item)
		items = append(items, item)
	}
	plan.BuildItems = items
	plan.PlanFingerprint = selfDeployBuildPlanFingerprint(plan)
	if reason := selfDeployBuildPlanExpectedFingerprintMismatch(input.ExpectedBuildPlanFingerprint, plan.PlanFingerprint); reason != "" {
		plan.SafeSummary = selfDeployBuildPlanContextRequiredSummary(plan)
		return SelfDeployBuildPlanResult{Status: enum.SelfDeployBuildPlanStatusBuildContextInvalid, Plan: plan, SafeReason: reason}, nil
	}
	if len(input.MaterializedBuildContexts) == 0 {
		plan.SafeSummary = selfDeployBuildPlanContextRequiredSummary(plan)
		return SelfDeployBuildPlanResult{Status: enum.SelfDeployBuildPlanStatusBuildContextRequired, Plan: plan, SafeReason: "build_context_required:" + keys[0]}, nil
	}
	contexts, reason := normalizeSelfDeployMaterializedBuildContexts(input.MaterializedBuildContexts, keys)
	if reason != "" {
		plan.SafeSummary = selfDeployBuildPlanContextRequiredSummary(plan)
		return SelfDeployBuildPlanResult{Status: enum.SelfDeployBuildPlanStatusBuildContextInvalid, Plan: plan, SafeReason: reason}, nil
	}
	status := enum.SelfDeployBuildPlanStatusReady
	safeReason := ""
	for i := range plan.BuildItems {
		item := plan.BuildItems[i]
		context, ok := contexts[item.ServiceKey]
		if !ok {
			item.Status = SelfDeployBuildPlanItemStatusBuildContextRequired
			item.SafeReason = "build_context_required:" + item.ServiceKey
			if status == enum.SelfDeployBuildPlanStatusReady {
				status = enum.SelfDeployBuildPlanStatusBuildContextRequired
				safeReason = item.SafeReason
			}
			plan.BuildItems[i] = item
			continue
		}
		if context.PlanItemFingerprint != "" && context.PlanItemFingerprint != item.PlanItemFingerprint {
			item.Status = SelfDeployBuildPlanItemStatusBuildContextInvalid
			item.SafeReason = "build_context_fingerprint_mismatch:" + item.ServiceKey
			status = enum.SelfDeployBuildPlanStatusBuildContextInvalid
			if safeReason == "" {
				safeReason = item.SafeReason
			}
			plan.BuildItems[i] = item
			continue
		}
		ready, err := selfDeployReadyBuildPlanItem(input, item, context)
		if err != nil {
			item.Status = SelfDeployBuildPlanItemStatusBuildContextInvalid
			item.SafeReason = "build_context_invalid:" + item.ServiceKey
			status = enum.SelfDeployBuildPlanStatusBuildContextInvalid
			if safeReason == "" {
				safeReason = item.SafeReason
			}
			plan.BuildItems[i] = item
			continue
		}
		plan.BuildItems[i] = ready
	}
	plan.PlanFingerprint = selfDeployBuildPlanFingerprint(plan)
	for i := range plan.BuildItems {
		if plan.BuildItems[i].Status == SelfDeployBuildPlanItemStatusReady {
			plan.BuildItems[i].BuildExecutionSpec.BuildPlanFingerprint = plan.PlanFingerprint
		}
		plan.BuildItems[i].PlanItemFingerprint = selfDeployBuildPlanItemFingerprint(plan.BuildItems[i])
	}
	if status != enum.SelfDeployBuildPlanStatusReady {
		plan.SafeSummary = selfDeployBuildPlanContextRequiredSummary(plan)
		return SelfDeployBuildPlanResult{Status: status, Plan: plan, SafeReason: safeReason}, nil
	}
	plan.SafeSummary = selfDeployBuildPlanReadySummary(plan)
	return SelfDeployBuildPlanResult{Status: enum.SelfDeployBuildPlanStatusReady, Plan: plan}, nil
}

func selfDeployBuildPlanBase(input GetSelfDeployBuildPlanInput, policy entity.ServicesPolicy, keys []string) SelfDeployBuildPlan {
	return SelfDeployBuildPlan{
		ProjectRef:          input.ProjectID.String(),
		RepositoryRef:       input.RepositoryID.String(),
		ProviderSignalRef:   selfDeployBuildPlanProviderSignalRef(input),
		SourceRef:           strings.TrimSpace(input.SourceRef),
		MergeCommitSHA:      strings.TrimSpace(input.MergeCommitSHA),
		ServicesYaml:        selfDeployServicesYamlProjection(policy),
		AffectedServiceKeys: keys,
		Version:             selfDeployBuildPlanVersion,
	}
}

func selfDeployBuildPlanProviderSignalRef(input GetSelfDeployBuildPlanInput) string {
	return firstNonEmpty(input.ProviderSignalRef, input.ProviderSignalKey, input.ProviderSignalID)
}

func selfDeployBuildPolicyStaleReason(input GetSelfDeployBuildPlanInput, policy entity.ServicesPolicy) string {
	if strings.TrimSpace(input.SourceRef) != strings.TrimSpace(policy.SourceRef) {
		return "services_policy_source_ref_mismatch"
	}
	if !strings.EqualFold(strings.TrimSpace(input.MergeCommitSHA), strings.TrimSpace(policy.SourceCommitSHA)) {
		return "services_policy_source_commit_mismatch"
	}
	if strings.TrimSpace(input.ExpectedServicesPolicyDigest) != strings.TrimSpace(policy.ContentHash) {
		return "services_policy_digest_mismatch"
	}
	if expected := strings.TrimSpace(input.ExpectedServicesPolicyFingerprint); expected != "" && expected != selfDeployServicesYamlFingerprint(policy) {
		return "services_policy_fingerprint_mismatch"
	}
	if input.ExpectedServicesPolicyVersion != nil && *input.ExpectedServicesPolicyVersion != policy.PolicyVersion {
		return "services_policy_version_mismatch"
	}
	return ""
}

func normalizeBuildPlanServiceKeys(values []string) ([]string, error) {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		key := strings.TrimSpace(value)
		if key == "" {
			continue
		}
		if !serviceKeyPattern.MatchString(key) {
			return nil, errs.ErrInvalidArgument
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, key)
	}
	if len(result) == 0 {
		return nil, errs.ErrInvalidArgument
	}
	sort.Strings(result)
	return result, nil
}

func serviceDescriptorsByKey(descriptors []entity.ServiceDescriptor) map[string]entity.ServiceDescriptor {
	result := make(map[string]entity.ServiceDescriptor, len(descriptors))
	for _, descriptor := range descriptors {
		result[strings.TrimSpace(descriptor.ServiceKey)] = descriptor
	}
	return result
}

func firstMissingServiceKey(keys []string, descriptors map[string]entity.ServiceDescriptor) string {
	for _, key := range keys {
		if _, ok := descriptors[key]; !ok {
			return key
		}
	}
	return ""
}

func buildSpecsFromCheckedPolicy(policy entity.ServicesPolicy) (map[string]value.ServicesPolicyBuildSpec, error) {
	document, err := parseServicesPolicyDocument(policy.ValidatedPayload)
	if err != nil {
		return nil, err
	}
	result := map[string]value.ServicesPolicyBuildSpec{}
	for _, entry := range serviceEntries(document) {
		key := serviceKey(entry)
		if key == "" || entry.Build == nil {
			continue
		}
		result[key] = *entry.Build
	}
	derived := buildSpecsFromCheckedInventory(document, result)
	for key, spec := range derived {
		result[key] = spec
	}
	if len(result) == 0 {
		return nil, errBuildPlanUnavailable
	}
	return result, nil
}

func buildSpecsFromCheckedInventory(document value.ServicesPolicyDocument, existing map[string]value.ServicesPolicyBuildSpec) map[string]value.ServicesPolicyBuildSpec {
	if len(document.Spec.Images) == 0 {
		return nil
	}
	result := map[string]value.ServicesPolicyBuildSpec{}
	for _, entry := range serviceEntries(document) {
		key := serviceKey(entry)
		if key == "" {
			continue
		}
		if _, ok := existing[key]; ok {
			continue
		}
		imageSpec, ok := document.Spec.Images[key]
		if !ok || strings.TrimSpace(imageSpec.Type) != "build" {
			continue
		}
		spec, err := checkedInventoryBuildSpec(key, imageSpec, document)
		if err != nil {
			continue
		}
		result[key] = spec
	}
	return result
}

func checkedInventoryBuildSpec(key string, imageSpec value.ServicesPolicyImageSpec, document value.ServicesPolicyDocument) (value.ServicesPolicyBuildSpec, error) {
	imageRef, imageTag, err := checkedInventoryImageRefAndTag(document, key, imageSpec)
	if err != nil {
		return value.ServicesPolicyBuildSpec{}, err
	}
	dockerfileRef, err := checkedInventoryDockerfileRef(imageSpec.Context, imageSpec.Dockerfile)
	if err != nil {
		return value.ServicesPolicyBuildSpec{}, err
	}
	builderImageRef := strings.TrimSpace(imageSpec.BuilderImageRef)
	if builderImageRef == "" {
		builderSpec, ok := document.Spec.Images["kaniko-executor"]
		if !ok {
			return value.ServicesPolicyBuildSpec{}, errBuildPlanUnavailable
		}
		builderImageRef, err = checkedInventoryImage(document, "kaniko-executor", builderSpec)
		if err != nil {
			return value.ServicesPolicyBuildSpec{}, err
		}
	}
	return value.ServicesPolicyBuildSpec{
		ImageRef:           imageRef,
		ImageTag:           imageTag,
		BuildContextRef:    strings.TrimSpace(imageSpec.BuildContextRef),
		BuildContextDigest: strings.ToLower(strings.TrimSpace(imageSpec.BuildContextDigest)),
		DockerfileRef:      dockerfileRef,
		DockerfileDigest:   strings.ToLower(strings.TrimSpace(imageSpec.DockerfileDigest)),
		DockerfileTarget:   strings.TrimSpace(imageSpec.Target),
		BuilderImageRef:    builderImageRef,
		AllowedSecretRefs:  append([]value.ServicesPolicyAllowedSecretRef(nil), imageSpec.AllowedSecretRefs...),
		OutputRefs:         append([]value.ServicesPolicyOutputRef(nil), imageSpec.OutputRefs...),
	}, nil
}

func checkedInventoryImageRefAndTag(document value.ServicesPolicyDocument, key string, imageSpec value.ServicesPolicyImageSpec) (string, string, error) {
	image, err := checkedInventoryImage(document, key, imageSpec)
	if err != nil {
		return "", "", err
	}
	ref, tag, ok := splitBuildImageRefAndTag(image)
	if !ok {
		return "", "", errBuildPlanUnavailable
	}
	return ref, tag, nil
}

func checkedInventoryImage(document value.ServicesPolicyDocument, key string, imageSpec value.ServicesPolicyImageSpec) (string, error) {
	switch {
	case strings.TrimSpace(imageSpec.From) != "":
		return renderCheckedInventoryTemplate(document, key, "from", imageSpec.From)
	case strings.TrimSpace(imageSpec.Repository) != "" && strings.TrimSpace(imageSpec.TagTemplate) != "":
		repository, err := renderCheckedInventoryTemplate(document, key, "repository", imageSpec.Repository)
		if err != nil {
			return "", err
		}
		tag, err := renderCheckedInventoryTemplate(document, key, "tagTemplate", imageSpec.TagTemplate)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(repository) == "" || strings.TrimSpace(tag) == "" {
			return "", errBuildPlanUnavailable
		}
		return repository + ":" + tag, nil
	default:
		return "", errBuildPlanUnavailable
	}
}

func renderCheckedInventoryTemplate(document value.ServicesPolicyDocument, imageName string, fieldName string, source string) (string, error) {
	tpl, err := template.New(imageName + "." + fieldName).Funcs(template.FuncMap{
		"envOr": func(_ string, fallback string) string {
			return fallback
		},
		"env": func(name string) (string, error) {
			return "", fmt.Errorf("checked services policy template env %q is unavailable", name)
		},
		"version": func(name string) (string, error) {
			return checkedInventoryVersion(document, name)
		},
	}).Parse(source)
	if err != nil {
		return "", err
	}
	var output bytes.Buffer
	if err := tpl.Execute(&output, struct {
		Versions map[string]string
	}{
		Versions: checkedInventoryVersionValues(document),
	}); err != nil {
		return "", err
	}
	return output.String(), nil
}

func checkedInventoryVersion(document value.ServicesPolicyDocument, key string) (string, error) {
	version := strings.TrimSpace(document.Spec.Versions[key].Value)
	if version == "" {
		return "", errBuildPlanUnavailable
	}
	return version, nil
}

func checkedInventoryVersionValues(document value.ServicesPolicyDocument) map[string]string {
	result := make(map[string]string, len(document.Spec.Versions))
	for key, version := range document.Spec.Versions {
		result[key] = strings.TrimSpace(version.Value)
	}
	return result
}

func splitBuildImageRefAndTag(value string) (string, string, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || strings.Contains(trimmed, "@") {
		return "", "", false
	}
	lastSlash := strings.LastIndex(trimmed, "/")
	lastColon := strings.LastIndex(trimmed, ":")
	if lastColon <= lastSlash || lastColon == len(trimmed)-1 {
		return "", "", false
	}
	return strings.TrimSpace(trimmed[:lastColon]), strings.TrimSpace(trimmed[lastColon+1:]), true
}

func checkedInventoryDockerfileRef(contextPath string, dockerfile string) (string, error) {
	dockerfile = strings.TrimSpace(dockerfile)
	if dockerfile == "" {
		return "", errBuildPlanUnavailable
	}
	if strings.HasPrefix(dockerfile, "context://") {
		dockerfile = strings.TrimPrefix(dockerfile, "context://")
	} else if strings.Contains(dockerfile, "://") {
		return "", errBuildPlanUnavailable
	}
	contextPath = strings.TrimSpace(contextPath)
	switch contextPath {
	case "", ".":
		// Dockerfile path is already relative to repository root build context.
	default:
		if !strings.HasPrefix(dockerfile, contextPath+"/") {
			dockerfile = path.Join(contextPath, dockerfile)
		}
	}
	normalized, err := normalizeWorkspacePath(dockerfile)
	if err != nil {
		return "", err
	}
	return "context://" + normalized, nil
}

func selfDeployBuildPlanRecipeItem(descriptor entity.ServiceDescriptor, spec value.ServicesPolicyBuildSpec) (SelfDeployBuildPlanItem, error) {
	recipe, err := selfDeployBuildRecipe(spec)
	if err != nil {
		return SelfDeployBuildPlanItem{}, err
	}
	recipe.RecipeFingerprint = selfDeployBuildRecipeFingerprint(recipe)
	return SelfDeployBuildPlanItem{
		ServiceKey:  descriptor.ServiceKey,
		ServiceRef:  "project-catalog:service-descriptor:" + descriptor.ID.String() + ":" + descriptor.ServiceKey,
		Status:      SelfDeployBuildPlanItemStatusBuildContextRequired,
		BuildRecipe: recipe,
		SafeReason:  "build_context_required:" + descriptor.ServiceKey,
	}, nil
}

func selfDeployBuildRecipe(spec value.ServicesPolicyBuildSpec) (SelfDeployBuildRecipe, error) {
	secretRefs, err := normalizeBuildAllowedSecretRefs(spec.AllowedSecretRefs)
	if err != nil {
		return SelfDeployBuildRecipe{}, err
	}
	outputRefs, err := normalizeBuildOutputRefs(spec.OutputRefs)
	if err != nil {
		return SelfDeployBuildRecipe{}, err
	}
	result := SelfDeployBuildRecipe{
		ImageRef:          strings.TrimSpace(spec.ImageRef),
		ImageTag:          strings.TrimSpace(spec.ImageTag),
		ImageDigest:       strings.ToLower(strings.TrimSpace(spec.ImageDigest)),
		DockerfileRef:     strings.TrimSpace(spec.DockerfileRef),
		DockerfileTarget:  strings.TrimSpace(spec.DockerfileTarget),
		BuilderImageRef:   strings.TrimSpace(spec.BuilderImageRef),
		AllowedSecretRefs: secretRefs,
		OutputRefs:        outputRefs,
	}
	if !buildRecipeRequiredRefsPresent(result) || !buildRecipeRefsSafe(result) {
		return SelfDeployBuildRecipe{}, errBuildPlanUnavailable
	}
	return result, nil
}

func selfDeployReadyBuildPlanItem(input GetSelfDeployBuildPlanInput, item SelfDeployBuildPlanItem, context SelfDeployMaterializedBuildContext) (SelfDeployBuildPlanItem, error) {
	executionSpec, err := selfDeployBuildExecutionSpec(input, item.ServiceKey, item.BuildRecipe, context)
	if err != nil {
		return SelfDeployBuildPlanItem{}, err
	}
	item.Status = SelfDeployBuildPlanItemStatusReady
	item.SafeReason = ""
	item.BuildExecutionSpec = executionSpec
	return item, nil
}

func selfDeployBuildExecutionSpec(input GetSelfDeployBuildPlanInput, serviceKey string, recipe SelfDeployBuildRecipe, context SelfDeployMaterializedBuildContext) (SelfDeployBuildExecutionSpec, error) {
	result := SelfDeployBuildExecutionSpec{
		SourceRef:          strings.TrimSpace(input.SourceRef),
		SourceCommitSHA:    strings.ToLower(strings.TrimSpace(input.MergeCommitSHA)),
		ServiceKey:         strings.TrimSpace(serviceKey),
		ImageRef:           recipe.ImageRef,
		ImageTag:           recipe.ImageTag,
		ImageDigest:        recipe.ImageDigest,
		BuildContextRef:    strings.TrimSpace(context.BuildContextRef),
		BuildContextDigest: strings.ToLower(strings.TrimSpace(context.BuildContextDigest)),
		DockerfileRef:      recipe.DockerfileRef,
		DockerfileDigest:   strings.ToLower(strings.TrimSpace(context.DockerfileDigest)),
		DockerfileTarget:   recipe.DockerfileTarget,
		BuilderImageRef:    recipe.BuilderImageRef,
		AllowedSecretRefs:  append([]RuntimeJobAllowedSecretRef(nil), recipe.AllowedSecretRefs...),
		OutputRefs:         append([]RuntimeJobOutputRef(nil), recipe.OutputRefs...),
	}
	if !buildSpecContextReady(result) {
		return SelfDeployBuildExecutionSpec{}, errBuildContextUnavailable
	}
	if !buildSpecRequiredRefsPresent(result) || !buildSpecRefsSafe(result) {
		return SelfDeployBuildExecutionSpec{}, errBuildPlanUnavailable
	}
	return result, nil
}

func buildRecipeRequiredRefsPresent(recipe SelfDeployBuildRecipe) bool {
	return recipe.ImageRef != "" &&
		recipe.ImageTag != "" &&
		recipe.DockerfileRef != "" &&
		recipe.DockerfileTarget != "" &&
		recipe.BuilderImageRef != ""
}

func buildRecipeRefsSafe(recipe SelfDeployBuildRecipe) bool {
	checks := []func() bool{
		func() bool { return safeBuildPlanRef(recipe.ImageRef, true) },
		func() bool { return safeBuildPlanLabel(recipe.ImageTag, maxSelfDeployBuildPlanLabelBytes) },
		func() bool { return safeBuildPlanRef(recipe.DockerfileRef, true) },
		func() bool { return safeBuildPlanLabel(recipe.DockerfileTarget, maxSelfDeployBuildPlanLabelBytes) },
		func() bool { return safeBuildPlanRef(recipe.BuilderImageRef, true) },
		func() bool { return recipe.ImageDigest == "" || validBuildPlanSHA256Digest(recipe.ImageDigest) },
	}
	for _, check := range checks {
		if !check() {
			return false
		}
	}
	return true
}

func buildSpecContextReady(spec SelfDeployBuildExecutionSpec) bool {
	return spec.BuildContextRef != "" &&
		spec.BuildContextDigest != "" &&
		safeBuildPlanRef(spec.BuildContextRef, true) &&
		validBuildPlanSHA256Digest(spec.BuildContextDigest)
}

func buildSpecRequiredRefsPresent(spec SelfDeployBuildExecutionSpec) bool {
	return spec.SourceRef != "" &&
		spec.SourceCommitSHA != "" &&
		spec.ServiceKey != "" &&
		spec.ImageRef != "" &&
		spec.ImageTag != "" &&
		spec.BuildContextRef != "" &&
		spec.BuildContextDigest != "" &&
		spec.DockerfileRef != "" &&
		spec.DockerfileTarget != "" &&
		spec.BuilderImageRef != ""
}

func buildSpecRefsSafe(spec SelfDeployBuildExecutionSpec) bool {
	checks := []func() bool{
		func() bool { return safeBuildPlanRef(spec.SourceRef, true) },
		func() bool { return validBuildPlanCommitSHA(spec.SourceCommitSHA) },
		func() bool { return safeBuildPlanLabel(spec.ServiceKey, maxSelfDeployBuildPlanLabelBytes) },
		func() bool { return safeBuildPlanRef(spec.ImageRef, true) },
		func() bool { return safeBuildPlanLabel(spec.ImageTag, maxSelfDeployBuildPlanLabelBytes) },
		func() bool { return safeBuildPlanRef(spec.BuildContextRef, true) },
		func() bool { return validBuildPlanSHA256Digest(spec.BuildContextDigest) },
		func() bool { return safeBuildPlanRef(spec.DockerfileRef, true) },
		func() bool { return safeBuildPlanLabel(spec.DockerfileTarget, maxSelfDeployBuildPlanLabelBytes) },
		func() bool { return safeBuildPlanRef(spec.BuilderImageRef, true) },
		func() bool { return spec.ImageDigest == "" || validBuildPlanSHA256Digest(spec.ImageDigest) },
		func() bool { return spec.DockerfileDigest == "" || validBuildPlanSHA256Digest(spec.DockerfileDigest) },
	}
	for _, check := range checks {
		if !check() {
			return false
		}
	}
	return true
}

func normalizeSelfDeployMaterializedBuildContexts(values []SelfDeployMaterializedBuildContext, affectedServiceKeys []string) (map[string]SelfDeployMaterializedBuildContext, string) {
	allowed := make(map[string]struct{}, len(affectedServiceKeys))
	for _, key := range affectedServiceKeys {
		allowed[key] = struct{}{}
	}
	result := make(map[string]SelfDeployMaterializedBuildContext, len(values))
	for _, value := range values {
		context := normalizeSelfDeployMaterializedBuildContext(value)
		if !selfDeployMaterializedBuildContextSafe(context) {
			return nil, "build_context_invalid:" + firstNonEmpty(context.ServiceKey, "unknown")
		}
		if _, ok := allowed[context.ServiceKey]; !ok {
			return nil, "build_context_unexpected:" + context.ServiceKey
		}
		if existing, ok := result[context.ServiceKey]; ok {
			if selfDeployMaterializedBuildContextFingerprint(existing) != selfDeployMaterializedBuildContextFingerprint(context) {
				return nil, "build_context_conflict:" + context.ServiceKey
			}
			continue
		}
		result[context.ServiceKey] = context
	}
	return result, ""
}

func normalizeSelfDeployMaterializedBuildContext(value SelfDeployMaterializedBuildContext) SelfDeployMaterializedBuildContext {
	return SelfDeployMaterializedBuildContext{
		ServiceKey:                 strings.TrimSpace(value.ServiceKey),
		PlanItemFingerprint:        strings.ToLower(strings.TrimSpace(value.PlanItemFingerprint)),
		BuildContextRef:            strings.TrimSpace(value.BuildContextRef),
		BuildContextDigest:         strings.ToLower(strings.TrimSpace(value.BuildContextDigest)),
		DockerfileDigest:           strings.ToLower(strings.TrimSpace(value.DockerfileDigest)),
		MaterializationRef:         strings.TrimSpace(value.MaterializationRef),
		MaterializationFingerprint: strings.ToLower(strings.TrimSpace(value.MaterializationFingerprint)),
	}
}

func selfDeployMaterializedBuildContextSafe(context SelfDeployMaterializedBuildContext) bool {
	return serviceKeyPattern.MatchString(context.ServiceKey) &&
		safeBuildPlanRef(context.BuildContextRef, true) &&
		validBuildPlanSHA256Digest(context.BuildContextDigest) &&
		(context.PlanItemFingerprint == "" || validBuildPlanSHA256Digest(context.PlanItemFingerprint)) &&
		(context.DockerfileDigest == "" || validBuildPlanSHA256Digest(context.DockerfileDigest)) &&
		safeBuildPlanRef(context.MaterializationRef, false) &&
		(context.MaterializationFingerprint == "" || validBuildPlanSHA256Digest(context.MaterializationFingerprint))
}

func selfDeployMaterializedBuildContextFingerprint(context SelfDeployMaterializedBuildContext) string {
	encoded, _ := json.Marshal(context)
	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func selfDeployBuildPlanExpectedFingerprintMismatch(expected string, actual string) string {
	expected = strings.ToLower(strings.TrimSpace(expected))
	if expected == "" {
		return ""
	}
	if !validBuildPlanSHA256Digest(expected) {
		return "build_plan_fingerprint_invalid"
	}
	if expected != actual {
		return "build_plan_fingerprint_mismatch"
	}
	return ""
}

func normalizeBuildAllowedSecretRefs(values []value.ServicesPolicyAllowedSecretRef) ([]RuntimeJobAllowedSecretRef, error) {
	if len(values) > maxSelfDeployBuildPlanExecutionRefs {
		return nil, errBuildPlanUnavailable
	}
	return normalizeTypedBuildRefs(values, buildSecretRefPair, buildAllowedSecretPairSafe, buildAllowedSecretRefFromPair, buildAllowedSecretSortKey)
}

func normalizeBuildOutputRefs(values []value.ServicesPolicyOutputRef) ([]RuntimeJobOutputRef, error) {
	if len(values) > maxSelfDeployBuildPlanExecutionRefs {
		return nil, errBuildPlanUnavailable
	}
	return normalizeTypedBuildRefs(values, buildOutputRefPair, buildOutputPairSafe, buildOutputRefFromPair, buildOutputSortKey)
}

func normalizeTypedBuildRefs[T any, R any](
	values []T,
	extract func(T) (string, string),
	validate func(buildRefPair) bool,
	convert func(buildRefPair) R,
	sortKey func(R) string,
) ([]R, error) {
	pairs, err := normalizeBuildRefPairs(values, extract)
	if err != nil {
		return nil, err
	}
	result := make([]R, 0, len(pairs))
	for _, pair := range pairs {
		if !validate(pair) {
			return nil, errBuildPlanUnavailable
		}
		result = append(result, convert(pair))
	}
	sort.Slice(result, func(i, j int) bool {
		return sortKey(result[i]) < sortKey(result[j])
	})
	return result, nil
}

func buildSecretRefPair(value value.ServicesPolicyAllowedSecretRef) (string, string) {
	return value.SecretRef, value.Purpose
}

func buildAllowedSecretRefFromPair(pair buildRefPair) RuntimeJobAllowedSecretRef {
	return RuntimeJobAllowedSecretRef{SecretRef: pair.Left, Purpose: pair.Right}
}

func buildAllowedSecretPairSafe(pair buildRefPair) bool {
	return safeBuildPlanRef(pair.Left, true) && safeBuildPlanLabel(pair.Right, maxSelfDeployBuildPlanRefKindBytes)
}

func buildAllowedSecretSortKey(ref RuntimeJobAllowedSecretRef) string {
	return ref.SecretRef + "\x00" + ref.Purpose
}

func buildOutputRefPair(value value.ServicesPolicyOutputRef) (string, string) {
	return value.Kind, value.Ref
}

func buildOutputRefFromPair(pair buildRefPair) RuntimeJobOutputRef {
	return RuntimeJobOutputRef{Kind: pair.Left, Ref: pair.Right}
}

func buildOutputPairSafe(pair buildRefPair) bool {
	return safeBuildPlanLabel(pair.Left, maxSelfDeployBuildPlanRefKindBytes) && safeBuildPlanRef(pair.Right, true)
}

func buildOutputSortKey(ref RuntimeJobOutputRef) string {
	return ref.Kind + "\x00" + ref.Ref
}

type buildRefPair struct {
	Left  string
	Right string
}

func normalizeBuildRefPairs[T any](values []T, extract func(T) (string, string)) ([]buildRefPair, error) {
	result := make([]buildRefPair, 0, len(values))
	for _, value := range values {
		left, right := extract(value)
		left = strings.TrimSpace(left)
		right = strings.TrimSpace(right)
		if left == "" || right == "" {
			return nil, errBuildPlanUnavailable
		}
		result = append(result, buildRefPair{Left: left, Right: right})
	}
	return result, nil
}

const (
	maxSelfDeployBuildPlanSafeRefBytes  = 512
	maxSelfDeployBuildPlanLabelBytes    = 128
	maxSelfDeployBuildPlanRefKindBytes  = 64
	maxSelfDeployBuildPlanExecutionRefs = 16
	selfDeployBuildPlanUnsafeMarkers    = "raw_provider_payload|provider_payload|prompt_text|prompt_body|transcript|tool_input|tool_output|workspace_path|kubeconfig|secret_value|secret-value|token=|authorization|stdout|stderr|large_log|-----begin|bearer "
)

var (
	selfDeployBuildPlanDigestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	selfDeployBuildPlanCommitPattern = regexp.MustCompile(`^([0-9a-f]{40}|[0-9a-f]{64})$`)
)

func safeBuildPlanRef(value string, required bool) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return !required
	}
	if len(trimmed) > maxSelfDeployBuildPlanSafeRefBytes || !utf8.ValidString(trimmed) || strings.ContainsAny(trimmed, "\r\n\t{}") {
		return false
	}
	return !unsafeBuildPlanText(trimmed)
}

func safeBuildPlanLabel(value string, maxBytes int) bool {
	trimmed := strings.TrimSpace(value)
	checks := []bool{
		trimmed != "",
		len(trimmed) <= maxBytes,
		utf8.ValidString(trimmed),
		!strings.ContainsAny(trimmed, "\r\n\t {}"),
		!unsafeBuildPlanText(trimmed),
	}
	for _, ok := range checks {
		if !ok {
			return false
		}
	}
	return true
}

func validBuildPlanSHA256Digest(value string) bool {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	return selfDeployBuildPlanDigestPattern.MatchString(trimmed)
}

func validBuildPlanCommitSHA(value string) bool {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	return selfDeployBuildPlanCommitPattern.MatchString(trimmed)
}

func unsafeBuildPlanText(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	for _, marker := range strings.Split(selfDeployBuildPlanUnsafeMarkers, "|") {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func selfDeployBuildPlanFingerprint(plan SelfDeployBuildPlan) string {
	payload := struct {
		ProjectRef          string
		RepositoryRef       string
		ProviderSignalRef   string
		SourceRef           string
		MergeCommitSHA      string
		ServicesYamlDigest  string
		ServicesYamlVersion int64
		AffectedServiceKeys []string
		BuildItems          []SelfDeployBuildPlanFingerprintItem
	}{
		ProjectRef:          plan.ProjectRef,
		RepositoryRef:       plan.RepositoryRef,
		ProviderSignalRef:   plan.ProviderSignalRef,
		SourceRef:           plan.SourceRef,
		MergeCommitSHA:      plan.MergeCommitSHA,
		ServicesYamlDigest:  plan.ServicesYaml.ServicesYamlDigest,
		ServicesYamlVersion: plan.ServicesYaml.PolicyVersion,
		AffectedServiceKeys: plan.AffectedServiceKeys,
		BuildItems:          make([]SelfDeployBuildPlanFingerprintItem, 0, len(plan.BuildItems)),
	}
	for _, item := range plan.BuildItems {
		payload.BuildItems = append(payload.BuildItems, selfDeployBuildPlanFingerprintItem(item))
	}
	encoded, _ := json.Marshal(payload)
	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// SelfDeployBuildPlanFingerprintItem содержит стабильное подмножество для fingerprint плана.
type SelfDeployBuildPlanFingerprintItem struct {
	ServiceKey         string
	ServiceRef         string
	Status             SelfDeployBuildPlanItemStatus
	BuildRecipe        SelfDeployBuildRecipe
	BuildExecutionSpec SelfDeployBuildExecutionSpec
}

func selfDeployBuildPlanFingerprintItem(item SelfDeployBuildPlanItem) SelfDeployBuildPlanFingerprintItem {
	spec := item.BuildExecutionSpec
	spec.BuildPlanFingerprint = ""
	return SelfDeployBuildPlanFingerprintItem{
		ServiceKey:         item.ServiceKey,
		ServiceRef:         item.ServiceRef,
		Status:             item.Status,
		BuildRecipe:        item.BuildRecipe,
		BuildExecutionSpec: spec,
	}
}

func selfDeployBuildPlanItemFingerprint(item SelfDeployBuildPlanItem) string {
	item.SafeReason = ""
	item.PlanItemFingerprint = ""
	encoded, _ := json.Marshal(item)
	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func selfDeployBuildRecipeFingerprint(recipe SelfDeployBuildRecipe) string {
	recipe.RecipeFingerprint = ""
	encoded, _ := json.Marshal(recipe)
	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func selfDeployBuildPlanReadySummary(plan SelfDeployBuildPlan) string {
	return strings.Join([]string{
		"self-deploy build plan ready",
		"services=" + strings.Join(plan.AffectedServiceKeys, ","),
		"commit=" + plan.MergeCommitSHA,
	}, "; ")
}

func selfDeployBuildPlanContextRequiredSummary(plan SelfDeployBuildPlan) string {
	return strings.Join([]string{
		"self-deploy build recipe ready",
		"services=" + strings.Join(plan.AffectedServiceKeys, ","),
		"commit=" + plan.MergeCommitSHA,
		"runtime build context required",
	}, "; ")
}
