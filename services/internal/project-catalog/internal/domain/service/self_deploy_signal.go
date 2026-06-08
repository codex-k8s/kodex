package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/value"
)

const (
	selfDeployPolicySourcePath  = "services.yaml"
	selfDeploySignalVersion     = int64(1)
	selfDeployMaxServices       = int32(500)
	selfDeploySignalLookupLimit = int32(5)
)

// GetSelfDeploySignal возвращает project-side safe enrichment для provider repository change signal.
func (s *Service) GetSelfDeploySignal(ctx context.Context, input GetSelfDeploySignalInput) (SelfDeploySignalResult, error) {
	if err := requireProjectID(input.ProjectID); err != nil {
		return SelfDeploySignalResult{}, err
	}
	if strings.TrimSpace(input.ProviderSignalID) == "" && strings.TrimSpace(input.ProviderSignalKey) == "" {
		return SelfDeploySignalResult{}, errs.ErrInvalidArgument
	}
	if err := s.authorizeProjectQuery(ctx, input.ProjectID, input.Meta, projectActionPolicyRead, projectAggregateServicesPolicy); err != nil {
		return SelfDeploySignalResult{}, err
	}
	if s.changeSignals == nil {
		return SelfDeploySignalResult{}, errs.ErrDependencyUnavailable
	}
	providerResult, err := s.changeSignals.GetRepositoryChangeSignal(ctx, RepositoryChangeSignalReadInput{
		SignalID:  strings.TrimSpace(input.ProviderSignalID),
		SignalKey: strings.TrimSpace(input.ProviderSignalKey),
		Meta:      input.Meta,
	})
	if err != nil {
		return SelfDeploySignalResult{}, err
	}
	var preloadedRepository *entity.RepositoryBinding
	switch providerResult.Status {
	case ProviderOwnedDataStatusReady:
	case ProviderOwnedDataStatusNotFound:
		fallback, repository, ok, safeReason, err := s.resolveProviderSignalByBinding(ctx, input)
		if err != nil {
			return SelfDeploySignalResult{}, err
		}
		if !ok {
			if safeReason != "" {
				return SelfDeploySignalResult{Status: enum.SelfDeploySignalStatusRepositoryBindingNotFound, SafeReason: safeReason}, nil
			}
			return SelfDeploySignalResult{Status: enum.SelfDeploySignalStatusProviderSignalNotFound, SafeReason: "provider_signal_not_found"}, nil
		}
		providerResult = fallback
		preloadedRepository = repository
	case ProviderOwnedDataStatusNotVerified, ProviderOwnedDataStatusStale:
		return SelfDeploySignalResult{Status: enum.SelfDeploySignalStatusProviderSignalNotReady, SafeReason: "provider_signal_not_ready"}, nil
	default:
		return SelfDeploySignalResult{Status: enum.SelfDeploySignalStatusProviderSignalNotReady, SafeReason: "provider_signal_not_ready"}, nil
	}

	signal := normalizeRepositoryChangeSignal(providerResult.Signal)
	if signal.ProjectID != "" && signal.ProjectID != input.ProjectID.String() {
		return SelfDeploySignalResult{Status: enum.SelfDeploySignalStatusRepositoryBindingNotFound, SafeReason: "provider_signal_project_mismatch"}, nil
	}
	repository, ok, err := s.selfDeployRepositoryBinding(ctx, input, signal, preloadedRepository)
	if err != nil {
		return SelfDeploySignalResult{}, err
	}
	if !ok {
		return SelfDeploySignalResult{Status: enum.SelfDeploySignalStatusRepositoryBindingNotFound, SafeReason: "repository_binding_not_found"}, nil
	}
	if signal.RepositoryID != "" && signal.RepositoryID != repository.ID.String() {
		return SelfDeploySignalResult{Status: enum.SelfDeploySignalStatusRepositoryBindingNotFound, SafeReason: "provider_signal_repository_mismatch"}, nil
	}
	if repository.ProjectID != input.ProjectID || repository.Status != enum.RepositoryStatusActive {
		return SelfDeploySignalResult{Status: enum.SelfDeploySignalStatusRepositoryBindingNotFound, SafeReason: "repository_binding_not_active"}, nil
	}
	if reason := selfDeploySourceBindingMismatchReason(signal, repository); reason != "" {
		return SelfDeploySignalResult{Status: enum.SelfDeploySignalStatusRepositoryBindingNotFound, SafeReason: reason}, nil
	}
	if signal.BaseBranch != "" && repository.DefaultBranch != "" && signal.BaseBranch != repository.DefaultBranch {
		return SelfDeploySignalResult{Status: enum.SelfDeploySignalStatusRepositoryBindingNotFound, SafeReason: "provider_signal_branch_mismatch"}, nil
	}

	baseSignal := selfDeploySignalBase(signal, repository)
	if signal.PathSummaryStatus == RepositoryChangePathSummaryStatusUnavailable {
		return SelfDeploySignalResult{Status: enum.SelfDeploySignalStatusNeedsRepositoryChangeSummary, Signal: baseSignal, SafeReason: "path_summary_unavailable"}, nil
	}
	if !signal.ServicesPolicyChanged && !signal.DeployRelevantChanged {
		baseSignal.SafeSummary = "provider signal has no deploy-relevant project changes"
		return SelfDeploySignalResult{Status: enum.SelfDeploySignalStatusNotDeployRelevant, Signal: baseSignal, SafeReason: "not_deploy_relevant"}, nil
	}

	policy, err := s.repository.GetServicesPolicy(ctx, input.ProjectID, nil)
	if err != nil {
		if err == errs.ErrNotFound {
			return SelfDeploySignalResult{Status: enum.SelfDeploySignalStatusServicesPolicyNotFound, Signal: baseSignal, SafeReason: "services_policy_not_found"}, nil
		}
		return SelfDeploySignalResult{}, err
	}
	baseSignal.ServicesYaml = selfDeployServicesYamlProjection(policy)
	if !selfDeployPolicyReady(policy) {
		return SelfDeploySignalResult{Status: enum.SelfDeploySignalStatusServicesPolicyNotReady, Signal: baseSignal, SafeReason: "services_policy_not_ready"}, nil
	}
	if policy.SourceRepositoryID == nil || *policy.SourceRepositoryID != repository.ID || strings.TrimSpace(policy.SourcePath) != selfDeployPolicySourcePath {
		return SelfDeploySignalResult{Status: enum.SelfDeploySignalStatusNeedsServicesPolicyReconcile, Signal: baseSignal, SafeReason: "services_policy_source_mismatch"}, nil
	}
	if signal.ServicesPolicyChanged && !strings.EqualFold(strings.TrimSpace(policy.SourceCommitSHA), strings.TrimSpace(signal.CommitSHA)) {
		return SelfDeploySignalResult{Status: enum.SelfDeploySignalStatusNeedsServicesPolicyReconcile, Signal: baseSignal, SafeReason: "services_policy_commit_not_reconciled"}, nil
	}

	services, err := s.activeSelfDeployServiceKeys(ctx, input.ProjectID, repository.ID)
	if err != nil {
		return SelfDeploySignalResult{}, err
	}
	if len(services) == 0 {
		return SelfDeploySignalResult{Status: enum.SelfDeploySignalStatusNeedsServicesPolicyReconcile, Signal: baseSignal, SafeReason: "services_policy_has_no_active_services"}, nil
	}
	baseSignal.AffectedServiceKeys = services
	baseSignal.ExpectedRuntimeJobTypes = []enum.SelfDeployExpectedRuntimeJobType{
		enum.SelfDeployExpectedRuntimeJobTypeBuild,
		enum.SelfDeployExpectedRuntimeJobTypeDeploy,
		enum.SelfDeployExpectedRuntimeJobTypeHealthCheck,
	}
	baseSignal.GovernanceRequirement = SelfDeployGovernanceRequirement{GateRequired: true, GatePolicyRef: "self_deploy.owner_gate"}
	baseSignal.ProjectSignalFingerprint = selfDeployProjectFingerprint(baseSignal)
	baseSignal.SafeSummary = selfDeployReadySummary(baseSignal)
	return SelfDeploySignalResult{Status: enum.SelfDeploySignalStatusReady, Signal: baseSignal}, nil
}

