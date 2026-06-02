package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/value"
)

const (
	selfDeploySummaryLimit = 1000
	selfDeployRefLimit     = 512
)

type selfDeployPlanCommandPayload struct {
	SelfDeployPlan entity.SelfDeployPlan `json:"self_deploy_plan"`
}

func (s *Service) CreateSelfDeployPlan(ctx context.Context, input CreateSelfDeployPlanInput) (entity.SelfDeployPlan, error) {
	if err := s.requireRepository(); err != nil {
		return entity.SelfDeployPlan{}, err
	}
	idempotencyKey, err := selfDeployPlanIdempotencyKey(input.Meta)
	if err != nil {
		return entity.SelfDeployPlan{}, err
	}
	plan, err := normalizeSelfDeployPlanInput(input, idempotencyKey)
	if err != nil {
		return entity.SelfDeployPlan{}, err
	}
	verifyReplay := verifyEntityRequestReplay(plan, s.repository.GetSelfDeployPlan, selfDeployPlanID, sameSelfDeployPlanRequest)
	if replay, ok, err := findReplay(ctx, s, input.Meta, operationCreateSelfDeployPlan, enum.CommandAggregateTypeSelfDeployPlan, selfDeployPlanFromPayload, verifyReplay); ok || err != nil {
		return replay, err
	}
	now := s.clock.Now()
	plan.ID = s.idGenerator.New()
	plan.Version = 1
	plan.CreatedAt = now
	plan.UpdatedAt = now
	payload, err := marshalCommandPayload(selfDeployPlanCommandPayload{SelfDeployPlan: plan})
	if err != nil {
		return entity.SelfDeployPlan{}, err
	}
	result, err := commandResult(input.Meta, operationCreateSelfDeployPlan, enum.CommandAggregateTypeSelfDeployPlan, plan.ID, payload, now)
	if err != nil {
		return entity.SelfDeployPlan{}, err
	}
	event, err := selfDeployPlanRequestedEvent(s.idGenerator.New(), plan, now)
	if err != nil {
		return entity.SelfDeployPlan{}, err
	}
	return plan, s.repository.CreateSelfDeployPlanWithResult(ctx, plan, result, event)
}

func (s *Service) GetSelfDeployPlan(ctx context.Context, id uuid.UUID) (entity.SelfDeployPlan, error) {
	return getByID(ctx, s, id, s.repository.GetSelfDeployPlan)
}

func (s *Service) ListSelfDeployPlans(ctx context.Context, filter query.SelfDeployPlanFilter) ([]entity.SelfDeployPlan, value.PageResult, error) {
	if err := validateSelfDeployPlanFilter(filter); err != nil {
		return nil, value.PageResult{}, err
	}
	return listFromRepository(ctx, s, filter, s.repository.ListSelfDeployPlans)
}

