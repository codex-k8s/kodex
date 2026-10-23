package exactkubernetessecret

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

const (
	// RecordActive requires a nonempty bounded value matching its content digest.
	// RecordActive требует непустого ограниченного значения, совпадающего с дайджестом содержимого.
	RecordActive = "ACTIVE"
	// RecordRevoked retains a versioned tombstone with no value and the canonical zero digest.
	// RecordRevoked сохраняет версионную отметку отзыва без значения с каноническим нулевым дайджестом.
	RecordRevoked = "REVOKED"
)

// Record stores one versioned private value or revocation tombstone; its contents must never be logged or exposed as public metadata.
// Record хранит одно версионное частное значение или отметку отзыва; содержимое нельзя логировать и выдавать как публичные метаданные.
type Record struct {
	Version       uint64 `json:"version"`
	Status        string `json:"status"`
	ContentSHA256 string `json:"content_sha256"`
	Value         []byte `json:"value,omitempty"`
}

// Aggregate binds the schema, generation and private record map to a canonical digest for exact Secret storage.
// Aggregate связывает схему, поколение и словарь частных записей с каноническим дайджестом для хранения в точном Secret.
type Aggregate struct {
	SchemaVersion uint64            `json:"schema_version"`
	Generation    uint64            `json:"generation"`
	Records       map[string]Record `json:"records"`
	DigestSHA256  string            `json:"digest_sha256"`
}

// aggregateDigestInput excludes the digest field itself while preserving all generation and record material in the hash input.
// aggregateDigestInput исключает само поле дайджеста, сохраняя в хэшируемом входе все данные поколения и записей.
type aggregateDigestInput struct {
	SchemaVersion uint64            `json:"schema_version"`
	Generation    uint64            `json:"generation"`
	Records       map[string]Record `json:"records"`
}

// NewAggregate creates an empty generation-one document whose digest is assigned only by validated encoding.
// NewAggregate создаёт пустой документ первого поколения, дайджест которого назначается только при проверенном кодировании.
func NewAggregate() Aggregate {
	return Aggregate{SchemaVersion: 1, Generation: 1, Records: map[string]Record{}}
}

// DecodeAggregate checks one bounded JSON document, record limits, active or revoked value rules and the full digest before returning any private data.
// DecodeAggregate проверяет один ограниченный JSON-документ, лимиты записей, правила активных и отозванных значений и полный дайджест до возврата частных данных.
func DecodeAggregate(raw []byte, maximumRecords int) (Aggregate, error) {
	if len(raw) == 0 || len(raw) > 1<<20 || maximumRecords < 1 || maximumRecords > 4096 {
		return Aggregate{}, errors.New("exact Secret aggregate input is invalid")
	}
	var document Aggregate
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&document) != nil || decoder.Decode(&struct{}{}) != io.EOF ||
		document.SchemaVersion != 1 || document.Generation == 0 || document.Records == nil ||
		len(document.Records) > maximumRecords || len(document.DigestSHA256) != 64 {
		return Aggregate{}, errors.New("exact Secret aggregate is invalid")
	}
	for ref, record := range document.Records {
		if ref == "" || len(ref) > 512 || strings.ContainsAny(ref, "\x00\r\n") || record.Version == 0 {
			return Aggregate{}, errors.New("exact Secret aggregate record is invalid")
		}
		switch record.Status {
		case RecordActive:
			if len(record.Value) == 0 || len(record.Value) > 64<<10 || digestValue(record.Value) != record.ContentSHA256 {
				return Aggregate{}, errors.New("exact Secret aggregate record digest mismatch")
			}
		case RecordRevoked:
			if len(record.Value) != 0 || record.ContentSHA256 != strings.Repeat("0", 64) {
				return Aggregate{}, errors.New("exact Secret aggregate revoked record is invalid")
			}
		default:
			return Aggregate{}, errors.New("exact Secret aggregate record status is invalid")
		}
	}
	expected, err := aggregateDigest(document)
	if err != nil || expected != document.DigestSHA256 {
		return Aggregate{}, errors.New("exact Secret aggregate digest mismatch")
	}
	return document, nil
}

// EncodeAggregate computes the aggregate digest, serializes within the byte ceiling and validates its own output through the full decoder before returning bytes.
// EncodeAggregate вычисляет дайджест агрегата, сериализует в пределах байтового лимита и проверяет результат полным декодером до возврата байтов.
func EncodeAggregate(document Aggregate, maximumRecords int) ([]byte, error) {
	if document.SchemaVersion != 1 || document.Generation == 0 || document.Records == nil ||
		len(document.Records) > maximumRecords {
		return nil, errors.New("exact Secret aggregate output is invalid")
	}
	digest, err := aggregateDigest(document)
	if err != nil {
		return nil, err
	}
	document.DigestSHA256 = digest
	raw, err := json.Marshal(document)
	if err != nil || len(raw) == 0 || len(raw) > 1<<20 {
		return nil, errors.New("encode exact Secret aggregate")
	}
	if _, err = DecodeAggregate(raw, maximumRecords); err != nil {
		return nil, err
	}
	return raw, nil
}

// ValidateForwardTransition requires schema one and exactly one generation increment without unsigned overflow; record semantics remain the encoder and decoder's responsibility.
// ValidateForwardTransition требует первую схему и увеличение поколения ровно на единицу без беззнакового переполнения; смысл записей проверяют кодер и декодер.
func ValidateForwardTransition(previous, next Aggregate) error {
	if previous.SchemaVersion != 1 || next.SchemaVersion != 1 ||
		previous.Generation == ^uint64(0) || next.Generation != previous.Generation+1 {
		return errors.New("exact Secret aggregate generation rollback rejected")
	}
	return nil
}

// CloneAggregate copies the record map and each private byte slice while preserving the document's scalar metadata and digest.
// CloneAggregate копирует словарь записей и каждый частный байтовый срез, сохраняя скалярные метаданные и дайджест документа.
func CloneAggregate(document Aggregate) Aggregate {
	clone := document
	clone.Records = make(map[string]Record, len(document.Records))
	for ref, record := range document.Records {
		record.Value = bytes.Clone(record.Value)
		clone.Records[ref] = record
	}
	return clone
}

// ValueSHA256 returns the lowercase SHA-256 of exact value bytes without altering or retaining the input.
// ValueSHA256 возвращает строчный SHA-256 точных байтов значения без изменения и сохранения входа.
func ValueSHA256(value []byte) string { return digestValue(value) }

// aggregateDigest hashes the JSON schema, generation and record map only, excluding the previously stored aggregate digest.
// aggregateDigest хэширует только JSON схемы, поколения и словаря записей, исключая ранее сохранённый дайджест агрегата.
func aggregateDigest(document Aggregate) (string, error) {
	raw, err := json.Marshal(aggregateDigestInput{
		SchemaVersion: document.SchemaVersion, Generation: document.Generation, Records: document.Records,
	})
	if err != nil {
		return "", errors.New("digest exact Secret aggregate")
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// digestValue computes the lowercase hexadecimal SHA-256 used for exact active-record content checks.
// digestValue вычисляет строчный шестнадцатеричный SHA-256 для точной проверки содержимого активной записи.
func digestValue(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}
