// Package integrationfixture implements a bounded, process-local synthetic provider for disposable integration scenarios.
// Пакет integrationfixture реализует ограниченный synthetic provider с состоянием одного процесса для одноразовых интеграционных сценариев.
package integrationfixture

import (
	"crypto/sha256"
	"errors"
	"strconv"
	"sync"
)

const (
	// maximumJournals bounds retained journal states, including tombstones and fault-only diagnostics, without eviction.
	// maximumJournals ограничивает сохраняемые состояния журналов, включая удалённые записи и диагностику ошибок, без вытеснения.
	maximumJournals = 128
	// maximumEffects bounds each receipt/fault-attempt map separately rather than their combined allocation count.
	// maximumEffects ограничивает каждую карту квитанций и попыток ошибок отдельно, а не их общий размер.
	maximumEffects = 4096
)

var (
	// errIdempotencyConflict rejects an effect key reused with a different journal/action/value/version/fault digest.
	// errIdempotencyConflict отклоняет повтор ключа эффекта с другим дайджестом журнала, действия, значения, версии или ошибки.
	errIdempotencyConflict = errors.New("idempotency key conflicts with a different request")
	// errStoreCapacity rejects a mutation before growing a full journal, receipt or fault-attempt map.
	// errStoreCapacity отклоняет изменение до расширения заполненной карты журналов, квитанций или попыток ошибок.
	errStoreCapacity = errors.New("fixture store capacity is exhausted")
	// errResourceExists rejects CREATE against a journal whose synthetic resource already exists.
	// errResourceExists отклоняет CREATE журнала, искусственный ресурс которого уже существует.
	errResourceExists = errors.New("synthetic resource already exists")
	// errResourceNotFound rejects UPDATE/DELETE against an absent synthetic resource, without allocating a receipt.
	// errResourceNotFound отклоняет UPDATE и DELETE отсутствующего искусственного ресурса без создания квитанции.
	errResourceNotFound = errors.New("synthetic resource is not found")
	// errVersionConflict rejects UPDATE/DELETE whose expected sequence differs from the current journal version.
	// errVersionConflict отклоняет UPDATE и DELETE, если ожидаемая последовательность отличается от текущей версии журнала.
	errVersionConflict = errors.New("synthetic resource version conflicts")
	// errRetryableFault represents the first controlled retryable failure for one immutable fixture request.
	// errRetryableFault обозначает первый управляемый повторяемый сбой одного неизменяемого запроса оснастки.
	errRetryableFault = errors.New("synthetic retryable fault")
	// errTerminalFault represents an explicit terminal fixture fault or an unsupported direct store action.
	// errTerminalFault обозначает явную терминальную ошибку оснастки либо неподдерживаемое действие прямого вызова хранилища.
	errTerminalFault = errors.New("synthetic terminal fault")
)

// mutationAction selects one synthetic CRUD transition; HTTP validation closes the set before Store.Mutate.
// mutationAction выбирает один искусственный CRUD-переход; HTTP-проверка ограничивает множество до Store.Mutate.
type mutationAction string

const (
	// mutationCreate creates an absent resource and advances its retained sequence.
	// mutationCreate создаёт отсутствующий ресурс и продвигает его сохраняемую последовательность.
	mutationCreate mutationAction = "CREATE"
	// mutationUpdate replaces an existing value only when its expected sequence matches.
	// mutationUpdate заменяет существующее значение только при совпадении ожидаемой последовательности.
	mutationUpdate mutationAction = "UPDATE"
	// mutationDelete leaves a versioned tombstone and returns the prior value in its effect receipt.
	// mutationDelete оставляет версионированную запись удаления и возвращает предыдущее значение в квитанции эффекта.
	mutationDelete mutationAction = "DELETE"
)

// faultMode selects deterministic fault injection for this disposable store, not a provider retry policy.
// faultMode выбирает детерминированное внесение ошибок одноразового хранилища, а не политику повторов провайдера.
type faultMode string

