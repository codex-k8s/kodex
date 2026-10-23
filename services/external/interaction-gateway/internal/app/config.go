package app

import (
	"errors"
	"net"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/transportprofile"
)

const (
	// serviceName identifies the workload to telemetry and the protected control-plane operation profile.
	// RU: serviceName определяет workload для telemetry и защищённого профиля операций control-plane.
	serviceName = "interaction-gateway"
	// metricsSubsystem names this process's technical metrics without using external connection identifiers as labels.
	// RU: metricsSubsystem задаёт имя технических метрик процесса без внешних идентификаторов подключений в метках.
	metricsSubsystem = "interaction_gateway"
)

// Config carries RPC credential paths, the exact egress proxy and bounded polling, claim and process lifecycle settings.
// RU: Config передаёт пути реквизитов RPC, точный egress proxy и ограниченные настройки опроса, claim и жизненного цикла процесса.
// Secret contents are not loaded into this configuration; the adapter resolves connection credentials separately.
// RU: Содержимое секретов в эти настройки не загружается; адаптер отдельно разрешает реквизиты подключений.
type Config struct {
	RPCProfile                  string        `env:"KODEX_RPC_PROFILE"`
	Environment                 string        `env:"DEPLOYMENT_ENVIRONMENT"`
	TechnicalListen             string        `env:"INTERACTION_GATEWAY_TECHNICAL_LISTEN"`
	ControlPlaneTarget          string        `env:"INTERACTION_GATEWAY_CONTROL_PLANE_TARGET"`
	ControlPlaneTLSServerName   string        `env:"INTERACTION_GATEWAY_CONTROL_PLANE_TLS_SERVER_NAME"`
	ControlPlaneCAFile          string        `env:"INTERACTION_GATEWAY_CONTROL_PLANE_CA_FILE"`
	ControlPlaneCertificateFile string        `env:"INTERACTION_GATEWAY_CONTROL_PLANE_CERTIFICATE_FILE"`
	ControlPlanePrivateKeyFile  string        `env:"INTERACTION_GATEWAY_CONTROL_PLANE_PRIVATE_KEY_FILE"`
	ApplicationGrantFile        string        `env:"INTERACTION_GATEWAY_APPLICATION_GRANT_FILE"`
	InstanceID                  string        `env:"INTERACTION_GATEWAY_INSTANCE_ID"`
	CredentialDirectory         string        `env:"INTERACTION_GATEWAY_CREDENTIAL_DIRECTORY"`
	EgressProxyURL              string        `env:"INTERACTION_GATEWAY_EGRESS_PROXY_URL"`
	AllowedHosts                string        `env:"INTERACTION_GATEWAY_ALLOWED_HOSTS"`
	StartupTimeout              time.Duration `env:"INTERACTION_GATEWAY_STARTUP_TIMEOUT"`
	ShutdownTimeout             time.Duration `env:"INTERACTION_GATEWAY_SHUTDOWN_TIMEOUT"`
	RequestTimeout              time.Duration `env:"INTERACTION_GATEWAY_REQUEST_TIMEOUT"`
	OperationTimeout            time.Duration `env:"INTERACTION_GATEWAY_OPERATION_TIMEOUT"`
	PollInterval                time.Duration `env:"INTERACTION_GATEWAY_POLL_INTERVAL"`
	SourceRefreshInterval       time.Duration `env:"INTERACTION_GATEWAY_SOURCE_REFRESH_INTERVAL"`
	ReadinessInterval           time.Duration `env:"INTERACTION_GATEWAY_READINESS_INTERVAL"`
	ClaimLimit                  int32         `env:"INTERACTION_GATEWAY_CLAIM_LIMIT"`
}

