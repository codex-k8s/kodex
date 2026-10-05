package imageowner

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	sharedclient "github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func admissionRiskFixture(t *testing.T, risk bool) (Claim, AdmissionEvidence) {
	t.Helper()
	hex := strings.Repeat("a", 64)
	binding := runtimecontract.ImageVulnerabilityReport{ArtifactRef: "imgart_abcdefgh", ImageDigest: "sha256:" + hex,
		SBOMSHA256: hex, ScopeKind: "ORGANIZATION", OrganizationRef: "org_abcdefgh", RecipeRef: "imgrec_abcdefgh",
		RecipeVersion: 2, RecipeGeneration: 3, BuildRef: "imgbld_abcdefgh", BuildVersion: 4, BuildAttempt: 5,
		PolicyRevision: 6, PolicySHA256: hex}
	matches := []any{}
	blocking := 0
	if risk {
		matches = append(matches, map[string]any{"artifact": map[string]any{"name": "example/pkg", "version": "v1.0.0", "type": "go-module"},
			"vulnerability": map[string]any{"id": "GO-2026-1234", "severity": "High", "fix": map[string]any{"state": "fixed", "versions": []string{"v1.0.1"}}}})
		blocking = 1
	}
	source, err := json.Marshal(map[string]any{"matches": matches, "ignoredMatches": []any{}, "kodexPolicy": map[string]any{
		"schema": "kodex.dev/fix-available-high-or-critical/v1", "policyRevision": 6, "policySHA256": hex,
		"highOrCriticalMatchCount": blocking, "blockingMatchCount": blocking, "unresolvedNoFixMatchCount": 0}})
	if err != nil {
		t.Fatal(err)
	}
	report, err := runtimecontract.ProjectImageVulnerabilityReport(source, binding)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := runtimecontract.CanonicalImageVulnerabilityReport(report)
	if err != nil {
		t.Fatal(err)
	}
	claim := Claim{ScopeKind: binding.ScopeKind, OrganizationRef: binding.OrganizationRef, ArtifactID: binding.ArtifactRef,
		Version: 7, Fence: 8, AuthorityGeneration: 9, ClaimToken: "private-fixture-token", ExpiresAt: time.Date(2026, 10, 5, 6, 0, 0, 0, time.UTC),
		RecipeID: binding.RecipeRef, RecipeVersion: binding.RecipeVersion, RecipeGeneration: binding.RecipeGeneration,
		BuildID: binding.BuildRef, BuildVersion: binding.BuildVersion, BuildAttempt: binding.BuildAttempt,
		ManifestDigest: binding.ImageDigest, PolicyRevision: binding.PolicyRevision, PolicySHA256: binding.PolicySHA256,
		ImmutableBuildSHA256: hex, ProvenanceSHA256: hex, SpecSHA256: hex, Platforms: []string{"linux/amd64"},
		AdmissionAttemptRef: "imgadm_abcdefgh", AdmissionAttempt: 2}
	evidence := AdmissionEvidence{SBOMSHA256: report.SBOMSHA256, VulnerabilityEvidenceSHA256: report.ReportSHA256,
		VulnerabilityReportJSON: string(raw), VulnerabilityReportProjectionSHA256: runtimecontract.ImageVulnerabilitySHA256(raw),
		Accepted: true, AdmissionReceiptSHA256: hex, AdmissionReceiptOCIManifestDigest: "sha256:" + hex}
	if risk {
		decision := runtimecontract.ImageRiskAcceptance{Schema: runtimecontract.ImageRiskAcceptanceSchema, DecisionRef: "imgrisk_abcdefgh",
			DecisionVersion: 1, Action: "ACCEPT_RISK", ScopeKind: binding.ScopeKind, OrganizationRef: binding.OrganizationRef,
			ArtifactRef: binding.ArtifactRef, ImageDigest: binding.ImageDigest, ReportSHA256: report.ReportSHA256,
			ProjectionSHA256: evidence.VulnerabilityReportProjectionSHA256, SourceAdmissionRevision: 1,
			SourceAdmissionReceiptSHA256: hex, SourceEvidenceManifestDigest: "sha256:" + hex,
			RecipeRef: binding.RecipeRef, RecipeVersion: binding.RecipeVersion, RecipeGeneration: binding.RecipeGeneration,
			BuildRef: binding.BuildRef, BuildVersion: binding.BuildVersion, BuildAttempt: binding.BuildAttempt,
			PolicyRevision: binding.PolicyRevision, PolicySHA256: binding.PolicySHA256,
			Reason: "Риск принят для точного образа", DecidedByActorRef: "act_abcdefgh", DecidedAt: "2026-10-05T05:00:00Z"}
		riskRaw, err := runtimecontract.CanonicalImageRiskAcceptance(decision)
		if err != nil {
			t.Fatal(err)
		}
		claim.RiskAcceptanceJSON, claim.RiskAcceptanceSHA256 = string(riskRaw), runtimecontract.ImageVulnerabilitySHA256(riskRaw)
		claim.SourceAdmissionReceiptSHA256, claim.SourceEvidenceManifestDigest, claim.SourceAdmissionRevision = hex, "sha256:"+hex, 1
		evidence.RiskAcceptanceSHA256 = claim.RiskAcceptanceSHA256
	}
	return claim, evidence
}

