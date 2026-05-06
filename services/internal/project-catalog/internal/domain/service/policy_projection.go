package service

import (
	"encoding/json"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/value"
)

var serviceKeyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,127}$`)

type servicesPolicyProjection struct {
	payload     []byte
	descriptors []entity.ServiceDescriptor
}

func buildServicesPolicyProjection(input ImportServicesPolicyInput, validationStatus enum.ServicesPolicyValidationStatus) (servicesPolicyProjection, error) {
	payload := []byte(strings.TrimSpace(string(input.ValidatedPayload)))
	if len(payload) == 0 || !json.Valid(payload) {
		return servicesPolicyProjection{}, errs.ErrInvalidArgument
	}
	if _, err := parseServicesPolicyDocument(payload); err != nil {
		return servicesPolicyProjection{}, err
	}
	if validationStatus != enum.ServicesPolicyValidationValid {
		return servicesPolicyProjection{payload: payload}, nil
	}
	descriptors, err := descriptorsFromPolicyPayload(payload, input.SourceRepositoryID)
	if err != nil {
		return servicesPolicyProjection{}, err
	}
	if len(descriptors) == 0 {
		return servicesPolicyProjection{}, errs.ErrInvalidArgument
	}
	if err := validatePolicyDocumentationSources(payload, descriptors, input.SourceRepositoryID); err != nil {
		return servicesPolicyProjection{}, err
	}
	return servicesPolicyProjection{payload: payload, descriptors: descriptors}, nil
}

func descriptorsFromPolicyPayload(payload []byte, fallbackRepositoryID *uuid.UUID) ([]entity.ServiceDescriptor, error) {
	document, err := parseServicesPolicyDocument(payload)
	if err != nil {
		return nil, err
	}
	entries := serviceEntries(document)
	descriptors := make([]entity.ServiceDescriptor, 0, len(entries))
	for _, entry := range entries {
		descriptor, err := descriptorFromPolicyService(entry, fallbackRepositoryID)
		if err != nil {
			return nil, err
		}
		descriptors = append(descriptors, descriptor)
	}
	return validateServiceDescriptorSet(descriptors)
}

func parseServicesPolicyDocument(payload []byte) (value.ServicesPolicyDocument, error) {
	var document value.ServicesPolicyDocument
	if err := json.Unmarshal(payload, &document); err != nil {
		return value.ServicesPolicyDocument{}, errs.ErrInvalidArgument
	}
	return document, nil
}

func serviceEntries(document value.ServicesPolicyDocument) []value.ServicesPolicyService {
	if len(document.Spec.Services) > 0 {
		return document.Spec.Services
	}
	if len(document.Spec.DeployableServices) > 0 {
		return document.Spec.DeployableServices
	}
	return document.Services
}

func documentationEntries(document value.ServicesPolicyDocument) []value.ServicesPolicyDocumentationSource {
	if len(document.Spec.DocumentationSources) > 0 {
		return document.Spec.DocumentationSources
	}
	return document.DocumentationSources
}

func descriptorFromPolicyService(item value.ServicesPolicyService, fallbackRepositoryID *uuid.UUID) (entity.ServiceDescriptor, error) {
	serviceKey := serviceKey(item)
	rootPath, err := normalizeWorkspacePath(firstNonEmpty(item.RootPath, item.Path))
	if err != nil {
		return entity.ServiceDescriptor{}, err
	}
	repositoryID, err := policyServiceRepositoryID(item.RepositoryID, fallbackRepositoryID)
	if err != nil {
		return entity.ServiceDescriptor{}, err
	}
	status, err := serviceStatusFromPolicy(item.Status)
	if err != nil {
		return entity.ServiceDescriptor{}, err
	}
	dependencies := normalizeServiceKeys(append(item.DependsOn, item.DependsOnServiceKeys...))
	return entity.ServiceDescriptor{
		RepositoryID:         repositoryID,
		ServiceKey:           serviceKey,
		DisplayName:          firstNonEmpty(item.DisplayName, item.Name, serviceKey),
		Kind:                 serviceKindFromPolicy(firstNonEmpty(item.Kind, item.Type)),
		RootPath:             rootPath,
		DocumentationScopeID: firstNonEmpty(item.DocumentationScopeID, serviceKey),
		DependsOnServiceKeys: dependencies,
		Status:               status,
	}, nil
}

func serviceKey(item value.ServicesPolicyService) string {
	return strings.TrimSpace(firstNonEmpty(item.Key, item.Name))
}

func policyServiceRepositoryID(text string, fallback *uuid.UUID) (*uuid.UUID, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return fallback, nil
	}
	parsed, err := uuid.Parse(trimmed)
	if err != nil {
		return nil, errs.ErrInvalidArgument
	}
	return &parsed, nil
}

func serviceKindFromPolicy(kind string) enum.ServiceKind {
	switch enum.ServiceKind(strings.TrimSpace(kind)) {
	case enum.ServiceKindBackend:
		return enum.ServiceKindBackend
	case enum.ServiceKindFrontend:
		return enum.ServiceKindFrontend
	case enum.ServiceKindWorker:
		return enum.ServiceKindWorker
	case enum.ServiceKindDocumentation:
		return enum.ServiceKindDocumentation
	case enum.ServiceKindPackage:
		return enum.ServiceKindPackage
	default:
		return enum.ServiceKindOther
	}
}

func serviceStatusFromPolicy(status string) (enum.ServiceStatus, error) {
	switch strings.TrimSpace(status) {
	case "", "foundation", string(enum.ServiceStatusActive):
		return enum.ServiceStatusActive, nil
	case string(enum.ServiceStatusDisabled):
		return enum.ServiceStatusDisabled, nil
	case string(enum.ServiceStatusStale):
		return enum.ServiceStatusStale, nil
	default:
		return "", errs.ErrInvalidArgument
	}
}

func normalizeWorkspacePath(text string) (string, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || strings.Contains(trimmed, `\`) || strings.HasPrefix(trimmed, "/") {
		return "", errs.ErrInvalidArgument
	}
	cleaned := path.Clean(trimmed)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", errs.ErrInvalidArgument
	}
	return cleaned, nil
}

func validateServiceDescriptorSet(descriptors []entity.ServiceDescriptor) ([]entity.ServiceDescriptor, error) {
	seen := make(map[string]struct{}, len(descriptors))
	for _, descriptor := range descriptors {
		if !serviceKeyPattern.MatchString(descriptor.ServiceKey) || descriptor.RootPath == "" || descriptor.DisplayName == "" {
			return nil, errs.ErrInvalidArgument
		}
		if _, ok := seen[descriptor.ServiceKey]; ok {
			return nil, errs.ErrInvalidArgument
		}
		seen[descriptor.ServiceKey] = struct{}{}
		if !validServiceKind(descriptor.Kind) || !validServiceStatus(descriptor.Status) {
			return nil, errs.ErrInvalidArgument
		}
		if descriptor.DocumentationScopeID != "" && !serviceKeyPattern.MatchString(descriptor.DocumentationScopeID) {
			return nil, errs.ErrInvalidArgument
		}
		for _, dependency := range descriptor.DependsOnServiceKeys {
			if !serviceKeyPattern.MatchString(dependency) {
				return nil, errs.ErrInvalidArgument
			}
		}
	}
	for _, descriptor := range descriptors {
		for _, dependency := range descriptor.DependsOnServiceKeys {
			if _, ok := seen[dependency]; !ok {
				return nil, errs.ErrInvalidArgument
			}
		}
	}
	return descriptors, nil
}

func validatePolicyDocumentationSources(payload []byte, descriptors []entity.ServiceDescriptor, fallbackRepositoryID *uuid.UUID) error {
	document, err := parseServicesPolicyDocument(payload)
	if err != nil {
		return err
	}
	entries := documentationEntries(document)
	if len(entries) == 0 {
		return nil
	}
	serviceScopes, dependencyScopes := policyDocumentationScopes(descriptors)
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		source, err := documentationSourceFromPolicy(entry, fallbackRepositoryID)
		if err != nil {
			return err
		}
		if err := validateDocumentationSource(source); err != nil {
			return err
		}
		if err := validatePolicyDocumentationScope(source, serviceScopes, dependencyScopes); err != nil {
			return err
		}
		key := string(source.ScopeType) + "\x00" + source.ScopeID + "\x00" + source.LocalPath
		if _, ok := seen[key]; ok {
			return errs.ErrInvalidArgument
		}
		seen[key] = struct{}{}
	}
	return nil
}

func policyDocumentationScopes(descriptors []entity.ServiceDescriptor) (map[string]struct{}, map[string]struct{}) {
	serviceScopes := make(map[string]struct{}, len(descriptors))
	dependencyScopes := make(map[string]struct{}, len(descriptors))
	for _, descriptor := range descriptors {
		scopeID := firstNonEmpty(descriptor.DocumentationScopeID, descriptor.ServiceKey)
		serviceScopes[scopeID] = struct{}{}
		serviceScopes[descriptor.ServiceKey] = struct{}{}
		for _, dependency := range descriptor.DependsOnServiceKeys {
			dependencyScopes[dependency] = struct{}{}
		}
	}
	for _, descriptor := range descriptors {
		if _, ok := dependencyScopes[descriptor.ServiceKey]; ok {
			dependencyScopes[firstNonEmpty(descriptor.DocumentationScopeID, descriptor.ServiceKey)] = struct{}{}
		}
	}
	return serviceScopes, dependencyScopes
}

func documentationSourceFromPolicy(item value.ServicesPolicyDocumentationSource, fallbackRepositoryID *uuid.UUID) (entity.DocumentationSource, error) {
	localPath, err := normalizeWorkspacePath(firstNonEmpty(item.LocalPath, item.Path))
	if err != nil {
		return entity.DocumentationSource{}, err
	}
	repositoryID, err := policyServiceRepositoryID(item.RepositoryID, fallbackRepositoryID)
	if err != nil {
		return entity.DocumentationSource{}, err
	}
	accessMode, err := documentationAccessModeFromPolicy(item.AccessMode)
	if err != nil {
		return entity.DocumentationSource{}, err
	}
	status, err := documentationStatusFromPolicy(item.Status)
	if err != nil {
		return entity.DocumentationSource{}, err
	}
	return entity.DocumentationSource{
		RepositoryID: repositoryID,
		ScopeType:    enum.DocumentationScopeType(strings.TrimSpace(item.ScopeType)),
		ScopeID:      strings.TrimSpace(firstNonEmpty(item.ScopeID, item.Key)),
		LocalPath:    localPath,
		AccessMode:   accessMode,
		Status:       status,
	}, nil
}

func documentationAccessModeFromPolicy(mode string) (enum.DocumentationAccessMode, error) {
	switch strings.TrimSpace(mode) {
	case "", string(enum.DocumentationAccessRead):
		return enum.DocumentationAccessRead, nil
	case string(enum.DocumentationAccessWrite):
		return enum.DocumentationAccessWrite, nil
	default:
		return "", errs.ErrInvalidArgument
	}
}

func documentationStatusFromPolicy(status string) (enum.DocumentationSourceStatus, error) {
	switch strings.TrimSpace(status) {
	case "", string(enum.DocumentationSourceStatusActive):
		return enum.DocumentationSourceStatusActive, nil
	case string(enum.DocumentationSourceStatusDisabled):
		return enum.DocumentationSourceStatusDisabled, nil
	case string(enum.DocumentationSourceStatusBlocked):
		return enum.DocumentationSourceStatusBlocked, nil
	default:
		return "", errs.ErrInvalidArgument
	}
}

func validateDocumentationSource(source entity.DocumentationSource) error {
	if !validDocumentationScope(source.ScopeType, source.ScopeID) ||
		!validDocumentationAccessMode(source.AccessMode) ||
		!validDocumentationStatus(source.Status) ||
		source.LocalPath == "" {
		return errs.ErrInvalidArgument
	}
	return nil
}

func validatePolicyDocumentationScope(source entity.DocumentationSource, serviceScopes map[string]struct{}, dependencyScopes map[string]struct{}) error {
	switch source.ScopeType {
	case enum.DocumentationScopeProject, enum.DocumentationScopeGuidanceRef:
		return nil
	case enum.DocumentationScopeService:
		if _, ok := serviceScopes[source.ScopeID]; ok {
			return nil
		}
	case enum.DocumentationScopeDependency:
		if _, ok := dependencyScopes[source.ScopeID]; ok {
			return nil
		}
	}
	return errs.ErrInvalidArgument
}

func validServiceKind(kind enum.ServiceKind) bool {
	switch kind {
	case enum.ServiceKindBackend, enum.ServiceKindFrontend, enum.ServiceKindWorker, enum.ServiceKindDocumentation, enum.ServiceKindPackage, enum.ServiceKindOther:
		return true
	default:
		return false
	}
}

func validServiceStatus(status enum.ServiceStatus) bool {
	switch status {
	case enum.ServiceStatusActive, enum.ServiceStatusDisabled, enum.ServiceStatusStale:
		return true
	default:
		return false
	}
}

func validDocumentationScope(scopeType enum.DocumentationScopeType, scopeID string) bool {
	switch scopeType {
	case enum.DocumentationScopeProject:
		return true
	case enum.DocumentationScopeService, enum.DocumentationScopeDependency, enum.DocumentationScopeGuidanceRef:
		return strings.TrimSpace(scopeID) != ""
	default:
		return false
	}
}

func validDocumentationAccessMode(mode enum.DocumentationAccessMode) bool {
	switch mode {
	case enum.DocumentationAccessRead, enum.DocumentationAccessWrite:
		return true
	default:
		return false
	}
}

func validDocumentationStatus(status enum.DocumentationSourceStatus) bool {
	switch status {
	case enum.DocumentationSourceStatusActive, enum.DocumentationSourceStatusDisabled, enum.DocumentationSourceStatusBlocked:
		return true
	default:
		return false
	}
}

func normalizeServiceKeys(values []string) []string {
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" && !slices.Contains(normalized, trimmed) {
			normalized = append(normalized, trimmed)
		}
	}
	return normalized
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
