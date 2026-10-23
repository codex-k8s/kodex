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
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/transportprofile"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

const (
	// controlPlaneTarget pins the only control-plane gRPC DNS endpoint accepted by configuration validation.
	// controlPlaneTarget закрепляет единственный DNS-адрес gRPC control-plane, допустимый при проверке конфигурации.
	controlPlaneTarget = "dns:///control-plane.kodex-system.svc:8443"
	// controlPlaneTLSServerName identifies the expected control-plane certificate name independently of its target address.
	// controlPlaneTLSServerName задаёт ожидаемое имя сертификата control-plane отдельно от адреса подключения.
	controlPlaneTLSServerName = "control-plane.kodex-system.svc.cluster.local"
	// secretBrokerTarget fixes credential projection routing to the in-cluster secret-broker gRPC endpoint.
	// secretBrokerTarget фиксирует маршрут проекции учётных данных на внутрикластерный адрес gRPC secret-broker.
	secretBrokerTarget = "dns:///secret-broker.kodex-system.svc:8443"
	// secretBrokerTLSServerName pins the broker certificate name; routing to its service does not by itself establish identity.
	// secretBrokerTLSServerName закрепляет имя сертификата брокера; маршрут к сервису сам по себе не подтверждает идентичность.
	secretBrokerTLSServerName = "secret-broker.kodex-system.svc.cluster.local"
	// callbackTLSServerName fixes the callback server certificate identity expected by runtime runners.
	// callbackTLSServerName фиксирует идентичность сертификата сервера обратных вызовов, ожидаемую исполнителями.
	callbackTLSServerName = "runtime-controller-callback.kodex-system.svc.cluster.local"
	// defaultControlNamespace names the required control-service namespace, kept distinct from runtime workloads.
	// defaultControlNamespace задаёт обязательное пространство управляющих сервисов, отдельное от рабочих нагрузок выполнения.
	defaultControlNamespace = "kodex-system"
	// defaultRuntimeNamespace names the required namespace for runner workloads, separate from the control plane.
	// defaultRuntimeNamespace задаёт обязательное пространство рабочих нагрузок исполнителя, отдельное от управляющих сервисов.
	defaultRuntimeNamespace = "kodex-runtime"
	// runtimeCallbackClientID fixes the runner SPIFFE identity allowed by callback configuration validation.
	// It is an expected identity, not a credential issued by this constant.
	// runtimeCallbackClientID фиксирует SPIFFE-идентичность исполнителя, допустимую в настройках обратных вызовов.
	// Это ожидаемая идентичность, а не выдаваемые константой учётные данные.
	runtimeCallbackClientID = "spiffe://kodex.local/ns/kodex-runtime/sa/agent-runner"
)

// sha256TextPattern recognizes exactly 64 lowercase hexadecimal characters for configured image and runtime digests.
// Matching the expression does not verify the referenced bytes.
// sha256TextPattern распознаёт ровно 64 строчных шестнадцатеричных символа настроенных дайджестов образа и контракта выполнения.
// Совпадение с выражением не проверяет указанные байты.
var sha256TextPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

