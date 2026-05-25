package packagehub

import (
	"context"
	"errors"
	"strings"
	"testing"

	packagesv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/packages/v1"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/errs"
	agentservice "github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/value"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestResolveGuidanceRefsSelectsActiveInstallationsByScope(t *testing.T) {
	t.Parallel()

	client := newFakePackageHubClient()
	resolver := newTestGuidanceResolver(t, client)
	refs, err := resolver.ResolveGuidanceRefs(context.Background(), agentservice.GuidanceResolutionInput{
		Meta:  testCommandMeta(),
		Scope: value.ScopeRef{Type: "project", Ref: "project-1"},
	})
	if err != nil {
		t.Fatalf("ResolveGuidanceRefs() err = %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("refs len = %d, want 1", len(refs))
	}
	ref := refs[0]
	if ref.PackageInstallationRef != "installation-1" || ref.PackageVersionRef != "version-1" || ref.PackageSlug != "go-guidelines" {
		t.Fatalf("ref = %+v", ref)
	}
	if ref.PolicySummaryJSON == "" || strings.Contains(ref.PolicySummaryJSON, "SKILL.md") {
		t.Fatalf("unsafe policy summary = %q", ref.PolicySummaryJSON)
	}
}

func TestResolveGuidanceRefsDeduplicatesHints(t *testing.T) {
	t.Parallel()

	client := newFakePackageHubClient()
	resolver := newTestGuidanceResolver(t, client)
	refs, err := resolver.ResolveGuidanceRefs(context.Background(), agentservice.GuidanceResolutionInput{
		Meta:  testCommandMeta(),
		Scope: value.ScopeRef{Type: "project", Ref: "project-1"},
		Hints: []value.GuidanceSelectionHint{
			{PackageInstallationRef: "installation-1"},
			{PackageInstallationRef: "installation-1"},
		},
	})
	if err != nil {
		t.Fatalf("ResolveGuidanceRefs() err = %v", err)
	}
	if len(refs) != 1 || client.getInstallationCalls != 1 {
		t.Fatalf("refs/calls = %d/%d, want 1/1", len(refs), client.getInstallationCalls)
	}
}

func TestResolveGuidanceRefsRejectsInactiveInstallation(t *testing.T) {
	t.Parallel()

	client := newFakePackageHubClient()
	client.installations["installation-1"].InstallationStatus = packagesv1.PackageInstallationStatus_PACKAGE_INSTALLATION_STATUS_DISABLED
	resolver := newTestGuidanceResolver(t, client)
	_, err := resolver.ResolveGuidanceRefs(context.Background(), agentservice.GuidanceResolutionInput{
		Meta:  testCommandMeta(),
		Scope: value.ScopeRef{Type: "project", Ref: "project-1"},
		Hints: []value.GuidanceSelectionHint{{PackageInstallationRef: "installation-1"}},
	})
	if !errors.Is(err, errs.ErrPreconditionFailed) {
		t.Fatalf("ResolveGuidanceRefs() err = %v, want %v", err, errs.ErrPreconditionFailed)
	}
}

func TestResolveGuidanceRefsRejectsUnavailablePackage(t *testing.T) {
	t.Parallel()

	client := newFakePackageHubClient()
	client.packages["package-1"].Status = packagesv1.PackageStatus_PACKAGE_STATUS_BLOCKED
	resolver := newTestGuidanceResolver(t, client)
	_, err := resolver.ResolveGuidanceRefs(context.Background(), agentservice.GuidanceResolutionInput{
		Meta:  testCommandMeta(),
		Scope: value.ScopeRef{Type: "project", Ref: "project-1"},
	})
	if !errors.Is(err, errs.ErrPreconditionFailed) {
		t.Fatalf("ResolveGuidanceRefs() err = %v, want %v", err, errs.ErrPreconditionFailed)
	}
}

func TestResolveGuidanceRefsReportsMissingSlug(t *testing.T) {
	t.Parallel()

	client := newFakePackageHubClient()
	resolver := newTestGuidanceResolver(t, client)
	_, err := resolver.ResolveGuidanceRefs(context.Background(), agentservice.GuidanceResolutionInput{
		Meta:  testCommandMeta(),
		Scope: value.ScopeRef{Type: "project", Ref: "project-1"},
		Hints: []value.GuidanceSelectionHint{{PackageSlug: "missing-guidelines"}},
	})
	if !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("ResolveGuidanceRefs() err = %v, want %v", err, errs.ErrNotFound)
	}
}

func TestResolveGuidanceRefsRejectsInvalidScope(t *testing.T) {
	t.Parallel()

	client := newFakePackageHubClient()
	resolver := newTestGuidanceResolver(t, client)
	_, err := resolver.ResolveGuidanceRefs(context.Background(), agentservice.GuidanceResolutionInput{
		Meta:  testCommandMeta(),
		Scope: value.ScopeRef{Type: "unknown", Ref: "project-1"},
	})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("ResolveGuidanceRefs() err = %v, want %v", err, errs.ErrInvalidArgument)
	}
}

