package app

import (
	"errors"
	"net"
	"net/url"
	"path/filepath"
	"slices"
	"time"

	"github.com/caarlos0/env/v11"
)

const (
	// defaultControlPlaneTarget fixes the owner service DNS resolver target accepted by configuration
	// validation, not an arbitrary caller-selected endpoint.
	// defaultControlPlaneTarget фиксирует DNS resolver target owner service, принимаемый configuration
	// validation, не произвольный endpoint по выбору caller.
	defaultControlPlaneTarget = "dns:///control-plane.kodex-system.svc:8443"
	// defaultControlPlaneTLSServerName pins the expected owner TLS server name independently of its resolver
	// target.
	// defaultControlPlaneTLSServerName закрепляет ожидаемый owner TLS server name независимо от resolver
	// target.
	defaultControlPlaneTLSServerName = "control-plane.kodex-system.svc.cluster.local"
	// defaultControlAPIClientSPIFFEID identifies the gateway certificate URI in the fixed client-identity set;
	// membership alone does not grant every RPC.
	// defaultControlAPIClientSPIFFEID обозначает certificate URI шлюза в фиксированном client-identity set;
	// наличие в нём не разрешает каждый RPC.
	defaultControlAPIClientSPIFFEID = "spiffe://kodex.local/ns/kodex-system/sa/control-api-gateway"
	// defaultControlPlaneClientSPIFFEID identifies the control-plane client workload for the broker's exact
	// TLS identity allowlist.
	// defaultControlPlaneClientSPIFFEID обозначает control-plane client workload в exact TLS identity
	// allowlist broker.
	defaultControlPlaneClientSPIFFEID = "spiffe://kodex.local/ns/kodex-system/sa/control-plane"
	// defaultRuntimeClientSPIFFEID identifies the runtime-controller workload allowed at the certificate
	// boundary, with operation authorization enforced separately.
	// defaultRuntimeClientSPIFFEID обозначает runtime-controller workload, допустимый на certificate boundary;
	// operation authorization проверяется отдельно.
	defaultRuntimeClientSPIFFEID = "spiffe://kodex.local/ns/kodex-system/sa/runtime-controller"
	// defaultSTTClientSPIFFEID identifies the STT workload for the shared certificate boundary without
	// conferring arbitrary credential-read rights.
	// defaultSTTClientSPIFFEID обозначает STT workload на общей certificate boundary без выдачи произвольных
	// credential-read rights.
	defaultSTTClientSPIFFEID = "spiffe://kodex.local/ns/kodex-system/sa/stt-tts-service"
	// defaultProviderEgressProxy pins the provider transport's internal managed proxy for both HTTP_PROXY and
	// HTTPS_PROXY; the HTTP scheme describes the proxy hop, not a plaintext provider fallback.
	// defaultProviderEgressProxy закрепляет внутренний управляемый proxy provider transport для HTTP_PROXY и
	// HTTPS_PROXY; HTTP scheme относится к proxy hop, не к plaintext provider fallback.
	defaultProviderEgressProxy = "http://egress-gateway.kodex-system.svc.cluster.local:8080"
)