// Config holds runtime-controller identity, protected endpoint paths, image/runtime policy and bounded execution settings.
// Values refer to mounted credentials; parsing the structure does not read them or create workloads.
// Config хранит идентичность runtime-controller, пути защищённых подключений, правила образа/выполнения и ограниченные настройки запуска.
// Значения ссылаются на смонтированные учётные данные; разбор структуры их не читает и не создаёт нагрузки.
type Config struct {
	RPCProfile                     string        `env:"KODEX_RPC_PROFILE"`
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
	SecretBrokerTarget             string        `env:"RUNTIME_CONTROLLER_SECRET_BROKER_TARGET"`
	SecretBrokerTLSServerName      string        `env:"RUNTIME_CONTROLLER_SECRET_BROKER_TLS_SERVER_NAME"`
	SecretBrokerCAFile             string        `env:"RUNTIME_CONTROLLER_SECRET_BROKER_CA_FILE"`
	PromotedRoleImageRepository    string        `env:"RUNTIME_CONTROLLER_PROMOTED_ROLE_IMAGE_REPOSITORY"`
	DefaultRoleImageReference      string        `env:"RUNTIME_CONTROLLER_DEFAULT_ROLE_IMAGE_REFERENCE"`
	RoleRuntimeContractRevision    uint64        `env:"RUNTIME_CONTROLLER_ROLE_RUNTIME_CONTRACT_REVISION"`
	RoleRuntimeContractSHA256      string        `env:"RUNTIME_CONTROLLER_ROLE_RUNTIME_CONTRACT_SHA256"`
	ProviderHTTPSProxy             string        `env:"RUNTIME_CONTROLLER_PROVIDER_HTTPS_PROXY"`
	RuntimeEgressSigningKeyFile    string        `env:"RUNTIME_CONTROLLER_EGRESS_SIGNING_KEY_FILE"`
	RuntimeEgressCASecret          string        `env:"RUNTIME_CONTROLLER_EGRESS_CA_SECRET"`
	ProviderAppArmorProfile        string        `env:"RUNTIME_CONTROLLER_PROVIDER_APPARMOR_PROFILE"`
	StorageClass                   string        `env:"RUNTIME_CONTROLLER_STORAGE_CLASS"`
	SessionPVCSize                 string        `env:"RUNTIME_CONTROLLER_SESSION_PVC_SIZE"`
	RunnerServiceAccount           string        `env:"RUNTIME_CONTROLLER_RUNNER_SERVICE_ACCOUNT"`
	MaximumConcurrentTurns         int           `env:"RUNTIME_CONTROLLER_MAXIMUM_CONCURRENT_TURNS"`
	PollInterval                   time.Duration `env:"RUNTIME_CONTROLLER_POLL_INTERVAL"`
	InfrastructureCheckInterval    time.Duration `env:"RUNTIME_CONTROLLER_INFRASTRUCTURE_CHECK_INTERVAL"`
	LeaseRenewInterval             time.Duration `env:"RUNTIME_CONTROLLER_LEASE_RENEW_INTERVAL"`
	RequestTimeout                 time.Duration `env:"RUNTIME_CONTROLLER_REQUEST_TIMEOUT"`
	FileTransferTimeout            time.Duration `env:"RUNTIME_CONTROLLER_FILE_TRANSFER_TIMEOUT"`
	ArtifactSpoolDirectory         string        `env:"RUNTIME_CONTROLLER_ARTIFACT_SPOOL_DIRECTORY"`
	ExecutionTimeout               time.Duration `env:"RUNTIME_CONTROLLER_EXECUTION_TIMEOUT"`
	ShutdownTimeout                time.Duration `env:"RUNTIME_CONTROLLER_SHUTDOWN_TIMEOUT"`
	WarmLongPoll                   time.Duration `env:"RUNTIME_CONTROLLER_WARM_LONG_POLL"`
}

