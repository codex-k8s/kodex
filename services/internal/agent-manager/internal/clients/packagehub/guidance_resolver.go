// Package packagehub adapts package-hub reads to agent-manager guidance refs.
package packagehub

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	packagesv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/packages/v1"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/clients/grpcclient"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/errs"
	agentservice "github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/value"
	"google.golang.org/grpc"
)

const (
	callerID               = "agent-manager"
	capabilityKindGuidance = "guidance"
	defaultReadTimeout     = 3 * time.Second
	defaultPageSize        = 100
)

// Config contains package-hub client settings.
type Config struct {
	Addr      string
	AuthToken string
	Timeout   time.Duration
}

type packageHubClient interface {
	GetPackage(context.Context, *packagesv1.GetPackageRequest, ...grpc.CallOption) (*packagesv1.PackageResponse, error)
	ListPackages(context.Context, *packagesv1.ListPackagesRequest, ...grpc.CallOption) (*packagesv1.ListPackagesResponse, error)
	GetPackageVersion(context.Context, *packagesv1.GetPackageVersionRequest, ...grpc.CallOption) (*packagesv1.PackageVersionResponse, error)
	GetPackageManifest(context.Context, *packagesv1.GetPackageManifestRequest, ...grpc.CallOption) (*packagesv1.PackageManifestResponse, error)
	GetPackageInstallation(context.Context, *packagesv1.GetPackageInstallationRequest, ...grpc.CallOption) (*packagesv1.PackageInstallationResponse, error)
	ListPackageInstallations(context.Context, *packagesv1.ListPackageInstallationsRequest, ...grpc.CallOption) (*packagesv1.ListPackageInstallationsResponse, error)
}

// GuidanceResolver reads value-free package metadata and freezes safe refs for runs.
type GuidanceResolver struct {
	client    packageHubClient
	authToken string
	timeout   time.Duration
}

var _ agentservice.GuidanceResolver = (*GuidanceResolver)(nil)

// NewConnection creates a gRPC connection to package-hub.
func NewConnection(cfg Config) (*grpc.ClientConn, error) {
	return grpcclient.NewConnection(cfg.Addr, "package-hub")
}

// NewGuidanceResolver creates package-hub guidance resolver.
func NewGuidanceResolver(client packagesv1.PackageHubServiceClient, cfg Config) (*GuidanceResolver, error) {
	return newGuidanceResolver(client, cfg)
}

func newGuidanceResolver(client packageHubClient, cfg Config) (*GuidanceResolver, error) {
	return grpcclient.BuildAdapter(client, cfg.AuthToken, cfg.Timeout, defaultReadTimeout, "package-hub", func(settings grpcclient.ClientSettings) *GuidanceResolver {
		return &GuidanceResolver{client: client, authToken: settings.AuthToken, timeout: settings.Timeout}
	})
}

// ResolveGuidanceRefs resolves caller hints or active guidance installations in scope.
func (r *GuidanceResolver) ResolveGuidanceRefs(ctx context.Context, input agentservice.GuidanceResolutionInput) ([]value.GuidanceRef, error) {
	if r == nil || r.client == nil {
		return nil, errs.ErrDependencyUnavailable
	}
	scope, err := packageScope(input.Scope)
	if err != nil {
		return nil, err
	}
	hints, err := normalizeHints(input.Hints)
	if err != nil {
		return nil, err
	}
	callCtx, cancel := context.WithTimeout(r.outgoingContext(ctx), r.timeout)
	defer cancel()
	if len(hints) == 0 {
		return r.resolveDefaultGuidance(callCtx, input.Meta, scope)
	}
	return r.resolveHintedGuidance(callCtx, input.Meta, scope, hints)
}

