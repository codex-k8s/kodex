package app

import (
	"errors"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/authorityclient"
)

const (
	providerTarget                  = "dns:///secret-broker.kodex-system.svc:8443"
	providerTLSServerName           = "secret-broker.kodex-system.svc.cluster.local"
	providerAuthorityResolverTarget = "dns:///127.0.0.1:8443"
	providerAuthorityResolverSNI    = "control-plane.kodex-system.svc.cluster.local"
)

type Config struct {
	SkillScannerSocket              string        `env:"CONTROL_PLANE_SKILL_SCANNER_SOCKET"`
	SkillScannerTimeout             time.Duration `env:"CONTROL_PLANE_SKILL_SCANNER_TIMEOUT"`
	GRPCListen                      string        `env:"CONTROL_PLANE_GRPC_LISTEN"`
	TechnicalListen                 string        `env:"CONTROL_PLANE_TECHNICAL_LISTEN"`
	ServerCertificateFile           string        `env:"CONTROL_PLANE_TLS_CERTIFICATE_FILE"`
	ServerPrivateKeyFile            string        `env:"CONTROL_PLANE_TLS_PRIVATE_KEY_FILE"`
	ClientCAFile                    string        `env:"CONTROL_PLANE_TLS_CLIENT_CA_FILE"`
	PostgresDSNFile                 string        `env:"CONTROL_PLANE_POSTGRES_DSN_FILE"`
	PostgresCAFile                  string        `env:"CONTROL_PLANE_POSTGRES_CA_FILE"`
	PostgresTLSServerName           string        `env:"CONTROL_PLANE_POSTGRES_TLS_SERVER_NAME"`
	PostgresMaxConnections          int32         `env:"CONTROL_PLANE_POSTGRES_MAX_CONNECTIONS"`
	ObjectStorageEndpoint           string        `env:"CONTROL_PLANE_OBJECT_STORAGE_ENDPOINT"`
	ObjectStorageRegion             string        `env:"CONTROL_PLANE_OBJECT_STORAGE_REGION"`
	ObjectStorageBucket             string        `env:"CONTROL_PLANE_OBJECT_STORAGE_BUCKET"`
	ObjectStorageAccessKeyFile      string        `env:"CONTROL_PLANE_OBJECT_STORAGE_ACCESS_KEY_FILE"`
	ObjectStorageSecretKeyFile      string        `env:"CONTROL_PLANE_OBJECT_STORAGE_SECRET_KEY_FILE"`
	ObjectStorageUsePathStyle       bool          `env:"CONTROL_PLANE_OBJECT_STORAGE_USE_PATH_STYLE"`
	ObjectStorageAllowInsecureLocal bool          `env:"CONTROL_PLANE_OBJECT_STORAGE_ALLOW_INSECURE_LOCAL"`
	NATSURL                         string        `env:"CONTROL_PLANE_NATS_URL"`
	NATSTLSServerName               string        `env:"CONTROL_PLANE_NATS_TLS_SERVER_NAME"`
	NATSCAFile                      string        `env:"CONTROL_PLANE_NATS_CA_FILE"`
	NATSCertificateFile             string        `env:"CONTROL_PLANE_NATS_CERTIFICATE_FILE"`
	NATSPrivateKeyFile              string        `env:"CONTROL_PLANE_NATS_PRIVATE_KEY_FILE"`
	NATSCredentialsFile             string        `env:"CONTROL_PLANE_NATS_CREDENTIALS_FILE"`
	NATSStream                      string        `env:"CONTROL_PLANE_NATS_STREAM"`
	NATSReplicas                    int           `env:"CONTROL_PLANE_NATS_REPLICAS"`
	NATSMaxBytes                    int64         `env:"CONTROL_PLANE_NATS_MAX_BYTES"`
	InstanceID                      string        `env:"POD_UID"`
	DefaultRuntimeProvider          string        `env:"CONTROL_PLANE_DEFAULT_RUNTIME_PROVIDER"`
	DefaultRuntimeModel             string        `env:"CONTROL_PLANE_DEFAULT_RUNTIME_MODEL"`
	DefaultProviderSecretName       string        `env:"CONTROL_PLANE_DEFAULT_PROVIDER_SECRET_NAME"`
	DefaultProviderSecretUID        string        `env:"CONTROL_PLANE_DEFAULT_PROVIDER_SECRET_UID"`
	DefaultProviderSecretVersion    string        `env:"CONTROL_PLANE_DEFAULT_PROVIDER_SECRET_RESOURCE_VERSION"`
	DefaultProviderCredentialSHA256 string        `env:"CONTROL_PLANE_DEFAULT_PROVIDER_CREDENTIAL_SHA256"`
	IntegrationCredentialNamespace  string        `env:"CONTROL_PLANE_INTEGRATION_CREDENTIAL_NAMESPACE"`
	IntegrationCredentialSecretName string        `env:"CONTROL_PLANE_INTEGRATION_CREDENTIAL_SECRET_NAME"`
	RuntimeSecretNamespace          string        `env:"CONTROL_PLANE_RUNTIME_SECRET_NAMESPACE"`
	RuntimeSecretStagingNamespace   string        `env:"CONTROL_PLANE_RUNTIME_SECRET_STAGING_NAMESPACE"`
	KubernetesAPITimeout            time.Duration `env:"CONTROL_PLANE_KUBERNETES_API_TIMEOUT"`
	SecretBrokerTarget              string        `env:"CONTROL_PLANE_SECRET_BROKER_TARGET"`
	SecretBrokerTLSServerName       string        `env:"CONTROL_PLANE_SECRET_BROKER_TLS_SERVER_NAME"`
	ProviderResolverTarget          string        `env:"CONTROL_PLANE_PROVIDER_AUTHORITY_RESOLVER_TARGET"`
	ProviderResolverTLSServerName   string        `env:"CONTROL_PLANE_PROVIDER_AUTHORITY_RESOLVER_TLS_SERVER_NAME"`
	ProviderResolverCAFile          string        `env:"CONTROL_PLANE_PROVIDER_AUTHORITY_RESOLVER_CA_FILE"`
	ProviderApplicationGrantFile    string        `env:"CONTROL_PLANE_PROVIDER_APPLICATION_GRANT_FILE"`
	ProviderIssuerUID               uint32        `env:"CONTROL_PLANE_PROVIDER_AUTHORITY_ISSUER_UID"`
	ProviderIssuerGID               uint32        `env:"CONTROL_PLANE_PROVIDER_AUTHORITY_ISSUER_GID"`
	AuthorityVerifierSocket         string        `env:"CONTROL_PLANE_AUTHORITY_VERIFIER_SOCKET"`
	AuthorityVerifierUID            uint32        `env:"CONTROL_PLANE_AUTHORITY_VERIFIER_UID"`
	AuthorityVerifierGID            uint32        `env:"CONTROL_PLANE_AUTHORITY_VERIFIER_GID"`
	AuthorityPolicyFile             string        `env:"CONTROL_PLANE_AUTHORITY_POLICY_FILE"`
	ProofSignerFile                 string        `env:"CONTROL_PLANE_AUTHORITY_PROOF_SIGNER_FILE"`
	ProofSignerTrustFile            string        `env:"CONTROL_PLANE_AUTHORITY_PROOF_TRUST_FILE"`
	AutomationGrantTrustFile        string        `env:"CONTROL_PLANE_AUTOMATION_GRANT_TRUST_FILE"`
	SessionArchiveGrantTrustFile    string        `env:"CONTROL_PLANE_SESSION_ARCHIVE_GRANT_TRUST_FILE"`
	IntegrationGrantTrustFile       string        `env:"CONTROL_PLANE_INTEGRATION_GRANT_TRUST_FILE"`
	InteractionGrantTrustFile       string        `env:"CONTROL_PLANE_INTERACTION_GRANT_TRUST_FILE"`
	EmailGrantTrustFile             string        `env:"CONTROL_PLANE_EMAIL_GRANT_TRUST_FILE"`
	EmailConfigurationFile          string        `env:"CONTROL_PLANE_EMAIL_CONFIGURATION_FILE"`
	RuntimeGrantTrustFile           string        `env:"CONTROL_PLANE_RUNTIME_GRANT_TRUST_FILE"`
	RoleImageBuilderGrantTrustFile  string        `env:"CONTROL_PLANE_ROLE_IMAGE_BUILDER_GRANT_TRUST_FILE"`
	ImageAdmissionGrantTrustFile    string        `env:"CONTROL_PLANE_IMAGE_ADMISSION_GRANT_TRUST_FILE"`
	ImagePromotionGrantTrustFile    string        `env:"CONTROL_PLANE_IMAGE_PROMOTION_GRANT_TRUST_FILE"`
	SecretBrokerGrantTrustFile      string        `env:"CONTROL_PLANE_SECRET_BROKER_GRANT_TRUST_FILE"`
	ControlPlaneGrantTrustFile      string        `env:"CONTROL_PLANE_SELF_GRANT_TRUST_FILE"`
	LeaseSigningKeyFile             string        `env:"CONTROL_PLANE_LEASE_SIGNING_KEY_FILE"`
	ImagePolicyRevision             uint64        `env:"CONTROL_PLANE_IMAGE_POLICY_REVISION"`
	ImagePolicySHA256               string        `env:"CONTROL_PLANE_IMAGE_POLICY_SHA256"`
	ImageBuildLeaseDuration         time.Duration `env:"CONTROL_PLANE_IMAGE_BUILD_LEASE_DURATION"`
	ImageAdmissionClaimTTL          time.Duration `env:"CONTROL_PLANE_IMAGE_ADMISSION_CLAIM_TTL"`
	ImagePromotionClaimTTL          time.Duration `env:"CONTROL_PLANE_IMAGE_PROMOTION_CLAIM_TTL"`
	ImageMaximumAttempts            uint32        `env:"CONTROL_PLANE_IMAGE_MAXIMUM_ATTEMPTS"`
	StagingImageRepository          string        `env:"CONTROL_PLANE_STAGING_IMAGE_REPOSITORY"`
	PromotedImageRepository         string        `env:"CONTROL_PLANE_PROMOTED_IMAGE_REPOSITORY"`
	RoleImageInputRepository        string        `env:"CONTROL_PLANE_ROLE_IMAGE_INPUT_REPOSITORY"`
	RoleEnvironmentCatalogFile      string        `env:"CONTROL_PLANE_ROLE_ENVIRONMENT_CATALOG_FILE"`
	RoleImageBuilderSHA256          string        `env:"CONTROL_PLANE_ROLE_IMAGE_BUILDER_SHA256"`
	RoleImageFrontendSHA256         string        `env:"CONTROL_PLANE_ROLE_IMAGE_FRONTEND_SHA256"`
	RoleImageToolchainSHA256        string        `env:"CONTROL_PLANE_ROLE_IMAGE_TOOLCHAIN_SHA256"`
	TrustedRoleBaseRepository       string        `env:"CONTROL_PLANE_TRUSTED_ROLE_BASE_REPOSITORY"`
	TrustedRoleBaseDigest           string        `env:"CONTROL_PLANE_TRUSTED_ROLE_BASE_DIGEST"`
	DefaultRoleImageReference       string        `env:"CONTROL_PLANE_DEFAULT_ROLE_IMAGE_REFERENCE"`
	RoleRuntimeContractRevision     uint64        `env:"CONTROL_PLANE_ROLE_RUNTIME_CONTRACT_REVISION"`
	RoleRuntimeContractSHA256       string        `env:"CONTROL_PLANE_ROLE_RUNTIME_CONTRACT_SHA256"`
	OIDCIssuer                      string        `env:"CONTROL_PLANE_OIDC_ISSUER"`
	OIDCAudience                    string        `env:"CONTROL_PLANE_OIDC_AUDIENCE"`
	OIDCJWKSURL                     string        `env:"CONTROL_PLANE_OIDC_JWKS_URL"`
	OIDCConnectAddress              string        `env:"CONTROL_PLANE_OIDC_CONNECT_ADDRESS"`
	OIDCTLSServerName               string        `env:"CONTROL_PLANE_OIDC_TLS_SERVER_NAME"`
	OIDCCAFile                      string        `env:"CONTROL_PLANE_OIDC_CA_FILE"`
	OIDCRefreshInterval             time.Duration `env:"CONTROL_PLANE_OIDC_REFRESH_INTERVAL"`
	StartupTimeout                  time.Duration `env:"CONTROL_PLANE_STARTUP_TIMEOUT"`
	ReadinessTimeout                time.Duration `env:"CONTROL_PLANE_READINESS_TIMEOUT"`
	ReadinessInterval               time.Duration `env:"CONTROL_PLANE_READINESS_INTERVAL"`
	ShutdownTimeout                 time.Duration `env:"CONTROL_PLANE_SHUTDOWN_TIMEOUT"`
	RelayPollInterval               time.Duration `env:"CONTROL_PLANE_RELAY_POLL_INTERVAL"`
	RelayLeaseDuration              time.Duration `env:"CONTROL_PLANE_RELAY_LEASE_DURATION"`
	RelayPublishTimeout             time.Duration `env:"CONTROL_PLANE_RELAY_PUBLISH_TIMEOUT"`
	RelayFinalizeTimeout            time.Duration `env:"CONTROL_PLANE_RELAY_FINALIZE_TIMEOUT"`
	ProviderCleanupBatchSize        int32         `env:"CONTROL_PLANE_PROVIDER_CREDENTIAL_CLEANUP_BATCH_SIZE"`
	ProviderCleanupPollInterval     time.Duration `env:"CONTROL_PLANE_PROVIDER_CREDENTIAL_CLEANUP_POLL_INTERVAL"`
	ProviderCleanupTimeout          time.Duration `env:"CONTROL_PLANE_PROVIDER_CREDENTIAL_CLEANUP_OPERATION_TIMEOUT"`
}

