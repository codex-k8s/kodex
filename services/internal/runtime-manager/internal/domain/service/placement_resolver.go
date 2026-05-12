package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
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
	request := PlacementResolutionRequest{
		ProjectID:             projectID,
		RepositoryIDs:         repositoryIDs,
		ServiceKeys:           normalizedPlacementStrings(input.PlacementConstraints.ServiceKeys),
		RuntimeMode:           input.RuntimeMode,
		RuntimeProfile:        runtimeProfile,
		PreferredFleetScopeID: input.PlacementConstraints.PreferredFleetScopeID,
		RequiredCapabilities:  normalizedPlacementStrings(input.PlacementConstraints.RequiredCapabilities),
		Meta:                  input.Meta,
	}
	return enrichPlacementRequest(request, input.PlacementConstraints)
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
	request := PlacementResolutionRequest{
		ProjectID:             projectRef,
		RepositoryIDs:         repositoryRefs,
		ServiceKeys:           normalizedPlacementStrings(input.PlacementConstraints.ServiceKeys),
		RuntimeMode:           input.RuntimeMode,
		RuntimeProfile:        runtimeProfile,
		PreferredFleetScopeID: input.PlacementConstraints.PreferredFleetScopeID,
		RequiredCapabilities:  normalizedPlacementStrings(input.PlacementConstraints.RequiredCapabilities),
		Meta:                  input.Meta,
	}
	return enrichPlacementRequest(request, input.PlacementConstraints)
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
	request := PlacementResolutionRequest{
		ProjectID:             projectID,
		RepositoryIDs:         repositoryIDs,
		ServiceKeys:           normalizedPlacementStrings(input.PlacementConstraints.ServiceKeys),
		RuntimeMode:           enum.RuntimeModePlatformJob,
		RuntimeProfile:        jobRuntimeProfile(input.PlacementConstraints.RuntimeProfile),
		PreferredFleetScopeID: input.PlacementConstraints.PreferredFleetScopeID,
		RequiredCapabilities:  normalizedPlacementStrings(input.PlacementConstraints.RequiredCapabilities),
		Meta:                  input.Meta,
	}
	return enrichPlacementRequest(request, input.PlacementConstraints)
}

func normalizePlacementResolutionRequest(request PlacementResolutionRequest) (PlacementResolutionRequest, error) {
	if !validRuntimeMode(request.RuntimeMode) || strings.TrimSpace(request.RuntimeProfile) == "" {
		return PlacementResolutionRequest{}, errs.ErrInvalidArgument
	}
	request.RepositoryIDs = normalizedPlacementUUIDs(request.RepositoryIDs)
	request.ServiceKeys = normalizedPlacementStrings(request.ServiceKeys)
	request.RuntimeProfile = strings.TrimSpace(request.RuntimeProfile)
	request.RequiredCapabilities = normalizedPlacementStrings(request.RequiredCapabilities)
	if len(request.RepositoryIDs) > 1 || len(request.ServiceKeys) > 1 {
		return PlacementResolutionRequest{}, errs.ErrInvalidArgument
	}
	return request, nil
}

func enrichPlacementRequest(request PlacementResolutionRequest, constraints PlacementConstraintsInput) (PlacementResolutionRequest, error) {
	placementJSON, err := normalizedPlacementConstraintsJSON(constraints.MetadataJSON)
	if err != nil {
		return PlacementResolutionRequest{}, err
	}
	requirementsJSON, err := normalizedRuntimeRequirementsJSON(constraints.RequiredCapabilities)
	if err != nil {
		return PlacementResolutionRequest{}, err
	}
	request.PlacementConstraintsJSON = placementJSON
	request.RuntimeRequirementsJSON = requirementsJSON
	return normalizePlacementResolutionRequest(request)
}

type placementConstraintsJSON struct {
	FleetScopeIDs   []string `json:"fleet_scope_ids,omitempty"`
	ClusterIDs      []string `json:"cluster_ids,omitempty"`
	ClusterKeys     []string `json:"cluster_keys,omitempty"`
	Regions         []string `json:"regions,omitempty"`
	CapacityClasses []string `json:"capacity_classes,omitempty"`
	RequireDefault  *bool    `json:"require_default,omitempty"`
	AllowDegraded   *bool    `json:"allow_degraded,omitempty"`
}

