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
	DeploymentProfile string                 `env:"INTERACTION_GATEWAY_DEPLOYMENT_PROFILE" envDefault:"production"`
	CredentialBackend string                 `env:"INTERACTION_GATEWAY_BOT_CREDENTIAL_BACKEND" envDefault:"vault"`
	Gateway           GatewayConfig          `envPrefix:"INTERACTION_GATEWAY_"`
	Mattermost        MattermostConfig       `envPrefix:"INTERACTION_GATEWAY_MATTERMOST_"`
	Object            ObjectConfig           `envPrefix:"INTERACTION_GATEWAY_S3_"`
	Control           ControlConfig          `envPrefix:"INTERACTION_GATEWAY_CONTROL_PLANE_"`
	Bot               BotConfig              `envPrefix:"INTERACTION_GATEWAY_BOT_SERVICE_"`
	Credential        CredentialConfig       `envPrefix:"INTERACTION_GATEWAY_BOT_CREDENTIAL_VAULT_"`
	DirectCredential  DirectCredentialConfig `envPrefix:"INTERACTION_GATEWAY_BOT_CREDENTIAL_KUBERNETES_"`
}

type GatewayConfig struct {
	HTTPListen                    string        `env:"HTTP_LISTEN" envDefault:":8443"`
	TeamRPCListen                 string        `env:"TEAM_RPC_LISTEN" envDefault:":9443"`
	TechnicalListen               string        `env:"TECHNICAL_LISTEN" envDefault:":9090"`
	TLSCertificateFile            string        `env:"TLS_CERTIFICATE_FILE,required"`
	TLSPrivateKeyFile             string        `env:"TLS_PRIVATE_KEY_FILE,required"`
	TLSClientCAFile               string        `env:"TLS_CLIENT_CA_FILE,required"`
	MattermostClientSPIFFE        string        `env:"MATTERMOST_CLIENT_SPIFFE_ID,required"`
	ReadbackClientSPIFFEIDs       []string      `env:"READBACK_CLIENT_SPIFFE_IDS,required" envSeparator:","`
	MaterializationClientSPIFFE   string        `env:"MATERIALIZATION_CLIENT_SPIFFE_ID,required"`
	TeamRPCClientSPIFFE           string        `env:"TEAM_RPC_CLIENT_SPIFFE_ID,required"`
	AuthorityVerifierUID          uint32        `env:"AUTHORITY_VERIFIER_UID" envDefault:"29002"`
	AuthorityVerifierGID          uint32        `env:"AUTHORITY_VERIFIER_GID" envDefault:"29000"`
	SlashTokenFile                string        `env:"SLASH_TOKEN_FILE,required"`
	PostgresDSNFile               string        `env:"POSTGRES_DSN_FILE,required"`
	PostgresCAFile                string        `env:"POSTGRES_CA_FILE,required"`
	PostgresTLSServerName         string        `env:"POSTGRES_TLS_SERVER_NAME,required"`
	PostgresExpectedUser          string        `env:"POSTGRES_EXPECTED_SESSION_USER,required"`
	PostgresPrincipalGeneration   uint64        `env:"POSTGRES_PRINCIPAL_GENERATION,required"`
	PostgresMaxConnections        int32         `env:"POSTGRES_MAX_CONNECTIONS" envDefault:"16"`
	DeliveryKeyFile               string        `env:"DELIVERY_KEY_FILE,required"`
	EventIssuer                   string        `env:"EVENT_ISSUER,required"`
	EventAudience                 string        `env:"EVENT_AUDIENCE,required"`
	EventPrivateJWKFile           string        `env:"EVENT_PRIVATE_JWK_FILE,required"`
	EventGeneration               uint64        `env:"EVENT_GENERATION,required"`
	EventTTL                      time.Duration `env:"EVENT_TTL" envDefault:"2m"`
	CallbackKeyFile               string        `env:"CALLBACK_KEY_FILE,required"`
	ReadbackIssuer                string        `env:"READBACK_ISSUER,required"`
	ReadbackAudience              string        `env:"READBACK_AUDIENCE,required"`
	ReadbackPublicKeysetFile      string        `env:"READBACK_PUBLIC_KEYSET_FILE,required"`
	ReadbackGeneration            uint64        `env:"READBACK_GENERATION,required"`
	ProviderReceiptIssuer         string        `env:"PROVIDER_RECEIPT_ISSUER,required"`
	ProviderReceiptPrivateJWKFile string        `env:"PROVIDER_RECEIPT_PRIVATE_JWK_FILE,required"`
	ProviderReceiptTTL            time.Duration `env:"PROVIDER_RECEIPT_TTL" envDefault:"2m"`
	ActionCallbackURL             string        `env:"ACTION_CALLBACK_URL,required"`
	DialogCallbackURL             string        `env:"DIALOG_CALLBACK_URL,required"`
	ArtifactDownloadBaseURL       string        `env:"ARTIFACT_DOWNLOAD_BASE_URL,required"`
	ArtifactDownloadTTL           time.Duration `env:"ARTIFACT_DOWNLOAD_TTL" envDefault:"5m"`
	RetentionRef                  string        `env:"RETENTION_REF,required"`
	InstanceID                    string        `env:"INSTANCE_ID,required"`
	MaximumBodyBytes              int64         `env:"MAXIMUM_BODY_BYTES" envDefault:"1048576"`
	MaximumPromptBytes            int           `env:"MAXIMUM_PROMPT_BYTES" envDefault:"262144"`
	MaximumFiles                  int           `env:"MAXIMUM_FILES" envDefault:"16"`
	MaximumAttempts               uint32        `env:"MAXIMUM_ATTEMPTS" envDefault:"8"`
	MaximumConnections            int           `env:"MAXIMUM_CONNECTIONS" envDefault:"128"`
	InboundLease                  time.Duration `env:"INBOUND_LEASE" envDefault:"30s"`
	DeliveryLease                 time.Duration `env:"DELIVERY_LEASE" envDefault:"30s"`
	TeamOperationLease            time.Duration `env:"TEAM_OPERATION_LEASE" envDefault:"30s"`
	TeamSelectorTTL               time.Duration `env:"TEAM_SELECTOR_TTL" envDefault:"15m"`
	TeamRecoveryInterval          time.Duration `env:"TEAM_RECOVERY_INTERVAL" envDefault:"5s"`
	TeamRecoveryWindow            time.Duration `env:"TEAM_RECOVERY_WINDOW" envDefault:"5m"`
	BotOperationLease             time.Duration `env:"BOT_OPERATION_LEASE" envDefault:"30s"`
	BotSelectorTTL                time.Duration `env:"BOT_SELECTOR_TTL" envDefault:"15m"`
	BotRecoveryInterval           time.Duration `env:"BOT_RECOVERY_INTERVAL" envDefault:"5s"`
	BotRecoveryWindow             time.Duration `env:"BOT_RECOVERY_WINDOW" envDefault:"5m"`
	ScanPollInterval              time.Duration `env:"SCAN_POLL_INTERVAL" envDefault:"5s"`
	RetryBase                     time.Duration `env:"RETRY_BASE" envDefault:"2s"`
	WorkerInterval                time.Duration `env:"WORKER_INTERVAL" envDefault:"500ms"`
	OwnerGateInterval             time.Duration `env:"OWNER_GATE_INTERVAL" envDefault:"2s"`
	ExpiryInterval                time.Duration `env:"EXPIRY_INTERVAL" envDefault:"30s"`
	ReadinessInterval             time.Duration `env:"READINESS_INTERVAL" envDefault:"10s"`
	OperationTimeout              time.Duration `env:"OPERATION_TIMEOUT" envDefault:"8s"`
	StartupTimeout                time.Duration `env:"STARTUP_TIMEOUT" envDefault:"30s"`
	ShutdownTimeout               time.Duration `env:"SHUTDOWN_TIMEOUT" envDefault:"10s"`
}