func (s *Service) selfDeployRepositoryBinding(ctx context.Context, input GetSelfDeploySignalInput, signal RepositoryChangeSignal, preloaded *entity.RepositoryBinding) (entity.RepositoryBinding, bool, error) {
	if preloaded != nil {
		return *preloaded, true, nil
	}
	if input.RepositoryID != nil {
		repository, err := s.repository.GetRepository(ctx, *input.RepositoryID)
		if err != nil {
			if err == errs.ErrNotFound {
				return entity.RepositoryBinding{}, false, nil
			}
			return entity.RepositoryBinding{}, false, err
		}
		return repository, true, nil
	}
	if signal.RepositoryID != "" {
		repositoryID, err := uuid.Parse(signal.RepositoryID)
		if err == nil {
			repository, err := s.repository.GetRepository(ctx, repositoryID)
			if err == nil {
				return repository, true, nil
			}
			if err != errs.ErrNotFound {
				return entity.RepositoryBinding{}, false, err
			}
		}
	}
	provider, owner, name, ok := repositoryBindingLookup(signal)
	if !ok {
		return entity.RepositoryBinding{}, false, nil
	}
	repository, err := s.repository.GetRepositoryByProviderRef(ctx, provider, owner, name)
	if err != nil {
		if err == errs.ErrNotFound {
			return entity.RepositoryBinding{}, false, nil
		}
		return entity.RepositoryBinding{}, false, err
	}
	return repository, true, nil
}

