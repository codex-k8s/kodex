package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/role-image-builder/internal/clients/imageowner"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func bridgeRiskFixture(t *testing.T, risk bool) (imageowner.Claim, imageowner.AdmissionEvidence) {
	t.Helper()
	hex := strings.Repeat("a", 64)
	binding := runtimecontract.ImageVulnerabilityReport{ArtifactRef: "imgart_abcdefgh", ImageDigest: "sha256:" + hex, SBOMSHA256: hex,
		ScopeKind: "ORGANIZATION", OrganizationRef: "org_abcdefgh", RecipeRef: "imgrec_abcdefgh", RecipeVersion: 2, RecipeGeneration: 3,
		BuildRef: "imgbld_abcdefgh", BuildVersion: 4, BuildAttempt: 5, PolicyRevision: 6, PolicySHA256: hex}
	matches := []any{}
	blocking := 0
	if risk {
		blocking = 1
		matches = append(matches, map[string]any{"artifact": map[string]any{"name": "example/pkg", "version": "v1.0.0", "type": "go-module"},
			"vulnerability": map[string]any{"id": "GO-2026-1234", "severity": "High", "fix": map[string]any{"state": "fixed", "versions": []string{"v1.0.1"}}}})
	}
	source, _ := json.Marshal(map[string]any{"matches": matches, "ignoredMatches": []any{}, "kodexPolicy": map[string]any{
		"schema": "kodex.dev/fix-available-high-or-critical/v1", "policyRevision": 6, "policySHA256": hex, "highOrCriticalMatchCount": blocking, "blockingMatchCount": blocking, "unresolvedNoFixMatchCount": 0}})
	report, err := runtimecontract.ProjectImageVulnerabilityReport(source, binding)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := runtimecontract.CanonicalImageVulnerabilityReport(report)
	if err != nil {
		t.Fatal(err)
	}
	claim := imageowner.Claim{ScopeKind: binding.ScopeKind, OrganizationRef: binding.OrganizationRef, ArtifactID: binding.ArtifactRef,
		Version: 7, Fence: 8, AuthorityGeneration: 9, ClaimToken: "private-fixture-token", ExpiresAt: time.Date(2026, 10, 5, 6, 0, 0, 0, time.UTC),
		RecipeID: binding.RecipeRef, RecipeVersion: binding.RecipeVersion, RecipeGeneration: binding.RecipeGeneration,
		BuildID: binding.BuildRef, BuildVersion: binding.BuildVersion, BuildAttempt: binding.BuildAttempt, ManifestDigest: binding.ImageDigest,
		PolicyRevision: binding.PolicyRevision, PolicySHA256: binding.PolicySHA256, AdmissionAttemptRef: "imgadm_abcdefgh", AdmissionAttempt: 2}
	evidence := imageowner.AdmissionEvidence{SBOMSHA256: report.SBOMSHA256, VulnerabilityEvidenceSHA256: report.ReportSHA256, Accepted: true,
		VulnerabilityReportJSON: string(raw), VulnerabilityReportProjectionSHA256: runtimecontract.ImageVulnerabilitySHA256(raw)}
	if risk {
		decision := runtimecontract.ImageRiskAcceptance{Schema: runtimecontract.ImageRiskAcceptanceSchema, DecisionRef: "imgrisk_abcdefgh", DecisionVersion: 1, Action: "ACCEPT_RISK",
			ScopeKind: binding.ScopeKind, OrganizationRef: binding.OrganizationRef, ArtifactRef: binding.ArtifactRef, ImageDigest: binding.ImageDigest,
			ReportSHA256: report.ReportSHA256, ProjectionSHA256: evidence.VulnerabilityReportProjectionSHA256, SourceAdmissionRevision: 1,
			SourceAdmissionReceiptSHA256: hex, SourceEvidenceManifestDigest: "sha256:" + hex, RecipeRef: binding.RecipeRef, RecipeVersion: binding.RecipeVersion, RecipeGeneration: binding.RecipeGeneration,
			BuildRef: binding.BuildRef, BuildVersion: binding.BuildVersion, BuildAttempt: binding.BuildAttempt, PolicyRevision: binding.PolicyRevision, PolicySHA256: binding.PolicySHA256,
			Reason: "Риск принят для точного образа", DecidedByActorRef: "act_abcdefgh", DecidedAt: "2026-10-05T05:00:00Z"}
		riskRaw, err := runtimecontract.CanonicalImageRiskAcceptance(decision)
		if err != nil {
			t.Fatal(err)
		}
		claim.RiskAcceptanceJSON, claim.RiskAcceptanceSHA256 = string(riskRaw), runtimecontract.ImageVulnerabilitySHA256(riskRaw)
		claim.SourceAdmissionReceiptSHA256, claim.SourceEvidenceManifestDigest, claim.SourceAdmissionRevision = hex, "sha256:"+hex, 1
		evidence.RiskAcceptanceSHA256 = claim.RiskAcceptanceSHA256
	}
	return claim, evidence
}