type MattermostConfig struct {
	SiteURL                 string        `env:"SITE_URL,required"`
	TLSServerName           string        `env:"TLS_SERVER_NAME,required"`
	CAFile                  string        `env:"CA_FILE,required"`
	ClientCertificateFile   string        `env:"CLIENT_CERTIFICATE_FILE,required"`
	ClientPrivateKeyFile    string        `env:"CLIENT_PRIVATE_KEY_FILE,required"`
	MappingManifestFile     string        `env:"MAPPING_MANIFEST_FILE,required"`
	MappingExpectedRevision string        `env:"MAPPING_EXPECTED_REVISION,required"`
	MappingSHA256File       string        `env:"MAPPING_SHA256_FILE,required"`
	MappingVaultKVVersion   uint64        `env:"MAPPING_VAULT_KV_VERSION,required"`
	RequestTimeout          time.Duration `env:"REQUEST_TIMEOUT" envDefault:"10s"`
	MaximumFileBytes        int64         `env:"MAXIMUM_FILE_BYTES" envDefault:"67108864"`
	CatchUpWindow           time.Duration `env:"CATCH_UP_WINDOW" envDefault:"1h"`
}

type ObjectConfig struct {
	Endpoint              string        `env:"ENDPOINT,required"`
	TLSServerName         string        `env:"TLS_SERVER_NAME,required"`
	CAFile                string        `env:"CA_FILE,required"`
	ClientCertificateFile string        `env:"CLIENT_CERTIFICATE_FILE,required"`
	ClientPrivateKeyFile  string        `env:"CLIENT_PRIVATE_KEY_FILE,required"`
	AccessKeyFile         string        `env:"ACCESS_KEY_FILE,required"`
	SecretKeyFile         string        `env:"SECRET_KEY_FILE,required"`
	SessionTokenFile      string        `env:"SESSION_TOKEN_FILE"`
	Bucket                string        `env:"BUCKET,required"`
	MaximumObjectBytes    int64         `env:"MAXIMUM_OBJECT_BYTES" envDefault:"268435456"`
	Timeout               time.Duration `env:"TIMEOUT" envDefault:"15s"`
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

type BotConfig struct {
	URL                   string        `env:"URL,required"`
	TLSServerName         string        `env:"TLS_SERVER_NAME,required"`
	CAFile                string        `env:"CA_FILE,required"`
	ClientCertificateFile string        `env:"CLIENT_CERTIFICATE_FILE,required"`
	ClientPrivateKeyFile  string        `env:"CLIENT_PRIVATE_KEY_FILE,required"`
	Timeout               time.Duration `env:"TIMEOUT" envDefault:"8s"`
}

type CredentialConfig struct {
	Address       string        `env:"ADDRESS"`
	TLSServerName string        `env:"TLS_SERVER_NAME"`
	CAFile        string        `env:"CA_FILE"`
	TokenFile     string        `env:"TOKEN_FILE"`
	AuthMount     string        `env:"AUTH_MOUNT" envDefault:"kubernetes"`
	Role          string        `env:"ROLE" envDefault:"interaction-gateway-agent-bot-credential"`
	Mount         string        `env:"MOUNT" envDefault:"mattercodex"`
	PathPrefix    string        `env:"PATH_PREFIX" envDefault:"interaction-gateway/agent-bot-identities"`
	Timeout       time.Duration `env:"TIMEOUT" envDefault:"5s"`
}

type DirectCredentialConfig struct {
	ResourceName string        `env:"RESOURCE_NAME"`
	DataKey      string        `env:"DATA_KEY"`
	Timeout      time.Duration `env:"TIMEOUT"`
}

func loadConfig() (Config, error) {
	config := Config{DirectCredential: defaultDirectCredentialConfig()}
	if err := env.ParseWithOptions(&config, env.Options{}); err != nil {
		return Config{}, errors.New("parse interaction gateway configuration")
	}
	return config, config.validate()
}

func (config Config) validate() error {
	credentialSelection, err := selectCredentialBackend(config.DeploymentProfile, config.CredentialBackend)
	if err != nil {
		return err
	}
	gateway := config.Gateway
	for _, address := range []string{gateway.HTTPListen, gateway.TeamRPCListen, gateway.TechnicalListen} {
		if _, _, err := net.SplitHostPort(address); err != nil {
			return errors.New("interaction gateway listen address is invalid")
		}
	}
	names := []string{
		gateway.PostgresTLSServerName, config.Mattermost.TLSServerName,
		config.Object.TLSServerName, config.Control.TLSServerName, config.Bot.TLSServerName,
	}
	if credentialSelection == credentialBackendVault {
		names = append(names, config.Credential.TLSServerName)
	}
	for _, name := range names {
		if name == "" || net.ParseIP(name) != nil {
			return errors.New("interaction gateway TLS server name is invalid")
		}
	}
	paths := []string{
		gateway.TLSCertificateFile, gateway.TLSPrivateKeyFile, gateway.TLSClientCAFile,
		gateway.SlashTokenFile, gateway.PostgresDSNFile, gateway.PostgresCAFile,
		gateway.DeliveryKeyFile, gateway.EventPrivateJWKFile, gateway.CallbackKeyFile,
		gateway.ReadbackPublicKeysetFile,
		gateway.ProviderReceiptPrivateJWKFile,
		config.Mattermost.CAFile, config.Mattermost.ClientCertificateFile,
		config.Mattermost.ClientPrivateKeyFile, config.Mattermost.MappingManifestFile,
		config.Mattermost.MappingSHA256File,
		config.Object.CAFile, config.Object.ClientCertificateFile, config.Object.ClientPrivateKeyFile,
		config.Object.AccessKeyFile, config.Object.SecretKeyFile, config.Control.CAFile,
		config.Control.ClientCertificateFile, config.Control.ClientPrivateKeyFile,
		config.Control.ApplicationGrantFile, config.Bot.CAFile, config.Bot.ClientCertificateFile,
		config.Bot.ClientPrivateKeyFile,
	}
	if credentialSelection == credentialBackendVault {
		paths = append(paths, config.Credential.CAFile, config.Credential.TokenFile)
	}
	for _, path := range paths {
		if !filepath.IsAbs(path) {
			return errors.New("interaction gateway runtime path is invalid")
		}
	}
	if config.Object.SessionTokenFile != "" && !filepath.IsAbs(config.Object.SessionTokenFile) {
		return errors.New("interaction gateway session token path is invalid")
	}
	urls := []string{
		gateway.ActionCallbackURL, gateway.DialogCallbackURL, gateway.ArtifactDownloadBaseURL,
		config.Mattermost.SiteURL, config.Object.Endpoint, config.Bot.URL,
	}
	if credentialSelection == credentialBackendVault {
		urls = append(urls, config.Credential.Address)
	}
	for _, raw := range urls {
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
			return errors.New("interaction gateway HTTPS URL is invalid")
		}
	}
	expectedPostgresUser := "interaction_gateway_runtime_g" + strconv.FormatUint(gateway.PostgresPrincipalGeneration, 10)
	if !strings.HasPrefix(gateway.MattermostClientSPIFFE, "spiffe://") ||
		!strings.HasPrefix(gateway.MaterializationClientSPIFFE, "spiffe://") ||
		gateway.TeamRPCClientSPIFFE != "spiffe://mattercodex.local/ns/mattercodex-system/sa/control-api-gateway" ||
		gateway.AuthorityVerifierUID == 0 || gateway.AuthorityVerifierGID == 0 ||
		len(gateway.ReadbackClientSPIFFEIDs) == 0 || len(gateway.ReadbackClientSPIFFEIDs) > 8 ||
		gateway.InstanceID == "" || gateway.RetentionRef == "" || gateway.EventIssuer == "" || gateway.EventAudience == "" ||
		gateway.ProviderReceiptIssuer != "https://interaction-gateway.mattercodex-system.svc.cluster.local/authority/provider-readback" ||
		gateway.ProviderReceiptTTL < 30*time.Second || gateway.ProviderReceiptTTL > 5*time.Minute ||
		gateway.PostgresPrincipalGeneration == 0 || gateway.PostgresExpectedUser != expectedPostgresUser ||
		gateway.PostgresMaxConnections < 2 || gateway.PostgresMaxConnections > 64 ||
		gateway.MaximumConnections < 16 || gateway.MaximumConnections > 1024 ||
		gateway.WorkerInterval < 50*time.Millisecond || gateway.OwnerGateInterval < 100*time.Millisecond ||
		gateway.TeamOperationLease <= gateway.OperationTimeout ||
		gateway.TeamSelectorTTL < time.Minute || gateway.TeamSelectorTTL > 24*time.Hour ||
		gateway.TeamRecoveryInterval < time.Second || gateway.TeamRecoveryInterval > time.Minute ||
		gateway.TeamRecoveryWindow <= gateway.TeamRecoveryInterval || gateway.TeamRecoveryWindow > time.Hour ||
		gateway.BotOperationLease <= gateway.OperationTimeout ||
		gateway.BotSelectorTTL < time.Minute || gateway.BotSelectorTTL > 24*time.Hour ||
		gateway.BotRecoveryInterval < time.Second || gateway.BotRecoveryInterval > time.Minute ||
		gateway.BotRecoveryWindow <= gateway.BotRecoveryInterval || gateway.BotRecoveryWindow > time.Hour ||
		gateway.ExpiryInterval < time.Second || gateway.ReadinessInterval < time.Second ||
		gateway.OperationTimeout < time.Second || gateway.OperationTimeout > time.Minute ||
		gateway.InboundLease <= gateway.OperationTimeout || gateway.DeliveryLease <= gateway.OperationTimeout ||
		gateway.StartupTimeout < 5*time.Second || gateway.StartupTimeout > 2*time.Minute ||
		gateway.ShutdownTimeout < time.Second || gateway.ShutdownTimeout > time.Minute {
		return errors.New("interaction gateway bounded configuration is invalid")
	}
	if config.Bot.Timeout < time.Second || config.Bot.Timeout > time.Minute {
		return errors.New("interaction gateway bot-service timeout is invalid")
	}
	if credentialSelection == credentialBackendVault && (config.Credential.Timeout < time.Second || config.Credential.Timeout > 30*time.Second ||
		config.Credential.Mount == "" || strings.Contains(config.Credential.Mount, "/") ||
		config.Credential.AuthMount == "" || strings.Contains(config.Credential.AuthMount, "/") ||
		config.Credential.Role == "" || strings.ContainsAny(config.Credential.Role, " /\r\n\x00") ||
		config.Credential.PathPrefix == "" || strings.HasPrefix(config.Credential.PathPrefix, "/") ||
		strings.Contains(config.Credential.PathPrefix, "..")) {
		return errors.New("interaction gateway bot credential configuration is invalid")
	}
	if credentialSelection == credentialBackendDirect && (config.DirectCredential.ResourceName != "interaction-gateway-bot-credentials" ||
		config.DirectCredential.DataKey != "state.json" || config.DirectCredential.Timeout < time.Second ||
		config.DirectCredential.Timeout > 10*time.Second) {
		return errors.New("interaction gateway direct bot credential registry is invalid")
	}
	return nil
}
