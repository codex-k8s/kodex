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
	serviceName               = "control-api-gateway"
	controlPlaneTarget        = "dns:///control-plane.mattercodex-system.svc:8443"
	controlPlaneTLSServerName = "control-plane.mattercodex-system.svc.cluster.local"
	interactionTarget         = "dns:///interaction-gateway.mattercodex-system.svc:9443"
	interactionTLSServerName  = "interaction-gateway.mattercodex-system.svc.cluster.local"
	integrationTarget         = "dns:///integration-gateway.mattercodex-system.svc:9443"
	integrationTLSServerName  = "integration-gateway.mattercodex-system.svc.cluster.local"
)

type Config struct {
	HTTPListen                        string        `env:"CONTROL_API_GATEWAY_HTTP_LISTEN"`
	TechnicalListen                   string        `env:"CONTROL_API_GATEWAY_TECHNICAL_LISTEN"`
	TLSCertificateFile                string        `env:"CONTROL_API_GATEWAY_TLS_CERTIFICATE_FILE"`
	TLSPrivateKeyFile                 string        `env:"CONTROL_API_GATEWAY_TLS_PRIVATE_KEY_FILE"`
	PublicTLSCAFile                   string        `env:"CONTROL_API_GATEWAY_PUBLIC_TLS_CA_FILE"`
	PublicTLSMaterialFile             string        `env:"CONTROL_API_GATEWAY_PUBLIC_TLS_MATERIAL_FILE"`
	PublicTLSServerName               string        `env:"CONTROL_API_GATEWAY_PUBLIC_TLS_SERVER_NAME"`
	OIDCIssuer                        string        `env:"CONTROL_API_GATEWAY_OIDC_ISSUER"`
	OIDCAudience                      string        `env:"CONTROL_API_GATEWAY_OIDC_AUDIENCE"`
	OIDCConnectAddress                string        `env:"CONTROL_API_GATEWAY_OIDC_CONNECT_ADDRESS"`
	OIDCTLSServerName                 string        `env:"CONTROL_API_GATEWAY_OIDC_TLS_SERVER_NAME"`
	OIDCCAFile                        string        `env:"CONTROL_API_GATEWAY_OIDC_CA_FILE"`
	AllowedOrigins                    string        `env:"CONTROL_API_GATEWAY_ALLOWED_ORIGINS"`
	SessionCurrentKeyFile             string        `env:"CONTROL_API_GATEWAY_SESSION_CURRENT_KEY_FILE"`
	SessionPreviousKeyFile            string        `env:"CONTROL_API_GATEWAY_SESSION_PREVIOUS_KEY_FILE"`
	SessionTTL                        time.Duration `env:"CONTROL_API_GATEWAY_SESSION_TTL"`
	ControlPlaneTarget                string        `env:"CONTROL_API_GATEWAY_CONTROL_PLANE_TARGET"`
	ControlPlaneTLSServerName         string        `env:"CONTROL_API_GATEWAY_CONTROL_PLANE_TLS_SERVER_NAME"`
	ControlPlaneCAFile                string        `env:"CONTROL_API_GATEWAY_CONTROL_PLANE_CA_FILE"`
	ControlPlaneClientCertificateFile string        `env:"CONTROL_API_GATEWAY_CONTROL_PLANE_CLIENT_CERTIFICATE_FILE"`
	ControlPlaneClientPrivateKeyFile  string        `env:"CONTROL_API_GATEWAY_CONTROL_PLANE_CLIENT_PRIVATE_KEY_FILE"`
	ControlPlaneApplicationGrantFile  string        `env:"CONTROL_API_GATEWAY_CONTROL_PLANE_APPLICATION_GRANT_FILE"`
	InteractionTarget                 string        `env:"CONTROL_API_GATEWAY_INTERACTION_TARGET"`
	InteractionTLSServerName          string        `env:"CONTROL_API_GATEWAY_INTERACTION_TLS_SERVER_NAME"`
	IntegrationTarget                 string        `env:"CONTROL_API_GATEWAY_INTEGRATION_TARGET"`
	IntegrationTLSServerName          string        `env:"CONTROL_API_GATEWAY_INTEGRATION_TLS_SERVER_NAME"`
	RequestTimeout                    time.Duration `env:"CONTROL_API_GATEWAY_REQUEST_TIMEOUT"`
	RPCTimeout                        time.Duration `env:"CONTROL_API_GATEWAY_RPC_TIMEOUT"`
	StartupTimeout                    time.Duration `env:"CONTROL_API_GATEWAY_STARTUP_TIMEOUT"`
	ShutdownTimeout                   time.Duration `env:"CONTROL_API_GATEWAY_SHUTDOWN_TIMEOUT"`
	ReadinessInterval                 time.Duration `env:"CONTROL_API_GATEWAY_READINESS_INTERVAL"`
	RealtimePollInterval              time.Duration `env:"CONTROL_API_GATEWAY_REALTIME_POLL_INTERVAL"`
	RateWindow                        time.Duration `env:"CONTROL_API_GATEWAY_RATE_WINDOW"`
	RateLimit                         uint32        `env:"CONTROL_API_GATEWAY_RATE_LIMIT"`
	MaximumRateKeys                   int           `env:"CONTROL_API_GATEWAY_MAXIMUM_RATE_KEYS"`
	PreAuthConcurrency                int           `env:"CONTROL_API_GATEWAY_PREAUTH_CONCURRENCY"`
	MaximumHTTPConcurrency            int           `env:"CONTROL_API_GATEWAY_MAXIMUM_HTTP_CONCURRENCY"`
	PerSubjectHTTPConcurrency         int           `env:"CONTROL_API_GATEWAY_PER_SUBJECT_HTTP_CONCURRENCY"`
	MaximumWebSocketConcurrency       int           `env:"CONTROL_API_GATEWAY_MAXIMUM_WEBSOCKET_CONCURRENCY"`
	PerSubjectWebSocketConcurrency    int           `env:"CONTROL_API_GATEWAY_PER_SUBJECT_WEBSOCKET_CONCURRENCY"`
}