func selfDeploySourceBindingMismatchReason(signal RepositoryChangeSignal, repository entity.RepositoryBinding) string {
	providerSlug, err := repositoryProviderSlug(repository.Provider)
	if err != nil {
		return "repository_provider_invalid"
	}
	if signal.ProviderSlug != providerSlug {
		return "provider_signal_provider_mismatch"
	}
	owner, name := providerOwnerNameFromFullName(signal.RepositoryFullName, "", "")
	if owner == "" || name == "" {
		return "provider_signal_repository_ref_missing"
	}
	signalOwner, signalName := canonicalProviderOwnerName(providerSlug, owner, name)
	bindingOwner, bindingName := canonicalProviderOwnerName(providerSlug, repository.ProviderOwner, repository.ProviderName)
	if signalOwner != bindingOwner || signalName != bindingName {
		return "provider_signal_repository_ref_mismatch"
	}
	if signal.ProviderRepositoryID != "" &&
		strings.TrimSpace(repository.ProviderRepositoryID) != "" &&
		signal.ProviderRepositoryID != strings.TrimSpace(repository.ProviderRepositoryID) {
		return "provider_signal_provider_repository_mismatch"
	}
	return ""
}

func (s *Service) resolveProviderSignalByBinding(ctx context.Context, input GetSelfDeploySignalInput) (RepositoryChangeSignalReadResult, *entity.RepositoryBinding, bool, string, error) {
	if input.RepositoryID == nil {
		return RepositoryChangeSignalReadResult{}, nil, false, "", nil
	}
	repository, err := s.repository.GetRepository(ctx, *input.RepositoryID)
	if err != nil {
		if err == errs.ErrNotFound {
			return RepositoryChangeSignalReadResult{}, nil, false, "repository_binding_not_found", nil
		}
		return RepositoryChangeSignalReadResult{}, nil, false, "", err
	}
	if repository.ProjectID != input.ProjectID || repository.Status != enum.RepositoryStatusActive {
		return RepositoryChangeSignalReadResult{}, &repository, false, "repository_binding_not_active", nil
	}
	providerSlug, err := repositoryProviderSlug(repository.Provider)
	if err != nil {
		return RepositoryChangeSignalReadResult{}, &repository, false, "repository_binding_provider_unsupported", nil
	}
	signalKey := strings.TrimSpace(input.ProviderSignalKey)
	if signalKey == "" {
		return RepositoryChangeSignalReadResult{}, &repository, false, "", nil
	}
	parts, ok := parseRepositoryChangeSignalKey(signalKey)
	if !ok {
		return RepositoryChangeSignalReadResult{}, &repository, false, "provider_signal_identity_unavailable", nil
	}
	if parts.ProviderSlug != providerSlug {
		return RepositoryChangeSignalReadResult{}, &repository, false, "provider_signal_provider_mismatch", nil
	}
	if parts.Kind == "" || parts.BaseBranch == "" || parts.CommitSHA == "" {
		return RepositoryChangeSignalReadResult{}, &repository, false, "provider_signal_identity_unavailable", nil
	}
	signalOwner, signalName := providerOwnerNameFromFullName(parts.RepositoryFullName, "", "")
	signalOwner, signalName = canonicalProviderOwnerName(providerSlug, signalOwner, signalName)
	bindingOwner, bindingName := canonicalProviderOwnerName(providerSlug, repository.ProviderOwner, repository.ProviderName)
	if signalOwner != bindingOwner || signalName != bindingName {
		return RepositoryChangeSignalReadResult{}, &repository, false, "provider_signal_repository_ref_mismatch", nil
	}
	fullName := bindingOwner + "/" + bindingName
	result, err := s.changeSignals.ListRepositoryChangeSignals(ctx, RepositoryChangeSignalListInput{
		ProviderSlug:         providerSlug,
		RepositoryFullName:   fullName,
		ProviderRepositoryID: strings.TrimSpace(repository.ProviderRepositoryID),
		Kinds:                []string{parts.Kind},
		Statuses:             []string{"observed"},
		BaseBranch:           parts.BaseBranch,
		CommitSHA:            parts.CommitSHA,
		Page:                 value.PageRequest{PageSize: selfDeploySignalLookupLimit},
		Meta:                 input.Meta,
	})
	if err != nil {
		return RepositoryChangeSignalReadResult{}, &repository, false, "", err
	}
	var matched *RepositoryChangeSignal
	for _, candidate := range result.Signals {
		candidate = normalizeRepositoryChangeSignal(candidate)
		if candidate.CommitSHA != parts.CommitSHA || candidate.BaseBranch != parts.BaseBranch || candidate.Kind != parts.Kind {
			continue
		}
		if selfDeploySourceBindingMismatchReason(candidate, repository) != "" {
			continue
		}
		copyCandidate := candidate
		matched = &copyCandidate
		break
	}
	if matched == nil {
		return RepositoryChangeSignalReadResult{}, &repository, false, "", nil
	}
	return RepositoryChangeSignalReadResult{Status: ProviderOwnedDataStatusReady, Signal: *matched}, &repository, true, "", nil
}

