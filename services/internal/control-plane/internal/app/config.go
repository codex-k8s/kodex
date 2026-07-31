package app

import (
	"errors"
	"net"
	"path/filepath"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
	controlplanev1 "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/generated/controlplane/v1"
)

const serviceName = "control_plane"

// Config задаёт только typed names/paths/endpoints; secret values читаются из файлов.
type Config struct {
	GRPCListen             string        `env:"CONTROL_PLANE_GRPC_LISTEN"`
	TechnicalListen        string        `env:"CONTROL_PLANE_TECHNICAL_LISTEN"`
	ServerCertificateFile  string        `env:"CONTROL_PLANE_TLS_CERTIFICATE_FILE"`
	ServerPrivateKeyFile   string        `env:"CONTROL_PLANE_TLS_PRIVATE_KEY_FILE"`
	ClientCAFile           string        `env:"CONTROL_PLANE_TLS_CLIENT_CA_FILE"`
	PostgresDSNFile        string        `env:"CONTROL_PLANE_POSTGRES_DSN_FILE"`
	PostgresRelayDSNFile   string        `env:"CONTROL_PLANE_POSTGRES_RELAY_DSN_FILE"`
	PostgresTLSServerName  string        `env:"CONTROL_PLANE_POSTGRES_TLS_SERVER_NAME"`
	PostgresCAFile         string        `env:"CONTROL_PLANE_POSTGRES_CA_FILE"`
	PostgresMaxConnections int32         `env:"CONTROL_PLANE_POSTGRES_MAX_CONNECTIONS"`
	RedisAddress           string        `env:"CONTROL_PLANE_REDIS_ADDRESS"`
	RedisTLSServerName     string        `env:"CONTROL_PLANE_REDIS_TLS_SERVER_NAME"`
	RedisCAFile            string        `env:"CONTROL_PLANE_REDIS_CA_FILE"`
	RedisUsername          string        `env:"CONTROL_PLANE_REDIS_USERNAME"`
	RedisPasswordFile      string        `env:"CONTROL_PLANE_REDIS_PASSWORD_FILE"`
	RedisDatabase          int           `env:"CONTROL_PLANE_REDIS_DATABASE"`
	RedisPoolSize          int           `env:"CONTROL_PLANE_REDIS_POOL_SIZE"`
	NATSURL                string        `env:"CONTROL_PLANE_NATS_URL"`
	NATSTLSServerName      string        `env:"CONTROL_PLANE_NATS_TLS_SERVER_NAME"`
	NATSCAFile             string        `env:"CONTROL_PLANE_NATS_CA_FILE"`
	NATSCredentialsFile    string        `env:"CONTROL_PLANE_NATS_CREDENTIALS_FILE"`
	NATSStream             string        `env:"CONTROL_PLANE_NATS_STREAM"`
	NATSReplicas           int           `env:"CONTROL_PLANE_NATS_REPLICAS"`
	AuthorityPolicyFile    string        `env:"CONTROL_PLANE_AUTHORITY_POLICY_FILE"`
	ProofPrivateJWKFile    string        `env:"CONTROL_PLANE_PROOF_PRIVATE_JWK_FILE"`
	ProofTrustFile         string        `env:"CONTROL_PLANE_PROOF_TRUST_FILE"`
	ProofSignerGeneration  uint64        `env:"CONTROL_PLANE_PROOF_SIGNER_GENERATION"`
	LeaseSigningKeyFile    string        `env:"CONTROL_PLANE_LEASE_SIGNING_KEY_FILE"`
	OIDCTLSServerName      string        `env:"CONTROL_PLANE_OIDC_TLS_SERVER_NAME"`
	OIDCCAFile             string        `env:"CONTROL_PLANE_OIDC_CA_FILE"`
	InstanceID             string        `env:"POD_UID"`
	StartupTimeout         time.Duration `env:"CONTROL_PLANE_STARTUP_TIMEOUT"`
	ReadinessTimeout       time.Duration `env:"CONTROL_PLANE_READINESS_TIMEOUT"`
	ReadinessInterval      time.Duration `env:"CONTROL_PLANE_READINESS_INTERVAL"`
	ShutdownTimeout        time.Duration `env:"CONTROL_PLANE_SHUTDOWN_TIMEOUT"`
	CacheTTL               time.Duration `env:"CONTROL_PLANE_CACHE_TTL"`
	CacheTimeout           time.Duration `env:"CONTROL_PLANE_CACHE_TIMEOUT"`
	TurnLeaseDuration      time.Duration `env:"CONTROL_PLANE_TURN_LEASE_DURATION"`
	ScheduleClaimLimit     int           `env:"CONTROL_PLANE_SCHEDULE_CLAIM_LIMIT"`
	RelayPollInterval      time.Duration `env:"CONTROL_PLANE_RELAY_POLL_INTERVAL"`
	RelayLeaseDuration     time.Duration `env:"CONTROL_PLANE_RELAY_LEASE_DURATION"`
	RelayPublishTimeout    time.Duration `env:"CONTROL_PLANE_RELAY_PUBLISH_TIMEOUT"`
	RelayFinalizeTimeout   time.Duration `env:"CONTROL_PLANE_RELAY_FINALIZE_TIMEOUT"`
}

