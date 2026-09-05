package app

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
)

// Mode выбирает назначение локального authority-компонента.
type Mode string

// Поддерживаемые режимы локального authority-компонента.
const (
	ModeIssuer   Mode = "issuer"
	ModeVerifier Mode = "verifier"
)

const socketRoot = "/run/kodex/internal-rpc-authority"

var (
	digestPattern      = regexp.MustCompile(`^[a-f0-9]{64}$`)
	runtimeUUIDPattern = regexp.MustCompile(
		`^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`,
	)
)

// Config задаёт полностью проверенную конфигурацию issuer или verifier.
type Config struct {
	Mode                              Mode
	ServiceName                       string
	SecretBackend                     string `env:"INTERNAL_RPC_AUTHORITY_SECRET_BACKEND"`
	WorkloadID                        string `env:"INTERNAL_RPC_AUTHORITY_WORKLOAD_ID"`
	SocketPath                        string
	ExpectedProcessUID                uint32
	ExpectedProcessGID                uint32
	ExpectedPeerUID                   uint32 `env:"INTERNAL_RPC_AUTHORITY_EXPECTED_PEER_UID"`
	ExpectedPeerGID                   uint32 `env:"INTERNAL_RPC_AUTHORITY_EXPECTED_PEER_GID"`
	SocketMode                        os.FileMode
	TechnicalListen                   string `env:"INTERNAL_RPC_AUTHORITY_TECHNICAL_LISTEN"`
	PostgresDSNFile                   string `env:"INTERNAL_RPC_AUTHORITY_POSTGRES_DSN_FILE"`
	PostgresTLSServerName             string `env:"INTERNAL_RPC_AUTHORITY_POSTGRES_TLS_SERVER_NAME"`
	PostgresExpectedSessionUser       string `env:"INTERNAL_RPC_AUTHORITY_POSTGRES_EXPECTED_SESSION_USER"`
	DatabaseCapabilityRole            string
	PostgresMaxConnections            int32  `env:"INTERNAL_RPC_AUTHORITY_POSTGRES_MAX_CONNECTIONS"`
	SnapshotJWSFile                   string `env:"INTERNAL_RPC_AUTHORITY_SNAPSHOT_JWS_FILE"`
	ManifestRootPublicJWKFile         string `env:"INTERNAL_RPC_AUTHORITY_MANIFEST_ROOT_PUBLIC_JWK_FILE"`
	ManifestRootMetadataFile          string `env:"INTERNAL_RPC_AUTHORITY_MANIFEST_ROOT_METADATA_FILE"`
	ManifestTrustBundleJWSFile        string `env:"INTERNAL_RPC_AUTHORITY_MANIFEST_TRUST_BUNDLE_JWS_FILE"`
	ContextPrivateJWKFile             string `env:"INTERNAL_RPC_AUTHORITY_CONTEXT_PRIVATE_JWK_FILE"`
	ProofTrustJWKFile                 string `env:"INTERNAL_RPC_AUTHORITY_PROOF_TRUST_JWK_FILE"`
	ReadbackCredentialSecret          string
	ReadbackPossessionSecret          string
	ResolverReadbackCredentialSecret  string
	ResolverReadbackPossessionSecret  string
	ResolverProofPrivateJWKFile       string `env:"INTERNAL_RPC_AUTHORITY_RESOLVER_PROOF_PRIVATE_JWK_FILE"`
	ResolverProofSignerGenerationFile string `env:"INTERNAL_RPC_AUTHORITY_RESOLVER_PROOF_SIGNER_GENERATION_FILE"`
	ResolverProofTrustJWKFile         string `env:"INTERNAL_RPC_AUTHORITY_RESOLVER_PROOF_TRUST_JWK_FILE"`
	ReadbackAttestorAddress           string `env:"INTERNAL_RPC_AUTHORITY_READBACK_ATTESTOR_ADDRESS"`
	ReadbackAttestorTLSServerName     string `env:"INTERNAL_RPC_AUTHORITY_READBACK_ATTESTOR_TLS_SERVER_NAME"`
	ReadbackAttestorCAFile            string `env:"INTERNAL_RPC_AUTHORITY_READBACK_ATTESTOR_CA_FILE"`
	WorkloadCertificateFile           string `env:"INTERNAL_RPC_AUTHORITY_WORKLOAD_CERTIFICATE_FILE"`
	WorkloadPrivateKeyFile            string `env:"INTERNAL_RPC_AUTHORITY_WORKLOAD_PRIVATE_KEY_FILE"`
	RestoreRoleCredentialSecret       string
	RestoreACKSecret                  string
	RestoreControllerAddress          string `env:"INTERNAL_RPC_AUTHORITY_RESTORE_CONTROLLER_ADDRESS"`
	RestoreControllerTLSServerName    string `env:"INTERNAL_RPC_AUTHORITY_RESTORE_CONTROLLER_TLS_SERVER_NAME"`
	RestoreControllerCAFile           string `env:"INTERNAL_RPC_AUTHORITY_RESTORE_CONTROLLER_CA_FILE"`
	RestoreControllerCertificateFile  string `env:"INTERNAL_RPC_AUTHORITY_RESTORE_CONTROLLER_CERTIFICATE_FILE"`
	RestoreRoleTrustJWSFile           string `env:"INTERNAL_RPC_AUTHORITY_RESTORE_ROLE_TRUST_JWS_FILE"`
	WorkloadSPIFFEID                  string `env:"INTERNAL_RPC_AUTHORITY_WORKLOAD_SPIFFE_ID"`
	ReadbackRole                      string
	WorkloadGeneration                uint64 `env:"INTERNAL_RPC_AUTHORITY_WORKLOAD_GENERATION"`
	CredentialGeneration              uint64 `env:"INTERNAL_RPC_AUTHORITY_CREDENTIAL_GENERATION"`
	PossessionKeyGeneration           uint64 `env:"INTERNAL_RPC_AUTHORITY_READBACK_POSSESSION_KEY_GENERATION"`
	ResolverCredentialGeneration      uint64 `env:"INTERNAL_RPC_AUTHORITY_RESOLVER_CREDENTIAL_GENERATION"`
	ResolverPossessionKeyGeneration   uint64 `env:"INTERNAL_RPC_AUTHORITY_RESOLVER_POSSESSION_KEY_GENERATION"`
	ResolverEnabled                   bool
	RestoreACKKeyGeneration           uint64        `env:"INTERNAL_RPC_AUTHORITY_RESTORE_ACK_KEY_GENERATION"`
	StartupTimeout                    time.Duration `env:"INTERNAL_RPC_AUTHORITY_STARTUP_TIMEOUT"`
	ReadinessTimeout                  time.Duration `env:"INTERNAL_RPC_AUTHORITY_READINESS_TIMEOUT"`
	ShutdownTimeout                   time.Duration `env:"INTERNAL_RPC_AUTHORITY_SHUTDOWN_TIMEOUT"`
	SnapshotReloadInterval            time.Duration `env:"INTERNAL_RPC_AUTHORITY_SNAPSHOT_RELOAD_INTERVAL"`
	ReplayCleanupInterval             time.Duration `env:"INTERNAL_RPC_AUTHORITY_REPLAY_CLEANUP_INTERVAL"`
	ReplayRetentionAfterExpiry        time.Duration
}

