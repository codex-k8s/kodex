package callback

import (
	"errors"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// Эта эфемерная диагностическая запись не является owner store. Любое чтение
// выполняется только после fresh leased CP read; restart возвращает UNKNOWN.
type providerProcessExecution struct {
	binding     runtimecontract.ProviderProcessObservation
	generation  int64
	fence       string
	observation *runtimecontract.ProviderProcessObservation
}

// Вызывается под coordinator.mu только из прежнего Register exact owner input.
func (coordinator *Coordinator) registerProviderExecution(input runtimecontract.RunnerInput) {
	if coordinator.providerExecutions == nil {
		coordinator.providerExecutions = make(map[string]providerProcessExecution)
	}
	binding := runtimecontract.BindProviderProcessObservation(input, "")
	current, exists := coordinator.providerExecutions[input.LeaseRef]
	// Поздняя registration не откатывает локальное diagnostic поколение.
	// Это не authority watermark: свежий CP lease read обязателен независимо.
	if exists && input.LeaseGeneration <= current.generation {
		return
	}
	if !exists || current.binding != binding || current.generation != input.LeaseGeneration || current.fence != input.LeaseFence {
		coordinator.providerExecutions[input.LeaseRef] = providerProcessExecution{binding: binding, generation: input.LeaseGeneration, fence: input.LeaseFence}
	}
}

func (coordinator *Coordinator) recordProviderProcess(input runtimecontract.RunnerInput, observation runtimecontract.ProviderProcessObservation) error {
	invalid := errors.New("provider process observation binding is invalid")
	if coordinator == nil || !observation.Matches(input) {
		return invalid
	}
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	current, exists := coordinator.providerExecutions[input.LeaseRef]
	if !exists || current.binding != runtimecontract.BindProviderProcessObservation(input, "") ||
		current.generation != input.LeaseGeneration || current.fence != input.LeaseFence ||
		(current.observation != nil && *current.observation != observation) {
		return invalid
	}
	value := observation
	current.observation = &value
	coordinator.providerExecutions[input.LeaseRef] = current
	return nil
}

func (coordinator *Coordinator) providerProcessSnapshot(input runtimecontract.RunnerInput) map[string]any {
	unknown := map[string]any{"status": "UNKNOWN", "provider": "CODEX"}
	if coordinator == nil {
		return unknown
	}
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	current, exists := coordinator.providerExecutions[input.LeaseRef]
	if !exists || current.observation == nil || current.generation != input.LeaseGeneration || current.fence != input.LeaseFence ||
		!current.observation.Matches(input) {
		return unknown
	}
	return map[string]any{"status": "OBSERVED", "provider": "CODEX", "version": current.observation.Version, "observation_source": "INITIALIZE_USER_AGENT"}
}