// Config holds typed environment-derived runtime/identity paths, exact service boundaries and time/size
// budgets; parsing these fields does not prove file safety, network reachability or caller authority.
// Config хранит typed environment-derived runtime/identity paths, exact service boundaries и time/size
// budgets; разбор полей не доказывает file safety, network reachability и caller authority.
type Config struct {
	RPCProfile                  string        `env:"KODEX_RPC_PROFILE"`
	Environment                 string        `env:"DEPLOYMENT_ENVIRONMENT"`
	RuntimeNamespace            string        `env:"SECRET_BROKER_RUNTIME_NAMESPACE"`
	DraftNamespace              string        `env:"SECRET_BROKER_DRAFT_NAMESPACE"`
	DraftKeyringFile            string        `env:"SECRET_BROKER_DRAFT_KEYRING_FILE"`
	DraftKeyGuardName           string        `env:"SECRET_BROKER_DRAFT_KEY_GUARD_NAME"`
	ClaimantID                  string        `env:"POD_UID"`
	GRPCListen                  string        `env:"SECRET_BROKER_GRPC_LISTEN"`
	TechnicalListen             string        `env:"SECRET_BROKER_TECHNICAL_LISTEN"`
	ServerCertificateFile       string        `env:"SECRET_BROKER_SERVER_CERTIFICATE_FILE"`
	ServerPrivateKeyFile        string        `env:"SECRET_BROKER_SERVER_PRIVATE_KEY_FILE"`
	ClientCAFile                string        `env:"SECRET_BROKER_CLIENT_CA_FILE"`
	ExpectedClientSPIFFEIDs     []string      `env:"SECRET_BROKER_EXPECTED_CLIENT_SPIFFE_IDS" envSeparator:","`
	ControlPlaneTarget          string        `env:"SECRET_BROKER_CONTROL_PLANE_TARGET"`
	ControlPlaneTLSServerName   string        `env:"SECRET_BROKER_CONTROL_PLANE_TLS_SERVER_NAME"`
	ControlPlaneCAFile          string        `env:"SECRET_BROKER_CONTROL_PLANE_CA_FILE"`
	ControlPlaneCertificateFile string        `env:"SECRET_BROKER_CONTROL_PLANE_CERTIFICATE_FILE"`
	ControlPlanePrivateKeyFile  string        `env:"SECRET_BROKER_CONTROL_PLANE_PRIVATE_KEY_FILE"`
	ApplicationGrantFile        string        `env:"SECRET_BROKER_APPLICATION_GRANT_FILE"`
	RequestTimeout              time.Duration `env:"SECRET_BROKER_REQUEST_TIMEOUT"`
	ShutdownTimeout             time.Duration `env:"SECRET_BROKER_SHUTDOWN_TIMEOUT"`
	MaximumSecretBytes          int           `env:"SECRET_BROKER_MAXIMUM_SECRET_BYTES"`
	RecoveryInterval            time.Duration `env:"SECRET_BROKER_RECOVERY_INTERVAL"`
	RecoveryTimeout             time.Duration `env:"SECRET_BROKER_RECOVERY_TIMEOUT"`
	CodexBinary                 string        `env:"SECRET_BROKER_CODEX_BINARY"`
	ProviderAuthorizationRoot   string        `env:"SECRET_BROKER_PROVIDER_AUTHORIZATION_ROOT"`
	ProviderDeviceAuthTTL       time.Duration `env:"SECRET_BROKER_PROVIDER_DEVICE_AUTH_TTL"`
	ProviderHTTPSProxy          string        `env:"HTTPS_PROXY"`
	ProviderHTTPProxy           string        `env:"HTTP_PROXY"`
	AuthorityVerifierSocket     string        `env:"INTERNAL_RPC_AUTHORITY_VERIFIER_SOCKET"`
	AuthorityVerifierUID        uint32        `env:"SECRET_BROKER_AUTHORITY_VERIFIER_UID"`
	AuthorityVerifierGID        uint32        `env:"SECRET_BROKER_AUTHORITY_VERIFIER_GID"`
}

// loadConfig starts from explicit deployment defaults, overlays tagged environment values and validates
// the result; parse failure returns zero Config, while validation failure returns the parsed Config with
// its error.
// loadConfig начинает с явных deployment defaults, применяет tagged environment values и проверяет
// результат; parse failure возвращает zero Config, validation failure — разобранный Config с ошибкой.
func loadConfig() (Config, error) {
	config := Config{
		RuntimeNamespace: "kodex-runtime", GRPCListen: ":8443", TechnicalListen: ":9090",
		DraftNamespace: "kodex-secret-drafts", DraftKeyGuardName: "secret-broker-draft-key-guard",
		DraftKeyringFile:      "/var/run/secrets/kodex/secret-broker/draft-keyring/keyring.json",
		ServerCertificateFile: "/var/run/secrets/kodex/secret-broker/server/tls.crt",
		ServerPrivateKeyFile:  "/var/run/secrets/kodex/secret-broker/server/tls.key",
		ClientCAFile:          "/var/run/config/kodex/secret-broker/client/ca.pem",
		ExpectedClientSPIFFEIDs: []string{
			defaultControlAPIClientSPIFFEID, defaultControlPlaneClientSPIFFEID,
			defaultRuntimeClientSPIFFEID, defaultSTTClientSPIFFEID,
		},
		ControlPlaneTarget:          defaultControlPlaneTarget,
		ControlPlaneTLSServerName:   defaultControlPlaneTLSServerName,
		ControlPlaneCAFile:          "/var/run/config/kodex/secret-broker/control-plane/ca.pem",
		ControlPlaneCertificateFile: "/var/run/secrets/kodex/secret-broker/workload-tls/tls.crt",
		ControlPlanePrivateKeyFile:  "/var/run/secrets/kodex/secret-broker/workload-tls/tls.key",
		ApplicationGrantFile:        "/var/run/secrets/kodex/secret-broker/application-grant/application-grant.jws",
		RequestTimeout:              5 * time.Second, ShutdownTimeout: 20 * time.Second, MaximumSecretBytes: 512 << 10,
		RecoveryInterval: 30 * time.Second, RecoveryTimeout: 10 * time.Second,
		CodexBinary: "/usr/local/bin/codex", ProviderAuthorizationRoot: "/var/lib/kodex-provider-auth/sessions",
		ProviderDeviceAuthTTL: 15 * time.Minute, ProviderHTTPSProxy: defaultProviderEgressProxy,
		ProviderHTTPProxy: defaultProviderEgressProxy, AuthorityVerifierSocket: "/run/kodex/internal-rpc-authority/verifier.sock",
		AuthorityVerifierUID: 29002, AuthorityVerifierGID: 29000,
	}
	if err := env.Parse(&config); err != nil {
		return Config{}, err
	}
	return config, config.validate()
}

