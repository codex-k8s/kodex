package app

import (
	"errors"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/transportprofile"
	"github.com/codex-k8s/kodex/services/jobs/role-image-builder/internal/admissioncontroller"
)

// admissionControllerConfig holds environment-bound controller, Pod identity, policy, RPC-path and lifecycle settings.
// Credential fields are file paths only; work-source transport initialization performs its own detailed checks.
// admissionControllerConfig хранит настройки контроллера, идентичности Pod, политики, RPC-пути и жизненного цикла из окружения.
// Поля реквизитов содержат только пути; подробную проверку транспорта источника работы выполняет его создание.
type admissionControllerConfig struct {
	RPCProfile                  string        `env:"KODEX_RPC_PROFILE"`
	Environment                 string        `env:"DEPLOYMENT_ENVIRONMENT"`
	ControlPlaneTarget          string        `env:"IMAGE_ADMISSION_CONTROLLER_CONTROL_PLANE_TARGET"`
	ControlPlaneTLSServerName   string        `env:"IMAGE_ADMISSION_CONTROLLER_CONTROL_PLANE_TLS_SERVER_NAME"`
	ControlPlaneCAFile          string        `env:"IMAGE_ADMISSION_CONTROLLER_CONTROL_PLANE_CA_FILE"`
	ControlPlaneCertificateFile string        `env:"IMAGE_ADMISSION_CONTROLLER_CONTROL_PLANE_CERTIFICATE_FILE"`
	ControlPlanePrivateKeyFile  string        `env:"IMAGE_ADMISSION_CONTROLLER_CONTROL_PLANE_PRIVATE_KEY_FILE"`
	ApplicationGrantFile        string        `env:"IMAGE_ADMISSION_CONTROLLER_APPLICATION_GRANT_FILE"`
	Namespace                   string        `env:"POD_NAMESPACE"`
	PodUID                      string        `env:"POD_UID"`
	PolicyConfigMap             string        `env:"IMAGE_ADMISSION_CONTROLLER_POLICY_CONFIG_MAP"`
	PauseNewRuns                bool          `env:"IMAGE_ADMISSION_CONTROLLER_PAUSE_NEW_RUNS"`
	HoldProofJobs               bool          `env:"IMAGE_ADMISSION_CONTROLLER_HOLD_PROOF_JOBS"`
	ProofHoldUntil              time.Time     `env:"IMAGE_ADMISSION_CONTROLLER_PROOF_HOLD_UNTIL"`
	RendererPath                string        `env:"IMAGE_ADMISSION_CONTROLLER_RENDERER_PATH"`
	TechnicalListen             string        `env:"IMAGE_ADMISSION_CONTROLLER_TECHNICAL_LISTEN"`
	ReconcileInterval           time.Duration `env:"IMAGE_ADMISSION_CONTROLLER_RECONCILE_INTERVAL"`
	RetryInterval               time.Duration `env:"IMAGE_ADMISSION_CONTROLLER_RETRY_INTERVAL"`
	InfrastructureCheck         time.Duration `env:"IMAGE_ADMISSION_CONTROLLER_INFRASTRUCTURE_CHECK_INTERVAL"`
	RequestTimeout              time.Duration `env:"IMAGE_ADMISSION_CONTROLLER_REQUEST_TIMEOUT"`
	ShutdownTimeout             time.Duration `env:"IMAGE_ADMISSION_CONTROLLER_SHUTDOWN_TIMEOUT"`
}