func TestAdmissionRiskClaimRejectsUnknownMissingAndChangedPins(t *testing.T) {
	claim, evidence := admissionRiskFixture(t, true)
	if ValidateAdmissionClaim(claim) != nil || ValidateAdmissionEvidence(claim, evidence) != nil {
		t.Fatal("exact risk tuple rejected")
	}
	for name, mutate := range map[string]func(*Claim){
		"attempt ref":       func(v *Claim) { v.AdmissionAttemptRef = "imgart_abcdefgh" },
		"attempt number":    func(v *Claim) { v.AdmissionAttempt = 0 },
		"risk digest":       func(v *Claim) { v.RiskAcceptanceSHA256 = strings.Repeat("b", 64) },
		"source receipt":    func(v *Claim) { v.SourceAdmissionReceiptSHA256 = strings.Repeat("b", 64) },
		"source evidence":   func(v *Claim) { v.SourceEvidenceManifestDigest = "sha256:" + strings.Repeat("b", 64) },
		"source revision":   func(v *Claim) { v.SourceAdmissionRevision++ },
		"organization":      func(v *Claim) { v.OrganizationRef = "org_ijklmnop" },
		"project":           func(v *Claim) { v.ScopeKind = "PROJECT"; v.ProjectRef = "prj_abcdefgh" },
		"artifact":          func(v *Claim) { v.ArtifactID = "imgart_ijklmnop" },
		"image":             func(v *Claim) { v.ManifestDigest = "sha256:" + strings.Repeat("b", 64) },
		"recipe":            func(v *Claim) { v.RecipeID = "imgrec_ijklmnop" },
		"recipe version":    func(v *Claim) { v.RecipeVersion++ },
		"recipe generation": func(v *Claim) { v.RecipeGeneration++ },
		"build":             func(v *Claim) { v.BuildID = "imgbld_ijklmnop" },
		"build version":     func(v *Claim) { v.BuildVersion++ },
		"build attempt":     func(v *Claim) { v.BuildAttempt++ },
		"policy":            func(v *Claim) { v.PolicyRevision++ },
		"policy digest":     func(v *Claim) { v.PolicySHA256 = strings.Repeat("b", 64) },
		"unknown private field": func(v *Claim) {
			v.RiskAcceptanceJSON = `{"claimToken":"private-do-not-print",` + v.RiskAcceptanceJSON[1:]
			v.RiskAcceptanceSHA256 = runtimecontract.ImageVulnerabilitySHA256([]byte(v.RiskAcceptanceJSON))
		},
		"duplicate field": func(v *Claim) {
			v.RiskAcceptanceJSON = `{"action":"ACCEPT_RISK",` + v.RiskAcceptanceJSON[1:]
			v.RiskAcceptanceSHA256 = runtimecontract.ImageVulnerabilitySHA256([]byte(v.RiskAcceptanceJSON))
		},
		"noncanonical": func(v *Claim) { v.RiskAcceptanceJSON = " " + v.RiskAcceptanceJSON },
	} {
		t.Run(name, func(t *testing.T) {
			changed := claim
			mutate(&changed)
			err := ValidateAdmissionClaim(changed)
			if err == nil || strings.Contains(err.Error(), "private") {
				t.Fatal("changed risk claim accepted or leaked private data")
			}
		})
	}
	normal, normalEvidence := admissionRiskFixture(t, false)
	if ValidateAdmissionEvidence(normal, normalEvidence) != nil {
		t.Fatal("normal admission rejected")
	}
	normal.SourceAdmissionRevision = 1
	if ValidateAdmissionClaim(normal) == nil {
		t.Fatal("partial risk tuple accepted as normal claim")
	}
	claim.RiskAcceptanceJSON, claim.RiskAcceptanceSHA256, claim.SourceAdmissionReceiptSHA256, claim.SourceEvidenceManifestDigest, claim.SourceAdmissionRevision = "", "", "", "", 0
	evidence.RiskAcceptanceSHA256 = ""
	if ValidateAdmissionEvidence(claim, evidence) == nil {
		t.Fatal("blocking report accepted without risk decision")
	}
}