// loadConfig initializes controller defaults, overlays the process environment and returns configuration with its validation error.
// Unlike an environment parser failure, validation failure still returns the populated configuration; no dotenv file is read.
// loadConfig задаёт исходные настройки контроллера, применяет окружение процесса и возвращает конфигурацию с ошибкой её проверки.
// В отличие от ошибки разбора окружения, ошибка проверки возвращает заполненную конфигурацию; файл dotenv не читается.
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
		SecretBrokerTarget:          secretBrokerTarget,
		SecretBrokerTLSServerName:   secretBrokerTLSServerName,
		SecretBrokerCAFile:          "/var/run/config/kodex/runtime-controller/control-plane/ca.pem",
		DefaultRoleImageReference:   "registry-pull.invalid/kodex/agent-runner@sha256:" + strings.Repeat("0", 64),
		ProviderHTTPSProxy:          "http://egress-gateway.kodex-system.svc:8084",
		RuntimeEgressSigningKeyFile: "/var/run/secrets/kodex/runtime-controller/egress-signing/key",
		RuntimeEgressCASecret:       "runtime-egress-proxy-ca",
		ProviderAppArmorProfile:     "",
		StorageClass:                "", SessionPVCSize: "20Gi",
		RunnerServiceAccount: "agent-runner", MaximumConcurrentTurns: 16,
		PollInterval: 500 * time.Millisecond, InfrastructureCheckInterval: 10 * time.Second,
		LeaseRenewInterval: 10 * time.Second, RequestTimeout: 5 * time.Second,
		FileTransferTimeout:    runtimecontract.MaximumArtifactTransferDuration,
		ArtifactSpoolDirectory: "/var/lib/kodex/runtime-controller/artifact-spool",
		ExecutionTimeout:       60 * time.Minute, ShutdownTimeout: 30 * time.Second, WarmLongPoll: 20 * time.Second,
	}
	if err := env.Parse(&config); err != nil {
		return Config{}, err
	}
	return config, config.validate()
}

// validate enforces fixed service identities and separate namespaces, absolute credential paths, the managed proxy,
// image/contract syntax and bounded timing/concurrency. It performs no file reads, connection probes or image admission.
// validate требует фиксированных идентичностей сервисов и раздельных пространств имён, абсолютных путей учётных данных, управляемого прокси,
// синтаксиса образа/контракта и ограниченных времени и параллелизма. Файлы не читаются, подключения и допуск образа не проверяются.
func (config Config) validate() error {
	if config.RPCProfile != "" && config.RPCProfile != transportprofile.TrustedCluster {
		return errors.New("runtime controller RPC profile is invalid")
	}
	if config.PodUID == "" || len(config.PodUID) > 128 || net.ParseIP(config.PodIP) == nil ||
		config.ControlNamespace != defaultControlNamespace || config.RuntimeNamespace != defaultRuntimeNamespace ||
		config.ControlNamespace == config.RuntimeNamespace || config.Environment == "" || config.ControlPlaneTarget != controlPlaneTarget ||
		config.ControlPlaneTLSServerName != controlPlaneTLSServerName || config.CallbackTLSServerName != callbackTLSServerName ||
		config.CallbackExpectedClientSPIFFEID != runtimeCallbackClientID || config.SecretBrokerTarget != secretBrokerTarget ||
		config.SecretBrokerTLSServerName != secretBrokerTLSServerName {
		return errors.New("runtime controller identity is invalid")
	}
	for _, address := range []string{config.TechnicalListen, config.CallbackListen} {
		if _, _, err := net.SplitHostPort(address); err != nil {
			return errors.New("runtime controller listen address is invalid")
		}
	}
	for _, fileName := range []string{config.CallbackServerCertificateFile, config.CallbackServerPrivateKeyFile,
		config.CallbackClientCAFile, config.ControlPlaneCAFile, config.ControlPlaneCertificateFile,
		config.ControlPlanePrivateKeyFile, config.ApplicationGrantFile, config.SecretBrokerCAFile} {
		if !filepath.IsAbs(fileName) {
			return errors.New("runtime controller file path is invalid")
		}
	}
	if !filepath.IsAbs(config.RuntimeEgressSigningKeyFile) || !validDNSLabel(config.RuntimeEgressCASecret) {
		return errors.New("runtime egress signing key path is invalid")
	}
	if !filepath.IsAbs(config.ArtifactSpoolDirectory) || filepath.Clean(config.ArtifactSpoolDirectory) != config.ArtifactSpoolDirectory {
		return errors.New("runtime artifact spool directory is invalid")
	}
	spiffe, err := url.Parse(config.CallbackExpectedClientSPIFFEID)
	if err != nil || spiffe.Scheme != "spiffe" || spiffe.Host == "" || spiffe.Path == "" || spiffe.RawQuery != "" || spiffe.Fragment != "" {
		return errors.New("runtime callback client identity is invalid")
	}
	proxy, proxyErr := url.Parse(config.ProviderHTTPSProxy)
	if !validDNSLabel(config.CallbackClientCASecret) || !validDNSLabel(config.CallbackClientTLSSecret) ||
		(config.StorageClass != "" && !validDNSSubdomain(config.StorageClass)) || !validDNSLabel(config.RunnerServiceAccount) ||
		proxyErr != nil || proxy.Scheme != "http" || proxy.Host != "egress-gateway.kodex-system.svc:8084" || proxy.Path != "" || proxy.RawQuery != "" || proxy.Fragment != "" || proxy.User != nil ||
		!strings.Contains(config.PromotedRoleImageRepository, "/") || strings.ContainsAny(config.PromotedRoleImageRepository, "@${}") ||
		!validPinnedImageReference(config.DefaultRoleImageReference) ||
		(config.ProviderAppArmorProfile != "" && config.ProviderAppArmorProfile != "kodex-provider-runtime") ||
		config.RoleRuntimeContractRevision == 0 || !sha256TextPattern.MatchString(config.RoleRuntimeContractSHA256) {
		return errors.New("runtime role image policy is invalid")
	}
	if config.MaximumConcurrentTurns < 1 || config.MaximumConcurrentTurns > 128 ||
		config.PollInterval < 100*time.Millisecond || config.PollInterval > 10*time.Second ||
		config.InfrastructureCheckInterval < 5*time.Second || config.InfrastructureCheckInterval > time.Minute ||
		config.LeaseRenewInterval < time.Second || config.LeaseRenewInterval > 20*time.Second ||
		config.RequestTimeout < time.Second || config.RequestTimeout > 10*time.Second ||
		config.FileTransferTimeout < time.Second || config.FileTransferTimeout > runtimecontract.MaximumArtifactTransferDuration ||
		config.ExecutionTimeout < time.Minute || config.ExecutionTimeout > 4*time.Hour ||
		config.ShutdownTimeout < 5*time.Second || config.ShutdownTimeout > time.Minute ||
		config.WarmLongPoll < time.Second || config.WarmLongPoll > 30*time.Second {
		return errors.New("runtime controller bounded configuration is invalid")
	}
	return nil
}