func (r *GuidanceResolver) resolveDefaultGuidance(ctx context.Context, meta value.CommandMeta, scope *packagesv1.ScopeRef) ([]value.GuidanceRef, error) {
	status := packagesv1.PackageInstallationStatus_PACKAGE_INSTALLATION_STATUS_ACTIVE
	kind := packagesv1.PackageKind_PACKAGE_KIND_GUIDANCE
	response, err := r.client.ListPackageInstallations(ctx, &packagesv1.ListPackageInstallationsRequest{
		Meta:               queryMeta(meta),
		Scope:              scope,
		PackageKind:        &kind,
		InstallationStatus: &status,
		Page:               &packagesv1.PageRequest{PageSize: defaultPageSize},
	})
	if err != nil {
		return nil, mapPackageHubError(err)
	}
	return r.guidanceRefsFromInstallations(ctx, meta, scope, response.GetItems())
}

func (r *GuidanceResolver) resolveHintedGuidance(ctx context.Context, meta value.CommandMeta, scope *packagesv1.ScopeRef, hints []value.GuidanceSelectionHint) ([]value.GuidanceRef, error) {
	installations := make([]*packagesv1.PackageInstallation, 0, len(hints))
	seen := make(map[string]struct{}, len(hints))
	for _, hint := range hints {
		var (
			selected *packagesv1.PackageInstallation
			err      error
		)
		if hint.PackageInstallationRef != "" {
			selected, err = r.installationByID(ctx, meta, hint.PackageInstallationRef)
		} else {
			selected, err = r.installationByPackageSlug(ctx, meta, scope, hint.PackageSlug)
		}
		if err != nil {
			return nil, err
		}
		if err := validateInstallationForScope(selected, scope); err != nil {
			return nil, err
		}
		if _, ok := seen[selected.GetId()]; ok {
			continue
		}
		seen[selected.GetId()] = struct{}{}
		installations = append(installations, selected)
	}
	return r.guidanceRefsFromInstallations(ctx, meta, scope, installations)
}

func (r *GuidanceResolver) installationByID(ctx context.Context, meta value.CommandMeta, id string) (*packagesv1.PackageInstallation, error) {
	response, err := r.client.GetPackageInstallation(ctx, &packagesv1.GetPackageInstallationRequest{
		Meta:           queryMeta(meta),
		InstallationId: id,
	})
	if err != nil {
		return nil, mapPackageHubError(err)
	}
	return response.GetInstallation(), nil
}

func (r *GuidanceResolver) installationByPackageSlug(ctx context.Context, meta value.CommandMeta, scope *packagesv1.ScopeRef, slug string) (*packagesv1.PackageInstallation, error) {
	packageEntry, err := r.packageBySlug(ctx, meta, slug)
	if err != nil {
		return nil, err
	}
	status := packagesv1.PackageInstallationStatus_PACKAGE_INSTALLATION_STATUS_ACTIVE
	response, err := r.client.ListPackageInstallations(ctx, &packagesv1.ListPackageInstallationsRequest{
		Meta:               queryMeta(meta),
		Scope:              scope,
		PackageId:          optionalString(packageEntry.GetId()),
		InstallationStatus: &status,
		Page:               &packagesv1.PageRequest{PageSize: defaultPageSize},
	})
	if err != nil {
		return nil, mapPackageHubError(err)
	}
	items := response.GetItems()
	if len(items) == 0 {
		return nil, errs.ErrNotFound
	}
	if len(items) > 1 {
		return nil, errs.ErrPreconditionFailed
	}
	return items[0], nil
}

func (r *GuidanceResolver) packageBySlug(ctx context.Context, meta value.CommandMeta, slug string) (*packagesv1.PackageEntry, error) {
	status := packagesv1.PackageStatus_PACKAGE_STATUS_AVAILABLE
	kind := packagesv1.PackageKind_PACKAGE_KIND_GUIDANCE
	response, err := r.client.ListPackages(ctx, &packagesv1.ListPackagesRequest{
		Meta:        queryMeta(meta),
		PackageKind: &kind,
		Status:      &status,
		Query:       optionalString(slug),
		Page:        &packagesv1.PageRequest{PageSize: defaultPageSize},
	})
	if err != nil {
		return nil, mapPackageHubError(err)
	}
	var matches []*packagesv1.PackageEntry
	for _, item := range response.GetItems() {
		if strings.EqualFold(strings.TrimSpace(item.GetSlug()), slug) {
			matches = append(matches, item)
		}
	}
	if len(matches) == 0 {
		return nil, errs.ErrNotFound
	}
	if len(matches) > 1 {
		return nil, errs.ErrPreconditionFailed
	}
	if err := validatePackageEntry(matches[0]); err != nil {
		return nil, err
	}
	return matches[0], nil
}

