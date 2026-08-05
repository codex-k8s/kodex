package app

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
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

const socketRoot = "/run/mattercodex/internal-rpc-authority"

// Config задаёт полностью проверенную конфигурацию issuer или verifier.
type Config struct {
	Mode                             Mode
	ServiceName                      string
	WorkloadID                       string `env:"INTERNAL_RPC_AUTHORITY_WORKLOAD_ID"`
	SocketPath                       string
	ExpectedProcessUID               uint32
	ExpectedProcessGID               uint32
	ExpectedPeerUID                  uint32 `env:"INTERNAL_RPC_AUTHORITY_EXPECTED_PEER_UID"`
	ExpectedPeerGID                  uint32 `env:"INTERNAL_RPC_AUTHORITY_EXPECTED_PEER_GID"`
	SocketMode                       os.FileMode
	TechnicalListen                  string `env:"INTERNAL_RPC_AUTHORITY_TECHNICAL_LISTEN"`
	PostgresDSNFile                  string `env:"INTERNAL_RPC_AUTHORITY_POSTGRES_DSN_FILE"`
	PostgresTLSServerName            string `env:"INTERNAL_RPC_AUTHORITY_POSTGRES_TLS_SERVER_NAME"`
	PostgresExpectedSessionUser      string `env:"INTERNAL_RPC_AUTHORITY_POSTGRES_EXPECTED_SESSION_USER"`
	DatabaseCapabilityRole           string
	PostgresMaxConnections           int32  `env:"INTERNAL_RPC_AUTHORITY_POSTGRES_MAX_CONNECTIONS"`
	SnapshotJWSFile                  string `env:"INTERNAL_RPC_AUTHORITY_SNAPSHOT_JWS_FILE"`
	ManifestRootPublicJWKFile        string `env:"INTERNAL_RPC_AUTHORITY_MANIFEST_ROOT_PUBLIC_JWK_FILE"`
	ManifestRootMetadataFile         string `env:"INTERNAL_RPC_AUTHORITY_MANIFEST_ROOT_METADATA_FILE"`
	ManifestTrustBundleJWSFile       string `env:"INTERNAL_RPC_AUTHORITY_MANIFEST_TRUST_BUNDLE_JWS_FILE"`
	ContextPrivateJWKFile            string `env:"INTERNAL_RPC_AUTHORITY_CONTEXT_PRIVATE_JWK_FILE"`
	ProofTrustJWKFile                string `env:"INTERNAL_RPC_AUTHORITY_PROOF_TRUST_JWK_FILE"`
	VaultAddress                     string `env:"INTERNAL_RPC_AUTHORITY_VAULT_ADDRESS"`
	VaultTLSServerName               string `env:"INTERNAL_RPC_AUTHORITY_VAULT_TLS_SERVER_NAME"`
	VaultCAFile                      string `env:"INTERNAL_RPC_AUTHORITY_VAULT_CA_FILE"`
	VaultAuthRole                    string `env:"INTERNAL_RPC_AUTHORITY_VAULT_AUTH_ROLE"`
	VaultAuthFile                    string `env:"INTERNAL_RPC_AUTHORITY_VAULT_AUTH_FILE"`
	ReadbackCredentialVaultPath      string
	ReadbackPossessionVaultPath      string
	ResolverReadbackCredentialPath   string `env:"INTERNAL_RPC_AUTHORITY_RESOLVER_READBACK_CREDENTIAL_VAULT_PATH"`
	ResolverReadbackPossessionPath   string `env:"INTERNAL_RPC_AUTHORITY_RESOLVER_READBACK_POSSESSION_VAULT_PATH"`
	ResolverProofPrivateJWKFile      string `env:"INTERNAL_RPC_AUTHORITY_RESOLVER_PROOF_PRIVATE_JWK_FILE"`
	ResolverProofTrustJWKFile        string `env:"INTERNAL_RPC_AUTHORITY_RESOLVER_PROOF_TRUST_JWK_FILE"`
	ReadbackAttestorAddress          string `env:"INTERNAL_RPC_AUTHORITY_READBACK_ATTESTOR_ADDRESS"`
	ReadbackAttestorTLSServerName    string `env:"INTERNAL_RPC_AUTHORITY_READBACK_ATTESTOR_TLS_SERVER_NAME"`
	ReadbackAttestorCAFile           string `env:"INTERNAL_RPC_AUTHORITY_READBACK_ATTESTOR_CA_FILE"`
	WorkloadCertificateFile          string `env:"INTERNAL_RPC_AUTHORITY_WORKLOAD_CERTIFICATE_FILE"`
	WorkloadPrivateKeyFile           string `env:"INTERNAL_RPC_AUTHORITY_WORKLOAD_PRIVATE_KEY_FILE"`
	RestoreRoleCredentialVaultPath   string
	RestoreACKVaultPath              string
	RestoreControllerAddress         string `env:"INTERNAL_RPC_AUTHORITY_RESTORE_CONTROLLER_ADDRESS"`
	RestoreControllerTLSServerName   string `env:"INTERNAL_RPC_AUTHORITY_RESTORE_CONTROLLER_TLS_SERVER_NAME"`
	RestoreControllerCAFile          string `env:"INTERNAL_RPC_AUTHORITY_RESTORE_CONTROLLER_CA_FILE"`
	RestoreControllerCertificateFile string `env:"INTERNAL_RPC_AUTHORITY_RESTORE_CONTROLLER_CERTIFICATE_FILE"`
	RestoreRoleTrustJWSFile          string `env:"INTERNAL_RPC_AUTHORITY_RESTORE_ROLE_TRUST_JWS_FILE"`
	WorkloadSPIFFEID                 string `env:"INTERNAL_RPC_AUTHORITY_WORKLOAD_SPIFFE_ID"`
	ReadbackRole                     string
	WorkloadGeneration               uint64        `env:"INTERNAL_RPC_AUTHORITY_WORKLOAD_GENERATION"`
	CredentialGeneration             uint64        `env:"INTERNAL_RPC_AUTHORITY_CREDENTIAL_GENERATION"`
	PossessionKeyGeneration          uint64        `env:"INTERNAL_RPC_AUTHORITY_READBACK_POSSESSION_KEY_GENERATION"`
	ResolverCredentialGeneration     uint64        `env:"INTERNAL_RPC_AUTHORITY_RESOLVER_CREDENTIAL_GENERATION"`
	ResolverPossessionKeyGeneration  uint64        `env:"INTERNAL_RPC_AUTHORITY_RESOLVER_POSSESSION_KEY_GENERATION"`
	ResolverProofSignerGeneration    uint64        `env:"INTERNAL_RPC_AUTHORITY_RESOLVER_PROOF_SIGNER_GENERATION"`
	RestoreACKKeyGeneration          uint64        `env:"INTERNAL_RPC_AUTHORITY_RESTORE_ACK_KEY_GENERATION"`
	StartupTimeout                   time.Duration `env:"INTERNAL_RPC_AUTHORITY_STARTUP_TIMEOUT"`
	ReadinessTimeout                 time.Duration `env:"INTERNAL_RPC_AUTHORITY_READINESS_TIMEOUT"`
	ShutdownTimeout                  time.Duration `env:"INTERNAL_RPC_AUTHORITY_SHUTDOWN_TIMEOUT"`
	SnapshotReloadInterval           time.Duration `env:"INTERNAL_RPC_AUTHORITY_SNAPSHOT_RELOAD_INTERVAL"`
	ReplayCleanupInterval            time.Duration `env:"INTERNAL_RPC_AUTHORITY_REPLAY_CLEANUP_INTERVAL"`
	ReplayRetentionAfterExpiry       time.Duration
}

