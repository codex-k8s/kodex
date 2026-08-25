package internalrpcauth

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
)

const (
	// AuthorityProofTrustPurpose отделяет trust document proof-подписантов от
	// остальных наборов ключей internal RPC authority.
	AuthorityProofTrustPurpose     = "AUTHORITY_PROOF_VERIFICATION"
	maximumAuthorityProofTrustKeys = 32
	maximumTrustHistoryEntries     = 32
	zeroSHA256                     = "0000000000000000000000000000000000000000000000000000000000000000"
)

// TrustRevisionDigest задаёт одно звено forward-only цепочки trust-снимков.
type TrustRevisionDigest struct {
	Revision     uint64 `json:"revision"`
	DigestSHA256 string `json:"digest_sha256"`
}

// AuthorityProofTrustKey задаёт один ключ, которым workload подписывает
// короткоживущие authority proof.
type AuthorityProofTrustKey struct {
	Issuer     string          `json:"issuer"`
	Generation uint64          `json:"generation"`
	Status     string          `json:"status"`
	Purpose    string          `json:"purpose"`
	Audiences  []string        `json:"audiences"`
	NotBefore  int64           `json:"not_before"`
	NotAfter   int64           `json:"not_after"`
	JWK        json.RawMessage `json:"jwk"`
}

// AuthorityProofTrustDocument является единым canonical контрактом между
// publisher и всеми consumers набора authority proof ключей.
type AuthorityProofTrustDocument struct {
	Version        int                      `json:"v"`
	Purpose        string                   `json:"purpose"`
	SourceRevision uint64                   `json:"source_revision"`
	SourceDigest   string                   `json:"source_digest_sha256"`
	Predecessor    TrustRevisionDigest      `json:"predecessor"`
	History        []TrustRevisionDigest    `json:"history"`
	Keys           []AuthorityProofTrustKey `json:"keys"`
}

// DecodeAuthorityProofTrustDocument строго декодирует и проверяет provenance
// canonical trust document. Проверки конкретного issuer, audience и времени
// действия остаются обязанностью consumer.
func DecodeAuthorityProofTrustDocument(data []byte) (AuthorityProofTrustDocument, error) {
	var document AuthorityProofTrustDocument
	if err := DecodeCanonicalJSON(data, &document); err != nil {
		return AuthorityProofTrustDocument{}, errors.New("decode authority proof trust document")
	}
	if document.Version != ContractVersion ||
		document.Purpose != AuthorityProofTrustPurpose ||
		document.SourceRevision == 0 ||
		!validSHA256(document.SourceDigest) ||
		len(document.Keys) == 0 ||
		len(document.Keys) > maximumAuthorityProofTrustKeys {
		return AuthorityProofTrustDocument{}, errors.New("authority proof trust document metadata is invalid")
	}
	if err := validateTrustHistory(document.SourceRevision, document.Predecessor, document.History); err != nil {
		return AuthorityProofTrustDocument{}, err
	}
	keyIDs := make(map[string]struct{}, len(document.Keys))
	currentByIssuer := make(map[string]int)
	issuers := make(map[string]struct{})
	for _, key := range document.Keys {
		if key.Issuer == "" || key.Generation == 0 ||
			(key.Status != "CURRENT" && key.Status != "NEXT" && key.Status != "PREVIOUS") ||
			key.Purpose != "AUTHORITY_PROOF" || len(key.Audiences) == 0 ||
			key.NotBefore >= key.NotAfter {
			return AuthorityProofTrustDocument{}, errors.New("authority proof trust key metadata is invalid")
		}
		seenAudiences := make(map[string]struct{}, len(key.Audiences))
		for _, audience := range key.Audiences {
			if audience == "" {
				return AuthorityProofTrustDocument{}, errors.New("authority proof trust key audience is invalid")
			}
			if _, duplicate := seenAudiences[audience]; duplicate {
				return AuthorityProofTrustDocument{}, errors.New("authority proof trust key audience is duplicated")
			}
			seenAudiences[audience] = struct{}{}
		}
		parsed, err := ParsePublicJWK(key.JWK)
		if err != nil {
			return AuthorityProofTrustDocument{}, errors.New("authority proof trust public key is invalid")
		}
		if _, duplicate := keyIDs[parsed.KeyID]; duplicate {
			return AuthorityProofTrustDocument{}, errors.New("authority proof trust key id is duplicated")
		}
		keyIDs[parsed.KeyID] = struct{}{}
		issuers[key.Issuer] = struct{}{}
		if key.Status == "CURRENT" {
			currentByIssuer[key.Issuer]++
		}
	}
	for issuer := range issuers {
		if currentByIssuer[issuer] != 1 {
			return AuthorityProofTrustDocument{}, errors.New("authority proof trust issuer must have exactly one CURRENT key")
		}
	}
	return document, nil
}

func validateTrustHistory(
	sourceRevision uint64,
	predecessor TrustRevisionDigest,
	history []TrustRevisionDigest,
) error {
	if len(history) > maximumTrustHistoryEntries {
		return errors.New("authority proof trust history exceeds the bounded window")
	}
	if sourceRevision == 1 {
		if predecessor.Revision != 0 || predecessor.DigestSHA256 != zeroSHA256 || len(history) != 0 {
			return errors.New("authority proof trust bootstrap history is invalid")
		}
		return nil
	}
	if predecessor.Revision != sourceRevision-1 ||
		!validSHA256(predecessor.DigestSHA256) ||
		len(history) == 0 || uint64(len(history)) >= sourceRevision {
		return errors.New("authority proof trust predecessor is invalid")
	}
	firstRevision := sourceRevision - uint64(len(history))
	for index, entry := range history {
		if entry.Revision != firstRevision+uint64(index) || !validSHA256(entry.DigestSHA256) {
			return errors.New("authority proof trust history is gapped or malformed")
		}
	}
	last := history[len(history)-1]
	if last != predecessor {
		return errors.New("authority proof trust history does not end at the predecessor")
	}
	return nil
}

func validSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	if strings.ToLower(value) != value {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32
}
