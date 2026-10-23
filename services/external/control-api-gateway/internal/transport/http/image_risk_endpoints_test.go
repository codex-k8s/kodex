package httptransport

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TestImageRiskSeverityKeepsClosedMapping checks all supported filter severities, including UNKNOWN, and rejects unspecified, case variants and arbitrary strings. It pins values independently of the production name-table lookup and verifies invalid filters cannot reach either specialized owner RPC.
// TestImageRiskSeverityKeepsClosedMapping проверяет все поддерживаемые severity фильтра, включая UNKNOWN, отклоняет unspecified, иной регистр и произвольные строки. Закрепляет значения независимо от production lookup и проверяет, что неверный фильтр не достигает обоих отдельных owner RPC.
func TestImageRiskSeverityKeepsClosedMapping(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		input generated.ImageVulnerabilitySeverity
		want  cp.ImageVulnerabilitySeverity
	}{
		{input: "CRITICAL", want: cp.ImageVulnerabilitySeverity_IMAGE_VULNERABILITY_SEVERITY_CRITICAL},
		{input: "HIGH", want: cp.ImageVulnerabilitySeverity_IMAGE_VULNERABILITY_SEVERITY_HIGH},
		{input: "MEDIUM", want: cp.ImageVulnerabilitySeverity_IMAGE_VULNERABILITY_SEVERITY_MEDIUM},
		{input: "LOW", want: cp.ImageVulnerabilitySeverity_IMAGE_VULNERABILITY_SEVERITY_LOW},
		{input: "NEGLIGIBLE", want: cp.ImageVulnerabilitySeverity_IMAGE_VULNERABILITY_SEVERITY_NEGLIGIBLE},
		{input: "UNKNOWN", want: cp.ImageVulnerabilitySeverity_IMAGE_VULNERABILITY_SEVERITY_UNKNOWN},
		{input: ""}, {input: "UNSPECIFIED"}, {input: "high"}, {input: " HIGH "},
		{input: "IMAGE_VULNERABILITY_SEVERITY_HIGH"}, {input: "invalid"},
	} {
		t.Run(string(test.input), func(t *testing.T) {
			before := test.input
			actual, ok := imageRiskSeverity(test.input)
			if actual != test.want || ok != (test.want != 0) || test.input != before {
				t.Fatal("severity mapping changed")
			}
			if ok {
				return
			}
			for _, organization := range []bool{false, true} {
				client := &catalogRPCRecorder{}
				server := &Server{control: &controlplaneclient.Client{RoleImages: cp.NewRoleImageServiceClient(client)}}
				writer := httptest.NewRecorder()
				request := httptest.NewRequest("GET", "/synthetic/report", nil)
				if organization {
					server.GetOrganizationImageVulnerabilityReport(writer, request, "imgrec_fixture01", "imgart_fixture01", generated.GetOrganizationImageVulnerabilityReportParams{Severity: &test.input})
				} else {
					server.GetImageVulnerabilityReport(writer, request, "prj_fixture01", "imgrec_fixture01", "imgart_fixture01", generated.GetImageVulnerabilityReportParams{Severity: &test.input})
				}
				if writer.Code != 400 || client.method != "" {
					t.Fatal("invalid severity reached owner")
				}
			}
		})
	}
}