// validPinnedImageReference requires a nonempty prefix, the last @sha256 separator and exactly 64 lowercase hex digits,
// excluding template placeholders in the prefix; it does not parse a complete repository grammar or establish admission.
// validPinnedImageReference требует непустого префикса, последнего разделителя @sha256 и ровно 64 строчных шестнадцатеричных цифр,
// запрещая шаблонные подстановки в префиксе; полная грамматика репозитория и допуск не проверяются.
func validPinnedImageReference(reference string) bool {
	separator := strings.LastIndex(reference, "@sha256:")
	return separator > 0 && separator+len("@sha256:")+64 == len(reference) &&
		sha256TextPattern.MatchString(reference[separator+len("@sha256:"):]) &&
		!strings.ContainsAny(reference[:separator], "${}")
}

// validDNSLabel uses the shared bounded ASCII grammar for configured Kubernetes resource names;
// resource existence and access are not established by this lexical check.
// validDNSLabel использует общую ограниченную ASCII-грамматику имён ресурсов Kubernetes;
// существование ресурса и доступ к нему этой лексической проверкой не подтверждаются.
func validDNSLabel(value string) bool {
	return runtimecontract.ValidDNSLabel(value)
}

// validDNSSubdomain accepts a nonempty name of at most 253 bytes only when every dot-separated part is a valid DNS label.
// Empty labels, including leading or trailing dots, are rejected without DNS lookup.
// validDNSSubdomain принимает непустое имя не длиннее 253 байт, только если каждая часть между точками является корректной DNS-меткой.
// Пустые метки, включая начальную или конечную точку, отклоняются без DNS-запроса.
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