// LoadConfig читает типизированное окружение и проверяет конфигурацию режима.
func LoadConfig(mode Mode) (Config, error) {
	config := Config{
		Mode:                             mode,
		SecretBackend:                    string(secretBackendKubernetes),
		WorkloadID:                       "",
		ExpectedPeerUID:                  10001,
		ExpectedPeerGID:                  10001,
		SocketMode:                       0o660,
		TechnicalListen:                  ":9090",
		PostgresDSNFile:                  "/var/run/secrets/kodex/internal-rpc-authority/postgres/dsn",
		PostgresTLSServerName:            "internal-rpc-authority-postgresql-rw.kodex-system.svc.cluster.local",
		PostgresMaxConnections:           8,
		SnapshotJWSFile:                  "/var/run/config/kodex/internal-rpc-authority/snapshot/snapshot.jws",
		ManifestRootPublicJWKFile:        "/usr/local/share/internal-rpc-authority/manifest-root/bootstrap-public.jwk",
		ManifestRootMetadataFile:         "/usr/local/share/internal-rpc-authority/manifest-root/bootstrap-metadata.json",
		ManifestTrustBundleJWSFile:       "/var/run/config/kodex/internal-rpc-authority/manifest-trust/bundle.jws",
		ContextPrivateJWKFile:            "/var/run/secrets/kodex/internal-rpc-authority/issuer/private.jwk",
		ProofTrustJWKFile:                "/var/run/config/kodex/internal-rpc-authority/authority-proof-trust/jwks.json",
		ReadbackAttestorAddress:          "internal-rpc-authority-readback-attestor.kodex-system.svc:8443",
		ReadbackAttestorTLSServerName:    "internal-rpc-authority-readback-attestor.kodex-system.svc",
		ReadbackAttestorCAFile:           "/var/run/config/kodex/internal-rpc-authority/readback/attestor-ca.pem",
		WorkloadCertificateFile:          "/var/run/secrets/kodex/internal-rpc-authority/workload-tls/tls.crt",
		WorkloadPrivateKeyFile:           "/var/run/secrets/kodex/internal-rpc-authority/workload-tls/tls.key",
		RestoreControllerAddress:         "internal-rpc-authority-restore-controller.kodex-system.svc:8443",
		RestoreControllerTLSServerName:   "internal-rpc-authority-restore-controller.kodex-system.svc",
		RestoreControllerCAFile:          "/var/run/config/kodex/internal-rpc-authority/restore/controller-ca.pem",
		RestoreControllerCertificateFile: "/var/run/config/kodex/internal-rpc-authority/restore/controller-trust/tls.crt",
		RestoreRoleTrustJWSFile:          "/var/run/config/kodex/internal-rpc-authority/restore/role-trust/restore-role-trust.jws",
		WorkloadGeneration:               1,
		CredentialGeneration:             1,
		PossessionKeyGeneration:          1,
		RestoreACKKeyGeneration:          1,
		StartupTimeout:                   15 * time.Second,
		ReadinessTimeout:                 2 * time.Second,
		ShutdownTimeout:                  10 * time.Second,
		SnapshotReloadInterval:           5 * time.Second,
		ReplayCleanupInterval:            time.Minute,
		ReplayRetentionAfterExpiry:       10 * time.Minute,
	}
	switch mode {
	case ModeIssuer:
		config.ServiceName = "internal_rpc_authority_issuer"
		config.SocketPath = socketRoot + "/issuer.sock"
		config.ExpectedProcessUID = 29001
		config.ExpectedProcessGID = 29000
		config.DatabaseCapabilityRole = "internal_rpc_authority_issuer"
		config.ReadbackRole = "AUTHORIZATION_ISSUER"
		setRuntimeSecretNames(&config, "control-api-gateway-issuer")
	case ModeVerifier:
		config.ServiceName = "internal_rpc_authority_verifier"
		config.SocketPath = socketRoot + "/verifier.sock"
		config.ExpectedProcessUID = 29002
		config.ExpectedProcessGID = 29000
		config.DatabaseCapabilityRole = "internal_rpc_authority_verifier"
		config.ReadbackRole = "AUTHORIZATION_VERIFIER"
		setRuntimeSecretNames(&config, "control-plane-verifier")
		config.ResolverReadbackCredentialSecret = "internal-rpc-authority-control-plane-resolver-readback-credential"
		config.ResolverReadbackPossessionSecret = "internal-rpc-authority-control-plane-resolver-readback-possession"
		config.ResolverProofPrivateJWKFile = "/var/run/secrets/kodex/internal-rpc-authority/proof-signer/private.jwk"
		config.ResolverProofSignerGenerationFile = "/var/run/secrets/kodex/internal-rpc-authority/proof-signer/current_generation"
		config.ResolverProofTrustJWKFile = "/var/run/config/kodex/internal-rpc-authority/authority-proof-trust/jwks.json"
		config.ResolverCredentialGeneration = 1
		config.ResolverPossessionKeyGeneration = 1
		config.ResolverEnabled = true
	default:
		return Config{}, errors.New("unsupported internal-rpc-authority mode")
	}
	if err := parseEnvironment(&config); err != nil {
		return Config{}, err
	}
	if err := applyWorkloadProfile(&config); err != nil {
		return Config{}, err
	}
	if err := config.Validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}

