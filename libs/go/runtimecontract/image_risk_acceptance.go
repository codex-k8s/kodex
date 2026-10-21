package runtimecontract

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// ImageRiskAcceptanceSchema pins the only accepted owner decision schema.
// ImageRiskAcceptanceSchema закрепляет единственную допустимую схему решения владельца.
const ImageRiskAcceptanceSchema = "kodex.dev/image-risk-acceptance/v1"

// MaximumImageRiskAcceptanceBytes bounds the complete serialized decision.
// MaximumImageRiskAcceptanceBytes ограничивает размер всего сериализованного решения.
const MaximumImageRiskAcceptanceBytes = 16 << 10

// MaximumImageRiskReasonBytes bounds the owner's human-readable justification.
// MaximumImageRiskReasonBytes ограничивает размер текстового обоснования владельца.
const MaximumImageRiskReasonBytes = 2048

// errImageRiskAcceptance is the closed-failure result for malformed or mismatched decisions.
// errImageRiskAcceptance обозначает закрытый отказ для неверного или несоответствующего решения.
var errImageRiskAcceptance = errors.New("image risk acceptance is invalid")

// ImageRiskAcceptance binds an owner decision to one image, report, build, and policy revision;
// a worker neither creates this decision nor derives transport authority from its identifiers.
// ImageRiskAcceptance связывает решение владельца с конкретными образом, отчётом, сборкой и версией политики;
// worker не создаёт решение и не выводит полномочия транспорта из его идентификаторов.
type ImageRiskAcceptance struct {
	Schema                       string `json:"schema"`
	DecisionRef                  string `json:"decisionRef"`
	DecisionVersion              uint64 `json:"decisionVersion"`
	Action                       string `json:"action"`
	ScopeKind                    string `json:"scopeKind"`
	OrganizationRef              string `json:"organizationRef"`
	ProjectRef                   string `json:"projectRef"`
	ArtifactRef                  string `json:"artifactRef"`
	ImageDigest                  string `json:"imageDigest"`
	ReportSHA256                 string `json:"reportSHA256"`
	ProjectionSHA256             string `json:"projectionSHA256"`
	SourceAdmissionRevision      uint64 `json:"sourceAdmissionRevision"`
	SourceAdmissionReceiptSHA256 string `json:"sourceAdmissionReceiptSHA256"`
	SourceEvidenceManifestDigest string `json:"sourceEvidenceManifestDigest"`
	RecipeRef                    string `json:"recipeRef"`
	RecipeVersion                uint64 `json:"recipeVersion"`
	RecipeGeneration             uint64 `json:"recipeGeneration"`
	BuildRef                     string `json:"buildRef"`
	BuildVersion                 uint64 `json:"buildVersion"`
	BuildAttempt                 uint32 `json:"buildAttempt"`
	PolicyRevision               uint64 `json:"policyRevision"`
	PolicySHA256                 string `json:"policySHA256"`
	Reason                       string `json:"reason"`
	DecidedByActorRef            string `json:"decidedByActorRef"`
	DecidedAt                    string `json:"decidedAt"`
}

// Validate rejects incomplete, noncanonical, or unpinned risk-acceptance fields before use.
// Validate отклоняет неполные, неканонические или не закреплённые по версии поля решения до использования.
func (value ImageRiskAcceptance) Validate() error {
	decidedAt, err := time.Parse(time.RFC3339Nano, value.DecidedAt)
	if err != nil || decidedAt.IsZero() || decidedAt.UTC().Format(time.RFC3339Nano) != value.DecidedAt ||
		value.Schema != ImageRiskAcceptanceSchema || !vulnerabilityRef(value.DecisionRef, "imgrisk") ||
		value.DecisionVersion != 1 || value.Action != "ACCEPT_RISK" ||
		!vulnerabilityScope(value.ScopeKind, value.OrganizationRef, value.ProjectRef) ||
		!vulnerabilityRef(value.ArtifactRef, "imgart") || !imageDigestPattern.MatchString(value.ImageDigest) ||
		!lowerHexDigestPattern.MatchString(value.ReportSHA256) || !lowerHexDigestPattern.MatchString(value.ProjectionSHA256) ||
		!vulnerabilityPin(value.SourceAdmissionRevision) || !lowerHexDigestPattern.MatchString(value.SourceAdmissionReceiptSHA256) ||
		!imageDigestPattern.MatchString(value.SourceEvidenceManifestDigest) ||
		!vulnerabilityRef(value.RecipeRef, "imgrec") || !vulnerabilityPin(value.RecipeVersion) || !vulnerabilityPin(value.RecipeGeneration) ||
		!vulnerabilityRef(value.BuildRef, "imgbld") || !vulnerabilityPin(value.BuildVersion) || value.BuildAttempt == 0 ||
		!vulnerabilityPin(value.PolicyRevision) || !lowerHexDigestPattern.MatchString(value.PolicySHA256) ||
		!vulnerabilitySafeText(value.Reason, MaximumImageRiskReasonBytes) || strings.TrimSpace(value.Reason) != value.Reason ||
		!opaqueReferencePattern.MatchString(value.DecidedByActorRef) {
		return errImageRiskAcceptance
	}
	return nil
}

