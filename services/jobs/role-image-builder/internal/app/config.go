package app

import (
	"errors"
	"net"
	"path/filepath"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
)

const (
	serviceName      = "role-image-builder"
	metricsSubsystem = "role_image_builder"
)

type Config struct {
	Environment                 string        `env:"DEPLOYMENT_ENVIRONMENT"`
	TechnicalListen             string        `env:"ROLE_IMAGE_BUILDER_TECHNICAL_LISTEN"`
	ControlPlaneTarget          string        `env:"ROLE_IMAGE_BUILDER_CONTROL_PLANE_TARGET"`
	ControlPlaneTLSServerName   string        `env:"ROLE_IMAGE_BUILDER_CONTROL_PLANE_TLS_SERVER_NAME"`
	ControlPlaneCAFile          string        `env:"ROLE_IMAGE_BUILDER_CONTROL_PLANE_CA_FILE"`
	ControlPlaneCertificateFile string        `env:"ROLE_IMAGE_BUILDER_CONTROL_PLANE_CERTIFICATE_FILE"`
	ControlPlanePrivateKeyFile  string        `env:"ROLE_IMAGE_BUILDER_CONTROL_PLANE_PRIVATE_KEY_FILE"`
	ApplicationGrantFile        string        `env:"ROLE_IMAGE_BUILDER_APPLICATION_GRANT_FILE"`
	BuildKitBinary              string        `env:"ROLE_IMAGE_BUILDER_BUILDKIT_BINARY"`
	BuildKitAddress             string        `env:"ROLE_IMAGE_BUILDER_BUILDKIT_ADDRESS"`
	BuildKitTLSServerName       string        `env:"ROLE_IMAGE_BUILDER_BUILDKIT_TLS_SERVER_NAME"`
	BuildKitCAFile              string        `env:"ROLE_IMAGE_BUILDER_BUILDKIT_CA_FILE"`
	BuildKitCertificateFile     string        `env:"ROLE_IMAGE_BUILDER_BUILDKIT_CERTIFICATE_FILE"`
	BuildKitPrivateKeyFile      string        `env:"ROLE_IMAGE_BUILDER_BUILDKIT_PRIVATE_KEY_FILE"`
	ContextRoot                 string        `env:"ROLE_IMAGE_BUILDER_CONTEXT_ROOT"`
	SecretRoot                  string        `env:"ROLE_IMAGE_BUILDER_SECRET_ROOT"`
	WorkspaceRoot               string        `env:"ROLE_IMAGE_BUILDER_WORKSPACE_ROOT"`
	BaseDockerConfig            string        `env:"ROLE_IMAGE_BUILDER_BASE_DOCKER_CONFIG"`
	StagingDockerConfig         string        `env:"ROLE_IMAGE_BUILDER_STAGING_DOCKER_CONFIG"`
	FrontendRepository          string        `env:"ROLE_IMAGE_BUILDER_FRONTEND_REPOSITORY"`
	StagingRepository           string        `env:"ROLE_IMAGE_BUILDER_STAGING_REPOSITORY"`
	ExpectedBuilderSHA256       string        `env:"ROLE_IMAGE_BUILDER_EXPECTED_BUILDER_SHA256"`
	ExpectedToolchainSHA256     string        `env:"ROLE_IMAGE_BUILDER_EXPECTED_TOOLCHAIN_SHA256"`
	StartupTimeout              time.Duration `env:"ROLE_IMAGE_BUILDER_STARTUP_TIMEOUT"`
	ShutdownTimeout             time.Duration `env:"ROLE_IMAGE_BUILDER_SHUTDOWN_TIMEOUT"`
	RPCDeadline                 time.Duration `env:"ROLE_IMAGE_BUILDER_RPC_DEADLINE"`
	PollInterval                time.Duration `env:"ROLE_IMAGE_BUILDER_POLL_INTERVAL"`
	RenewInterval               time.Duration `env:"ROLE_IMAGE_BUILDER_RENEW_INTERVAL"`
	ReadinessInterval           time.Duration `env:"ROLE_IMAGE_BUILDER_READINESS_INTERVAL"`
}