func loadConfig() (Config, error) {
	config := Config{
		SkillScannerSocket:  "/run/kodex-skill-scanner/clamd.sock",
		SkillScannerTimeout: 15 * time.Second,
		GRPCListen:          ":8443", TechnicalListen: ":9090",
		ServerCertificateFile:           "/var/run/secrets/kodex/control-plane/workload-tls/tls.crt",
		ServerPrivateKeyFile:            "/var/run/secrets/kodex/control-plane/workload-tls/tls.key",
		ClientCAFile:                    "/var/run/config/kodex/control-plane/internal-ca/ca.pem",
		PostgresDSNFile:                 "/var/run/secrets/kodex/control-plane/postgres-runtime/dsn",
		PostgresCAFile:                  "/var/run/config/kodex/control-plane/postgres/ca.pem",
		PostgresTLSServerName:           "control-plane-postgresql-rw.kodex-system.svc.cluster.local",
		PostgresMaxConnections:          16,
		ObjectStorageRegion:             "us-east-1",
		ObjectStorageBucket:             "kodex-artifacts",
		ObjectStorageAccessKeyFile:      "/var/run/secrets/kodex/control-plane/object-storage/access-key-id",
		ObjectStorageSecretKeyFile:      "/var/run/secrets/kodex/control-plane/object-storage/secret-access-key",
		ObjectStorageUsePathStyle:       true,
		NATSURL:                         "tls://nats.kodex-system.svc:4222",
		NATSTLSServerName:               "nats.kodex-system.svc.cluster.local",
		NATSCAFile:                      "/var/run/config/kodex/control-plane/nats/ca.pem",
		NATSCertificateFile:             "/var/run/secrets/kodex/control-plane/nats-client/tls.crt",
		NATSPrivateKeyFile:              "/var/run/secrets/kodex/control-plane/nats-client/tls.key",
		NATSCredentialsFile:             "/var/run/secrets/kodex/control-plane/nats/user.creds",
		NATSStream:                      "CONTROL_PLANE",
		NATSReplicas:                    3,
		NATSMaxBytes:                    32 << 30,
		DefaultRuntimeProvider:          "openai-codex",
		DefaultRuntimeModel:             "gpt-5.6-sol",
		IntegrationCredentialNamespace:  "kodex-system",
		IntegrationCredentialSecretName: "kodex-integration-credentials",
		RuntimeSecretNamespace:          "kodex-runtime",
		RuntimeSecretStagingNamespace:   "kodex-secret-drafts",
		KubernetesAPITimeout:            3 * time.Second,
		SecretBrokerTarget:              providerTarget,
		SecretBrokerTLSServerName:       providerTLSServerName,
		ProviderResolverTarget:          providerAuthorityResolverTarget,
		ProviderResolverTLSServerName:   providerAuthorityResolverSNI,
		ProviderResolverCAFile:          "/var/run/config/kodex/control-plane/internal-ca/ca.pem",
		ProviderApplicationGrantFile:    "/var/run/secrets/kodex/control-plane/application-grant/application-grant.jws",
		ProviderIssuerUID:               29001,
		ProviderIssuerGID:               29000,
		AuthorityVerifierSocket:         authorityclient.VerifierSocketPath,
		AuthorityVerifierUID:            29002, AuthorityVerifierGID: 29000,
		AuthorityPolicyFile:            "/var/run/config/kodex/control-plane/authority/policy.json",
		ProofSignerFile:                "/var/run/secrets/kodex/internal-rpc-authority/proof-signer/private.jwk",
		ProofSignerTrustFile:           "/var/run/config/kodex/internal-rpc-authority/authority-proof-trust/jwks.json",
		AutomationGrantTrustFile:       "/var/run/config/kodex/control-plane/application-grants/automation-scheduler.platform-worker.public.jwk",
		SessionArchiveGrantTrustFile:   "/var/run/config/kodex/control-plane/application-grants/session-archive.platform-worker.public.jwk",
		IntegrationGrantTrustFile:      "/var/run/config/kodex/control-plane/application-grants/integration-gateway.platform-worker.public.jwk",
		InteractionGrantTrustFile:      "",
		RuntimeGrantTrustFile:          "/var/run/config/kodex/control-plane/application-grants/runtime-controller.platform-worker.public.jwk",
		RoleImageBuilderGrantTrustFile: "/var/run/config/kodex/control-plane/application-grants/role-image-builder.platform-worker.public.jwk",
		ImageAdmissionGrantTrustFile:   "/var/run/config/kodex/control-plane/application-grants/image-admission.platform-worker.public.jwk",
		ImagePromotionGrantTrustFile:   "/var/run/config/kodex/control-plane/application-grants/image-promotion.platform-worker.public.jwk",
		SecretBrokerGrantTrustFile:     "/var/run/config/kodex/control-plane/application-grants/secret-broker.platform-worker.public.jwk",
		ControlPlaneGrantTrustFile:     "/var/run/config/kodex/control-plane/application-grants/control-plane.platform-worker.public.jwk",
		LeaseSigningKeyFile:            "/var/run/secrets/kodex/control-plane/lease-signing/key",
		ImageBuildLeaseDuration:        5 * time.Minute,
		ImageAdmissionClaimTTL:         30 * time.Minute,
		ImagePromotionClaimTTL:         15 * time.Minute,
		ImageMaximumAttempts:           3,
		RoleEnvironmentCatalogFile:     "/var/run/config/kodex/control-plane/role-environments/catalog.json",
		OIDCAudience:                   "kodex-control-api", OIDCCAFile: "/var/run/config/kodex/control-plane/oidc/ca.pem",
		OIDCRefreshInterval: 30 * time.Second,
		StartupTimeout:      20 * time.Second, ReadinessTimeout: 2 * time.Second,
		ReadinessInterval: 2 * time.Second, ShutdownTimeout: 10 * time.Second,
		RelayPollInterval: 250 * time.Millisecond, RelayLeaseDuration: 10 * time.Second,
		RelayPublishTimeout: 2 * time.Second, RelayFinalizeTimeout: 2 * time.Second,
		ProviderCleanupBatchSize:    16,
		ProviderCleanupPollInterval: 250 * time.Millisecond,
		ProviderCleanupTimeout:      10 * time.Second,
	}
	if err := env.Parse(&config); err != nil {
		return Config{}, errors.New("parse control-plane environment")
	}
	if err := config.validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}