type repositoryChangeSignalKeyParts struct {
	ProviderSlug       string
	Kind               string
	RepositoryFullName string
	BaseBranch         string
	CommitSHA          string
}

func parseRepositoryChangeSignalKey(signalKey string) (repositoryChangeSignalKeyParts, bool) {
	parts := strings.Split(strings.TrimSpace(signalKey), ":")
	if len(parts) < 7 ||
		parts[0] != "provider" ||
		parts[2] != "repository_change" {
		return repositoryChangeSignalKeyParts{}, false
	}
	return repositoryChangeSignalKeyParts{
		ProviderSlug:       strings.TrimSpace(parts[1]),
		Kind:               strings.TrimSpace(parts[3]),
		RepositoryFullName: strings.TrimSpace(parts[4]),
		BaseBranch:         strings.TrimSpace(parts[5]),
		CommitSHA:          strings.TrimSpace(parts[6]),
	}, true
}

func repositoryBindingLookup(signal RepositoryChangeSignal) (enum.RepositoryProvider, string, string, bool) {
	var provider enum.RepositoryProvider
	providerSlug := strings.TrimSpace(signal.ProviderSlug)
	switch providerSlug {
	case "github":
		provider = enum.RepositoryProviderGitHub
	case "gitlab":
		provider = enum.RepositoryProviderGitLab
	default:
		return "", "", "", false
	}
	owner, name := providerOwnerNameFromFullName(signal.RepositoryFullName, "", "")
	if owner == "" || name == "" {
		return "", "", "", false
	}
	owner, name = canonicalProviderOwnerName(providerSlug, owner, name)
	return provider, owner, name, true
}

func canonicalProviderOwnerName(providerSlug string, owner string, name string) (string, string) {
	owner = strings.TrimSpace(owner)
	name = strings.TrimSpace(name)
	switch strings.TrimSpace(providerSlug) {
	case "github":
		return strings.ToLower(owner), strings.ToLower(name)
	default:
		return owner, name
	}
}

