// Package integrationfixture реализует одноразовый synthetic provider для local E2E.
package integrationfixture

import (
	"crypto/sha256"
	"errors"
	"strconv"
	"sync"
)

const (
	maximumJournals = 128
	maximumEffects  = 4096
)

var (
	errIdempotencyConflict = errors.New("idempotency key conflicts with a different request")
	errStoreCapacity       = errors.New("fixture store capacity is exhausted")
	errResourceExists      = errors.New("synthetic resource already exists")
	errResourceNotFound    = errors.New("synthetic resource is not found")
	errVersionConflict     = errors.New("synthetic resource version conflicts")
	errRetryableFault      = errors.New("synthetic retryable fault")
	errTerminalFault       = errors.New("synthetic terminal fault")
)

type mutationAction string

const (
	mutationCreate mutationAction = "CREATE"
	mutationUpdate mutationAction = "UPDATE"
	mutationDelete mutationAction = "DELETE"
)

type faultMode string

const (
	faultNone          faultMode = "NONE"
	faultRetryableOnce faultMode = "RETRYABLE_ONCE"
	faultTerminal      faultMode = "TERMINAL"
)

type mutationInput struct {
	Action           mutationAction
	Value            string
	ExpectedSequence int64
	Fault            faultMode
}

// Projection является строгим provider readback журнала или одного effect.
type Projection struct {
	Journal   string `json:"journal"`
	EffectKey string `json:"effect_key"`
	Sequence  int64  `json:"sequence"`
	Value     string `json:"value"`
	Count     int64  `json:"count"`
}

// DiagnosticProjection доступен только через local-only fixture и позволяет
// E2E доказать состояние ресурса, exact replay и fault classification.
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

type receipt struct {
	requestDigest [sha256.Size]byte
	projection    Projection
}

type faultAttempt struct {
	requestDigest [sha256.Size]byte
}

// Store хранит ограниченное состояние одного disposable процесса.
type Store struct {
	mu            sync.Mutex
	journals      map[string]journalState
	receipts      map[string]receipt
	faultAttempts map[string]faultAttempt
}

func NewStore() *Store {
	return &Store{
		journals:      make(map[string]journalState),
		receipts:      make(map[string]receipt),
		faultAttempts: make(map[string]faultAttempt),
	}
}

func (store *Store) Read(journal, effectKey string) Projection {
	store.mu.Lock()
	defer store.mu.Unlock()

	state := store.journals[journal]
	return Projection{
		Journal: journal, EffectKey: effectKey, Sequence: state.sequence,
		Value: state.value, Count: activeCount(state),
	}
}

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

func activeCount(state journalState) int64 {
	if state.exists {
		return 1
	}
	return 0
}

func mutationDigest(journal string, input mutationInput) [sha256.Size]byte {
	payload := journal + "\x00" + string(input.Action) + "\x00" + input.Value + "\x00" +
		strconv.FormatInt(input.ExpectedSequence, 10) + "\x00" + string(input.Fault)
	return sha256.Sum256([]byte(payload))
}
