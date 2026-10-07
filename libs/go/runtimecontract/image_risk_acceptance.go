package runtimecontract

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
)

const ImageRiskAcceptanceSchema = "kodex.dev/image-risk-acceptance/v1"
const MaximumImageRiskAcceptanceBytes = 16 << 10
const MaximumImageRiskReasonBytes = 2048

var errImageRiskAcceptance = errors.New("image risk acceptance is invalid")

// Binding содержит только назначенные владельцем поля. Worker не создаёт
// решение и не заменяет transport authority проверкой этих идентификаторов.
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

func DecodeImageRiskAcceptance(raw []byte) (ImageRiskAcceptance, error) {
	var value ImageRiskAcceptance
	if decodeVulnerabilityJSON(raw, &value, MaximumImageRiskAcceptanceBytes, true) != nil || value.Validate() != nil {
		return ImageRiskAcceptance{}, errImageRiskAcceptance
	}
	return value, nil
}

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

// Проверка общего tuple дополняет, а не заменяет проверку owner receipt,
// предыдущего evidence manifest и свежего admission attempt/fence.
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
