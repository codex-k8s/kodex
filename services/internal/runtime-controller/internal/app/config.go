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

const serviceName = "runtime-controller"

type Config struct {
	Environment                    string        `env:"DEPLOYMENT_ENVIRONMENT"`
	TechnicalListen                string        `env:"RUNTIME_CONTROLLER_TECHNICAL_LISTEN"`
	Namespace                      string        `env:"POD_NAMESPACE"`
	PodUID                         string        `env:"POD_UID"`
	ControlPlaneTarget             string        `env:"RUNTIME_CONTROL_PLANE_TARGET"`
	ControlPlaneTLSServerName      string        `env:"RUNTIME_CONTROL_PLANE_TLS_SERVER_NAME"`
	ControlPlaneCAFile             string        `env:"RUNTIME_CONTROL_PLANE_CA_FILE"`
	ControlPlaneCertificateFile    string        `env:"RUNTIME_CONTROL_PLANE_CERTIFICATE_FILE"`
	ControlPlanePrivateKeyFile     string        `env:"RUNTIME_CONTROL_PLANE_PRIVATE_KEY_FILE"`
	ApplicationGrantFile           string        `env:"RUNTIME_APPLICATION_GRANT_FILE"`
	RoleImageRepository            string        `env:"RUNTIME_ROLE_IMAGE_REPOSITORY"`
	AgentGatewayURL                string        `env:"RUNTIME_AGENT_GATEWAY_URL"`
	MCPGatewayURL                  string        `env:"RUNTIME_MCP_GATEWAY_URL"`
	ControllerImage                string        `env:"RUNTIME_CONTROLLER_IMAGE"`
	AuthorityImage                 string        `env:"RUNTIME_AUTHORITY_IMAGE"`
	StorageClass                   string        `env:"RUNTIME_STORAGE_CLASS"`
	PVCSize                        string        `env:"RUNTIME_PVC_SIZE"`
	ReadClusterRole                string        `env:"RUNTIME_READ_CLUSTER_ROLE"`
	AdminClusterRole               string        `env:"RUNTIME_ADMIN_CLUSTER_ROLE"`
	ArchiveServiceAccount          string        `env:"RUNTIME_ARCHIVE_SERVICE_ACCOUNT"`
	RestoreServiceAccount          string        `env:"RUNTIME_RESTORE_SERVICE_ACCOUNT"`
	CleanupServiceAccount          string        `env:"RUNTIME_CLEANUP_SERVICE_ACCOUNT"`
	CredentialBrokerServiceAccount string        `env:"RUNTIME_CREDENTIAL_BROKER_SERVICE_ACCOUNT"`
	S3ArchiveBrokerServiceAccount  string        `env:"RUNTIME_S3_ARCHIVE_BROKER_SERVICE_ACCOUNT"`
	S3RestoreBrokerServiceAccount  string        `env:"RUNTIME_S3_RESTORE_BROKER_SERVICE_ACCOUNT"`
	MaximumPods                    int           `env:"RUNTIME_MAXIMUM_PODS"`
	MaximumOrganizationExecutions  int           `env:"RUNTIME_MAXIMUM_ORGANIZATION_EXECUTIONS"`
	MaximumCPUMilli                int64         `env:"RUNTIME_MAXIMUM_CPU_MILLI"`
	MaximumMemoryBytes             int64         `env:"RUNTIME_MAXIMUM_MEMORY_BYTES"`
	S3Endpoint                     string        `env:"RUNTIME_S3_ENDPOINT"`
	S3TLSServerName                string        `env:"RUNTIME_S3_TLS_SERVER_NAME"`
	S3Bucket                       string        `env:"RUNTIME_S3_BUCKET"`
	S3Region                       string        `env:"RUNTIME_S3_REGION"`
	NATSURL                        string        `env:"RUNTIME_NATS_URL"`
	NATSTLSServerName              string        `env:"RUNTIME_NATS_TLS_SERVER_NAME"`
	NATSCAFile                     string        `env:"RUNTIME_NATS_CA_FILE"`
	NATSCertificateFile            string        `env:"RUNTIME_NATS_CERTIFICATE_FILE"`
	NATSPrivateKeyFile             string        `env:"RUNTIME_NATS_PRIVATE_KEY_FILE"`
	NATSCredentialsFile            string        `env:"RUNTIME_NATS_CREDENTIALS_FILE"`
	NATSStream                     string        `env:"RUNTIME_NATS_STREAM"`
	NATSDurable                    string        `env:"RUNTIME_NATS_DURABLE"`
	NATSReplicas                   int           `env:"RUNTIME_NATS_REPLICAS"`
	PostgresDSNFile                string        `env:"RUNTIME_POSTGRES_DSN_FILE"`
	PostgresTLSServerName          string        `env:"RUNTIME_POSTGRES_TLS_SERVER_NAME"`
	PostgresCAFile                 string        `env:"RUNTIME_POSTGRES_CA_FILE"`
	PostgresPrincipal              string        `env:"RUNTIME_POSTGRES_PRINCIPAL"`
	StartupTimeout                 time.Duration `env:"RUNTIME_STARTUP_TIMEOUT"`
	ShutdownTimeout                time.Duration `env:"RUNTIME_SHUTDOWN_TIMEOUT"`
	ReconcileInterval              time.Duration `env:"RUNTIME_RECONCILE_INTERVAL"`
	ClaimInterval                  time.Duration `env:"RUNTIME_CLAIM_INTERVAL"`
	ExpiryInterval                 time.Duration `env:"RUNTIME_EXPIRY_INTERVAL"`
	ReadinessInterval              time.Duration `env:"RUNTIME_READINESS_INTERVAL"`
	Watchdog                       time.Duration `env:"RUNTIME_WATCHDOG"`
	WarmTTL                        time.Duration `env:"RUNTIME_WARM_TTL"`
	JobTTL                         time.Duration `env:"RUNTIME_JOB_TTL"`
}

