package app

import (
	"errors"
	"net"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
)

type Config struct {
	Gateway    GatewayConfig    `envPrefix:"INTERACTION_GATEWAY_"`
	Mattermost MattermostConfig `envPrefix:"INTERACTION_GATEWAY_MATTERMOST_"`
	Object     ObjectConfig     `envPrefix:"INTERACTION_GATEWAY_S3_"`
	Control    ControlConfig    `envPrefix:"INTERACTION_GATEWAY_CONTROL_PLANE_"`
}

type GatewayConfig struct {
	HTTPListen                  string        `env:"HTTP_LISTEN" envDefault:":8443"`
	TechnicalListen             string        `env:"TECHNICAL_LISTEN" envDefault:":9090"`
	TLSCertificateFile          string        `env:"TLS_CERTIFICATE_FILE,required"`
	TLSPrivateKeyFile           string        `env:"TLS_PRIVATE_KEY_FILE,required"`
	TLSClientCAFile             string        `env:"TLS_CLIENT_CA_FILE,required"`
	MattermostClientSPIFFE      string        `env:"MATTERMOST_CLIENT_SPIFFE_ID,required"`
	ReadbackClientSPIFFEIDs     []string      `env:"READBACK_CLIENT_SPIFFE_IDS,required" envSeparator:","`
	SlashTokenFile              string        `env:"SLASH_TOKEN_FILE,required"`
	PostgresDSNFile             string        `env:"POSTGRES_DSN_FILE,required"`
	PostgresCAFile              string        `env:"POSTGRES_CA_FILE,required"`
	PostgresTLSServerName       string        `env:"POSTGRES_TLS_SERVER_NAME,required"`
	PostgresExpectedUser        string        `env:"POSTGRES_EXPECTED_SESSION_USER,required"`
	PostgresPrincipalGeneration uint64        `env:"POSTGRES_PRINCIPAL_GENERATION,required"`
	PostgresContextKeyID        string        `env:"POSTGRES_CONTEXT_KEY_ID,required"`
	PostgresContextKeyFile      string        `env:"POSTGRES_CONTEXT_KEY_FILE,required"`
	PostgresContextTTL          time.Duration `env:"POSTGRES_CONTEXT_TTL" envDefault:"5s"`
	PostgresMaxConnections      int32         `env:"POSTGRES_MAX_CONNECTIONS" envDefault:"16"`
	DeliveryKeyFile             string        `env:"DELIVERY_KEY_FILE,required"`
	EventIssuer                 string        `env:"EVENT_ISSUER,required"`
	EventAudience               string        `env:"EVENT_AUDIENCE,required"`
	EventPrivateJWKFile         string        `env:"EVENT_PRIVATE_JWK_FILE,required"`
	EventGeneration             uint64        `env:"EVENT_GENERATION,required"`
	EventTTL                    time.Duration `env:"EVENT_TTL" envDefault:"2m"`
	CallbackKeyFile             string        `env:"CALLBACK_KEY_FILE,required"`
	ReadbackIssuer              string        `env:"READBACK_ISSUER,required"`
	ReadbackAudience            string        `env:"READBACK_AUDIENCE,required"`
	ReadbackPublicJWKFile       string        `env:"READBACK_PUBLIC_JWK_FILE,required"`
	ReadbackReadinessGrantFile  string        `env:"READBACK_READINESS_GRANT_FILE,required"`
	ReadbackGeneration          uint64        `env:"READBACK_GENERATION,required"`
	ActionCallbackURL           string        `env:"ACTION_CALLBACK_URL,required"`
	DialogCallbackURL           string        `env:"DIALOG_CALLBACK_URL,required"`
	RetentionRef                string        `env:"RETENTION_REF,required"`
	InstanceID                  string        `env:"INSTANCE_ID,required"`
	MaximumBodyBytes            int64         `env:"MAXIMUM_BODY_BYTES" envDefault:"1048576"`
	MaximumPromptBytes          int           `env:"MAXIMUM_PROMPT_BYTES" envDefault:"262144"`
	MaximumFiles                int           `env:"MAXIMUM_FILES" envDefault:"16"`
	MaximumAttempts             uint32        `env:"MAXIMUM_ATTEMPTS" envDefault:"8"`
	MaximumConnections          int           `env:"MAXIMUM_CONNECTIONS" envDefault:"128"`
	InboundLease                time.Duration `env:"INBOUND_LEASE" envDefault:"30s"`
	DeliveryLease               time.Duration `env:"DELIVERY_LEASE" envDefault:"30s"`
	ScanPollInterval            time.Duration `env:"SCAN_POLL_INTERVAL" envDefault:"5s"`
	RetryBase                   time.Duration `env:"RETRY_BASE" envDefault:"2s"`
	WorkerInterval              time.Duration `env:"WORKER_INTERVAL" envDefault:"500ms"`
	OwnerGateInterval           time.Duration `env:"OWNER_GATE_INTERVAL" envDefault:"2s"`
	ExpiryInterval              time.Duration `env:"EXPIRY_INTERVAL" envDefault:"30s"`
	ReadinessInterval           time.Duration `env:"READINESS_INTERVAL" envDefault:"10s"`
	OperationTimeout            time.Duration `env:"OPERATION_TIMEOUT" envDefault:"8s"`
	StartupTimeout              time.Duration `env:"STARTUP_TIMEOUT" envDefault:"30s"`
	ShutdownTimeout             time.Duration `env:"SHUTDOWN_TIMEOUT" envDefault:"10s"`
}

