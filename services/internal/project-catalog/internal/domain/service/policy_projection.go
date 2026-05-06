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
