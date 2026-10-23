package admissioncontroller

import (
	"errors"
	"net"
	"path/filepath"
	"time"
)

// Config defines the controller's local orchestration identity, work source,
// hold/drain choices and bounded timings. Registry/signing/owner credentials belong
// to phase Jobs and are not carried or granted by this configuration value.
// Config задаёт локальные identity, work source, hold/drain и ограниченные интервалы
// контроллера. Registry/signing/owner credentials принадлежат phase Jobs
// и не передаются либо выдаются этим значением конфигурации.
type Config struct {
	WorkSource          WorkSource
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

// Validate checks the closed environment/namespace, policy name, lexical renderer
// path, required hold deadline, host:port shape and timing bounds. It does not
// verify files, network reachability or work-source readiness; New checks the
// required work source separately and reconciliation enforces hold expiry.
// Validate проверяет закрытые environment/namespace, имя policy, лексический путь
// renderer, наличие hold deadline, host:port и пределы интервалов. Файлы, сеть
// и готовность work source не проверяются; обязательный work source проверяет New,
// а истечение hold — reconcile.
func (config Config) Validate() error {
	if config.Environment != "staging" && config.Environment != "production" ||
		config.Namespace != "kodex-system" ||
		!validPolicyName(config.PolicyConfigMap) ||
		!filepathIsCanonicalAbsolute(config.RendererPath) ||
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

// filepathIsCanonicalAbsolute accepts an already-clean absolute OS path without
// filesystem I/O. It neither resolves symlinks nor proves existence, executable
// permissions or authority; callers retain those separate execution boundaries.
// filepathIsCanonicalAbsolute принимает уже чистый абсолютный путь ОС без I/O.
// Ссылки не разрешаются, существование, executable permissions и authority
// не доказываются; эти отдельные границы исполнения остаются у вызывающего кода.
func filepathIsCanonicalAbsolute(path string) bool {
	return filepath.IsAbs(path) && filepath.Clean(path) == path
}
