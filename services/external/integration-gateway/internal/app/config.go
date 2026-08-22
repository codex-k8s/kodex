package app

import (
	"errors"
	"net"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
)

const (
	serviceName      = "integration-gateway"
	metricsSubsystem = "integration_gateway"
)

type Config struct {
	Environment                 string        `env:"DEPLOYMENT_ENVIRONMENT"`
	TechnicalListen             string        `env:"INTEGRATION_GATEWAY_TECHNICAL_LISTEN"`
	ControlPlaneTarget          string        `env:"INTEGRATION_GATEWAY_CONTROL_PLANE_TARGET"`
	ControlPlaneTLSServerName   string        `env:"INTEGRATION_GATEWAY_CONTROL_PLANE_TLS_SERVER_NAME"`
	ControlPlaneCAFile          string        `env:"INTEGRATION_GATEWAY_CONTROL_PLANE_CA_FILE"`
	ControlPlaneCertificateFile string        `env:"INTEGRATION_GATEWAY_CONTROL_PLANE_CERTIFICATE_FILE"`
	ControlPlanePrivateKeyFile  string        `env:"INTEGRATION_GATEWAY_CONTROL_PLANE_PRIVATE_KEY_FILE"`
	ApplicationGrantFile        string        `env:"INTEGRATION_GATEWAY_APPLICATION_GRANT_FILE"`
	InstanceID                  string        `env:"INTEGRATION_GATEWAY_INSTANCE_ID"`
	CredentialDirectory         string        `env:"INTEGRATION_GATEWAY_CREDENTIAL_DIRECTORY"`
	EgressProxyURL              string        `env:"INTEGRATION_GATEWAY_EGRESS_PROXY_URL"`
	AllowedIntegrationHosts     string        `env:"INTEGRATION_GATEWAY_ALLOWED_HOSTS"`
	StartupTimeout              time.Duration `env:"INTEGRATION_GATEWAY_STARTUP_TIMEOUT"`
	ShutdownTimeout             time.Duration `env:"INTEGRATION_GATEWAY_SHUTDOWN_TIMEOUT"`
	RequestTimeout              time.Duration `env:"INTEGRATION_GATEWAY_REQUEST_TIMEOUT"`
	OperationTimeout            time.Duration `env:"INTEGRATION_GATEWAY_OPERATION_TIMEOUT"`
	PollInterval                time.Duration `env:"INTEGRATION_GATEWAY_POLL_INTERVAL"`
	ReadinessInterval           time.Duration `env:"INTEGRATION_GATEWAY_READINESS_INTERVAL"`
	ClaimLimit                  int32         `env:"INTEGRATION_GATEWAY_CLAIM_LIMIT"`
}

func loadConfig() (Config, error) {
	config := Config{
		TechnicalListen: ":9090", ControlPlaneTarget: "control-plane.mattercodex-system.svc:8443",
		ControlPlaneTLSServerName:   "control-plane.mattercodex-system.svc.cluster.local",
		ControlPlaneCAFile:          "/var/run/config/mattercodex/integration-gateway/control-plane/ca.pem",
		ControlPlaneCertificateFile: "/var/run/secrets/mattercodex/integration-gateway/workload-tls/tls.crt",
		ControlPlanePrivateKeyFile:  "/var/run/secrets/mattercodex/integration-gateway/workload-tls/tls.key",
		ApplicationGrantFile:        "/var/run/secrets/mattercodex/integration-gateway/application-grant/application-grant.jws",
		InstanceID:                  "integration-gateway-0", CredentialDirectory: "/var/run/secrets/mattercodex/integration-connections",
		EgressProxyURL: "http://egress-gateway.mattercodex-system.svc.cluster.local:8080",
		StartupTimeout: 30 * time.Second, ShutdownTimeout: 20 * time.Second, RequestTimeout: 3 * time.Second,
		OperationTimeout: 30 * time.Second, PollInterval: 500 * time.Millisecond, ReadinessInterval: 10 * time.Second, ClaimLimit: 8,
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
		return errors.New("integration-gateway environment is invalid")
	}
	for _, endpoint := range []string{config.TechnicalListen, config.ControlPlaneTarget} {
		if _, _, err := net.SplitHostPort(endpoint); err != nil {
			return errors.New("integration-gateway endpoint is invalid")
		}
	}
	proxy, err := url.Parse(config.EgressProxyURL)
	if err != nil || proxy.Scheme != "http" || proxy.Host != "egress-gateway.mattercodex-system.svc.cluster.local:8080" || proxy.Path != "" || proxy.User != nil || proxy.RawQuery != "" {
		return errors.New("integration-gateway egress proxy is invalid")
	}
	if strings.TrimSpace(config.ControlPlaneTLSServerName) == "" || strings.ContainsAny(config.ControlPlaneTLSServerName, "*/") {
		return errors.New("integration-gateway control-plane TLS name is invalid")
	}
	for _, path := range []string{config.ControlPlaneCAFile, config.ControlPlaneCertificateFile, config.ControlPlanePrivateKeyFile, config.ApplicationGrantFile, config.CredentialDirectory} {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return errors.New("integration-gateway runtime path is invalid")
		}
	}
	if config.InstanceID == "" || len(config.InstanceID) > 128 || config.StartupTimeout < 5*time.Second || config.StartupTimeout > 2*time.Minute || config.ShutdownTimeout < 5*time.Second || config.ShutdownTimeout > time.Minute || config.RequestTimeout < 500*time.Millisecond || config.RequestTimeout > 10*time.Second || config.OperationTimeout < time.Second || config.OperationTimeout > 2*time.Minute || config.PollInterval < 250*time.Millisecond || config.PollInterval > time.Minute || config.ReadinessInterval < time.Second || config.ReadinessInterval > time.Minute || config.ClaimLimit < 1 || config.ClaimLimit > 32 {
		return errors.New("integration-gateway lifecycle configuration is invalid")
	}
	return nil
}
