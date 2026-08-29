package app

import (
	"errors"
	"net"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
)

const (
	controlPlaneTarget        = "dns:///control-plane.kodex-system.svc:8443"
	controlPlaneTLSServerName = "control-plane.kodex-system.svc.cluster.local"
	callbackTLSServerName     = "runtime-controller-callback.kodex-system.svc.cluster.local"
	defaultControlNamespace   = "kodex-system"
	defaultRuntimeNamespace   = "kodex-runtime"
	runtimeCallbackClientID   = "spiffe://kodex.local/ns/kodex-runtime/sa/agent-runner"
)

var sha256TextPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

type Config struct {
	Environment                    string        `env:"DEPLOYMENT_ENVIRONMENT"`
	ControlNamespace               string        `env:"POD_NAMESPACE"`
	RuntimeNamespace               string        `env:"RUNTIME_CONTROLLER_RUNTIME_NAMESPACE"`
	PodUID                         string        `env:"POD_UID"`
	PodIP                          string        `env:"POD_IP"`
	TechnicalListen                string        `env:"RUNTIME_CONTROLLER_TECHNICAL_LISTEN"`
	CallbackListen                 string        `env:"RUNTIME_CONTROLLER_CALLBACK_LISTEN"`
	CallbackTLSServerName          string        `env:"RUNTIME_CONTROLLER_CALLBACK_TLS_SERVER_NAME"`
	CallbackServerCertificateFile  string        `env:"RUNTIME_CONTROLLER_CALLBACK_SERVER_CERTIFICATE_FILE"`
	CallbackServerPrivateKeyFile   string        `env:"RUNTIME_CONTROLLER_CALLBACK_SERVER_PRIVATE_KEY_FILE"`
	CallbackClientCAFile           string        `env:"RUNTIME_CONTROLLER_CALLBACK_CLIENT_CA_FILE"`
	CallbackExpectedClientSPIFFEID string        `env:"RUNTIME_CONTROLLER_CALLBACK_EXPECTED_CLIENT_SPIFFE_ID"`
	CallbackClientCASecret         string        `env:"RUNTIME_CONTROLLER_CALLBACK_CLIENT_CA_SECRET"`
	CallbackClientTLSSecret        string        `env:"RUNTIME_CONTROLLER_CALLBACK_CLIENT_TLS_SECRET"`
	ControlPlaneTarget             string        `env:"RUNTIME_CONTROLLER_CONTROL_PLANE_TARGET"`
	ControlPlaneTLSServerName      string        `env:"RUNTIME_CONTROLLER_CONTROL_PLANE_TLS_SERVER_NAME"`
	ControlPlaneCAFile             string        `env:"RUNTIME_CONTROLLER_CONTROL_PLANE_CA_FILE"`
	ControlPlaneCertificateFile    string        `env:"RUNTIME_CONTROLLER_CONTROL_PLANE_CERTIFICATE_FILE"`
	ControlPlanePrivateKeyFile     string        `env:"RUNTIME_CONTROLLER_CONTROL_PLANE_PRIVATE_KEY_FILE"`
	ApplicationGrantFile           string        `env:"RUNTIME_CONTROLLER_APPLICATION_GRANT_FILE"`
	PromotedRoleImageRepository    string        `env:"RUNTIME_CONTROLLER_PROMOTED_ROLE_IMAGE_REPOSITORY"`
	DefaultRoleImageReference      string        `env:"RUNTIME_CONTROLLER_DEFAULT_ROLE_IMAGE_REFERENCE"`
	RoleRuntimeContractRevision    uint64        `env:"RUNTIME_CONTROLLER_ROLE_RUNTIME_CONTRACT_REVISION"`
	RoleRuntimeContractSHA256      string        `env:"RUNTIME_CONTROLLER_ROLE_RUNTIME_CONTRACT_SHA256"`
	ProviderHTTPSProxy             string        `env:"RUNTIME_CONTROLLER_PROVIDER_HTTPS_PROXY"`
	StorageClass                   string        `env:"RUNTIME_CONTROLLER_STORAGE_CLASS"`
	SessionPVCSize                 string        `env:"RUNTIME_CONTROLLER_SESSION_PVC_SIZE"`
	RunnerServiceAccount           string        `env:"RUNTIME_CONTROLLER_RUNNER_SERVICE_ACCOUNT"`
	MaximumConcurrentTurns         int           `env:"RUNTIME_CONTROLLER_MAXIMUM_CONCURRENT_TURNS"`
	TurnCPUMilli                   int64         `env:"RUNTIME_CONTROLLER_TURN_CPU_MILLI"`
	TurnMemoryBytes                int64         `env:"RUNTIME_CONTROLLER_TURN_MEMORY_BYTES"`
	PollInterval                   time.Duration `env:"RUNTIME_CONTROLLER_POLL_INTERVAL"`
	InfrastructureCheckInterval    time.Duration `env:"RUNTIME_CONTROLLER_INFRASTRUCTURE_CHECK_INTERVAL"`
	LeaseRenewInterval             time.Duration `env:"RUNTIME_CONTROLLER_LEASE_RENEW_INTERVAL"`
	RequestTimeout                 time.Duration `env:"RUNTIME_CONTROLLER_REQUEST_TIMEOUT"`
	ExecutionTimeout               time.Duration `env:"RUNTIME_CONTROLLER_EXECUTION_TIMEOUT"`
	ShutdownTimeout                time.Duration `env:"RUNTIME_CONTROLLER_SHUTDOWN_TIMEOUT"`
	WarmLongPoll                   time.Duration `env:"RUNTIME_CONTROLLER_WARM_LONG_POLL"`
}