func (config Config) validate() error {
	if !filepath.IsAbs(config.SkillScannerSocket) || filepath.Clean(config.SkillScannerSocket) != config.SkillScannerSocket || strings.ContainsAny(config.SkillScannerSocket, "\x00\n\r") || config.SkillScannerTimeout < time.Second || config.SkillScannerTimeout > time.Minute {
		return errors.New("control-plane skill scanner configuration is invalid")
	}
	for _, address := range []string{config.GRPCListen, config.TechnicalListen} {
		if _, _, err := net.SplitHostPort(address); err != nil {
			return errors.New("control-plane listen address is invalid")
		}
	}
	for _, path := range []string{config.ServerCertificateFile, config.ServerPrivateKeyFile, config.ClientCAFile, config.PostgresDSNFile, config.PostgresCAFile, config.ObjectStorageAccessKeyFile, config.ObjectStorageSecretKeyFile, config.NATSCAFile, config.NATSCertificateFile, config.NATSPrivateKeyFile, config.NATSCredentialsFile, config.ProviderResolverCAFile, config.ProviderApplicationGrantFile, config.AuthorityVerifierSocket, config.AuthorityPolicyFile, config.ProofSignerFile, config.ProofSignerTrustFile, config.AutomationGrantTrustFile, config.SessionArchiveGrantTrustFile, config.IntegrationGrantTrustFile, config.RuntimeGrantTrustFile, config.RoleImageBuilderGrantTrustFile, config.ImageAdmissionGrantTrustFile, config.ImagePromotionGrantTrustFile, config.SecretBrokerGrantTrustFile, config.ControlPlaneGrantTrustFile, config.LeaseSigningKeyFile, config.RoleEnvironmentCatalogFile, config.OIDCCAFile} {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return errors.New("control-plane file path is invalid")
		}
	}
	if config.InteractionGrantTrustFile != "" && (!filepath.IsAbs(config.InteractionGrantTrustFile) || filepath.Clean(config.InteractionGrantTrustFile) != config.InteractionGrantTrustFile) {
		return errors.New("control-plane interaction grant trust path is invalid")
	}
	if config.EmailGrantTrustFile != "" && (!filepath.IsAbs(config.EmailGrantTrustFile) || filepath.Clean(config.EmailGrantTrustFile) != config.EmailGrantTrustFile) {
		return errors.New("control-plane email grant trust path is invalid")
	}
	if config.EmailConfigurationFile != "" && (!filepath.IsAbs(config.EmailConfigurationFile) || filepath.Clean(config.EmailConfigurationFile) != config.EmailConfigurationFile) {
		return errors.New("control-plane email configuration path is invalid")
	}
	if config.PostgresTLSServerName == "" || net.ParseIP(config.PostgresTLSServerName) != nil ||
		config.NATSTLSServerName == "" || net.ParseIP(config.NATSTLSServerName) != nil || config.NATSURL == "" ||
		config.PostgresMaxConnections < 2 || config.PostgresMaxConnections > 64 ||
		config.NATSStream != "CONTROL_PLANE" || config.NATSReplicas < 1 || config.NATSReplicas > 5 || config.NATSMaxBytes < 256<<20 ||
		config.InstanceID == "" || len(config.InstanceID) > 128 ||
		config.DefaultRuntimeProvider != "openai-codex" || !validRuntimeIdentifier(config.DefaultRuntimeModel) ||
		!validDNSLabel(config.DefaultProviderSecretName) || !validUUID(config.DefaultProviderSecretUID) ||
		config.DefaultProviderSecretVersion == "" || len(config.DefaultProviderSecretVersion) > 128 ||
		!validSHA256(config.DefaultProviderCredentialSHA256) ||
		config.IntegrationCredentialNamespace != "kodex-system" || config.IntegrationCredentialSecretName != "kodex-integration-credentials" ||
		!validDNSLabel(config.RuntimeSecretNamespace) ||
		!validDNSLabel(config.RuntimeSecretStagingNamespace) || config.RuntimeSecretNamespace == config.RuntimeSecretStagingNamespace ||
		config.KubernetesAPITimeout < 500*time.Millisecond || config.KubernetesAPITimeout > 10*time.Second ||
		!validProviderCredentialBoundary(config) ||
		config.ProviderIssuerUID == 0 || config.ProviderIssuerGID == 0 ||
		config.ImagePolicyRevision == 0 || !validSHA256(config.ImagePolicySHA256) ||
		config.ImageBuildLeaseDuration < 30*time.Second || config.ImageBuildLeaseDuration > 30*time.Minute ||
		config.ImageAdmissionClaimTTL < time.Minute || config.ImageAdmissionClaimTTL > time.Hour ||
		config.ImagePromotionClaimTTL < time.Minute || config.ImagePromotionClaimTTL > time.Hour ||
		config.ImageMaximumAttempts < 1 || config.ImageMaximumAttempts > 10 ||
		!validImageRepository(config.StagingImageRepository) || !validImageRepository(config.PromotedImageRepository) ||
		!validImageRepository(config.RoleImageInputRepository) || !validImageRepository(config.TrustedRoleBaseRepository) ||
		!validManifestDigest(config.TrustedRoleBaseDigest) || !validPinnedImageReference(config.DefaultRoleImageReference) ||
		!validSHA256(config.RoleImageBuilderSHA256) || !validSHA256(config.RoleImageFrontendSHA256) ||
		!validSHA256(config.RoleImageToolchainSHA256) ||
		config.RoleRuntimeContractRevision == 0 || !validSHA256(config.RoleRuntimeContractSHA256) ||
		config.AuthorityVerifierUID == 0 || config.AuthorityVerifierGID == 0 ||
		config.OIDCAudience != "kodex-control-api" || config.OIDCTLSServerName == "" || net.ParseIP(config.OIDCTLSServerName) != nil ||
		config.OIDCRefreshInterval < 10*time.Second || config.OIDCRefreshInterval > time.Minute ||
		config.StartupTimeout < time.Second || config.StartupTimeout > time.Minute ||
		config.ReadinessTimeout < 100*time.Millisecond || config.ReadinessTimeout > 10*time.Second ||
		config.ReadinessInterval < 500*time.Millisecond || config.ReadinessInterval > time.Minute ||
		config.ShutdownTimeout < time.Second || config.ShutdownTimeout > time.Minute ||
		config.RelayPollInterval < 50*time.Millisecond || config.RelayLeaseDuration < time.Second ||
		config.RelayPublishTimeout <= 0 || config.RelayFinalizeTimeout <= 0 || config.RelayPublishTimeout+config.RelayFinalizeTimeout >= config.RelayLeaseDuration ||
		!validProviderCredentialCleanupConfig(config) {
		return errors.New("control-plane bounded configuration is invalid")
	}
	issuer, issuerErr := url.Parse(config.OIDCIssuer)
	jwks, jwksErr := url.Parse(config.OIDCJWKSURL)
	connectHost, connectPort, connectErr := net.SplitHostPort(config.OIDCConnectAddress)
	if issuerErr != nil || issuer.Scheme != "https" || issuer.Hostname() != config.OIDCTLSServerName || issuer.User != nil || issuer.RawQuery != "" || issuer.Fragment != "" ||
		jwksErr != nil || jwks.Scheme != "https" || jwks.Hostname() != issuer.Hostname() || jwks.User != nil || jwks.RawQuery != "" || jwks.Fragment != "" || jwks.Path == "" ||
		connectErr != nil || connectHost == "" || net.ParseIP(connectHost) != nil || connectPort != "443" {
		return errors.New("control-plane OIDC boundary is invalid")
	}
	if !validObjectStorageBoundary(config) {
		return errors.New("control-plane object storage boundary is invalid")
	}
	if info, err := os.Lstat(filepath.Dir(config.AuthorityVerifierSocket)); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("control-plane authority socket directory is invalid")
	}
	return nil
}