func (r *GuidanceResolver) guidanceRefsFromInstallations(ctx context.Context, meta value.CommandMeta, scope *packagesv1.ScopeRef, installations []*packagesv1.PackageInstallation) ([]value.GuidanceRef, error) {
	result := make([]value.GuidanceRef, 0, len(installations))
	seen := make(map[string]struct{}, len(installations))
	for _, installation := range installations {
		if installation == nil {
			continue
		}
		if _, ok := seen[installation.GetId()]; ok {
			continue
		}
		seen[installation.GetId()] = struct{}{}
		if err := validateInstallationForScope(installation, scope); err != nil {
			return nil, err
		}
		ref, err := r.guidanceRefFromInstallation(ctx, meta, installation)
		if err != nil {
			return nil, err
		}
		result = append(result, ref)
	}
	sort.Slice(result, func(left, right int) bool {
		return result[left].PackageInstallationRef < result[right].PackageInstallationRef
	})
	return result, nil
}

func (r *GuidanceResolver) guidanceRefFromInstallation(ctx context.Context, meta value.CommandMeta, installation *packagesv1.PackageInstallation) (value.GuidanceRef, error) {
	version, err := r.packageVersion(ctx, meta, installation.GetPackageVersionId())
	if err != nil {
		return value.GuidanceRef{}, err
	}
	if version.GetPackageId() != installation.GetPackageId() {
		return value.GuidanceRef{}, errs.ErrPreconditionFailed
	}
	if err := validatePackageVersion(version); err != nil {
		return value.GuidanceRef{}, err
	}
	packageEntry, err := r.packageEntry(ctx, meta, installation.GetPackageId())
	if err != nil {
		return value.GuidanceRef{}, err
	}
	if err := validatePackageEntry(packageEntry); err != nil {
		return value.GuidanceRef{}, err
	}
	manifest, err := r.packageManifest(ctx, meta, installation.GetPackageVersionId())
	if err != nil {
		return value.GuidanceRef{}, err
	}
	if manifest.GetPackageVersionId() != installation.GetPackageVersionId() {
		return value.GuidanceRef{}, errs.ErrPreconditionFailed
	}
	if err := validateManifest(manifest); err != nil {
		return value.GuidanceRef{}, err
	}
	summary, err := policySummaryJSON(policySummary{
		PackageKind:               packageEntry.GetPackageKind().String(),
		PackageStatus:             packageEntry.GetStatus().String(),
		PackageTrustStatus:        packageEntry.GetTrustStatus().String(),
		VersionVerificationStatus: version.GetVerificationStatus().String(),
		VersionReleaseStatus:      version.GetReleaseStatus().String(),
		InstallationStatus:        installation.GetInstallationStatus().String(),
		InstallationDesiredState:  installation.GetDesiredState().String(),
		SecretBindingStatus:       installation.GetSecretBindingStatus().String(),
		HealthStatus:              installation.GetLastHealthStatus().String(),
		ManifestValidationStatus:  manifest.GetValidationStatus().String(),
	})
	if err != nil {
		return value.GuidanceRef{}, err
	}
	return value.GuidanceRef{
		PackageInstallationRef: installation.GetId(),
		PackageVersionRef:      installation.GetPackageVersionId(),
		ManifestDigest:         version.GetManifestDigest(),
		SourceRef:              sourceRefString(version.GetSourceRef()),
		CapabilityRef:          capabilityKindGuidance + ":" + installation.GetId(),
		CapabilityKind:         capabilityKindGuidance,
		PackageRef:             installation.GetPackageId(),
		PackageSlug:            packageEntry.GetSlug(),
		PackageVersionLabel:    version.GetVersionLabel(),
		PolicySummaryJSON:      summary,
	}, nil
}