func loadConfig() (Config, error) {
	config := Config{
		GRPCListen:             ":8443",
		TechnicalListen:        ":9090",
		ServerCertificateFile:  "/var/run/secrets/mattercodex/control-plane/workload-tls/tls.crt",
		ServerPrivateKeyFile:   "/var/run/secrets/mattercodex/control-plane/workload-tls/tls.key",
		ClientCAFile:           "/var/run/config/mattercodex/control-plane/internal-ca/ca.pem",
		PostgresDSNFile:        "/var/run/secrets/mattercodex/control-plane/postgres-runtime/dsn",
		PostgresRelayDSNFile:   "/var/run/secrets/mattercodex/control-plane/postgres-relay/dsn",
		PostgresTLSServerName:  "control-plane-postgresql.mattercodex-system.svc.cluster.local",
		PostgresCAFile:         "/var/run/config/mattercodex/control-plane/postgres/ca.pem",
		PostgresMaxConnections: 16,
		RedisAddress:           "control-plane-redis.mattercodex-system.svc:6379",
		RedisTLSServerName:     "control-plane-redis.mattercodex-system.svc.cluster.local",
		RedisCAFile:            "/var/run/config/mattercodex/control-plane/redis/ca.pem",
		RedisUsername:          "control-plane",
		RedisPasswordFile:      "/var/run/secrets/mattercodex/control-plane/redis/password",
		RedisDatabase:          0,
		RedisPoolSize:          16,
		NATSURL:                "tls://nats.mattercodex-system.svc:4222",
		NATSTLSServerName:      "nats.mattercodex-system.svc.cluster.local",
		NATSCAFile:             "/var/run/config/mattercodex/control-plane/nats/ca.pem",
		NATSCredentialsFile:    "/var/run/secrets/mattercodex/control-plane/nats/user.creds",
		NATSStream:             "CONTROL_PLANE",
		NATSReplicas:           3,
		AuthorityPolicyFile:    "/var/run/config/mattercodex/control-plane/authority/policy.json",
		ProofPrivateJWKFile:    "/var/run/secrets/mattercodex/internal-rpc-authority/proof-signer/private.jwk",
		ProofTrustFile:         "/var/run/config/mattercodex/internal-rpc-authority/authority-proof-trust/jwks.json",
		ProofSignerGeneration:  1,
		LeaseSigningKeyFile:    "/var/run/secrets/mattercodex/control-plane/lease-signing/key",
		OIDCTLSServerName:      "sso.mattercodex.local",
		OIDCCAFile:             "/var/run/config/mattercodex/control-plane/oidc/ca.pem",
		StartupTimeout:         15 * time.Second,
		ReadinessTimeout:       2 * time.Second,
		ReadinessInterval:      10 * time.Second,
		ShutdownTimeout:        10 * time.Second,
		CacheTTL:               30 * time.Second,
		CacheTimeout:           100 * time.Millisecond,
		TurnLeaseDuration:      30 * time.Second,
		ScheduleClaimLimit:     64,
		RelayPollInterval:      250 * time.Millisecond,
		RelayLeaseDuration:     10 * time.Second,
		RelayPublishTimeout:    2 * time.Second,
		RelayFinalizeTimeout:   2 * time.Second,
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
	for _, endpoint := range []string{
		config.GRPCListen,
		config.TechnicalListen,
		config.RedisAddress,
	} {
		if _, _, err := net.SplitHostPort(endpoint); err != nil {
			return errors.New("control-plane endpoint is invalid")
		}
	}
	if !strings.HasPrefix(config.NATSURL, "tls://") ||
		config.NATSStream != "CONTROL_PLANE" ||
		config.NATSReplicas < 1 || config.NATSReplicas > 5 ||
		config.PostgresMaxConnections < 2 ||
		config.PostgresMaxConnections > 64 ||
		config.RedisPoolSize < 1 || config.RedisPoolSize > 64 ||
		config.RedisDatabase < 0 || config.RedisDatabase > 15 ||
		config.ProofSignerGeneration == 0 ||
		config.InstanceID == "" || len(config.InstanceID) > 128 ||
		config.ScheduleClaimLimit < 1 || config.ScheduleClaimLimit > 128 {
		return errors.New("control-plane bounded configuration is invalid")
	}
	for _, serverName := range []string{
		config.PostgresTLSServerName,
		config.RedisTLSServerName,
		config.NATSTLSServerName,
		config.OIDCTLSServerName,
	} {
		if serverName == "" || net.ParseIP(serverName) != nil {
			return errors.New("control-plane TLS server name is invalid")
		}
	}
	for _, path := range []string{
		config.ServerCertificateFile,
		config.ServerPrivateKeyFile,
		config.ClientCAFile,
		config.PostgresDSNFile,
		config.PostgresRelayDSNFile,
		config.PostgresCAFile,
		config.RedisCAFile,
		config.RedisPasswordFile,
		config.NATSCAFile,
		config.NATSCredentialsFile,
		config.AuthorityPolicyFile,
		config.ProofPrivateJWKFile,
		config.ProofTrustFile,
		config.LeaseSigningKeyFile,
		config.OIDCCAFile,
	} {
		if !filepath.IsAbs(path) {
			return errors.New("control-plane runtime path must be absolute")
		}
	}
	if config.StartupTimeout < time.Second ||
		config.StartupTimeout > time.Minute ||
		config.ReadinessTimeout < 100*time.Millisecond ||
		config.ReadinessTimeout > 5*time.Second ||
		config.ReadinessInterval < time.Second ||
		config.ReadinessInterval > time.Minute ||
		config.ShutdownTimeout < time.Second ||
		config.ShutdownTimeout > 30*time.Second ||
		config.CacheTTL <= 0 || config.CacheTTL > time.Minute ||
		config.CacheTimeout < 10*time.Millisecond ||
		config.CacheTimeout > time.Second ||
		config.TurnLeaseDuration < 5*time.Second ||
		config.TurnLeaseDuration > 5*time.Minute {
		return errors.New("control-plane duration is invalid")
	}
	return nil
}

func expectedOIDCOperations() map[string]string {
	return map[string]string{
		"control.project.create":      controlplanev1.ControlPlaneService_CreateProject_FullMethodName,
		"control.project.list":        controlplanev1.ControlPlaneService_ListProjects_FullMethodName,
		"control.resource.create":     controlplanev1.ControlPlaneService_CreateResource_FullMethodName,
		"control.resource.update":     controlplanev1.ControlPlaneService_UpdateResource_FullMethodName,
		"control.resource.transition": controlplanev1.ControlPlaneService_TransitionResource_FullMethodName,
		"control.resource.delete":     controlplanev1.ControlPlaneService_DeleteResource_FullMethodName,
		"control.resource.get":        controlplanev1.ControlPlaneService_GetResource_FullMethodName,
		"control.resource.list":       controlplanev1.ControlPlaneService_ListResources_FullMethodName,
		"control.turn.enqueue":        controlplanev1.ControlPlaneService_EnqueueTurn_FullMethodName,
		"control.owner-gate.resolve":  controlplanev1.ControlPlaneService_ResolveOwnerGate_FullMethodName,
	}
}
