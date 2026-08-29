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
	controlPlaneTarget        = "dns:///control-plane.kodex-system.svc:8443"
	controlPlaneTLSServerName = "control-plane.kodex-system.svc.cluster.local"
	secretBrokerTarget        = "dns:///secret-broker.kodex-system.svc:8443"
	secretBrokerTLSServerName = "secret-broker.kodex-system.svc.cluster.local"
)

type Config struct {
	HTTPListen                        string        `env:"CONTROL_API_GATEWAY_HTTP_LISTEN"`
	TechnicalListen                   string        `env:"CONTROL_API_GATEWAY_TECHNICAL_LISTEN"`
	TLSCertificateFile                string        `env:"CONTROL_API_GATEWAY_TLS_CERTIFICATE_FILE"`
	TLSPrivateKeyFile                 string        `env:"CONTROL_API_GATEWAY_TLS_PRIVATE_KEY_FILE"`
	OIDCIssuer                        string        `env:"CONTROL_API_GATEWAY_OIDC_ISSUER"`
	OIDCAudience                      string        `env:"CONTROL_API_GATEWAY_OIDC_AUDIENCE"`
	OIDCJWKSURL                       string        `env:"CONTROL_API_GATEWAY_OIDC_JWKS_URL"`
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
	SecretBrokerTarget                string        `env:"CONTROL_API_GATEWAY_SECRET_BROKER_TARGET"`
	SecretBrokerTLSServerName         string        `env:"CONTROL_API_GATEWAY_SECRET_BROKER_TLS_SERVER_NAME"`
	SecretBrokerCAFile                string        `env:"CONTROL_API_GATEWAY_SECRET_BROKER_CA_FILE"`
	SecretBrokerClientCertificateFile string        `env:"CONTROL_API_GATEWAY_SECRET_BROKER_CLIENT_CERTIFICATE_FILE"`
	SecretBrokerClientPrivateKeyFile  string        `env:"CONTROL_API_GATEWAY_SECRET_BROKER_CLIENT_PRIVATE_KEY_FILE"`
	NATSURL                           string        `env:"CONTROL_API_GATEWAY_NATS_URL"`
	NATSTLSServerName                 string        `env:"CONTROL_API_GATEWAY_NATS_TLS_SERVER_NAME"`
	NATSCAFile                        string        `env:"CONTROL_API_GATEWAY_NATS_CA_FILE"`
	NATSCertificateFile               string        `env:"CONTROL_API_GATEWAY_NATS_CERTIFICATE_FILE"`
	NATSPrivateKeyFile                string        `env:"CONTROL_API_GATEWAY_NATS_PRIVATE_KEY_FILE"`
	NATSCredentialsFile               string        `env:"CONTROL_API_GATEWAY_NATS_CREDENTIALS_FILE"`
	NATSReplicas                      int           `env:"CONTROL_API_GATEWAY_NATS_REPLICAS"`
	RequestTimeout                    time.Duration `env:"CONTROL_API_GATEWAY_REQUEST_TIMEOUT"`
	RPCTimeout                        time.Duration `env:"CONTROL_API_GATEWAY_RPC_TIMEOUT"`
	StartupTimeout                    time.Duration `env:"CONTROL_API_GATEWAY_STARTUP_TIMEOUT"`
	ShutdownTimeout                   time.Duration `env:"CONTROL_API_GATEWAY_SHUTDOWN_TIMEOUT"`
	ReadinessInterval                 time.Duration `env:"CONTROL_API_GATEWAY_READINESS_INTERVAL"`
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
		HTTPListen: ":8443", TechnicalListen: ":9090", TLSCertificateFile: "/var/run/secrets/kodex/control-api-gateway/public-tls/tls.crt", TLSPrivateKeyFile: "/var/run/secrets/kodex/control-api-gateway/public-tls/tls.key",
		OIDCAudience: "kodex-control-api", OIDCCAFile: "/var/run/config/kodex/control-api-gateway/oidc/ca.pem",
		SessionCurrentKeyFile: "/var/run/secrets/kodex/control-api-gateway/session/current.hex", SessionPreviousKeyFile: "/var/run/secrets/kodex/control-api-gateway/session/previous.hex", SessionTTL: 15 * time.Minute,
		ControlPlaneTarget: controlPlaneTarget, ControlPlaneTLSServerName: controlPlaneTLSServerName, ControlPlaneCAFile: "/var/run/config/kodex/control-api-gateway/control-plane/ca.pem", ControlPlaneClientCertificateFile: "/var/run/secrets/kodex/control-api-gateway/control-plane-client/tls.crt", ControlPlaneClientPrivateKeyFile: "/var/run/secrets/kodex/control-api-gateway/control-plane-client/tls.key",
		SecretBrokerTarget: secretBrokerTarget, SecretBrokerTLSServerName: secretBrokerTLSServerName, SecretBrokerCAFile: "/var/run/config/kodex/control-api-gateway/secret-broker/ca.pem", SecretBrokerClientCertificateFile: "/var/run/secrets/kodex/control-api-gateway/control-plane-client/tls.crt", SecretBrokerClientPrivateKeyFile: "/var/run/secrets/kodex/control-api-gateway/control-plane-client/tls.key",
		NATSURL: "tls://nats.kodex-system.svc:4222", NATSTLSServerName: "nats.kodex-system.svc.cluster.local", NATSCAFile: "/var/run/config/kodex/control-api-gateway/nats/ca.pem", NATSCertificateFile: "/var/run/secrets/kodex/control-api-gateway/nats-client/tls.crt", NATSPrivateKeyFile: "/var/run/secrets/kodex/control-api-gateway/nats-client/tls.key", NATSCredentialsFile: "/var/run/secrets/kodex/control-api-gateway/nats/user.creds", NATSReplicas: 1,
		RequestTimeout: 15 * time.Second, RPCTimeout: 5 * time.Second, StartupTimeout: 20 * time.Second, ShutdownTimeout: 20 * time.Second, ReadinessInterval: 10 * time.Second, RateWindow: time.Minute, RateLimit: 120, MaximumRateKeys: 10000, PreAuthConcurrency: 32, MaximumHTTPConcurrency: 256, PerSubjectHTTPConcurrency: 16, MaximumWebSocketConcurrency: 128, PerSubjectWebSocketConcurrency: 4,
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
	for _, path := range []string{config.TLSCertificateFile, config.TLSPrivateKeyFile, config.OIDCCAFile, config.SessionCurrentKeyFile, config.ControlPlaneCAFile, config.ControlPlaneClientCertificateFile, config.ControlPlaneClientPrivateKeyFile, config.SecretBrokerCAFile, config.SecretBrokerClientCertificateFile, config.SecretBrokerClientPrivateKeyFile, config.NATSCAFile, config.NATSCertificateFile, config.NATSPrivateKeyFile, config.NATSCredentialsFile} {
		if !filepath.IsAbs(path) {
			return errors.New("control API runtime path is invalid")
		}
	}
	issuer, err := url.Parse(config.OIDCIssuer)
	jwks, jwksErr := url.Parse(config.OIDCJWKSURL)
	if err != nil || issuer.Scheme != "https" || issuer.Hostname() != config.OIDCTLSServerName ||
		jwksErr != nil || jwks.Scheme != "https" || jwks.Hostname() != issuer.Hostname() ||
		jwks.User != nil || jwks.RawQuery != "" || jwks.Fragment != "" || jwks.Path == "" {
		return errors.New("control API OIDC issuer is invalid")
	}
	natsURL, err := url.Parse(config.NATSURL)
	if err != nil || natsURL.Scheme != "tls" || natsURL.Port() != "4222" || natsURL.Hostname() == "" || net.ParseIP(natsURL.Hostname()) != nil {
		return errors.New("control API NATS URL is invalid")
	}
	if config.ControlPlaneTarget != controlPlaneTarget || config.ControlPlaneTLSServerName != controlPlaneTLSServerName || config.SecretBrokerTarget != secretBrokerTarget || config.SecretBrokerTLSServerName != secretBrokerTLSServerName || config.NATSTLSServerName == "" || net.ParseIP(config.NATSTLSServerName) != nil {
		return errors.New("control API internal identity is invalid")
	}
	origins := strings.Split(config.AllowedOrigins, ",")
	if len(origins) < 1 || len(origins) > 8 {
		return errors.New("control API origin allowlist is invalid")
	}
	for _, origin := range origins {
		parsed, parseErr := url.Parse(origin)
		if parseErr != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || origin == "*" {
			return errors.New("control API origin allowlist is invalid")
		}
	}
	if config.RequestTimeout < time.Second || config.RequestTimeout > time.Minute || config.RPCTimeout < time.Second || config.RPCTimeout > 10*time.Second || config.StartupTimeout < time.Second || config.ShutdownTimeout < config.RequestTimeout || config.ReadinessInterval < time.Second || config.NATSReplicas < 1 || config.NATSReplicas > 5 || config.RateLimit == 0 || config.MaximumRateKeys < 100 || config.PreAuthConcurrency < 1 || config.MaximumHTTPConcurrency < 2 || config.PerSubjectHTTPConcurrency >= config.MaximumHTTPConcurrency || config.MaximumWebSocketConcurrency < 2 || config.PerSubjectWebSocketConcurrency >= config.MaximumWebSocketConcurrency {
		return errors.New("control API bounded configuration is invalid")
	}
	return nil
}
func (config Config) origins() []string { return strings.Split(config.AllowedOrigins, ",") }