type admissionRPCStub struct {
	cp.RoleImageServiceClient
	claim   *cp.ClaimImageAdmissionResponse
	record  *cp.RecordImageAdmissionRequest
	corrupt bool
}

func (s *admissionRPCStub) ClaimImageAdmission(context.Context, *cp.ClaimImageAdmissionRequest, ...grpc.CallOption) (*cp.ClaimImageAdmissionResponse, error) {
	return s.claim, nil
}
func (s *admissionRPCStub) RecordImageAdmission(_ context.Context, request *cp.RecordImageAdmissionRequest, _ ...grpc.CallOption) (*cp.RecordImageAdmissionResponse, error) {
	s.record = request
	state := cp.ImageAdmissionAttemptState_IMAGE_ADMISSION_ATTEMPT_STATE_ACCEPTED
	if request.Verdict == cp.ImageAdmissionVerdict_IMAGE_ADMISSION_VERDICT_REJECTED {
		state = cp.ImageAdmissionAttemptState_IMAGE_ADMISSION_ATTEMPT_STATE_REJECTED
	}
	attempt := &cp.ImageAdmissionAttempt{Ref: request.ExpectedAdmissionAttemptRef, Number: request.ExpectedAdmissionAttempt,
		ArtifactRef: request.ImageArtifactRef, Fence: request.ExpectedFence, State: state,
		AdmissionReceiptSha256: request.AdmissionReceiptSha256, EvidenceManifestDigest: request.AdmissionReceiptOciManifestDigest}
	if s.corrupt {
		attempt.Number++
	}
	return &cp.RecordImageAdmissionResponse{ImageArtifact: &cp.ImageArtifact{Ref: request.ImageArtifactRef, Version: request.ExpectedVersion + 1, AdmissionAttempt: attempt}}, nil
}

