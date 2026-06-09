package entity

import "time"

type AuditEvent struct {
	ID           int64
	EventType    string
	ActorUserID  string
	ActorUser    string
	ResourceType string
	ResourceName string
	Summary      string
	CreatedAt    time.Time
}