func normalizeSelfDeployPlanInput(input CreateSelfDeployPlanInput, idempotencyKey string) (entity.SelfDeployPlan, error) {
	if err := validateScope(input.Scope); err != nil {
		return entity.SelfDeployPlan{}, err
	}
	projectRef, err := normalizeSelfDeployRef(input.ProjectRef, true)
	if err != nil {
		return entity.SelfDeployPlan{}, err
	}
	repositoryRef, err := normalizeSelfDeployRef(input.RepositoryRef, true)
	if err != nil {
		return entity.SelfDeployPlan{}, err
	}
	providerSignalRef, err := normalizeSelfDeployRef(input.ProviderSignalRef, false)
	if err != nil {
		return entity.SelfDeployPlan{}, err
	}
	sourceRef, err := normalizeSelfDeployRef(input.SourceRef, true)
	if err != nil {
		return entity.SelfDeployPlan{}, err
	}
	servicesYAMLRef, err := normalizeSelfDeployRef(input.ServicesYAMLRef, false)
	if err != nil {
		return entity.SelfDeployPlan{}, err
	}
	commit, err := normalizeSelfDeployCommit(input.MergeCommitSHA)
	if err != nil {
		return entity.SelfDeployPlan{}, err
	}
	servicesDigest, err := normalizeSHA256Digest(input.ServicesYAMLDigest)
	if err != nil || servicesDigest == "" {
		return entity.SelfDeployPlan{}, errs.ErrInvalidArgument
	}
	serviceKeys, err := normalizeSelfDeployServiceKeys(input.AffectedServiceKeys)
	if err != nil {
		return entity.SelfDeployPlan{}, err
	}
	categories, err := normalizeSelfDeployPathCategories(input.PathCategories)
	if err != nil {
		return entity.SelfDeployPlan{}, err
	}
	jobTypes, err := normalizeSelfDeployRuntimeJobTypes(input.ExpectedRuntimeJobTypes)
	if err != nil {
		return entity.SelfDeployPlan{}, err
	}
	governanceContext, err := normalizeGovernanceContext(input.GovernanceContext)
	if err != nil {
		return entity.SelfDeployPlan{}, err
	}
	summary, err := normalizeSelfDeployText(input.SafeSummary, selfDeploySummaryLimit, false)
	if err != nil {
		return entity.SelfDeployPlan{}, err
	}
	plan := entity.SelfDeployPlan{
		Scope:                   value.ScopeRef{Type: strings.TrimSpace(input.Scope.Type), Ref: strings.TrimSpace(input.Scope.Ref)},
		ProjectRef:              projectRef,
		RepositoryRef:           repositoryRef,
		ProviderSignalRef:       providerSignalRef,
		SourceRef:               sourceRef,
		MergeCommitSHA:          commit,
		ServicesYAMLRef:         servicesYAMLRef,
		ServicesYAMLDigest:      servicesDigest,
		AffectedServiceKeys:     serviceKeys,
		PathCategories:          categories,
		ExpectedRuntimeJobTypes: jobTypes,
		GovernanceContext:       governanceContext,
		SafeSummary:             summary,
		IdempotencyKey:          idempotencyKey,
		Status:                  enum.SelfDeployPlanStatusPendingApproval,
	}
	plan.PlanFingerprint, err = selfDeployPlanFingerprint(plan)
	if err != nil {
		return entity.SelfDeployPlan{}, err
	}
	return plan, nil
}

func validateSelfDeployPlanFilter(filter query.SelfDeployPlanFilter) error {
	if filter.Scope.Type != "" || filter.Scope.Ref != "" {
		if err := validateScope(filter.Scope); err != nil {
			return err
		}
	}
	for _, ref := range []string{filter.ProjectRef, filter.RepositoryRef, filter.ProviderSignalRef} {
		if _, err := normalizeSelfDeployRef(ref, false); err != nil {
			return err
		}
	}
	if filter.Status != nil && !validSelfDeployPlanStatus(*filter.Status) {
		return errs.ErrInvalidArgument
	}
	if strings.TrimSpace(filter.Scope.Ref) == "" &&
		strings.TrimSpace(filter.ProjectRef) == "" &&
		strings.TrimSpace(filter.RepositoryRef) == "" &&
		strings.TrimSpace(filter.ProviderSignalRef) == "" {
		return errs.ErrInvalidArgument
	}
	return nil
}

func selfDeployPlanIdempotencyKey(meta value.CommandMeta) (string, error) {
	identity, err := commandIdentity(meta, operationCreateSelfDeployPlan)
	if err != nil {
		return "", err
	}
	return commandResultKey(identity), nil
}

