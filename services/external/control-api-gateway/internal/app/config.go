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
	// serviceName supplies the fixed gateway identity used by telemetry and the NATS connection name, not a caller-controlled resource or actor name.
	// serviceName задаёт фиксированную идентичность gateway для telemetry и имени NATS connection, а не выбранные caller имена ресурса и actor.
	serviceName = "control-api-gateway"
	// controlPlaneTarget pins the permitted gRPC resolver target for the control-plane service; configuration validation rejects another target independently of certificate trust.
	// controlPlaneTarget закрепляет допустимый gRPC resolver target control-plane service; configuration validation отклоняет иной target независимо от certificate trust.
	controlPlaneTarget = "dns:///control-plane.kodex-system.svc:8443"
	// controlPlaneTLSServerName pins the exact control-plane TLS identity separately from its resolver address; the transport client performs certificate verification.
	// controlPlaneTLSServerName закрепляет точную TLS identity control-plane отдельно от resolver address; сертификат проверяет transport client.
	controlPlaneTLSServerName = "control-plane.kodex-system.svc.cluster.local"
	// secretBrokerTarget pins the sole secret-broker gRPC destination used for protected secret operations rather than a URL supplied by browser input.
	// secretBrokerTarget закрепляет единственное secret-broker gRPC destination защищённых secret operations, а не URL browser input.
	secretBrokerTarget = "dns:///secret-broker.kodex-system.svc:8443"
	// secretBrokerTLSServerName fixes the TLS server identity expected for the dedicated secret broker, independent of the caller's project or grant payload.
	// secretBrokerTLSServerName фиксирует ожидаемую TLS server identity выделенного secret broker независимо от проекта caller и grant payload.
	secretBrokerTLSServerName = "secret-broker.kodex-system.svc.cluster.local"
	// sttTarget pins the direct speech-service gRPC destination so organization-scoped speech operations do not route to an arbitrary caller-selected service.
	// sttTarget закрепляет direct speech-service gRPC destination, не позволяя organization-scoped speech operations выбрать произвольный сервис из caller input.
	sttTarget = "dns:///stt-tts-service.kodex-system.svc:8443"
	// sttTLSServerName fixes the speech service certificate name independently of the resolver target and the model-provider identity behind that service.
	// sttTLSServerName фиксирует имя сертификата speech service отдельно от resolver target и model-provider identity за этим сервисом.
	sttTLSServerName = "stt-tts-service.kodex-system.svc.cluster.local"
)