func normalizeRepositoryChangeSignal(signal RepositoryChangeSignal) RepositoryChangeSignal {
	signal.SignalID = strings.TrimSpace(signal.SignalID)
	signal.SignalKey = strings.TrimSpace(signal.SignalKey)
	signal.Kind = strings.TrimSpace(signal.Kind)
	signal.ProviderSlug = strings.TrimSpace(signal.ProviderSlug)
	signal.ProjectID = strings.TrimSpace(signal.ProjectID)
	signal.RepositoryID = strings.TrimSpace(signal.RepositoryID)
	signal.RepositoryFullName = strings.TrimSpace(signal.RepositoryFullName)
	signal.ProviderRepositoryID = strings.TrimSpace(signal.ProviderRepositoryID)
	signal.Ref = strings.TrimSpace(signal.Ref)
	signal.BaseBranch = strings.TrimSpace(signal.BaseBranch)
	signal.CommitSHA = strings.TrimSpace(signal.CommitSHA)
	signal.BeforeSHA = strings.TrimSpace(signal.BeforeSHA)
	signal.SourceRef = strings.TrimSpace(signal.SourceRef)
	signal.PathDigest = strings.TrimSpace(signal.PathDigest)
	signal.ChangeFingerprint = strings.TrimSpace(signal.ChangeFingerprint)
	signal.ObservedAt = strings.TrimSpace(signal.ObservedAt)
	signal.Status = strings.TrimSpace(signal.Status)
	signal.ETag = strings.TrimSpace(signal.ETag)
	return signal
}

func selfDeploySignalBase(signal RepositoryChangeSignal, repository entity.RepositoryBinding) SelfDeploySignal {
	ref := selfDeployProviderSignalRef(signal)
	return SelfDeploySignal{
		ProviderSignalRef:         ref,
		ProviderSignalID:          signal.SignalID,
		ProviderSignalKey:         signal.SignalKey,
		ProjectRef:                repository.ProjectID.String(),
		RepositoryRef:             repository.ID.String(),
		ProviderSlug:              signal.ProviderSlug,
		RepositoryFullName:        signal.RepositoryFullName,
		ProviderRepositoryID:      signal.ProviderRepositoryID,
		SourceRef:                 firstNonEmpty(signal.Ref, signal.SourceRef, signal.BaseBranch),
		MergeCommitSHA:            signal.CommitSHA,
		PathCategories:            selfDeployPathCategories(signal),
		ServicesYamlChanged:       signal.ServicesPolicyChanged,
		DeployRelevantChanged:     signal.DeployRelevantChanged,
		ProviderChangeFingerprint: signal.ChangeFingerprint,
		ProviderETag:              signal.ETag,
		ObservedAt:                signal.ObservedAt,
		Version:                   selfDeploySignalVersion,
	}
}

func selfDeployProviderSignalRef(signal RepositoryChangeSignal) string {
	if signal.SignalKey != "" {
		return signal.SignalKey
	}
	return signal.SignalID
}

func selfDeployPathCategories(signal RepositoryChangeSignal) []RepositoryChangePathCategoryCount {
	categories := make([]RepositoryChangePathCategoryCount, 0, len(signal.PathCategories)+1)
	seen := map[enum.SelfDeployPathCategory]struct{}{}
	for _, category := range signal.PathCategories {
		if category.Category == "" || category.Count <= 0 {
			continue
		}
		categories = append(categories, category)
		seen[category.Category] = struct{}{}
	}
	if signal.ServicesPolicyChanged {
		if _, ok := seen[enum.SelfDeployPathCategoryServicesPolicy]; !ok {
			categories = append(categories, RepositoryChangePathCategoryCount{Category: enum.SelfDeployPathCategoryServicesPolicy, Count: 1})
		}
	}
	sort.Slice(categories, func(i, j int) bool {
		return string(categories[i].Category) < string(categories[j].Category)
	})
	return categories
}

