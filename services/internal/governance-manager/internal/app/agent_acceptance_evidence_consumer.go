package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	eventconsumer "github.com/codex-k8s/kodex/libs/go/eventconsumer"
	eventlog "github.com/codex-k8s/kodex/libs/go/eventlog"
	agentevents "github.com/codex-k8s/kodex/libs/go/platformevents/agent"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	governanceservice "github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/value"
)

const (
	agentAcceptanceEvidenceSourceService = "agent-manager"
	agentAcceptanceEvidenceConsumerActor = "agent-manager"
	agentAcceptanceEvidenceRetention     = "safe_ref"
	agentAcceptanceEvidenceKind          = "agent_acceptance"
)

type releaseAgentEvidenceRecorder interface {
	GetReleaseDecisionPackage(context.Context, governanceservice.GetReleaseDecisionPackageInput) (entity.ReleaseDecisionPackage, error)
	RecordReleaseAgentEvidence(context.Context, governanceservice.RecordReleaseAgentEvidenceInput) (entity.ReleaseDecisionPackage, error)
}

type agentAcceptanceEvidenceConsumerStart struct {
	cfg          Config
	eventLogPool *pgxpool.Pool
	recorder     releaseAgentEvidenceRecorder
	logger       *slog.Logger
	errCh        chan<- error
}

func startAgentAcceptanceEvidenceConsumer(ctx context.Context, start agentAcceptanceEvidenceConsumerStart) error {
	starter := agentAcceptanceEvidenceConsumerStarter{
		cfg:          start.cfg,
		eventLogPool: start.eventLogPool,
		recorder:     start.recorder,
	}
	return startGovernanceEventConsumer(
		ctx,
		start.cfg.AgentAcceptanceEvidenceConsumerEnabled,
		start.logger,
		start.errCh,
		"governance-manager agent acceptance evidence consumer starting",
		starter.build,
	)
}

type agentAcceptanceEvidenceConsumerStarter struct {
	cfg          Config
	eventLogPool *pgxpool.Pool
	recorder     releaseAgentEvidenceRecorder
}

func (s agentAcceptanceEvidenceConsumerStarter) build(logger *slog.Logger) (*eventconsumer.Runner, error) {
	return newAgentAcceptanceEvidenceRunner(s.cfg, s.eventLogPool, s.recorder, logger)
}

func newAgentAcceptanceEvidenceRunner(cfg Config, eventLogPool *pgxpool.Pool, recorder releaseAgentEvidenceRecorder, logger *slog.Logger) (*eventconsumer.Runner, error) {
	switch {
	case recorder == nil:
		return nil, fmt.Errorf("governance-manager agent acceptance evidence consumer requires release evidence recorder")
	case eventLogPool == nil:
		return nil, fmt.Errorf("governance-manager agent acceptance evidence consumer requires platform event-log database")
	}
	handler := agentAcceptanceEvidenceEventHandler{recorder: recorder}
	registry, err := eventconsumer.NewRegistry(
		eventconsumer.Registration{EventType: agentevents.EventAcceptanceCompleted, SchemaVersion: agentevents.SchemaVersion, Handler: handler},
		eventconsumer.Registration{EventType: agentevents.EventAcceptanceFailed, SchemaVersion: agentevents.SchemaVersion, Handler: handler},
	)
	if err != nil {
		return nil, fmt.Errorf("build agent acceptance evidence consumer registry: %w", err)
	}
	runner, err := eventconsumer.NewRunner(eventlog.NewStore(eventLogPool), registry, cfg.AgentAcceptanceEvidenceConsumerConfig(), logger, nil)
	if err != nil {
		return nil, fmt.Errorf("build agent acceptance evidence consumer runner: %w", err)
	}
	return runner, nil
}

type agentAcceptanceEvidenceEventHandler struct {
	recorder releaseAgentEvidenceRecorder
}

func (h agentAcceptanceEvidenceEventHandler) HandleEvent(ctx context.Context, event eventconsumer.Event) eventconsumer.Result {
	storedEvent := event.StoredEvent
	if strings.TrimSpace(storedEvent.SourceService) != agentAcceptanceEvidenceSourceService {
		return eventconsumer.Poison("invalid_source_service", "agent acceptance evidence event source service is not agent-manager")
	}
	if strings.TrimSpace(storedEvent.AggregateType) != agentevents.AggregateAcceptance {
		return eventconsumer.Poison("invalid_aggregate_type", "agent acceptance evidence event aggregate type is not acceptance")
	}
	var payload agentevents.Payload
	if err := json.Unmarshal(storedEvent.Payload, &payload); err != nil {
		return eventconsumer.Poison("invalid_payload", "agent acceptance evidence event payload is not valid agent json")
	}
	input, result := h.agentAcceptanceEvidenceInput(ctx, storedEvent, payload)
	if result.Status != "" {
		return result
	}
	if _, err := h.recorder.RecordReleaseAgentEvidence(ctx, input); err != nil {
		return agentAcceptanceEvidenceConsumerError(err)
	}
	return eventconsumer.Ack()
}

