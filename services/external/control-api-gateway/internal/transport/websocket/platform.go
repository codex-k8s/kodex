package websockettransport

import (
	"bytes"
	"encoding/json"
	"io"
	"time"

	"github.com/google/uuid"
)

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
		Kind        string `json:"kind"`
		State       string `json:"state,omitempty"`
		SafeSummary string `json:"safeSummary"`
	} `json:"data"`
}

type platformSignal struct {
	Sequence  int64
	EventName string
	Kind      string
}

func decodePlatformSignal(payload []byte, organizationRef string) (platformSignal, bool) {
	if len(payload) == 0 || len(payload) > maximumFrameBytes {
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
		len(envelope.Data.SafeSummary) > 2000 {
		return platformSignal{}, false
	}
	if envelope.ProjectRef != "" && !safeRef.MatchString(envelope.ProjectRef) {
		return platformSignal{}, false
	}
	return platformSignal{Sequence: envelope.Sequence, EventName: envelope.EventName, Kind: kind}, true
}
