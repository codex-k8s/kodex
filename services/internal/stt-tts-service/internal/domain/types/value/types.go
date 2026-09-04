// Package value содержит неизменяемые значения одного STT-запроса.
package value

import (
	"io"
	"time"
)

const (
	PermissionTranscribe           = "stt.transcribe"
	DefaultModel                   = "gpt-transcribe"
	DefaultLanguage                = "ru"
	MaximumAbsoluteBytes     int64 = 25 << 20
	MaximumConcurrentStreams       = 2
	MaximumInflightBytes     int64 = MaximumAbsoluteBytes * MaximumConcurrentStreams
	MaximumChunkBytes              = 64 << 10
)

type AuthorityProvenance struct {
	Source       int32
	Reference    string
	Revision     uint64
	DigestSHA256 string
}

type Principal struct {
	ActorID, TenantID, ProjectID string
	Actor, Tenant, Project       AuthorityProvenance
	RequestID                    string
	Permission                   string
	AuthorityRevision            uint64
	AuthorityDigestSHA256        string
	ExpiresAt                    time.Time
}

type Audio struct {
	Reader    io.ReadSeeker
	SizeBytes int64
	MediaType string
	FileName  string
	Duration  time.Duration
}

type Policy struct {
	Revision                     uint64
	DigestSHA256                 string
	Model, Language              string
	MaximumAudioBytes            int64
	MaximumAudioDuration         time.Duration
	ProviderTimeout              time.Duration
	ProviderAccountRef           string
	ProviderCredentialGeneration uint64
	ExpiresAt                    time.Time
}

type Credential struct {
	APIKey                       []byte
	ProviderAccountRef           string
	ProviderCredentialGeneration uint64
	ConfigDigestSHA256           string
	ExpiresAt                    time.Time
}

type ProviderRequest struct {
	Audio    Audio
	Model    string
	Language string
	APIKey   []byte
}

type TranscriptionReceipt struct {
	RequestID, CorrelationID            string
	ActorID, TenantID, ProjectID        string
	AuthoritySourceRevision             uint64
	AuthoritySourceDigestSHA256         string
	ConfigRevision                      uint64
	ConfigDigestSHA256, Model, Language string
	ProviderAccountRef                  string
	ProviderCredentialGeneration        uint64
	CompletedStage                      Stage
}

type TranscriptionResult struct {
	Text    string
	Receipt TranscriptionReceipt
}

type Stage string

const (
	StageAuthority  Stage = "authority"
	StagePolicy     Stage = "policy"
	StageAudio      Stage = "audio"
	StageCredential Stage = "credential"
	StageEgress     Stage = "egress"
	StageProvider   Stage = "provider"
	StageSuccess    Stage = "success"
	StageUnknown    Stage = "unknown"
)

type ErrorClass string

const (
	ErrorNone        ErrorClass = "none"
	ErrorDenied      ErrorClass = "denied"
	ErrorInvalid     ErrorClass = "invalid"
	ErrorUnavailable ErrorClass = "unavailable"
	ErrorTimeout     ErrorClass = "timeout"
	ErrorRejected    ErrorClass = "rejected"
	ErrorUnknown     ErrorClass = "unknown"
)
