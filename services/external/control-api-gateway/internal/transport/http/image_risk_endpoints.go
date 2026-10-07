package httptransport

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// Проекция использует только отдельные публичные DTO, а не внутренний artifact.
func imageRiskPublic[T any](message proto.Message) (T, bool) {
	var result T
	raw, err := (protojson.MarshalOptions{EmitUnpopulated: true}).Marshal(message)
	if err != nil {
		return result, false
	}
	var object map[string]any
	if json.Unmarshal(raw, &object) != nil || normalizeProtoJSONShape(object, message.ProtoReflect().Descriptor()) != nil {
		return result, false
	}
	var normalizeEnums func(map[string]any)
	normalizeEnums = func(value map[string]any) {
		for key, item := range value {
			if nested, ok := item.(map[string]any); ok {
				normalizeEnums(nested)
			}
			if list, ok := item.([]any); ok {
				for _, child := range list {
					if nested, ok := child.(map[string]any); ok {
						normalizeEnums(nested)
					}
				}
			}
			text, ok := item.(string)
			if !ok {
				continue
			}
			for _, prefix := range []string{"IMAGE_VULNERABILITY_REPORT_STATE_", "IMAGE_VULNERABILITY_SEVERITY_", "IMAGE_VULNERABILITY_ADVISORY_KIND_", "IMAGE_VULNERABILITY_FIX_STATE_", "IMAGE_ADMISSION_RISK_ACTION_", "IMAGE_ADMISSION_ATTEMPT_STATE_"} {
				if strings.HasPrefix(text, prefix) && (key == "state" || key == "severity" || key == "advisoryKind" || key == "fixState" || key == "action") {
					value[key] = strings.TrimPrefix(text, prefix)
				}
			}
		}
	}
	normalizeEnums(object)
	raw, err = json.Marshal(object)
	if err != nil {
		return result, false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	return result, decoder.Decode(&result) == nil
}

func imageRiskOwner(scope cp.RuntimeResourceScopeKind, org, project, expectedProject string) bool {
	expectedScope := cp.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_ORGANIZATION
	if expectedProject != "" {
		expectedScope = cp.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_PROJECT
	}
	return scope == expectedScope && project == expectedProject && validRuntimeResourceScope(runtimeResourceScopeKind(scope.String()), org, project)
}

func validImageRiskReason(reason string) bool {
	return len(reason) > 0 && len(reason) <= 2048 && strings.TrimSpace(reason) == reason && utf8.ValidString(reason) && !strings.ContainsFunc(reason, unicode.IsControl)
}

func imageRiskRequest(body generated.ImageAdmissionRiskDecisionInput, mutation *cp.MutationContext, recipe, artifact string) (*cp.DecideOrganizationImageAdmissionRiskRequest, bool) {
	if body.ExpectedArtifactVersion < 1 || body.ExpectedArtifactVersion > maximumSafeJSONInteger || body.ExpectedAdmissionRevision < 1 || body.ExpectedRecipeVersion < 1 || body.ExpectedRecipeGeneration < 1 || body.ExpectedBuildAttempt < 1 || body.PolicyRevision < 1 || mutation.GetExpectedVersion() != body.ExpectedArtifactVersion || !validImageRiskReason(body.Reason) {
		return nil, false
	}
	action := cp.ImageAdmissionRiskAction(cp.ImageAdmissionRiskAction_value["IMAGE_ADMISSION_RISK_ACTION_"+string(body.Action)])
	if action == 0 {
		return nil, false
	}
	for _, pin := range []int64{body.ExpectedAdmissionRevision, body.ExpectedRecipeVersion, body.ExpectedRecipeGeneration, body.PolicyRevision} {
		if pin > maximumSafeJSONInteger {
			return nil, false
		}
	}
	if body.ExpectedBuildAttempt > 10 || !strings.HasPrefix(recipe, "imgrec_") || !effectiveCapabilityRef(recipe) || !strings.HasPrefix(artifact, "imgart_") || !effectiveCapabilityRef(artifact) || !strings.HasPrefix(body.ExpectedBuildRef, "imgbld_") || !effectiveCapabilityRef(body.ExpectedBuildRef) || !imageRiskManifest(body.ManifestDigest) || !imageRiskManifest(body.PriorEvidenceManifestDigest) || !validManagedDigest(body.VulnerabilityEvidenceSha256) || !validManagedDigest(body.ProjectionSha256) || !validManagedDigest(body.PriorAdmissionReceiptSha256) || !validManagedDigest(body.PolicySha256) {
		return nil, false
	}
	return &cp.DecideOrganizationImageAdmissionRiskRequest{Mutation: mutation, RecipeRef: recipe, ArtifactRef: artifact, ExpectedArtifactVersion: uint64(body.ExpectedArtifactVersion), ExpectedAdmissionRevision: uint64(body.ExpectedAdmissionRevision), ExpectedRecipeVersion: uint64(body.ExpectedRecipeVersion), ExpectedRecipeGeneration: uint64(body.ExpectedRecipeGeneration), ExpectedBuildRef: body.ExpectedBuildRef, ExpectedBuildAttempt: uint32(body.ExpectedBuildAttempt), ManifestDigest: body.ManifestDigest, VulnerabilityEvidenceSha256: body.VulnerabilityEvidenceSha256, ProjectionSha256: body.ProjectionSha256, PriorAdmissionReceiptSha256: body.PriorAdmissionReceiptSha256, PriorEvidenceManifestDigest: body.PriorEvidenceManifestDigest, PolicyRevision: uint64(body.PolicyRevision), PolicySha256: body.PolicySha256, Action: action, Reason: body.Reason}, true
}

func (server *Server) DecideOrganizationImageAdmissionRisk(w http.ResponseWriter, r *http.Request, recipe generated.RecipeRef, artifact generated.ArtifactRef, p generated.DecideOrganizationImageAdmissionRiskParams) {
	body, ok := decodeJSON[generated.ImageAdmissionRiskDecisionInput](w, r)
	if !ok {
		return
	}
	m, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	input, ok := imageRiskRequest(body, m, recipe, artifact)
	if !ok {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	result, err := server.control.RoleImages.DecideOrganizationImageAdmissionRisk(r.Context(), input)
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if result == nil {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	writeImageRiskDecision(w, result.GetDecision(), result.GetAdmissionAttempt(), result.GetArtifact(), input, "")
}

func (server *Server) DecideImageAdmissionRisk(w http.ResponseWriter, r *http.Request, project generated.ProjectRef, recipe generated.RecipeRef, artifact generated.ArtifactRef, p generated.DecideImageAdmissionRiskParams) {
	body, ok := decodeJSON[generated.ImageAdmissionRiskDecisionInput](w, r)
	if !ok {
		return
	}
	m, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	input, ok := imageRiskRequest(body, m, recipe, artifact)
	if !ok {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	request := &cp.DecideImageAdmissionRiskRequest{Mutation: m, ProjectRef: project, RecipeRef: recipe, ArtifactRef: artifact, ExpectedArtifactVersion: input.ExpectedArtifactVersion, ExpectedAdmissionRevision: input.ExpectedAdmissionRevision, ExpectedRecipeVersion: input.ExpectedRecipeVersion, ExpectedRecipeGeneration: input.ExpectedRecipeGeneration, ExpectedBuildRef: input.ExpectedBuildRef, ExpectedBuildAttempt: input.ExpectedBuildAttempt, ManifestDigest: input.ManifestDigest, VulnerabilityEvidenceSha256: input.VulnerabilityEvidenceSha256, ProjectionSha256: input.ProjectionSha256, PriorAdmissionReceiptSha256: input.PriorAdmissionReceiptSha256, PriorEvidenceManifestDigest: input.PriorEvidenceManifestDigest, PolicyRevision: input.PolicyRevision, PolicySha256: input.PolicySha256, Action: input.Action, Reason: input.Reason}
	result, err := server.control.RoleImages.DecideImageAdmissionRisk(r.Context(), request)
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if result == nil {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	writeImageRiskDecision(w, result.GetDecision(), result.GetAdmissionAttempt(), result.GetArtifact(), input, project)
}

func writeImageRiskDecision(w http.ResponseWriter, d *cp.ImageAdmissionRiskDecision, attempt *cp.ImageAdmissionAttempt, a *cp.ImageArtifact, input *cp.DecideOrganizationImageAdmissionRiskRequest, project string) {
	invalid := func() { writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false) }
	if !validImageRiskHistory(a) {
		invalid()
		return
	}
	if a.GetVersion() < input.GetExpectedArtifactVersion() || a.GetManifestDigest() != input.GetManifestDigest() || a.GetRecipeGeneration() != input.GetExpectedRecipeGeneration() || a.GetBuildRef() != input.GetExpectedBuildRef() || a.GetBuildAttempt() != input.GetExpectedBuildAttempt() || a.GetPolicyRevision() != input.GetPolicyRevision() || a.GetPolicySha256() != input.GetPolicySha256() || a.GetAdmissionRevision() != input.GetExpectedAdmissionRevision() || a.GetVulnerabilityEvidenceSha256() != input.GetVulnerabilityEvidenceSha256() {
		invalid()
		return
	}
	if d == nil || a == nil || !imageRiskOwner(d.GetScopeKind(), d.GetOrganizationRef(), d.GetProjectRef(), project) || !imageRiskOwner(a.GetScopeKind(), a.GetOrganizationRef(), a.GetProjectRef(), project) || a.GetOrganizationRef() != d.GetOrganizationRef() || a.GetRef() != input.GetArtifactRef() || a.GetRecipeRef() != input.GetRecipeRef() || d.GetArtifactRef() != a.GetRef() || d.GetRecipeRef() != a.GetRecipeRef() || d.GetArtifactVersion() != input.GetExpectedArtifactVersion() || d.GetAdmissionRevision() != input.GetExpectedAdmissionRevision() || d.GetRecipeVersion() != input.GetExpectedRecipeVersion() || d.GetRecipeGeneration() != input.GetExpectedRecipeGeneration() || d.GetBuildRef() != input.GetExpectedBuildRef() || d.GetBuildAttempt() != input.GetExpectedBuildAttempt() || d.GetManifestDigest() != input.GetManifestDigest() || d.GetVulnerabilityEvidenceSha256() != input.GetVulnerabilityEvidenceSha256() || d.GetPriorAdmissionReceiptSha256() != input.GetPriorAdmissionReceiptSha256() || d.GetPriorEvidenceManifestDigest() != input.GetPriorEvidenceManifestDigest() || d.GetPolicyRevision() != input.GetPolicyRevision() || d.GetPolicySha256() != input.GetPolicySha256() || d.GetAction() != input.GetAction() || d.GetReason() != input.GetReason() || d.GetVersion() != 1 || !effectiveCapabilityRef(d.GetRef()) || !effectiveCapabilityRef(d.GetDecidedByActorRef()) || d.GetDecidedAt() == nil || d.GetDecidedAt().CheckValid() != nil {
		invalid()
		return
	}
	decision, ok := imageRiskPublic[generated.ImageAdmissionRiskDecision](d)
	if !ok {
		invalid()
		return
	}
	out := generated.ImageAdmissionRiskDecisionResponse{Decision: decision, Artifact: publicRoleImageArtifact(a)}
	if d.GetAction() == cp.ImageAdmissionRiskAction_IMAGE_ADMISSION_RISK_ACTION_ACCEPT_RISK {
		if attempt == nil || !proto.Equal(attempt, a.GetAdmissionAttempt()) || attempt.GetArtifactRef() != a.GetRef() || attempt.GetDecisionRef() != d.GetRef() || (attempt.GetState() != cp.ImageAdmissionAttemptState_IMAGE_ADMISSION_ATTEMPT_STATE_PENDING && attempt.GetState() != cp.ImageAdmissionAttemptState_IMAGE_ADMISSION_ATTEMPT_STATE_CLAIMED) || attempt.GetNumber() < 1 || attempt.GetVersion() < 1 {
			invalid()
			return
		}
		value, ok := imageRiskPublic[generated.ImageAdmissionAttempt](attempt)
		if !ok {
			invalid()
			return
		}
		out.AdmissionAttempt = &value
	} else if attempt != nil {
		invalid()
		return
	}
	setVersionETag(w, a.GetVersion())
	writeJSON(w, http.StatusOK, out)
}

func (server *Server) GetOrganizationImageVulnerabilityReport(w http.ResponseWriter, r *http.Request, recipe generated.RecipeRef, artifact generated.ArtifactRef, p generated.GetOrganizationImageVulnerabilityReportParams) {
	input := &cp.GetOrganizationImageVulnerabilityReportRequest{RecipeRef: recipe, ArtifactRef: artifact, Page: page(p.PageSize, p.PageToken), PackageQuery: stringValue(p.PackageQuery), AdvisoryQuery: stringValue(p.AdvisoryQuery), BlockingOnly: p.BlockingOnly, ExpectedReportSha256: stringValue(p.ExpectedReportSha256)}
	if p.Severity != nil {
		value := cp.ImageVulnerabilitySeverity(cp.ImageVulnerabilitySeverity_value["IMAGE_VULNERABILITY_SEVERITY_"+string(*p.Severity)])
		if value == 0 {
			writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
			return
		}
		input.Severity = &value
	}
	response, err := server.control.RoleImages.GetOrganizationImageVulnerabilityReport(r.Context(), input)
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if response == nil {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	writeImageVulnerabilityReport(w, response.GetReport(), response.GetFindings(), response.GetPage(), recipe, artifact, "")
}

func (server *Server) GetImageVulnerabilityReport(w http.ResponseWriter, r *http.Request, project generated.ProjectRef, recipe generated.RecipeRef, artifact generated.ArtifactRef, p generated.GetImageVulnerabilityReportParams) {
	input := &cp.GetImageVulnerabilityReportRequest{ProjectRef: project, RecipeRef: recipe, ArtifactRef: artifact, Page: page(p.PageSize, p.PageToken), PackageQuery: stringValue(p.PackageQuery), AdvisoryQuery: stringValue(p.AdvisoryQuery), BlockingOnly: p.BlockingOnly, ExpectedReportSha256: stringValue(p.ExpectedReportSha256)}
	if p.Severity != nil {
		value := cp.ImageVulnerabilitySeverity(cp.ImageVulnerabilitySeverity_value["IMAGE_VULNERABILITY_SEVERITY_"+string(*p.Severity)])
		if value == 0 {
			writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
			return
		}
		input.Severity = &value
	}
	response, err := server.control.RoleImages.GetImageVulnerabilityReport(r.Context(), input)
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if response == nil {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	writeImageVulnerabilityReport(w, response.GetReport(), response.GetFindings(), response.GetPage(), recipe, artifact, project)
}

func writeImageVulnerabilityReport(w http.ResponseWriter, report *cp.ImageVulnerabilityReport, findings []*cp.ImageVulnerabilityFinding, p *cp.PageInfo, recipe, artifact, project string) {
	invalid := func() { writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false) }
	if report == nil || report.GetRecipeRef() != recipe || report.GetArtifactRef() != artifact || !imageRiskOwner(report.GetScopeKind(), report.GetOrganizationRef(), report.GetProjectRef(), project) || report.GetArtifactVersion() < 1 || len(findings) > 100 || len(p.GetNextPageToken()) > 512 {
		invalid()
		return
	}
	ready := report.GetState() == cp.ImageVulnerabilityReportState_IMAGE_VULNERABILITY_REPORT_STATE_READY
	if !ready && (report.GetState() != cp.ImageVulnerabilityReportState_IMAGE_VULNERABILITY_REPORT_STATE_UNAVAILABLE || len(findings) != 0 || report.GetComplete() || p.GetNextPageToken() != "") || ready && (!report.GetComplete() || report.GetVersion() < 1 || len(report.GetSeverityCounts()) != 6 || !validImageVulnerabilityPage(report, findings)) {
		invalid()
		return
	}
	value, ok := imageRiskPublic[generated.ImageVulnerabilityReport](report)
	if !ok {
		invalid()
		return
	}
	out := generated.ImageVulnerabilityReportResponse{Report: value, Findings: make([]generated.ImageVulnerabilityFinding, 0, len(findings)), Page: generated.ImageVulnerabilityReportPageInfo{NextPageToken: p.GetNextPageToken()}}
	seen := map[string]bool{}
	for _, finding := range findings {
		if finding == nil || finding.GetRef() == "" || seen[finding.GetRef()] || finding.GetOccurrences() < 1 || finding.GetSeverity() == 0 || finding.GetFixState() == 0 || finding.GetAdvisoryKind() == 0 {
			invalid()
			return
		}
		seen[finding.GetRef()] = true
		item, ok := imageRiskPublic[generated.ImageVulnerabilityFinding](finding)
		if !ok {
			invalid()
			return
		}
		out.Findings = append(out.Findings, item)
	}
	setVersionETag(w, report.GetArtifactVersion())
	writeJSON(w, http.StatusOK, out)
}

func validImageVulnerabilityPage(r *cp.ImageVulnerabilityReport, rows []*cp.ImageVulnerabilityFinding) bool {
	if !validManagedDigest(r.GetProjectionSha256()) || !validManagedDigest(r.GetPriorAdmissionReceiptSha256()) || !imageRiskManifest(r.GetPriorEvidenceManifestDigest()) || r.GetSourceAdmissionRevision() < 1 || r.GetSourceAdmissionRevision() != r.GetVersion() || r.GetAdmissionRevision() != r.GetSourceAdmissionRevision() {
		return false
	}
	severities := []string{"CRITICAL", "HIGH", "MEDIUM", "LOW", "NEGLIGIBLE", "UNKNOWN"}
	var total uint64
	for index, count := range r.GetSeverityCounts() {
		if count == nil || strings.TrimPrefix(count.GetSeverity().String(), "IMAGE_VULNERABILITY_SEVERITY_") != severities[index] {
			return false
		}
		total += uint64(count.GetMatchCount())
	}
	if total != uint64(r.GetMatchCount()) || r.GetBlockingMatchCount() > r.GetMatchCount() || r.GetUnresolvedNoFixMatchCount() > r.GetMatchCount() || r.GetSuppressedMatchCount() > r.GetMatchCount() || r.GetUniqueAdvisoryCount() > r.GetMatchCount() {
		return false
	}
	// Проверка каждой страницы переиспользует canonical validator ссылок и tuple;
	// сумма всего отчёта остаётся отдельным server-pinned metadata, не пересчётом UI.
	page := runtimecontract.ImageVulnerabilityReport{Schema: runtimecontract.ImageVulnerabilityReportSchema, ArtifactRef: r.GetArtifactRef(), ImageDigest: r.GetManifestDigest(), ReportSHA256: r.GetVulnerabilityEvidenceSha256(), SBOMSHA256: r.GetSbomSha256(), ScopeKind: strings.TrimPrefix(r.GetScopeKind().String(), "RUNTIME_RESOURCE_SCOPE_KIND_"), OrganizationRef: r.GetOrganizationRef(), ProjectRef: r.GetProjectRef(), RecipeRef: r.GetRecipeRef(), RecipeVersion: r.GetRecipeVersion(), RecipeGeneration: r.GetRecipeGeneration(), BuildRef: r.GetBuildRef(), BuildVersion: r.GetBuildVersion(), BuildAttempt: r.GetBuildAttempt(), PolicyRevision: r.GetPolicyRevision(), PolicySHA256: r.GetPolicySha256(), Findings: []runtimecontract.ImageVulnerabilityFinding{}}
	counts := map[string]uint32{}
	advisories := map[string]bool{}
	for _, f := range rows {
		if f == nil {
			return false
		}
		item := runtimecontract.ImageVulnerabilityFinding{Ref: f.GetRef(), PackageName: f.GetPackageName(), InstalledVersion: f.GetInstalledVersion(), Ecosystem: f.GetEcosystem(), AdvisoryID: f.GetAdvisoryId(), AdvisoryKind: strings.TrimPrefix(f.GetAdvisoryKind().String(), "IMAGE_VULNERABILITY_ADVISORY_KIND_"), AdvisoryURL: f.GetAdvisoryUrl(), Severity: strings.TrimPrefix(f.GetSeverity().String(), "IMAGE_VULNERABILITY_SEVERITY_"), FixState: strings.TrimPrefix(f.GetFixState().String(), "IMAGE_VULNERABILITY_FIX_STATE_"), FixedVersions: append([]string{}, f.GetFixedVersions()...), Blocking: f.GetBlocking(), Ignored: f.GetIgnored(), Occurrences: f.GetOccurrences()}
		page.Findings = append(page.Findings, item)
		if uint64(page.MatchCount)+uint64(item.Occurrences) > uint64(r.GetMatchCount()) {
			return false
		}
		page.MatchCount += item.Occurrences
		counts[item.Severity] += item.Occurrences
		advisories[item.AdvisoryID] = true
		if item.Blocking {
			page.BlockingMatchCount += item.Occurrences
		}
		if item.Ignored {
			page.SuppressedMatchCount += item.Occurrences
		} else if (item.Severity == "HIGH" || item.Severity == "CRITICAL") && !item.Blocking {
			page.UnresolvedNoFixMatchCount += item.Occurrences
		}
	}
	page.UniqueAdvisoryCount = uint32(len(advisories))
	for _, severity := range severities {
		page.SeverityCounts = append(page.SeverityCounts, runtimecontract.ImageVulnerabilitySeverityCount{Severity: severity, MatchCount: counts[severity]})
	}
	return page.Validate() == nil
}

func imageRiskManifest(value string) bool {
	return strings.HasPrefix(value, "sha256:") && validManagedDigest(strings.TrimPrefix(value, "sha256:"))
}

func imageRiskPin(value uint64) bool { return value > 0 && value <= uint64(maximumSafeJSONInteger) }

func validImageRiskHistory(a *cp.ImageArtifact) bool {
	if a == nil {
		return false
	}
	d := a.GetRiskDecision()
	attempt := a.GetAdmissionAttempt()
	if d != nil {
		if !strings.HasPrefix(d.GetRef(), "imgrisk_") || !effectiveCapabilityRef(d.GetRef()) || !imageRiskPin(d.GetArtifactVersion()) || !imageRiskPin(d.GetAdmissionRevision()) || !validManagedDigest(d.GetBindingSha256()) || !validImageRiskReason(d.GetReason()) || d.GetDecidedAt() == nil || d.GetDecidedAt().CheckValid() != nil || d.GetVersion() != 1 || (d.GetAction() != cp.ImageAdmissionRiskAction_IMAGE_ADMISSION_RISK_ACTION_ACCEPT_RISK && d.GetAction() != cp.ImageAdmissionRiskAction_IMAGE_ADMISSION_RISK_ACTION_REJECT_RISK) || !effectiveCapabilityRef(d.GetDecidedByActorRef()) || d.GetArtifactRef() != a.GetRef() || d.GetRecipeRef() != a.GetRecipeRef() || d.GetScopeKind() != a.GetScopeKind() || d.GetOrganizationRef() != a.GetOrganizationRef() || d.GetProjectRef() != a.GetProjectRef() || d.GetManifestDigest() != a.GetManifestDigest() || d.GetBuildRef() != a.GetBuildRef() || d.GetRecipeGeneration() != a.GetRecipeGeneration() {
			return false
		}
	}
	if attempt != nil {
		if !strings.HasPrefix(attempt.GetRef(), "imgadm_") || !effectiveCapabilityRef(attempt.GetRef()) || attempt.GetArtifactRef() != a.GetRef() || !imageRiskPin(attempt.GetVersion()) || attempt.GetNumber() < 1 || attempt.GetFence() > uint64(maximumSafeJSONInteger) {
			return false
		}
		switch attempt.GetState() {
		case cp.ImageAdmissionAttemptState_IMAGE_ADMISSION_ATTEMPT_STATE_PENDING, cp.ImageAdmissionAttemptState_IMAGE_ADMISSION_ATTEMPT_STATE_CLAIMED, cp.ImageAdmissionAttemptState_IMAGE_ADMISSION_ATTEMPT_STATE_ACCEPTED, cp.ImageAdmissionAttemptState_IMAGE_ADMISSION_ATTEMPT_STATE_REJECTED, cp.ImageAdmissionAttemptState_IMAGE_ADMISSION_ATTEMPT_STATE_FAILED, cp.ImageAdmissionAttemptState_IMAGE_ADMISSION_ATTEMPT_STATE_CANCELLED:
		default:
			return false
		}
		if attempt.GetDecisionRef() != "" && (d == nil || attempt.GetDecisionRef() != d.GetRef()) {
			return false
		}
	}
	return true
}
