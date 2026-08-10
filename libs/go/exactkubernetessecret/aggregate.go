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
	RecordActive  = "ACTIVE"
	RecordRevoked = "REVOKED"
)

type Record struct {
	Version       uint64 `json:"version"`
	Status        string `json:"status"`
	ContentSHA256 string `json:"content_sha256"`
	Value         []byte `json:"value,omitempty"`
}

type Aggregate struct {
	SchemaVersion uint64            `json:"schema_version"`
	Generation    uint64            `json:"generation"`
	Records       map[string]Record `json:"records"`
	DigestSHA256  string            `json:"digest_sha256"`
}

type aggregateDigestInput struct {
	SchemaVersion uint64            `json:"schema_version"`
	Generation    uint64            `json:"generation"`
	Records       map[string]Record `json:"records"`
}

func NewAggregate() Aggregate {
	return Aggregate{SchemaVersion: 1, Generation: 1, Records: map[string]Record{}}
}

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

func ValidateForwardTransition(previous, next Aggregate) error {
	if previous.SchemaVersion != 1 || next.SchemaVersion != 1 ||
		previous.Generation == ^uint64(0) || next.Generation != previous.Generation+1 {
		return errors.New("exact Secret aggregate generation rollback rejected")
	}
	return nil
}

func CloneAggregate(document Aggregate) Aggregate {
	clone := document
	clone.Records = make(map[string]Record, len(document.Records))
	for ref, record := range document.Records {
		record.Value = bytes.Clone(record.Value)
		clone.Records[ref] = record
	}
	return clone
}

func ValueSHA256(value []byte) string { return digestValue(value) }

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

func digestValue(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}