func loadConfig() (Config, error) {
	config := Config{
		HTTPListen: ":8443", TechnicalListen: ":9090",
		TLSCertificateFile:    "/var/run/secrets/mattercodex/control-api-gateway/public-tls/tls.crt",
		TLSPrivateKeyFile:     "/var/run/secrets/mattercodex/control-api-gateway/public-tls/tls.key",
		PublicTLSCAFile:       "/var/run/secrets/mattercodex/control-api-gateway/public-tls/ca.crt",
		PublicTLSMaterialFile: "/var/run/secrets/mattercodex/control-api-gateway/public-tls/material.json",
		PublicTLSServerName:   "control-api.mattercodex.local",
		OIDCIssuer:            "https://sso.kodex.works/realms/mattercodex", OIDCAudience: "mattercodex-control-api",
		OIDCConnectAddress: "sso.identity.svc.cluster.local:443",
		OIDCTLSServerName:  "sso.kodex.works", OIDCCAFile: "/var/run/config/mattercodex/control-api-gateway/oidc/ca.pem",
		AllowedOrigins:         "https://control.kodex.works",
		SessionCurrentKeyFile:  "/var/run/secrets/mattercodex/control-api-gateway/session/current.hex",
		SessionPreviousKeyFile: "/var/run/secrets/mattercodex/control-api-gateway/session/previous.hex", SessionTTL: 15 * time.Minute,
		ControlPlaneTarget:                controlPlaneTarget,
		ControlPlaneTLSServerName:         controlPlaneTLSServerName,
		ControlPlaneCAFile:                "/var/run/config/mattercodex/control-api-gateway/control-plane/ca.pem",
		ControlPlaneClientCertificateFile: "/var/run/secrets/mattercodex/control-api-gateway/control-plane-client/tls.crt",
		ControlPlaneClientPrivateKeyFile:  "/var/run/secrets/mattercodex/control-api-gateway/control-plane-client/tls.key",
		ControlPlaneApplicationGrantFile:  "/var/run/secrets/mattercodex/control-api-gateway/application-grant/readiness.jwt",
		InteractionTarget:                 interactionTarget, InteractionTLSServerName: interactionTLSServerName,
		IntegrationTarget: integrationTarget, IntegrationTLSServerName: integrationTLSServerName,
		RequestTimeout: 15 * time.Second, RPCTimeout: 5 * time.Second, StartupTimeout: 20 * time.Second,
		ShutdownTimeout: 20 * time.Second, ReadinessInterval: 10 * time.Second, RealtimePollInterval: 3 * time.Second,
		RateWindow: time.Minute, RateLimit: 120, MaximumRateKeys: 10000,
		PreAuthConcurrency: 32, MaximumHTTPConcurrency: 256, PerSubjectHTTPConcurrency: 16,
		MaximumWebSocketConcurrency: 128, PerSubjectWebSocketConcurrency: 4,
	}
	if err := env.Parse(&config); err != nil {
		return Config{}, err
	}
	return config, config.validate()
}

