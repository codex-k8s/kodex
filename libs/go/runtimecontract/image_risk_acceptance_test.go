package runtimecontract

import (
	"bytes"
	"strings"
	"testing"
)

func riskAcceptanceTestValue(t *testing.T) ImageRiskAcceptance {
	t.Helper()
	report := vulnerabilityTestReport(t)
	raw, err := CanonicalImageVulnerabilityReport(report)
	if err != nil {
		t.Fatal(err)
	}
	return ImageRiskAcceptance{Schema: ImageRiskAcceptanceSchema, DecisionRef: "imgrisk_abcdefgh", DecisionVersion: 1, Action: "ACCEPT_RISK",
		ScopeKind: report.ScopeKind, OrganizationRef: report.OrganizationRef, ProjectRef: report.ProjectRef, ArtifactRef: report.ArtifactRef,
		ImageDigest: report.ImageDigest, ReportSHA256: report.ReportSHA256, ProjectionSHA256: ImageVulnerabilitySHA256(raw),
		SourceAdmissionRevision: 2, SourceAdmissionReceiptSHA256: strings.Repeat("d", 64), SourceEvidenceManifestDigest: "sha256:" + strings.Repeat("e", 64),
		RecipeRef: report.RecipeRef, RecipeVersion: report.RecipeVersion, RecipeGeneration: report.RecipeGeneration,
		BuildRef: report.BuildRef, BuildVersion: report.BuildVersion, BuildAttempt: report.BuildAttempt,
		PolicyRevision: report.PolicyRevision, PolicySHA256: report.PolicySHA256, Reason: "Риск оценён для точного образа",
		DecidedByActorRef: "act_abcdefgh", DecidedAt: "2026-10-05T06:00:00.123456789Z"}
}

func TestImageRiskAcceptanceExactBindingAndCanonicalRoundTrip(t *testing.T) {
	value := riskAcceptanceTestValue(t)
	raw, err := CanonicalImageRiskAcceptance(value)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeImageRiskAcceptance(raw)
	if err != nil || decoded != value {
		t.Fatal("risk canonical round trip mismatch")
	}
	if !ImageRiskAcceptanceMatchesReport(value, vulnerabilityTestReport(t)) {
		t.Fatal("exact risk/report binding rejected")
	}
	value.ProjectionSHA256 = strings.Repeat("f", 64)
	if ImageRiskAcceptanceMatchesReport(value, vulnerabilityTestReport(t)) {
		t.Fatal("foreign projection accepted")
	}
}

func TestImageRiskAcceptanceRejectsUnsafeOrIncompleteBinding(t *testing.T) {
	mutations := map[string]func(*ImageRiskAcceptance){
		"reject action":          func(v *ImageRiskAcceptance) { v.Action = "REJECT_RISK" },
		"missing actor":          func(v *ImageRiskAcceptance) { v.DecidedByActorRef = "" },
		"missing receipt":        func(v *ImageRiskAcceptance) { v.SourceAdmissionReceiptSHA256 = "" },
		"missing evidence":       func(v *ImageRiskAcceptance) { v.SourceEvidenceManifestDigest = "" },
		"zero revision":          func(v *ImageRiskAcceptance) { v.SourceAdmissionRevision = 0 },
		"wrong decision version": func(v *ImageRiskAcceptance) { v.DecisionVersion = 2 },
		"wrong ref":              func(v *ImageRiskAcceptance) { v.DecisionRef = "imgart_abcdefgh" },
		"blank reason":           func(v *ImageRiskAcceptance) { v.Reason = "" },
		"trim reason":            func(v *ImageRiskAcceptance) { v.Reason = " reason " },
		"control reason":         func(v *ImageRiskAcceptance) { v.Reason = "reason\nsecret" },
		"oversize reason":        func(v *ImageRiskAcceptance) { v.Reason = strings.Repeat("я", 1025) },
		"bad timestamp":          func(v *ImageRiskAcceptance) { v.DecidedAt = "2026-10-05T06:00:00+00:00" },
		"noncanonical timestamp": func(v *ImageRiskAcceptance) { v.DecidedAt = "2026-10-05T06:00:00.000Z" },
	}
	for name, mutation := range mutations {
		t.Run(name, func(t *testing.T) {
			v := riskAcceptanceTestValue(t)
			mutation(&v)
			if _, err := CanonicalImageRiskAcceptance(v); err == nil {
				t.Fatal("invalid risk accepted")
			}
		})
	}
	raw, _ := CanonicalImageRiskAcceptance(riskAcceptanceTestValue(t))
	for name, invalid := range map[string][]byte{
		"duplicate":  append([]byte(`{"action":"ACCEPT_RISK",`), raw[1:]...),
		"unknown":    append([]byte(`{"claimToken":"private",`), raw[1:]...),
		"whitespace": append([]byte(" "), raw...),
		"trailing":   append(append([]byte{}, raw...), []byte(`{}`)...),
		"oversize":   bytes.Repeat([]byte("x"), MaximumImageRiskAcceptanceBytes+1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeImageRiskAcceptance(invalid); err == nil {
				t.Fatal("invalid risk encoding accepted")
			}
		})
	}
}

func TestImageRiskAcceptanceRejectsChangedOwnerBuildPolicyAndImage(t *testing.T) {
	mutations := map[string]func(*ImageRiskAcceptance){
		"organization":   func(v *ImageRiskAcceptance) { v.OrganizationRef = "org_ijklmnop" },
		"project":        func(v *ImageRiskAcceptance) { v.ScopeKind = "PROJECT"; v.ProjectRef = "prj_abcdefgh" },
		"artifact":       func(v *ImageRiskAcceptance) { v.ArtifactRef = "imgart_ijklmnop" },
		"image":          func(v *ImageRiskAcceptance) { v.ImageDigest = "sha256:" + strings.Repeat("f", 64) },
		"report":         func(v *ImageRiskAcceptance) { v.ReportSHA256 = strings.Repeat("f", 64) },
		"recipe":         func(v *ImageRiskAcceptance) { v.RecipeRef = "imgrec_ijklmnop" },
		"recipe version": func(v *ImageRiskAcceptance) { v.RecipeVersion++ },
		"generation":     func(v *ImageRiskAcceptance) { v.RecipeGeneration++ },
		"build":          func(v *ImageRiskAcceptance) { v.BuildRef = "imgbld_ijklmnop" },
		"build version":  func(v *ImageRiskAcceptance) { v.BuildVersion++ },
		"build attempt":  func(v *ImageRiskAcceptance) { v.BuildAttempt++ },
		"policy":         func(v *ImageRiskAcceptance) { v.PolicyRevision++ },
		"policy hash":    func(v *ImageRiskAcceptance) { v.PolicySHA256 = strings.Repeat("f", 64) },
	}
	for name, mutation := range mutations {
		t.Run(name, func(t *testing.T) {
			v := riskAcceptanceTestValue(t)
			mutation(&v)
			if ImageRiskAcceptanceMatchesReport(v, vulnerabilityTestReport(t)) {
				t.Fatal("changed immutable tuple accepted")
			}
		})
	}
}