// CanonicalImageRiskAcceptance validates and size-bounds the JSON used for a decision digest.
// CanonicalImageRiskAcceptance проверяет и ограничивает размер JSON, используемого для дайджеста решения.
func CanonicalImageRiskAcceptance(value ImageRiskAcceptance) ([]byte, error) {
	if value.Validate() != nil {
		return nil, errImageRiskAcceptance
	}
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > MaximumImageRiskAcceptanceBytes {
		return nil, errImageRiskAcceptance
	}
	return raw, nil
}

// DecodeImageRiskAcceptance accepts only a bounded, strict JSON decision with valid fields.
// DecodeImageRiskAcceptance принимает только ограниченное строгое JSON-решение с допустимыми полями.
func DecodeImageRiskAcceptance(raw []byte) (ImageRiskAcceptance, error) {
	var value ImageRiskAcceptance
	if decodeVulnerabilityJSON(raw, &value, MaximumImageRiskAcceptanceBytes, true) != nil || value.Validate() != nil {
		return ImageRiskAcceptance{}, errImageRiskAcceptance
	}
	return value, nil
}

// VerifyImageRiskAcceptanceReportEnvelope rejects a decision/report envelope whose pinned facts disagree.
// VerifyImageRiskAcceptanceReportEnvelope отклоняет пару решения и отчёта при несовпадении закреплённых фактов.
func VerifyImageRiskAcceptanceReportEnvelope(raw []byte) error {
	var envelope struct {
		RiskAcceptance ImageRiskAcceptance      `json:"riskAcceptance"`
		Report         ImageVulnerabilityReport `json:"report"`
	}
	if decodeVulnerabilityJSON(raw, &envelope, MaximumImageVulnerabilityReportBytes+MaximumImageRiskAcceptanceBytes, false) != nil ||
		!ImageRiskAcceptanceMatchesReport(envelope.RiskAcceptance, envelope.Report) {
		return errImageRiskAcceptance
	}
	return nil
}

// ImageRiskAcceptanceMatchesReport compares the shared decision/report tuple; owner receipt,
// previous evidence manifest, and fresh admission attempt/fence still require separate checks.
// ImageRiskAcceptanceMatchesReport сравнивает общий набор полей решения и отчёта; owner receipt,
// предыдущий evidence manifest и свежие admission attempt/fence проверяются отдельно.
func ImageRiskAcceptanceMatchesReport(decision ImageRiskAcceptance, report ImageVulnerabilityReport) bool {
	projection, err := CanonicalImageVulnerabilityReport(report)
	return err == nil && decision.Validate() == nil && report.BlockingMatchCount > 0 &&
		decision.ScopeKind == report.ScopeKind && decision.OrganizationRef == report.OrganizationRef && decision.ProjectRef == report.ProjectRef &&
		decision.ArtifactRef == report.ArtifactRef && decision.ImageDigest == report.ImageDigest &&
		decision.ReportSHA256 == report.ReportSHA256 && decision.ProjectionSHA256 == ImageVulnerabilitySHA256(projection) &&
		decision.RecipeRef == report.RecipeRef && decision.RecipeVersion == report.RecipeVersion && decision.RecipeGeneration == report.RecipeGeneration &&
		decision.BuildRef == report.BuildRef && decision.BuildVersion == report.BuildVersion && decision.BuildAttempt == report.BuildAttempt &&
		decision.PolicyRevision == report.PolicyRevision && decision.PolicySHA256 == report.PolicySHA256
}
