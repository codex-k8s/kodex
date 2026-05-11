package service

import (
	"context"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/value"
)

// ListPackageInstallationSecretRefs returns value-free secret refs for one package installation.
func (s *Service) ListPackageInstallationSecretRefs(ctx context.Context, input ListPackageInstallationSecretRefsInput) (ListPackageInstallationSecretRefsResult, error) {
	normalized, err := normalizePackageInstallationSecretRefsInput(input)
	if err != nil {
		return ListPackageInstallationSecretRefsResult{}, err
	}
	if err := s.requireAllowed(
		ctx,
		normalized.Meta,
		accessActionPackageInstallationSecretRefRead,
		value.ResourceRef{Type: accessResourcePackageInstallationSecretRef, ID: normalized.PackageInstallationID.String()},
		packageInstallationAccessScope(normalized.InstallationScope),
	); err != nil {
		return ListPackageInstallationSecretRefsResult{}, err
	}
	refs, err := s.repository.ListPackageInstallationSecretRefs(ctx, query.PackageInstallationSecretRefsFilter{
		PackageInstallationID: normalized.PackageInstallationID,
		InstallationScope:     normalized.InstallationScope,
		LogicalKeys:           normalized.LogicalKeys,
	})
	if err != nil {
		return ListPackageInstallationSecretRefsResult{}, err
	}
	return ListPackageInstallationSecretRefsResult{
		PackageInstallationID: normalized.PackageInstallationID,
		InstallationScope:     normalized.InstallationScope,
		SecretRefs:            completeRequestedSecretRefs(normalized, refs),
	}, nil
}

func normalizePackageInstallationSecretRefsInput(input ListPackageInstallationSecretRefsInput) (ListPackageInstallationSecretRefsInput, error) {
	if input.PackageInstallationID == uuid.Nil {
		return ListPackageInstallationSecretRefsInput{}, errs.ErrInvalidArgument
	}
	input.InstallationScope.Type = strings.TrimSpace(input.InstallationScope.Type)
	input.InstallationScope.ID = strings.TrimSpace(input.InstallationScope.ID)
	if err := validatePackageInstallationScope(input.InstallationScope); err != nil {
		return ListPackageInstallationSecretRefsInput{}, err
	}
	keys, err := normalizeLogicalKeys(input.LogicalKeys)
	if err != nil {
		return ListPackageInstallationSecretRefsInput{}, err
	}
	input.LogicalKeys = keys
	return input, nil
}

func validatePackageInstallationScope(scope value.ScopeRef) error {
	switch scope.Type {
	case packageInstallationScopePlatform, accessRuleScopeOrganization, accessRuleScopeProject, accessRuleScopeRepository:
		if scope.ID == "" {
			return errs.ErrInvalidArgument
		}
		return nil
	default:
		return errs.ErrInvalidArgument
	}
}

func normalizeLogicalKeys(keys []string) ([]string, error) {
	seen := make(map[string]struct{}, len(keys))
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			return nil, errs.ErrInvalidArgument
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, key)
	}
	sort.Strings(result)
	return result, nil
}

func packageInstallationAccessScope(scope value.ScopeRef) value.ScopeRef {
	if scope.Type == packageInstallationScopePlatform {
		return value.ScopeRef{Type: accessRuleScopeGlobal}
	}
	return scope
}

func completeRequestedSecretRefs(input ListPackageInstallationSecretRefsInput, refs []entity.PackageInstallationSecretRef) []entity.PackageInstallationSecretRef {
	sort.SliceStable(refs, func(left int, right int) bool {
		return refs[left].LogicalKey < refs[right].LogicalKey
	})
	if len(input.LogicalKeys) == 0 {
		return refs
	}
	byKey := make(map[string]entity.PackageInstallationSecretRef, len(refs))
	for _, ref := range refs {
		byKey[ref.LogicalKey] = ref
	}
	result := make([]entity.PackageInstallationSecretRef, 0, len(input.LogicalKeys))
	for _, key := range input.LogicalKeys {
		if ref, ok := byKey[key]; ok {
			result = append(result, ref)
			continue
		}
		result = append(result, entity.PackageInstallationSecretRef{
			PackageInstallationID: input.PackageInstallationID,
			InstallationScope:     input.InstallationScope,
			LogicalKey:            key,
			Status:                enum.PackageInstallationSecretRefStatusMissing,
			Metadata:              map[string]string{},
		})
	}
	return result
}