func validProviderCredentialCleanupConfig(config Config) bool {
	return config.ProviderCleanupBatchSize >= 1 && config.ProviderCleanupBatchSize <= 16 &&
		config.ProviderCleanupPollInterval >= 50*time.Millisecond &&
		config.ProviderCleanupPollInterval <= time.Minute &&
		config.ProviderCleanupTimeout >= 100*time.Millisecond &&
		config.ProviderCleanupTimeout <= 30*time.Second
}

func validProviderCredentialBoundary(config Config) bool {
	return config.SecretBrokerTarget == providerTarget &&
		config.SecretBrokerTLSServerName == providerTLSServerName &&
		config.ProviderResolverTarget == providerAuthorityResolverTarget &&
		config.ProviderResolverTLSServerName == providerAuthorityResolverSNI &&
		config.ProviderResolverCAFile == config.ClientCAFile &&
		config.SecretBrokerTarget != config.ProviderResolverTarget &&
		config.SecretBrokerTLSServerName != config.ProviderResolverTLSServerName
}

func validObjectStorageBoundary(config Config) bool {
	endpoint, err := url.Parse(config.ObjectStorageEndpoint)
	if err != nil || endpoint == nil {
		return false
	}
	localInsecure := config.ObjectStorageAllowInsecureLocal && endpoint.Scheme == "http" &&
		endpoint.Hostname() == "seaweedfs-s3.kodex-system.svc.cluster.local" &&
		endpoint.Port() == "8333"
	return (endpoint.Scheme == "https" || localInsecure) && endpoint.Host != "" &&
		endpoint.User == nil && endpoint.RawQuery == "" && endpoint.Fragment == "" &&
		(endpoint.Path == "" || endpoint.Path == "/") &&
		validRuntimeIdentifier(config.ObjectStorageRegion) && validDNSLabel(config.ObjectStorageBucket)
}

func validDNSLabel(value string) bool {
	if value == "" || len(value) > 63 {
		return false
	}
	for index, character := range value {
		valid := character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '-'
		if !valid || character == '-' && (index == 0 || index == len(value)-1) {
			return false
		}
	}
	return true
}

func validUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for index, character := range value {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			if character != '-' {
				return false
			}
			continue
		}
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func validSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func validManifestDigest(value string) bool {
	return len(value) == 71 && value[:7] == "sha256:" && validSHA256(value[7:])
}

func validImageRepository(value string) bool {
	return value != "" && len(value) <= 500 && !strings.ContainsAny(value, "@ \t\r\n") && strings.Contains(value, "/")
}

func validPinnedImageReference(value string) bool {
	separator := strings.LastIndex(value, "@")
	return separator > 0 && validImageRepository(value[:separator]) && validManifestDigest(value[separator+1:])
}

func validRuntimeIdentifier(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') && (character < '0' || character > '9') && character != '.' && character != '_' && character != '-' {
			return false
		}
	}
	return true
}