const (
	// faultNone disables explicit fault injection while preserving ordinary version and capacity checks.
	// faultNone выключает явное внесение ошибки, сохраняя обычные проверки версии и ёмкости.
	faultNone faultMode = "NONE"
	// faultRetryableOnce records one failed attempt before allowing the exact same effect request to proceed.
	// faultRetryableOnce сохраняет одну неуспешную попытку до разрешения того же точного запроса эффекта.
	faultRetryableOnce faultMode = "RETRYABLE_ONCE"
	// faultTerminal fails every matching request while retaining its immutable digest and diagnostic count.
	// faultTerminal отклоняет каждый совпадающий запрос, сохраняя его неизменяемый дайджест и диагностический счётчик.
	faultTerminal faultMode = "TERMINAL"
)

// mutationInput carries the already validated fixture action, value, expected sequence and fault choice into the store.
// mutationInput переносит уже проверенные действие оснастки, значение, ожидаемую последовательность и выбор ошибки в хранилище.
type mutationInput struct {
	Action           mutationAction
	Value            string
	ExpectedSequence int64
	Fault            faultMode
}

// Projection is the fixture's typed current-state or historical effect readback; Count indicates zero/one active resource.
// Historical delete receipts retain the removed value, while ordinary reads of a tombstone return an empty value.
// Projection задаёт типизированный ответ оснастки для текущего состояния или исторического эффекта; Count обозначает ноль либо один активный ресурс.
// Исторические квитанции удаления сохраняют удалённое значение, а обычное чтение записи удаления возвращает пустое значение.
type Projection struct {
	Journal   string `json:"journal"`
	EffectKey string `json:"effect_key"`
	Sequence  int64  `json:"sequence"`
	Value     string `json:"value"`
	Count     int64  `json:"count"`
}

// DiagnosticProjection exposes fixture resource, replay and injected-failure counters for local scenario evidence.
// It is process-local diagnostic state, not durable platform audit or proof of a real external provider effect.
// DiagnosticProjection раскрывает состояние ресурса, повторы и внесённые ошибки оснастки для доказательств локального сценария.
// Это диагностика одного процесса, а не устойчивый аудит платформы или доказательство настоящего внешнего эффекта провайдера.
type DiagnosticProjection struct {
	Journal               string `json:"journal"`
	Exists                bool   `json:"exists"`
	Sequence              int64  `json:"sequence"`
	Count                 int64  `json:"count"`
	Value                 string `json:"value"`
	LastEffectKey         string `json:"last_effect_key"`
	ReplayCount           int64  `json:"replay_count"`
	LastReplayEffectKey   string `json:"last_replay_effect_key"`
	RetryableFailureCount int64  `json:"retryable_failure_count"`
	TerminalFailureCount  int64  `json:"terminal_failure_count"`
}

// journalState retains one resource/tombstone version and its replay/fault diagnostics under the store mutex.
// journalState хранит версию ресурса или удаления и диагностику повторов и ошибок под mutex хранилища.
type journalState struct {
	exists                bool
	sequence              int64
	value                 string
	lastEffectKey         string
	replayCount           int64
	lastReplayEffectKey   string
	retryableFailureCount int64
	terminalFailureCount  int64
}

// receipt binds an accepted immutable request digest to its exact original typed projection for later replay.
// receipt связывает принятый неизменяемый дайджест запроса с его точной исходной типизированной проекцией для последующего повтора.
type receipt struct {
	requestDigest [sha256.Size]byte
	projection    Projection
}

// faultAttempt retains the immutable request digest for a faulted effect so changed retries cannot reuse its key.
// faultAttempt сохраняет неизменяемый дайджест ошибочного эффекта, чтобы изменённый повтор не переиспользовал ключ.
type faultAttempt struct {
	requestDigest [sha256.Size]byte
}

