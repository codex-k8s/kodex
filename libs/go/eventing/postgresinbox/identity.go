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

// eventRecord pins a copied envelope to its canonical digest and database ordering key.
// eventRecord связывает копию конверта с каноническим дайджестом и ключом порядка в базе.
type eventRecord struct {
	Envelope    eventing.Envelope
	Digest      [sha256.Size]byte
	OrderingKey string
}

// newEventRecord validates bounded wire fields before deriving immutable delivery identity.
// newEventRecord проверяет ограниченные поля wire до вычисления неизменной идентичности доставки.
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

// cloneEnvelope detaches mutable JSON data so caller edits cannot change a received record.
// cloneEnvelope отделяет изменяемый JSON, чтобы правки вызывающего не меняли принятое событие.
func cloneEnvelope(envelope eventing.Envelope) eventing.Envelope {
	envelope.Data = append(json.RawMessage(nil), envelope.Data...)
	return envelope
}

// canonicalUUID accepts only nonzero UUIDs in their exact canonical lowercase representation.
// canonicalUUID допускает только ненулевые UUID в точном каноническом написании строчными буквами.
func canonicalUUID(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed != uuid.Nil && parsed.String() == value
}

// buildOrderingKey encodes the optional organization and aggregate identity as a bounded JSON array.
// buildOrderingKey кодирует необязательную организацию и агрегат ограниченным JSON-массивом.
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

// writeAuthorityDigest binds server-established actor, scope, operation, and key hash in fixed order.
// writeAuthorityDigest связывает назначенные сервером actor, область, операцию и хеш ключа в фиксированном порядке.
func writeAuthorityDigest(hash interface{ Write([]byte) (int, error) }, authority OperatorAuthority) {
	writeDigestPart(hash, authority.Actor)
	writeDigestPart(hash, authority.Organization)
	writeDigestPart(hash, authority.Project)
	writeDigestPart(hash, authority.Operation)
	_, _ = hash.Write(authority.KeyHash[:])
}

// writeDigestPart length-prefixes each string to prevent ambiguous digest concatenation.
// writeDigestPart добавляет длину перед строкой, исключая неоднозначное склеивание дайджеста.
func writeDigestPart(buffer interface{ Write([]byte) (int, error) }, value string) {
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(len(value)))
	_, _ = buffer.Write(size[:])
	_, _ = buffer.Write([]byte(value))
}

// sameDigest compares two complete SHA-256 digests and rejects truncated evidence.
// sameDigest сравнивает два полных SHA-256 дайджеста и отклоняет усечённые данные.
func sameDigest(left, right []byte) bool {
	return len(left) == sha256.Size && len(right) == sha256.Size &&
		bytes.Equal(left, right)
}

// sameOrderingKey compares decoded JSON components rather than their textual whitespace.
// sameOrderingKey сравнивает разобранные компоненты JSON, а не текстовые пробелы.
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

// validOrderingKey rejects malformed, empty, or oversized three- and four-part keys.
// validOrderingKey отклоняет неверные, пустые или слишком длинные ключи из трёх или четырёх частей.
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
