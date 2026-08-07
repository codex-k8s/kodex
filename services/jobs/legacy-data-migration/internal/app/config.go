package app

import (
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
)

var planIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{15,127}$`)

type Config struct {
	Mode                 string        `env:"LEGACY_DATA_MIGRATION_MODE,required"`
	PlanID               string        `env:"LEGACY_DATA_MIGRATION_PLAN_ID,required"`
	SourceDSNFile        string        `env:"LEGACY_DATA_MIGRATION_SOURCE_DSN_FILE,required"`
	TargetDSNFile        string        `env:"LEGACY_DATA_MIGRATION_TARGET_DSN_FILE,required"`
	SourceTLSServerName  string        `env:"LEGACY_DATA_MIGRATION_SOURCE_TLS_SERVER_NAME,required"`
	SourceCAFile         string        `env:"LEGACY_DATA_MIGRATION_SOURCE_CA_FILE,required"`
	TargetTLSServerName  string        `env:"LEGACY_DATA_MIGRATION_TARGET_TLS_SERVER_NAME,required"`
	TargetCAFile         string        `env:"LEGACY_DATA_MIGRATION_TARGET_CA_FILE,required"`
	RestoreDSNFile       string        `env:"LEGACY_DATA_MIGRATION_RESTORE_DSN_FILE"`
	RestoreTLSServerName string        `env:"LEGACY_DATA_MIGRATION_RESTORE_TLS_SERVER_NAME"`
	RestoreCAFile        string        `env:"LEGACY_DATA_MIGRATION_RESTORE_CA_FILE"`
	BackupDirectory      string        `env:"LEGACY_DATA_MIGRATION_BACKUP_DIRECTORY,required"`
	BackupKeyFile        string        `env:"LEGACY_DATA_MIGRATION_BACKUP_KEY_FILE,required"`
	ReportPath           string        `env:"LEGACY_DATA_MIGRATION_REPORT_PATH,required"`
	TechnicalListen      string        `env:"LEGACY_DATA_MIGRATION_TECHNICAL_LISTEN"`
	StartupTimeout       time.Duration `env:"LEGACY_DATA_MIGRATION_STARTUP_TIMEOUT"`
	OperationTimeout     time.Duration `env:"LEGACY_DATA_MIGRATION_OPERATION_TIMEOUT"`
	ShutdownTimeout      time.Duration `env:"LEGACY_DATA_MIGRATION_SHUTDOWN_TIMEOUT"`
	TerminalScrapeHold   time.Duration `env:"LEGACY_DATA_MIGRATION_TERMINAL_SCRAPE_HOLD"`
}

func loadConfig() (Config, error) {
	config := Config{
		TechnicalListen: ":9090", StartupTimeout: 30 * time.Second,
		OperationTimeout: 30 * time.Minute, ShutdownTimeout: 10 * time.Second,
		TerminalScrapeHold: 20 * time.Second,
	}
	if err := env.ParseWithOptions(&config, env.Options{}); err != nil {
		return Config{}, err
	}
	if err := config.validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}

func (config Config) validate() error {
	switch config.Mode {
	case "dry-run", "pre-commit", "commit", "rollback", "restore-verify":
	default:
		return errors.New("legacy migration mode is invalid")
	}
	if !planIDPattern.MatchString(config.PlanID) {
		return errors.New("legacy migration plan identifier is invalid")
	}
	if _, _, err := net.SplitHostPort(config.TechnicalListen); err != nil {
		return errors.New("legacy migration technical endpoint is invalid")
	}
	for _, path := range []string{config.SourceDSNFile, config.TargetDSNFile, config.SourceCAFile,
		config.TargetCAFile, config.BackupDirectory, config.BackupKeyFile, config.ReportPath} {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return errors.New("legacy migration path is invalid")
		}
	}
	if filepath.Base(config.BackupDirectory) != "backups" || filepath.Base(filepath.Dir(config.ReportPath)) != "reports" ||
		filepath.Dir(config.BackupDirectory) != filepath.Dir(filepath.Dir(config.ReportPath)) ||
		filepath.Ext(config.ReportPath) != ".json" {
		return errors.New("legacy migration storage boundary is invalid")
	}
	if config.Mode == "restore-verify" {
		for _, path := range []string{config.RestoreDSNFile, config.RestoreCAFile} {
			if !filepath.IsAbs(path) || filepath.Clean(path) != path {
				return errors.New("legacy migration restore path is invalid")
			}
		}
		if strings.TrimSpace(config.RestoreTLSServerName) == "" {
			return errors.New("legacy migration restore TLS server name is invalid")
		}
	} else if config.RestoreDSNFile != "" || config.RestoreTLSServerName != "" || config.RestoreCAFile != "" {
		return errors.New("legacy migration restore configuration is unexpected")
	}
	for _, serverName := range []string{config.SourceTLSServerName, config.TargetTLSServerName} {
		if strings.TrimSpace(serverName) == "" || strings.ContainsAny(serverName, "*/") {
			return errors.New("legacy migration TLS server name is invalid")
		}
	}
	if config.StartupTimeout < 5*time.Second || config.StartupTimeout > 2*time.Minute ||
		config.OperationTimeout < time.Minute || config.OperationTimeout > 2*time.Hour ||
		config.ShutdownTimeout < time.Second || config.ShutdownTimeout > time.Minute ||
		config.TerminalScrapeHold < 16*time.Second || config.TerminalScrapeHold > time.Minute {
		return errors.New("legacy migration lifecycle timeout is invalid")
	}
	return nil
}

func readDSN(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", errors.New("read database configuration")
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, 8193))
	if err != nil {
		return "", errors.New("read database configuration")
	}
	value := strings.TrimSpace(string(raw))
	if value == "" || len(value) > 8192 {
		return "", errors.New("database configuration is invalid")
	}
	return value, nil
}
