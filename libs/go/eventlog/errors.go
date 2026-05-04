package eventlog

import "errors"

var (
	// ErrInvalidEvent means the producer supplied an event that cannot be safely published.
	ErrInvalidEvent = errors.New("invalid event")
	// ErrInvalidClaim means the consumer lease request is malformed.
	ErrInvalidClaim = errors.New("invalid event log claim")
	// ErrCheckpointNotOwned means a consumer tried to advance or release a lease it no longer owns.
	ErrCheckpointNotOwned = errors.New("event log checkpoint is not owned by lease owner")
)
