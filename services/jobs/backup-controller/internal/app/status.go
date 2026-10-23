package app

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// statusSnapshot is the JSON projection of the current cycle and last verified backup.
// RU: statusSnapshot представляет JSON-проекцию текущего цикла и последней проверенной копии.
type statusSnapshot struct {
	State              string    `json:"state"`
	LastAttemptAt      time.Time `json:"lastAttemptAt,omitempty"`
	LastSuccessAt      time.Time `json:"lastSuccessAt,omitempty"`
	LastVerifiedBackup string    `json:"lastVerifiedBackup,omitempty"`
}

// statusStore protects the technical status snapshot across worker and HTTP goroutines.
// RU: statusStore защищает техническую проекцию состояния между worker и HTTP goroutines.
type statusStore struct {
	mutex    sync.RWMutex
	snapshot statusSnapshot
}

// newStatusStore starts with an explicit not-yet-ready state.
// RU: newStatusStore начинает с явного состояния до достижения готовности.
func newStatusStore() *statusStore {
	return &statusStore{snapshot: statusSnapshot{State: "starting"}}
}

// set atomically records a cycle transition and timestamps only attempted or successful backups.
// RU: set атомарно фиксирует переход цикла и обновляет время только попытки либо успешной копии.
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

// initialize seeds status from authoritative startup readback before HTTP serving begins.
// RU: initialize задаёт состояние из авторитетного стартового readback до запуска HTTP-обслуживания.
func (store *statusStore) initialize(backupID string, completedAt time.Time) {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	store.snapshot.State = "idle"
	store.snapshot.LastSuccessAt = completedAt
	store.snapshot.LastVerifiedBackup = backupID
}

// ServeHTTP returns a synchronized JSON copy of technical status without exposing secrets.
// RU: ServeHTTP возвращает синхронизированную JSON-копию технического состояния без секретов.
func (store *statusStore) ServeHTTP(writer http.ResponseWriter, _ *http.Request) {
	store.mutex.RLock()
	snapshot := store.snapshot
	store.mutex.RUnlock()
	writer.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(writer).Encode(snapshot)
}
