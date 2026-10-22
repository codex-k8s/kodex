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

// Mode selects the local authorization issuer or verifier composition; it does not represent the publisher, readback or restore-controller binaries, which use their own configurations.
// Mode выбирает локальную сборку издателя или проверяющего авторизации; publisher, readback и restore-controller используют собственные конфигурации и этим типом не задаются.
type Mode string

// Поддерживаемые режимы локального authority-компонента.
const (
	// ModeIssuer selects authorization-context issuance and its mode-derived socket, process identity, database capability and readback role defaults.
	// ModeIssuer выбирает выдачу контекста авторизации и зависящие от режима исходные сокет, идентичность процесса, возможность БД и роль контрольного чтения.
	ModeIssuer Mode = "issuer"
	// ModeVerifier selects authorization verification and its mode-specific defaults; resolver activation is subsequently pinned by the registered workload profile.
	// ModeVerifier выбирает проверку авторизации и исходные параметры этого режима; включение resolver затем закрепляется зарегистрированным профилем workload.
	ModeVerifier Mode = "verifier"
)

// socketRoot is the fixed parent directory for the issuer and verifier Unix sockets; Validate requires the exact mode path and this parent rather than accepting arbitrary runtime paths.
// socketRoot — фиксированный родительский каталог Unix-сокетов issuer и verifier; Validate требует точный путь режима и этот каталог, а не произвольный путь runtime.
const socketRoot = "/run/kodex/internal-rpc-authority"

var (
	// digestPattern accepts exactly sixty-four lowercase hexadecimal characters for digest fields used by app configuration consumers; matching syntax does not rehash or authenticate source content.
	// digestPattern принимает ровно шестьдесят четыре шестнадцатеричных символа нижнего регистра для полей дайджестов потребителей конфигурации app; совпадение синтаксиса не пересчитывает хеш и не подтверждает подлинность содержимого.
	digestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
	// runtimeUUIDPattern describes lowercase hyphenated UUID syntax with versions one through eight and RFC variant bits. This declaration alone neither assigns a runtime identity nor proves that any particular path applies the pattern.
	// runtimeUUIDPattern описывает UUID нижнего регистра с дефисами, версиями от одной до восьмой и битами варианта RFC. Само объявление не назначает идентичность runtime и не доказывает применение шаблона конкретным путём.
	runtimeUUIDPattern = regexp.MustCompile(
		`^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`,
	)
)

// Config carries mode-derived process and socket identities, tagged environment inputs, file paths, registered Secret names and lifecycle budgets for issuer or verifier. LoadConfig applies profiles and validation; constructing this struct directly does not prove readable files, trusted TLS material or reachable dependencies.
// Config содержит зависящие от режима идентичности процесса и сокета, входы окружения с тегами, файловые пути, зарегистрированные имена Secret и бюджеты жизненного цикла issuer или verifier. LoadConfig применяет профили и проверки; прямое создание структуры не доказывает читаемость файлов, доверенный TLS-материал или доступность зависимостей.
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

// LoadConfig initializes mode-specific defaults, parses tagged environment values, applies the exact registered workload/SPIFFE profile and calls Validate in that order. Unknown modes fail before environment parsing; success is configuration validation, not file loading or a database or network readiness probe.
// LoadConfig последовательно задаёт исходные параметры режима, разбирает значения окружения по тегам, применяет точный зарегистрированный профиль workload/SPIFFE и вызывает Validate. Неизвестные режимы отклоняются до разбора окружения; успех означает проверку конфигурации, а не загрузку файлов или пробу готовности БД либо сети.
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

// setRuntimeSecretNames initializes four readback and restore Secret names from a supplied prefix. It performs no registry check or Secret access; LoadConfig subsequently replaces these defaults with the registered workload profile.
// setRuntimeSecretNames задаёт четыре исходных имени readback- и restore-Secret по переданному префиксу. Проверки реестра или доступа к Secret нет; LoadConfig затем заменяет эти значения зарегистрированным профилем workload.
func setRuntimeSecretNames(config *Config, prefix string) {
	base := "internal-rpc-authority-" + prefix
	config.ReadbackCredentialSecret = base + "-readback-credential"
	config.ReadbackPossessionSecret = base + "-readback-possession"
	config.RestoreRoleCredentialSecret = base + "-restore-credential"
	config.RestoreACKSecret = base + "-restore-ack"
}

// applyWorkloadProfile requires a known mode/workload pair, the profile's exact SPIFFE identity and a supported Secret backend, then overwrites the four Secret names and resolver-enabled flag from that closed registry. It rejects nil or foreign bindings without consulting Kubernetes or accepting environment-chosen Secret names.
// applyWorkloadProfile требует известную пару режима и workload, точную SPIFFE-идентичность профиля и поддерживаемое хранилище Secret, затем заменяет четыре имени Secret и признак включения resolver данными закрытого реестра. Nil и чужие привязки отклоняются без обращения к Kubernetes и без принятия выбранных окружением имён Secret.
func applyWorkloadProfile(config *Config) error {
	if config == nil {
		return errors.New("authority workload profile is required")
	}
	// workloadProfile is the local registry record binding one mode/workload to an exact SPIFFE identity, four Secret object names and resolver availability. It stores references, not credential values or authority granted by caller input.
	// workloadProfile — локальная запись реестра, связывающая режим и workload с точной SPIFFE-идентичностью, четырьмя именами объектов Secret и доступностью resolver. Она хранит ссылки, а не значения реквизитов или полномочия из ввода вызывающего.
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

// Validate checks supported backend, workload and SPIFFE shape, endpoint syntax, pinned Secret-name presence, exact mode socket and actual process UID/GID, nonroot peer IDs, generation and connection bounds, enabled-resolver paths, duration limits, socket permissions, database identity and absolute file paths. It neither opens those files nor proves TLS/DNS connectivity; exact workload registration belongs to applyWorkloadProfile.
// Validate проверяет поддерживаемое хранилище, форму workload и SPIFFE, синтаксис endpoint, наличие закреплённых имён Secret, точный сокет режима и фактические UID/GID процесса, ненулевые идентификаторы peer, пределы поколений и соединений, пути включённого resolver, интервалы, права сокета, идентичность БД и абсолютность файловых путей. Файлы не открываются, TLS/DNS-связность не доказывается; точная регистрация workload проверяется applyWorkloadProfile.
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
	// maximumSafeInteger bounds generation counters to the exact integer range shared with JSON number consumers; it does not validate any referenced credential.
	// maximumSafeInteger ограничивает счётчики поколений диапазоном точных целых, общим с потребителями JSON-чисел; связанный реквизит этим не проверяется.
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
		config.SnapshotReloadInterval > 5*time.Second ||
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

// parseEnvironment delegates tagged struct decoding to env.Parse and replaces any parser error with one fixed English message, avoiding reflection of invalid environment values. It does not execute shell expressions or load a dotenv file.
// parseEnvironment передаёт разбор структуры по тегам в env.Parse и заменяет любую ошибку разборщика одним фиксированным английским сообщением, не отражая некорректные значения окружения. Shell-выражения не исполняются, dotenv-файл не загружается.
func parseEnvironment(target any) error {
	if err := env.Parse(target); err != nil {
		return errors.New("parse environment configuration")
	}
	return nil
}
