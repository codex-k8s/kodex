package service

import (
	"context"
	"slices"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/enum"
)

func (s *Service) resolvePlacement(ctx context.Context, request PlacementResolutionRequest) (PlacementResolution, error) {
	if s.placementResolver == nil {
		return PlacementResolution{}, errs.ErrDependencyUnavailable
	}
	normalized, err := normalizePlacementResolutionRequest(request)
	if err != nil {
		return PlacementResolution{}, err
	}
	resolution, err := s.placementResolver.ResolvePlacement(ctx, normalized)
	if err != nil {
		return PlacementResolution{}, err
	}
	if resolution.FleetScopeID == uuid.Nil || resolution.ClusterID == uuid.Nil {
		return PlacementResolution{}, errs.ErrDependencyUnavailable
	}
	return resolution, nil
}

func slotPlacementRequest(input ReserveSlotInput) (PlacementResolutionRequest, error) {
	projectID, err := mergePlacementProjectID(input.ProjectID, input.PlacementConstraints.ProjectID)
	if err != nil {
		return PlacementResolutionRequest{}, err
	}
	repositoryIDs, err := mergePlacementRepositoryIDs(input.RepositoryIDs, input.PlacementConstraints.RepositoryIDs)
	if err != nil {
		return PlacementResolutionRequest{}, err
	}
	runtimeProfile, err := mergePlacementRuntimeProfile(input.RuntimeProfile, input.PlacementConstraints.RuntimeProfile)
	if err != nil {
		return PlacementResolutionRequest{}, err
	}
	if err := validatePlacementMetadata(input.PlacementConstraints.MetadataJSON); err != nil {
		return PlacementResolutionRequest{}, err
	}
	return PlacementResolutionRequest{
		ProjectID:             projectID,
		RepositoryIDs:         repositoryIDs,
		ServiceKeys:           normalizedPlacementStrings(input.PlacementConstraints.ServiceKeys),
		RuntimeMode:           input.RuntimeMode,
		RuntimeProfile:        runtimeProfile,
		PreferredFleetScopeID: input.PlacementConstraints.PreferredFleetScopeID,
		RequiredCapabilities:  normalizedPlacementStrings(input.PlacementConstraints.RequiredCapabilities),
		Meta:                  input.Meta,
	}, nil
}

func prepareRuntimePlacementRequest(input PrepareRuntimeInput, repositoryIDs []uuid.UUID) (PlacementResolutionRequest, error) {
	projectID := input.WorkspacePolicy.ProjectID
	projectRef, err := mergePlacementProjectID(&projectID, input.PlacementConstraints.ProjectID)
	if err != nil {
		return PlacementResolutionRequest{}, err
	}
	repositoryRefs, err := mergePlacementRepositoryIDs(repositoryIDs, input.PlacementConstraints.RepositoryIDs)
	if err != nil {
		return PlacementResolutionRequest{}, err
	}
	runtimeProfile, err := mergePlacementRuntimeProfile(input.RuntimeProfile, input.PlacementConstraints.RuntimeProfile)
	if err != nil {
		return PlacementResolutionRequest{}, err
	}
	if err := validatePlacementMetadata(input.PlacementConstraints.MetadataJSON); err != nil {
		return PlacementResolutionRequest{}, err
	}
	return PlacementResolutionRequest{
		ProjectID:             projectRef,
		RepositoryIDs:         repositoryRefs,
		ServiceKeys:           normalizedPlacementStrings(input.PlacementConstraints.ServiceKeys),
		RuntimeMode:           input.RuntimeMode,
		RuntimeProfile:        runtimeProfile,
		PreferredFleetScopeID: input.PlacementConstraints.PreferredFleetScopeID,
		RequiredCapabilities:  normalizedPlacementStrings(input.PlacementConstraints.RequiredCapabilities),
		Meta:                  input.Meta,
	}, nil
}