func (r *GuidanceResolver) packageEntry(ctx context.Context, meta value.CommandMeta, packageID string) (*packagesv1.PackageEntry, error) {
	request := &packagesv1.GetPackageRequest{Meta: queryMeta(meta), PackageId: packageID}
	response, err := r.client.GetPackage(ctx, request)
	if err != nil {
		return nil, mapPackageHubError(err)
	}
	return response.GetPackageEntry(), nil
}

func (r *GuidanceResolver) packageVersion(ctx context.Context, meta value.CommandMeta, versionID string) (*packagesv1.PackageVersion, error) {
	request := packageVersionRequest(meta, versionID)
	response, err := r.client.GetPackageVersion(ctx, request)
	if err != nil {
		return nil, mapPackageHubError(err)
	}
	return response.GetVersion(), nil
}

func (r *GuidanceResolver) packageManifest(ctx context.Context, meta value.CommandMeta, versionID string) (*packagesv1.PackageManifestSnapshot, error) {
	response, err := r.client.GetPackageManifest(ctx, packageManifestRequest(meta, versionID))
	if err != nil {
		return nil, mapPackageHubError(err)
	}
	return response.GetManifest(), nil
}

func (r *GuidanceResolver) outgoingContext(ctx context.Context) context.Context {
	return grpcclient.OutgoingContext(ctx, r.authToken, callerID)
}

func normalizeHints(input []value.GuidanceSelectionHint) ([]value.GuidanceSelectionHint, error) {
	result := make([]value.GuidanceSelectionHint, 0, len(input))
	seen := make(map[string]struct{}, len(input))
	for _, hint := range input {
		installationRef := strings.TrimSpace(hint.PackageInstallationRef)
		slug := strings.TrimSpace(hint.PackageSlug)
		if (installationRef == "" && slug == "") || (installationRef != "" && slug != "") {
			return nil, errs.ErrInvalidArgument
		}
		key := "installation:" + installationRef
		if slug != "" {
			key = "slug:" + strings.ToLower(slug)
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value.GuidanceSelectionHint{PackageInstallationRef: installationRef, PackageSlug: slug})
	}
	return result, nil
}

func packageScope(scope value.ScopeRef) (*packagesv1.ScopeRef, error) {
	if strings.TrimSpace(scope.Ref) == "" {
		return nil, errs.ErrInvalidArgument
	}
	scopeType, ok := map[string]packagesv1.PackageInstallationScopeType{
		"platform":     packagesv1.PackageInstallationScopeType_PACKAGE_INSTALLATION_SCOPE_TYPE_PLATFORM,
		"organization": packagesv1.PackageInstallationScopeType_PACKAGE_INSTALLATION_SCOPE_TYPE_ORGANIZATION,
		"project":      packagesv1.PackageInstallationScopeType_PACKAGE_INSTALLATION_SCOPE_TYPE_PROJECT,
		"repository":   packagesv1.PackageInstallationScopeType_PACKAGE_INSTALLATION_SCOPE_TYPE_REPOSITORY,
	}[strings.TrimSpace(scope.Type)]
	if !ok {
		return nil, errs.ErrInvalidArgument
	}
	return &packagesv1.ScopeRef{Type: scopeType, Ref: strings.TrimSpace(scope.Ref)}, nil
}

func validateInstallationForScope(installation *packagesv1.PackageInstallation, scope *packagesv1.ScopeRef) error {
	switch {
	case installation == nil:
		return errs.ErrNotFound
	case strings.TrimSpace(installation.GetId()) == "":
		return errs.ErrPreconditionFailed
	case installation.GetInstallationStatus() != packagesv1.PackageInstallationStatus_PACKAGE_INSTALLATION_STATUS_ACTIVE:
		return errs.ErrPreconditionFailed
	case installation.GetDesiredState() != packagesv1.PackageDesiredState_PACKAGE_DESIRED_STATE_PRESENT:
		return errs.ErrPreconditionFailed
	case installation.GetScope().GetType() != scope.GetType() || strings.TrimSpace(installation.GetScope().GetRef()) != scope.GetRef():
		return errs.ErrPreconditionFailed
	default:
		return nil
	}
}

