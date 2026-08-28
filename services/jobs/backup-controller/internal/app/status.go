package app

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

type statusSnapshot struct {
	State              string    `json:"state"`
	LastAttemptAt      time.Time `json:"lastAttemptAt,omitempty"`
	LastSuccessAt      time.Time `json:"lastSuccessAt,omitempty"`
	LastVerifiedBackup string    `json:"lastVerifiedBackup,omitempty"`
}

type statusStore struct {
	mutex    sync.RWMutex
	snapshot statusSnapshot
}

func newStatusStore() *statusStore {
	return &statusStore{snapshot: statusSnapshot{State: "starting"}}
}

func (store *statusStore) set(state, backupID string, attempted, succeeded bool) {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	store.snapshot.State = state
	now := time.Now().UTC()
	if attempted {
		store.snapshot.LastAttemptAt = now
	}
	if succeeded {
		store.snapshot.LastSuccessAt = now
		store.snapshot.LastVerifiedBackup = backupID
	}
}

func (store *statusStore) initialize(backupID string, completedAt time.Time) {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	store.snapshot.State = "idle"
	store.snapshot.LastSuccessAt = completedAt
	store.snapshot.LastVerifiedBackup = backupID
}

func (store *statusStore) ServeHTTP(writer http.ResponseWriter, _ *http.Request) {
	store.mutex.RLock()
	snapshot := store.snapshot
	store.mutex.RUnlock()
	writer.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(writer).Encode(snapshot)
}
