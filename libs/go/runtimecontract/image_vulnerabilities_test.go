package runtimecontract

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func vulnerabilityTestBinding() ImageVulnerabilityReport {
	return ImageVulnerabilityReport{ArtifactRef: "imgart_abcdefgh", ImageDigest: "sha256:" + strings.Repeat("a", 64),
		SBOMSHA256: strings.Repeat("b", 64), ScopeKind: "ORGANIZATION", OrganizationRef: "org_abcdefgh",
		RecipeRef: "imgrec_abcdefgh", RecipeVersion: 2, RecipeGeneration: 2, BuildRef: "imgbld_abcdefgh",
		BuildVersion: 3, BuildAttempt: 1, PolicyRevision: 4, PolicySHA256: strings.Repeat("c", 64)}
}

func vulnerabilityTestMatch(id, severity, state string, versions []string) map[string]any {
	return map[string]any{
		"artifact":      map[string]any{"name": "example/pkg", "version": "v1.2.3", "type": "go-module", "locations": []any{map[string]any{"path": "/private-do-not-copy"}}},
		"vulnerability": map[string]any{"id": id, "severity": severity, "fix": map[string]any{"state": state, "versions": versions}, "dataSource": "https://internal.invalid/private"},
	}
}

func vulnerabilityTestSource(t *testing.T, matches, ignored []any, high, blocking, noFix int) []byte {
	t.Helper()
	value := map[string]any{"matches": matches, "ignoredMatches": ignored, "descriptor": map[string]any{"privateMetadata": "do-not-copy"},
		"kodexPolicy": map[string]any{"schema": "kodex.dev/fix-available-high-or-critical/v1", "policyRevision": 4,
			"policySHA256": strings.Repeat("c", 64), "highOrCriticalMatchCount": high, "blockingMatchCount": blocking, "unresolvedNoFixMatchCount": noFix}}
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func vulnerabilityTestReport(t *testing.T) ImageVulnerabilityReport {
	t.Helper()
	fixed := vulnerabilityTestMatch("CVE-2026-12345", "High", "fixed", []string{"v1.3.0", "v1.3.0"})
	raw := vulnerabilityTestSource(t, []any{fixed, fixed,
		vulnerabilityTestMatch("GO-2026-1234", "Critical", "not-fixed", []string{}),
		vulnerabilityTestMatch("GHSA-abcd-1234-5678", "Medium", "fixed", []string{"v2.0.0"}),
		vulnerabilityTestMatch("CVE-2026-54321", "Low", "fixed", []string{"v3.0.0"}),
		vulnerabilityTestMatch("OTHER-ONE", "Negligible", "unknown", []string{}),
		vulnerabilityTestMatch("OTHER-TWO", "Unknown", "wont-fix", []string{}),
	}, []any{fixed}, 3, 2, 1)
	report, err := ProjectImageVulnerabilityReport(raw, vulnerabilityTestBinding())
	if err != nil {
		t.Fatal(err)
	}
	return report
}

func TestImageVulnerabilityProjectionCompleteSafeAndCanonical(t *testing.T) {
	report := vulnerabilityTestReport(t)
	if report.MatchCount != 8 || report.UniqueAdvisoryCount != 6 || report.BlockingMatchCount != 2 ||
		report.UnresolvedNoFixMatchCount != 1 || report.SuppressedMatchCount != 1 || len(report.Findings) != 7 {
		t.Fatalf("complete counts mismatch: %+v", report)
	}
	raw, err := CanonicalImageVulnerabilityReport(report)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("private")) || bytes.Contains(raw, []byte("internal.invalid")) || bytes.Contains(raw, []byte("do-not-copy")) {
		t.Fatal("scanner metadata leaked into projection")
	}
	var grouped, ignored, goLink bool
	for _, finding := range report.Findings {
		grouped = grouped || finding.Occurrences == 2 && finding.Blocking
		ignored = ignored || finding.Ignored && !finding.Blocking && finding.Occurrences == 1
		goLink = goLink || finding.AdvisoryURL == "https://pkg.go.dev/vuln/GO-2026-1234"
	}
	if !grouped || !ignored || !goLink {
		t.Fatal("group, suppression or canonical advisory link missing")
	}
	decoded, err := DecodeImageVulnerabilityReport(raw)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := CanonicalImageVulnerabilityReport(decoded)
	if err != nil || !bytes.Equal(raw, encoded) {
		t.Fatal("canonical report round trip mismatch")
	}
}

