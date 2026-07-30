package app

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Mode string

const (
	ModeIssuer   Mode = "issuer"
	ModeVerifier Mode = "verifier"
)

const socketRoot = "/run/mattercodex/internal-rpc-authority"

type Config struct {
	Mode                        Mode
	ServiceName                 string
	WorkloadID                  string
	SocketPath                  string
	ExpectedProcessUID          uint32
	ExpectedProcessGID          uint32
	ExpectedPeerUID             uint32
	ExpectedPeerGID             uint32
	SocketMode                  os.FileMode
	TechnicalListen             string
	PostgresDSNFile             string
	PostgresTLSServerName       string
	PostgresExpectedSessionUser string
	DatabaseCapabilityRole      string
	PostgresMaxConnections      int32
	SnapshotJWSFile             string
	ManifestPublicJWKFile       string
	ContextPrivateJWKFile       string
	ProofTrustJWKFile           string
	ReadbackPrivateJWKFile      string
	StartupTimeout              time.Duration
	ReadinessTimeout            time.Duration
	ShutdownTimeout             time.Duration
	ReplayCleanupInterval       time.Duration
	ReplayRetentionAfterExpiry  time.Duration
}

func LoadConfig(mode Mode) (Config, error) {
	config := Config{
		Mode:                        mode,
		WorkloadID:                  strings.TrimSpace(os.Getenv("INTERNAL_RPC_AUTHORITY_WORKLOAD_ID")),
		ExpectedPeerUID:             10001,
		ExpectedPeerGID:             10001,
		SocketMode:                  0o660,
		TechnicalListen:             envOrDefault("INTERNAL_RPC_AUTHORITY_TECHNICAL_LISTEN", ":9090"),
		PostgresDSNFile:             envOrDefault("INTERNAL_RPC_AUTHORITY_POSTGRES_DSN_FILE", "/var/run/secrets/mattercodex/internal-rpc-authority/postgres/dsn"),
		PostgresTLSServerName:       envOrDefault("INTERNAL_RPC_AUTHORITY_POSTGRES_TLS_SERVER_NAME", "internal-rpc-authority-postgresql.mattercodex-system.svc.cluster.local"),
		PostgresExpectedSessionUser: strings.TrimSpace(os.Getenv("INTERNAL_RPC_AUTHORITY_POSTGRES_EXPECTED_SESSION_USER")),
		PostgresMaxConnections:      8,
		SnapshotJWSFile:             envOrDefault("INTERNAL_RPC_AUTHORITY_SNAPSHOT_JWS_FILE", "/var/run/config/mattercodex/internal-rpc-authority/snapshot/snapshot.jws"),
		ManifestPublicJWKFile:       envOrDefault("INTERNAL_RPC_AUTHORITY_MANIFEST_PUBLIC_JWK_FILE", "/var/run/config/mattercodex/internal-rpc-authority/manifest-trust/current.jwk"),
		ContextPrivateJWKFile:       envOrDefault("INTERNAL_RPC_AUTHORITY_CONTEXT_PRIVATE_JWK_FILE", "/var/run/secrets/mattercodex/internal-rpc-authority/issuer/private.jwk"),
		ProofTrustJWKFile:           envOrDefault("INTERNAL_RPC_AUTHORITY_PROOF_TRUST_JWK_FILE", "/var/run/config/mattercodex/internal-rpc-authority/authority-proof-trust/jwks.json"),
		ReadbackPrivateJWKFile:      envOrDefault("INTERNAL_RPC_AUTHORITY_READBACK_PRIVATE_JWK_FILE", "/var/run/secrets/mattercodex/internal-rpc-authority/readback/possession-key.jwk"),
		StartupTimeout:              15 * time.Second,
		ReadinessTimeout:            2 * time.Second,
		ShutdownTimeout:             10 * time.Second,
		ReplayCleanupInterval:       time.Minute,
		ReplayRetentionAfterExpiry:  10 * time.Minute,
	}
	switch mode {
	case ModeIssuer:
		config.ServiceName = "internal_rpc_authority_issuer"
		config.SocketPath = socketRoot + "/issuer.sock"
		config.ExpectedProcessUID = 29001
		config.ExpectedProcessGID = 29000
		config.DatabaseCapabilityRole = "internal_rpc_authority_issuer"
	case ModeVerifier:
		config.ServiceName = "internal_rpc_authority_verifier"
		config.SocketPath = socketRoot + "/verifier.sock"
		config.ExpectedProcessUID = 29002
		config.ExpectedProcessGID = 29000
		config.DatabaseCapabilityRole = "internal_rpc_authority_verifier"
	default:
		return Config{}, errors.New("unsupported internal-rpc-authority mode")
	}
	var err error
	if config.ExpectedPeerUID, err = uint32Env(
		"INTERNAL_RPC_AUTHORITY_EXPECTED_PEER_UID",
		config.ExpectedPeerUID,
	); err != nil {
		return Config{}, err
	}
	if config.ExpectedPeerGID, err = uint32Env(
		"INTERNAL_RPC_AUTHORITY_EXPECTED_PEER_GID",
		config.ExpectedPeerGID,
	); err != nil {
		return Config{}, err
	}
	if config.PostgresMaxConnections, err = int32Env(
		"INTERNAL_RPC_AUTHORITY_POSTGRES_MAX_CONNECTIONS",
		config.PostgresMaxConnections,
		1,
		32,
	); err != nil {
		return Config{}, err
	}
	if config.StartupTimeout, err = durationEnv(
		"INTERNAL_RPC_AUTHORITY_STARTUP_TIMEOUT",
		config.StartupTimeout,
		time.Second,
		time.Minute,
	); err != nil {
		return Config{}, err
	}
	if config.ReadinessTimeout, err = durationEnv(
		"INTERNAL_RPC_AUTHORITY_READINESS_TIMEOUT",
		config.ReadinessTimeout,
		100*time.Millisecond,
		5*time.Second,
	); err != nil {
		return Config{}, err
	}
	if config.ShutdownTimeout, err = durationEnv(
		"INTERNAL_RPC_AUTHORITY_SHUTDOWN_TIMEOUT",
		config.ShutdownTimeout,
		time.Second,
		30*time.Second,
	); err != nil {
		return Config{}, err
	}
	if config.ReplayCleanupInterval, err = durationEnv(
		"INTERNAL_RPC_AUTHORITY_REPLAY_CLEANUP_INTERVAL",
		config.ReplayCleanupInterval,
		10*time.Second,
		10*time.Minute,
	); err != nil {
		return Config{}, err
	}
	if err := config.Validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}

func (config Config) Validate() error {
	if config.WorkloadID == "" || len(config.WorkloadID) > 96 {
		return errors.New("INTERNAL_RPC_AUTHORITY_WORKLOAD_ID is required and bounded")
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
		"PostgresDSNFile":        config.PostgresDSNFile,
		"SnapshotJWSFile":        config.SnapshotJWSFile,
		"ManifestPublicJWKFile":  config.ManifestPublicJWKFile,
		"ReadbackPrivateJWKFile": config.ReadbackPrivateJWKFile,
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

func envOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func uint32Env(name string, fallback uint32) (uint32, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseUint(raw, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("%s must be uint32: %w", name, err)
	}
	return uint32(value), nil
}

func int32Env(name string, fallback, minimum, maximum int32) (int32, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseInt(raw, 10, 32)
	if err != nil || value < int64(minimum) || value > int64(maximum) {
		return 0, fmt.Errorf("%s is outside the allowed boundary", name)
	}
	return int32(value), nil
}

func durationEnv(
	name string,
	fallback time.Duration,
	minimum time.Duration,
	maximum time.Duration,
) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value < minimum || value > maximum {
		return 0, fmt.Errorf("%s is outside the allowed duration boundary", name)
	}
	return value, nil
}
