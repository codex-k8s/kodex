package entity

import (
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// ImageVulnerabilityReport presents the permitted vulnerability projection and finding page with exact
// artifact, admission, receipt, evidence, and projection pins. Available distinguishes an obtainable
// report from absence; neither an empty page nor report availability establishes admission.
// ImageVulnerabilityReport представляет разрешённую проекцию уязвимостей и страницу находок с точными
// привязками материала, допуска, квитанции, свидетельств и проекции. Available отличает доступный
// отчёт от его отсутствия; пустая страница или доступность отчёта не устанавливают допуск.
type ImageVulnerabilityReport struct {
	Artifact                                                         ImageArtifact
	NextActions                                                      []string
	Available                                                        bool
	ArtifactVersion, AdmissionRevision                               uint64
	ProjectionSHA256, AdmissionReceiptSHA256, EvidenceManifestDigest string
	Report                                                           runtimecontract.ImageVulnerabilityReport
	Findings                                                         []runtimecontract.ImageVulnerabilityFinding
	NextPageToken                                                    string
	Total                                                            int64
}

// ImageAdmissionRiskDecision records the human actor, action, reason, time, and exact artifact,
// recipe, build, policy, SBOM, prior admission, and vulnerability-report pins of a stored risk
// decision. It binds one evidence snapshot and cannot waive a later unrelated report.
// ImageAdmissionRiskDecision фиксирует человека, действие, причину, время и точные привязки материала,
// рецепта, сборки, политики, SBOM, прежнего допуска и отчёта об уязвимостях сохранённого решения о
// риске. Решение связывает один снимок свидетельств и не снимает ограничения с последующего
// несвязанного отчёта.
type ImageAdmissionRiskDecision struct {
	Ref, ArtifactRef, RecipeRef, ScopeKind, OrganizationRef, ProjectRef               string
	ActorRef, Action, Reason                                                          string
	ArtifactVersion, AdmissionRevision, RecipeVersion, RecipeGeneration, BuildVersion uint64
	BuildRef                                                                          string
	BuildAttempt                                                                      uint32
	ManifestDigest, VulnerabilityEvidenceSHA256, ProjectionSHA256                     string
	PriorAdmissionReceiptSHA256, PriorEvidenceManifestDigest                          string
	PolicyRevision                                                                    uint64
	PolicySHA256                                                                      string
	SBOMSHA256, BindingSHA256                                                         string
	DecidedAt                                                                         time.Time
}

// ImageAdmissionAttempt describes a numbered admission attempt with artifact and risk-decision
// references, version, fence, state, and resulting evidence pins. The source admission revision
// identifies its predecessor rather than authorizing reuse of a stale decision.
// ImageAdmissionAttempt описывает нумерованную попытку допуска со ссылками на материал и решение о
// риске, версией, барьером, состоянием и итоговыми свидетельствами. Исходная ревизия допуска
// определяет предшественника, а не разрешает повторное использование устаревшего решения.
type ImageAdmissionAttempt struct {
	Ref, ArtifactRef, RiskDecisionRef, State       string
	AdmissionReceiptSHA256, EvidenceManifestDigest string
	Version, Fence                                 uint64
	Number                                         uint32
	SourceAdmissionRevision                        uint64
	CreatedAt                                      time.Time
}

// ImageAdmissionRiskResult returns the accepted risk decision, optional resulting admission attempt,
// and current artifact projection. An accepted decision and a newly scheduled attempt are distinct
// from successful admission or promotion.
// ImageAdmissionRiskResult возвращает принятое решение о риске, необязательную итоговую попытку
// допуска и текущее представление материала. Принятое решение и назначенная новая попытка отличаются
// от успешного допуска или публикации.
type ImageAdmissionRiskResult struct {
	Decision         ImageAdmissionRiskDecision
	AdmissionAttempt *ImageAdmissionAttempt
	Artifact         ImageArtifact
}
