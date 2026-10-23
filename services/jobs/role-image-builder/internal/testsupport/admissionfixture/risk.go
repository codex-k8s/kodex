// Package admissionfixture provides synthetic image-admission inputs shared by
// bridge and client tests. It issues no real decision, claim, credential or grant
// and depends only on runtime contracts to avoid importing either tested adapter.
// Package admissionfixture предоставляет искусственные входные данные допуска
// для тестов моста и клиента. Настоящие решения, claim, реквизиты и grants не
// выдаются; зависимость только от runtime-контрактов не связывает два адаптера.
package admissionfixture

import "github.com/codex-k8s/kodex/libs/go/runtimecontract"

// RiskAcceptance constructs the shared synthetic acceptance envelope, copying exact
// report/recipe/build/policy bindings and the caller's source receipt digest. Fixed
// decision/actor/time/reason fields preserve the original fixtures; the value is
// neither validated nor serialized here and does not confer runtime authority.
// RiskAcceptance строит общий искусственный конверт принятия риска с точными
// привязками отчёта, рецепта, сборки, политики и хешем исходной квитанции вызывающего.
// Фиксированные decision/actor/time/reason сохраняют прежние fixtures; значение
// здесь не проверяется и не сериализуется и не наделяет полномочиями runtime.
func RiskAcceptance(binding runtimecontract.ImageVulnerabilityReport, reportSHA256, projectionSHA256, sourceReceiptSHA256 string) runtimecontract.ImageRiskAcceptance {
	return runtimecontract.ImageRiskAcceptance{
		Schema: runtimecontract.ImageRiskAcceptanceSchema, DecisionRef: "imgrisk_abcdefgh", DecisionVersion: 1, Action: "ACCEPT_RISK",
		ScopeKind: binding.ScopeKind, OrganizationRef: binding.OrganizationRef, ArtifactRef: binding.ArtifactRef, ImageDigest: binding.ImageDigest,
		ReportSHA256: reportSHA256, ProjectionSHA256: projectionSHA256, SourceAdmissionRevision: 1,
		SourceAdmissionReceiptSHA256: sourceReceiptSHA256, SourceEvidenceManifestDigest: "sha256:" + sourceReceiptSHA256,
		RecipeRef: binding.RecipeRef, RecipeVersion: binding.RecipeVersion, RecipeGeneration: binding.RecipeGeneration,
		BuildRef: binding.BuildRef, BuildVersion: binding.BuildVersion, BuildAttempt: binding.BuildAttempt,
		PolicyRevision: binding.PolicyRevision, PolicySHA256: binding.PolicySHA256,
		Reason: "Риск принят для точного образа", DecidedByActorRef: "act_abcdefgh", DecidedAt: "2026-10-05T05:00:00Z",
	}
}