func selfDeployPlanFingerprint(plan entity.SelfDeployPlan) (string, error) {
	payload := struct {
		Scope                   value.ScopeRef                  `json:"scope"`
		ProjectRef              string                          `json:"project_ref"`
		RepositoryRef           string                          `json:"repository_ref"`
		ProviderSignalRef       string                          `json:"provider_signal_ref,omitempty"`
		SourceRef               string                          `json:"source_ref"`
		MergeCommitSHA          string                          `json:"merge_commit_sha"`
		ServicesYAMLRef         string                          `json:"services_yaml_ref,omitempty"`
		ServicesYAMLDigest      string                          `json:"services_yaml_digest"`
		AffectedServiceKeys     []string                        `json:"affected_service_keys"`
		PathCategories          []enum.SelfDeployPathCategory   `json:"path_categories"`
		ExpectedRuntimeJobTypes []enum.SelfDeployRuntimeJobType `json:"expected_runtime_job_types"`
		GovernanceContext       value.GovernanceContextRef      `json:"governance_context"`
		SafeSummary             string                          `json:"safe_summary,omitempty"`
	}{
		Scope:                   plan.Scope,
		ProjectRef:              plan.ProjectRef,
		RepositoryRef:           plan.RepositoryRef,
		ProviderSignalRef:       plan.ProviderSignalRef,
		SourceRef:               plan.SourceRef,
		MergeCommitSHA:          plan.MergeCommitSHA,
		ServicesYAMLRef:         plan.ServicesYAMLRef,
		ServicesYAMLDigest:      plan.ServicesYAMLDigest,
		AffectedServiceKeys:     plan.AffectedServiceKeys,
		PathCategories:          plan.PathCategories,
		ExpectedRuntimeJobTypes: plan.ExpectedRuntimeJobTypes,
		GovernanceContext:       plan.GovernanceContext,
		SafeSummary:             plan.SafeSummary,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, data); err != nil {
		return "", err
	}
	sum := sha256.Sum256(compact.Bytes())
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func normalizeSelfDeployRef(value string, required bool) (string, error) {
	normalized, err := normalizeFollowUpRefText(value, required)
	if err != nil {
		return "", err
	}
	if len(normalized) > selfDeployRefLimit {
		return "", errs.ErrInvalidArgument
	}
	return normalized, nil
}

func normalizeSelfDeployCommit(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) != 40 && len(trimmed) != 64 {
		return "", errs.ErrInvalidArgument
	}
	for _, char := range trimmed {
		if !asciiHex(char) {
			return "", errs.ErrInvalidArgument
		}
	}
	return strings.ToLower(trimmed), nil
}

func normalizeSelfDeployServiceKeys(values []string) ([]string, error) {
	return normalizeSelfDeployStringSet(values, 100, validateSelfDeployServiceKey)
}

