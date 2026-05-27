package eventconsumer

import (
	"fmt"
	"strings"

	eventlog "github.com/codex-k8s/kodex/libs/go/eventlog"
)

// Registration binds one event type/schema version to a handler.
type Registration struct {
	EventType     string
	SchemaVersion int
	Handler       Handler
}

// Registry contains typed event handlers indexed by event type and schema version.
type Registry struct {
	handlers map[eventKey]Handler
	types    map[string]struct{}
}

type eventKey struct {
	eventType     string
	schemaVersion int
}

// NewRegistry creates an immutable handler registry.
func NewRegistry(registrations ...Registration) (Registry, error) {
	registry := Registry{
		handlers: make(map[eventKey]Handler, len(registrations)),
		types:    make(map[string]struct{}, len(registrations)),
	}
	for _, registration := range registrations {
		eventType := strings.TrimSpace(registration.EventType)
		if eventType == "" || registration.SchemaVersion < 1 || registration.Handler == nil {
			return Registry{}, fmt.Errorf("%w: handler registration is invalid", ErrInvalidConfig)
		}
		key := eventKey{eventType: eventType, schemaVersion: registration.SchemaVersion}
		if _, ok := registry.handlers[key]; ok {
			return Registry{}, fmt.Errorf("%w: %s/%d", ErrDuplicateHandler, eventType, registration.SchemaVersion)
		}
		registry.handlers[key] = registration.Handler
		registry.types[eventType] = struct{}{}
	}
	return registry, nil
}

func (r Registry) lookup(event eventlog.StoredEvent) (Handler, lookupStatus) {
	eventType := strings.TrimSpace(event.EventType)
	handler, ok := r.handlers[eventKey{eventType: eventType, schemaVersion: event.SchemaVersion}]
	if ok {
		return handler, lookupHandled
	}
	if _, known := r.types[eventType]; known {
		return nil, lookupUnsupportedVersion
	}
	return nil, lookupUnknownType
}

type lookupStatus int

const (
	lookupHandled lookupStatus = iota
	lookupUnknownType
	lookupUnsupportedVersion
)
