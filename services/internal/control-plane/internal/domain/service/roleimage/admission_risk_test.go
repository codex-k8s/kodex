package roleimage

import (
	"context"
	"strings"
	"testing"

	repo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/roleimage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

type riskRepositoryStub struct {
	repo.Repository
	principal value.Principal
	calls     int
}

func (s *riskRepositoryStub) ResolvePrincipal(context.Context, value.Principal) (value.Principal, error) {
	return s.principal, nil
}
func (s *riskRepositoryStub) DecideAdmissionRisk(context.Context, repo.AdmissionRiskInput) (entity.ImageAdmissionRiskResult, error) {
	s.calls++
	return entity.ImageAdmissionRiskResult{}, nil
}
func (s *riskRepositoryStub) GetVulnerabilityReport(context.Context, value.Principal, repo.VulnerabilityReportFilter) (entity.ImageVulnerabilityReport, error) {
	s.calls++
	return entity.ImageVulnerabilityReport{}, nil
}

func TestImageRiskSpecialtyRejectsInvalidAuthorityAndPins(t *testing.T) {
	catalog, err := NewCatalog([]Environment{validEnvironment(true, true)})
	if err != nil {
		t.Fatal(err)
	}
	p := value.Principal{ActorID: "usr_owner", AuthorityTenant: "org_installation", Permission: "platform.command.organization.role-images.risk.decide", CorrelationRef: "cor_risk", CallerWorkload: "control-api-gateway", CredentialRevision: 1}
	v := int64(2)
	input := repo.AdmissionRiskInput{Principal: p, ScopeKind: "ORGANIZATION", RecipeRef: "imgrec_12345678", ArtifactRef: "imgart_12345678", ExpectedArtifactVersion: 2, ExpectedAdmissionRevision: 1, ExpectedRecipeVersion: 1, ExpectedRecipeGeneration: 1, ExpectedBuildRef: "imgbld_12345678", ExpectedBuildAttempt: 1, ManifestDigest: "sha256:" + strings.Repeat("a", 64), VulnerabilityEvidenceSHA256: strings.Repeat("b", 64), ProjectionSHA256: strings.Repeat("c", 64), PriorAdmissionReceiptSHA256: strings.Repeat("d", 64), PriorEvidenceManifestDigest: "sha256:" + strings.Repeat("e", 64), PolicyRevision: 1, PolicySHA256: strings.Repeat("f", 64), Action: "ACCEPT_RISK", Reason: "Owner decision", Mutation: value.Mutation{IdempotencyKey: "risk-unit", ExpectedVersion: &v}}
	for _, name := range []string{"valid", "signed_project", "assistant", "wrong_permission", "empty_scope", "scope_project_mixed", "empty_reason", "control_reason", "oversized_reason", "wrong_occ", "missing_report", "unknown_action"} {
		t.Run(name, func(t *testing.T) {
			in := input
			principal := p
			switch name {
			case "signed_project":
				principal.ProjectRef = "prj_12345678"
			case "assistant":
				principal.CallerWorkload = "runtime-controller"
			case "wrong_permission":
				principal.Permission = "platform.role-images.recipes.manage"
			case "empty_scope":
				in.ScopeKind = ""
			case "scope_project_mixed":
				in.ProjectRef = "prj_12345678"
			case "empty_reason":
				in.Reason = ""
			case "control_reason":
				in.Reason = "unsafe\nreason"
			case "oversized_reason":
				in.Reason = strings.Repeat("я", 1025)
			case "wrong_occ":
				in.ExpectedArtifactVersion++
			case "missing_report":
				in.ProjectionSHA256 = ""
			case "unknown_action":
				in.Action = "ACCEPT"
			}
			stub := &riskRepositoryStub{principal: principal}
			service, _ := New(stub, catalog)
			_, err := service.DecideAdmissionRisk(t.Context(), in)
			if name == "valid" {
				if err != nil || stub.calls != 1 {
					t.Fatal("valid closed input rejected", err)
				}
			} else if err == nil || stub.calls != 0 {
				t.Fatal("invalid risk input crossed owner boundary")
			}
		})
	}
}
