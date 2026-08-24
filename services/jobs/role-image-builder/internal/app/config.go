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
	Environment                  string        `env:"DEPLOYMENT_ENVIRONMENT"`
	TechnicalListen              string        `env:"ROLE_IMAGE_BUILDER_TECHNICAL_LISTEN"`
	ControlPlaneTarget           string        `env:"ROLE_IMAGE_BUILDER_CONTROL_PLANE_TARGET"`
	ControlPlaneTLSServerName    string        `env:"ROLE_IMAGE_BUILDER_CONTROL_PLANE_TLS_SERVER_NAME"`
	ControlPlaneCAFile           string        `env:"ROLE_IMAGE_BUILDER_CONTROL_PLANE_CA_FILE"`
	ControlPlaneCertificateFile  string        `env:"ROLE_IMAGE_BUILDER_CONTROL_PLANE_CERTIFICATE_FILE"`
	ControlPlanePrivateKeyFile   string        `env:"ROLE_IMAGE_BUILDER_CONTROL_PLANE_PRIVATE_KEY_FILE"`
	ApplicationGrantFile         string        `env:"ROLE_IMAGE_BUILDER_APPLICATION_GRANT_FILE"`
	BuildKitBinary               string        `env:"ROLE_IMAGE_BUILDER_BUILDKIT_BINARY"`
	BuildKitAddress              string        `env:"ROLE_IMAGE_BUILDER_BUILDKIT_ADDRESS"`
	BuildKitTLSServerName        string        `env:"ROLE_IMAGE_BUILDER_BUILDKIT_TLS_SERVER_NAME"`
	BuildKitCAFile               string        `env:"ROLE_IMAGE_BUILDER_BUILDKIT_CA_FILE"`
	BuildKitCertificateFile      string        `env:"ROLE_IMAGE_BUILDER_BUILDKIT_CERTIFICATE_FILE"`
	BuildKitPrivateKeyFile       string        `env:"ROLE_IMAGE_BUILDER_BUILDKIT_PRIVATE_KEY_FILE"`
	BuildKitPullDockerConfig     string        `env:"ROLE_IMAGE_BUILDER_BUILDKIT_PULL_DOCKER_CONFIG"`
	WorkspaceRoot                string        `env:"ROLE_IMAGE_BUILDER_WORKSPACE_ROOT"`
	InputDockerConfig            string        `env:"ROLE_IMAGE_BUILDER_INPUT_DOCKER_CONFIG"`
	InputRepository              string        `env:"ROLE_IMAGE_BUILDER_INPUT_REPOSITORY"`
	InputRegistryTLSServerName   string        `env:"ROLE_IMAGE_BUILDER_INPUT_REGISTRY_TLS_SERVER_NAME"`
	InputRegistryCAFile          string        `env:"ROLE_IMAGE_BUILDER_INPUT_REGISTRY_CA_FILE"`
	InputRegistryCertificateFile string        `env:"ROLE_IMAGE_BUILDER_INPUT_REGISTRY_CERTIFICATE_FILE"`
	InputRegistryPrivateKeyFile  string        `env:"ROLE_IMAGE_BUILDER_INPUT_REGISTRY_PRIVATE_KEY_FILE"`
	AllowedRoleBaseImagesFile    string        `env:"ROLE_IMAGE_BUILDER_ALLOWED_ROLE_BASE_IMAGES_FILE"`
	TrustedRoleBaseRepository    string        `env:"ROLE_IMAGE_BUILDER_TRUSTED_ROLE_BASE_REPOSITORY"`
	TrustedRoleBaseDigest        string        `env:"ROLE_IMAGE_BUILDER_TRUSTED_ROLE_BASE_DIGEST"`
	FrontendRepository           string        `env:"ROLE_IMAGE_BUILDER_FRONTEND_REPOSITORY"`
	StagingRepository            string        `env:"ROLE_IMAGE_BUILDER_STAGING_REPOSITORY"`
	ExpectedBuilderSHA256        string        `env:"ROLE_IMAGE_BUILDER_EXPECTED_BUILDER_SHA256"`
	ExpectedFrontendSHA256       string        `env:"ROLE_IMAGE_BUILDER_EXPECTED_FRONTEND_SHA256"`
	ExpectedToolchainSHA256      string        `env:"ROLE_IMAGE_BUILDER_EXPECTED_TOOLCHAIN_SHA256"`
	RoleRuntimeContractRevision  uint64        `env:"ROLE_IMAGE_BUILDER_ROLE_RUNTIME_CONTRACT_REVISION"`
	RoleRuntimeContractSHA256    string        `env:"ROLE_IMAGE_BUILDER_ROLE_RUNTIME_CONTRACT_SHA256"`
	StartupTimeout               time.Duration `env:"ROLE_IMAGE_BUILDER_STARTUP_TIMEOUT"`
	ShutdownTimeout              time.Duration `env:"ROLE_IMAGE_BUILDER_SHUTDOWN_TIMEOUT"`
	RPCDeadline                  time.Duration `env:"ROLE_IMAGE_BUILDER_RPC_DEADLINE"`
	PollInterval                 time.Duration `env:"ROLE_IMAGE_BUILDER_POLL_INTERVAL"`
	RenewInterval                time.Duration `env:"ROLE_IMAGE_BUILDER_RENEW_INTERVAL"`
	ReadinessInterval            time.Duration `env:"ROLE_IMAGE_BUILDER_READINESS_INTERVAL"`
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
		BuildKitTLSServerName:        "mattercodex-buildkit.mattercodex-system.svc.cluster.local",
		BuildKitCAFile:               "/var/run/secrets/mattercodex/role-image-builder/buildkit/ca.pem",
		BuildKitCertificateFile:      "/var/run/secrets/mattercodex/role-image-builder/buildkit/tls.crt",
		BuildKitPrivateKeyFile:       "/var/run/secrets/mattercodex/role-image-builder/buildkit/tls.key",
		BuildKitPullDockerConfig:     "/var/run/secrets/mattercodex/role-image-builder/base-pull/config.json",
		WorkspaceRoot:                "/var/run/mattercodex/work",
		InputDockerConfig:            "/var/run/secrets/mattercodex/role-image-builder/input-read/config.json",
		InputRepository:              "mattercodex-image-registry.mattercodex-system.svc.cluster.local:5000/mattercodex/role-image-inputs",
		InputRegistryTLSServerName:   "mattercodex-image-registry.mattercodex-system.svc.cluster.local",
		InputRegistryCAFile:          "/var/run/secrets/mattercodex/role-image-builder/input-read/ca.pem",
		InputRegistryCertificateFile: "/var/run/secrets/mattercodex/role-image-builder/input-read/tls.crt",
		InputRegistryPrivateKeyFile:  "/var/run/secrets/mattercodex/role-image-builder/input-read/tls.key",
		AllowedRoleBaseImagesFile:    "/var/run/config/mattercodex/role-image-builder/role-environments/catalog.json",
		TrustedRoleBaseRepository:    "mattercodex-image-registry.mattercodex-system.svc.cluster.local:5000/mattercodex/agent-runner",
		TrustedRoleBaseDigest:        "sha256:0000000000000000000000000000000000000000000000000000000000000000",
		FrontendRepository:           "mattercodex-image-registry.mattercodex-system.svc.cluster.local:5000/mattercodex/dockerfile",
		StagingRepository:            "mattercodex-image-registry-push.mattercodex-system.svc.cluster.local:5001/staging/role-images",
		ExpectedBuilderSHA256:        "8c2ce26a3722e0cf4514fad4cfcd0e0f0f16214219ca7b73f3e1fcef74640ac4",
		ExpectedFrontendSHA256:       "0000000000000000000000000000000000000000000000000000000000000000",
		ExpectedToolchainSHA256:      "0000000000000000000000000000000000000000000000000000000000000000",
		RoleRuntimeContractRevision:  1,
		RoleRuntimeContractSHA256:    "0000000000000000000000000000000000000000000000000000000000000000",
		StartupTimeout:               30 * time.Second, ShutdownTimeout: 20 * time.Second, RPCDeadline: 5 * time.Second,
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
		config.BuildKitPullDockerConfig,
		config.WorkspaceRoot, config.InputDockerConfig, config.InputRegistryCAFile,
		config.InputRegistryCertificateFile, config.InputRegistryPrivateKeyFile} {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return errors.New("role image builder path is invalid")
		}
	}
	if !filepath.IsAbs(config.AllowedRoleBaseImagesFile) ||
		filepath.Clean(config.AllowedRoleBaseImagesFile) != config.AllowedRoleBaseImagesFile {
		return errors.New("role image builder path is invalid")
	}
	if config.StartupTimeout < 5*time.Second || config.StartupTimeout > 2*time.Minute ||
		config.ShutdownTimeout < 5*time.Second || config.ShutdownTimeout > time.Minute ||
		config.RPCDeadline < time.Second || config.RPCDeadline > 15*time.Second ||
		config.PollInterval < 250*time.Millisecond || config.PollInterval > time.Minute ||
		config.RenewInterval < 5*time.Second || config.RenewInterval > time.Minute ||
		config.ReadinessInterval < time.Second || config.ReadinessInterval > time.Minute ||
		config.InputRepository == "" || strings.ContainsAny(config.InputRepository, "@?# \r\n\t") ||
		config.InputRegistryTLSServerName == "" || strings.ContainsAny(config.InputRegistryTLSServerName, "*/:@?# \r\n\t") ||
		config.TrustedRoleBaseRepository == "" || strings.ContainsAny(config.TrustedRoleBaseRepository, "@?# \r\n\t") ||
		!validManifestDigest(config.TrustedRoleBaseDigest) ||
		config.FrontendRepository == "" || strings.ContainsAny(config.FrontendRepository, "@?# \r\n\t") ||
		config.StagingRepository == "" || strings.ContainsAny(config.StagingRepository, "@?# \r\n\t") ||
		!validSHA256(config.ExpectedBuilderSHA256) || !validSHA256(config.ExpectedFrontendSHA256) ||
		!validSHA256(config.ExpectedToolchainSHA256) || config.RoleRuntimeContractRevision == 0 ||
		!validSHA256(config.RoleRuntimeContractSHA256) ||
		config.ExpectedToolchainSHA256 == strings.Repeat("0", 64) {
		return errors.New("role image builder bounded configuration is invalid")
	}
	return nil
}

func validSHA256(value string) bool {
	return len(value) == 64 && strings.Trim(value, "0123456789abcdef") == ""
}

func validManifestDigest(value string) bool {
	return strings.HasPrefix(value, "sha256:") && validSHA256(strings.TrimPrefix(value, "sha256:"))
}