func loadConfig() (Config, error) {
	config := Config{
		Environment: "development", ControlNamespace: defaultControlNamespace, RuntimeNamespace: defaultRuntimeNamespace,
		TechnicalListen: ":9090", CallbackListen: ":8444", CallbackTLSServerName: callbackTLSServerName,
		CallbackServerCertificateFile:  "/var/run/secrets/kodex/runtime-controller/callback-server/tls.crt",
		CallbackServerPrivateKeyFile:   "/var/run/secrets/kodex/runtime-controller/callback-server/tls.key",
		CallbackClientCAFile:           "/var/run/config/kodex/runtime-controller/callback-client/ca.crt",
		CallbackExpectedClientSPIFFEID: runtimeCallbackClientID,
		CallbackClientCASecret:         "runtime-execution-client-tls", CallbackClientTLSSecret: "runtime-execution-client-tls",
		ControlPlaneTarget: controlPlaneTarget, ControlPlaneTLSServerName: controlPlaneTLSServerName,
		ControlPlaneCAFile:          "/var/run/config/kodex/runtime-controller/control-plane/ca.pem",
		ControlPlaneCertificateFile: "/var/run/secrets/kodex/runtime-controller/workload-tls/tls.crt",
		ControlPlanePrivateKeyFile:  "/var/run/secrets/kodex/runtime-controller/workload-tls/tls.key",
		ApplicationGrantFile:        "/var/run/secrets/kodex/runtime-controller/application-grant/application-grant.jws",
		DefaultRoleImageReference:   "registry-pull.invalid/kodex/agent-runner@sha256:" + strings.Repeat("0", 64),
		ProviderHTTPSProxy:          "http://egress-gateway.kodex-system.svc:8080",
		StorageClass:                "", SessionPVCSize: "20Gi",
		RunnerServiceAccount: "agent-runner", MaximumConcurrentTurns: 16, TurnCPUMilli: 2000, TurnMemoryBytes: 4 << 30,
		PollInterval: 500 * time.Millisecond, InfrastructureCheckInterval: 10 * time.Second,
		LeaseRenewInterval: 10 * time.Second, RequestTimeout: 5 * time.Second,
		ExecutionTimeout: 60 * time.Minute, ShutdownTimeout: 30 * time.Second, WarmLongPoll: 20 * time.Second,
	}
	if err := env.Parse(&config); err != nil {
		return Config{}, err
	}
	return config, config.validate()
}