func validatePackageEntry(entry *packagesv1.PackageEntry) error {
	switch {
	case entry == nil:
		return errs.ErrNotFound
	case strings.TrimSpace(entry.GetId()) == "":
		return errs.ErrPreconditionFailed
	case entry.GetPackageKind() != packagesv1.PackageKind_PACKAGE_KIND_GUIDANCE:
		return errs.ErrPreconditionFailed
	case entry.GetStatus() != packagesv1.PackageStatus_PACKAGE_STATUS_AVAILABLE:
		return errs.ErrPreconditionFailed
	default:
		return nil
	}
}

func validatePackageVersion(version *packagesv1.PackageVersion) error {
	switch {
	case version == nil:
		return errs.ErrNotFound
	case strings.TrimSpace(version.GetId()) == "":
		return errs.ErrPreconditionFailed
	case version.GetVerificationStatus() != packagesv1.PackageVerificationStatus_PACKAGE_VERIFICATION_STATUS_VERIFIED:
		return errs.ErrPreconditionFailed
	case version.GetReleaseStatus() != packagesv1.PackageReleaseStatus_PACKAGE_RELEASE_STATUS_ACTIVE:
		return errs.ErrPreconditionFailed
	case strings.TrimSpace(version.GetManifestDigest()) == "":
		return errs.ErrPreconditionFailed
	default:
		return nil
	}
}

func validateManifest(manifest *packagesv1.PackageManifestSnapshot) error {
	if manifest == nil {
		return errs.ErrNotFound
	}
	switch manifest.GetValidationStatus() {
	case packagesv1.PackageManifestValidationStatus_PACKAGE_MANIFEST_VALIDATION_STATUS_VALID,
		packagesv1.PackageManifestValidationStatus_PACKAGE_MANIFEST_VALIDATION_STATUS_WARNING:
		return nil
	default:
		return errs.ErrPreconditionFailed
	}
}

type policySummary struct {
	PackageKind               string `json:"package_kind"`
	PackageStatus             string `json:"package_status"`
	PackageTrustStatus        string `json:"package_trust_status"`
	VersionVerificationStatus string `json:"version_verification_status"`
	VersionReleaseStatus      string `json:"version_release_status"`
	InstallationStatus        string `json:"installation_status"`
	InstallationDesiredState  string `json:"installation_desired_state"`
	SecretBindingStatus       string `json:"secret_binding_status"`
	HealthStatus              string `json:"health_status"`
	ManifestValidationStatus  string `json:"manifest_validation_status"`
}

func policySummaryJSON(summary policySummary) (string, error) {
	payload, err := json.Marshal(summary)
	if err != nil {
		return "", fmt.Errorf("%w: guidance policy summary", errs.ErrInvalidArgument)
	}
	return string(payload), nil
}

func queryMeta(meta value.CommandMeta) *packagesv1.QueryMeta {
	return &packagesv1.QueryMeta{
		Actor: &packagesv1.Actor{
			Type: meta.Actor.Type,
			Id:   meta.Actor.ID,
		},
		RequestContext: &packagesv1.RequestContext{Source: callerID},
	}
}

func packageVersionRequest(meta value.CommandMeta, versionID string) *packagesv1.GetPackageVersionRequest {
	return &packagesv1.GetPackageVersionRequest{
		Meta:             queryMeta(meta),
		PackageVersionId: versionID,
	}
}

func packageManifestRequest(meta value.CommandMeta, versionID string) *packagesv1.GetPackageManifestRequest {
	return &packagesv1.GetPackageManifestRequest{
		Meta:             queryMeta(meta),
		PackageVersionId: versionID,
	}
}

func optionalString(text string) *string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func sourceRefString(source *packagesv1.SourceRef) string {
	if source == nil {
		return ""
	}
	parts := []string{strings.TrimSpace(source.GetKind().String()), strings.TrimSpace(source.GetRef())}
	if commit := strings.TrimSpace(source.GetCommitSha()); commit != "" {
		parts = append(parts, commit)
	}
	return strings.Join(parts, ":")
}

func mapPackageHubError(err error) error {
	return grpcclient.MapReadError(err, "package-hub read failed")
}
