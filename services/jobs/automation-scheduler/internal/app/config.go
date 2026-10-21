package app

import (
	"errors"
	"net"
	"path/filepath"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/transportprofile"
)

const (
	// serviceName selects the protected RPC workload identity and telemetry service label.
	// RU: serviceName задаёт identity защищённого RPC workload и метку сервиса в телеметрии.
	serviceName = "automation-scheduler"
	// metricsSubsystem names the service-specific metrics namespace in shared telemetry.
	// RU: metricsSubsystem именует подсистему сервисных метрик в общей телеметрии.
	metricsSubsystem = "automation_scheduler"
)

// Config contains the scheduler's validated endpoints, credential paths and bounded lifecycle
// budgets; it carries no permission to mutate an occurrence without a fresh owner claim.
// RU: Config содержит проверенные адреса, пути реквизитов и ограниченные бюджеты жизненного цикла;
// сама по себе она не разрешает изменение occurrence без свежего claim владельца.
type Config struct {
	RPCProfile                  string        `env:"KODEX_RPC_PROFILE"`
	Environment                 string        `env:"DEPLOYMENT_ENVIRONMENT"`
	TechnicalListen             string        `env:"AUTOMATION_SCHEDULER_TECHNICAL_LISTEN"`
	ControlPlaneTarget          string        `env:"AUTOMATION_SCHEDULER_CONTROL_PLANE_TARGET"`
	ControlPlaneTLSServerName   string        `env:"AUTOMATION_SCHEDULER_CONTROL_PLANE_TLS_SERVER_NAME"`
	ControlPlaneCAFile          string        `env:"AUTOMATION_SCHEDULER_CONTROL_PLANE_CA_FILE"`
	ControlPlaneCertificateFile string        `env:"AUTOMATION_SCHEDULER_CONTROL_PLANE_CERTIFICATE_FILE"`
	ControlPlanePrivateKeyFile  string        `env:"AUTOMATION_SCHEDULER_CONTROL_PLANE_PRIVATE_KEY_FILE"`
	ApplicationGrantFile        string        `env:"AUTOMATION_SCHEDULER_APPLICATION_GRANT_FILE"`
	InstanceID                  string        `env:"AUTOMATION_SCHEDULER_INSTANCE_ID"`
	StartupTimeout              time.Duration `env:"AUTOMATION_SCHEDULER_STARTUP_TIMEOUT"`
	ShutdownTimeout             time.Duration `env:"AUTOMATION_SCHEDULER_SHUTDOWN_TIMEOUT"`
	RPCDeadline                 time.Duration `env:"AUTOMATION_SCHEDULER_RPC_DEADLINE"`
	PollInterval                time.Duration `env:"AUTOMATION_SCHEDULER_POLL_INTERVAL"`
	ReadinessInterval           time.Duration `env:"AUTOMATION_SCHEDULER_READINESS_INTERVAL"`
	DueLimit                    int           `env:"AUTOMATION_SCHEDULER_DUE_LIMIT"`
}

// loadConfig applies pinned defaults, parses the scheduler environment once and rejects an
// unusable configuration before listeners or polling workers are started.
// RU: loadConfig применяет закреплённые значения, один раз читает окружение планировщика и
// отклоняет недопустимую конфигурацию до запуска listeners и работников опроса.
func loadConfig() (Config, error) {
	config := Config{
		TechnicalListen:             ":9090",
		ControlPlaneTarget:          "control-plane.kodex-system.svc:8443",
		ControlPlaneTLSServerName:   "control-plane.kodex-system.svc.cluster.local",
		ControlPlaneCAFile:          "/var/run/config/kodex/automation-scheduler/control-plane/ca.pem",
		ControlPlaneCertificateFile: "/var/run/secrets/kodex/automation-scheduler/workload-tls/tls.crt",
		ControlPlanePrivateKeyFile:  "/var/run/secrets/kodex/automation-scheduler/workload-tls/tls.key",
		ApplicationGrantFile:        "/var/run/secrets/kodex/automation-scheduler/application-grant/application-grant.jws",
		InstanceID:                  "automation-scheduler-0",
		StartupTimeout:              30 * time.Second,
		ShutdownTimeout:             20 * time.Second,
		RPCDeadline:                 3 * time.Second,
		PollInterval:                time.Second,
		ReadinessInterval:           10 * time.Second,
		DueLimit:                    32,
	}
	if err := env.ParseWithOptions(&config, env.Options{}); err != nil {
		return Config{}, err
	}
	if err := config.validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}

// validate permits only the empty or trusted-cluster RPC profile and enforces TLS hostname shape,
// absolute clean credential paths and bounded timing, batch and instance settings.
// RU: validate допускает только пустой профиль RPC или профиль доверенного кластера и проверяет
// форму TLS-имени, абсолютные нормализованные пути и границы сроков, партии и реплики.
func (config Config) validate() error {
	if config.RPCProfile != "" && config.RPCProfile != transportprofile.TrustedCluster {
		return errors.New("automation-scheduler RPC profile is invalid")
	}
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
		config.DueLimit < 1 || config.DueLimit > 100 || config.InstanceID == "" || len(config.InstanceID) > 128 {
		return errors.New("automation-scheduler lifecycle configuration is invalid")
	}
	return nil
}