// loadConfig overlays environment values on deployment defaults and rejects invalid configuration before constructing dependencies.
// RU: loadConfig накладывает переменные окружения на настройки развёртывания по умолчанию и отклоняет неверную конфигурацию до создания зависимостей.
func loadConfig() (Config, error) {
	config := Config{
		TechnicalListen: ":9090", ControlPlaneTarget: "control-plane.kodex-system.svc:8443",
		ControlPlaneTLSServerName:   "control-plane.kodex-system.svc.cluster.local",
		ControlPlaneCAFile:          "/var/run/config/kodex/interaction-gateway/control-plane/ca.pem",
		ControlPlaneCertificateFile: "/var/run/secrets/kodex/interaction-gateway/workload-tls/tls.crt",
		ControlPlanePrivateKeyFile:  "/var/run/secrets/kodex/interaction-gateway/workload-tls/tls.key",
		ApplicationGrantFile:        "/var/run/secrets/kodex/interaction-gateway/application-grant/application-grant.jws",
		CredentialDirectory:         "/var/run/secrets/kodex/integration-connections",
		EgressProxyURL:              "http://egress-gateway.kodex-system.svc.cluster.local:8080",
		StartupTimeout:              30 * time.Second, ShutdownTimeout: 20 * time.Second,
		RequestTimeout: 3 * time.Second, OperationTimeout: 30 * time.Second,
		PollInterval: 500 * time.Millisecond, SourceRefreshInterval: 10 * time.Second,
		ReadinessInterval: 10 * time.Second, ClaimLimit: 8,
	}
	if err := env.Parse(&config); err != nil {
		return Config{}, errors.New("parse interaction-gateway environment")
	}
	if err := config.validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}

// validate checks the RPC profile, supported environment, exact proxy, canonical paths and lifecycle bounds without opening files or sockets.
// RU: validate проверяет профиль RPC, поддерживаемую среду, точный proxy, канонические пути и пределы жизненного цикла без открытия файлов или сокетов.
// Syntactic TLS-name validation here does not replace the RPC client's certificate and application-grant verification.
// RU: Синтаксическая проверка TLS-имени здесь не заменяет проверку сертификата и application grant в RPC клиенте.
func (config Config) validate() error {
	if config.RPCProfile != "" && config.RPCProfile != transportprofile.TrustedCluster {
		return errors.New("interaction-gateway RPC profile is invalid")
	}
	if config.Environment != "staging" && config.Environment != "production" {
		return errors.New("interaction-gateway environment is invalid")
	}
	for _, endpoint := range []string{config.TechnicalListen, config.ControlPlaneTarget} {
		if _, _, err := net.SplitHostPort(endpoint); err != nil {
			return errors.New("interaction-gateway endpoint is invalid")
		}
	}
	proxy, err := url.Parse(config.EgressProxyURL)
	if err != nil || proxy.Scheme != "http" || proxy.Host != "egress-gateway.kodex-system.svc.cluster.local:8080" || proxy.Path != "" || proxy.User != nil || proxy.RawQuery != "" {
		return errors.New("interaction-gateway egress proxy is invalid")
	}
	if strings.TrimSpace(config.ControlPlaneTLSServerName) == "" || strings.ContainsAny(config.ControlPlaneTLSServerName, "*/") {
		return errors.New("interaction-gateway control-plane TLS name is invalid")
	}
	for _, path := range []string{config.ControlPlaneCAFile, config.ControlPlaneCertificateFile, config.ControlPlanePrivateKeyFile, config.ApplicationGrantFile, config.CredentialDirectory} {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return errors.New("interaction-gateway runtime path is invalid")
		}
	}
	if config.InstanceID == "" || len(config.InstanceID) > 128 ||
		config.StartupTimeout < 5*time.Second || config.StartupTimeout > 2*time.Minute ||
		config.ShutdownTimeout < 5*time.Second || config.ShutdownTimeout > time.Minute ||
		config.RequestTimeout < 500*time.Millisecond || config.RequestTimeout > 10*time.Second ||
		config.OperationTimeout < time.Second || config.OperationTimeout > 2*time.Minute ||
		config.PollInterval < 250*time.Millisecond || config.PollInterval > time.Minute ||
		config.SourceRefreshInterval < time.Second || config.SourceRefreshInterval > time.Minute ||
		config.ReadinessInterval < time.Second || config.ReadinessInterval > time.Minute ||
		config.ClaimLimit < 1 || config.ClaimLimit > 32 {
		return errors.New("interaction-gateway lifecycle configuration is invalid")
	}
	return nil
}