func TestClaimStateRetainsExactAttemptRiskAndNoClobberRecovery(t *testing.T) {
	claim, _ := bridgeRiskFixture(t, true)
	path := filepath.Join(t.TempDir(), "owner-claim.json")
	if writeClaimState(path, claim) != nil || writeClaimState(path, claim) != nil {
		t.Fatal("exact first claim or replay rejected")
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var restored imageowner.Claim
	if readState(path, &restored) != nil || !reflect.DeepEqual(restored, claim) {
		t.Fatal("claim recovery dropped risk/attempt/credential snapshot")
	}
	for _, mutate := range []func(*imageowner.Claim){
		func(v *imageowner.Claim) { v.ClaimToken = "replacement-fixture-token" },
		func(v *imageowner.Claim) { v.Fence++ },
		func(v *imageowner.Claim) { v.AdmissionAttempt++ },
		func(v *imageowner.Claim) { v.AdmissionAttemptRef = "imgadm_ijklmnop" },
	} {
		changed := claim
		mutate(&changed)
		if writeClaimState(path, changed) == nil {
			t.Fatal("retained claim was replaced")
		}
		after, _ := os.ReadFile(path)
		if !bytes.Equal(before, after) {
			t.Fatal("failed replacement mutated state")
		}
	}
	if admissionIdempotencyKey("record", "run-fixture", claim) != admissionIdempotencyKey("record", "run-fixture", restored) {
		t.Fatal("recovery changed receipt key")
	}
	changed := claim
	changed.AdmissionAttempt++
	if admissionIdempotencyKey("record", "run-fixture", claim) == admissionIdempotencyKey("record", "run-fixture", changed) {
		t.Fatal("distinct attempt shared receipt key")
	}
	changed = claim
	changed.AdmissionAttemptRef = "imgadm_ijklmnop"
	if admissionIdempotencyKey("fail", "run-fixture", claim) == admissionIdempotencyKey("fail", "run-fixture", changed) {
		t.Fatal("distinct attempt ref shared terminal key")
	}
}

func TestConcurrentClaimPublicationKeepsOneImmutableWinner(t *testing.T) {
	first, _ := bridgeRiskFixture(t, false)
	second := first
	second.ClaimToken = "other-private-fixture-token"
	path := filepath.Join(t.TempDir(), "owner-claim.json")
	start := make(chan struct{})
	outcomes := make(chan error, 2)
	for _, claim := range []imageowner.Claim{first, second} {
		go func() { <-start; outcomes <- writeClaimState(path, claim) }()
	}
	close(start)
	one, two := <-outcomes, <-outcomes
	if (one == nil) == (two == nil) {
		t.Fatal("concurrent publication did not retain one winner")
	}
	var retained imageowner.Claim
	if readState(path, &retained) != nil || (!reflect.DeepEqual(retained, first) && !reflect.DeepEqual(retained, second)) {
		t.Fatal("concurrent publication produced partial or synthetic state")
	}
}

func TestReportFilesCarryOnlyCanonicalExactClaimEvidence(t *testing.T) {
	for _, risk := range []bool{false, true} {
		claim, evidence := bridgeRiskFixture(t, risk)
		dir := t.TempDir()
		reportPath := filepath.Join(dir, "vulnerability-report.json")
		digestPath := filepath.Join(dir, "vulnerability-report.sha256")
		if os.WriteFile(reportPath, []byte(evidence.VulnerabilityReportJSON), 0600) != nil || os.WriteFile(digestPath, []byte(evidence.VulnerabilityReportProjectionSHA256+"\n"), 0600) != nil {
			t.Fatal("write report fixture")
		}
		t.Setenv("IMAGE_OWNER_VULNERABILITY_REPORT_JSON_FILE", reportPath)
		t.Setenv("IMAGE_OWNER_VULNERABILITY_REPORT_PROJECTION_SHA256_FILE", digestPath)
		t.Setenv("IMAGE_OWNER_RISK_ACCEPTANCE_SHA256", strings.Repeat("b", 64))
		original := evidence
		evidence.VulnerabilityReportJSON, evidence.VulnerabilityReportProjectionSHA256, evidence.RiskAcceptanceSHA256 = "", "", ""
		if readVulnerabilityReportEvidence(claim, &evidence) != nil || evidence != original {
			t.Fatal("canonical report or retained risk digest lost")
		}
		invalid := `{"credential":"private-do-not-print",` + original.VulnerabilityReportJSON[1:]
		if os.WriteFile(reportPath, []byte(invalid), 0600) != nil {
			t.Fatal("write invalid report fixture")
		}
		err := readVulnerabilityReportEvidence(claim, &evidence)
		if err == nil || strings.Contains(err.Error(), "private-do-not-print") {
			t.Fatal("unknown report accepted or leaked")
		}
	}
}

func TestReportInputIsBoundedPrivateRegularAndNoFollow(t *testing.T) {
	for _, scenario := range []string{"valid", "symlink", "fifo", "oversized", "public", "empty"} {
		t.Run(scenario, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "report.json")
			raw := []byte("{}")
			if scenario == "oversized" {
				raw = bytes.Repeat([]byte("x"), 17)
			}
			if scenario == "empty" {
				raw = nil
			}
			if scenario == "fifo" {
				if syscall.Mkfifo(path, 0600) != nil {
					t.Fatal("create FIFO fixture")
				}
			} else if os.WriteFile(path, raw, 0600) != nil {
				t.Fatal("write report fixture")
			}
			if scenario == "public" {
				if os.Chmod(path, 0644) != nil {
					t.Fatal("chmod fixture")
				}
			}
			if scenario == "symlink" {
				link := path + ".alias"
				if os.Symlink(path, link) != nil {
					t.Fatal("create symlink fixture")
				}
				path = link
			}
			t.Setenv("IMAGE_OWNER_VULNERABILITY_REPORT_JSON_FILE", path)
			_, err := readBoundedPrivateFile("IMAGE_OWNER_VULNERABILITY_REPORT_JSON_FILE", 16)
			if (err == nil) != (scenario == "valid") {
				t.Fatal("report violated bounded private regular no-follow contract")
			}
		})
	}
}

func TestBridgeDiagnosticNeverPrintsRemoteCredentialsOrPayload(t *testing.T) {
	const privateDetail = "private-fixture-token raw-report internal-url"
	for _, code := range []codes.Code{codes.Internal, codes.Unavailable, codes.Unknown, codes.DataLoss, codes.InvalidArgument, codes.PermissionDenied} {
		for _, err := range []error{status.Error(code, privateDetail), fmt.Errorf("%s: %w", privateDetail, status.Error(code, privateDetail))} {
			if bridgeDiagnostic(err) != "image admission bridge failed: "+code.String() {
				t.Fatal("remote diagnostics leaked")
			}
		}
	}
	if bridgeDiagnostic(fmt.Errorf("%s: %w", privateDetail, errors.New(privateDetail))) != "image admission bridge failed: Unknown" {
		t.Fatal("local diagnostics leaked")
	}
}