func (config Config) validate() error {
	for _, address := range []string{config.HTTPListen, config.TechnicalListen} {
		if _, _, err := net.SplitHostPort(address); err != nil {
			return errors.New("control API listen address is invalid")
		}
	}
	if config.HTTPListen == config.TechnicalListen {
		return errors.New("control API listeners must be separate")
	}
	for _, path := range []string{config.TLSCertificateFile, config.TLSPrivateKeyFile, config.PublicTLSCAFile, config.PublicTLSMaterialFile, config.OIDCCAFile, config.SessionCurrentKeyFile, config.ControlPlaneCAFile, config.ControlPlaneClientCertificateFile, config.ControlPlaneClientPrivateKeyFile, config.ControlPlaneApplicationGrantFile} {
		if !filepath.IsAbs(path) {
			return errors.New("control API runtime path is invalid")
		}
	}
	if config.SessionPreviousKeyFile != "" && !filepath.IsAbs(config.SessionPreviousKeyFile) {
		return errors.New("control API previous session key path is invalid")
	}
	issuer, err := url.Parse(config.OIDCIssuer)
	if err != nil || issuer.Scheme != "https" || issuer.Hostname() != config.OIDCTLSServerName || issuer.User != nil || issuer.RawQuery != "" || issuer.Fragment != "" {
		return errors.New("control API OIDC issuer is invalid")
	}
	oidcConnectHost, oidcConnectPort, oidcConnectErr := net.SplitHostPort(config.OIDCConnectAddress)
	if oidcConnectErr != nil || oidcConnectHost == "" || net.ParseIP(oidcConnectHost) != nil || oidcConnectPort != "443" {
		return errors.New("control API OIDC connect address is invalid")
	}
	if config.PublicTLSServerName == "" || net.ParseIP(config.PublicTLSServerName) != nil ||
		config.OIDCAudience != "mattercodex-control-api" || config.OIDCTLSServerName == "" || net.ParseIP(config.OIDCTLSServerName) != nil ||
		config.ControlPlaneTarget != controlPlaneTarget || config.ControlPlaneTLSServerName != controlPlaneTLSServerName ||
		config.InteractionTarget != interactionTarget || config.InteractionTLSServerName != interactionTLSServerName ||
		config.IntegrationTarget != integrationTarget || config.IntegrationTLSServerName != integrationTLSServerName {
		return errors.New("control API identity or TLS configuration is invalid")
	}
	origins := strings.Split(config.AllowedOrigins, ",")
	if len(origins) == 0 || len(origins) > 8 {
		return errors.New("control API CORS allowlist is invalid")
	}
	for _, origin := range origins {
		if strings.TrimSpace(origin) != origin || origin == "" || origin == "*" {
			return errors.New("control API CORS allowlist is invalid")
		}
	}
	if config.SessionTTL < time.Minute || config.SessionTTL > time.Hour || config.RequestTimeout < time.Second || config.RequestTimeout > time.Minute ||
		config.RPCTimeout < time.Second || config.RPCTimeout > 10*time.Second || config.StartupTimeout < time.Second || config.StartupTimeout > time.Minute ||
		config.ShutdownTimeout < time.Second || config.ShutdownTimeout > time.Minute || config.ReadinessInterval < time.Second || config.RealtimePollInterval < time.Second ||
		config.RateWindow < time.Second || config.RateWindow > time.Hour || config.RateLimit == 0 || config.RateLimit > 10000 ||
		config.ShutdownTimeout < config.RequestTimeout || config.MaximumRateKeys < 100 || config.MaximumRateKeys > 100000 ||
		config.PreAuthConcurrency < 1 || config.PreAuthConcurrency > 256 || config.MaximumHTTPConcurrency < 1 || config.MaximumHTTPConcurrency > 2048 ||
		config.PerSubjectHTTPConcurrency < 1 || config.PerSubjectHTTPConcurrency >= config.MaximumHTTPConcurrency ||
		config.MaximumWebSocketConcurrency < 1 || config.MaximumWebSocketConcurrency > 1024 ||
		config.PerSubjectWebSocketConcurrency < 1 || config.PerSubjectWebSocketConcurrency >= config.MaximumWebSocketConcurrency {
		return errors.New("control API bounded configuration is invalid")
	}
	return nil
}

func (config Config) origins() []string { return strings.Split(config.AllowedOrigins, ",") }