// Config holds the process environment bindings for listeners, trust/credential paths, OIDC/session state, exact internal targets, NATS and bounded request/concurrency budgets. Values are validated during loading; no secret contents are embedded in this structure.
// Config содержит process environment bindings listeners, trust и credential paths, OIDC и session state, exact internal targets, NATS и ограниченные request и concurrency budgets. Значения проверяются при загрузке; содержимое секретов в структуру не включается.
type Config struct {
	RPCProfile                        string        `env:"KODEX_RPC_PROFILE"`
	WebSocketLegacyUntil              string        `env:"KODEX_WS_LEGACY_UNTIL"`
	HTTPListen                        string        `env:"CONTROL_API_GATEWAY_HTTP_LISTEN"`
	TechnicalListen                   string        `env:"CONTROL_API_GATEWAY_TECHNICAL_LISTEN"`
	TLSCertificateFile                string        `env:"CONTROL_API_GATEWAY_TLS_CERTIFICATE_FILE"`
	TLSPrivateKeyFile                 string        `env:"CONTROL_API_GATEWAY_TLS_PRIVATE_KEY_FILE"`
	OIDCIssuer                        string        `env:"CONTROL_API_GATEWAY_OIDC_ISSUER"`
	OIDCAudience                      string        `env:"CONTROL_API_GATEWAY_OIDC_AUDIENCE"`
	OIDCBrowserOrigin                 string        `env:"CONTROL_API_GATEWAY_OIDC_BROWSER_ORIGIN"`
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
	STTTarget                         string        `env:"CONTROL_API_GATEWAY_STT_TARGET"`
	STTTLSServerName                  string        `env:"CONTROL_API_GATEWAY_STT_TLS_SERVER_NAME"`
	STTCAFile                         string        `env:"CONTROL_API_GATEWAY_STT_CA_FILE"`
	STTClientCertificateFile          string        `env:"CONTROL_API_GATEWAY_STT_CLIENT_CERTIFICATE_FILE"`
	STTClientPrivateKeyFile           string        `env:"CONTROL_API_GATEWAY_STT_CLIENT_PRIVATE_KEY_FILE"`
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

// loadConfig applies explicit process defaults, parses tagged environment overrides with caarlos0/env and returns the config with its validation result. It does not open referenced files, dial dependencies or silently replace invalid values.
// loadConfig задаёт явные process defaults, разбирает помеченные environment overrides через caarlos0/env и возвращает config с результатом validation. Указанные файлы не открываются, dependencies не подключаются и неверные значения тихо не заменяются.
func loadConfig() (Config, error) {
	config := Config{
		HTTPListen: ":8443", TechnicalListen: ":9090", TLSCertificateFile: "/var/run/secrets/kodex/control-api-gateway/public-tls/tls.crt", TLSPrivateKeyFile: "/var/run/secrets/kodex/control-api-gateway/public-tls/tls.key",
		OIDCAudience: "kodex-control-api", OIDCCAFile: "/var/run/config/kodex/control-api-gateway/oidc/ca.pem",
		SessionCurrentKeyFile: "/var/run/secrets/kodex/control-api-gateway/session/current.hex", SessionPreviousKeyFile: "/var/run/secrets/kodex/control-api-gateway/session/previous.hex", SessionTTL: 15 * time.Minute,
		ControlPlaneTarget: controlPlaneTarget, ControlPlaneTLSServerName: controlPlaneTLSServerName, ControlPlaneCAFile: "/var/run/config/kodex/control-api-gateway/control-plane/ca.pem", ControlPlaneClientCertificateFile: "/var/run/secrets/kodex/control-api-gateway/control-plane-client/tls.crt", ControlPlaneClientPrivateKeyFile: "/var/run/secrets/kodex/control-api-gateway/control-plane-client/tls.key",
		SecretBrokerTarget: secretBrokerTarget, SecretBrokerTLSServerName: secretBrokerTLSServerName, SecretBrokerCAFile: "/var/run/config/kodex/control-api-gateway/secret-broker/ca.pem", SecretBrokerClientCertificateFile: "/var/run/secrets/kodex/control-api-gateway/control-plane-client/tls.crt", SecretBrokerClientPrivateKeyFile: "/var/run/secrets/kodex/control-api-gateway/control-plane-client/tls.key",
		STTTarget: sttTarget, STTTLSServerName: sttTLSServerName, STTCAFile: "/var/run/config/kodex/control-api-gateway/control-plane/ca.pem", STTClientCertificateFile: "/var/run/secrets/kodex/control-api-gateway/control-plane-client/tls.crt", STTClientPrivateKeyFile: "/var/run/secrets/kodex/control-api-gateway/control-plane-client/tls.key",
		NATSURL: "tls://nats.kodex-system.svc:4222", NATSTLSServerName: "nats.kodex-system.svc.cluster.local", NATSCAFile: "/var/run/config/kodex/control-api-gateway/nats/ca.pem", NATSCertificateFile: "/var/run/secrets/kodex/control-api-gateway/nats-client/tls.crt", NATSPrivateKeyFile: "/var/run/secrets/kodex/control-api-gateway/nats-client/tls.key", NATSCredentialsFile: "/var/run/secrets/kodex/control-api-gateway/nats/user.creds", NATSReplicas: 1,
		RequestTimeout: 15 * time.Second, RPCTimeout: 5 * time.Second, StartupTimeout: 20 * time.Second, ShutdownTimeout: 20 * time.Second, ReadinessInterval: 10 * time.Second, RateWindow: time.Minute, RateLimit: 120, MaximumRateKeys: 10000, PreAuthConcurrency: 32, MaximumHTTPConcurrency: 256, PerSubjectHTTPConcurrency: 16, MaximumWebSocketConcurrency: 128, PerSubjectWebSocketConcurrency: 4,
	}
	if err := env.Parse(&config); err != nil {
		return Config{}, err
	}
	return config, config.validate()
}

// validate checks the RPC profile, absolute cutoff/listener/path shape, HTTPS OIDC and origin relationships, pinned internal targets, TLS NATS shape and selected timeout/concurrency bounds. It does not read credentials, verify certificates or prove dependency readiness; later constructors apply their own constraints.
// validate проверяет RPC profile, форму абсолютного cutoff, listeners и paths, связи HTTPS OIDC и origins, закреплённые internal targets, форму TLS NATS и выбранные timeout и concurrency bounds. Credentials не читаются, сертификаты и готовность dependencies не проверяются; последующие конструкторы применяют свои ограничения.
func (config Config) validate() error {
	if config.RPCProfile != "" && config.RPCProfile != "trusted-cluster" {
		return errors.New("control API RPC profile is invalid")
	}
	if _, err := config.legacyWebSocketDeadline(time.Now().UTC()); err != nil {
		return err
	}
	for _, address := range []string{config.HTTPListen, config.TechnicalListen} {
		if _, _, err := net.SplitHostPort(address); err != nil {
			return errors.New("control API listen address is invalid")
		}
	}
	if config.HTTPListen == config.TechnicalListen {
		return errors.New("control API listeners must be separate")
	}
	for _, path := range []string{config.TLSCertificateFile, config.TLSPrivateKeyFile, config.OIDCCAFile, config.SessionCurrentKeyFile, config.ControlPlaneCAFile, config.ControlPlaneClientCertificateFile, config.ControlPlaneClientPrivateKeyFile, config.SecretBrokerCAFile, config.SecretBrokerClientCertificateFile, config.SecretBrokerClientPrivateKeyFile, config.STTCAFile, config.STTClientCertificateFile, config.STTClientPrivateKeyFile, config.NATSCAFile, config.NATSCertificateFile, config.NATSPrivateKeyFile, config.NATSCredentialsFile} {
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
	if config.ControlPlaneTarget != controlPlaneTarget || config.ControlPlaneTLSServerName != controlPlaneTLSServerName || config.SecretBrokerTarget != secretBrokerTarget || config.SecretBrokerTLSServerName != secretBrokerTLSServerName || config.STTTarget != sttTarget || config.STTTLSServerName != sttTLSServerName || config.NATSTLSServerName == "" || net.ParseIP(config.NATSTLSServerName) != nil {
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
	browserOriginAllowed := false
	for _, origin := range origins {
		browserOriginAllowed = browserOriginAllowed || origin == config.OIDCBrowserOrigin
	}
	if !browserOriginAllowed {
		return errors.New("control API OIDC browser origin is invalid")
	}
	if config.RequestTimeout < time.Second || config.RequestTimeout > time.Minute || config.RPCTimeout < time.Second || config.RPCTimeout > 10*time.Second || config.StartupTimeout < time.Second || config.ShutdownTimeout < config.RequestTimeout || config.ReadinessInterval < time.Second || config.NATSReplicas < 1 || config.NATSReplicas > 5 || config.RateLimit == 0 || config.MaximumRateKeys < 100 || config.PreAuthConcurrency < 1 || config.MaximumHTTPConcurrency < 2 || config.PerSubjectHTTPConcurrency >= config.MaximumHTTPConcurrency || config.MaximumWebSocketConcurrency < 2 || config.PerSubjectWebSocketConcurrency >= config.MaximumWebSocketConcurrency {
		return errors.New("control API bounded configuration is invalid")
	}
	return nil
}

// origins splits the configured comma-delimited allowlist without trimming, sorting, deduplicating or validating entries; load-time validation owns eligibility.
// origins делит настроенный allowlist по запятым без обрезки, сортировки, удаления повторов и проверки entries; допустимость проверяется при загрузке.
func (config Config) origins() []string { return strings.Split(config.AllowedOrigins, ",") }

// legacyWebSocketDeadline accepts an absent cutoff or canonical UTC-Z RFC3339 text no more than twenty-four hours ahead of the supplied clock. An already expired absolute cutoff remains valid data and is not reset to a fresh window.
// legacyWebSocketDeadline допускает отсутствие cutoff либо канонический UTC-Z RFC3339 текст не более чем на двадцать четыре часа впереди переданных часов. Уже истёкший абсолютный cutoff остаётся допустимыми данными и не сбрасывается на новое окно.
func (config Config) legacyWebSocketDeadline(now time.Time) (time.Time, error) {
	if config.WebSocketLegacyUntil == "" {
		return time.Time{}, nil
	}
	until, err := time.Parse(time.RFC3339, config.WebSocketLegacyUntil)
	if err != nil || !strings.HasSuffix(config.WebSocketLegacyUntil, "Z") || until.Format(time.RFC3339) != config.WebSocketLegacyUntil || until.After(now.Add(24*time.Hour)) {
		return time.Time{}, errors.New("legacy WebSocket cutoff is invalid")
	}
	return until, nil
}
