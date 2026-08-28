// Package integrationfixture реализует одноразовый synthetic provider для local E2E.
package integrationfixture

import (
	"crypto/sha256"
	"errors"
	"sync"
)

const (
	maximumJournals = 128
	maximumEffects  = 4096
)

var (
	errIdempotencyConflict = errors.New("idempotency key conflicts with a different request")
	errStoreCapacity       = errors.New("fixture store capacity is exhausted")
)

// Projection является строгим provider readback журнала или одного effect.
type Projection struct {
	Journal   string `json:"journal"`
	EffectKey string `json:"effect_key"`
	Sequence  int64  `json:"sequence"`
	Value     string `json:"value"`
	Count     int64  `json:"count"`
}

// DiagnosticProjection доступен только через local-only fixture и позволяет
// E2E доказать provider effect и exact replay без записи в provider напрямую.
type DiagnosticProjection struct {
	Journal             string `json:"journal"`
	Count               int64  `json:"count"`
	Value               string `json:"value"`
	LastEffectKey       string `json:"last_effect_key"`
	ReplayCount         int64  `json:"replay_count"`
	LastReplayEffectKey string `json:"last_replay_effect_key"`
}

type journalState struct {
	sequence            int64
	value               string
	lastEffectKey       string
	replayCount         int64
	lastReplayEffectKey string
}

type receipt struct {
	requestDigest [sha256.Size]byte
	projection    Projection
}

// Store хранит ограниченное состояние одного disposable процесса.
type Store struct {
	mu       sync.Mutex
	journals map[string]journalState
	receipts map[string]receipt
}

func NewStore() *Store {
	return &Store{
		journals: make(map[string]journalState),
		receipts: make(map[string]receipt),
	}
}

func (store *Store) Read(journal, effectKey string) Projection {
	store.mu.Lock()
	defer store.mu.Unlock()

	state := store.journals[journal]
	return Projection{
		Journal: journal, EffectKey: effectKey, Sequence: state.sequence,
		Value: state.value, Count: state.sequence,
	}
}

func (store *Store) ReadDiagnostic(journal string) DiagnosticProjection {
	store.mu.Lock()
	defer store.mu.Unlock()

	state := store.journals[journal]
	return DiagnosticProjection{
		Journal: journal, Count: state.sequence, Value: state.value,
		LastEffectKey: state.lastEffectKey, ReplayCount: state.replayCount,
		LastReplayEffectKey: state.lastReplayEffectKey,
	}
}

func (store *Store) Append(journal, effectKey, value string) (Projection, bool, error) {
	digest := requestDigest(journal, value)
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
	if len(store.receipts) >= maximumEffects {
		return Projection{}, false, errStoreCapacity
	}
	state, exists := store.journals[journal]
	if !exists && len(store.journals) >= maximumJournals {
		return Projection{}, false, errStoreCapacity
	}
	state.sequence++
	state.value = value
	state.lastEffectKey = effectKey
	store.journals[journal] = state
	projection := Projection{
		Journal: journal, EffectKey: effectKey, Sequence: state.sequence,
		Value: value, Count: state.sequence,
	}
	store.receipts[effectKey] = receipt{requestDigest: digest, projection: projection}
	return projection, false, nil
}

func requestDigest(journal, value string) [sha256.Size]byte {
	return sha256.Sum256([]byte(journal + "\x00" + value))
}