func (h agentAcceptanceEvidenceEventHandler) agentAcceptanceEvidenceInput(ctx context.Context, storedEvent eventlog.StoredEvent, payload agentevents.Payload) (governanceservice.RecordReleaseAgentEvidenceInput, eventconsumer.Result) {
	packageRef := strings.TrimSpace(payload.GovernanceReleaseDecisionPackageRef)
	if packageRef == "" {
		return governanceservice.RecordReleaseAgentEvidenceInput{}, eventconsumer.Ack()
	}
	packageID, err := parseReleaseDecisionPackageID(packageRef)
	if err != nil {
		return governanceservice.RecordReleaseAgentEvidenceInput{}, eventconsumer.Poison("invalid_release_package_ref", "agent acceptance event does not reference a governance release package")
	}
	if strings.TrimSpace(payload.AcceptanceResultID) == "" || strings.TrimSpace(payload.SessionID) == "" {
		return governanceservice.RecordReleaseAgentEvidenceInput{}, eventconsumer.Poison("missing_agent_acceptance_refs", "agent acceptance event misses acceptance or session ref")
	}
	status, statusResult := agentAcceptanceEvidenceStatus(storedEvent.EventType, payload.Status)
	if statusResult.Status != "" {
		return governanceservice.RecordReleaseAgentEvidenceInput{}, statusResult
	}
	queryMeta := governanceservice.QueryMeta{
		Actor:     value.Actor{Type: "service", ID: agentAcceptanceEvidenceConsumerActor},
		RequestID: agentAcceptanceEvidenceRequestID(storedEvent),
	}
	pkg, loadErr := h.recorder.GetReleaseDecisionPackage(ctx, governanceservice.GetReleaseDecisionPackageInput{
		ReleaseDecisionPackageID: packageID,
		Meta:                     queryMeta,
	})
	if loadErr != nil {
		return governanceservice.RecordReleaseAgentEvidenceInput{}, agentAcceptanceEvidenceConsumerError(loadErr)
	}
	expectedVersion := pkg.Version
	return governanceservice.RecordReleaseAgentEvidenceInput{
		ReleaseDecisionPackageID: packageID,
		AgentContext:             agentAcceptanceContextPayload(payload),
		EvidenceRefs:             []value.EvidenceRef{agentAcceptanceEvidenceRef(storedEvent, payload, status)},
		IntegrationRefs:          agentAcceptanceIntegrationRefs(storedEvent, payload, status),
		Meta: governanceservice.CommandMeta{
			IdempotencyKey:  agentAcceptanceEvidenceIdempotencyKey(storedEvent, payload, packageID, status),
			ExpectedVersion: &expectedVersion,
			Actor:           value.Actor{Type: "service", ID: agentAcceptanceEvidenceConsumerActor},
			RequestID:       agentAcceptanceEvidenceRequestID(storedEvent),
		},
	}, eventconsumer.Result{}
}

func agentAcceptanceEvidenceStatus(eventType string, status string) (string, eventconsumer.Result) {
	normalized := strings.TrimSpace(status)
	switch eventType {
	case agentevents.EventAcceptanceCompleted:
		if normalized == "passed" || normalized == "skipped" {
			return normalized, eventconsumer.Result{}
		}
	case agentevents.EventAcceptanceFailed:
		if normalized == "failed" {
			return normalized, eventconsumer.Result{}
		}
	}
	return "", eventconsumer.Poison("invalid_acceptance_status", "agent acceptance event status does not match its event type")
}

func agentAcceptanceContextPayload(payload agentevents.Payload) []byte {
	item := agentAcceptanceContextRef{
		SessionRef:    agentAcceptanceRef("session", payload.SessionID),
		RunRef:        agentAcceptanceRef("run", payload.RunID),
		StageRef:      agentAcceptanceRef("stage", payload.StageID),
		AcceptanceRef: agentAcceptanceRef("acceptance", payload.AcceptanceResultID),
	}
	body, _ := json.Marshal(item)
	return body
}

type agentAcceptanceContextRef struct {
	SessionRef    string `json:"sessionRef,omitempty"`
	RunRef        string `json:"runRef,omitempty"`
	StageRef      string `json:"stageRef,omitempty"`
	AcceptanceRef string `json:"acceptanceRef,omitempty"`
}

