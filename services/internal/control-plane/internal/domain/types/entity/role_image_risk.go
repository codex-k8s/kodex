package entity

import (
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// ImageVulnerabilityReport хранит безопасную проекцию вместе с owner pins.
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

// ImageAdmissionRiskDecision неизменяемо связывает человеческое решение с отчётом.
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

type ImageAdmissionAttempt struct {
	Ref, ArtifactRef, RiskDecisionRef, State       string
	AdmissionReceiptSHA256, EvidenceManifestDigest string
	Version, Fence                                 uint64
	Number                                         uint32
	SourceAdmissionRevision                        uint64
	CreatedAt                                      time.Time
}

type ImageAdmissionRiskResult struct {
	Decision         ImageAdmissionRiskDecision
	AdmissionAttempt *ImageAdmissionAttempt
	Artifact         ImageArtifact
}