func TestAdmissionRecordCarriesAttemptAndFullReportAndBindsUnaryDigest(t *testing.T) {
	for _, risk := range []bool{false, true} {
		claim, evidence := admissionRiskFixture(t, risk)
		stub := &admissionRPCStub{}
		client := &Client{shared: &sharedclient.Client{RoleImages: stub}, rpcDeadline: time.Second}
		if err := client.Record(t.Context(), "record-key", claim, evidence); err != nil {
			t.Fatal(err)
		}
		request := stub.record
		if request.ExpectedAdmissionAttemptRef != claim.AdmissionAttemptRef || request.ExpectedAdmissionAttempt != claim.AdmissionAttempt ||
			request.VulnerabilityReportJson != evidence.VulnerabilityReportJSON || request.VulnerabilityReportProjectionSha256 != evidence.VulnerabilityReportProjectionSHA256 || request.RiskAcceptanceSha256 != claim.RiskAcceptanceSHA256 {
			t.Fatal("record lost new immutable inputs")
		}
		raw, _ := proto.MarshalOptions{Deterministic: true}.Marshal(request)
		original := sha256.Sum256(raw)
		for _, mutate := range []func(*cp.RecordImageAdmissionRequest){
			func(v *cp.RecordImageAdmissionRequest) { v.ExpectedAdmissionAttemptRef = "imgadm_ijklmnop" },
			func(v *cp.RecordImageAdmissionRequest) { v.ExpectedAdmissionAttempt++ },
			func(v *cp.RecordImageAdmissionRequest) { v.VulnerabilityReportJson += " " },
			func(v *cp.RecordImageAdmissionRequest) {
				v.VulnerabilityReportProjectionSha256 = strings.Repeat("b", 64)
			},
			func(v *cp.RecordImageAdmissionRequest) { v.RiskAcceptanceSha256 = strings.Repeat("b", 64) },
		} {
			changed := proto.Clone(request).(*cp.RecordImageAdmissionRequest)
			mutate(changed)
			raw, _ := proto.MarshalOptions{Deterministic: true}.Marshal(changed)
			if sha256.Sum256(raw) == original {
				t.Fatal("new input omitted from unary request digest")
			}
		}
		stub.corrupt = true
		if client.Record(t.Context(), "record-key", claim, evidence) == nil {
			t.Fatal("foreign attempt receipt accepted")
		}
		stub.record = nil
		evidence.VulnerabilityEvidenceSHA256 = strings.Repeat("b", 64)
		if client.Record(t.Context(), "record-key", claim, evidence) == nil || stub.record != nil {
			t.Fatal("foreign report reached RPC")
		}
	}
}

func TestAdmissionClaimRetainsRiskAndExactClaimedAttempt(t *testing.T) {
	claim, _ := admissionRiskFixture(t, true)
	artifact := &cp.ImageArtifact{Ref: claim.ArtifactID, Version: claim.Version, ScopeKind: cp.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_ORGANIZATION,
		OrganizationRef: claim.OrganizationRef, RecipeRef: claim.RecipeID, RecipeVersion: claim.RecipeVersion, RecipeGeneration: claim.RecipeGeneration,
		BuildRef: claim.BuildID, BuildVersion: claim.BuildVersion, BuildAttempt: claim.BuildAttempt, ManifestDigest: claim.ManifestDigest,
		PolicyRevision: claim.PolicyRevision, PolicySha256: claim.PolicySHA256, Platforms: []*cp.RoleImagePlatform{{Os: "linux", Architecture: "amd64"}},
		AdmissionAttempt: &cp.ImageAdmissionAttempt{Ref: claim.AdmissionAttemptRef, Number: claim.AdmissionAttempt, ArtifactRef: claim.ArtifactID, Fence: claim.Fence, State: cp.ImageAdmissionAttemptState_IMAGE_ADMISSION_ATTEMPT_STATE_CLAIMED}}
	response := &cp.ClaimImageAdmissionResponse{ImageArtifact: artifact, Fence: claim.Fence, AuthorityGeneration: claim.AuthorityGeneration, ClaimToken: claim.ClaimToken,
		ClaimExpiresAt: timestamppb.New(claim.ExpiresAt), AdmissionAttemptRef: claim.AdmissionAttemptRef, AdmissionAttempt: claim.AdmissionAttempt,
		RiskAcceptanceJson: claim.RiskAcceptanceJSON, RiskAcceptanceSha256: claim.RiskAcceptanceSHA256, SourceAdmissionRevision: claim.SourceAdmissionRevision,
		SourceAdmissionReceiptSha256: claim.SourceAdmissionReceiptSHA256, SourceEvidenceManifestDigest: claim.SourceEvidenceManifestDigest}
	stub := &admissionRPCStub{claim: response}
	client := &Client{shared: &sharedclient.Client{RoleImages: stub}, rpcDeadline: time.Second}
	actual, err := client.Claim(t.Context(), "claim-key")
	if err != nil || actual.RiskAcceptanceJSON != claim.RiskAcceptanceJSON || actual.AdmissionAttemptRef != claim.AdmissionAttemptRef || actual.ClaimToken != claim.ClaimToken {
		t.Fatal("claim dropped original risk or fenced snapshot")
	}
	response.ImageArtifact.AdmissionAttempt.Number++
	if _, err := client.Claim(t.Context(), "claim-key"); err == nil {
		t.Fatal("mismatched claim attempt accepted")
	}
}