// TestImageRiskHTTPBodyKeepsArtifactOCCAndClosedAction checks the risk input builder against independent valid pins, rejecting unknown action, empty/control reason and mismatched or negative artifact version. It proves transport input validation, not a real owner risk decision, scanner result or promotion.
// TestImageRiskHTTPBodyKeepsArtifactOCCAndClosedAction проверяет risk input builder с независимыми допустимыми pins, отклоняет неизвестный action, пустую/control reason, несовпадающую и отрицательную artifact version. Доказывает transport input validation, не реальное owner risk decision, результат scanner и promotion.
func TestImageRiskHTTPBodyKeepsArtifactOCCAndClosedAction(t *testing.T) {
	body := generated.ImageAdmissionRiskDecisionInput{Action: generated.ImageAdmissionRiskAction("ACCEPT_RISK"), ExpectedArtifactVersion: 2, ExpectedAdmissionRevision: 1, ExpectedRecipeVersion: 1, ExpectedRecipeGeneration: 1, ExpectedBuildAttempt: 1, PolicyRevision: 1, Reason: "Owner decision"}
	body.ExpectedBuildRef = "imgbld_12345678"
	body.ManifestDigest = "sha256:" + strings.Repeat("a", 64)
	body.PriorEvidenceManifestDigest = "sha256:" + strings.Repeat("b", 64)
	body.VulnerabilityEvidenceSha256 = strings.Repeat("c", 64)
	body.ProjectionSha256 = strings.Repeat("d", 64)
	body.PriorAdmissionReceiptSha256 = strings.Repeat("e", 64)
	body.PolicySha256 = strings.Repeat("f", 64)
	version := int64(2)
	for _, name := range []string{"valid", "unknown_action", "missing_reason", "control_reason", "wrong_occ", "negative_version"} {
		t.Run(name, func(t *testing.T) {
			in := body
			switch name {
			case "unknown_action":
				in.Action = "ACCEPT"
			case "missing_reason":
				in.Reason = ""
			case "control_reason":
				in.Reason = "raw\nreason"
			case "wrong_occ":
				in.ExpectedArtifactVersion++
			case "negative_version":
				in.ExpectedArtifactVersion = -1
			}
			request, ok := imageRiskRequest(in, &cp.MutationContext{ExpectedVersion: &version}, "imgrec_12345678", "imgart_12345678")
			if (name == "valid") != ok {
				t.Fatal("risk mutation accepted invalid shape")
			}
			if ok && (request.GetArtifactRef() != "imgart_12345678" || request.GetExpectedArtifactVersion() != 2) {
				t.Fatal("route/OCC lost")
			}
		})
	}
}

// TestImageVulnerabilityHTTPUnavailableIsExplicitAndScoped keeps an unavailable organization report explicit with allocated empty findings, while rejecting foreign scope/ref and contradictory READY or row metadata. All replies are in-memory; no scanner or stored report is consulted.
// TestImageVulnerabilityHTTPUnavailableIsExplicitAndScoped сохраняет явный unavailable organization report с выделенным пустым findings, отклоняет чужие scope/ref и противоречивые READY или rows metadata. Ответы in-memory; scanner и сохранённый отчёт не используются.
func TestImageVulnerabilityHTTPUnavailableIsExplicitAndScoped(t *testing.T) {
	base := &cp.ImageVulnerabilityReport{ScopeKind: cp.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_ORGANIZATION, OrganizationRef: "org_12345678", RecipeRef: "imgrec_12345678", ArtifactRef: "imgart_12345678", ArtifactVersion: 2, State: cp.ImageVulnerabilityReportState_IMAGE_VULNERABILITY_REPORT_STATE_UNAVAILABLE, NextActions: []string{"REBUILD_FOR_REPORT"}}
	for _, name := range []string{"valid", "mixed_project", "foreign_recipe", "ready_missing_counts", "unavailable_rows"} {
		t.Run(name, func(t *testing.T) {
			r := proto.Clone(base).(*cp.ImageVulnerabilityReport)
			var rows []*cp.ImageVulnerabilityFinding
			switch name {
			case "mixed_project":
				r.ProjectRef = "prj_12345678"
			case "foreign_recipe":
				r.RecipeRef = "imgrec_foreign1"
			case "ready_missing_counts":
				r.State = cp.ImageVulnerabilityReportState_IMAGE_VULNERABILITY_REPORT_STATE_READY
				r.Complete = true
				r.Version = 1
			case "unavailable_rows":
				rows = []*cp.ImageVulnerabilityFinding{{Ref: "unsafe"}}
			}
			w := httptest.NewRecorder()
			writeImageVulnerabilityReport(w, r, rows, &cp.PageInfo{}, base.RecipeRef, base.ArtifactRef, "")
			if name == "valid" {
				if w.Code != 200 {
					t.Fatal("unavailable report rejected", w.Body.String())
				}
				var response generated.ImageVulnerabilityReportResponse
				if json.Unmarshal(w.Body.Bytes(), &response) != nil || response.Report.State != "UNAVAILABLE" || response.Report.ProjectRef != "" || response.Findings == nil || strings.Contains(w.Body.String(), "IMAGE_VULNERABILITY_REPORT_STATE_") {
					t.Fatal("public report projection invalid")
				}
			} else if w.Code != 502 {
				t.Fatal("mixed upstream owner/shape emitted")
			}
		})
	}
}

