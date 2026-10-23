package app

import (
	"sync"

	sharedobservability "github.com/codex-k8s/kodex/libs/go/observability"
	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
	internalobservability "github.com/codex-k8s/kodex/services/external/egress-gateway/internal/observability"
	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/policy"
)

// Process states are the closed lifecycle vocabulary exposed by policy readback.
// Состояния процесса составляют закрытый словарь жизненного цикла для readback.
const (
	processBooting  = "BOOTING"
	processReady    = "READY"
	processNotReady = "NOT_READY"
	processDraining = "DRAINING"
	processStopped  = "STOPPED"
)

// state synchronizes effective policy, resolver, and process readiness.
// state синхронизирует готовность политики, resolver и процесса.
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

// newState begins an active-policy run as booting until DNS and listeners are ready.
// newState начинает запуск активной политики в состоянии загрузки до готовности DNS и listeners.
func newState(activePolicy *policy.Active, readiness *serviceruntime.Readiness, metrics *sharedobservability.Metrics, business *internalobservability.Metrics) *state {
	value := &state{
		process: processBooting, policyState: "ACTIVE", revision: activePolicy.Revision(), digest: activePolicy.Digest(),
		resolverState: "INVALID", readiness: readiness, metrics: metrics,
	}
	business.SetPolicyActive(true)
	value.publishReadiness()
	return value
}

// newDegradedState retains a valid policy readback when resolver startup fails.
// newDegradedState сохраняет состояние допустимой политики при сбое resolver.
func newDegradedState(activePolicy *policy.Active, readiness *serviceruntime.Readiness, metrics *sharedobservability.Metrics, business *internalobservability.Metrics) *state {
	value := &state{
		process: processNotReady, policyState: "ACTIVE", revision: activePolicy.Revision(), digest: activePolicy.Digest(),
		resolverState: "INVALID", readiness: readiness, metrics: metrics,
	}
	business.SetPolicyActive(true)
	value.publishReadiness()
	return value
}

// newInvalidPolicyState denies readiness without exposing an unverified policy.
// newInvalidPolicyState запрещает готовность и не выдаёт непроверенную политику.
func newInvalidPolicyState(readiness *serviceruntime.Readiness, metrics *sharedobservability.Metrics, business *internalobservability.Metrics) *state {
	value := &state{
		process: processNotReady, policyState: "INVALID", resolverState: "INVALID",
		readiness: readiness, metrics: metrics,
	}
	business.SetPolicyActive(false)
	value.publishReadiness()
	return value
}

// Ready reports the effective technical readiness, including its reason.
// Ready возвращает фактическую техническую готовность и её причину.
func (value *state) Ready() (bool, string) { return value.readiness.Ready() }

// setProcess advances process state without reviving a draining or stopped run.
// setProcess меняет состояние процесса, не возобновляя останавливающийся запуск.
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

// setResolverConfigured records validated resolver startup and republishes readiness.
// setResolverConfigured отмечает настроенный resolver и обновляет готовность.
func (value *state) setResolverConfigured() {
	value.mu.Lock()
	value.resolverState = "CONFIGURED"
	value.mu.Unlock()
	value.publishReadiness()
}

// publishReadiness combines process, policy, and resolver states into one signal.
// publishReadiness объединяет состояния процесса, политики и resolver в один сигнал.
func (value *state) publishReadiness() {
	value.mu.RLock()
	ready := value.process == processReady && value.policyState == "ACTIVE" && value.resolverState == "CONFIGURED"
	value.mu.RUnlock()
	reason := "not_ready"
	if ready {
		reason = "ready"
	}
	value.readiness.Set(ready, reason)
	value.metrics.SetReady(ready)
}

// readback copies the synchronized non-secret policy status for HTTP delivery.
// readback копирует синхронизированное состояние политики без секретов для HTTP.
func (value *state) readback() policyReadback {
	value.mu.RLock()
	defer value.mu.RUnlock()
	return policyReadback{
		ProcessState: value.process, PolicyState: value.policyState,
		Revision: value.revision, Digest: value.digest, ResolverState: value.resolverState,
	}
}

// policyReadback is the restricted JSON view of effective egress policy state.
// policyReadback задаёт ограниченный JSON-вид действующей egress-политики.
type policyReadback struct {
	ProcessState  string `json:"processState"`
	PolicyState   string `json:"policyState"`
	Revision      string `json:"revision"`
	Digest        string `json:"digest"`
	ResolverState string `json:"resolverState"`
}