func loadConfig() (Config, error) {
	config := Config{
		TechnicalListen: ":9090", ControlPlaneTarget: "control-plane.mattercodex-system.svc:8443",
		ControlPlaneTLSServerName:   "control-plane.mattercodex-system.svc.cluster.local",
		ControlPlaneCAFile:          "/var/run/config/mattercodex/runtime-controller/control-plane/ca.pem",
		ControlPlaneCertificateFile: "/var/run/secrets/mattercodex/runtime-controller/workload-tls/tls.crt",
		ControlPlanePrivateKeyFile:  "/var/run/secrets/mattercodex/runtime-controller/workload-tls/tls.key",
		ApplicationGrantFile:        "/var/run/secrets/mattercodex/runtime-controller/application-grant/application-grant.jws",
		RoleImageRepository:         "mattercodex-image-registry.mattercodex-system.svc.cluster.local:5000/mattercodex/agent-runtime",
		AgentGatewayURL:             "https://matter-codex-bot-service.mattermost.svc.cluster.local:8443",
		MCPGatewayURL:               "https://matter-codex-bot-service.mattermost.svc.cluster.local:8443",
		StorageClass:                "runtime-session", PVCSize: "20Gi",
		ReadClusterRole:  "runtime-role-project-read",
		AdminClusterRole: "cluster-admin", ArchiveServiceAccount: "runtime-archive",
		RestoreServiceAccount: "runtime-restore-verifier", CleanupServiceAccount: "runtime-cleanup-authorizer",
		CredentialBrokerServiceAccount: "runtime-credential-broker",
		S3ArchiveBrokerServiceAccount:  "runtime-s3-archive-broker",
		S3RestoreBrokerServiceAccount:  "runtime-s3-restore-broker",
		MaximumPods:                    100, MaximumOrganizationExecutions: 16,
		MaximumCPUMilli: 100_000, MaximumMemoryBytes: 400 << 30,
		S3Endpoint:      "https://runtime-archive-s3.mattercodex-system.svc:9000",
		S3TLSServerName: "runtime-archive-s3.mattercodex-system.svc.cluster.local",
		S3Bucket:        "mattercodex-runtime", S3Region: "mattercodex-1",
		NATSURL:             "tls://nats.mattercodex-system.svc:4222",
		NATSTLSServerName:   "nats.mattercodex-system.svc.cluster.local",
		NATSCAFile:          "/var/run/config/mattercodex/runtime-controller/nats/ca.pem",
		NATSCertificateFile: "/var/run/secrets/mattercodex/runtime-controller/nats-tls/tls.crt",
		NATSPrivateKeyFile:  "/var/run/secrets/mattercodex/runtime-controller/nats-tls/tls.key",
		NATSCredentialsFile: "/var/run/secrets/mattercodex/runtime-controller/nats/user.creds",
		NATSStream:          "CONTROL_PLANE", NATSDurable: "RUNTIME_CONTROLLER_V1", NATSReplicas: 3,
		PostgresDSNFile:       "/var/run/secrets/mattercodex/runtime-controller/postgres/dsn",
		PostgresTLSServerName: "runtime-controller-postgresql.mattercodex-system.svc.cluster.local",
		PostgresCAFile:        "/var/run/config/mattercodex/runtime-controller/postgres/ca.pem",
		PostgresPrincipal:     "runtime_controller_runtime_g1",
		StartupTimeout:        30 * time.Second, ShutdownTimeout: 20 * time.Second,
		ReconcileInterval: 5 * time.Second, ClaimInterval: time.Second, ExpiryInterval: 15 * time.Second,
		ReadinessInterval: 10 * time.Second, Watchdog: 2 * time.Minute,
		WarmTTL: 4 * time.Hour, JobTTL: time.Hour,
	}
	if err := env.Parse(&config); err != nil {
		return Config{}, err
	}
	if err := config.validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}

