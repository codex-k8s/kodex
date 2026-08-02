package postgresinbox

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"math"

	"github.com/codex-k8s/matter-codex/libs/go/eventing"
	"github.com/google/uuid"
)

type eventRecord struct {
	Envelope    eventing.Envelope
	Digest      [sha256.Size]byte
	OrderingKey string
}

func newEventRecord(envelope eventing.Envelope) (eventRecord, error) {
	encoded, err := envelope.Marshal()
	if err != nil {
		return eventRecord{}, ErrInvalidEvent
	}
	if envelope.EventVersion > math.MaxInt32 ||
		envelope.SchemaVersion > math.MaxInt32 ||
		envelope.AggregateVersion > math.MaxInt64 ||
		envelope.EventSequence > math.MaxInt64 ||
		len(envelope.OrganizationID) > 128 {
		return eventRecord{}, ErrInvalidEvent
	}
	if !canonicalUUID(envelope.EventID) || !canonicalUUID(envelope.CorrelationID) ||
		(envelope.CausationID != "" && !canonicalUUID(envelope.CausationID)) {
		return eventRecord{}, ErrInvalidEvent
	}
	orderingKey, err := buildOrderingKey(envelope)
	if err != nil {
		return eventRecord{}, err
	}
	return eventRecord{
		Envelope:    envelope,
		Digest:      sha256.Sum256(encoded),
		OrderingKey: orderingKey,
	}, nil
}

func canonicalUUID(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed != uuid.Nil && parsed.String() == value
}

func buildOrderingKey(envelope eventing.Envelope) (string, error) {
	parts := []string{
		envelope.EventName,
		envelope.AggregateType,
		envelope.AggregateID,
	}
	if envelope.OrganizationID != "" {
		parts = append([]string{envelope.OrganizationID}, parts...)
	}
	encoded, err := json.Marshal(parts)
	if err != nil {
		return "", ErrInvalidEvent
	}
	if len(encoded) < 7 || len(encoded) > 1024 {
		return "", ErrInvalidEvent
	}
	return string(encoded), nil
}

func repairRequestDigest(request RepairRequest, actor string) [sha256.Size]byte {
	hash := sha256.New()
	writeDigestPart(hash, request.Consumer.Name)
	writeDigestPart(hash, request.Consumer.Scope)
	writeDigestPart(hash, request.IdempotencyKey)
	writeDigestPart(hash, request.EventID)
	hash.Write(request.EventDigest[:])
	var numbers [16]byte
	binary.BigEndian.PutUint64(numbers[:8], request.ExpectedGeneration)
	binary.BigEndian.PutUint64(numbers[8:], request.ExpectedFence)
	hash.Write(numbers[:])
	writeDigestPart(hash, actor)
	writeDigestPart(hash, request.Reason)
	hash.Write(request.EvidenceDigest[:])
	var result [sha256.Size]byte
	copy(result[:], hash.Sum(nil))
	return result
}

func writeDigestPart(buffer interface{ Write([]byte) (int, error) }, value string) {
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(len(value)))
	_, _ = buffer.Write(size[:])
	_, _ = buffer.Write([]byte(value))
}

func sameDigest(left, right []byte) bool {
	return len(left) == sha256.Size && len(right) == sha256.Size &&
		bytes.Equal(left, right)
}

func sameOrderingKey(left, right string) bool {
	var leftParts []string
	var rightParts []string
	if err := json.Unmarshal([]byte(left), &leftParts); err != nil {
		return false
	}
	if err := json.Unmarshal([]byte(right), &rightParts); err != nil {
		return false
	}
	if len(leftParts) != len(rightParts) {
		return false
	}
	for index := range leftParts {
		if leftParts[index] != rightParts[index] {
			return false
		}
	}
	return true
}

func validOrderingKey(value string) bool {
	var parts []string
	if err := json.Unmarshal([]byte(value), &parts); err != nil ||
		(len(parts) != 3 && len(parts) != 4) || len(value) < 7 || len(value) > 1024 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
	}
	return true
}
