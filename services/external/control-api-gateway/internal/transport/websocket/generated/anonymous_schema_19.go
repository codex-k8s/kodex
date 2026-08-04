package generated

import (
	"encoding/json"
)

type AnonymousSchema_19 uint

const (
	AnonymousSchema_19Active AnonymousSchema_19 = iota
	AnonymousSchema_19Paused
	AnonymousSchema_19Archived
	AnonymousSchema_19DeletionPending
	AnonymousSchema_19Deleted
	AnonymousSchema_19Queued
	AnonymousSchema_19Claimed
	AnonymousSchema_19Running
	AnonymousSchema_19WaitingOwner
	AnonymousSchema_19WaitingExternal
	AnonymousSchema_19Succeeded
	AnonymousSchema_19Failed
	AnonymousSchema_19Cancelled
	AnonymousSchema_19Expired
	AnonymousSchema_19Blocked
)

// Value returns the value of the enum.
func (op AnonymousSchema_19) Value() any {
	if op >= AnonymousSchema_19(len(AnonymousSchema_19Values)) {
		return nil
	}
	return AnonymousSchema_19Values[op]
}

var AnonymousSchema_19Values = []any{"ACTIVE", "PAUSED", "ARCHIVED", "DELETION_PENDING", "DELETED", "QUEUED", "CLAIMED", "RUNNING", "WAITING_OWNER", "WAITING_EXTERNAL", "SUCCEEDED", "FAILED", "CANCELLED", "EXPIRED", "BLOCKED"}
var ValuesToAnonymousSchema_19 = map[any]AnonymousSchema_19{
	AnonymousSchema_19Values[AnonymousSchema_19Active]:          AnonymousSchema_19Active,
	AnonymousSchema_19Values[AnonymousSchema_19Paused]:          AnonymousSchema_19Paused,
	AnonymousSchema_19Values[AnonymousSchema_19Archived]:        AnonymousSchema_19Archived,
	AnonymousSchema_19Values[AnonymousSchema_19DeletionPending]: AnonymousSchema_19DeletionPending,
	AnonymousSchema_19Values[AnonymousSchema_19Deleted]:         AnonymousSchema_19Deleted,
	AnonymousSchema_19Values[AnonymousSchema_19Queued]:          AnonymousSchema_19Queued,
	AnonymousSchema_19Values[AnonymousSchema_19Claimed]:         AnonymousSchema_19Claimed,
	AnonymousSchema_19Values[AnonymousSchema_19Running]:         AnonymousSchema_19Running,
	AnonymousSchema_19Values[AnonymousSchema_19WaitingOwner]:    AnonymousSchema_19WaitingOwner,
	AnonymousSchema_19Values[AnonymousSchema_19WaitingExternal]: AnonymousSchema_19WaitingExternal,
	AnonymousSchema_19Values[AnonymousSchema_19Succeeded]:       AnonymousSchema_19Succeeded,
	AnonymousSchema_19Values[AnonymousSchema_19Failed]:          AnonymousSchema_19Failed,
	AnonymousSchema_19Values[AnonymousSchema_19Cancelled]:       AnonymousSchema_19Cancelled,
	AnonymousSchema_19Values[AnonymousSchema_19Expired]:         AnonymousSchema_19Expired,
	AnonymousSchema_19Values[AnonymousSchema_19Blocked]:         AnonymousSchema_19Blocked,
}

func (op *AnonymousSchema_19) UnmarshalJSON(raw []byte) error {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return err
	}
	*op = ValuesToAnonymousSchema_19[v]
	return nil
}

func (op AnonymousSchema_19) MarshalJSON() ([]byte, error) {
	return json.Marshal(op.Value())
}
