// Package value содержит неизменяемые значения одного STT-запроса.
package value

import "time"

const (
	PermissionTranscribe = "stt.transcribe"
	DefaultModel         = "gpt-transcribe"
	DefaultLanguage      = "ru"
	MaximumAbsoluteBytes = 25 << 20
)

type Principal struct {
	ActorID, TenantID, ProjectID string
	RequestID                    string
	Permission                   string
	AuthorityRevision            uint64
	AuthorityDigestSHA256        string
	ExpiresAt                    time.Time
}

type Audio struct {
	Bytes     []byte
	MediaType string
	FileName  string
	Duration  time.Duration
}

type Policy struct {
	Revision                     uint64
	DigestSHA256                 string
	Model, Language              string
	MaximumAudioBytes            int
	MaximumAudioDuration         time.Duration
	ProviderTimeout              time.Duration
	ProviderAccountRef           string
	ProviderCredentialGeneration uint64
	CredentialProjectionGrant    string
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
