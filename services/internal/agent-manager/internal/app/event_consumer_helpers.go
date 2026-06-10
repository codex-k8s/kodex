package app

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"

	eventconsumer "github.com/codex-k8s/kodex/libs/go/eventconsumer"
	eventlog "github.com/codex-k8s/kodex/libs/go/eventlog"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/errs"
)

func handleTypedEventCommand[Payload any, Input any](
	ctx context.Context,
	event eventconsumer.Event,
	decode typedEventDecodeConfig,
	relevant func(Payload) bool,
	input func(context.Context, Payload) (Input, eventconsumer.Result),
	record func(context.Context, Input) error,
	consumeError func(error) eventconsumer.Result,
) eventconsumer.Result {
	payload, decodeResult := decodeTypedEventPayload[Payload](event, decode.sourceService, decode.invalidSource, decode.aggregateType, decode.invalidAggregate, decode.invalidPayload)
	if decodeResult.Status != "" {
		return decodeResult
	}
	if !relevant(payload) {
		return eventconsumer.Ack()
	}
	commandInput, inputResult := input(ctx, payload)
	if inputResult.Status != "" {
		return inputResult
	}
	if err := record(ctx, commandInput); err != nil {
		return consumeError(err)
	}
	return eventconsumer.Ack()
}

type typedEventDecodeConfig struct {
	sourceService    string
	invalidSource    eventconsumer.Result
	aggregateType    string
	invalidAggregate eventconsumer.Result
	invalidPayload   eventconsumer.Result
}

func commonEventConsumerDomainError(err error, invalid eventconsumer.Result, conflict eventconsumer.Result, notFound eventconsumer.Result, precondition eventconsumer.Result) eventconsumer.Result {
	switch {
	case errors.Is(err, errs.ErrInvalidArgument):
		return invalid
	case errors.Is(err, errs.ErrConflict):
		return conflict
	case errors.Is(err, errs.ErrNotFound):
		return notFound
	case errors.Is(err, errs.ErrPreconditionFailed):
		return precondition
	default:
		return eventconsumer.Retry(err)
	}
}

func invalidConflictEventConsumerDomainError(err error, invalid eventconsumer.Result, conflict eventconsumer.Result) eventconsumer.Result {
	switch {
	case errors.Is(err, errs.ErrInvalidArgument):
		return invalid
	case errors.Is(err, errs.ErrConflict):
		return conflict
	default:
		return eventconsumer.Retry(err)
	}
}

func newEventConsumerRunner(eventLogPool *pgxpool.Pool, registration eventconsumer.Registration, cfg eventconsumer.Config, logger *slog.Logger) (*eventconsumer.Runner, error) {
	registry, err := eventconsumer.NewRegistry(registration)
	if err != nil {
		return nil, err
	}
	return eventconsumer.NewRunner(eventlog.NewStore(eventLogPool), registry, cfg, logger, nil)
}

func decodeTypedEventPayload[Payload any](
	event eventconsumer.Event,
	expectedSourceService string,
	invalidSource eventconsumer.Result,
	expectedAggregateType string,
	invalidAggregate eventconsumer.Result,
	invalidPayload eventconsumer.Result,
) (Payload, eventconsumer.Result) {
	storedEvent := event.StoredEvent
	var payload Payload
	if strings.TrimSpace(storedEvent.SourceService) != expectedSourceService {
		return payload, invalidSource
	}
	if strings.TrimSpace(storedEvent.AggregateType) != expectedAggregateType {
		return payload, invalidAggregate
	}
	if err := json.Unmarshal(storedEvent.Payload, &payload); err != nil {
		return payload, invalidPayload
	}
	return payload, eventconsumer.Result{}
}
