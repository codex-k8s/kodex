// Package value contains agent-manager value objects.
package value

import "github.com/google/uuid"

type LocalizedText struct {
	Locale string `json:"locale"`
	Text   string `json:"text"`
}

type ScopeRef struct {
	Type string
	Ref  string
}

type ObjectRef struct {
	ObjectURI       string
	ObjectDigest    string
	ObjectSizeBytes *int64
}

type CommandMeta struct {
	CommandID       uuid.UUID
	IdempotencyKey  string
	ExpectedVersion *int64
	ActorRef        string
}

type QueryMeta struct {
	ActorRef string
	Page     PageRequest
}

type PageRequest struct {
	PageSize  int32
	PageToken string
}

type PageResult struct {
	NextPageToken string
}