// LoadConfig читает типизированное окружение и проверяет конфигурацию режима.
func LoadConfig(mode Mode) (Config, error) {
	config := Config{
		Mode:                             mode,
		WorkloadID:                       "",
		ExpectedPeerUID:                  10001,
		ExpectedPeerGID:                  10001,
		SocketMode:                       0o660,
		TechnicalListen:                  ":9090",
		PostgresDSNFile:                  "/var/run/secrets/mattercodex/internal-rpc-authority/postgres/dsn",
		PostgresTLSServerName:            "internal-rpc-authority-postgresql.mattercodex-system.svc.cluster.local",
		PostgresMaxConnections:           8,
		SnapshotJWSFile:                  "/var/run/config/mattercodex/internal-rpc-authority/snapshot/snapshot.jws",
		ManifestRootPublicJWKFile:        "/usr/local/share/internal-rpc-authority/manifest-root/bootstrap-public.jwk",
		ManifestRootMetadataFile:         "/usr/local/share/internal-rpc-authority/manifest-root/bootstrap-metadata.json",
		ManifestTrustBundleJWSFile:       "/var/run/config/mattercodex/internal-rpc-authority/manifest-trust/bundle.jws",
		ContextPrivateJWKFile:            "/var/run/secrets/mattercodex/internal-rpc-authority/issuer/private.jwk",
		ProofTrustJWKFile:                "/var/run/config/mattercodex/internal-rpc-authority/authority-proof-trust/jwks.json",
		VaultAddress:                     "https://vault.mattercodex-system.svc:8200",
		VaultTLSServerName:               "vault.mattercodex-system.svc.cluster.local",
		VaultCAFile:                      "/var/run/config/mattercodex/internal-rpc-authority/vault/ca.pem",
		VaultAuthFile:                    "/var/run/secrets/tokens/vault/token",
		ReadbackAttestorAddress:          "internal-rpc-authority-readback-attestor.mattercodex-system.svc:8443",
		ReadbackAttestorTLSServerName:    "internal-rpc-authority-readback-attestor.mattercodex-system.svc",
		ReadbackAttestorCAFile:           "/var/run/config/mattercodex/internal-rpc-authority/readback/attestor-ca.pem",
		WorkloadCertificateFile:          "/var/run/secrets/mattercodex/internal-rpc-authority/workload-tls/tls.crt",
		WorkloadPrivateKeyFile:           "/var/run/secrets/mattercodex/internal-rpc-authority/workload-tls/tls.key",
		RestoreControllerAddress:         "internal-rpc-authority-restore-controller.mattercodex-system.svc:8443",
		RestoreControllerTLSServerName:   "internal-rpc-authority-restore-controller.mattercodex-system.svc",
		RestoreControllerCAFile:          "/var/run/config/mattercodex/internal-rpc-authority/restore/controller-ca.pem",
		RestoreControllerCertificateFile: "/var/run/config/mattercodex/internal-rpc-authority/restore/controller-trust/tls.crt",
		RestoreRoleTrustJWSFile:          "/var/run/config/mattercodex/internal-rpc-authority/restore/role-trust/restore-role-trust.jws",
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
		config.VaultAuthRole = "internal-rpc-authority-control-api-gateway"
		config.ReadbackCredentialVaultPath = "kv/data/mattercodex/internal-rpc-authority/control-api-gateway/issuer/readback-credential"
		config.ReadbackPossessionVaultPath = "kv/data/mattercodex/internal-rpc-authority/control-api-gateway/issuer/readback-possession"
		config.RestoreRoleCredentialVaultPath = "kv/data/mattercodex/internal-rpc-authority/control-api-gateway/issuer/restore-credential"
		config.RestoreACKVaultPath = "kv/data/mattercodex/internal-rpc-authority/control-api-gateway/issuer/restore-ack"
	case ModeVerifier:
		config.ServiceName = "internal_rpc_authority_verifier"
		config.SocketPath = socketRoot + "/verifier.sock"
		config.ExpectedProcessUID = 29002
		config.ExpectedProcessGID = 29000
		config.DatabaseCapabilityRole = "internal_rpc_authority_verifier"
		config.ReadbackRole = "AUTHORIZATION_VERIFIER"
		config.VaultAuthRole = "internal-rpc-authority-control-plane"
		config.ReadbackCredentialVaultPath = "kv/data/mattercodex/internal-rpc-authority/control-plane/verifier/readback-credential"
		config.ReadbackPossessionVaultPath = "kv/data/mattercodex/internal-rpc-authority/control-plane/verifier/readback-possession"
		config.ResolverReadbackCredentialPath = "kv/data/mattercodex/internal-rpc-authority/control-plane/resolver/readback-credential"
		config.ResolverReadbackPossessionPath = "kv/data/mattercodex/internal-rpc-authority/control-plane/resolver/readback-possession"
		config.ResolverProofPrivateJWKFile = "/var/run/secrets/mattercodex/internal-rpc-authority/proof-signer/private.jwk"
		config.ResolverProofTrustJWKFile = "/var/run/config/mattercodex/internal-rpc-authority/authority-proof-trust/jwks.json"
		config.ResolverCredentialGeneration = 1
		config.ResolverPossessionKeyGeneration = 1
		config.ResolverProofSignerGeneration = 1
		config.RestoreRoleCredentialVaultPath = "kv/data/mattercodex/internal-rpc-authority/control-plane/verifier/restore-credential"
		config.RestoreACKVaultPath = "kv/data/mattercodex/internal-rpc-authority/control-plane/verifier/restore-ack"
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

// applyWorkloadProfile закрывает trusted deployment configuration до реестра
// известных workload. Vault paths нельзя выбирать через окружение workload.
func applyWorkloadProfile(config *Config) error {
	if config == nil {
		return errors.New("authority workload profile is required")
	}
	type workloadProfile struct {
		spiffeID                  string
		vaultRole                 string
		readbackCredentialPath    string
		readbackPossessionPath    string
		restoreRoleCredentialPath string
		restoreACKPath            string
	}
	profiles := map[Mode]map[string]workloadProfile{
		ModeIssuer: {
			"automation-scheduler": {
				spiffeID:                  "spiffe://mattercodex.local/ns/mattercodex-system/sa/automation-scheduler",
				vaultRole:                 "internal-rpc-authority-automation-scheduler",
				readbackCredentialPath:    "kv/data/mattercodex/internal-rpc-authority/automation-scheduler/issuer/readback-credential",
				readbackPossessionPath:    "kv/data/mattercodex/internal-rpc-authority/automation-scheduler/issuer/readback-possession",
				restoreRoleCredentialPath: "kv/data/mattercodex/internal-rpc-authority/automation-scheduler/issuer/restore-credential",
				restoreACKPath:            "kv/data/mattercodex/internal-rpc-authority/automation-scheduler/issuer/restore-ack",
			},
			"control-api-gateway": {
				spiffeID:                  "spiffe://mattercodex.local/ns/mattercodex-system/sa/control-api-gateway",
				vaultRole:                 "internal-rpc-authority-control-api-gateway",
				readbackCredentialPath:    "kv/data/mattercodex/internal-rpc-authority/control-api-gateway/issuer/readback-credential",
				readbackPossessionPath:    "kv/data/mattercodex/internal-rpc-authority/control-api-gateway/issuer/readback-possession",
				restoreRoleCredentialPath: "kv/data/mattercodex/internal-rpc-authority/control-api-gateway/issuer/restore-credential",
				restoreACKPath:            "kv/data/mattercodex/internal-rpc-authority/control-api-gateway/issuer/restore-ack",
			},
			"integration-gateway": {
				spiffeID:                  "spiffe://mattercodex.local/ns/mattercodex-system/sa/integration-gateway",
				vaultRole:                 "internal-rpc-authority-integration-gateway",
				readbackCredentialPath:    "kv/data/mattercodex/internal-rpc-authority/integration-gateway/issuer/readback-credential",
				readbackPossessionPath:    "kv/data/mattercodex/internal-rpc-authority/integration-gateway/issuer/readback-possession",
				restoreRoleCredentialPath: "kv/data/mattercodex/internal-rpc-authority/integration-gateway/issuer/restore-credential",
				restoreACKPath:            "kv/data/mattercodex/internal-rpc-authority/integration-gateway/issuer/restore-ack",
			},
		},
		ModeVerifier: {
			"control-plane": {
				spiffeID:                  "spiffe://mattercodex.local/ns/mattercodex-system/sa/control-plane",
				vaultRole:                 "internal-rpc-authority-control-plane",
				readbackCredentialPath:    "kv/data/mattercodex/internal-rpc-authority/control-plane/verifier/readback-credential",
				readbackPossessionPath:    "kv/data/mattercodex/internal-rpc-authority/control-plane/verifier/readback-possession",
				restoreRoleCredentialPath: "kv/data/mattercodex/internal-rpc-authority/control-plane/verifier/restore-credential",
				restoreACKPath:            "kv/data/mattercodex/internal-rpc-authority/control-plane/verifier/restore-ack",
			},
		},
	}
	profile, ok := profiles[config.Mode][config.WorkloadID]
	if !ok || config.WorkloadSPIFFEID != profile.spiffeID || config.VaultAuthRole != profile.vaultRole {
		return errors.New("authority workload profile is not registered")
	}
	config.ReadbackCredentialVaultPath = profile.readbackCredentialPath
	config.ReadbackPossessionVaultPath = profile.readbackPossessionPath
	config.RestoreRoleCredentialVaultPath = profile.restoreRoleCredentialPath
	config.RestoreACKVaultPath = profile.restoreACKPath
	return nil
}

// Validate проверяет точные пути, идентичности и ограниченные интервалы.
func (config Config) Validate() error {
	if config.WorkloadID == "" || len(config.WorkloadID) > 96 {
		return errors.New("INTERNAL_RPC_AUTHORITY_WORKLOAD_ID is required and bounded")
	}
	if !strings.HasPrefix(
		config.WorkloadSPIFFEID,
		"spiffe://mattercodex.local/ns/mattercodex-system/sa/",
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
	if config.VaultAddress != "https://vault.mattercodex-system.svc:8200" ||
		config.VaultTLSServerName != "vault.mattercodex-system.svc.cluster.local" ||
		config.VaultAuthRole == "" ||
		config.ReadbackCredentialVaultPath == "" ||
		config.ReadbackPossessionVaultPath == "" ||
		config.RestoreRoleCredentialVaultPath == "" ||
		config.RestoreACKVaultPath == "" {
		return errors.New("exact Vault authority delivery boundary is required")
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
	if config.Mode == ModeVerifier &&
		(config.ResolverReadbackCredentialPath == "" ||
			config.ResolverReadbackPossessionPath == "" ||
			!filepath.IsAbs(config.ResolverProofPrivateJWKFile) ||
			!filepath.IsAbs(config.ResolverProofTrustJWKFile) ||
			config.ResolverCredentialGeneration == 0 ||
			config.ResolverCredentialGeneration > maximumSafeInteger ||
			config.ResolverPossessionKeyGeneration == 0 ||
			config.ResolverPossessionKeyGeneration > maximumSafeInteger ||
			config.ResolverProofSignerGeneration == 0 ||
			config.ResolverProofSignerGeneration > maximumSafeInteger) {
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
	for name, path := range map[string]string{
		"PostgresDSNFile":                  config.PostgresDSNFile,
		"SnapshotJWSFile":                  config.SnapshotJWSFile,
		"ManifestRootPublicJWKFile":        config.ManifestRootPublicJWKFile,
		"ManifestRootMetadataFile":         config.ManifestRootMetadataFile,
		"ManifestTrustBundleJWSFile":       config.ManifestTrustBundleJWSFile,
		"VaultCAFile":                      config.VaultCAFile,
		"VaultAuthFile":                    config.VaultAuthFile,
		"ReadbackAttestorCAFile":           config.ReadbackAttestorCAFile,
		"WorkloadCertificateFile":          config.WorkloadCertificateFile,
		"WorkloadPrivateKeyFile":           config.WorkloadPrivateKeyFile,
		"RestoreControllerCAFile":          config.RestoreControllerCAFile,
		"RestoreControllerCertificateFile": config.RestoreControllerCertificateFile,
		"RestoreRoleTrustJWSFile":          config.RestoreRoleTrustJWSFile,
	} {
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