func jobPlacementRequest(input CreateJobInput) (PlacementResolutionRequest, error) {
	projectID, err := mergePlacementProjectID(input.ProjectID, input.PlacementConstraints.ProjectID)
	if err != nil {
		return PlacementResolutionRequest{}, err
	}
	repositoryIDs, err := mergePlacementRepositoryIDs(repositoryIDsForJob(input.RepositoryID), input.PlacementConstraints.RepositoryIDs)
	if err != nil {
		return PlacementResolutionRequest{}, err
	}
	if err := validatePlacementMetadata(input.PlacementConstraints.MetadataJSON); err != nil {
		return PlacementResolutionRequest{}, err
	}
	return PlacementResolutionRequest{
		ProjectID:             projectID,
		RepositoryIDs:         repositoryIDs,
		ServiceKeys:           normalizedPlacementStrings(input.PlacementConstraints.ServiceKeys),
		RuntimeMode:           enum.RuntimeModePlatformJob,
		RuntimeProfile:        jobRuntimeProfile(input.PlacementConstraints.RuntimeProfile),
		PreferredFleetScopeID: input.PlacementConstraints.PreferredFleetScopeID,
		RequiredCapabilities:  normalizedPlacementStrings(input.PlacementConstraints.RequiredCapabilities),
		Meta:                  input.Meta,
	}, nil
}

func normalizePlacementResolutionRequest(request PlacementResolutionRequest) (PlacementResolutionRequest, error) {
	if !validRuntimeMode(request.RuntimeMode) || strings.TrimSpace(request.RuntimeProfile) == "" {
		return PlacementResolutionRequest{}, errs.ErrInvalidArgument
	}
	request.RepositoryIDs = normalizedPlacementUUIDs(request.RepositoryIDs)
	request.ServiceKeys = normalizedPlacementStrings(request.ServiceKeys)
	request.RuntimeProfile = strings.TrimSpace(request.RuntimeProfile)
	request.RequiredCapabilities = normalizedPlacementStrings(request.RequiredCapabilities)
	return request, nil
}

func mergePlacementProjectID(primary *uuid.UUID, constraint *uuid.UUID) (*uuid.UUID, error) {
	switch {
	case primary != nil && constraint != nil && *primary != *constraint:
		return nil, errs.ErrConflict
	case primary != nil:
		return primary, nil
	default:
		return constraint, nil
	}
}

func mergePlacementRepositoryIDs(primary []uuid.UUID, constraints []uuid.UUID) ([]uuid.UUID, error) {
	normalizedPrimary := normalizedPlacementUUIDs(primary)
	normalizedConstraints := normalizedPlacementUUIDs(constraints)
	switch {
	case len(normalizedPrimary) > 0 && len(normalizedConstraints) > 0 && !slices.Equal(normalizedPrimary, normalizedConstraints):
		return nil, errs.ErrConflict
	case len(normalizedPrimary) > 0:
		return normalizedPrimary, nil
	default:
		return normalizedConstraints, nil
	}
}

func mergePlacementRuntimeProfile(primary string, constraint string) (string, error) {
	normalizedPrimary := strings.TrimSpace(primary)
	normalizedConstraint := strings.TrimSpace(constraint)
	switch {
	case normalizedPrimary != "" && normalizedConstraint != "" && normalizedPrimary != normalizedConstraint:
		return "", errs.ErrConflict
	case normalizedPrimary != "":
		return normalizedPrimary, nil
	default:
		return normalizedConstraint, nil
	}
}

func validatePlacementMetadata(payload []byte) error {
	_, err := normalizedJSONObject(payload)
	return err
}

func normalizedPlacementUUIDs(ids []uuid.UUID) []uuid.UUID {
	unique := make(map[uuid.UUID]struct{}, len(ids))
	for _, id := range ids {
		if id != uuid.Nil {
			unique[id] = struct{}{}
		}
	}
	result := make([]uuid.UUID, 0, len(unique))
	for id := range unique {
		result = append(result, id)
	}
	sort.Slice(result, func(left int, right int) bool {
		return result[left].String() < result[right].String()
	})
	return result
}

func normalizedPlacementStrings(values []string) []string {
	unique := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			unique[trimmed] = struct{}{}
		}
	}
	result := make([]string, 0, len(unique))
	for value := range unique {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