// loadAdmissionControllerConfig overlays environment onto fixed defaults and validates controller shape, Pod UID, profile and shutdown bounds.
// Empty/protected and trusted-cluster profiles retain their existing behavior; no identity file or Kubernetes resource is read here.
// loadAdmissionControllerConfig накладывает окружение на фиксированные умолчания и проверяет форму контроллера, Pod UID, профиль и предел остановки.
// Пустой protected-профиль и trusted-cluster сохраняют прежнее поведение; файлы идентичности и ресурсы Kubernetes здесь не читаются.
func loadAdmissionControllerConfig() (admissionControllerConfig, error) {
	config := admissionControllerConfig{
		Namespace: "kodex-system", PolicyConfigMap: "kodex-image-admission-policy",
		ControlPlaneTarget:          "control-plane.kodex-system.svc:8443",
		ControlPlaneTLSServerName:   "control-plane.kodex-system.svc.cluster.local",
		ControlPlaneCAFile:          "/var/run/config/kodex/image-admission-controller/control-plane/ca.pem",
		ControlPlaneCertificateFile: "/var/run/secrets/kodex/image-admission-controller/workload-tls/tls.crt",
		ControlPlanePrivateKeyFile:  "/var/run/secrets/kodex/image-admission-controller/workload-tls/tls.key",
		ApplicationGrantFile:        "/var/run/secrets/kodex/image-admission-controller/application-grant/application-grant.jws",
		RendererPath:                "/opt/kodex/render-image-admission-job.sh", TechnicalListen: ":9090",
		ReconcileInterval: 5 * time.Second, RetryInterval: 30 * time.Second,
		InfrastructureCheck: 10 * time.Second, RequestTimeout: 5 * time.Second, ShutdownTimeout: 20 * time.Second,
	}
	if err := env.Parse(&config); err != nil {
		return admissionControllerConfig{}, err
	}
	if err := config.controllerConfig().Validate(); err != nil {
		return admissionControllerConfig{}, err
	}
	if !admissioncontroller.ValidPodUID(config.PodUID) {
		return admissionControllerConfig{}, errors.New("image admission controller Pod UID is invalid")
	}
	if config.RPCProfile != "" && config.RPCProfile != transportprofile.TrustedCluster {
		return admissionControllerConfig{}, errors.New("image admission controller RPC profile is invalid")
	}
	if config.ShutdownTimeout < 5*time.Second || config.ShutdownTimeout > time.Minute {
		return admissionControllerConfig{}, errors.New("image admission controller shutdown timeout is invalid")
	}
	return config, nil
}

// workSourceConfig projects the caller's RPC endpoint/identity paths with fixed issuer IDs and request-bound dial/RPC budgets.
// It does not dial, read the grant or prove availability while constructing this value.
// workSourceConfig проецирует заданные RPC endpoint и пути идентичности с фиксированными issuer IDs и бюджетами dial и RPC из RequestTimeout.
// При создании значения соединение не устанавливается, grant не читается и доступность не доказывается.
func (config admissionControllerConfig) workSourceConfig() admissioncontroller.WorkSourceConfig {
	return admissioncontroller.WorkSourceConfig{
		RPCProfile: config.RPCProfile, Target: config.ControlPlaneTarget,
		TLSServerName: config.ControlPlaneTLSServerName, CAFile: config.ControlPlaneCAFile,
		ClientCertificateFile: config.ControlPlaneCertificateFile,
		ClientPrivateKeyFile:  config.ControlPlanePrivateKeyFile,
		ApplicationGrantFile:  config.ApplicationGrantFile,
		ExpectedIssuerUID:     29001, ExpectedIssuerGID: 29000,
		DialTimeout: config.RequestTimeout, RPCDeadline: config.RequestTimeout,
	}
}

// controllerConfig copies policy, renderer and timing choices without selecting a WorkSource or configuring leadership.
// Those runtime dependencies are assigned explicitly by RunAdmissionController after loading this projection.
// controllerConfig копирует выбор политики, renderer и интервалов без выбора WorkSource или настройки лидерства.
// Эти зависимости среды явно назначаются RunAdmissionController после загрузки проекции.
func (config admissionControllerConfig) controllerConfig() admissioncontroller.Config {
	return admissioncontroller.Config{
		Environment: config.Environment, Namespace: config.Namespace, PolicyConfigMap: config.PolicyConfigMap,
		PauseNewRuns: config.PauseNewRuns, HoldProofJobs: config.HoldProofJobs, ProofHoldUntil: config.ProofHoldUntil,
		RendererPath: config.RendererPath, TechnicalListen: config.TechnicalListen,
		ReconcileInterval: config.ReconcileInterval, RetryInterval: config.RetryInterval,
		InfrastructureCheck: config.InfrastructureCheck, RequestTimeout: config.RequestTimeout,
	}
}
