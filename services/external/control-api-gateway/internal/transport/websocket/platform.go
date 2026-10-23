package websockettransport

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// maximumPlatformFrameBytes caps inbound platform wake JSON at 64 KiB before decoding; it is distinct from the outbound full-snapshot byte bound.
// maximumPlatformFrameBytes ограничивает входной JSON сигнала платформы размером 64 КиБ до разбора; это отдельный предел от полного исходящего снимка.
const maximumPlatformFrameBytes = 65536

// platformEventNames is the closed event-name to resource-kind mapping for wake decoding; event payloads cannot invent a kind or authorize access to its resources.
// platformEventNames — закрытая таблица event-name → resource-kind для разбора сигналов; payload события не может выдумать вид или разрешить доступ к ресурсам.
var platformEventNames = map[string]string{
	"PROJECT_CHANGED":                "PROJECT",
	"AGENT_CHANGED":                  "AGENT",
	"ARTIFACT_CHANGED":               "ARTIFACT",
	"INSTRUCTIONS_PUBLISHED":         "INSTRUCTIONS",
	"WORKFLOW_CHANGED":               "WORKFLOW",
	"SCHEDULE_CHANGED":               "SCHEDULE",
	"INTEGRATION_CONNECTION_CHANGED": "INTEGRATION_CONNECTION",
	"INTEGRATION_GRANT_CHANGED":      "INTEGRATION_GRANT",
	"MEMBERSHIP_CHANGED":             "MEMBERSHIP",
	"PLATFORM_MEMBERSHIP_CHANGED":    "PLATFORM_MEMBERSHIP",
	"SYSTEM_ASSISTANT_CHANGED":       "SYSTEM_ASSISTANT",
	"ROLE_IMAGE_RECIPE_CHANGED":      "ROLE_IMAGE_RECIPE",
	"ROLE_IMAGE_PROMOTION_REQUESTED": "ROLE_IMAGE_RECIPE",
	"ROLE_IMAGE_PROMOTED":            "ROLE_IMAGE_RECIPE",
	"RUNTIME_ENVIRONMENT_CHANGED":    "RUNTIME_ENVIRONMENT",
	"PROVIDER_ACCOUNT_CHANGED":       "PROVIDER_ACCOUNT",
	"RUNTIME_SECRET_CHANGED":         "RUNTIME_SECRET",
	"MANAGED_CONFIGURATION_CHANGED":  "MANAGED_CONFIGURATION",
	"RUN_CHANGED":                    "RUN",
}

// platformBusEnvelope models the existing stateless wake wire shape, including optional artifact-revision metadata. It is not a durable inbox record or a signed grant; decoding alone does not prove sender identity or resource eligibility.
// platformBusEnvelope описывает существующий формат сигнала без устойчивого эффекта, включая необязательные данные версии файла. Это не запись durable inbox и не подписанный grant; разбор не доказывает отправителя или допустимость ресурса.
type platformBusEnvelope struct {
	EventID          string    `json:"eventId"`
	EventName        string    `json:"eventName"`
	EventVersion     int64     `json:"eventVersion"`
	OccurredAt       time.Time `json:"occurredAt"`
	OrganizationRef  string    `json:"organizationRef"`
	ProjectRef       string    `json:"projectRef,omitempty"`
	AggregateRef     string    `json:"aggregateRef"`
	AggregateVersion int64     `json:"aggregateVersion"`
	Sequence         int64     `json:"sequence"`
	CorrelationRef   string    `json:"correlationRef"`
	CausationRef     string    `json:"causationRef,omitempty"`
	Data             struct {
		Kind             string `json:"kind"`
		State            string `json:"state,omitempty"`
		SafeSummary      string `json:"safeSummary"`
		ArtifactRevision *struct {
			Ref      string `json:"ref"`
			Revision int64  `json:"revision"`
			Digest   string `json:"digest"`
		} `json:"artifactRevision,omitempty"`
	} `json:"data"`
}

// platformSignal retains only sequence, known event/kind and optional project selector for a fresh owner read; it carries neither payload content nor authority.
// platformSignal сохраняет только sequence, известные event/kind и необязательный селектор проекта для свежего чтения у владельца; содержимого payload и полномочий здесь нет.
type platformSignal struct {
	Sequence   int64
	EventName  string
	Kind       string
	ProjectRef string
}

// decodePlatformSignal bounds bytes/UTF-8, rejects unknown fields and trailing JSON, then checks event-kind pairing, expected organization, positive versions/sequence, identifiers and optional revision digest shape. It is not a duplicate-key guard, signature verifier, complete metadata validator or permission check; the caller obtains the expected organization from the owner cursor.
// decodePlatformSignal ограничивает байты и UTF-8, отклоняет неизвестные поля и лишний JSON, затем проверяет пару event-kind, ожидаемую организацию, положительные версии и sequence, идентификаторы и формат необязательного дайджеста версии. Это не защита от повторных ключей, проверка подписи, полная проверка metadata или прав; ожидаемую организацию вызывающий получает из курсора владельца.
func decodePlatformSignal(payload []byte, organizationRef string) (platformSignal, bool) {
	if len(payload) == 0 || len(payload) > maximumPlatformFrameBytes || !utf8.Valid(payload) {
		return platformSignal{}, false
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var envelope platformBusEnvelope
	if decoder.Decode(&envelope) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return platformSignal{}, false
	}
	kind, known := platformEventNames[envelope.EventName]
	if !known || envelope.Data.Kind != kind || envelope.OrganizationRef != organizationRef ||
		envelope.EventVersion != 1 || envelope.AggregateVersion < 1 || envelope.Sequence < 1 ||
		envelope.OccurredAt.IsZero() || uuid.Validate(envelope.EventID) != nil ||
		!safeRef.MatchString(envelope.AggregateRef) || !safeRef.MatchString(envelope.CorrelationRef) ||
		utf8.RuneCountInString(envelope.Data.SafeSummary) > 1000 {
		return platformSignal{}, false
	}
	if envelope.ProjectRef != "" && !safeRef.MatchString(envelope.ProjectRef) {
		return platformSignal{}, false
	}
	if revision := envelope.Data.ArtifactRevision; revision != nil {
		if envelope.EventName != "ARTIFACT_CHANGED" || !safeRef.MatchString(revision.Ref) || revision.Revision < 1 ||
			len(revision.Digest) != 71 || !strings.HasPrefix(revision.Digest, "sha256:") || strings.Trim(revision.Digest[7:], "0123456789abcdef") != "" {
			return platformSignal{}, false
		}
	}
	return platformSignal{Sequence: envelope.Sequence, EventName: envelope.EventName, Kind: kind, ProjectRef: envelope.ProjectRef}, true
}