// Store serializes bounded journal, receipt and fault maps with one mutex for the lifetime of a disposable process.
// It has no disk persistence, tenant authorization or distributed coordination; direct callers must validate their inputs.
// Store сериализует ограниченные карты журналов, квитанций и ошибок одним mutex на срок жизни одноразового процесса.
// Дисковая устойчивость, tenant-авторизация и распределённая координация отсутствуют; прямые вызывающие обязаны проверить входы.
type Store struct {
	mu            sync.Mutex
	journals      map[string]journalState
	receipts      map[string]receipt
	faultAttempts map[string]faultAttempt
}

// NewStore allocates independent empty maps without background work, external storage or preexisting journal state.
// NewStore создаёт независимые пустые карты без фоновой работы, внешнего хранилища или начальных журналов.
func NewStore() *Store {
	return &Store{
		journals:      make(map[string]journalState),
		receipts:      make(map[string]receipt),
		faultAttempts: make(map[string]faultAttempt),
	}
}

// Read returns a mutex-consistent current journal snapshot and echoes the supplied effect key without looking up a receipt.
// An absent journal yields zero state without allocating it; exact historical receipt lookup belongs to ReadEffect.
// Read возвращает согласованный под mutex текущий снимок журнала и повторяет переданный ключ эффекта без поиска квитанции.
// Отсутствующий журнал даёт нулевое состояние без создания; точная историческая квитанция читается через ReadEffect.
func (store *Store) Read(journal, effectKey string) Projection {
	store.mu.Lock()
	defer store.mu.Unlock()

	state := store.journals[journal]
	return Projection{
		Journal: journal, EffectKey: effectKey, Sequence: state.sequence,
		Value: state.value, Count: activeCount(state),
	}
}

// ReadEffect returns an exact stored receipt only when both the effect key and journal match, without incrementing replay diagnostics.
// ReadEffect возвращает точную сохранённую квитанцию только при совпадении ключа и журнала без увеличения диагностики повторов.
func (store *Store) ReadEffect(journal, effectKey string) (Projection, bool) {
	store.mu.Lock()
	defer store.mu.Unlock()
	stored, exists := store.receipts[effectKey]
	if !exists || stored.projection.Journal != journal {
		return Projection{}, false
	}
	return stored.projection, true
}

// ReadDiagnostic projects one mutex-consistent resource and replay/fault snapshot without allocating an absent journal.
// ReadDiagnostic формирует согласованный под mutex снимок ресурса и повторов и ошибок без создания отсутствующего журнала.
func (store *Store) ReadDiagnostic(journal string) DiagnosticProjection {
	store.mu.Lock()
	defer store.mu.Unlock()

	state := store.journals[journal]
	return DiagnosticProjection{
		Journal: journal, Exists: state.exists, Sequence: state.sequence,
		Count: activeCount(state), Value: state.value, LastEffectKey: state.lastEffectKey,
		ReplayCount: state.replayCount, LastReplayEffectKey: state.lastReplayEffectKey,
		RetryableFailureCount: state.retryableFailureCount,
		TerminalFailureCount:  state.terminalFailureCount,
	}
}

