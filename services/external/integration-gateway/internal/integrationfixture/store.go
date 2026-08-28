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

type journalState struct {
	sequence int64
	value    string
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

func (store *Store) Append(journal, effectKey, value string) (Projection, bool, error) {
	digest := requestDigest(journal, value)
	store.mu.Lock()
	defer store.mu.Unlock()

	if stored, exists := store.receipts[effectKey]; exists {
		if stored.requestDigest != digest {
			return Projection{}, false, errIdempotencyConflict
		}
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