type MattermostConfig struct {
	SiteURL               string        `env:"SITE_URL,required"`
	TLSServerName         string        `env:"TLS_SERVER_NAME,required"`
	CAFile                string        `env:"CA_FILE,required"`
	ClientCertificateFile string        `env:"CLIENT_CERTIFICATE_FILE,required"`
	ClientPrivateKeyFile  string        `env:"CLIENT_PRIVATE_KEY_FILE,required"`
	MappingManifestFile   string        `env:"MAPPING_MANIFEST_FILE,required"`
	RequestTimeout        time.Duration `env:"REQUEST_TIMEOUT" envDefault:"10s"`
	MaximumFileBytes      int64         `env:"MAXIMUM_FILE_BYTES" envDefault:"67108864"`
	CatchUpWindow         time.Duration `env:"CATCH_UP_WINDOW" envDefault:"1h"`
}

type ObjectConfig struct {
	Endpoint               string        `env:"ENDPOINT,required"`
	PublicDownloadEndpoint string        `env:"PUBLIC_DOWNLOAD_ENDPOINT,required"`
	TLSServerName          string        `env:"TLS_SERVER_NAME,required"`
	CAFile                 string        `env:"CA_FILE,required"`
	ClientCertificateFile  string        `env:"CLIENT_CERTIFICATE_FILE,required"`
	ClientPrivateKeyFile   string        `env:"CLIENT_PRIVATE_KEY_FILE,required"`
	AccessKeyFile          string        `env:"ACCESS_KEY_FILE,required"`
	SecretKeyFile          string        `env:"SECRET_KEY_FILE,required"`
	SessionTokenFile       string        `env:"SESSION_TOKEN_FILE"`
	Bucket                 string        `env:"BUCKET,required"`
	MaximumObjectBytes     int64         `env:"MAXIMUM_OBJECT_BYTES" envDefault:"268435456"`
	Timeout                time.Duration `env:"TIMEOUT" envDefault:"15s"`
}

type ControlConfig struct {
	Target                     string        `env:"TARGET,required"`
	TLSServerName              string        `env:"TLS_SERVER_NAME,required"`
	CAFile                     string        `env:"CA_FILE,required"`
	ClientCertificateFile      string        `env:"CLIENT_CERTIFICATE_FILE,required"`
	ClientPrivateKeyFile       string        `env:"CLIENT_PRIVATE_KEY_FILE,required"`
	ApplicationGrantFile       string        `env:"APPLICATION_GRANT_FILE,required"`
	ExpectedAuthorityIssuerUID uint32        `env:"EXPECTED_AUTHORITY_ISSUER_UID" envDefault:"29001"`
	ExpectedAuthorityIssuerGID uint32        `env:"EXPECTED_AUTHORITY_ISSUER_GID" envDefault:"29000"`
	DialTimeout                time.Duration `env:"DIAL_TIMEOUT" envDefault:"2s"`
	RequestTimeout             time.Duration `env:"REQUEST_TIMEOUT" envDefault:"5s"`
}

