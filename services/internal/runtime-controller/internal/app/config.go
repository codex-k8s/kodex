package app

import (
	"errors"
	"net"
	"net/url"
	"path/filepath"
	"time"

	"github.com/caarlos0/env/v11"
)

const (
	controlPlaneTarget        = "dns:///control-plane.mattercodex-system.svc:8443"
	controlPlaneTLSServerName = "control-plane.mattercodex-system.svc.cluster.local"
	providerProxy             = "http://egress-gateway.mattercodex-system.svc.cluster.local:8080"
)

type Config struct {
	TechnicalListen                   string        `env:"RUNTIME_CONTROLLER_TECHNICAL_LISTEN"`
	WorkloadInstance                  string        `env:"RUNTIME_CONTROLLER_WORKLOAD_INSTANCE"`
	ControlPlaneTarget                string        `env:"RUNTIME_CONTROLLER_CONTROL_PLANE_TARGET"`
	ControlPlaneTLSServerName         string        `env:"RUNTIME_CONTROLLER_CONTROL_PLANE_TLS_SERVER_NAME"`
	ControlPlaneCAFile                string        `env:"RUNTIME_CONTROLLER_CONTROL_PLANE_CA_FILE"`
	ControlPlaneClientCertificateFile string        `env:"RUNTIME_CONTROLLER_CONTROL_PLANE_CLIENT_CERTIFICATE_FILE"`
	ControlPlaneClientPrivateKeyFile  string        `env:"RUNTIME_CONTROLLER_CONTROL_PLANE_CLIENT_PRIVATE_KEY_FILE"`
	ControlPlaneApplicationGrantFile  string        `env:"RUNTIME_CONTROLLER_CONTROL_PLANE_APPLICATION_GRANT_FILE"`
	ProviderProxy                     string        `env:"RUNTIME_CONTROLLER_PROVIDER_PROXY"`
	ProviderCredentialFile            string        `env:"RUNTIME_CONTROLLER_PROVIDER_CREDENTIAL_FILE"`
	PollInterval                      time.Duration `env:"RUNTIME_CONTROLLER_POLL_INTERVAL"`
	HeartbeatInterval                 time.Duration `env:"RUNTIME_CONTROLLER_HEARTBEAT_INTERVAL"`
	LeaseRenewInterval                time.Duration `env:"RUNTIME_CONTROLLER_LEASE_RENEW_INTERVAL"`
	RequestTimeout                    time.Duration `env:"RUNTIME_CONTROLLER_REQUEST_TIMEOUT"`
	ExecutionTimeout                  time.Duration `env:"RUNTIME_CONTROLLER_EXECUTION_TIMEOUT"`
	ShutdownTimeout                   time.Duration `env:"RUNTIME_CONTROLLER_SHUTDOWN_TIMEOUT"`
}

func loadConfig() (Config, error) {
	config := Config{
		TechnicalListen: ":9090", WorkloadInstance: "runtime-controller-warm-0",
		ControlPlaneTarget: controlPlaneTarget, ControlPlaneTLSServerName: controlPlaneTLSServerName,
		ControlPlaneCAFile: "/var/run/config/mattercodex/runtime-controller/control-plane/ca.pem", ControlPlaneClientCertificateFile: "/var/run/secrets/mattercodex/runtime-controller/control-plane-client/tls.crt", ControlPlaneClientPrivateKeyFile: "/var/run/secrets/mattercodex/runtime-controller/control-plane-client/tls.key", ControlPlaneApplicationGrantFile: "/var/run/secrets/mattercodex/runtime-controller/application-grant/application-grant.jws",
		ProviderProxy: providerProxy, ProviderCredentialFile: "/var/run/secrets/mattercodex/runtime-controller/provider/api-key",
		PollInterval: 500 * time.Millisecond, HeartbeatInterval: 15 * time.Second, LeaseRenewInterval: 10 * time.Second, RequestTimeout: 5 * time.Second, ExecutionTimeout: 20 * time.Minute, ShutdownTimeout: 30 * time.Second,
	}
	if err := env.Parse(&config); err != nil {
		return Config{}, err
	}
	return config, config.validate()
}

func (config Config) validate() error {
	if _, _, err := net.SplitHostPort(config.TechnicalListen); err != nil {
		return errors.New("runtime controller technical listen address is invalid")
	}
	if config.WorkloadInstance == "" || len(config.WorkloadInstance) > 128 || config.ControlPlaneTarget != controlPlaneTarget || config.ControlPlaneTLSServerName != controlPlaneTLSServerName {
		return errors.New("runtime controller internal identity is invalid")
	}
	proxy, err := url.Parse(config.ProviderProxy)
	if err != nil || proxy.String() != providerProxy {
		return errors.New("runtime controller provider proxy is invalid")
	}
	for _, fileName := range []string{config.ControlPlaneCAFile, config.ControlPlaneClientCertificateFile, config.ControlPlaneClientPrivateKeyFile, config.ControlPlaneApplicationGrantFile, config.ProviderCredentialFile} {
		if !filepath.IsAbs(fileName) {
			return errors.New("runtime controller file path is invalid")
		}
	}
	if config.PollInterval < 100*time.Millisecond || config.PollInterval > 10*time.Second || config.HeartbeatInterval < 5*time.Second || config.HeartbeatInterval > 30*time.Second || config.LeaseRenewInterval < time.Second || config.LeaseRenewInterval >= config.HeartbeatInterval || config.RequestTimeout < time.Second || config.RequestTimeout > 10*time.Second || config.ExecutionTimeout < time.Minute || config.ExecutionTimeout > time.Hour || config.ShutdownTimeout < 5*time.Second {
		return errors.New("runtime controller bounded configuration is invalid")
	}
	return nil
}