func (config Config) validate() error {
	for _, endpoint := range []string{config.TechnicalListen, config.ControlPlaneTarget} {
		if _, _, err := net.SplitHostPort(endpoint); err != nil {
			return errors.New("runtime-controller endpoint is invalid")
		}
	}
	s3Endpoint, err := url.Parse(config.S3Endpoint)
	if err != nil || s3Endpoint.Scheme != "https" || s3Endpoint.Host == "" || s3Endpoint.Path != "" ||
		config.S3TLSServerName == "" || net.ParseIP(config.S3TLSServerName) != nil ||
		config.ControlPlaneTLSServerName == "" || net.ParseIP(config.ControlPlaneTLSServerName) != nil {
		return errors.New("runtime-controller TLS endpoint is invalid")
	}
	for _, raw := range []string{config.AgentGatewayURL, config.MCPGatewayURL} {
		endpoint, parseErr := url.Parse(raw)
		if parseErr != nil || endpoint.Scheme != "https" || endpoint.Host == "" || endpoint.Path != "" ||
			endpoint.RawQuery != "" || endpoint.Fragment != "" || endpoint.User != nil ||
			!strings.HasSuffix(endpoint.Hostname(), ".svc.cluster.local") {
			return errors.New("runtime agent gateway endpoint is invalid")
		}
	}
	if config.AgentGatewayURL != config.MCPGatewayURL {
		return errors.New("runtime agent gateway endpoints must use the authoritative bot-service boundary")
	}
	if !strings.HasPrefix(config.NATSURL, "tls://") || config.NATSTLSServerName == "" ||
		net.ParseIP(config.NATSTLSServerName) != nil || config.PostgresTLSServerName == "" ||
		net.ParseIP(config.PostgresTLSServerName) != nil {
		return errors.New("runtime-controller dependency TLS endpoint is invalid")
	}
	for _, path := range []string{config.ControlPlaneCAFile, config.ControlPlaneCertificateFile,
		config.ControlPlanePrivateKeyFile, config.ApplicationGrantFile, config.NATSCAFile,
		config.NATSCertificateFile, config.NATSPrivateKeyFile, config.NATSCredentialsFile,
		config.PostgresDSNFile, config.PostgresCAFile} {
		if !filepath.IsAbs(path) {
			return errors.New("runtime-controller path must be absolute")
		}
	}
	if (config.Environment != "staging" && config.Environment != "production") ||
		config.Namespace == "" || config.PodUID == "" || config.StorageClass == "" || config.PVCSize == "" ||
		config.S3Bucket == "" || config.S3Region == "" || config.MaximumPods < 1 || config.MaximumPods > 10_000 ||
		config.MaximumOrganizationExecutions < 1 || config.MaximumOrganizationExecutions > config.MaximumPods ||
		config.NATSStream != "CONTROL_PLANE" || config.NATSDurable != "RUNTIME_CONTROLLER_V1" ||
		config.NATSReplicas < 1 || config.NATSReplicas > 5 || config.PostgresPrincipal == "" ||
		config.MaximumCPUMilli < 1 || config.MaximumMemoryBytes < 1 ||
		!validPinnedImage(config.ControllerImage) || !validPinnedImage(config.AuthorityImage) ||
		strings.Contains(config.RoleImageRepository, "@") {
		return errors.New("runtime-controller bounded configuration is invalid")
	}
	if config.WarmTTL != 4*time.Hour ||
		config.StartupTimeout < 5*time.Second || config.StartupTimeout > time.Minute ||
		config.ShutdownTimeout < 5*time.Second || config.ShutdownTimeout > time.Minute ||
		config.ReconcileInterval < time.Second || config.ReconcileInterval > time.Minute ||
		config.ClaimInterval < 250*time.Millisecond || config.ClaimInterval > time.Minute ||
		config.ExpiryInterval < 5*time.Second || config.ExpiryInterval > time.Minute ||
		config.ReadinessInterval < 5*time.Second || config.ReadinessInterval > time.Minute ||
		config.Watchdog < 30*time.Second || config.Watchdog > 10*time.Minute ||
		config.JobTTL < time.Minute || config.JobTTL > 24*time.Hour {
		return errors.New("runtime-controller duration is invalid")
	}
	return nil
}

func validPinnedImage(value string) bool {
	parts := strings.Split(value, "@sha256:")
	if len(parts) != 2 || parts[0] == "" || len(parts[1]) != 64 || strings.Trim(parts[1], "0123456789abcdef") != "" {
		return false
	}
	return parts[1] != strings.Repeat("0", 64)
}