// validate restricts deployment/profile, exact namespaces/owner route/proxy/identity set, claimant syntax,
// durations and sizes, then checks listen/path/URI shapes. Absolute paths are not opened or
// permission-checked and SplitHostPort alone is not port-range validation.
// validate ограничивает deployment/profile, exact namespaces/owner route/proxy/identity set, claimant
// syntax, durations и sizes, затем проверяет listen/path/URI shapes. Absolute paths не открываются и их
// права не проверяются, один SplitHostPort не проверяет port range.
func (config Config) validate() error {
	if config.RPCProfile != "" && config.RPCProfile != "trusted-cluster" {
		return errors.New("secret broker RPC profile is invalid")
	}
	if (config.Environment != "staging" && config.Environment != "production") ||
		config.RuntimeNamespace != "kodex-runtime" || config.DraftNamespace != "kodex-secret-drafts" ||
		config.DraftKeyGuardName != "secret-broker-draft-key-guard" || config.ClaimantID == "" || len(config.ClaimantID) > 128 ||
		config.ControlPlaneTarget != defaultControlPlaneTarget ||
		config.ControlPlaneTLSServerName != defaultControlPlaneTLSServerName ||
		config.RequestTimeout < time.Second || config.RequestTimeout > 10*time.Second ||
		config.ShutdownTimeout < 5*time.Second || config.ShutdownTimeout > time.Minute ||
		config.MaximumSecretBytes < 1<<10 || config.MaximumSecretBytes > 1<<20 ||
		config.RecoveryInterval < 5*time.Second || config.RecoveryInterval > 5*time.Minute ||
		config.RecoveryTimeout < time.Second || config.RecoveryTimeout > 30*time.Second {
		return errors.New("secret broker configuration is invalid")
	}
	for _, character := range config.ClaimantID {
		if character < 0x21 || character > 0x7e {
			return errors.New("secret broker claimant ID is invalid")
		}
	}
	for _, address := range []string{config.GRPCListen, config.TechnicalListen} {
		if _, _, err := net.SplitHostPort(address); err != nil {
			return errors.New("secret broker listen address is invalid")
		}
	}
	if config.ProviderDeviceAuthTTL < time.Minute || config.ProviderDeviceAuthTTL > time.Hour ||
		config.ProviderHTTPSProxy != defaultProviderEgressProxy || config.ProviderHTTPProxy != defaultProviderEgressProxy ||
		config.AuthorityVerifierUID == 0 || config.AuthorityVerifierGID == 0 || len(config.ExpectedClientSPIFFEIDs) != 4 ||
		!slices.Contains(config.ExpectedClientSPIFFEIDs, defaultControlAPIClientSPIFFEID) ||
		!slices.Contains(config.ExpectedClientSPIFFEIDs, defaultControlPlaneClientSPIFFEID) ||
		!slices.Contains(config.ExpectedClientSPIFFEIDs, defaultRuntimeClientSPIFFEID) ||
		!slices.Contains(config.ExpectedClientSPIFFEIDs, defaultSTTClientSPIFFEID) {
		return errors.New("secret broker provider credential configuration is invalid")
	}
	for _, path := range []string{config.ServerCertificateFile, config.ServerPrivateKeyFile, config.ClientCAFile,
		config.ControlPlaneCAFile, config.ControlPlaneCertificateFile, config.ControlPlanePrivateKeyFile, config.ApplicationGrantFile,
		config.CodexBinary, config.ProviderAuthorizationRoot, config.AuthorityVerifierSocket, config.DraftKeyringFile} {
		if !filepath.IsAbs(path) {
			return errors.New("secret broker file path is invalid")
		}
	}
	for _, rawIdentity := range config.ExpectedClientSPIFFEIDs {
		identity, err := url.Parse(rawIdentity)
		if err != nil || identity.Scheme != "spiffe" || identity.Host == "" || identity.Path == "" || identity.RawQuery != "" || identity.Fragment != "" {
			return errors.New("secret broker client identity is invalid")
		}
	}
	return nil
}
