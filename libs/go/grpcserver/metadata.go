package grpcserver

import "strings"

// ActorProto is the common shape of generated actor messages across service APIs.
type ActorProto interface {
	GetType() string
	GetId() string
}

// RequestContextProto is the common shape of generated request context messages.
type RequestContextProto interface {
	GetSource() string
	GetTraceId() string
	GetSessionId() string
	GetClientIpHash() string
}

// ActorParts normalizes actor fields before service-specific casting.
func ActorParts[Actor ActorProto](actor Actor) (actorType string, actorID string) {
	return strings.TrimSpace(actor.GetType()), strings.TrimSpace(actor.GetId())
}

// RequestContextParts normalizes request context fields before service-specific casting.
func RequestContextParts[RequestContext RequestContextProto](context RequestContext) (source string, traceID string, sessionID string, clientIPHash string) {
	return strings.TrimSpace(context.GetSource()),
		strings.TrimSpace(context.GetTraceId()),
		strings.TrimSpace(context.GetSessionId()),
		strings.TrimSpace(context.GetClientIpHash())
}