func loadConfig() (Config, error) {
	config := Config{
		TechnicalListen: ":9090", ControlPlaneTarget: "control-plane.mattercodex-system.svc:8443",
		ControlPlaneTLSServerName:   "control-plane.mattercodex-system.svc.cluster.local",
		ControlPlaneCAFile:          "/var/run/config/mattercodex/role-image-builder/control-plane/ca.pem",
		ControlPlaneCertificateFile: "/var/run/secrets/mattercodex/role-image-builder/workload-tls/tls.crt",
		ControlPlanePrivateKeyFile:  "/var/run/secrets/mattercodex/role-image-builder/workload-tls/tls.key",
		ApplicationGrantFile:        "/var/run/secrets/mattercodex/role-image-builder/application-grant/application-grant.jws",
		BuildKitBinary:              "/usr/bin/buildctl", BuildKitAddress: "tcp://mattercodex-buildkit.mattercodex-system.svc.cluster.local:1234",
		BuildKitTLSServerName:   "mattercodex-buildkit.mattercodex-system.svc.cluster.local",
		BuildKitCAFile:          "/var/run/secrets/mattercodex/role-image-builder/buildkit/ca.pem",
		BuildKitCertificateFile: "/var/run/secrets/mattercodex/role-image-builder/buildkit/tls.crt",
		BuildKitPrivateKeyFile:  "/var/run/secrets/mattercodex/role-image-builder/buildkit/tls.key",
		ContextRoot:             "/var/run/mattercodex/image-contexts", SecretRoot: "/var/run/secrets/mattercodex/role-image-builder/build-secrets",
		WorkspaceRoot: "/var/run/mattercodex/work", BaseDockerConfig: "/var/run/secrets/mattercodex/role-image-builder/base-pull/config.json",
		StagingDockerConfig:     "/var/run/secrets/mattercodex/role-image-builder/staging-push/config.json",
		FrontendRepository:      "mattercodex-image-registry.mattercodex-system.svc.cluster.local:5000/mattercodex/dockerfile",
		StagingRepository:       "mattercodex-image-registry-push.mattercodex-system.svc.cluster.local:5001/staging/role-images",
		ExpectedBuilderSHA256:   "995077ff90af1afff56ff23018699d7511d122b2b111041f2011bd12afd5c0fe",
		ExpectedToolchainSHA256: "0000000000000000000000000000000000000000000000000000000000000000",
		StartupTimeout:          30 * time.Second, ShutdownTimeout: 20 * time.Second, RPCDeadline: 5 * time.Second,
		PollInterval: time.Second, RenewInterval: 20 * time.Second, ReadinessInterval: 10 * time.Second,
	}
	if err := env.Parse(&config); err != nil {
		return Config{}, err
	}
	return config, config.validate()
}

func (config Config) validate() error {
	if config.Environment != "staging" && config.Environment != "production" {
		return errors.New("role image builder environment is invalid")
	}
	if _, _, err := net.SplitHostPort(config.TechnicalListen); err != nil {
		return errors.New("role image builder technical endpoint is invalid")
	}
	if _, _, err := net.SplitHostPort(config.ControlPlaneTarget); err != nil {
		return errors.New("role image builder control-plane endpoint is invalid")
	}
	if !strings.HasPrefix(config.BuildKitAddress, "tcp://") || strings.ContainsAny(config.BuildKitTLSServerName, "*/:@ ") ||
		strings.ContainsAny(config.ControlPlaneTLSServerName, "*/:@ ") {
		return errors.New("role image builder TLS endpoint is invalid")
	}
	for _, path := range []string{config.ControlPlaneCAFile, config.ControlPlaneCertificateFile,
		config.ControlPlanePrivateKeyFile, config.ApplicationGrantFile, config.BuildKitBinary,
		config.BuildKitCAFile, config.BuildKitCertificateFile, config.BuildKitPrivateKeyFile,
		config.ContextRoot, config.SecretRoot, config.WorkspaceRoot, config.BaseDockerConfig, config.StagingDockerConfig} {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return errors.New("role image builder path is invalid")
		}
	}
	if config.StartupTimeout < 5*time.Second || config.StartupTimeout > 2*time.Minute ||
		config.ShutdownTimeout < 5*time.Second || config.ShutdownTimeout > time.Minute ||
		config.RPCDeadline < time.Second || config.RPCDeadline > 15*time.Second ||
		config.PollInterval < 250*time.Millisecond || config.PollInterval > time.Minute ||
		config.RenewInterval < 5*time.Second || config.RenewInterval > time.Minute ||
		config.ReadinessInterval < time.Second || config.ReadinessInterval > time.Minute ||
		config.FrontendRepository == "" || strings.ContainsAny(config.FrontendRepository, "@?# \r\n\t") ||
		config.StagingRepository == "" || strings.ContainsAny(config.StagingRepository, "@?# \r\n\t") ||
		!validSHA256(config.ExpectedBuilderSHA256) || !validSHA256(config.ExpectedToolchainSHA256) ||
		config.ExpectedToolchainSHA256 == strings.Repeat("0", 64) {
		return errors.New("role image builder bounded configuration is invalid")
	}
	return nil
}

func validSHA256(value string) bool {
	return len(value) == 64 && strings.Trim(value, "0123456789abcdef") == ""
}