func normalizeSelfDeployStringSet(values []string, limit int, validate func(string) error) ([]string, error) {
	if len(values) == 0 || len(values) > limit {
		return nil, errs.ErrInvalidArgument
	}
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if err := validate(value); err != nil {
			return nil, err
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	if len(result) == 0 {
		return nil, errs.ErrInvalidArgument
	}
	sort.Strings(result)
	return result, nil
}

func validateSelfDeployServiceKey(value string) error {
	if value == "" || len(value) > 128 || unsafeFollowUpText(value) {
		return errs.ErrInvalidArgument
	}
	for index, char := range value {
		if asciiLetter(char) || asciiDigit(char) || char == '_' || char == '-' || char == '.' || char == '/' {
			if index == 0 && char == '/' {
				return errs.ErrInvalidArgument
			}
			continue
		}
		return errs.ErrInvalidArgument
	}
	return nil
}

func normalizeSelfDeployPathCategories(values []enum.SelfDeployPathCategory) ([]enum.SelfDeployPathCategory, error) {
	return normalizeSelfDeployEnumSet(values, 16, validSelfDeployPathCategory)
}

func normalizeSelfDeployRuntimeJobTypes(values []enum.SelfDeployRuntimeJobType) ([]enum.SelfDeployRuntimeJobType, error) {
	return normalizeSelfDeployEnumSet(values, 8, validSelfDeployRuntimeJobType)
}

func normalizeSelfDeployEnumSet[Item ~string](values []Item, limit int, valid func(Item) bool) ([]Item, error) {
	if len(values) == 0 || len(values) > limit {
		return nil, errs.ErrInvalidArgument
	}
	seen := map[Item]struct{}{}
	result := make([]Item, 0, len(values))
	for _, value := range values {
		if !valid(value) {
			return nil, errs.ErrInvalidArgument
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Slice(result, func(left int, right int) bool { return result[left] < result[right] })
	return result, nil
}

func normalizeSelfDeployText(value string, limit int, required bool) (string, error) {
	return normalizeBoundedSafeText(value, limit, required, unsafeSelfDeployText)
}

func unsafeSelfDeployText(value string) bool {
	if unsafeFollowUpText(value) {
		return true
	}
	lower := strings.ToLower(value)
	markers := []string{
		"webhook_body",
		"full_diff",
		"full_yaml",
		"kubeconfig",
		"oauth",
		"private_key",
		"client_secret",
	}
	for _, marker := range markers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func validSelfDeployPlanStatus(status enum.SelfDeployPlanStatus) bool {
	switch status {
	case enum.SelfDeployPlanStatusPendingApproval,
		enum.SelfDeployPlanStatusApproved,
		enum.SelfDeployPlanStatusRejected,
		enum.SelfDeployPlanStatusCancelled,
		enum.SelfDeployPlanStatusFailed:
		return true
	default:
		return false
	}
}

func validSelfDeployPathCategory(category enum.SelfDeployPathCategory) bool {
	switch category {
	case enum.SelfDeployPathCategoryServiceSource,
		enum.SelfDeployPathCategoryServiceConfig,
		enum.SelfDeployPathCategoryDeployManifest,
		enum.SelfDeployPathCategoryRuntimeConfig,
		enum.SelfDeployPathCategoryDocumentation,
		enum.SelfDeployPathCategoryTest,
		enum.SelfDeployPathCategoryPlatformPolicy,
		enum.SelfDeployPathCategoryOther:
		return true
	default:
		return false
	}
}

func validSelfDeployRuntimeJobType(jobType enum.SelfDeployRuntimeJobType) bool {
	switch jobType {
	case enum.SelfDeployRuntimeJobTypeBuild,
		enum.SelfDeployRuntimeJobTypeDeploy,
		enum.SelfDeployRuntimeJobTypeHealthCheck:
		return true
	default:
		return false
	}
}

func selfDeployPlanFromPayload(payload []byte) (entity.SelfDeployPlan, error) {
	var decoded selfDeployPlanCommandPayload
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return entity.SelfDeployPlan{}, errs.ErrInvalidArgument
	}
	return decoded.SelfDeployPlan, nil
}

func selfDeployPlanID(plan entity.SelfDeployPlan) uuid.UUID { return plan.ID }

func sameSelfDeployPlanRequest(stored entity.SelfDeployPlan, expected entity.SelfDeployPlan) bool {
	return sameScope(stored.Scope, expected.Scope) &&
		stored.ProjectRef == expected.ProjectRef &&
		stored.RepositoryRef == expected.RepositoryRef &&
		stored.ProviderSignalRef == expected.ProviderSignalRef &&
		stored.SourceRef == expected.SourceRef &&
		stored.MergeCommitSHA == expected.MergeCommitSHA &&
		stored.ServicesYAMLRef == expected.ServicesYAMLRef &&
		stored.ServicesYAMLDigest == expected.ServicesYAMLDigest &&
		stringSlicesEqual(stored.AffectedServiceKeys, expected.AffectedServiceKeys) &&
		selfDeployPathCategoriesEqual(stored.PathCategories, expected.PathCategories) &&
		selfDeployRuntimeJobTypesEqual(stored.ExpectedRuntimeJobTypes, expected.ExpectedRuntimeJobTypes) &&
		sameGovernanceContext(stored.GovernanceContext, expected.GovernanceContext) &&
		stored.SafeSummary == expected.SafeSummary &&
		stored.PlanFingerprint == expected.PlanFingerprint &&
		stored.IdempotencyKey == expected.IdempotencyKey &&
		stored.Status == expected.Status
}

func stringSlicesEqual(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for idx := range left {
		if left[idx] != right[idx] {
			return false
		}
	}
	return true
}

func selfDeployPathCategoriesEqual(left []enum.SelfDeployPathCategory, right []enum.SelfDeployPathCategory) bool {
	if len(left) != len(right) {
		return false
	}
	for idx := range left {
		if left[idx] != right[idx] {
			return false
		}
	}
	return true
}

func selfDeployRuntimeJobTypesEqual(left []enum.SelfDeployRuntimeJobType, right []enum.SelfDeployRuntimeJobType) bool {
	if len(left) != len(right) {
		return false
	}
	for idx := range left {
		if left[idx] != right[idx] {
			return false
		}
	}
	return true
}
