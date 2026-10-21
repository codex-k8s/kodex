package postgresinbox

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"math"

	"github.com/codex-k8s/kodex/libs/go/eventing"
	"github.com/google/uuid"
)

type eventRecord struct {
	Envelope    eventing.Envelope
	Digest      [sha256.Size]byte
	OrderingKey string
}

func newEventRecord(envelope eventing.Envelope) (eventRecord, error) {
	envelope = cloneEnvelope(envelope)
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

func cloneEnvelope(envelope eventing.Envelope) eventing.Envelope {
	envelope.Data = append(json.RawMessage(nil), envelope.Data...)
	return envelope
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

// repairRequestDigest binds a repair attempt to its server-authorized actor and immutable evidence.
// repairRequestDigest связывает попытку исправления с разрешённым сервером actor и неизменяемым доказательством.
func repairRequestDigest(request RepairRequest, authority OperatorAuthority) [sha256.Size]byte {
	return operatorRequestDigest(authority, operatorDigestInput{
		Action: OperatorActionRepair, Consumer: request.Consumer, EventID: request.EventID,
		EventDigest: request.EventDigest, ExpectedGeneration: request.ExpectedGeneration,
		ExpectedFence: request.ExpectedFence, Reason: request.Reason, EvidenceDigest: request.EvidenceDigest,
	})
}

// recoveryRequestDigest binds a recovery attempt to its distinct action and immutable evidence.
// recoveryRequestDigest связывает попытку восстановления с отдельным действием и неизменяемым доказательством.
func recoveryRequestDigest(request RecoveryRequest, authority OperatorAuthority) [sha256.Size]byte {
	return operatorRequestDigest(authority, operatorDigestInput{
		Action: OperatorActionRecover, Consumer: request.Consumer, EventID: request.EventID,
		EventDigest: request.EventDigest, ExpectedGeneration: request.ExpectedGeneration,
		ExpectedFence: request.ExpectedFence, Reason: request.Reason, EvidenceDigest: request.EvidenceDigest,
	})
}

// operatorDigestInput carries only the server-bound fields shared by repair and recovery digests.
// operatorDigestInput содержит только серверно связанные поля, общие для дайджестов исправления и восстановления.
type operatorDigestInput struct {
	Action             OperatorAction
	Consumer           Consumer
	EventID            string
	EventDigest        [sha256.Size]byte
	ExpectedGeneration uint64
	ExpectedFence      uint64
	Reason             string
	EvidenceDigest     [sha256.Size]byte
}

// operatorRequestDigest encodes the original field order while keeping action domains distinct.
// operatorRequestDigest сохраняет прежний порядок полей, разделяя домены действий.
func operatorRequestDigest(authority OperatorAuthority, input operatorDigestInput) [sha256.Size]byte {
	hash := sha256.New()
	writeAuthorityDigest(hash, authority)
	writeDigestPart(hash, string(input.Action))
	writeDigestPart(hash, input.Consumer.Name)
	writeDigestPart(hash, input.Consumer.Scope)
	writeDigestPart(hash, input.EventID)
	hash.Write(input.EventDigest[:])
	var numbers [16]byte
	binary.BigEndian.PutUint64(numbers[:8], input.ExpectedGeneration)
	binary.BigEndian.PutUint64(numbers[8:], input.ExpectedFence)
	hash.Write(numbers[:])
	writeDigestPart(hash, input.Reason)
	hash.Write(input.EvidenceDigest[:])
	var result [sha256.Size]byte
	copy(result[:], hash.Sum(nil))
	return result
}

func writeAuthorityDigest(hash interface{ Write([]byte) (int, error) }, authority OperatorAuthority) {
	writeDigestPart(hash, authority.Actor)
	writeDigestPart(hash, authority.Organization)
	writeDigestPart(hash, authority.Project)
	writeDigestPart(hash, authority.Operation)
	_, _ = hash.Write(authority.KeyHash[:])
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