func loadConfig() (Config, error) {
	var config Config
	if err := env.ParseWithOptions(&config, env.Options{}); err != nil {
		return Config{}, errors.New("parse interaction gateway configuration")
	}
	return config, config.validate()
}

func (config Config) validate() error {
	gateway := config.Gateway
	for _, address := range []string{gateway.HTTPListen, gateway.TechnicalListen} {
		if _, _, err := net.SplitHostPort(address); err != nil {
			return errors.New("interaction gateway listen address is invalid")
		}
	}
	for _, name := range []string{gateway.PostgresTLSServerName, config.Mattermost.TLSServerName,
		config.Object.TLSServerName, config.Control.TLSServerName} {
		if name == "" || net.ParseIP(name) != nil {
			return errors.New("interaction gateway TLS server name is invalid")
		}
	}
	for _, path := range []string{
		gateway.TLSCertificateFile, gateway.TLSPrivateKeyFile, gateway.TLSClientCAFile,
		gateway.SlashTokenFile, gateway.PostgresDSNFile, gateway.PostgresCAFile,
		gateway.PostgresContextKeyFile,
		gateway.DeliveryKeyFile, gateway.EventPrivateJWKFile, gateway.CallbackKeyFile,
		gateway.ReadbackPublicJWKFile, gateway.ReadbackReadinessGrantFile,
		config.Mattermost.CAFile, config.Mattermost.ClientCertificateFile,
		config.Mattermost.ClientPrivateKeyFile, config.Mattermost.MappingManifestFile,
		config.Object.CAFile, config.Object.ClientCertificateFile, config.Object.ClientPrivateKeyFile,
		config.Object.AccessKeyFile, config.Object.SecretKeyFile, config.Control.CAFile,
		config.Control.ClientCertificateFile, config.Control.ClientPrivateKeyFile,
		config.Control.ApplicationGrantFile,
	} {
		if !filepath.IsAbs(path) {
			return errors.New("interaction gateway runtime path is invalid")
		}
	}
	if config.Object.SessionTokenFile != "" && !filepath.IsAbs(config.Object.SessionTokenFile) {
		return errors.New("interaction gateway session token path is invalid")
	}
	for _, raw := range []string{gateway.ActionCallbackURL, gateway.DialogCallbackURL,
		config.Mattermost.SiteURL, config.Object.Endpoint, config.Object.PublicDownloadEndpoint} {
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
			return errors.New("interaction gateway HTTPS URL is invalid")
		}
	}
	expectedPostgresUser := "interaction_gateway_runtime_g" + strconv.FormatUint(gateway.PostgresPrincipalGeneration, 10)
	if !strings.HasPrefix(gateway.MattermostClientSPIFFE, "spiffe://") ||
		len(gateway.ReadbackClientSPIFFEIDs) == 0 || len(gateway.ReadbackClientSPIFFEIDs) > 8 ||
		gateway.InstanceID == "" || gateway.RetentionRef == "" || gateway.EventIssuer == "" || gateway.EventAudience == "" ||
		gateway.PostgresPrincipalGeneration == 0 || gateway.PostgresExpectedUser != expectedPostgresUser ||
		gateway.PostgresContextKeyID == "" ||
		gateway.PostgresContextTTL < time.Second || gateway.PostgresContextTTL > 10*time.Second ||
		gateway.PostgresMaxConnections < 2 || gateway.PostgresMaxConnections > 64 ||
		gateway.MaximumConnections < 16 || gateway.MaximumConnections > 1024 ||
		gateway.WorkerInterval < 50*time.Millisecond || gateway.OwnerGateInterval < 100*time.Millisecond ||
		gateway.ExpiryInterval < time.Second || gateway.ReadinessInterval < time.Second ||
		gateway.OperationTimeout < time.Second || gateway.OperationTimeout > time.Minute ||
		gateway.InboundLease <= gateway.OperationTimeout || gateway.DeliveryLease <= gateway.OperationTimeout ||
		gateway.StartupTimeout < 5*time.Second || gateway.StartupTimeout > 2*time.Minute ||
		gateway.ShutdownTimeout < time.Second || gateway.ShutdownTimeout > time.Minute {
		return errors.New("interaction gateway bounded configuration is invalid")
	}
	return nil
}