type runtimeRequirementsJSON struct {
	CapacityClasses []string `json:"capacity_classes,omitempty"`
}

func normalizedPlacementConstraintsJSON(payload []byte) ([]byte, error) {
	if len(bytes.TrimSpace(payload)) == 0 {
		return []byte(`{}`), nil
	}
	var decoded placementConstraintsJSON
	if err := decodePlacementConstraintsJSON(payload, &decoded); err != nil {
		return nil, err
	}
	fleetScopeIDs, err := normalizedPlacementUUIDStrings(decoded.FleetScopeIDs)
	if err != nil {
		return nil, err
	}
	clusterIDs, err := normalizedPlacementUUIDStrings(decoded.ClusterIDs)
	if err != nil {
		return nil, err
	}
	decoded.FleetScopeIDs = fleetScopeIDs
	decoded.ClusterIDs = clusterIDs
	decoded.ClusterKeys = normalizedPlacementStrings(decoded.ClusterKeys)
	decoded.Regions = normalizedPlacementStrings(decoded.Regions)
	decoded.CapacityClasses = normalizedPlacementStrings(decoded.CapacityClasses)
	return json.Marshal(decoded)
}

func normalizedRuntimeRequirementsJSON(requiredCapabilities []string) ([]byte, error) {
	return json.Marshal(runtimeRequirementsJSON{CapacityClasses: normalizedPlacementStrings(requiredCapabilities)})
}

func decodePlacementConstraintsJSON(payload []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(bytes.TrimSpace(payload)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return errs.ErrInvalidArgument
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errs.ErrInvalidArgument
	}
	return nil
}

func normalizedPlacementUUIDStrings(values []string) ([]string, error) {
	result := make([]string, 0, len(values))
	for _, value := range values {
		id, err := uuid.Parse(strings.TrimSpace(value))
		if err != nil {
			return nil, errs.ErrInvalidArgument
		}
		if id != uuid.Nil {
			result = append(result, id.String())
		}
	}
	return normalizedPlacementStrings(result), nil
}

func placementRequestFingerprint(request PlacementResolutionRequest) (string, error) {
	normalized, err := normalizePlacementResolutionRequest(request)
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(placementRequestFingerprintJSON{
		ProjectID:                uuidPtrString(normalized.ProjectID),
		RepositoryIDs:            uuidStrings(normalized.RepositoryIDs),
		ServiceKeys:              normalized.ServiceKeys,
		RuntimeMode:              string(normalized.RuntimeMode),
		RuntimeProfile:           normalized.RuntimeProfile,
		PreferredFleetScopeID:    uuidPtrString(normalized.PreferredFleetScopeID),
		RequiredCapabilities:     normalized.RequiredCapabilities,
		PlacementConstraintsJSON: json.RawMessage(defaultPlacementJSON(normalized.PlacementConstraintsJSON)),
		RuntimeRequirementsJSON:  json.RawMessage(defaultPlacementJSON(normalized.RuntimeRequirementsJSON)),
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

type placementRequestFingerprintJSON struct {
	ProjectID                string          `json:"project_id,omitempty"`
	RepositoryIDs            []string        `json:"repository_ids,omitempty"`
	ServiceKeys              []string        `json:"service_keys,omitempty"`
	RuntimeMode              string          `json:"runtime_mode"`
	RuntimeProfile           string          `json:"runtime_profile"`
	PreferredFleetScopeID    string          `json:"preferred_fleet_scope_id,omitempty"`
	RequiredCapabilities     []string        `json:"required_capabilities,omitempty"`
	PlacementConstraintsJSON json.RawMessage `json:"placement_constraints_json"`
	RuntimeRequirementsJSON  json.RawMessage `json:"runtime_requirements_json"`
}

func defaultPlacementJSON(payload []byte) []byte {
	if len(bytes.TrimSpace(payload)) == 0 {
		return []byte(`{}`)
	}
	return bytes.TrimSpace(payload)
}

func uuidPtrString(id *uuid.UUID) string {
	if id == nil || *id == uuid.Nil {
		return ""
	}
	return id.String()
}

func uuidStrings(ids []uuid.UUID) []string {
	result := make([]string, 0, len(ids))
	for _, id := range ids {
		if id != uuid.Nil {
			result = append(result, id.String())
		}
	}
	sort.Strings(result)
	return result
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
	_, err := normalizedPlacementConstraintsJSON(payload)
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
