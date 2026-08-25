package app

import (
	"sync"

	sharedobservability "github.com/codex-k8s/kodex/libs/go/observability"
	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
	internalobservability "github.com/codex-k8s/kodex/services/external/egress-gateway/internal/observability"
	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/policy"
)

const (
	processBooting  = "BOOTING"
	processReady    = "READY"
	processNotReady = "NOT_READY"
	processDraining = "DRAINING"
	processStopped  = "STOPPED"
)

type state struct {
	mu            sync.RWMutex
	process       string
	policyState   string
	revision      string
	digest        string
	resolverState string
	readiness     *serviceruntime.Readiness
	metrics       *sharedobservability.Metrics
}

func newState(activePolicy *policy.Active, readiness *serviceruntime.Readiness, metrics *sharedobservability.Metrics, business *internalobservability.Metrics) *state {
	value := &state{
		process: processBooting, policyState: "ACTIVE", revision: activePolicy.Revision(), digest: activePolicy.Digest(),
		resolverState: "REJECTED", readiness: readiness, metrics: metrics,
	}
	business.SetPolicyActive(true)
	value.publishReadiness()
	return value
}

func newDegradedState(activePolicy *policy.Active, readiness *serviceruntime.Readiness, metrics *sharedobservability.Metrics, business *internalobservability.Metrics) *state {
	value := &state{
		process: processNotReady, policyState: "ACTIVE", revision: activePolicy.Revision(), digest: activePolicy.Digest(),
		resolverState: "INVALID", readiness: readiness, metrics: metrics,
	}
	business.SetPolicyActive(true)
	value.publishReadiness()
	return value
}

func newInvalidPolicyState(readiness *serviceruntime.Readiness, metrics *sharedobservability.Metrics, business *internalobservability.Metrics) *state {
	value := &state{
		process: processNotReady, policyState: "INVALID", resolverState: "INVALID",
		readiness: readiness, metrics: metrics,
	}
	business.SetPolicyActive(false)
	value.publishReadiness()
	return value
}

func (value *state) Ready() (bool, string) { return value.readiness.Ready() }

func (value *state) setProcess(process string) {
	value.mu.Lock()
	if (value.process == processDraining || value.process == processStopped) && process != processStopped {
		value.mu.Unlock()
		return
	}
	value.process = process
	value.mu.Unlock()
	value.publishReadiness()
}

func (value *state) setResolverReady(ready bool) {
	value.mu.Lock()
	if ready {
		value.resolverState = "VALIDATED"
	} else {
		value.resolverState = "REJECTED"
	}
	value.mu.Unlock()
	value.publishReadiness()
}

func (value *state) publishReadiness() {
	value.mu.RLock()
	ready := value.process == processReady && value.policyState == "ACTIVE" && value.resolverState == "VALIDATED"
	value.mu.RUnlock()
	reason := "not_ready"
	if ready {
		reason = "ready"
	}
	value.readiness.Set(ready, reason)
	value.metrics.SetReady(ready)
}

func (value *state) readback() policyReadback {
	value.mu.RLock()
	defer value.mu.RUnlock()
	return policyReadback{
		ProcessState: value.process, PolicyState: value.policyState,
		Revision: value.revision, Digest: value.digest, ResolverState: value.resolverState,
	}
}

type policyReadback struct {
	ProcessState  string `json:"processState"`
	PolicyState   string `json:"policyState"`
	Revision      string `json:"revision"`
	Digest        string `json:"digest"`
	ResolverState string `json:"resolverState"`
}
