package errs

import "errors"

var (
	Invalid     = errors.New("INVALID")
	Denied      = errors.New("DENIED")
	Gate        = errors.New("GATE_REQUIRED")
	Conflict    = errors.New("CONFLICT")
	NotFound    = errors.New("NOT_FOUND")
	Unsupported = errors.New("UNSUPPORTED")
	Unavailable = errors.New("UNAVAILABLE")
)
