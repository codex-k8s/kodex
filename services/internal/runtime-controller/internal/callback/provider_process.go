package callback

import (
	"errors"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// providerProcessExecution stores an ephemeral exact-bound process observation
// with its lease generation and fence. It is not the authority store or a durable
// replay watermark; callers still require a fresh leased owner read.
// providerProcessExecution хранит эфемерное точно привязанное наблюдение процесса
// с поколением и fence аренды. Это не источник полномочий и не устойчивый replay
// watermark; caller по-прежнему обязан получить свежий owner read по аренде.
type providerProcessExecution struct {
	binding     runtimecontract.ProviderProcessObservation
	generation  int64
	fence       string
	observation *runtimecontract.ProviderProcessObservation
}

// registerProviderExecution runs under coordinator.mu for an owner-resolved
// input, creating or resetting diagnostic state only for a newer generation.
// Late registration cannot roll it back; this local rule does not replace
// authoritative lease validation and registration invents no process version.
// registerProviderExecution вызывается под coordinator.mu для input владельца,
// создавая или сбрасывая диагностику только для более нового поколения.
// Поздняя регистрация её не откатывает; локальное правило не заменяет проверку
// аренды у владельца, а регистрация не придумывает версию процесса.
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

// recordProviderProcess accepts only a registered exact binding, generation
// and fence, copying the first valid observation under the mutex. Identical
// repeats succeed; conflicting observations fail without replacing the first.
// It records diagnostics only and performs no fresh owner read itself.
// recordProviderProcess принимает только зарегистрированные точные привязку,
// поколение и fence, копируя первое валидное наблюдение под mutex. Точный повтор
// успешен, конфликт отвергается без замены первого значения. Это лишь запись
// диагностики; свежий owner read внутри функции не выполняется.
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

// providerProcessSnapshot returns a fresh safe map for the exact registered
// observation or UNKNOWN, exposing only version/source/provider/status. It
// neither returns private lease pins nor infers a version after restart;
// callers must establish current owner eligibility before publishing it.
// providerProcessSnapshot возвращает новую безопасную map точного наблюдения
// либо UNKNOWN, раскрывая только версию, источник, провайдера и статус. Закрытые
// привязки не выдаются, версия после restart не угадывается; перед публикацией
// caller обязан подтвердить актуальную допустимость чтения у владельца.
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
