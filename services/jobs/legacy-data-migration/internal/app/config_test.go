package app

import (
	"testing"
	"time"
)

func TestConfigValidateFailClosed(t *testing.T) {
	t.Parallel()
	valid := Config{
		Mode: "dry-run", PlanID: "issue-196-plan-0001",
		SourceDSNFile: "/run/source-dsn", TargetDSNFile: "/run/target-dsn",
		SourceTLSServerName: "source.database.svc", SourceCAFile: "/run/source-ca.pem",
		TargetTLSServerName: "target.database.svc", TargetCAFile: "/run/target-ca.pem",
		BackupDirectory: "/data/migration/backups", BackupKeyFile: "/run/backup-key",
		ReportPath: "/data/migration/reports/report.json", TechnicalListen: ":9090",
		StartupTimeout: 30 * time.Second, OperationTimeout: 30 * time.Minute, ShutdownTimeout: 10 * time.Second,
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
	allowed := map[[2]string]bool{
		{"PREPARED", "PREPARED"}:   true,
		{"FROZEN", "PREPARED"}:     true,
		{"FROZEN", "COMMITTED"}:    true,
		{"COMMITTED", "COMMITTED"}: true,
	}
	states := []string{"PREPARED", "FROZEN", "COMMITTED", "ABORTED"}
	for _, sourceState := range states {
		for _, targetState := range states {
			if commitStateAllowed(sourceState, targetState) != allowed[[2]string{sourceState, targetState}] {
				t.Fatalf("commitStateAllowed(%q, %q) returned an unsafe result", sourceState, targetState)
			}
		}
	}
}
