package app

import (
	"errors"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/codex-k8s/kodex/services/jobs/role-image-builder/internal/admissioncontroller"
)

type admissionControllerConfig struct {
	Environment         string        `env:"DEPLOYMENT_ENVIRONMENT"`
	Namespace           string        `env:"POD_NAMESPACE"`
	PolicyConfigMap     string        `env:"IMAGE_ADMISSION_CONTROLLER_POLICY_CONFIG_MAP"`
	RendererPath        string        `env:"IMAGE_ADMISSION_CONTROLLER_RENDERER_PATH"`
	TechnicalListen     string        `env:"IMAGE_ADMISSION_CONTROLLER_TECHNICAL_LISTEN"`
	ReconcileInterval   time.Duration `env:"IMAGE_ADMISSION_CONTROLLER_RECONCILE_INTERVAL"`
	RetryInterval       time.Duration `env:"IMAGE_ADMISSION_CONTROLLER_RETRY_INTERVAL"`
	InfrastructureCheck time.Duration `env:"IMAGE_ADMISSION_CONTROLLER_INFRASTRUCTURE_CHECK_INTERVAL"`
	RequestTimeout      time.Duration `env:"IMAGE_ADMISSION_CONTROLLER_REQUEST_TIMEOUT"`
	ShutdownTimeout     time.Duration `env:"IMAGE_ADMISSION_CONTROLLER_SHUTDOWN_TIMEOUT"`
}

func loadAdmissionControllerConfig() (admissionControllerConfig, error) {
	config := admissionControllerConfig{
		Namespace: "kodex-system", PolicyConfigMap: "kodex-image-admission-policy",
		RendererPath: "/opt/kodex/render-image-admission-job.sh", TechnicalListen: ":9090",
		ReconcileInterval: 5 * time.Second, RetryInterval: 30 * time.Second,
		InfrastructureCheck: 10 * time.Second, RequestTimeout: 5 * time.Second, ShutdownTimeout: 20 * time.Second,
	}
	if err := env.Parse(&config); err != nil {
		return admissionControllerConfig{}, err
	}
	if err := config.controllerConfig().Validate(); err != nil {
		return admissionControllerConfig{}, err
	}
	if config.ShutdownTimeout < 5*time.Second || config.ShutdownTimeout > time.Minute {
		return admissionControllerConfig{}, errors.New("image admission controller shutdown timeout is invalid")
	}
	return config, nil
}

func (config admissionControllerConfig) controllerConfig() admissioncontroller.Config {
	return admissioncontroller.Config{
		Environment: config.Environment, Namespace: config.Namespace, PolicyConfigMap: config.PolicyConfigMap,
		RendererPath: config.RendererPath, TechnicalListen: config.TechnicalListen,
		ReconcileInterval: config.ReconcileInterval, RetryInterval: config.RetryInterval,
		InfrastructureCheck: config.InfrastructureCheck, RequestTimeout: config.RequestTimeout,
	}
}