func setRuntimeSecretNames(config *Config, prefix string) {
	base := "internal-rpc-authority-" + prefix
	config.ReadbackCredentialSecret = base + "-readback-credential"
	config.ReadbackPossessionSecret = base + "-readback-possession"
	config.RestoreRoleCredentialSecret = base + "-restore-credential"
	config.RestoreACKSecret = base + "-restore-ack"
}

// applyWorkloadProfile закрывает trusted deployment configuration до реестра
// известных workload. Имена Secret нельзя выбирать через окружение workload.
func applyWorkloadProfile(config *Config) error {
	if config == nil {
		return errors.New("authority workload profile is required")
	}
	type workloadProfile struct {
		spiffeID                    string
		readbackCredentialSecret    string
		readbackPossessionSecret    string
		restoreRoleCredentialSecret string
		restoreACKSecret            string
		resolverEnabled             bool
	}
	profiles := map[Mode]map[string]workloadProfile{
		ModeIssuer: {
			"email-bridge": {
				spiffeID:                    "spiffe://kodex.local/ns/kodex-system/sa/email-bridge",
				readbackCredentialSecret:    "internal-rpc-authority-email-bridge-issuer-readback-credential",
				readbackPossessionSecret:    "internal-rpc-authority-email-bridge-issuer-readback-possession",
				restoreRoleCredentialSecret: "internal-rpc-authority-email-bridge-issuer-restore-credential",
				restoreACKSecret:            "internal-rpc-authority-email-bridge-issuer-restore-ack",
			},
			"stt-tts-service": {
				spiffeID:                    "spiffe://kodex.local/ns/kodex-system/sa/stt-tts-service",
				readbackCredentialSecret:    "internal-rpc-authority-stt-tts-service-issuer-readback-credential",
				readbackPossessionSecret:    "internal-rpc-authority-stt-tts-service-issuer-readback-possession",
				restoreRoleCredentialSecret: "internal-rpc-authority-stt-tts-service-issuer-restore-credential",
				restoreACKSecret:            "internal-rpc-authority-stt-tts-service-issuer-restore-ack",
			},
			"control-plane": {
				spiffeID:                    "spiffe://kodex.local/ns/kodex-system/sa/control-plane",
				readbackCredentialSecret:    "internal-rpc-authority-control-plane-issuer-readback-credential",
				readbackPossessionSecret:    "internal-rpc-authority-control-plane-issuer-readback-possession",
				restoreRoleCredentialSecret: "internal-rpc-authority-control-plane-issuer-restore-credential",
				restoreACKSecret:            "internal-rpc-authority-control-plane-issuer-restore-ack",
			},
			"secret-broker": {
				spiffeID:                    "spiffe://kodex.local/ns/kodex-system/sa/secret-broker",
				readbackCredentialSecret:    "internal-rpc-authority-secret-broker-issuer-readback-credential",
				readbackPossessionSecret:    "internal-rpc-authority-secret-broker-issuer-readback-possession",
				restoreRoleCredentialSecret: "internal-rpc-authority-secret-broker-issuer-restore-credential",
				restoreACKSecret:            "internal-rpc-authority-secret-broker-issuer-restore-ack",
			},
			"role-image-builder": {
				spiffeID:                    "spiffe://kodex.local/ns/kodex-system/sa/role-image-builder",
				readbackCredentialSecret:    "internal-rpc-authority-role-image-builder-issuer-readback-credential",
				readbackPossessionSecret:    "internal-rpc-authority-role-image-builder-issuer-readback-possession",
				restoreRoleCredentialSecret: "internal-rpc-authority-role-image-builder-issuer-restore-credential",
				restoreACKSecret:            "internal-rpc-authority-role-image-builder-issuer-restore-ack",
			},
			"image-admission": {
				spiffeID:                    "spiffe://kodex.local/ns/kodex-system/sa/image-admission",
				readbackCredentialSecret:    "internal-rpc-authority-image-admission-issuer-readback-credential",
				readbackPossessionSecret:    "internal-rpc-authority-image-admission-issuer-readback-possession",
				restoreRoleCredentialSecret: "internal-rpc-authority-image-admission-issuer-restore-credential",
				restoreACKSecret:            "internal-rpc-authority-image-admission-issuer-restore-ack",
			},
			"image-promotion": {
				spiffeID:                    "spiffe://kodex.local/ns/kodex-system/sa/image-promotion",
				readbackCredentialSecret:    "internal-rpc-authority-image-promotion-issuer-readback-credential",
				readbackPossessionSecret:    "internal-rpc-authority-image-promotion-issuer-readback-possession",
				restoreRoleCredentialSecret: "internal-rpc-authority-image-promotion-issuer-restore-credential",
				restoreACKSecret:            "internal-rpc-authority-image-promotion-issuer-restore-ack",
			},
			"automation-scheduler": {
				spiffeID:                    "spiffe://kodex.local/ns/kodex-system/sa/automation-scheduler",
				readbackCredentialSecret:    "internal-rpc-authority-automation-scheduler-issuer-readback-credential",
				readbackPossessionSecret:    "internal-rpc-authority-automation-scheduler-issuer-readback-possession",
				restoreRoleCredentialSecret: "internal-rpc-authority-automation-scheduler-issuer-restore-credential",
				restoreACKSecret:            "internal-rpc-authority-automation-scheduler-issuer-restore-ack",
			},
			"session-archive": {
				spiffeID:                    "spiffe://kodex.local/ns/kodex-system/sa/session-archive",
				readbackCredentialSecret:    "internal-rpc-authority-session-archive-issuer-readback-credential",
				readbackPossessionSecret:    "internal-rpc-authority-session-archive-issuer-readback-possession",
				restoreRoleCredentialSecret: "internal-rpc-authority-session-archive-issuer-restore-credential",
				restoreACKSecret:            "internal-rpc-authority-session-archive-issuer-restore-ack",
			},
			"control-api-gateway": {
				spiffeID:                    "spiffe://kodex.local/ns/kodex-system/sa/control-api-gateway",
				readbackCredentialSecret:    "internal-rpc-authority-control-api-gateway-issuer-readback-credential",
				readbackPossessionSecret:    "internal-rpc-authority-control-api-gateway-issuer-readback-possession",
				restoreRoleCredentialSecret: "internal-rpc-authority-control-api-gateway-issuer-restore-credential",
				restoreACKSecret:            "internal-rpc-authority-control-api-gateway-issuer-restore-ack",
			},
			"integration-gateway": {
				spiffeID:                    "spiffe://kodex.local/ns/kodex-system/sa/integration-gateway",
				readbackCredentialSecret:    "internal-rpc-authority-integration-gateway-issuer-readback-credential",
				readbackPossessionSecret:    "internal-rpc-authority-integration-gateway-issuer-readback-possession",
				restoreRoleCredentialSecret: "internal-rpc-authority-integration-gateway-issuer-restore-credential",
				restoreACKSecret:            "internal-rpc-authority-integration-gateway-issuer-restore-ack",
			},
			"interaction-gateway": {
				spiffeID:                    "spiffe://kodex.local/ns/kodex-system/sa/interaction-gateway",
				readbackCredentialSecret:    "internal-rpc-authority-interaction-gateway-issuer-readback-credential",
				readbackPossessionSecret:    "internal-rpc-authority-interaction-gateway-issuer-readback-possession",
				restoreRoleCredentialSecret: "internal-rpc-authority-interaction-gateway-issuer-restore-credential",
				restoreACKSecret:            "internal-rpc-authority-interaction-gateway-issuer-restore-ack",
			},
			"runtime-controller": {
				spiffeID:                    "spiffe://kodex.local/ns/kodex-system/sa/runtime-controller",
				readbackCredentialSecret:    "internal-rpc-authority-runtime-controller-issuer-readback-credential",
				readbackPossessionSecret:    "internal-rpc-authority-runtime-controller-issuer-readback-possession",
				restoreRoleCredentialSecret: "internal-rpc-authority-runtime-controller-issuer-restore-credential",
				restoreACKSecret:            "internal-rpc-authority-runtime-controller-issuer-restore-ack",
			},
		},
		ModeVerifier: {
			"stt-tts-service": {
				spiffeID:                    "spiffe://kodex.local/ns/kodex-system/sa/stt-tts-service",
				readbackCredentialSecret:    "internal-rpc-authority-stt-tts-service-verifier-readback-credential",
				readbackPossessionSecret:    "internal-rpc-authority-stt-tts-service-verifier-readback-possession",
				restoreRoleCredentialSecret: "internal-rpc-authority-stt-tts-service-verifier-restore-credential",
				restoreACKSecret:            "internal-rpc-authority-stt-tts-service-verifier-restore-ack",
			},
			"secret-broker": {
				spiffeID:                    "spiffe://kodex.local/ns/kodex-system/sa/secret-broker",
				readbackCredentialSecret:    "internal-rpc-authority-secret-broker-verifier-readback-credential",
				readbackPossessionSecret:    "internal-rpc-authority-secret-broker-verifier-readback-possession",
				restoreRoleCredentialSecret: "internal-rpc-authority-secret-broker-verifier-restore-credential",
				restoreACKSecret:            "internal-rpc-authority-secret-broker-verifier-restore-ack",
			},
			"control-plane": {
				spiffeID:                    "spiffe://kodex.local/ns/kodex-system/sa/control-plane",
				readbackCredentialSecret:    "internal-rpc-authority-control-plane-verifier-readback-credential",
				readbackPossessionSecret:    "internal-rpc-authority-control-plane-verifier-readback-possession",
				restoreRoleCredentialSecret: "internal-rpc-authority-control-plane-verifier-restore-credential",
				restoreACKSecret:            "internal-rpc-authority-control-plane-verifier-restore-ack",
				resolverEnabled:             true,
			},
		},
	}
	profile, ok := profiles[config.Mode][config.WorkloadID]
	if !ok || config.WorkloadSPIFFEID != profile.spiffeID {
		return errors.New("authority workload profile is not registered")
	}
	if _, err := selectSecretBackend(config.SecretBackend); err != nil {
		return err
	}
	config.ReadbackCredentialSecret = profile.readbackCredentialSecret
	config.ReadbackPossessionSecret = profile.readbackPossessionSecret
	config.RestoreRoleCredentialSecret = profile.restoreRoleCredentialSecret
	config.RestoreACKSecret = profile.restoreACKSecret
	config.ResolverEnabled = profile.resolverEnabled
	return nil
}

