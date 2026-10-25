package s3backup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

// OperationState exposes only closed lock state and a digest of its exact receipt, never the operation ID or object locator.
// OperationState выдаёт только закрытое состояние блокировки и дайджест точной квитанции, без идентификатора операции и адреса объекта.
type OperationState struct {
	State          string    `json:"state"`
	Operation      string    `json:"operation,omitempty"`
	SnapshotSHA256 string    `json:"snapshotSha256,omitempty"`
	ExpiresAt      time.Time `json:"expiresAt"`
}

// InspectOperation observes the current lock without acquiring, renewing, reclaiming or deleting it; expired locks remain visible.
// InspectOperation читает текущую блокировку без захвата, продления, перехвата либо удаления; просроченные блокировки остаются видимыми.
func (repository *Repository) InspectOperation(ctx context.Context, now time.Time) (OperationState, error) {
	if now.IsZero() {
		return OperationState{}, errors.New("backup operation inspection time is invalid")
	}
	receipt, err := repository.currentReceiptForKey(ctx, repository.key("locks/controller.json"))
	if errors.Is(err, ErrNotFound) {
		return OperationState{State: "ABSENT"}, nil
	}
	if err != nil {
		return OperationState{}, err
	}
	payload, err := repository.destination.readBytes(ctx, receipt, 4<<10)
	if err != nil {
		return OperationState{}, err
	}
	var document operationLockDocument
	if decodeJSON(payload, &document) != nil || !validOperationLockDocument(document) {
		return OperationState{}, errors.New("backup operation inspection document is invalid")
	}
	encoded, err := json.Marshal(receipt)
	if err != nil {
		return OperationState{}, errors.New("backup operation inspection receipt is invalid")
	}
	digest := sha256.Sum256(encoded)
	state := "ACTIVE"
	if !now.Before(document.ExpiresAt) {
		state = "EXPIRED"
	}
	return OperationState{State: state, Operation: document.Operation, SnapshotSHA256: "sha256:" + hex.EncodeToString(digest[:]), ExpiresAt: document.ExpiresAt}, nil
}
