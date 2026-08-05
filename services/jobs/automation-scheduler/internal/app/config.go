package app

import (
	"errors"
	"net"
	"path/filepath"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
)

const (
	serviceName      = "automation-scheduler"
	metricsSubsystem = "automation_scheduler"
)

type Config struct {
	Environment                 string        `env:"DEPLOYMENT_ENVIRONMENT"`
	TechnicalListen             string        `env:"AUTOMATION_SCHEDULER_TECHNICAL_LISTEN"`
	ControlPlaneTarget          string        `env:"AUTOMATION_SCHEDULER_CONTROL_PLANE_TARGET"`
	ControlPlaneTLSServerName   string        `env:"AUTOMATION_SCHEDULER_CONTROL_PLANE_TLS_SERVER_NAME"`
	ControlPlaneCAFile          string        `env:"AUTOMATION_SCHEDULER_CONTROL_PLANE_CA_FILE"`
	ControlPlaneCertificateFile string        `env:"AUTOMATION_SCHEDULER_CONTROL_PLANE_CERTIFICATE_FILE"`
	ControlPlanePrivateKeyFile  string        `env:"AUTOMATION_SCHEDULER_CONTROL_PLANE_PRIVATE_KEY_FILE"`
	ApplicationGrantFile        string        `env:"AUTOMATION_SCHEDULER_APPLICATION_GRANT_FILE"`
	StartupTimeout              time.Duration `env:"AUTOMATION_SCHEDULER_STARTUP_TIMEOUT"`
	ShutdownTimeout             time.Duration `env:"AUTOMATION_SCHEDULER_SHUTDOWN_TIMEOUT"`
	RPCDeadline                 time.Duration `env:"AUTOMATION_SCHEDULER_RPC_DEADLINE"`
	PollInterval                time.Duration `env:"AUTOMATION_SCHEDULER_POLL_INTERVAL"`
	ReadinessInterval           time.Duration `env:"AUTOMATION_SCHEDULER_READINESS_INTERVAL"`
	DueLimit                    int           `env:"AUTOMATION_SCHEDULER_DUE_LIMIT"`
	ClaimLimit                  int           `env:"AUTOMATION_SCHEDULER_CLAIM_LIMIT"`
	MaximumTrackedClaims        int           `env:"AUTOMATION_SCHEDULER_MAXIMUM_TRACKED_CLAIMS"`
}

func loadConfig() (Config, error) {
	config := Config{
		TechnicalListen:             ":9090",
		ControlPlaneTarget:          "control-plane.mattercodex-system.svc:8443",
		ControlPlaneTLSServerName:   "control-plane.mattercodex-system.svc.cluster.local",
		ControlPlaneCAFile:          "/var/run/config/mattercodex/automation-scheduler/control-plane/ca.pem",
		ControlPlaneCertificateFile: "/var/run/secrets/mattercodex/automation-scheduler/workload-tls/tls.crt",
		ControlPlanePrivateKeyFile:  "/var/run/secrets/mattercodex/automation-scheduler/workload-tls/tls.key",
		ApplicationGrantFile:        "/var/run/secrets/mattercodex/automation-scheduler/application-grant/application-grant.jws",
		StartupTimeout:              30 * time.Second,
		ShutdownTimeout:             20 * time.Second,
		RPCDeadline:                 3 * time.Second,
		PollInterval:                time.Second,
		ReadinessInterval:           10 * time.Second,
		DueLimit:                    32,
		ClaimLimit:                  32,
		MaximumTrackedClaims:        256,
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
	if config.Environment != "staging" && config.Environment != "production" {
		return errors.New("automation-scheduler environment is invalid")
	}
	for _, endpoint := range []string{config.TechnicalListen, config.ControlPlaneTarget} {
		if _, _, err := net.SplitHostPort(endpoint); err != nil {
			return errors.New("automation-scheduler endpoint is invalid")
		}
	}
	if strings.TrimSpace(config.ControlPlaneTLSServerName) == "" ||
		strings.ContainsAny(config.ControlPlaneTLSServerName, "*/") {
		return errors.New("automation-scheduler control-plane TLS server name is invalid")
	}
	for _, path := range []string{
		config.ControlPlaneCAFile,
		config.ControlPlaneCertificateFile,
		config.ControlPlanePrivateKeyFile,
		config.ApplicationGrantFile,
	} {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return errors.New("automation-scheduler credential path is invalid")
		}
	}
	if config.StartupTimeout < 5*time.Second || config.StartupTimeout > 2*time.Minute ||
		config.ShutdownTimeout < 5*time.Second || config.ShutdownTimeout > time.Minute ||
		config.RPCDeadline < 500*time.Millisecond || config.RPCDeadline > 10*time.Second ||
		config.PollInterval < 250*time.Millisecond || config.PollInterval > time.Minute ||
		config.ReadinessInterval < time.Second || config.ReadinessInterval > time.Minute ||
		config.DueLimit < 1 || config.DueLimit > 100 ||
		config.ClaimLimit < 1 || config.ClaimLimit > 100 ||
		config.MaximumTrackedClaims < config.ClaimLimit || config.MaximumTrackedClaims > 4096 {
		return errors.New("automation-scheduler lifecycle configuration is invalid")
	}
	return nil
}