// Validate проверяет точные пути, идентичности и ограниченные интервалы.
func (config Config) Validate() error {
	_, err := selectSecretBackend(config.SecretBackend)
	if err != nil {
		return err
	}
	if config.WorkloadID == "" || len(config.WorkloadID) > 96 {
		return errors.New("INTERNAL_RPC_AUTHORITY_WORKLOAD_ID is required and bounded")
	}
	if !strings.HasPrefix(
		config.WorkloadSPIFFEID,
		"spiffe://kodex.local/ns/kodex-system/sa/",
	) {
		return errors.New("exact authority workload SPIFFE ID is required")
	}
	if _, _, err := net.SplitHostPort(config.ReadbackAttestorAddress); err != nil ||
		config.ReadbackAttestorTLSServerName == "" ||
		net.ParseIP(config.ReadbackAttestorTLSServerName) != nil {
		return errors.New("readback attestor mTLS endpoint is invalid")
	}
	if _, _, err := net.SplitHostPort(config.RestoreControllerAddress); err != nil ||
		config.RestoreControllerTLSServerName == "" ||
		net.ParseIP(config.RestoreControllerTLSServerName) != nil {
		return errors.New("restore controller mTLS endpoint is invalid")
	}
	if config.ReadbackCredentialSecret == "" ||
		config.ReadbackPossessionSecret == "" ||
		config.RestoreRoleCredentialSecret == "" ||
		config.RestoreACKSecret == "" {
		return errors.New("exact Kubernetes Secret authority delivery boundary is required")
	}
	expectedSocket := map[Mode]string{
		ModeIssuer:   socketRoot + "/issuer.sock",
		ModeVerifier: socketRoot + "/verifier.sock",
	}[config.Mode]
	if config.SocketPath != expectedSocket || filepath.Dir(config.SocketPath) != socketRoot {
		return errors.New("authority UDS path differs from the capability registry")
	}
	if uint32(os.Getuid()) != config.ExpectedProcessUID ||
		uint32(os.Getgid()) != config.ExpectedProcessGID {
		return fmt.Errorf("process uid/gid does not match registered authority identity")
	}
	if config.ExpectedPeerUID == 0 || config.ExpectedPeerGID == 0 {
		return errors.New("root UDS peer is forbidden")
	}
	const maximumSafeInteger = uint64(9_007_199_254_740_991)
	if config.PostgresMaxConnections < 1 ||
		config.PostgresMaxConnections > 32 ||
		config.WorkloadGeneration == 0 ||
		config.WorkloadGeneration > maximumSafeInteger ||
		config.CredentialGeneration == 0 ||
		config.CredentialGeneration > maximumSafeInteger ||
		config.PossessionKeyGeneration == 0 ||
		config.PossessionKeyGeneration > maximumSafeInteger ||
		config.RestoreACKKeyGeneration == 0 ||
		config.RestoreACKKeyGeneration > maximumSafeInteger {
		return errors.New("authority generation or connection bound is invalid")
	}
	if config.Mode == ModeVerifier && config.ResolverEnabled &&
		(config.ResolverReadbackCredentialSecret == "" ||
			config.ResolverReadbackPossessionSecret == "" ||
			!filepath.IsAbs(config.ResolverProofPrivateJWKFile) ||
			!filepath.IsAbs(config.ResolverProofSignerGenerationFile) ||
			!filepath.IsAbs(config.ResolverProofTrustJWKFile) ||
			config.ResolverCredentialGeneration == 0 ||
			config.ResolverCredentialGeneration > maximumSafeInteger ||
			config.ResolverPossessionKeyGeneration == 0 ||
			config.ResolverPossessionKeyGeneration > maximumSafeInteger) {
		return errors.New("authority proof resolver readback boundary is invalid")
	}
	if config.StartupTimeout < time.Second ||
		config.StartupTimeout > time.Minute ||
		config.ReadinessTimeout < 100*time.Millisecond ||
		config.ReadinessTimeout > 5*time.Second ||
		config.ShutdownTimeout < time.Second ||
		config.ShutdownTimeout > 30*time.Second ||
		config.SnapshotReloadInterval < time.Second ||
		config.SnapshotReloadInterval > time.Minute ||
		config.ReplayCleanupInterval < 10*time.Second ||
		config.ReplayCleanupInterval > 10*time.Minute {
		return errors.New("authority duration is outside the allowed boundary")
	}
	if config.SocketMode != 0o660 {
		return errors.New("authority UDS mode must be 0660")
	}
	if config.PostgresTLSServerName == "" ||
		net.ParseIP(config.PostgresTLSServerName) != nil ||
		!strings.Contains(config.PostgresTLSServerName, ".") {
		return errors.New("exact PostgreSQL TLS server name is required")
	}
	if config.PostgresExpectedSessionUser == "" ||
		len(config.PostgresExpectedSessionUser) > 63 ||
		strings.ContainsAny(config.PostgresExpectedSessionUser, " \t\r\n\"';") {
		return errors.New("exact PostgreSQL session user is required")
	}
	if _, _, err := net.SplitHostPort(config.TechnicalListen); err != nil {
		return fmt.Errorf("invalid technical listen address: %w", err)
	}
	paths := map[string]string{
		"PostgresDSNFile":                  config.PostgresDSNFile,
		"SnapshotJWSFile":                  config.SnapshotJWSFile,
		"ManifestRootPublicJWKFile":        config.ManifestRootPublicJWKFile,
		"ManifestRootMetadataFile":         config.ManifestRootMetadataFile,
		"ManifestTrustBundleJWSFile":       config.ManifestTrustBundleJWSFile,
		"ReadbackAttestorCAFile":           config.ReadbackAttestorCAFile,
		"WorkloadCertificateFile":          config.WorkloadCertificateFile,
		"WorkloadPrivateKeyFile":           config.WorkloadPrivateKeyFile,
		"RestoreControllerCAFile":          config.RestoreControllerCAFile,
		"RestoreControllerCertificateFile": config.RestoreControllerCertificateFile,
		"RestoreRoleTrustJWSFile":          config.RestoreRoleTrustJWSFile,
	}
	for name, path := range paths {
		if !filepath.IsAbs(path) {
			return fmt.Errorf("%s must be an absolute path", name)
		}
	}
	if config.Mode == ModeIssuer {
		if !filepath.IsAbs(config.ContextPrivateJWKFile) ||
			!filepath.IsAbs(config.ProofTrustJWKFile) {
			return errors.New("issuer key and proof trust paths must be absolute")
		}
	}
	return nil
}

func parseEnvironment(target any) error {
	if err := env.Parse(target); err != nil {
		return errors.New("parse environment configuration")
	}
	return nil
}
