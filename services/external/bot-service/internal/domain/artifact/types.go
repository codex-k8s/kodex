package artifact

import (
	"context"
	"errors"
	"io"
	"time"
)

const (
	DirectionInbound  Direction = "inbound"
	DirectionOutbound Direction = "outbound"

	StateUploading   VersionState = "uploading"
	StateScanning    VersionState = "scanning"
	StateAvailable   VersionState = "available"
	StateQuarantined VersionState = "quarantined"
	StateFailed      VersionState = "failed"

	DeliveryPending     DeliveryState = "pending"
	DeliveryDelivered   DeliveryState = "delivered"
	DeliveryFailed      DeliveryState = "failed"
	DeliveryQuarantined DeliveryState = "quarantined"

	ManifestSchemaVersion = "mattercodex.artifact-manifest/v1"
)

var (
	ErrNotFound          = errors.New("artifact not found")
	ErrConflict          = errors.New("artifact conflict")
	ErrLimitExceeded     = errors.New("artifact limit exceeded")
	ErrMediaTypeDenied   = errors.New("artifact media type denied")
	ErrScopeDenied       = errors.New("artifact scope denied")
	ErrQuarantined       = errors.New("artifact quarantined")
	ErrDeliveryAmbiguous = errors.New("artifact delivery outcome is ambiguous")
)

type Direction string

type VersionState string

type DeliveryState string

type Scope struct {
	ProjectID            int64
	ChatID               int64
	SessionID            int64
	RoleID               int64
	TurnID               string
	SessionKey           string
	MattermostChannelID  string
	MattermostRootPostID string
}

type SourceFile struct {
	FileID            string
	PostID            string
	ChannelID         string
	CreatorID         string
	OriginalName      string
	DeclaredMediaType string
	DeclaredSize      int64
}

type Version struct {
	ArtifactID        string
	VersionID         string
	Scope             Scope
	Direction         Direction
	State             VersionState
	ErrorCode         string
	StorageKey        string
	OriginalName      string
	SafeName          string
	MediaType         string
	DeclaredMediaType string
	Size              int64
	SHA256            string
	SourcePostID      string
	SourceFileID      string
	Ordinal           int
	RetentionUntil    time.Time
	CreatedAt         time.Time
}

type Delivery struct {
	DeliveryID        string
	ArtifactVersion   Version
	Scope             Scope
	IdempotencyKey    string
	BotTokenSecretRef string
	State             DeliveryState
	MattermostFileID  string
	MattermostPostID  string
	ErrorCode         string
	Attempts          int
}

type Manifest struct {
	SchemaVersion string          `json:"schema_version"`
	TurnID        string          `json:"turn_id"`
	Files         []ManifestEntry `json:"files"`
}

type ManifestEntry struct {
	OriginalName      string         `json:"original_name"`
	LocalPath         string         `json:"local_path"`
	MediaType         string         `json:"media_type"`
	Size              int64          `json:"size"`
	SHA256            string         `json:"sha256"`
	ArtifactVersionID string         `json:"artifact_version_id"`
	Source            ManifestSource `json:"source"`
}

type ManifestSource struct {
	Kind   string `json:"kind"`
	PostID string `json:"post_id"`
	FileID string `json:"file_id"`
}

type PublishResult struct {
	ArtifactVersionID string        `json:"artifact_version_id"`
	DeliveryID        string        `json:"delivery_id"`
	State             DeliveryState `json:"state"`
	MattermostPostID  string        `json:"mattermost_post_id,omitempty"`
	Quarantined       bool          `json:"quarantined"`
}

type IncomingSource interface {
	Metadata(ctx context.Context, fileID string) (SourceFile, error)
	Open(ctx context.Context, fileID string) (io.ReadCloser, error)
}

type ObjectStore interface {
	PutImmutable(ctx context.Context, key string, mediaType string, size int64, sha256 string, body io.Reader) error
	Open(ctx context.Context, key string) (io.ReadCloser, error)
}

type DeliveryRequest struct {
	DeliveryID        string
	VersionID         string
	ChannelID         string
	RootPostID        string
	BotTokenSecretRef string
	FileName          string
	MediaType         string
	Size              int64
	SHA256            string
}

type DeliveryReceipt struct {
	MattermostFileID string
	MattermostPostID string
}

type MattermostDelivery interface {
	Upload(ctx context.Context, request DeliveryRequest, body io.Reader) (string, error)
	Publish(ctx context.Context, request DeliveryRequest, fileID string) (DeliveryReceipt, error)
}

type CreateVersionInput struct {
	Version           Version
	IdempotencyKey    string
	DeliveryID        string
	DeliveryState     DeliveryState
	BotTokenSecretRef string
}

type Repository interface {
	FindInbound(ctx context.Context, scope Scope, postID string, fileID string) (Version, error)
	CreateInbound(ctx context.Context, input CreateVersionInput) error
	BindInbound(ctx context.Context, versionID string, scope Scope, postID string, fileID string, ordinal int) error
	ListTurn(ctx context.Context, scope Scope) ([]Version, error)
	GetAvailable(ctx context.Context, scope Scope, versionID string) (Version, error)
	SetVersionState(ctx context.Context, versionID string, from VersionState, to VersionState, errorCode string) error
	FindDelivery(ctx context.Context, scope Scope, idempotencyKey string) (Delivery, error)
	CreateOutbound(ctx context.Context, input CreateVersionInput) error
	SetDeliveryResult(ctx context.Context, deliveryID string, state DeliveryState, fileID string, postID string, errorCode string) error
}
