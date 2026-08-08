package app

import (
	"strings"
	"testing"
	"time"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/matter-codex/services/jobs/legacy-data-migration/internal/planner"
)

func TestConfigValidateFailClosed(t *testing.T) {
	t.Parallel()
	valid := Config{
		Mode: "dry-run", PlanID: "11111111-1111-4111-8111-111111111111",
		SourceRootReference: "vault://legacy/root", SourceRootSHA256: strings.Repeat("a", 64),
		SourceDSNFile:       "/run/source-dsn",
		SourceTLSServerName: "source.database.svc", SourceCAFile: "/run/source-ca.pem",
		ControlPlaneTarget: "control-plane:8443", ControlPlaneTLSServerName: "control-plane.internal",
		ControlPlaneCAFile: "/run/control-ca.pem", ControlPlaneCertificateFile: "/run/tls.crt",
		ControlPlanePrivateKeyFile: "/run/tls.key", ApplicationGrantFile: "/run/grant.jws",
		AuthorityPolicyFile: "/run/authority-policy.json", OwnerEvidenceFile: "/run/owner-evidence.json",
		AuthorityPolicyRevision: 1, AuthorityPolicySHA256: strings.Repeat("b", 64),
		ImagePolicyRevision: 1, ImagePolicySHA256: strings.Repeat("c", 64),
		RoleRuntimeContractRevision: 1, RoleRuntimeContractSHA256: strings.Repeat("d", 64),
		RoleImageInputRepository: "registry.internal/inputs", TrustedRoleBaseRepository: "registry.internal/base",
		TrustedRoleBaseDigest: "sha256:" + strings.Repeat("e", 64), ControlPlaneRPCDeadline: 10 * time.Second,
		OwnerEvidence: planner.Evidence{RoleImage: &controlplanev1.RoleImageRecipeInput{
			BaseImageReference: "registry.internal/base", BaseImageDigest: "sha256:" + strings.Repeat("e", 64),
			ContextRef: "oci://registry.internal/inputs@sha256:" + strings.Repeat("f", 64),
		}},
		BackupDirectory: "/data/migration/backups", BackupKeyFile: "/run/backup-key",
		ReportPath: "/data/migration/reports/report.json", TechnicalListen: ":9090",
		StartupTimeout: 30 * time.Second, OperationTimeout: 30 * time.Minute, ShutdownTimeout: 10 * time.Second,
		TerminalScrapeHold: 20 * time.Second, MaximumStagingBytes: 1920 << 20,
	}
	if err := valid.validate(); err != nil {
		t.Fatalf("validate() error = %v", err)
	}
	tests := []struct {
		name string
		edit func(*Config)
	}{
		{name: "unknown mode", edit: func(value *Config) { value.Mode = "auto" }},
		{name: "relative DSN", edit: func(value *Config) { value.SourceDSNFile = "source-dsn" }},
		{name: "report outside storage", edit: func(value *Config) { value.ReportPath = "/other/reports/report.json" }},
		{name: "restore values in dry run", edit: func(value *Config) { value.RestoreDSNFile = "/run/restore-dsn" }},
		{name: "short terminal hold", edit: func(value *Config) { value.TerminalScrapeHold = 15 * time.Second }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := valid
			test.edit(&candidate)
			if err := candidate.validate(); err == nil {
				t.Fatal("validate() accepted an unsafe configuration")
			}
		})
	}
}

func TestCommitStateAllowed(t *testing.T) {
	t.Parallel()
	allowed := map[string]bool{"PREPARED": true, "FROZEN": true, "COMMITTED": true}
	states := []string{"PREPARED", "FROZEN", "COMMITTED", "ABORTED"}
	for _, sourceState := range states {
		if sourceCommitStateAllowed(sourceState) != allowed[sourceState] {
			t.Fatalf("sourceCommitStateAllowed(%q) returned an unsafe result", sourceState)
		}
	}
}