func newTestGuidanceResolver(t *testing.T, client *fakePackageHubClient) *GuidanceResolver {
	t.Helper()
	resolver, err := newGuidanceResolver(client, Config{AuthToken: "test-token"})
	if err != nil {
		t.Fatalf("newGuidanceResolver(): %v", err)
	}
	return resolver
}

func testCommandMeta() value.CommandMeta {
	return value.CommandMeta{Actor: value.Actor{Type: "service", ID: "agent-manager"}}
}

type fakePackageHubClient struct {
	packages             map[string]*packagesv1.PackageEntry
	versions             map[string]*packagesv1.PackageVersion
	manifests            map[string]*packagesv1.PackageManifestSnapshot
	installations        map[string]*packagesv1.PackageInstallation
	getInstallationCalls int
}

func newFakePackageHubClient() *fakePackageHubClient {
	return &fakePackageHubClient{
		packages: map[string]*packagesv1.PackageEntry{
			"package-1": {
				Id:               "package-1",
				Slug:             "go-guidelines",
				PackageKind:      packagesv1.PackageKind_PACKAGE_KIND_GUIDANCE,
				Status:           packagesv1.PackageStatus_PACKAGE_STATUS_AVAILABLE,
				TrustStatus:      packagesv1.PackageTrustStatus_PACKAGE_TRUST_STATUS_VERIFIED,
				CommercialStatus: packagesv1.PackageCommercialStatus_PACKAGE_COMMERCIAL_STATUS_FREE,
			},
		},
		versions: map[string]*packagesv1.PackageVersion{
			"version-1": {
				Id:                 "version-1",
				PackageId:          "package-1",
				VersionLabel:       "v1.0.0",
				ManifestDigest:     "sha256:manifest",
				VerificationStatus: packagesv1.PackageVerificationStatus_PACKAGE_VERIFICATION_STATUS_VERIFIED,
				ReleaseStatus:      packagesv1.PackageReleaseStatus_PACKAGE_RELEASE_STATUS_ACTIVE,
				SourceRef: &packagesv1.SourceRef{
					Kind:      packagesv1.PackageVersionSourceRefKind_PACKAGE_VERSION_SOURCE_REF_KIND_GIT_TAG,
					Ref:       "v1.0.0",
					CommitSha: optionalString("abc123"),
				},
			},
		},
		manifests: map[string]*packagesv1.PackageManifestSnapshot{
			"version-1": {
				Id:                   "manifest-1",
				PackageVersionId:     "version-1",
				PayloadJson:          `{"assets":["SKILL.md"]}`,
				ValidationStatus:     packagesv1.PackageManifestValidationStatus_PACKAGE_MANIFEST_VALIDATION_STATUS_VALID,
				ValidationErrorsJson: "[]",
			},
		},
		installations: map[string]*packagesv1.PackageInstallation{
			"installation-1": {
				Id:                  "installation-1",
				PackageId:           "package-1",
				PackageVersionId:    "version-1",
				Scope:               testPackageScope(),
				InstallationStatus:  packagesv1.PackageInstallationStatus_PACKAGE_INSTALLATION_STATUS_ACTIVE,
				DesiredState:        packagesv1.PackageDesiredState_PACKAGE_DESIRED_STATE_PRESENT,
				SecretBindingStatus: packagesv1.PackageSecretBindingStatus_PACKAGE_SECRET_BINDING_STATUS_COMPLETE,
				LastHealthStatus:    packagesv1.PackageHealthStatus_PACKAGE_HEALTH_STATUS_HEALTHY,
			},
		},
	}
}