// TestImageRiskPublicEnumNormalizationDoesNotRewriteReason verifies enum normalization changes the action field but preserves human reason text even when it resembles an enum prefix. This checks public projection, not approval authority.
// TestImageRiskPublicEnumNormalizationDoesNotRewriteReason проверяет, что enum normalization меняет action, но сохраняет human reason, даже похожую на enum prefix. Это публичная проекция, не approval authority.
func TestImageRiskPublicEnumNormalizationDoesNotRewriteReason(t *testing.T) {
	in := &cp.ImageAdmissionRiskDecision{ScopeKind: cp.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_ORGANIZATION, Action: cp.ImageAdmissionRiskAction_IMAGE_ADMISSION_RISK_ACTION_ACCEPT_RISK, Reason: "IMAGE_ADMISSION_RISK_ACTION_REJECT_RISK"}
	out, ok := imageRiskPublic[generated.ImageAdmissionRiskDecision](in)
	if !ok || out.Action != "ACCEPT_RISK" || out.Reason != in.Reason {
		t.Fatal("closed enum mapper rewrote human text")
	}
}

// TestImageVulnerabilityHTTPReadyUsesCanonicalSafeFinding builds an actual canonical projection from a synthetic scanner-shaped fixture and checks READY public output plus malformed scope, advisory URL, severity, counts and duplicate rows. It exercises canonical code locally without running an external scanner or admitting an image.
// TestImageVulnerabilityHTTPReadyUsesCanonicalSafeFinding строит настоящую canonical projection из synthetic scanner-shaped fixture и проверяет READY public output, неверные scope, advisory URL, severity, counts и duplicate rows. Выполняет canonical code локально без внешнего scanner и допуска образа.
func TestImageVulnerabilityHTTPReadyUsesCanonicalSafeFinding(t *testing.T) {
	policy := strings.Repeat("a", 64)
	image := "sha256:" + strings.Repeat("b", 64)
	raw := []byte(`{"matches":[{"artifact":{"name":"synthetic-package","version":"1.0.0","type":"go-module"},"vulnerability":{"id":"CVE-2026-12345","severity":"High","fix":{"state":"fixed","versions":["1.0.1"]}}}],"ignoredMatches":[],"kodexPolicy":{"schema":"kodex.dev/fix-available-high-or-critical/v1","policyRevision":1,"policySHA256":"` + policy + `","highOrCriticalMatchCount":1,"blockingMatchCount":1,"unresolvedNoFixMatchCount":0}}`)
	canonical, err := runtimecontract.ProjectImageVulnerabilityReport(raw, runtimecontract.ImageVulnerabilityReport{ArtifactRef: "imgart_12345678", ImageDigest: image, SBOMSHA256: strings.Repeat("c", 64), ScopeKind: "ORGANIZATION", OrganizationRef: "org_12345678", RecipeRef: "imgrec_12345678", RecipeVersion: 1, RecipeGeneration: 1, BuildRef: "imgbld_12345678", BuildVersion: 3, BuildAttempt: 1, PolicyRevision: 1, PolicySHA256: policy})
	if err != nil {
		t.Fatal(err)
	}
	projection, err := runtimecontract.CanonicalImageVulnerabilityReport(canonical)
	if err != nil {
		t.Fatal(err)
	}
	r := &cp.ImageVulnerabilityReport{ArtifactRef: canonical.ArtifactRef, ManifestDigest: image, ArtifactVersion: 3, AdmissionRevision: 1, SourceAdmissionRevision: 1, Version: 1, VulnerabilityEvidenceSha256: canonical.ReportSHA256, SbomSha256: canonical.SBOMSHA256, ProjectionSha256: runtimecontract.ImageVulnerabilitySHA256(projection), PriorAdmissionReceiptSha256: strings.Repeat("d", 64), PriorEvidenceManifestDigest: "sha256:" + strings.Repeat("e", 64), ScopeKind: cp.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_ORGANIZATION, OrganizationRef: canonical.OrganizationRef, RecipeRef: canonical.RecipeRef, RecipeVersion: 1, RecipeGeneration: 1, BuildRef: canonical.BuildRef, BuildVersion: 3, BuildAttempt: 1, PolicyRevision: 1, PolicySha256: policy, MatchCount: 1, UniqueAdvisoryCount: 1, BlockingMatchCount: 1, State: cp.ImageVulnerabilityReportState_IMAGE_VULNERABILITY_REPORT_STATE_READY, Complete: true}
	for _, c := range canonical.SeverityCounts {
		r.SeverityCounts = append(r.SeverityCounts, &cp.ImageVulnerabilitySeverityCount{Severity: cp.ImageVulnerabilitySeverity(cp.ImageVulnerabilitySeverity_value["IMAGE_VULNERABILITY_SEVERITY_"+c.Severity]), MatchCount: c.MatchCount})
	}
	f := canonical.Findings[0]
	row := &cp.ImageVulnerabilityFinding{Ref: f.Ref, PackageName: f.PackageName, InstalledVersion: f.InstalledVersion, Ecosystem: f.Ecosystem, AdvisoryId: f.AdvisoryID, AdvisoryKind: cp.ImageVulnerabilityAdvisoryKind_IMAGE_VULNERABILITY_ADVISORY_KIND_CVE, AdvisoryUrl: f.AdvisoryURL, Severity: cp.ImageVulnerabilitySeverity_IMAGE_VULNERABILITY_SEVERITY_HIGH, FixState: cp.ImageVulnerabilityFixState_IMAGE_VULNERABILITY_FIX_STATE_FIXED, FixedVersions: f.FixedVersions, Blocking: true, Occurrences: 1}
	for _, name := range []string{"valid", "foreign_org", "unsafe_url", "unknown_severity", "wrong_counts", "duplicate_row"} {
		t.Run(name, func(t *testing.T) {
			report := proto.Clone(r).(*cp.ImageVulnerabilityReport)
			finding := proto.Clone(row).(*cp.ImageVulnerabilityFinding)
			rows := []*cp.ImageVulnerabilityFinding{finding}
			switch name {
			case "foreign_org":
				report.OrganizationRef = "invalid"
			case "unsafe_url":
				finding.AdvisoryUrl = "https://invalid.example/private"
			case "unknown_severity":
				finding.Severity = cp.ImageVulnerabilitySeverity(999)
			case "wrong_counts":
				report.MatchCount = 2
			case "duplicate_row":
				rows = append(rows, finding)
			}
			w := httptest.NewRecorder()
			writeImageVulnerabilityReport(w, report, rows, &cp.PageInfo{}, r.RecipeRef, r.ArtifactRef, "")
			if name == "valid" {
				var out generated.ImageVulnerabilityReportResponse
				if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil || len(out.Findings) != 1 || out.Findings[0].PackageName != f.PackageName || out.Findings[0].Severity != "HIGH" {
					t.Fatal("safe READY report rejected", w.Code)
				}
			} else if w.Code != 502 {
				t.Fatal("invalid report emitted", w.Code)
			}
		})
	}
}

