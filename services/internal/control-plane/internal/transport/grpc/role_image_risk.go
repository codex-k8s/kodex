package grpc

import (
	"context"
	"strings"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	repo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/roleimage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

func (server *RoleImageServer) GetOrganizationImageVulnerabilityReport(ctx context.Context, r *cp.GetOrganizationImageVulnerabilityReportRequest) (*cp.GetOrganizationImageVulnerabilityReportResponse, error) {
	p, err := roleImagePrincipal(ctx, cp.RoleImageService_GetOrganizationImageVulnerabilityReport_FullMethodName)
	if err != nil {
		return nil, err
	}
	f := repo.VulnerabilityReportFilter{ScopeKind: "ORGANIZATION", RecipeRef: r.GetRecipeRef(), ArtifactRef: r.GetArtifactRef(), Page: page(r.GetPage()), PackageQuery: r.GetPackageQuery(), AdvisoryQuery: r.GetAdvisoryQuery(), BlockingOnly: r.BlockingOnly, ExpectedReportSHA256: r.GetExpectedReportSha256()}
	if r.Severity != nil {
		f.Severity = strings.TrimPrefix(r.GetSeverity().String(), "IMAGE_VULNERABILITY_SEVERITY_")
	}
	result, err := server.service.GetVulnerabilityReport(ctx, p, f)
	if err != nil {
		return nil, transportError(err)
	}
	return &cp.GetOrganizationImageVulnerabilityReportResponse{Report: castImageVulnerabilityReport(result), Findings: castImageVulnerabilityFindings(result.Findings), Page: &cp.PageInfo{NextPageToken: result.NextPageToken}}, nil
}

func (server *RoleImageServer) GetImageVulnerabilityReport(ctx context.Context, r *cp.GetImageVulnerabilityReportRequest) (*cp.GetImageVulnerabilityReportResponse, error) {
	p, err := roleImagePrincipal(ctx, cp.RoleImageService_GetImageVulnerabilityReport_FullMethodName)
	if err != nil {
		return nil, err
	}
	f := repo.VulnerabilityReportFilter{ScopeKind: "PROJECT", ProjectRef: r.GetProjectRef(), RecipeRef: r.GetRecipeRef(), ArtifactRef: r.GetArtifactRef(), Page: page(r.GetPage()), PackageQuery: r.GetPackageQuery(), AdvisoryQuery: r.GetAdvisoryQuery(), BlockingOnly: r.BlockingOnly, ExpectedReportSHA256: r.GetExpectedReportSha256()}
	if r.Severity != nil {
		f.Severity = strings.TrimPrefix(r.GetSeverity().String(), "IMAGE_VULNERABILITY_SEVERITY_")
	}
	result, err := server.service.GetVulnerabilityReport(ctx, p, f)
	if err != nil {
		return nil, transportError(err)
	}
	return &cp.GetImageVulnerabilityReportResponse{Report: castImageVulnerabilityReport(result), Findings: castImageVulnerabilityFindings(result.Findings), Page: &cp.PageInfo{NextPageToken: result.NextPageToken}}, nil
}

type imageRiskRequest interface {
	GetMutation() *cp.MutationContext
	GetRecipeRef() string
	GetArtifactRef() string
	GetExpectedArtifactVersion() uint64
	GetExpectedAdmissionRevision() uint64
	GetExpectedRecipeVersion() uint64
	GetExpectedRecipeGeneration() uint64
	GetExpectedBuildRef() string
	GetExpectedBuildAttempt() uint32
	GetManifestDigest() string
	GetVulnerabilityEvidenceSha256() string
	GetProjectionSha256() string
	GetPriorAdmissionReceiptSha256() string
	GetPriorEvidenceManifestDigest() string
	GetPolicyRevision() uint64
	GetPolicySha256() string
	GetAction() cp.ImageAdmissionRiskAction
	GetReason() string
}

func trimEnum(value, prefix string) string { return strings.TrimPrefix(value, prefix) }

func imageRiskInput(r imageRiskRequest) repo.AdmissionRiskInput {
	return repo.AdmissionRiskInput{Mutation: mutation(r.GetMutation()), RecipeRef: r.GetRecipeRef(), ArtifactRef: r.GetArtifactRef(), ExpectedArtifactVersion: r.GetExpectedArtifactVersion(), ExpectedAdmissionRevision: r.GetExpectedAdmissionRevision(), ExpectedRecipeVersion: r.GetExpectedRecipeVersion(), ExpectedRecipeGeneration: r.GetExpectedRecipeGeneration(), ExpectedBuildRef: r.GetExpectedBuildRef(), ExpectedBuildAttempt: r.GetExpectedBuildAttempt(), ManifestDigest: r.GetManifestDigest(), VulnerabilityEvidenceSHA256: r.GetVulnerabilityEvidenceSha256(), ProjectionSHA256: r.GetProjectionSha256(), PriorAdmissionReceiptSHA256: r.GetPriorAdmissionReceiptSha256(), PriorEvidenceManifestDigest: r.GetPriorEvidenceManifestDigest(), PolicyRevision: r.GetPolicyRevision(), PolicySHA256: r.GetPolicySha256(), Action: trimEnum(r.GetAction().String(), "IMAGE_ADMISSION_RISK_ACTION_"), Reason: r.GetReason()}
}

func (server *RoleImageServer) DecideOrganizationImageAdmissionRisk(ctx context.Context, r *cp.DecideOrganizationImageAdmissionRiskRequest) (*cp.DecideOrganizationImageAdmissionRiskResponse, error) {
	p, err := roleImagePrincipal(ctx, cp.RoleImageService_DecideOrganizationImageAdmissionRisk_FullMethodName)
	if err != nil {
		return nil, err
	}
	in := imageRiskInput(r)
	in.Principal = p
	in.ScopeKind = "ORGANIZATION"
	result, err := server.service.DecideAdmissionRisk(ctx, in)
	if err != nil {
		return nil, transportError(err)
	}
	return &cp.DecideOrganizationImageAdmissionRiskResponse{Decision: castImageRiskDecision(&result.Decision), AdmissionAttempt: castImageAdmissionAttempt(result.AdmissionAttempt), Artifact: castImageArtifact(result.Artifact)}, nil
}

func (server *RoleImageServer) DecideImageAdmissionRisk(ctx context.Context, r *cp.DecideImageAdmissionRiskRequest) (*cp.DecideImageAdmissionRiskResponse, error) {
	p, err := roleImagePrincipal(ctx, cp.RoleImageService_DecideImageAdmissionRisk_FullMethodName)
	if err != nil {
		return nil, err
	}
	in := imageRiskInput(r)
	in.Principal = p
	in.ScopeKind = "PROJECT"
	in.ProjectRef = r.GetProjectRef()
	result, err := server.service.DecideAdmissionRisk(ctx, in)
	if err != nil {
		return nil, transportError(err)
	}
	return &cp.DecideImageAdmissionRiskResponse{Decision: castImageRiskDecision(&result.Decision), AdmissionAttempt: castImageAdmissionAttempt(result.AdmissionAttempt), Artifact: castImageArtifact(result.Artifact)}, nil
}

func castImageVulnerabilityReport(value entity.ImageVulnerabilityReport) *cp.ImageVulnerabilityReport {
	a := value.Artifact
	r := value.Report
	result := &cp.ImageVulnerabilityReport{ScopeKind: cp.RuntimeResourceScopeKind(cp.RuntimeResourceScopeKind_value["RUNTIME_RESOURCE_SCOPE_KIND_"+a.ScopeKind]), OrganizationRef: a.OrganizationRef, ProjectRef: a.ProjectRef, RecipeRef: a.RecipeRef, RecipeVersion: a.RecipeVersion, RecipeGeneration: a.RecipeGeneration, BuildRef: a.BuildRef, BuildVersion: a.BuildVersion, BuildAttempt: a.BuildAttempt, ArtifactRef: a.Ref, ArtifactVersion: a.Version, ManifestDigest: a.ManifestDigest, AdmissionRevision: a.AdmissionRevision, SourceAdmissionRevision: value.AdmissionRevision, PriorAdmissionReceiptSha256: value.AdmissionReceiptSHA256, PriorEvidenceManifestDigest: value.EvidenceManifestDigest, VulnerabilityEvidenceSha256: a.VulnerabilityEvidenceSHA256, SbomSha256: a.SBOMSHA256, PolicyRevision: a.PolicyRevision, PolicySha256: a.PolicySHA256, State: cp.ImageVulnerabilityReportState_IMAGE_VULNERABILITY_REPORT_STATE_UNAVAILABLE, NextActions: value.NextActions}
	if value.Available {
		result.State = cp.ImageVulnerabilityReportState_IMAGE_VULNERABILITY_REPORT_STATE_READY
		result.Version = value.AdmissionRevision
		result.ProjectionSha256 = value.ProjectionSHA256
		result.Complete = true
		result.MatchCount = r.MatchCount
		result.UniqueAdvisoryCount = r.UniqueAdvisoryCount
		result.BlockingMatchCount = r.BlockingMatchCount
		result.UnresolvedNoFixMatchCount = r.UnresolvedNoFixMatchCount
		result.SuppressedMatchCount = r.SuppressedMatchCount
		for _, count := range r.SeverityCounts {
			result.SeverityCounts = append(result.SeverityCounts, &cp.ImageVulnerabilitySeverityCount{Severity: cp.ImageVulnerabilitySeverity(cp.ImageVulnerabilitySeverity_value["IMAGE_VULNERABILITY_SEVERITY_"+count.Severity]), MatchCount: count.MatchCount})
		}
	}
	return result
}

func castImageVulnerabilityFindings(values []runtimecontract.ImageVulnerabilityFinding) []*cp.ImageVulnerabilityFinding {
	result := make([]*cp.ImageVulnerabilityFinding, 0, len(values))
	for _, f := range values {
		result = append(result, &cp.ImageVulnerabilityFinding{Ref: f.Ref, PackageName: f.PackageName, InstalledVersion: f.InstalledVersion, Ecosystem: f.Ecosystem, AdvisoryId: f.AdvisoryID, AdvisoryKind: cp.ImageVulnerabilityAdvisoryKind(cp.ImageVulnerabilityAdvisoryKind_value["IMAGE_VULNERABILITY_ADVISORY_KIND_"+f.AdvisoryKind]), AdvisoryUrl: f.AdvisoryURL, Severity: cp.ImageVulnerabilitySeverity(cp.ImageVulnerabilitySeverity_value["IMAGE_VULNERABILITY_SEVERITY_"+f.Severity]), FixState: cp.ImageVulnerabilityFixState(cp.ImageVulnerabilityFixState_value["IMAGE_VULNERABILITY_FIX_STATE_"+f.FixState]), FixedVersions: append([]string(nil), f.FixedVersions...), Blocking: f.Blocking, Ignored: f.Ignored, Occurrences: f.Occurrences})
	}
	return result
}

func castImageRiskDecision(d *entity.ImageAdmissionRiskDecision) *cp.ImageAdmissionRiskDecision {
	if d == nil {
		return nil
	}
	return &cp.ImageAdmissionRiskDecision{Ref: d.Ref, Version: 1, Action: cp.ImageAdmissionRiskAction(cp.ImageAdmissionRiskAction_value["IMAGE_ADMISSION_RISK_ACTION_"+d.Action]), Reason: d.Reason, DecidedByActorRef: d.ActorRef, DecidedAt: timestamp(d.DecidedAt), BindingSha256: d.BindingSHA256, ScopeKind: cp.RuntimeResourceScopeKind(cp.RuntimeResourceScopeKind_value["RUNTIME_RESOURCE_SCOPE_KIND_"+d.ScopeKind]), OrganizationRef: d.OrganizationRef, ProjectRef: d.ProjectRef, RecipeRef: d.RecipeRef, RecipeVersion: d.RecipeVersion, RecipeGeneration: d.RecipeGeneration, BuildRef: d.BuildRef, BuildVersion: d.BuildVersion, BuildAttempt: d.BuildAttempt, ArtifactRef: d.ArtifactRef, ArtifactVersion: d.ArtifactVersion, ManifestDigest: d.ManifestDigest, AdmissionRevision: d.AdmissionRevision, SourceAdmissionRevision: d.AdmissionRevision, PriorAdmissionReceiptSha256: d.PriorAdmissionReceiptSHA256, PriorEvidenceManifestDigest: d.PriorEvidenceManifestDigest, VulnerabilityEvidenceSha256: d.VulnerabilityEvidenceSHA256, SbomSha256: d.SBOMSHA256, PolicyRevision: d.PolicyRevision, PolicySha256: d.PolicySHA256}
}

func castImageAdmissionAttempt(a *entity.ImageAdmissionAttempt) *cp.ImageAdmissionAttempt {
	if a == nil {
		return nil
	}
	return &cp.ImageAdmissionAttempt{Ref: a.Ref, Version: a.Version, Number: a.Number, State: cp.ImageAdmissionAttemptState(cp.ImageAdmissionAttemptState_value["IMAGE_ADMISSION_ATTEMPT_STATE_"+a.State]), ArtifactRef: a.ArtifactRef, DecisionRef: a.RiskDecisionRef, Fence: a.Fence, AdmissionReceiptSha256: a.AdmissionReceiptSHA256, EvidenceManifestDigest: a.EvidenceManifestDigest}
}
