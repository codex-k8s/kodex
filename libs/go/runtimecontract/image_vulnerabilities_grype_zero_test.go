package runtimecontract

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"testing"
)

func TestImageVulnerabilityGrypeExplicitZeroFixStateIsUnknown(t *testing.T) {
	// Форма воспроизводит Grype v0.117.0; scanner metadata не входит в проекцию.
	zero := vulnerabilityTestMatch("CVE-2026-12345", "High", "", []string{})
	fixed := vulnerabilityTestMatch("GO-2026-1234", "Critical", "fixed", []string{"v1.3.0"})
	source := vulnerabilityTestSource(t, []any{zero, zero, fixed}, []any{zero}, 3, 1, 2)
	envelope, err := json.Marshal(map[string]any{"binding": vulnerabilityTestBinding(), "reportBytesBase64": base64.StdEncoding.EncodeToString(source)})
	if err != nil {
		t.Fatal(err)
	}
	report, err := ProjectImageVulnerabilityEnvelope(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if report.MatchCount != 4 || report.BlockingMatchCount != 1 || report.UnresolvedNoFixMatchCount != 2 || report.SuppressedMatchCount != 1 || report.ReportSHA256 != ImageVulnerabilitySHA256(source) {
		t.Fatal("source counts, policy verdict or original bytes binding changed")
	}
	var unresolved, suppressed bool
	for _, finding := range report.Findings {
		if finding.AdvisoryID != "CVE-2026-12345" {
			continue
		}
		if finding.FixState != "UNKNOWN" || finding.Blocking || finding.FixedVersions == nil || len(finding.FixedVersions) != 0 {
			t.Fatal("zero scanner state gained fix authority")
		}
		unresolved = unresolved || !finding.Ignored && finding.Occurrences == 2
		suppressed = suppressed || finding.Ignored && finding.Occurrences == 1
	}
	if !unresolved || !suppressed {
		t.Fatal("zero-state occurrences or ignored boundary lost")
	}
	canonical, err := CanonicalImageVulnerabilityReport(report)
	if err != nil || bytes.Contains(canonical, []byte(`"fixState":""`)) {
		t.Fatal("noncanonical scanner state escaped projection")
	}
	if _, err := DecodeImageVulnerabilityReport(canonical); err != nil {
		t.Fatal(err)
	}
}

func TestImageVulnerabilityGrypeZeroFixStateRejectsIncompleteOrContradictoryShape(t *testing.T) {
	for _, name := range []string{"missing state", "null state", "missing fix", "null fix", "missing versions", "null versions", "nonempty versions", "unknown state", "canonical empty state"} {
		t.Run(name, func(t *testing.T) {
			match := vulnerabilityTestMatch("CVE-2026-12345", "High", "", []string{})
			vulnerability := match["vulnerability"].(map[string]any)
			fix := vulnerability["fix"].(map[string]any)
			switch name {
			case "missing state":
				delete(fix, "state")
			case "null state":
				fix["state"] = nil
			case "missing fix":
				delete(vulnerability, "fix")
			case "null fix":
				vulnerability["fix"] = nil
			case "missing versions":
				delete(fix, "versions")
			case "null versions":
				fix["versions"] = nil
			case "nonempty versions":
				fix["versions"] = []string{"v1.3.0"}
			case "unknown state":
				fix["state"] = "custom"
			case "canonical empty state":
				report := vulnerabilityTestReport(t)
				report.Findings[0].FixState = ""
				report.Findings[0].Ref = imageVulnerabilityFindingRef(report.Findings[0])
				if report.Validate() == nil {
					t.Fatal("canonical closed enum weakened")
				}
				return
			}
			source := vulnerabilityTestSource(t, []any{match}, []any{}, 1, 0, 1)
			if _, err := ProjectImageVulnerabilityReport(source, vulnerabilityTestBinding()); err == nil {
				t.Fatal("incomplete or contradictory scanner fix shape accepted")
			}
		})
	}
}