func agentAcceptanceEvidenceRef(storedEvent eventlog.StoredEvent, payload agentevents.Payload, status string) value.EvidenceRef {
	return value.EvidenceRef{
		Kind:           agentAcceptanceEvidenceKind,
		Ref:            agentAcceptanceRef("acceptance", payload.AcceptanceResultID),
		Summary:        agentAcceptanceEvidenceSummary(status, payload.ReasonCode),
		Digest:         agentAcceptanceEvidenceDigest(storedEvent, payload, status),
		RetentionClass: agentAcceptanceEvidenceRetention,
	}
}

func agentAcceptanceIntegrationRefs(storedEvent eventlog.StoredEvent, payload agentevents.Payload, status string) []value.ReleaseIntegrationRef {
	observedAt := storedEvent.OccurredAt.UTC().Format("2006-01-02T15:04:05Z")
	version := ""
	if payload.Version > 0 {
		version = strconv.FormatInt(payload.Version, 10)
	}
	result := []value.ReleaseIntegrationRef{
		{
			Domain:     "agent",
			Kind:       "acceptance",
			Ref:        agentAcceptanceRef("acceptance", payload.AcceptanceResultID),
			Status:     status,
			Summary:    agentAcceptanceEvidenceSummary(status, payload.ReasonCode),
			Digest:     agentAcceptanceEvidenceDigest(storedEvent, payload, status),
			ObservedAt: observedAt,
			Version:    version,
			ErrorCode:  strings.TrimSpace(payload.ReasonCode),
		},
	}
	for _, ref := range []struct {
		kind  string
		value string
	}{
		{kind: "session", value: payload.SessionID},
		{kind: "run", value: payload.RunID},
		{kind: "stage", value: payload.StageID},
	} {
		if normalized := agentAcceptanceRef(ref.kind, ref.value); normalized != "" {
			result = append(result, value.ReleaseIntegrationRef{Domain: "agent", Kind: ref.kind, Ref: normalized, ObservedAt: observedAt})
		}
	}
	if runtimeJobRef := strings.TrimSpace(payload.RuntimeJobRef); runtimeJobRef != "" {
		result = append(result, value.ReleaseIntegrationRef{Domain: "runtime", Kind: "job", Ref: runtimeJobRef, ObservedAt: observedAt})
	}
	return result
}

func agentAcceptanceRef(kind string, id string) string {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return ""
	}
	return "agent:" + kind + "/" + trimmed
}

func agentAcceptanceEvidenceSummary(status string, reasonCode string) string {
	if reason := strings.TrimSpace(reasonCode); reason != "" {
		return "agent acceptance " + strings.TrimSpace(status) + ": " + reason
	}
	return "agent acceptance " + strings.TrimSpace(status)
}

func agentAcceptanceEvidenceDigest(storedEvent eventlog.StoredEvent, payload agentevents.Payload, status string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		strings.TrimSpace(payload.AcceptanceResultID),
		strings.TrimSpace(status),
		strings.TrimSpace(payload.ReasonCode),
		strconv.FormatInt(payload.Version, 10),
		storedEvent.OccurredAt.UTC().Format("2006-01-02T15:04:05Z"),
	}, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func agentAcceptanceEvidenceIdempotencyKey(storedEvent eventlog.StoredEvent, payload agentevents.Payload, packageID uuid.UUID, status string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		storedEvent.ID.String(),
		packageID.String(),
		strings.TrimSpace(payload.AcceptanceResultID),
		strings.TrimSpace(status),
	}, "\x00")))
	return "agent_acceptance_evidence:" + hex.EncodeToString(sum[:])
}

func agentAcceptanceEvidenceRequestID(storedEvent eventlog.StoredEvent) string {
	if storedEvent.ID == uuid.Nil {
		return ""
	}
	return "agent_event:" + storedEvent.ID.String()
}

var agentAcceptanceEvidenceDomainErrorResults = governanceConsumerDomainErrors(
	eventConsumerPoison{code: "invalid_agent_acceptance_evidence", summary: "agent acceptance evidence metadata is invalid"},
	eventConsumerPoison{code: "conflicting_agent_acceptance_evidence", summary: "agent acceptance evidence conflicts with stored governance release package"},
	eventConsumerPoison{code: "forbidden_agent_acceptance_evidence", summary: "agent acceptance evidence actor is not authorized"},
	eventConsumerPoison{code: "unknown_agent_acceptance_evidence_ref", summary: "agent acceptance evidence references unknown governance state"},
	eventConsumerPoison{code: "stale_agent_acceptance_evidence", summary: "agent acceptance evidence cannot update current release package state"},
)

func agentAcceptanceEvidenceConsumerError(err error) eventconsumer.Result {
	return governanceConsumerError(err, agentAcceptanceEvidenceDomainErrorResults)
}