func (config Config) validate() error {
	if config.PodUID == "" || len(config.PodUID) > 128 || net.ParseIP(config.PodIP) == nil ||
		config.ControlNamespace != defaultControlNamespace || config.RuntimeNamespace != defaultRuntimeNamespace ||
		config.ControlNamespace == config.RuntimeNamespace || config.Environment == "" || config.ControlPlaneTarget != controlPlaneTarget ||
		config.ControlPlaneTLSServerName != controlPlaneTLSServerName || config.CallbackTLSServerName != callbackTLSServerName ||
		config.CallbackExpectedClientSPIFFEID != runtimeCallbackClientID {
		return errors.New("runtime controller identity is invalid")
	}
	for _, address := range []string{config.TechnicalListen, config.CallbackListen} {
		if _, _, err := net.SplitHostPort(address); err != nil {
			return errors.New("runtime controller listen address is invalid")
		}
	}
	for _, fileName := range []string{config.CallbackServerCertificateFile, config.CallbackServerPrivateKeyFile,
		config.CallbackClientCAFile, config.ControlPlaneCAFile, config.ControlPlaneCertificateFile,
		config.ControlPlanePrivateKeyFile, config.ApplicationGrantFile} {
		if !filepath.IsAbs(fileName) {
			return errors.New("runtime controller file path is invalid")
		}
	}
	spiffe, err := url.Parse(config.CallbackExpectedClientSPIFFEID)
	if err != nil || spiffe.Scheme != "spiffe" || spiffe.Host == "" || spiffe.Path == "" || spiffe.RawQuery != "" || spiffe.Fragment != "" {
		return errors.New("runtime callback client identity is invalid")
	}
	proxy, proxyErr := url.Parse(config.ProviderHTTPSProxy)
	if !validDNSLabel(config.CallbackClientCASecret) || !validDNSLabel(config.CallbackClientTLSSecret) ||
		(config.StorageClass != "" && !validDNSSubdomain(config.StorageClass)) || !validDNSLabel(config.RunnerServiceAccount) ||
		proxyErr != nil || proxy.Scheme != "http" || proxy.Host != "egress-gateway.kodex-system.svc:8080" || proxy.Path != "" || proxy.RawQuery != "" || proxy.Fragment != "" || proxy.User != nil ||
		!strings.Contains(config.PromotedRoleImageRepository, "/") || strings.ContainsAny(config.PromotedRoleImageRepository, "@${}") ||
		!validPinnedImageReference(config.DefaultRoleImageReference) ||
		config.RoleRuntimeContractRevision == 0 || !sha256TextPattern.MatchString(config.RoleRuntimeContractSHA256) {
		return errors.New("runtime role image policy is invalid")
	}
	if config.MaximumConcurrentTurns < 1 || config.MaximumConcurrentTurns > 128 || config.TurnCPUMilli < 100 ||
		config.TurnCPUMilli > 16000 || config.TurnMemoryBytes < 128<<20 || config.TurnMemoryBytes > 64<<30 ||
		config.PollInterval < 100*time.Millisecond || config.PollInterval > 10*time.Second ||
		config.InfrastructureCheckInterval < 5*time.Second || config.InfrastructureCheckInterval > time.Minute ||
		config.LeaseRenewInterval < time.Second || config.LeaseRenewInterval > 20*time.Second ||
		config.RequestTimeout < time.Second || config.RequestTimeout > 10*time.Second ||
		config.ExecutionTimeout < time.Minute || config.ExecutionTimeout > 4*time.Hour ||
		config.ShutdownTimeout < 5*time.Second || config.ShutdownTimeout > time.Minute ||
		config.WarmLongPoll < time.Second || config.WarmLongPoll > 30*time.Second {
		return errors.New("runtime controller bounded configuration is invalid")
	}
	return nil
}

func validPinnedImageReference(reference string) bool {
	separator := strings.LastIndex(reference, "@sha256:")
	return separator > 0 && separator+len("@sha256:")+64 == len(reference) &&
		sha256TextPattern.MatchString(reference[separator+len("@sha256:"):]) &&
		!strings.ContainsAny(reference[:separator], "${}")
}

func validDNSLabel(value string) bool {
	return regexp.MustCompile(`^[a-z0-9](?:[-a-z0-9]{0,61}[a-z0-9])?$`).MatchString(value)
}

func validDNSSubdomain(value string) bool {
	if value == "" || len(value) > 253 {
		return false
	}
	for _, label := range strings.Split(value, ".") {
		if !validDNSLabel(label) {
			return false
		}
	}
	return true
}
