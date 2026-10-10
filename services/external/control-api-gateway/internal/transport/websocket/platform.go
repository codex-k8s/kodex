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

const maximumPlatformFrameBytes = 65536

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

type platformSignal struct {
	Sequence   int64
	EventName  string
	Kind       string
	ProjectRef string
}

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