func (f *fakePackageHubClient) GetPackage(_ context.Context, request *packagesv1.GetPackageRequest, _ ...grpc.CallOption) (*packagesv1.PackageResponse, error) {
	item, ok := f.packages[request.GetPackageId()]
	if !ok {
		return nil, status.Error(codes.NotFound, "missing package")
	}
	return &packagesv1.PackageResponse{PackageEntry: item}, nil
}

func (f *fakePackageHubClient) ListPackages(_ context.Context, request *packagesv1.ListPackagesRequest, _ ...grpc.CallOption) (*packagesv1.ListPackagesResponse, error) {
	items := make([]*packagesv1.PackageEntry, 0, len(f.packages))
	for _, item := range f.packages {
		if request.PackageKind != nil && item.GetPackageKind() != request.GetPackageKind() {
			continue
		}
		if request.Status != nil && item.GetStatus() != request.GetStatus() {
			continue
		}
		if request.Query != nil && !strings.Contains(item.GetSlug(), request.GetQuery()) {
			continue
		}
		items = append(items, item)
	}
	return &packagesv1.ListPackagesResponse{Items: items}, nil
}

func (f *fakePackageHubClient) GetPackageVersion(_ context.Context, request *packagesv1.GetPackageVersionRequest, _ ...grpc.CallOption) (*packagesv1.PackageVersionResponse, error) {
	version, ok := f.versions[request.GetPackageVersionId()]
	if !ok {
		return nil, status.Error(codes.NotFound, "missing version")
	}
	return &packagesv1.PackageVersionResponse{Version: version}, nil
}

func (f *fakePackageHubClient) GetPackageManifest(_ context.Context, request *packagesv1.GetPackageManifestRequest, _ ...grpc.CallOption) (*packagesv1.PackageManifestResponse, error) {
	manifest, ok := f.manifests[request.GetPackageVersionId()]
	if !ok {
		return nil, status.Error(codes.NotFound, "missing manifest")
	}
	return &packagesv1.PackageManifestResponse{Manifest: manifest}, nil
}

func (f *fakePackageHubClient) GetPackageInstallation(_ context.Context, request *packagesv1.GetPackageInstallationRequest, _ ...grpc.CallOption) (*packagesv1.PackageInstallationResponse, error) {
	f.getInstallationCalls++
	installation, ok := f.installations[request.GetInstallationId()]
	if !ok {
		return nil, status.Error(codes.NotFound, "missing installation")
	}
	return &packagesv1.PackageInstallationResponse{Installation: installation}, nil
}

func (f *fakePackageHubClient) ListPackageInstallations(_ context.Context, request *packagesv1.ListPackageInstallationsRequest, _ ...grpc.CallOption) (*packagesv1.ListPackageInstallationsResponse, error) {
	items := make([]*packagesv1.PackageInstallation, 0, len(f.installations))
	for _, installation := range f.installations {
		if request.Scope != nil && (installation.GetScope().GetType() != request.GetScope().GetType() || installation.GetScope().GetRef() != request.GetScope().GetRef()) {
			continue
		}
		if request.PackageId != nil && installation.GetPackageId() != request.GetPackageId() {
			continue
		}
		if request.InstallationStatus != nil && installation.GetInstallationStatus() != request.GetInstallationStatus() {
			continue
		}
		if request.PackageKind != nil {
			item := f.packages[installation.GetPackageId()]
			if item == nil || item.GetPackageKind() != request.GetPackageKind() {
				continue
			}
		}
		items = append(items, installation)
	}
	return &packagesv1.ListPackageInstallationsResponse{Items: items}, nil
}

func testPackageScope() *packagesv1.ScopeRef {
	return &packagesv1.ScopeRef{
		Type: packagesv1.PackageInstallationScopeType_PACKAGE_INSTALLATION_SCOPE_TYPE_PROJECT,
		Ref:  "project-1",
	}
}