// Mutate serializes exact receipt replay, immutable fault-attempt binding, capacity/version checks and one CRUD transition.
// A successful mutation and receipt are recorded under the same mutex; replay returns the original projection and increments diagnostics only.
// Fault injection may update diagnostics without a resource mutation, and all state is lost on process replacement.
// Mutate сериализует точный повтор квитанции, привязку неизменяемой попытки ошибки, проверки ёмкости и версии и один CRUD-переход.
// Успешное изменение и квитанция записываются под одним mutex; повтор возвращает исходную проекцию и увеличивает лишь диагностику.
// Внесение ошибки может менять диагностику без изменения ресурса; всё состояние теряется при замене процесса.
func (store *Store) Mutate(journal, effectKey string, input mutationInput) (Projection, bool, error) {
	digest := mutationDigest(journal, input)
	store.mu.Lock()
	defer store.mu.Unlock()

	if stored, exists := store.receipts[effectKey]; exists {
		if stored.requestDigest != digest {
			return Projection{}, false, errIdempotencyConflict
		}
		state := store.journals[journal]
		state.replayCount++
		state.lastReplayEffectKey = effectKey
		store.journals[journal] = state
		return stored.projection, true, nil
	}
	state, exists := store.journals[journal]
	if !exists && len(store.journals) >= maximumJournals {
		return Projection{}, false, errStoreCapacity
	}
	attempt, attempted := store.faultAttempts[effectKey]
	if attempted && attempt.requestDigest != digest {
		return Projection{}, false, errIdempotencyConflict
	}
	if input.Fault == faultTerminal {
		if !attempted {
			if len(store.faultAttempts) >= maximumEffects {
				return Projection{}, false, errStoreCapacity
			}
			store.faultAttempts[effectKey] = faultAttempt{requestDigest: digest}
		}
		state.terminalFailureCount++
		store.journals[journal] = state
		return Projection{}, false, errTerminalFault
	}
	if input.Fault == faultRetryableOnce {
		if !attempted {
			if len(store.faultAttempts) >= maximumEffects {
				return Projection{}, false, errStoreCapacity
			}
			store.faultAttempts[effectKey] = faultAttempt{requestDigest: digest}
			state.retryableFailureCount++
			store.journals[journal] = state
			return Projection{}, false, errRetryableFault
		}
	}
	if len(store.receipts) >= maximumEffects {
		return Projection{}, false, errStoreCapacity
	}

	previousValue := state.value
	switch input.Action {
	case mutationCreate:
		if state.exists {
			return Projection{}, false, errResourceExists
		}
		state.exists = true
		state.sequence++
		state.value = input.Value
	case mutationUpdate:
		if !state.exists {
			return Projection{}, false, errResourceNotFound
		}
		if state.sequence != input.ExpectedSequence {
			return Projection{}, false, errVersionConflict
		}
		state.sequence++
		state.value = input.Value
	case mutationDelete:
		if !state.exists {
			return Projection{}, false, errResourceNotFound
		}
		if state.sequence != input.ExpectedSequence {
			return Projection{}, false, errVersionConflict
		}
		state.exists = false
		state.sequence++
		state.value = ""
	default:
		return Projection{}, false, errTerminalFault
	}
	state.lastEffectKey = effectKey
	store.journals[journal] = state
	projectionValue := state.value
	if input.Action == mutationDelete {
		projectionValue = previousValue
	}
	projection := Projection{
		Journal: journal, EffectKey: effectKey, Sequence: state.sequence,
		Value: projectionValue, Count: activeCount(state),
	}
	store.receipts[effectKey] = receipt{requestDigest: digest, projection: projection}
	return projection, false, nil
}

// activeCount maps the fixture existence flag to exactly one or zero, not the number of historical mutations.
// activeCount превращает флаг существования в ровно один либо ноль, а не в число исторических изменений.
func activeCount(state journalState) int64 {
	if state.exists {
		return 1
	}
	return 0
}

// mutationDigest hashes the ordered journal/action/value/sequence/fault tuple with NUL separators for validated fixture requests.
// It is an idempotency consistency digest, not a signature; direct callers must enforce the HTTP layer's text constraints themselves.
// mutationDigest хеширует упорядоченный кортеж журнала, действия, значения, последовательности и ошибки с NUL-разделителями для проверенных запросов оснастки.
// Это дайджест согласованности идемпотентности, а не подпись; прямые вызывающие обязаны сами обеспечить текстовые ограничения HTTP-слоя.
func mutationDigest(journal string, input mutationInput) [sha256.Size]byte {
	payload := journal + "\x00" + string(input.Action) + "\x00" + input.Value + "\x00" +
		strconv.FormatInt(input.ExpectedSequence, 10) + "\x00" + string(input.Fault)
	return sha256.Sum256([]byte(payload))
}
