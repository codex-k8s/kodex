package admissioncontroller

import (
	"errors"
	"net"
	"path/filepath"
	"time"
)

// Config задаёт только локальную Kubernetes-границу orchestration. Registry,
// signing и owner credentials принадлежат создаваемым phase Jobs.
type Config struct {
	Environment         string
	Namespace           string
	PolicyConfigMap     string
	PauseNewRuns        bool
	HoldProofJobs       bool
	ProofHoldUntil      time.Time
	RendererPath        string
	TechnicalListen     string
	ReconcileInterval   time.Duration
	RetryInterval       time.Duration
	InfrastructureCheck time.Duration
	RequestTimeout      time.Duration
}

func (config Config) Validate() error {
	if config.Environment != "staging" && config.Environment != "production" ||
		config.Namespace != "kodex-system" ||
		!validPolicyName(config.PolicyConfigMap) ||
		!filepath.IsAbs(config.RendererPath) || filepath.Clean(config.RendererPath) != config.RendererPath ||
		config.HoldProofJobs && config.ProofHoldUntil.IsZero() {
		return errors.New("image admission controller identity is invalid")
	}
	if _, _, err := net.SplitHostPort(config.TechnicalListen); err != nil {
		return errors.New("image admission controller technical endpoint is invalid")
	}
	if config.ReconcileInterval < time.Second || config.ReconcileInterval > time.Minute ||
		config.RetryInterval < 10*time.Second || config.RetryInterval > 10*time.Minute ||
		config.InfrastructureCheck < 5*time.Second || config.InfrastructureCheck > time.Minute ||
		config.RequestTimeout < time.Second || config.RequestTimeout > 15*time.Second {
		return errors.New("image admission controller timing is invalid")
	}
	return nil
}

func filepathIsCanonicalAbsolute(path string) bool {
	return filepath.IsAbs(path) && filepath.Clean(path) == path
}