// TestImageRiskHTTPDecisionPreservesPendingInsteadOfPromoting projects a synthetic exact risk-decision/artifact/attempt tuple and requires the public admission verdict and attempt to remain PENDING. Foreign digest/attempt or unknown attempt state fail; success is not a signed live risk approval or completed admission.
// TestImageRiskHTTPDecisionPreservesPendingInsteadOfPromoting проецирует synthetic exact risk-decision/artifact/attempt tuple и требует сохранения PENDING у публичных verdict и attempt. Чужие digest/attempt и неизвестный attempt state отклоняются; успех не является signed live approval и завершённым admission.
func TestImageRiskHTTPDecisionPreservesPendingInsteadOfPromoting(t *testing.T) {
	in := &cp.DecideOrganizationImageAdmissionRiskRequest{RecipeRef: "imgrec_12345678", ArtifactRef: "imgart_12345678", ExpectedArtifactVersion: 3, ExpectedAdmissionRevision: 1, ExpectedRecipeVersion: 1, ExpectedRecipeGeneration: 1, ExpectedBuildRef: "imgbld_12345678", ExpectedBuildAttempt: 1, ManifestDigest: "sha256:" + strings.Repeat("a", 64), VulnerabilityEvidenceSha256: strings.Repeat("b", 64), PriorAdmissionReceiptSha256: strings.Repeat("c", 64), PriorEvidenceManifestDigest: "sha256:" + strings.Repeat("d", 64), PolicyRevision: 1, PolicySha256: strings.Repeat("e", 64), Action: cp.ImageAdmissionRiskAction_IMAGE_ADMISSION_RISK_ACTION_ACCEPT_RISK, Reason: "Human reviewed fixed report"}
	d := &cp.ImageAdmissionRiskDecision{Ref: "imgrisk_12345678", Version: 1, ArtifactVersion: 3, AdmissionRevision: 1, RecipeVersion: 1, RecipeGeneration: 1, BuildRef: in.ExpectedBuildRef, BuildVersion: 3, BuildAttempt: 1, ArtifactRef: in.ArtifactRef, RecipeRef: in.RecipeRef, ManifestDigest: in.ManifestDigest, VulnerabilityEvidenceSha256: in.VulnerabilityEvidenceSha256, PriorAdmissionReceiptSha256: in.PriorAdmissionReceiptSha256, PriorEvidenceManifestDigest: in.PriorEvidenceManifestDigest, PolicyRevision: 1, PolicySha256: in.PolicySha256, Action: in.Action, Reason: in.Reason, ScopeKind: cp.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_ORGANIZATION, OrganizationRef: "org_12345678", DecidedByActorRef: "usr_12345678", DecidedAt: timestamppb.New(time.Now().UTC()), BindingSha256: strings.Repeat("f", 64)}
	try := &cp.ImageAdmissionAttempt{Ref: "imgadm_12345678", ArtifactRef: in.ArtifactRef, DecisionRef: d.Ref, Number: 2, Version: 1, State: cp.ImageAdmissionAttemptState_IMAGE_ADMISSION_ATTEMPT_STATE_PENDING}
	a := &cp.ImageArtifact{Ref: in.ArtifactRef, RecipeRef: in.RecipeRef, ScopeKind: d.ScopeKind, OrganizationRef: d.OrganizationRef, Version: 4, ManifestDigest: in.ManifestDigest, RecipeGeneration: 1, BuildRef: in.ExpectedBuildRef, BuildAttempt: 1, PolicyRevision: 1, PolicySha256: in.PolicySha256, AdmissionRevision: 1, VulnerabilityEvidenceSha256: in.VulnerabilityEvidenceSha256, AdmissionVerdict: cp.ImageAdmissionVerdict_IMAGE_ADMISSION_VERDICT_PENDING, RiskDecision: d, AdmissionAttempt: try}
	for _, name := range []string{"valid", "foreign_digest", "wrong_attempt", "unknown_attempt_state"} {
		t.Run(name, func(t *testing.T) {
			artifact := proto.Clone(a).(*cp.ImageArtifact)
			attempt := proto.Clone(try).(*cp.ImageAdmissionAttempt)
			switch name {
			case "foreign_digest":
				artifact.ManifestDigest = "sha256:" + strings.Repeat("0", 64)
			case "wrong_attempt":
				attempt.Ref = "imgadm_foreign1"
			case "unknown_attempt_state":
				artifact.AdmissionAttempt.State = cp.ImageAdmissionAttemptState(999)
			}
			w := httptest.NewRecorder()
			writeImageRiskDecision(w, d, attempt, artifact, in, "")
			if name == "valid" {
				var out generated.ImageAdmissionRiskDecisionResponse
				if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil || out.AdmissionAttempt == nil || out.AdmissionAttempt.State != "PENDING" || out.Artifact.AdmissionVerdict != "PENDING" {
					t.Fatal("risk decision fabricated approval or rejected valid tuple", w.Code)
				}
			} else if w.Code != 502 {
				t.Fatal("foreign risk tuple emitted", w.Code)
			}
		})
	}
}
