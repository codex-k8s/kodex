package eventing

import (
	"bytes"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

// Envelope carries the transport metadata and raw JSON object of one domain
// event; producers retain the same identity and sequence across retries.
// RU: Envelope переносит транспортные метаданные и исходный JSON-объект одного
// доменного события; при повторе отправитель сохраняет идентичность и порядок.
type Envelope struct {
	EventID          string          `json:"eventId"`
	EventName        string          `json:"eventName"`
	EventVersion     uint32          `json:"eventVersion"`
	SchemaVersion    uint32          `json:"schemaVersion"`
	OccurredAt       time.Time       `json:"occurredAt"`
	AggregateType    string          `json:"aggregateType"`
	AggregateID      string          `json:"aggregateId"`
	AggregateVersion uint64          `json:"aggregateVersion"`
	EventSequence    uint64          `json:"eventSequence"`
	CorrelationID    string          `json:"correlationId"`
	CausationID      string          `json:"causationId,omitempty"`
	OrganizationID   string          `json:"organizationId,omitempty"`
	Data             json.RawMessage `json:"data"`
}

// Validate checks required identifiers, positive versions, UTC microsecond
// precision and an object-shaped payload before a broker publish or storage.
// It does not validate the event-specific schema of Data.
// RU: Validate проверяет обязательные идентификаторы, положительные версии,
// точность времени UTC до микросекунды и форму объекта перед публикацией либо
// хранением. Схему Data для конкретного события метод не проверяет.
func (envelope Envelope) Validate() error {
	if _, err := uuid.Parse(envelope.EventID); err != nil ||
		envelope.EventName == "" || len(envelope.EventName) > 128 ||
		envelope.EventVersion == 0 || envelope.SchemaVersion == 0 ||
		envelope.OccurredAt.IsZero() ||
		envelope.AggregateType == "" || len(envelope.AggregateType) > 64 ||
		envelope.AggregateID == "" || len(envelope.AggregateID) > 128 ||
		envelope.AggregateVersion == 0 || envelope.EventSequence == 0 {
		return errors.New("event envelope is invalid")
	}
	if _, err := uuid.Parse(envelope.CorrelationID); err != nil {
		return errors.New("event correlation is invalid")
	}
	if envelope.CausationID != "" {
		if _, err := uuid.Parse(envelope.CausationID); err != nil {
			return errors.New("event causation is invalid")
		}
	}
	if envelope.OccurredAt.Location() != time.UTC ||
		envelope.OccurredAt.Nanosecond()%1000 != 0 {
		return errors.New("event time precision is invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(envelope.Data))
	var object map[string]json.RawMessage
	if err := decoder.Decode(&object); err != nil || object == nil {
		return errors.New("event data is invalid")
	}
	if decoder.Decode(&struct{}{}) == nil {
		return errors.New("event data has trailing value")
	}
	return nil
}

// Marshal validates the envelope before encoding it as JSON; on validation
// or encoding failure it returns no partial transport payload.
// RU: Marshal проверяет конверт до кодирования в JSON; при ошибке проверки
// либо кодирования частичная транспортная нагрузка не возвращается.
func (envelope Envelope) Marshal() ([]byte, error) {
	if err := envelope.Validate(); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return nil, errors.New("marshal event envelope")
	}
	return encoded, nil
}