func TestImageVulnerabilityReportRejectsIncompleteOrTamperedData(t *testing.T) {
	mutations := map[string]func(*ImageVulnerabilityReport){
		"missing scope":       func(v *ImageVulnerabilityReport) { v.OrganizationRef = "" },
		"foreign scope shape": func(v *ImageVulnerabilityReport) { v.ProjectRef = "prj_abcdefgh" },
		"zero build":          func(v *ImageVulnerabilityReport) { v.BuildAttempt = 0 },
		"unsafe version":      func(v *ImageVulnerabilityReport) { v.RecipeVersion = 9007199254740992 },
		"count":               func(v *ImageVulnerabilityReport) { v.MatchCount++ },
		"blocking":            func(v *ImageVulnerabilityReport) { v.BlockingMatchCount++ },
		"suppressed":          func(v *ImageVulnerabilityReport) { v.SuppressedMatchCount++ },
		"no fix":              func(v *ImageVulnerabilityReport) { v.UnresolvedNoFixMatchCount++ },
		"unique":              func(v *ImageVulnerabilityReport) { v.UniqueAdvisoryCount++ },
		"severity":            func(v *ImageVulnerabilityReport) { v.SeverityCounts[0].MatchCount++ },
		"missing findings":    func(v *ImageVulnerabilityReport) { v.Findings = nil },
		"noncanonical URL":    func(v *ImageVulnerabilityReport) { v.Findings[0].AdvisoryURL = "https://internal.invalid/" },
		"missing group":       func(v *ImageVulnerabilityReport) { v.Findings = v.Findings[1:] },
		"duplicate group":     func(v *ImageVulnerabilityReport) { v.Findings = append(v.Findings, v.Findings[0]) },
		"zero occurrences":    func(v *ImageVulnerabilityReport) { v.Findings[0].Occurrences = 0 },
	}
	for name, mutation := range mutations {
		t.Run(name, func(t *testing.T) {
			report := vulnerabilityTestReport(t)
			mutation(&report)
			if _, err := CanonicalImageVulnerabilityReport(report); err == nil {
				t.Fatal("tampered report accepted")
			}
		})
	}
	report := vulnerabilityTestReport(t)
	raw, _ := CanonicalImageVulnerabilityReport(report)
	for name, invalid := range map[string][]byte{
		"duplicate":  append([]byte(`{"schema":"duplicate",`), raw[1:]...),
		"unknown":    append([]byte(`{"unexpected":true,`), raw[1:]...),
		"whitespace": append([]byte(" "), raw...),
		"trailing":   append(append([]byte{}, raw...), []byte(`{}`)...),
		"oversize":   bytes.Repeat([]byte("x"), MaximumImageVulnerabilityReportBytes+1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeImageVulnerabilityReport(invalid); err == nil {
				t.Fatal("invalid encoding accepted")
			}
		})
	}
}

func TestImageVulnerabilitySourceRejectsUnknownStateAndPolicyMismatch(t *testing.T) {
	for name, match := range map[string]map[string]any{
		"unknown fix":           vulnerabilityTestMatch("CVE-2026-12345", "High", "custom", []string{}),
		"unknown severity":      vulnerabilityTestMatch("CVE-2026-12345", "Informational", "unknown", []string{}),
		"unsafe advisory":       vulnerabilityTestMatch("CVE-2026-12345\nsecret", "High", "unknown", []string{}),
		"fixed without version": vulnerabilityTestMatch("CVE-2026-12345", "High", "fixed", []string{}),
	} {
		t.Run(name, func(t *testing.T) {
			raw := vulnerabilityTestSource(t, []any{match}, []any{}, 1, 0, 1)
			if _, err := ProjectImageVulnerabilityReport(raw, vulnerabilityTestBinding()); err == nil {
				t.Fatal("invalid source accepted")
			}
		})
	}
	raw := vulnerabilityTestSource(t, []any{}, []any{}, 0, 0, 0)
	if report, err := ProjectImageVulnerabilityReport(raw, vulnerabilityTestBinding()); err != nil || report.MatchCount != 0 || report.Findings == nil {
		t.Fatal("complete empty source rejected")
	}
	for name, binding := range map[string]ImageVulnerabilityReport{
		"wrong hash": func() ImageVulnerabilityReport {
			v := vulnerabilityTestBinding()
			v.ReportSHA256 = strings.Repeat("e", 64)
			return v
		}(),
		"wrong policy": func() ImageVulnerabilityReport { v := vulnerabilityTestBinding(); v.PolicyRevision++; return v }(),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ProjectImageVulnerabilityReport(raw, binding); err == nil {
				t.Fatal("wrong source binding accepted")
			}
		})
	}
	duplicate := append([]byte(`{"matches":[],`), raw[1:]...)
	if _, err := ProjectImageVulnerabilityReport(duplicate, vulnerabilityTestBinding()); err == nil {
		t.Fatal("duplicate source key accepted")
	}
}

func TestImageVulnerabilityScannerMetadataDepthIsBoundedIndependently(t *testing.T) {
	raw := vulnerabilityTestSource(t, []any{}, []any{}, 0, 0, 0)
	for _, depth := range []int{12, maximumImageVulnerabilityJSONDepth + 2} {
		metadata := strings.Repeat(`{"nested":`, depth) + `"private"` + strings.Repeat(`}`, depth)
		input := append([]byte(`{"scannerMetadata":`+metadata+`,`), raw[1:]...)
		report, err := ProjectImageVulnerabilityReport(input, vulnerabilityTestBinding())
		if depth == 12 {
			if err != nil || report.MatchCount != 0 {
				t.Fatal("valid external metadata inherited prompt depth limit")
			}
		} else if err == nil {
			t.Fatal("unbounded external metadata accepted")
		}
	}
}
