package app

import (
	"errors"
	"net"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
)

const (
	// serviceName identifies the controller in runtime configuration and diagnostics.
	// RU: serviceName определяет контроллер в runtime-настройках и диагностике.
	serviceName = "backup-controller"
	// metricsSubsystem names its bounded business metrics under the shared namespace.
	// RU: metricsSubsystem задаёт имя ограниченных бизнес-метрик в общем пространстве имён.
	metricsSubsystem = "backup_controller"
)

// Config holds command-specific paths, timing bounds, retention and resource limits.
// RU: Config содержит пути, временные границы, политику хранения и лимиты ресурсов для команды.
type Config struct {
	Environment               string        `env:"DEPLOYMENT_ENVIRONMENT"`
	TechnicalListen           string        `env:"BACKUP_CONTROLLER_TECHNICAL_LISTEN"`
	CredentialsFile           string        `env:"BACKUP_CONTROLLER_CREDENTIALS_FILE"`
	RepositoryCredentialsFile string        `env:"BACKUP_CONTROLLER_REPOSITORY_CREDENTIALS_FILE"`
	RestoreTargetsFile        string        `env:"BACKUP_CONTROLLER_RESTORE_TARGETS_FILE"`
	RestoreApprovalFile       string        `env:"BACKUP_CONTROLLER_RESTORE_APPROVAL_FILE"`
	WorkDirectory             string        `env:"BACKUP_CONTROLLER_WORK_DIRECTORY"`
	RepositoryPrefix          string        `env:"BACKUP_CONTROLLER_REPOSITORY_PREFIX"`
	ReleaseRevision           string        `env:"BACKUP_CONTROLLER_RELEASE_REVISION"`
	BackupID                  string        `env:"BACKUP_CONTROLLER_BACKUP_ID"`
	RestoreID                 string        `env:"BACKUP_CONTROLLER_RESTORE_ID"`
	ExpectedApprovalID        string        `env:"BACKUP_CONTROLLER_EXPECTED_APPROVAL_ID"`
	ExpectedTargetSHA256      string        `env:"BACKUP_CONTROLLER_EXPECTED_TARGET_SHA256"`
	BackupInterval            time.Duration `env:"BACKUP_CONTROLLER_BACKUP_INTERVAL"`
	BackupTimeout             time.Duration `env:"BACKUP_CONTROLLER_BACKUP_TIMEOUT"`
	StartupTimeout            time.Duration `env:"BACKUP_CONTROLLER_STARTUP_TIMEOUT"`
	ShutdownTimeout           time.Duration `env:"BACKUP_CONTROLLER_SHUTDOWN_TIMEOUT"`
	ReadinessInterval         time.Duration `env:"BACKUP_CONTROLLER_READINESS_INTERVAL"`
	RetentionMinimumAge       time.Duration `env:"BACKUP_CONTROLLER_RETENTION_MINIMUM_AGE"`
	RetentionKeep             int           `env:"BACKUP_CONTROLLER_RETENTION_KEEP"`
	MaximumDatabaseBytes      int64         `env:"BACKUP_CONTROLLER_MAXIMUM_DATABASE_BYTES"`
}

// loadConfig applies bounded defaults, parses environment values and validates the selected command.
// RU: loadConfig задаёт ограниченные значения по умолчанию, читает окружение и проверяет выбранную команду.
func loadConfig(command string) (Config, error) {
	config := Config{
		TechnicalListen: ":9090", CredentialsFile: "/var/run/secrets/kodex/backup-controller/credentials.json",
		RepositoryCredentialsFile: "/var/run/secrets/kodex/backup-controller/repository.json",
		RestoreTargetsFile:        "/var/run/secrets/kodex/backup-controller/restore/targets.json",
		RestoreApprovalFile:       "/var/run/secrets/kodex/backup-controller/restore/approval.json",
		WorkDirectory:             "/work", RepositoryPrefix: "kodex", BackupInterval: 24 * time.Hour,
		BackupTimeout: 6 * time.Hour, StartupTimeout: 45 * time.Second, ShutdownTimeout: 30 * time.Second,
		ReadinessInterval: 30 * time.Second, RetentionMinimumAge: 30 * 24 * time.Hour,
		RetentionKeep: 14, MaximumDatabaseBytes: 128 << 30,
	}
	if err := env.ParseWithOptions(&config, env.Options{}); err != nil {
		return Config{}, errors.New("parse backup-controller configuration")
	}
	if err := config.validate(command); err != nil {
		return Config{}, err
	}
	return config, nil
}

// validate rejects unknown commands, unsafe paths and out-of-range runtime or retention policy values.
// RU: validate отклоняет неизвестные команды, небезопасные пути и настройки runtime или retention вне границ.
func (config Config) validate(command string) error {
	switch command {
	case "serve", "backup", "verify", "retain", "restore-drill", "fingerprint-targets", "list-verified", "inspect-backup", "inspect-verification", "inspect-restore-drill", "preflight-restore", "inspect-restore-targets", "inspect-restore-cleanup", "cleanup-restore-drill":
	default:
		return errors.New("backup-controller command is invalid")
	}
	if config.Environment != "staging" && config.Environment != "production" {
		return errors.New("backup-controller environment is invalid")
	}
	if _, _, err := net.SplitHostPort(config.TechnicalListen); err != nil {
		return errors.New("backup-controller technical endpoint is invalid")
	}
	for _, value := range []string{config.CredentialsFile, config.RepositoryCredentialsFile,
		config.RestoreTargetsFile, config.RestoreApprovalFile, config.WorkDirectory} {
		if !filepath.IsAbs(value) || filepath.Clean(value) != value {
			return errors.New("backup-controller file path is invalid")
		}
	}
	if config.RepositoryPrefix == "" ||
		(command != "fingerprint-targets" && (!regexp.MustCompile(`^sha256:[a-f0-9]{64}$`).MatchString(config.ReleaseRevision) || config.ReleaseRevision == "sha256:"+strings.Repeat("0", 64))) ||
		config.BackupInterval < time.Hour || config.BackupInterval > 7*24*time.Hour ||
		config.BackupTimeout < 10*time.Minute || config.BackupTimeout > 24*time.Hour ||
		config.StartupTimeout < 5*time.Second || config.StartupTimeout > 5*time.Minute ||
		config.ShutdownTimeout < 5*time.Second || config.ShutdownTimeout > 5*time.Minute ||
		config.ReadinessInterval < 5*time.Second || config.ReadinessInterval > 5*time.Minute ||
		config.RetentionMinimumAge < 24*time.Hour || config.RetentionMinimumAge > 3650*24*time.Hour ||
		config.RetentionKeep < 1 || config.RetentionKeep > 1000 || config.MaximumDatabaseBytes < 1<<20 {
		return errors.New("backup-controller policy configuration is invalid")
	}
	if command == "verify" && config.BackupID == "" {
		return errors.New("backup-controller backup ID is required")
	}
	if command == "inspect-backup" || command == "inspect-verification" || command == "inspect-restore-drill" {
		if !regexp.MustCompile(`^[0-9]{8}T[0-9]{6}Z-[a-f0-9]{16}$`).MatchString(config.BackupID) {
			return errors.New("backup metadata locator is invalid")
		}
	}
	if command == "inspect-restore-drill" && (!regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`).MatchString(config.RestoreID) ||
		!regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`).MatchString(config.ExpectedApprovalID) ||
		!regexp.MustCompile(`^sha256:[a-f0-9]{64}$`).MatchString(config.ExpectedTargetSHA256)) {
		return errors.New("restore metadata owner binding is invalid")
	}
	return nil
}
