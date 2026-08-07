package runtime

import (
	"sync"

	"github.com/codex-k8s/matter-codex/services/external/egress-gateway/internal/observability"
	"github.com/codex-k8s/matter-codex/services/external/egress-gateway/internal/policy"
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
	metrics       *observability.Metrics
}

func newState(activePolicy *policy.Active, metrics *observability.Metrics) *state {
	value := &state{
		process: processBooting, policyState: "ACTIVE", revision: activePolicy.Revision(), digest: activePolicy.Digest(),
		resolverState: "REJECTED", metrics: metrics,
	}
	metrics.SetPolicyActive(true)
	metrics.SetReady(false)
	return value
}

func newDegradedState(activePolicy *policy.Active, metrics *observability.Metrics) *state {
	value := &state{
		process: processNotReady, policyState: "ACTIVE", revision: activePolicy.Revision(), digest: activePolicy.Digest(),
		resolverState: "INVALID", metrics: metrics,
	}
	metrics.SetPolicyActive(true)
	metrics.SetReady(false)
	return value
}

func newInvalidPolicyState(metrics *observability.Metrics) *state {
	value := &state{process: processNotReady, policyState: "INVALID", resolverState: "INVALID", metrics: metrics}
	metrics.SetPolicyActive(false)
	metrics.SetReady(false)
	return value
}

func (value *state) setProcess(process string) {
	value.mu.Lock()
	if (value.process == processDraining || value.process == processStopped) && process != processStopped {
		value.mu.Unlock()
		return
	}
	value.process = process
	value.mu.Unlock()
	value.metrics.SetReady(value.ready())
}

func (value *state) ready() bool {
	value.mu.RLock()
	process := value.process
	resolverState := value.resolverState
	value.mu.RUnlock()
	return process == processReady && resolverState == "VALIDATED"
}

func (value *state) readback() policyReadback {
	value.mu.RLock()
	process := value.process
	resolverState := value.resolverState
	value.mu.RUnlock()
	return policyReadback{
		ProcessState: process, PolicyState: value.policyState,
		Revision: value.revision, Digest: value.digest, ResolverState: resolverState,
	}
}

func (value *state) setResolverReady(ready bool) {
	value.mu.Lock()
	if ready {
		value.resolverState = "VALIDATED"
	} else {
		value.resolverState = "REJECTED"
	}
	value.mu.Unlock()
	value.metrics.SetReady(value.ready())
}

type policyReadback struct {
	ProcessState  string `json:"processState"`
	PolicyState   string `json:"policyState"`
	Revision      string `json:"revision"`
	Digest        string `json:"digest"`
	ResolverState string `json:"resolverState"`
}
