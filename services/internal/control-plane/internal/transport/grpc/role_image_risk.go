package grpc

import (
	"context"
	"strings"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	repo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/roleimage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// GetOrganizationImageVulnerabilityReport fixes the ORGANIZATION filter after exact principal
// resolution, preserving optional severity/blocking filters and expected report digest before the
// domain query; it maps report/findings/cursor only.
// GetOrganizationImageVulnerabilityReport закрепляет фильтр ORGANIZATION после разрешения точного
// principal, сохраняя необязательные severity и blocking и ожидаемый хеш отчёта до доменного запроса;
// лишь переносит отчёт, находки и курсор.
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

// GetImageVulnerabilityReport binds the exact project-read principal and PROJECT/project filter, then
// forwards recipe/artifact/report-digest and optional finding filters; it does not scan an image or
// accept its risk.
// GetImageVulnerabilityReport связывает principal точного проектного чтения с фильтром PROJECT и
// проектом, затем передаёт привязки рецепта, артефакта, хеша отчёта и необязательные фильтры находок;
// образ не сканирует и риск не принимает.
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

// imageRiskRequest describes only shared typed risk-decision getters. Principal and scope are assigned
// by the exact RPC wrapper, not obtained through this request interface; every version/digest remains
// an expected pin for domain verification.
// imageRiskRequest задаёт лишь общие типизированные getters решения о риске. Principal и область
// назначаются точным RPC wrapper, не получаются из этого интерфейса запроса; каждая версия и хеш
// остаются ожидаемыми привязками для доменной проверки.
type imageRiskRequest interface {
	// GetMutation supplies expected version and idempotency input for the risk command, not actor or owner
	// identity.
	// GetMutation предоставляет ожидаемую версию и ввод идемпотентности команды риска, не личность
	// исполнителя или владельца.
	GetMutation() *cp.MutationContext
	// GetRecipeRef selects the recipe reference to resolve inside the authorized domain scope; the
	// identifier alone grants no access.
	// GetRecipeRef выбирает ссылку рецепта для разрешения внутри разрешённой доменной области;
	// идентификатор сам по себе доступа не даёт.
	GetRecipeRef() string
	// GetArtifactRef selects the exact artifact to bind to the risk decision, not a caller-owned proof of
	// artifact eligibility.
	// GetArtifactRef выбирает точный артефакт для привязки решения о риске, не пользовательское
	// доказательство его допустимости.
	GetArtifactRef() string
	// GetExpectedArtifactVersion pins the artifact version expected by the decision rather than selecting
	// a mutable latest artifact.
	// GetExpectedArtifactVersion закрепляет ожидаемую решением версию артефакта вместо изменяемого
	// последнего артефакта.
	GetExpectedArtifactVersion() uint64
	// GetExpectedAdmissionRevision pins the admission revision on which the proposed risk decision is
	// based.
	// GetExpectedAdmissionRevision закрепляет ревизию допуска, на которой основано предлагаемое решение о
	// риске.
	GetExpectedAdmissionRevision() uint64
	// GetExpectedRecipeVersion supplies the exact recipe version expected for domain consistency checks.
	// GetExpectedRecipeVersion передаёт точную ожидаемую версию рецепта для доменной проверки
	// согласованности.
	GetExpectedRecipeVersion() uint64
	// GetExpectedRecipeGeneration pins the recipe generation independently of its update version.
	// GetExpectedRecipeGeneration закрепляет поколение рецепта независимо от версии изменения.
	GetExpectedRecipeGeneration() uint64
	// GetExpectedBuildRef selects the exact producing build to verify against the artifact provenance.
	// GetExpectedBuildRef выбирает точную создавшую сборку для сверки с происхождением артефакта.
	GetExpectedBuildRef() string
	// GetExpectedBuildAttempt pins the producing build attempt so a retry is not silently treated as the
	// same result.
	// GetExpectedBuildAttempt закрепляет попытку создавшей сборки, чтобы повтор не считался тем же
	// результатом незаметно.
	GetExpectedBuildAttempt() uint32
	// GetManifestDigest supplies the expected immutable image manifest digest, not a mutable tag.
	// GetManifestDigest предоставляет ожидаемый неизменяемый хеш manifest образа, не изменяемый тег.
	GetManifestDigest() string
	// GetVulnerabilityEvidenceSha256 binds the proposed decision to the exact vulnerability evidence
	// digest.
	// GetVulnerabilityEvidenceSha256 связывает предлагаемое решение с точным хешем свидетельств
	// уязвимостей.
	GetVulnerabilityEvidenceSha256() string
	// GetProjectionSha256 pins the vulnerability report projection reviewed for this risk decision.
	// GetProjectionSha256 закрепляет проекцию отчёта об уязвимостях, рассмотренную для решения о риске.
	GetProjectionSha256() string
	// GetPriorAdmissionReceiptSha256 supplies the predecessor admission receipt digest for owner
	// validation, not authorization to reuse it.
	// GetPriorAdmissionReceiptSha256 передаёт хеш предыдущего receipt допуска для проверки владельцем, не
	// полномочие его повторного использования.
	GetPriorAdmissionReceiptSha256() string
	// GetPriorEvidenceManifestDigest pins the predecessor evidence manifest independently of the new
	// report projection.
	// GetPriorEvidenceManifestDigest закрепляет предыдущий manifest свидетельств независимо от новой
	// проекции отчёта.
	GetPriorEvidenceManifestDigest() string
	// GetPolicyRevision identifies the exact admission policy revision expected by the decision.
	// GetPolicyRevision указывает точную ревизию политики допуска, ожидаемую решением.
	GetPolicyRevision() uint64
	// GetPolicySha256 pins admission policy content independently of its numeric revision.
	// GetPolicySha256 закрепляет содержимое политики допуска независимо от числовой ревизии.
	GetPolicySha256() string
	// GetAction supplies the typed proposed risk action; the domain still decides whether the actor and
	// transition may apply it.
	// GetAction предоставляет типизированное предлагаемое действие с риском; домен по-прежнему проверяет
	// право исполнителя и перехода применить его.
	GetAction() cp.ImageAdmissionRiskAction
	// GetReason carries the user's decision justification as data, not a replacement for server-owned
	// approval authority.
	// GetReason несёт обоснование решения пользователя как данные, не заменяя серверные полномочия
	// подтверждения.
	GetReason() string
}

// trimEnum removes a known textual prefix only; it does not validate membership in a closed action or
// state set.
// trimEnum лишь удаляет известный текстовый префикс; членство в закрытом наборе действий или состояний
// не проверяет.
func trimEnum(value, prefix string) string { return strings.TrimPrefix(value, prefix) }

// imageRiskInput copies common risk mutation/version/digest/provenance/action/reason pins through the
// typed getter interface. It leaves Principal, ScopeKind and ProjectRef for the exact operation
// wrapper and performs no decision or persistence.
// imageRiskInput копирует общие привязки изменения, версий, хешей, происхождения, действия и причины
// через типизированный getter interface. Principal, ScopeKind и ProjectRef оставляет точному operation
// wrapper и решения или сохранения не выполняет.
func imageRiskInput(r imageRiskRequest) repo.AdmissionRiskInput {
	return repo.AdmissionRiskInput{Mutation: mutation(r.GetMutation()), RecipeRef: r.GetRecipeRef(), ArtifactRef: r.GetArtifactRef(), ExpectedArtifactVersion: r.GetExpectedArtifactVersion(), ExpectedAdmissionRevision: r.GetExpectedAdmissionRevision(), ExpectedRecipeVersion: r.GetExpectedRecipeVersion(), ExpectedRecipeGeneration: r.GetExpectedRecipeGeneration(), ExpectedBuildRef: r.GetExpectedBuildRef(), ExpectedBuildAttempt: r.GetExpectedBuildAttempt(), ManifestDigest: r.GetManifestDigest(), VulnerabilityEvidenceSHA256: r.GetVulnerabilityEvidenceSha256(), ProjectionSHA256: r.GetProjectionSha256(), PriorAdmissionReceiptSHA256: r.GetPriorAdmissionReceiptSha256(), PriorEvidenceManifestDigest: r.GetPriorEvidenceManifestDigest(), PolicyRevision: r.GetPolicyRevision(), PolicySHA256: r.GetPolicySha256(), Action: trimEnum(r.GetAction().String(), "IMAGE_ADMISSION_RISK_ACTION_"), Reason: r.GetReason()}
}

// DecideOrganizationImageAdmissionRisk sets the context-derived principal and ORGANIZATION scope on
// shared risk input, invokes the specialized domain decision and maps its decision/attempt/artifact
// without auto-accepting scan findings.
// DecideOrganizationImageAdmissionRisk задаёт полученный из контекста principal и область ORGANIZATION
// общему вводу риска, вызывает специализированное доменное решение и переносит решение, попытку и
// артефакт без автоматического принятия находок сканирования.
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

// DecideImageAdmissionRisk sets the exact context principal plus PROJECT and requested project
// reference before the domain validates the decision; transport does not derive ownership from that
// reference.
// DecideImageAdmissionRisk задаёт точный principal контекста, PROJECT и запрошенную ссылку проекта до
// доменной проверки решения; транспорт не выводит владение из этой ссылки.
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

// castImageVulnerabilityReport projects artifact/evidence/policy pins with an UNAVAILABLE default;
// only the domain Available flag enables READY, completeness, report version/digest and counts.
// NextActions is passed through, not deep-copied or recomputed here.
// castImageVulnerabilityReport переносит привязки артефакта, свидетельств и политики с исходным
// UNAVAILABLE; только доменный Available включает READY, полноту, версию, хеш и количества отчёта.
// NextActions передаётся напрямую, не копируется глубоко и не пересчитывается здесь.
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

// castImageVulnerabilityFindings preserves finding order and report-provided advisory/severity/fix
// enums, copies fixed-version slices and returns an allocated empty list for no findings. It neither
// visits advisory URLs nor decides blocking policy.
// castImageVulnerabilityFindings сохраняет порядок находок и enum advisory, severity и fix из отчёта,
// копирует списки исправленных версий и возвращает выделенный пустой список без находок. По advisory
// URL не переходит и политику блокировки не определяет.
func castImageVulnerabilityFindings(values []runtimecontract.ImageVulnerabilityFinding) []*cp.ImageVulnerabilityFinding {
	result := make([]*cp.ImageVulnerabilityFinding, 0, len(values))
	for _, f := range values {
		result = append(result, &cp.ImageVulnerabilityFinding{Ref: f.Ref, PackageName: f.PackageName, InstalledVersion: f.InstalledVersion, Ecosystem: f.Ecosystem, AdvisoryId: f.AdvisoryID, AdvisoryKind: cp.ImageVulnerabilityAdvisoryKind(cp.ImageVulnerabilityAdvisoryKind_value["IMAGE_VULNERABILITY_ADVISORY_KIND_"+f.AdvisoryKind]), AdvisoryUrl: f.AdvisoryURL, Severity: cp.ImageVulnerabilitySeverity(cp.ImageVulnerabilitySeverity_value["IMAGE_VULNERABILITY_SEVERITY_"+f.Severity]), FixState: cp.ImageVulnerabilityFixState(cp.ImageVulnerabilityFixState_value["IMAGE_VULNERABILITY_FIX_STATE_"+f.FixState]), FixedVersions: append([]string(nil), f.FixedVersions...), Blocking: f.Blocking, Ignored: f.Ignored, Occurrences: f.Occurrences})
	}
	return result
}

// castImageRiskDecision preserves nil or projects a domain-owned decision with fixed wire version 1,
// actor/time/reason, binding digest and exact artifact/build/admission/policy predecessor pins; source
// admission revision mirrors AdmissionRevision.
// castImageRiskDecision сохраняет nil либо переносит доменное решение с фиксированной wire version 1,
// исполнителем, временем, причиной, хешем привязки и точными предыдущими привязками артефакта, сборки,
// допуска и политики; source admission revision повторяет AdmissionRevision.
func castImageRiskDecision(d *entity.ImageAdmissionRiskDecision) *cp.ImageAdmissionRiskDecision {
	if d == nil {
		return nil
	}
	return &cp.ImageAdmissionRiskDecision{Ref: d.Ref, Version: 1, Action: cp.ImageAdmissionRiskAction(cp.ImageAdmissionRiskAction_value["IMAGE_ADMISSION_RISK_ACTION_"+d.Action]), Reason: d.Reason, DecidedByActorRef: d.ActorRef, DecidedAt: timestamp(d.DecidedAt), BindingSha256: d.BindingSHA256, ScopeKind: cp.RuntimeResourceScopeKind(cp.RuntimeResourceScopeKind_value["RUNTIME_RESOURCE_SCOPE_KIND_"+d.ScopeKind]), OrganizationRef: d.OrganizationRef, ProjectRef: d.ProjectRef, RecipeRef: d.RecipeRef, RecipeVersion: d.RecipeVersion, RecipeGeneration: d.RecipeGeneration, BuildRef: d.BuildRef, BuildVersion: d.BuildVersion, BuildAttempt: d.BuildAttempt, ArtifactRef: d.ArtifactRef, ArtifactVersion: d.ArtifactVersion, ManifestDigest: d.ManifestDigest, AdmissionRevision: d.AdmissionRevision, SourceAdmissionRevision: d.AdmissionRevision, PriorAdmissionReceiptSha256: d.PriorAdmissionReceiptSHA256, PriorEvidenceManifestDigest: d.PriorEvidenceManifestDigest, VulnerabilityEvidenceSha256: d.VulnerabilityEvidenceSHA256, SbomSha256: d.SBOMSHA256, PolicyRevision: d.PolicyRevision, PolicySha256: d.PolicySHA256}
}

// castImageAdmissionAttempt preserves nil or projects attempt identity/version/number, generated state
// enum, artifact/decision/fence and receipt/evidence pins. It does not turn a live attempt into
// terminal proof.
// castImageAdmissionAttempt сохраняет nil либо переносит личность, версию и номер попытки, generated
// enum состояния, артефакт, решение, fence и привязки receipt и evidence. Активную попытку в terminal
// proof не превращает.
func castImageAdmissionAttempt(a *entity.ImageAdmissionAttempt) *cp.ImageAdmissionAttempt {
	if a == nil {
		return nil
	}
	return &cp.ImageAdmissionAttempt{Ref: a.Ref, Version: a.Version, Number: a.Number, State: cp.ImageAdmissionAttemptState(cp.ImageAdmissionAttemptState_value["IMAGE_ADMISSION_ATTEMPT_STATE_"+a.State]), ArtifactRef: a.ArtifactRef, DecisionRef: a.RiskDecisionRef, Fence: a.Fence, AdmissionReceiptSha256: a.AdmissionReceiptSHA256, EvidenceManifestDigest: a.EvidenceManifestDigest}
}