func selfDeployServicesYamlProjection(policy entity.ServicesPolicy) SelfDeployServicesYamlProjection {
	return SelfDeployServicesYamlProjection{
		ServicesYamlRef:         "project-catalog:services-policy:" + policy.ID.String() + ":" + strings.TrimSpace(policy.SourcePath),
		ServicesYamlDigest:      strings.TrimSpace(policy.ContentHash),
		ServicesYamlFingerprint: selfDeployServicesYamlFingerprint(policy),
		ServicesPolicyID:        policy.ID,
		SourceRepositoryID:      policy.SourceRepositoryID,
		SourcePath:              strings.TrimSpace(policy.SourcePath),
		SourceRef:               strings.TrimSpace(policy.SourceRef),
		SourceCommitSHA:         strings.TrimSpace(policy.SourceCommitSHA),
		PolicyVersion:           policy.PolicyVersion,
		ValidationStatus:        policy.ValidationStatus,
		ProjectionStatus:        policy.ProjectionStatus,
		ImportedAt:              policy.ImportedAt.Format(time.RFC3339Nano),
	}
}

func selfDeployPolicyReady(policy entity.ServicesPolicy) bool {
	if policy.ValidationStatus != enum.ServicesPolicyValidationValid {
		return false
	}
	return policy.ProjectionStatus == enum.ServicesPolicyProjectionSynced || policy.ProjectionStatus == enum.ServicesPolicyProjectionOverridden
}

func (s *Service) activeSelfDeployServiceKeys(ctx context.Context, projectID uuid.UUID, repositoryID uuid.UUID) ([]string, error) {
	descriptors, _, err := s.repository.ListServiceDescriptors(ctx, query.ServiceDescriptorFilter{
		ProjectID:    projectID,
		RepositoryID: &repositoryID,
		Statuses:     []enum.ServiceStatus{enum.ServiceStatusActive},
		Page:         value.PageRequest{PageSize: selfDeployMaxServices},
	})
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(descriptors))
	seen := make(map[string]struct{}, len(descriptors))
	for _, descriptor := range descriptors {
		key := strings.TrimSpace(descriptor.ServiceKey)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys, nil
}

func selfDeployServicesYamlFingerprint(policy entity.ServicesPolicy) string {
	return sha256DigestHex([]string{
		policy.ID.String(),
		strings.TrimSpace(policy.ContentHash),
		strings.TrimSpace(policy.SourceCommitSHA),
		strings.TrimSpace(policy.SourcePath),
		string(policy.ValidationStatus),
		string(policy.ProjectionStatus),
	})
}

func selfDeployProjectFingerprint(signal SelfDeploySignal) string {
	payload := struct {
		ProviderSignalRef         string
		ProjectRef                string
		RepositoryRef             string
		SourceRef                 string
		MergeCommitSHA            string
		ServicesYamlDigest        string
		ServicesYamlFingerprint   string
		AffectedServiceKeys       []string
		PathCategories            []RepositoryChangePathCategoryCount
		ExpectedRuntimeJobTypes   []enum.SelfDeployExpectedRuntimeJobType
		GovernanceGateRequired    bool
		ProviderChangeFingerprint string
	}{
		ProviderSignalRef:         signal.ProviderSignalRef,
		ProjectRef:                signal.ProjectRef,
		RepositoryRef:             signal.RepositoryRef,
		SourceRef:                 signal.SourceRef,
		MergeCommitSHA:            signal.MergeCommitSHA,
		ServicesYamlDigest:        signal.ServicesYaml.ServicesYamlDigest,
		ServicesYamlFingerprint:   signal.ServicesYaml.ServicesYamlFingerprint,
		AffectedServiceKeys:       signal.AffectedServiceKeys,
		PathCategories:            signal.PathCategories,
		ExpectedRuntimeJobTypes:   signal.ExpectedRuntimeJobTypes,
		GovernanceGateRequired:    signal.GovernanceRequirement.GateRequired,
		ProviderChangeFingerprint: signal.ProviderChangeFingerprint,
	}
	encoded, _ := json.Marshal(payload)
	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func sha256DigestHex(parts []string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func selfDeployReadySummary(signal SelfDeploySignal) string {
	parts := []string{
		"self-deploy signal ready",
		"services=" + strings.Join(signal.AffectedServiceKeys, ","),
		"commit=" + signal.MergeCommitSHA,
	}
	if signal.ServicesYamlChanged {
		parts = append(parts, "services_yaml_changed=true")
	}
	if signal.DeployRelevantChanged {
		parts = append(parts, "deploy_relevant_changed=true")
	}
	return strings.Join(parts, "; ")
}
